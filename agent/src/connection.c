/**
 * connection.c — Connexion WSS persistante via libwebsockets (mTLS)
 *
 * Flux de connexion :
 *   leo_conn_create()
 *     → parse ws_endpoint (hôte, port, chemin)
 *     → configure lws_context avec mTLS (cert client + CA pinné)
 *     → lance _lws_thread
 *
 *   _lws_thread
 *     → lws_client_connect_via_info() → callbacks LWS
 *     → LWS_CALLBACK_CLIENT_ESTABLISHED : marque connected=true, envoie HELLO
 *     → LWS_CALLBACK_CLIENT_RECEIVE : appelle handler()
 *     → LWS_CALLBACK_CLIENT_WRITEABLE : vide send_queue
 *     → LWS_CALLBACK_CLIENT_CLOSED : backoff exponentiel, reconnexion
 *
 * Thread-safety :
 *   send_queue est protégée par queue_mutex.
 *   lws_cancel_service() est thread-safe et réveille la boucle LWS.
 */
#include "connection.h"
#include "logger.h"
#include "crypto.h"
#include "protocol.h"

#include <libwebsockets.h>
#include <openssl/x509.h>
#include <openssl/x509_vfy.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <time.h>

/* ─── Structure interne ─────────────────────────────────────────────────── */

typedef struct {
    char   data[LEO_MAX_MSG_SIZE];
    size_t len;
} queued_msg_t;

struct leo_conn {
    /* libwebsockets */
    struct lws_context    *lws_ctx;
    struct lws            *wsi;

    /* Paramètres de connexion (parsés depuis ws_endpoint) */
    char    host[256];
    int     port;
    char    path[256];

    /* File d'envoi thread-safe */
    pthread_mutex_t  queue_mutex;
    queued_msg_t     send_queue[LEO_SEND_QUEUE_DEPTH];
    int              queue_head;
    int              queue_tail;
    int              queue_count;

    /* État */
    volatile bool    connected;
    volatile bool    should_stop;
    /* Distinct de `!connected` : celui-ci ne devient vrai que sur un échec
     * ou une fermeture effective (callbacks CONNECTION_ERROR/CLOSED), pas
     * pendant la poignée de main TLS — voir _lws_thread : `!connected` seul
     * est aussi vrai *avant* que la connexion soit établie (wsi pas encore
     * assigné), ce qui faisait sortir la boucle événementielle dès le
     * premier lws_service() de chaque tentative, avant même la fin de la
     * poignée de main mTLS (qui prend plusieurs passages non-bloquants).
     * Résultat observé : reconnexion en boucle sans jamais aboutir dès que
     * le serveur exige mTLS (poignée de main trop lente pour un seul
     * lws_service(ctx, 50)). */
    volatile bool    attempt_failed;
    int              reconnect_delay_ms;

    /* Thread LWS */
    pthread_t        thread;

    /* Callback utilisateur */
    leo_msg_handler_t handler;
    void             *userdata;

    /* Référence à la configuration (non owné) */
    const leo_config_t *config;
};

/* ─── Callbacks libwebsockets ───────────────────────────────────────────── */

static int _lws_callback(struct lws *wsi, enum lws_callback_reasons reason,
                         void *user, void *in, size_t len)
{
    /* Récupère le contexte leo_conn via les données utilisateur du protocole */
    struct lws_context *ctx = lws_get_context(wsi);
    leo_conn_t *conn = (leo_conn_t *)lws_context_user(ctx);
    if (!conn) return 0;

    switch (reason) {

    case LWS_CALLBACK_CLIENT_ESTABLISHED: {
        conn->wsi       = wsi;
        conn->connected = true;
        conn->reconnect_delay_ms = LEO_RECONNECT_INIT_MS;
        LOG_INFO("WSS connecté à %s:%d%s", conn->host, conn->port, conn->path);

        /* Premier message de la session : identifie l'agent au backend
         * (voir Dispatcher.handleHello côté Go) — c'est ce qui fait passer
         * l'agent à l'état "online" ; sans lui, le statut ne changerait
         * qu'au premier heartbeat (jusqu'à 3h plus tard avec l'intervalle
         * par défaut actuel). */
        char hello_buf[LEO_MAX_MSG_SIZE];
        int  hello_len = leo_proto_build_hello(conn->config, hello_buf, sizeof(hello_buf));
        if (hello_len > 0) {
            leo_conn_send(conn, hello_buf, (size_t)hello_len);
        } else {
            LOG_ERROR("Échec de construction du message HELLO");
        }

        /* Sûr ici (thread LWS) : arme WRITEABLE pour ce wsi directement.
         * Voir LWS_CALLBACK_EVENT_WAIT_CANCELLED plus bas pour le cas
         * général (message mis en file depuis un AUTRE thread). */
        lws_callback_on_writable(wsi);
        break;
    }

    case LWS_CALLBACK_CLIENT_RECEIVE:
        if (in && len > 0 && conn->handler) {
            /* S'assurer que la chaîne est terminée par \0 avant de la passer */
            char buf[LEO_MAX_MSG_SIZE + 1];
            size_t copy_len = (len < LEO_MAX_MSG_SIZE) ? len : LEO_MAX_MSG_SIZE;
            memcpy(buf, in, copy_len);
            buf[copy_len] = '\0';

            conn->handler(buf, copy_len, conn->userdata);
        }
        break;

    case LWS_CALLBACK_CLIENT_WRITEABLE:
        pthread_mutex_lock(&conn->queue_mutex);
        if (conn->queue_count > 0) {
            queued_msg_t *msg = &conn->send_queue[conn->queue_head];

            /* libwebsockets exige LWS_PRE octets libres avant le payload */
            unsigned char frame[LWS_PRE + LEO_MAX_MSG_SIZE];
            size_t copy_len = (msg->len < LEO_MAX_MSG_SIZE) ? msg->len : LEO_MAX_MSG_SIZE;
            memcpy(frame + LWS_PRE, msg->data, copy_len);

            int rc = lws_write(wsi, frame + LWS_PRE, copy_len, LWS_WRITE_TEXT);
            if (rc < 0) {
                LOG_ERROR("lws_write a échoué (rc=%d)", rc);
            }

            conn->queue_head = (conn->queue_head + 1) % LEO_SEND_QUEUE_DEPTH;
            conn->queue_count--;

            /* S'il reste des messages, on redemande à être notifié */
            if (conn->queue_count > 0) {
                lws_callback_on_writable(wsi);
            }
        }
        pthread_mutex_unlock(&conn->queue_mutex);
        break;

    case LWS_CALLBACK_EVENT_WAIT_CANCELLED:
        /* Réveillé par lws_cancel_service() (voir leo_conn_send(), appelé
         * depuis d'autres threads — heartbeat/métriques/exécution de
         * commandes). Ce callback s'exécute sur le thread LWS lui-même :
         * c'est le SEUL endroit sûr pour armer WRITEABLE en réponse à un
         * message mis en file par un thread étranger — lws_cancel_service()
         * seul ne fait que réveiller poll(), il ne (re)programme pas
         * l'intérêt POLLOUT du socket (lws_callback_on_writable modifie la
         * table de poll fds, ce qui n'est pas thread-safe hors du thread
         * LWS). Sans ce relais, un message enqueué après l'établissement de
         * la connexion ne serait jamais transmis. */
        pthread_mutex_lock(&conn->queue_mutex);
        {
            bool has_pending = conn->queue_count > 0;
            pthread_mutex_unlock(&conn->queue_mutex);
            if (conn->wsi && has_pending) {
                lws_callback_on_writable(conn->wsi);
            }
        }
        break;

    case LWS_CALLBACK_CLIENT_CLOSED:
    case LWS_CALLBACK_CLIENT_CONNECTION_ERROR:
        conn->connected      = false;
        conn->wsi            = NULL;
        conn->attempt_failed = true;
        if (reason == LWS_CALLBACK_CLIENT_CONNECTION_ERROR) {
            LOG_ERROR("Erreur de connexion WSS : %s",
                      in ? (const char *)in : "(inconnue)");
        } else {
            LOG_INFO("Connexion WSS fermée");
        }
        break;

    case LWS_CALLBACK_OPENSSL_PERFORM_SERVER_CERT_VERIFICATION: {
        /* Pinning strict : on ignore la validation de chaîne OpenSSL
         * (preverify_ok, dans `len`) et on n'accepte que si le certificat
         * présenté correspond exactement à l'empreinte SHA-256 épinglée
         * dans la config. C'est la vérification serveur — indispensable,
         * sinon n'importe quel serveur (attaquant MITM inclus) est accepté.
         *
         * OpenSSL invoque ce callback UNE FOIS PAR CERTIFICAT de la chaîne
         * présentée (profondeur decroissante jusqu'à la feuille, profondeur
         * 0). Si le serveur envoie plus qu'un unique certificat auto-signé
         * (ex: feuille + CA intermédiaire), comparer l'empreinte épinglée à
         * CHAQUE profondeur rejetterait la poignée de main dès qu'un
         * certificat intermédiaire ne correspond pas — même si la feuille,
         * seule chose qui nous intéresse ici, correspond bien. On ne juge
         * donc que la profondeur 0 (la feuille) ; les autres profondeurs
         * sont acceptées sans vérification, la décision réelle étant prise
         * une fois la profondeur 0 atteinte. */
        X509_STORE_CTX *store_ctx = (X509_STORE_CTX *)user;
        if (!store_ctx) {
            LOG_ERROR("Certificat serveur rejeté — contexte de vérification absent");
            return 1;
        }

        if (X509_STORE_CTX_get_error_depth(store_ctx) != 0) {
            /* Certificat intermédiaire/CA : pas notre feuille, pas de
             * décision de pinning à prendre ici. */
            return 0;
        }

        X509       *peer_cert   = X509_STORE_CTX_get_current_cert(store_ctx);
        const char *expected_fp = conn->config ? conn->config->ca_fingerprint : NULL;

        if (peer_cert && expected_fp &&
            leo_crypto_x509_fingerprint_matches(peer_cert, expected_fp)) {
            /* Empreinte épinglée validée : on force le statut OK même si
             * OpenSSL a signalé une erreur de chaîne (cert auto-signé /
             * CA inconnue — attendu, on ne fait pas confiance à une
             * chaîne mais à une empreinte précise). */
            X509_STORE_CTX_set_error(store_ctx, X509_V_OK);
            return 0;
        }

        LOG_ERROR("Certificat serveur rejeté — empreinte non épinglée (pinning échoué)");
        return 1;
    }

    default:
        break;
    }

    return 0;
}

static struct lws_protocols g_protocols[] = {
    {
        .name                  = "leo-agent-v1",
        .callback              = _lws_callback,
        .per_session_data_size = 0,
        .rx_buffer_size        = LEO_MAX_MSG_SIZE,
    },
    { 0 }  /* terminateur */
};

/* ─── Thread de la boucle événementielle LWS ────────────────────────────── */

static void *_lws_thread(void *arg) {
    leo_conn_t *conn = (leo_conn_t *)arg;

    while (!conn->should_stop) {

        /* ── Création du contexte LWS avec mTLS ── */
        struct lws_context_creation_info ctx_info = {0};
        ctx_info.port      = CONTEXT_PORT_NO_LISTEN;
        ctx_info.protocols = g_protocols;
        ctx_info.user      = conn;
        ctx_info.options   = LWS_SERVER_OPTION_DO_SSL_GLOBAL_INIT;

        /* Certificat client (mTLS) */
        ctx_info.client_ssl_cert_filepath       = LEO_CLIENT_CERT_FILE;
        ctx_info.client_ssl_private_key_filepath = LEO_CLIENT_KEY_FILE;
        /* Pas de client_ssl_ca_filepath : on ne valide pas une chaîne de
         * confiance mais l'empreinte exacte du certificat serveur, dans
         * LWS_CALLBACK_OPENSSL_PERFORM_SERVER_CERT_VERIFICATION ci-dessous. */

        conn->lws_ctx = lws_create_context(&ctx_info);
        if (!conn->lws_ctx) {
            LOG_ERROR("Impossible de créer le contexte libwebsockets");
            goto reconnect;
        }

        /* ── Initiation de la connexion ── */
        struct lws_client_connect_info ci = {0};
        ci.context        = conn->lws_ctx;
        ci.address        = conn->host;
        ci.port           = conn->port;
        ci.path           = conn->path;
        ci.host           = conn->host;
        ci.origin         = conn->host;
        ci.protocol       = g_protocols[0].name;
        /* Pas de LCCSCF_ALLOW_SELFSIGNED : le pinning strict dans
         * LWS_CALLBACK_OPENSSL_PERFORM_SERVER_CERT_VERIFICATION est la
         * seule autorité de validation du certificat serveur. */
        ci.ssl_connection = LCCSCF_USE_SSL;

        conn->attempt_failed = false;

        struct lws *wsi = lws_client_connect_via_info(&ci);
        if (!wsi) {
            LOG_ERROR("lws_client_connect_via_info a échoué");
            lws_context_destroy(conn->lws_ctx);
            conn->lws_ctx = NULL;
            goto reconnect;
        }

        LOG_INFO("Connexion WSS en cours vers %s:%d%s…",
                 conn->host, conn->port, conn->path);

        /* ── Boucle événementielle ── */
        while (!conn->should_stop) {
            if (lws_service(conn->lws_ctx, 50) < 0) break;

            /* Sortir pour reconnecter seulement sur échec/fermeture avérés
             * (voir attempt_failed) — pas simplement "pas encore connecté",
             * qui est aussi vrai en plein milieu de la poignée de main. */
            if (conn->attempt_failed) break;
        }

        lws_context_destroy(conn->lws_ctx);
        conn->lws_ctx   = NULL;
        conn->connected = false;

        if (conn->should_stop) break;

    reconnect:
        LOG_INFO("Reconnexion dans %d ms…", conn->reconnect_delay_ms);
        {
            int elapsed = 0;
            while (!conn->should_stop && elapsed < conn->reconnect_delay_ms) {
                struct timespec ts = { .tv_sec = 0, .tv_nsec = 100 * 1000000L };
                nanosleep(&ts, NULL);
                elapsed += 100;
            }
        }

        /* Backoff exponentiel : init → init+step → ... → max */
        conn->reconnect_delay_ms += LEO_RECONNECT_STEP_MS;
        if (conn->reconnect_delay_ms > LEO_RECONNECT_MAX_MS)
            conn->reconnect_delay_ms = LEO_RECONNECT_MAX_MS;
    }

    LOG_INFO("Thread WSS terminé");
    return NULL;
}

/* ─── Parsing de ws_endpoint ────────────────────────────────────────────── */

/**
 * Parse "wss://host:port/path" en host, port, path.
 * Si port absent : 443 par défaut.
 */
static bool _parse_endpoint(const char *endpoint, char *host, size_t hsz,
                             int *port, char *path, size_t psz) {
    const char *p = endpoint;

    if (strncmp(p, "wss://", 6) == 0) p += 6;
    else if (strncmp(p, "ws://", 5) == 0) p += 5;

    const char *slash = strchr(p, '/');
    const char *colon = strchr(p, ':');

    if (colon && (!slash || colon < slash)) {
        /* host:port/path */
        size_t host_len = (size_t)(colon - p);
        if (host_len >= hsz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = atoi(colon + 1);
    } else if (slash) {
        /* host/path (port par défaut 443) */
        size_t host_len = (size_t)(slash - p);
        if (host_len >= hsz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = 443;
    } else {
        /* host uniquement */
        strncpy(host, p, hsz - 1);
        *port = 443;
        slash = NULL;
    }

    strncpy(path, slash ? slash : "/ws/agent", psz - 1);
    return (host[0] != '\0' && *port > 0);
}

/* ─── API publique ────────────────────────────────────────────────────── */

leo_conn_t *leo_conn_create(const leo_config_t *cfg,
                            leo_msg_handler_t   handler,
                            void               *userdata) {
    if (!cfg || !handler) return NULL;

    leo_conn_t *conn = calloc(1, sizeof(*conn));
    if (!conn) return NULL;

    conn->handler             = handler;
    conn->userdata            = userdata;
    conn->config              = cfg;
    conn->reconnect_delay_ms  = LEO_RECONNECT_INIT_MS;
    conn->connected           = false;
    conn->should_stop         = false;

    pthread_mutex_init(&conn->queue_mutex, NULL);

    if (!_parse_endpoint(cfg->ws_endpoint,
                         conn->host, sizeof(conn->host),
                         &conn->port,
                         conn->path, sizeof(conn->path))) {
        LOG_FATAL("ws_endpoint invalide : %s", cfg->ws_endpoint);
        free(conn);
        return NULL;
    }

    LOG_INFO("Endpoint WSS : %s:%d%s", conn->host, conn->port, conn->path);

    if (pthread_create(&conn->thread, NULL, _lws_thread, conn) != 0) {
        LOG_FATAL("Impossible de créer le thread WSS");
        pthread_mutex_destroy(&conn->queue_mutex);
        free(conn);
        return NULL;
    }

    return conn;
}

leo_error_t leo_conn_send(leo_conn_t *conn, const char *data, size_t len) {
    if (!conn || !data || len == 0) return LEO_ERR_NETWORK;
    if (!conn->connected)           return LEO_ERR_NETWORK;
    if (len > LEO_MAX_MSG_SIZE)     len = LEO_MAX_MSG_SIZE;

    pthread_mutex_lock(&conn->queue_mutex);

    if (conn->queue_count >= LEO_SEND_QUEUE_DEPTH) {
        pthread_mutex_unlock(&conn->queue_mutex);
        LOG_WARN("File d'envoi pleine — message abandonné");
        return LEO_ERR_QUEUE_FULL;
    }

    queued_msg_t *slot = &conn->send_queue[conn->queue_tail];
    memcpy(slot->data, data, len);
    slot->len = len;

    conn->queue_tail = (conn->queue_tail + 1) % LEO_SEND_QUEUE_DEPTH;
    conn->queue_count++;

    pthread_mutex_unlock(&conn->queue_mutex);

    /* Réveille la boucle LWS pour vider la file */
    if (conn->lws_ctx) {
        lws_cancel_service(conn->lws_ctx);
    }

    return LEO_OK;
}

bool leo_conn_is_connected(const leo_conn_t *conn) {
    return conn && conn->connected;
}

bool leo_conn_destroy(leo_conn_t *conn) {
    if (!conn) return true;

    conn->should_stop = true;

    /* Réveille la boucle pour qu'elle teste should_stop */
    if (conn->lws_ctx) lws_cancel_service(conn->lws_ctx);

    /* Attendre le thread (max 5s). Si le thread ne répond pas (ex: bloqué
     * dans une résolution DNS), on NE détruit PAS le mutex ni ne libère
     * conn : le thread continuerait à y accéder après coup (use-after-free).
     * On accepte une fuite plutôt qu'un crash / UB dans ce cas rare — et on
     * le SIGNALE à l'appelant (valeur de retour) : conn->config pointe vers
     * de la mémoire non-owned (ex: le leo_config_t d'un leo_agent_t) que le
     * thread encore actif va continuer à déréférencer, donc l'appelant ne
     * doit surtout pas libérer cette mémoire tant que le process tourne. */
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    ts.tv_sec += 5;
    if (pthread_timedjoin_np(conn->thread, NULL, &ts) != 0) {
        LOG_ERROR("Thread WSS non joignable après 5s — abandon (fuite volontaire, "
                  "évite un use-after-free si le thread est encore actif)");
        return false;
    }

    pthread_mutex_destroy(&conn->queue_mutex);

    LOG_INFO("Connexion WSS libérée");
    free(conn);
    return true;
}
