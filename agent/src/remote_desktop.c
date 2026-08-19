/**
 * remote_desktop.c — voir remote_desktop.h
 *
 * Une session = un thread détaché dédié, symétrique à _lws_thread dans
 * connection.c (même mTLS, même pinning serveur) mais avec sa propre boucle
 * de service lws_service() et son propre rythme d'émission (capture → JPEG →
 * lws_write binaire), au lieu de la file d'envoi générique de connection.c —
 * inadaptée ici (messages haute fréquence, taille variable, jamais du JSON).
 *
 * Au plus une session à la fois (état global g_rd, protégé par g_rd_mutex) :
 * borne cohérente avec la contrainte "une session active par agent" déjà
 * imposée côté backend (remotedesktop.Repository.ActiveForAgent).
 */
#include "remote_desktop.h"
#include "protocol.h"
#include "logger.h"
#include "../third_party/cjson/cJSON.h"

#include <libwebsockets.h>
#include <openssl/x509.h>
#include <openssl/x509_vfy.h>
#include <turbojpeg.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <errno.h>

/* leo_crypto_x509_fingerprint_matches() — même pinning que connection.c. */
#include "crypto.h"

/* ─── Types de message du protocole binaire de la connexion dédiée ───────── */

#define _RD_WIRE_FRAME         0x01
#define _RD_WIRE_INPUT_MOVE    0x10
#define _RD_WIRE_INPUT_BUTTON  0x11
#define _RD_WIRE_INPUT_SCROLL  0x12
#define _RD_WIRE_INPUT_KEY     0x13
#define _RD_WIRE_CONTROL       0x20

/* ─── Contexte d'une session en cours (user pointer du lws_context dédié) ── */

typedef struct {
    /* Paramètres, copiés depuis leo_rd_start() — stables pour la durée du thread. */
    char                 session_id[LEO_UUID_STR_LEN];
    char                 ws_url[LEO_RD_WS_URL_MAX_LEN];
    bool                 control_mode;
    int                  fps;
    int                  quality;
    int                  max_width;
    int                  max_height;
    const leo_config_t  *config;      /* non-owned, voir leo_rd_stop_all() */
    leo_conn_t          *control_conn; /* non-owned, pour LEO_MSG_REMOTE_DESKTOP_STATUS */

    /* Connexion dédiée parsée depuis ws_url. */
    char                 host[256];
    int                  port;
    char                 path[LEO_RD_WS_URL_MAX_LEN];

    /* Runtime — utilisé uniquement depuis le thread de session, sauf
     * `should_stop` (voir plus bas, écrit par leo_rd_stop()/leo_rd_stop_all()
     * depuis un autre thread). */
    struct lws          *wsi;
    leo_rd_capture_t    *cap;
    leo_rd_input_t      *input;
    tjhandle              tj;
    unsigned char        *jpeg_buf;
    unsigned long         jpeg_buf_size;
    uint32_t              frame_seq;
    int                   consecutive_capture_failures;

    volatile bool         established;
    volatile bool         done;
    char                  fail_reason[128]; /* vide si fin normale */

    /* Écrit par le thread de session lui-même ET lu/écrit par
     * leo_rd_stop()/leo_rd_stop_all() (autre thread) — c'est le SEUL champ de
     * ce contexte partagé entre threads après le démarrage, d'où `volatile`
     * ici alors que le reste de la struct ne l'est pas. */
    volatile bool         should_stop;
} _rd_session_ctx_t;

/* ─── État global : au plus une session active ────────────────────────────
 *
 * PTHREAD_MUTEX_INITIALIZER : initialisation statique, pas de leo_rd_init()
 * à appeler — cohérent avec le reste du module (créé/détruit à la demande
 * par leo_rd_start(), pas au démarrage de l'agent). */
static pthread_mutex_t     g_rd_mutex = PTHREAD_MUTEX_INITIALIZER; /* protège g_rd_ctx */
static _rd_session_ctx_t  *g_rd_ctx = NULL;   /* NULL si aucune session active */

/* g_rd_thread_running est un état DISTINCT de "g_rd_ctx != NULL", protégé
 * par son propre mutex (g_rd_exit_mutex, jamais g_rd_mutex) : il ne sert
 * qu'à l'attente bornée de leo_rd_stop_all(), signalée par le thread de
 * session juste avant de retourner (voir _rd_session_thread) — une seule
 * discipline de verrouillage par variable, pas de va-et-vient entre deux
 * mutex pour le même champ. */
static bool                g_rd_thread_running = false;
static pthread_mutex_t     g_rd_exit_mutex = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t      g_rd_exit_cond  = PTHREAD_COND_INITIALIZER;

/* ─── Parsing de ws_url (wss://host:port/path?query) ──────────────────────
 * Même forme que _parse_endpoint() dans connection.c (dupliqué plutôt que
 * partagé : 15 lignes, deux formats d'URL différents à terme — connection.c
 * vise toujours /ws/agent par défaut, ici il n'y a jamais de défaut puisque
 * le backend fournit toujours un chemin+jeton en query string). */
static bool _parse_ws_url(const char *url, char *host, size_t hsz,
                           int *port, char *path, size_t psz) {
    const char *p = url;
    if (strncmp(p, "wss://", 6) == 0) p += 6;
    else if (strncmp(p, "ws://", 5) == 0) p += 5;

    const char *slash = strchr(p, '/');
    const char *colon = strchr(p, ':');

    if (colon && (!slash || colon < slash)) {
        size_t host_len = (size_t)(colon - p);
        if (host_len == 0 || host_len >= hsz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = atoi(colon + 1);
        if (*port <= 0 || *port > 65535) return false;
    } else {
        size_t host_len = slash ? (size_t)(slash - p) : strlen(p);
        if (host_len == 0 || host_len >= hsz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = 443;
    }

    strncpy(path, slash ? slash : "/", psz - 1);
    path[psz - 1] = '\0';
    return host[0] != '\0';
}

/* ─── Rapport d'erreur/fin au backend (canal de contrôle) ─────────────────── */

static void _report_status(_rd_session_ctx_t *sctx, const char *status, const char *err) {
    if (!sctx->control_conn) return;
    char buf[512];
    int wlen = leo_proto_build_remote_desktop_status(sctx->session_id, status, err, buf, sizeof(buf));
    if (wlen > 0) leo_conn_send(sctx->control_conn, buf, (size_t)wlen);
}

/* ─── Décodage des messages entrants (input navigateur, contrôle) ─────────── */

static void _handle_input_message(_rd_session_ctx_t *sctx, const uint8_t *data, size_t len) {
    /* Défense en profondeur : le relais backend filtre déjà ces types en
     * mode "view" (voir Relay.pump côté Go), mais l'agent ne fait pas
     * confiance à la connexion pour respecter le mode par elle-même. */
    if (!sctx->control_mode || !sctx->input) return;

    switch (data[0]) {
    case _RD_WIRE_INPUT_MOVE:
        if (len >= 5) {
            int x = (data[1] << 8) | data[2];
            int y = (data[3] << 8) | data[4];
            leo_rd_input_move(sctx->input, x, y);
        }
        break;
    case _RD_WIRE_INPUT_BUTTON:
        if (len >= 3) {
            leo_rd_input_button(sctx->input, data[1], data[2] != 0);
        }
        break;
    case _RD_WIRE_INPUT_SCROLL:
        if (len >= 3) {
            int16_t delta = (int16_t)((data[1] << 8) | data[2]);
            leo_rd_input_scroll(sctx->input, delta);
        }
        break;
    case _RD_WIRE_INPUT_KEY:
        if (len >= 3) {
            leo_rd_input_key(sctx->input, (leo_rd_key_t)data[1], data[2] != 0);
        }
        break;
    default:
        break;
    }
}

static void _handle_control_message(_rd_session_ctx_t *sctx, const uint8_t *data, size_t len) {
    /* data[0] == _RD_WIRE_CONTROL, le JSON suit. cJSON_ParseWithLength ne
     * requiert pas de terminateur NUL — important ici, `data` vient
     * directement du buffer de réception lws, pas d'une chaîne C. */
    if (len < 2) return;
    cJSON *root = cJSON_ParseWithLength((const char *)data + 1, len - 1);
    if (!root) return;

    cJSON *jtype = cJSON_GetObjectItemCaseSensitive(root, "type");
    if (cJSON_IsString(jtype) && strcmp(jtype->valuestring, "stop") == 0) {
        LOG_INFO("Bureau à distance : arrêt demandé par le navigateur (session_id=%s)", sctx->session_id);
        sctx->done = true;
    }
    cJSON_Delete(root);
}

/* ─── Cadencement des frames ────────────────────────────────────────────────
 *
 * IMPORTANT : lws_service(ctx, timeout_ms) n'honore PAS timeout_ms dans cette
 * version de libwebsockets — voir lib/plat/unix/unix-service.c,
 * _lws_plat_service_tsi() : tout timeout_ms >= 0 passé par l'appelant est
 * silencieusement remplacé par un maximum interne ("23 days"), le vrai temps
 * d'attente étant déterminé par le prochain événement déjà programmé
 * (fd prêt, ou "sul" — scheduled unit list — arrivé à échéance). Une
 * première implémentation de ce module s'appuyait sur une boucle externe
 * appelant lws_service(ctx, 10) puis vérifiant elle-même si une frame était
 * due : ça ne fonctionne PAS avec ce comportement — le service se bloquait
 * plusieurs secondes (observé ~5s, temps mort interne par défaut) entre deux
 * frames au lieu des ~125ms attendus (vérifié en conditions réelles : agent
 * C réel, capture X11 réelle, voir l'historique de ce fichier).
 *
 * La bonne façon de cadencer un envoi périodique avec cette bibliothèque est
 * d'utiliser SON propre mécanisme de programmation par wsi
 * (lws_set_timer_usecs), qui alimente le calcul interne de la prochaine
 * échéance ripe et réveille donc lws_service() au bon moment — voir
 * LWS_CALLBACK_TIMER ci-dessous. */
static void _arm_frame_timer(_rd_session_ctx_t *sctx) {
    lws_usec_t interval_us = 1000000 / (lws_usec_t)(sctx->fps > 0 ? sctx->fps : 1);
    lws_set_timer_usecs(sctx->wsi, interval_us);
}

/** Capture, encode et envoie une frame — appelé depuis LWS_CALLBACK_CLIENT_WRITEABLE. */
static void _send_frame(_rd_session_ctx_t *sctx) {
    leo_rd_frame_t frame;
    if (!leo_rd_capture_grab(sctx->cap, &frame)) {
        sctx->consecutive_capture_failures++;
        if (sctx->consecutive_capture_failures >= 20) {
            snprintf(sctx->fail_reason, sizeof(sctx->fail_reason), "Échecs de capture d'écran répétés");
            sctx->done = true;
        }
        return;
    }
    sctx->consecutive_capture_failures = 0;

    int rc = tjCompress2(sctx->tj, frame.pixels, frame.width, 0, frame.height,
                          TJPF_BGRX, &sctx->jpeg_buf, &sctx->jpeg_buf_size,
                          TJSAMP_420, sctx->quality, TJFLAG_FASTDCT);
    if (rc != 0) {
        LOG_WARN("Échec de compression JPEG (bureau à distance) : %s", tjGetErrorStr2(sctx->tj));
        return;
    }

    /* En-tête protocole (voir remote_desktop.h) + LWS_PRE requis par
     * libwebsockets avant le payload (même contrainte que connection.c). */
    size_t header_len = 1 + 2 + 2 + 4;
    size_t total_len  = header_len + sctx->jpeg_buf_size;
    unsigned char *frame_buf = malloc(LWS_PRE + total_len);
    if (!frame_buf) return;

    unsigned char *w = frame_buf + LWS_PRE;
    w[0] = _RD_WIRE_FRAME;
    w[1] = (unsigned char)((frame.width  >> 8) & 0xFF);
    w[2] = (unsigned char)(frame.width   & 0xFF);
    w[3] = (unsigned char)((frame.height >> 8) & 0xFF);
    w[4] = (unsigned char)(frame.height  & 0xFF);
    w[5] = (unsigned char)((sctx->frame_seq >> 24) & 0xFF);
    w[6] = (unsigned char)((sctx->frame_seq >> 16) & 0xFF);
    w[7] = (unsigned char)((sctx->frame_seq >> 8)  & 0xFF);
    w[8] = (unsigned char)(sctx->frame_seq & 0xFF);
    memcpy(w + header_len, sctx->jpeg_buf, sctx->jpeg_buf_size);
    sctx->frame_seq++;

    int wrc = lws_write(sctx->wsi, w, total_len, LWS_WRITE_BINARY);
    if (wrc < 0) {
        LOG_WARN("Échec d'envoi d'une frame (bureau à distance)");
    }
    free(frame_buf);
}

/* ─── Callback libwebsockets de la connexion dédiée ───────────────────────── */

static int _rd_lws_callback(struct lws *wsi, enum lws_callback_reasons reason,
                             void *user, void *in, size_t len) {
    /* `user` n'est réellement utilisé que dans le case
     * OPENSSL_PERFORM_SERVER_CERT_VERIFICATION plus bas (il y désigne un
     * X509_STORE_CTX*, pas le user pointer lws habituel — voir sa
     * documentation). */
    struct lws_context *ctx = lws_get_context(wsi);
    _rd_session_ctx_t *sctx = (_rd_session_ctx_t *)lws_context_user(ctx);
    if (!sctx) return 0;

    switch (reason) {

    case LWS_CALLBACK_CLIENT_CONNECTION_ERROR:
        snprintf(sctx->fail_reason, sizeof(sctx->fail_reason),
                 "Connexion dédiée échouée : %s", in ? (const char *)in : "(inconnue)");
        sctx->done = true;
        break;

    case LWS_CALLBACK_CLIENT_ESTABLISHED:
        sctx->wsi = wsi;
        LOG_INFO("Bureau à distance : connexion dédiée établie (session_id=%s, mode=%s)",
                 sctx->session_id, sctx->control_mode ? "control" : "view");

        sctx->cap = leo_rd_capture_open(sctx->max_width, sctx->max_height);
        if (!sctx->cap) {
            snprintf(sctx->fail_reason, sizeof(sctx->fail_reason), "Capture d'écran indisponible");
            sctx->done = true;
            break;
        }

        if (sctx->control_mode) {
            sctx->input = leo_rd_input_open();
            if (!sctx->input) {
                snprintf(sctx->fail_reason, sizeof(sctx->fail_reason), "Injection d'input indisponible");
                sctx->done = true;
                break;
            }
        }

        sctx->established = true;
        _arm_frame_timer(sctx);
        break;

    case LWS_CALLBACK_CLIENT_RECEIVE:
        if (in && len > 0 && sctx->established) {
            const uint8_t *data = (const uint8_t *)in;
            if (data[0] == _RD_WIRE_CONTROL) {
                _handle_control_message(sctx, data, len);
            } else {
                _handle_input_message(sctx, data, len);
            }
        }
        break;

    /* Réveil périodique programmé par _arm_frame_timer() — voir son
     * commentaire pour pourquoi ce mécanisme (et pas un simple polling
     * externe) est nécessaire pour cadencer les frames avec cette
     * bibliothèque. On ne capture/encode/envoie pas directement ici : on
     * demande WRITEABLE, pour rester dans le flux normal d'écriture lws
     * (throttling/backpressure gérés par la bibliothèque). */
    case LWS_CALLBACK_TIMER:
        if (sctx->established && !sctx->done) {
            lws_callback_on_writable(wsi);
            _arm_frame_timer(sctx);
        }
        break;

    case LWS_CALLBACK_CLIENT_WRITEABLE:
        if (sctx->established && !sctx->done) {
            _send_frame(sctx);
        }
        break;

    case LWS_CALLBACK_CLIENT_CLOSED:
        sctx->done = true;
        break;

    case LWS_CALLBACK_OPENSSL_PERFORM_SERVER_CERT_VERIFICATION: {
        /* Pinning strict — identique à connection.c, voir son commentaire
         * détaillé pour le raisonnement sur la profondeur de certificat. */
        X509_STORE_CTX *store_ctx = (X509_STORE_CTX *)user;
        if (!store_ctx) return 1;
        if (X509_STORE_CTX_get_error_depth(store_ctx) != 0) return 0;

        X509       *peer_cert   = X509_STORE_CTX_get_current_cert(store_ctx);
        const char *expected_fp = sctx->config ? sctx->config->ca_fingerprint : NULL;
        if (peer_cert && expected_fp && leo_crypto_x509_fingerprint_matches(peer_cert, expected_fp)) {
            X509_STORE_CTX_set_error(store_ctx, X509_V_OK);
            return 0;
        }
        LOG_ERROR("Bureau à distance : certificat serveur rejeté (pinning échoué)");
        return 1;
    }

    default:
        break;
    }

    return 0;
}

static struct lws_protocols _rd_protocols[] = {
    { "leo-remote-desktop-v1", _rd_lws_callback, 0, LEO_MAX_MSG_SIZE, 0, NULL, 0 },
    { NULL, NULL, 0, 0, 0, NULL, 0 }
};

/* ─── Thread de session ────────────────────────────────────────────────────
 *
 * Une seule itération de connexion — contrairement à connection.c, une
 * session de bureau à distance ne se reconnecte jamais automatiquement : une
 * perte de connexion met fin à la session (l'opérateur en rouvre une
 * nouvelle depuis le frontend s'il le souhaite), pas de backoff/retry ici. */
static void *_rd_session_thread(void *arg) {
    _rd_session_ctx_t *sctx = (_rd_session_ctx_t *)arg;

    sctx->tj = tjInitCompress();
    if (!sctx->tj) {
        LOG_ERROR("Bureau à distance : tjInitCompress a échoué");
        _report_status(sctx, "failed", "Initialisation de l'encodeur JPEG échouée");
        goto done;
    }

    struct lws_context_creation_info ctx_info = {0};
    ctx_info.port      = CONTEXT_PORT_NO_LISTEN;
    ctx_info.protocols = _rd_protocols;
    ctx_info.user      = sctx;
    ctx_info.options   = LWS_SERVER_OPTION_DO_SSL_GLOBAL_INIT;
    ctx_info.client_ssl_cert_filepath        = LEO_CLIENT_CERT_FILE;
    ctx_info.client_ssl_private_key_filepath = LEO_CLIENT_KEY_FILE;

    struct lws_context *lws_ctx = lws_create_context(&ctx_info);
    if (!lws_ctx) {
        LOG_ERROR("Bureau à distance : lws_create_context a échoué");
        _report_status(sctx, "failed", "Contexte réseau indisponible");
        goto done;
    }

    struct lws_client_connect_info ci = {0};
    ci.context        = lws_ctx;
    ci.address        = sctx->host;
    ci.port           = sctx->port;
    ci.path           = sctx->path;
    ci.host           = sctx->host;
    ci.origin         = sctx->host;
    ci.protocol       = _rd_protocols[0].name;
    ci.ssl_connection = LCCSCF_USE_SSL;

    if (!lws_client_connect_via_info(&ci)) {
        LOG_ERROR("Bureau à distance : connexion dédiée impossible (%s:%d)", sctx->host, sctx->port);
        _report_status(sctx, "failed", "Connexion au serveur impossible");
        lws_context_destroy(lws_ctx);
        goto done;
    }

    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += LEO_RD_MAX_SESSION_SEC;

    /* Le cadencement des frames est piloté par LWS_CALLBACK_TIMER (voir
     * _arm_frame_timer/_send_frame ci-dessus), pas par cette boucle — le
     * timeout passé à lws_service() ci-dessous n'a qu'une valeur informative
     * (voir le commentaire détaillé au-dessus de _arm_frame_timer sur
     * pourquoi cette bibliothèque ignore ce paramètre) ; should_stop/le
     * délai max de session sont revérifiés à chaque retour de
     * lws_service(), qu'il ait été réveillé par le timer, un événement
     * réseau, ou le maximum interne par défaut. */
    while (!sctx->done && !sctx->should_stop) {
        if (lws_service(lws_ctx, 50) < 0) break;

        struct timespec now;
        clock_gettime(CLOCK_REALTIME, &now);
        if (now.tv_sec > deadline.tv_sec) {
            snprintf(sctx->fail_reason, sizeof(sctx->fail_reason),
                     "Durée maximale de session atteinte (%ds)", LEO_RD_MAX_SESSION_SEC);
            sctx->done = true;
        }
    }

    lws_context_destroy(lws_ctx);

    if (sctx->fail_reason[0] != '\0') {
        LOG_WARN("Bureau à distance terminé (session_id=%s) : %s", sctx->session_id, sctx->fail_reason);
        _report_status(sctx, "failed", sctx->fail_reason);
    } else {
        LOG_INFO("Bureau à distance terminé (session_id=%s)", sctx->session_id);
        _report_status(sctx, "ended", NULL);
    }

done:
    if (sctx->input) leo_rd_input_close(sctx->input);
    if (sctx->cap)   leo_rd_capture_close(sctx->cap);
    if (sctx->tj)    tjDestroy(sctx->tj);
    if (sctx->jpeg_buf) tjFree(sctx->jpeg_buf);

    pthread_mutex_lock(&g_rd_mutex);
    if (g_rd_ctx == sctx) g_rd_ctx = NULL;
    pthread_mutex_unlock(&g_rd_mutex);
    free(sctx);

    pthread_mutex_lock(&g_rd_exit_mutex);
    g_rd_thread_running = false;
    pthread_cond_signal(&g_rd_exit_cond);
    pthread_mutex_unlock(&g_rd_exit_mutex);

    return NULL;
}

/* ─── API publique ────────────────────────────────────────────────────── */

bool leo_rd_start(leo_conn_t *conn, const leo_config_t *config,
                   const char *session_id, const char *ws_url, const char *mode,
                   int fps, int quality, int max_width, int max_height) {
    if (!conn || !config || !session_id || !ws_url) return false;

    pthread_mutex_lock(&g_rd_mutex);
    if (g_rd_ctx != NULL) {
        pthread_mutex_unlock(&g_rd_mutex);
        LOG_WARN("Bureau à distance : session déjà active — nouvelle demande ignorée (session_id=%s)", session_id);
        return false;
    }

    _rd_session_ctx_t *sctx = calloc(1, sizeof(*sctx));
    if (!sctx) {
        pthread_mutex_unlock(&g_rd_mutex);
        return false;
    }

    strncpy(sctx->session_id, session_id, sizeof(sctx->session_id) - 1);
    strncpy(sctx->ws_url, ws_url, sizeof(sctx->ws_url) - 1);
    sctx->control_mode = mode && strcmp(mode, "control") == 0;
    sctx->fps          = fps > 0 ? fps : 8;
    sctx->quality      = quality > 0 ? quality : 60;
    sctx->max_width    = max_width  > 0 ? max_width  : 1920;
    sctx->max_height   = max_height > 0 ? max_height : 1080;
    sctx->config       = config;
    sctx->control_conn = conn;

    if (!_parse_ws_url(ws_url, sctx->host, sizeof(sctx->host), &sctx->port, sctx->path, sizeof(sctx->path))) {
        pthread_mutex_unlock(&g_rd_mutex);
        LOG_ERROR("Bureau à distance : ws_url invalide (session_id=%s)", session_id);
        free(sctx);
        return false;
    }

    g_rd_ctx = sctx;
    pthread_mutex_unlock(&g_rd_mutex);

    pthread_mutex_lock(&g_rd_exit_mutex);
    g_rd_thread_running = true;
    pthread_mutex_unlock(&g_rd_exit_mutex);

    pthread_t th;
    if (pthread_create(&th, NULL, _rd_session_thread, sctx) != 0) {
        LOG_ERROR("Bureau à distance : pthread_create a échoué (session_id=%s)", session_id);
        pthread_mutex_lock(&g_rd_mutex);
        g_rd_ctx = NULL;
        pthread_mutex_unlock(&g_rd_mutex);
        pthread_mutex_lock(&g_rd_exit_mutex);
        g_rd_thread_running = false;
        pthread_mutex_unlock(&g_rd_exit_mutex);
        free(sctx);
        return false;
    }
    pthread_detach(th);

    LOG_INFO("Bureau à distance : session démarrée (session_id=%s, mode=%s, fps=%d, qualité=%d)",
             session_id, mode ? mode : "?", sctx->fps, sctx->quality);
    return true;
}

void leo_rd_stop(const char *session_id) {
    pthread_mutex_lock(&g_rd_mutex);
    if (!g_rd_ctx) {
        pthread_mutex_unlock(&g_rd_mutex);
        LOG_WARN("Bureau à distance : demande d'arrêt sans session active (session_id=%s)", session_id ? session_id : "?");
        return;
    }
    if (session_id && strcmp(g_rd_ctx->session_id, session_id) != 0) {
        pthread_mutex_unlock(&g_rd_mutex);
        LOG_WARN("Bureau à distance : demande d'arrêt pour une session inconnue (reçu=%s, active=%s)",
                 session_id, g_rd_ctx->session_id);
        return;
    }
    g_rd_ctx->should_stop = true;
    pthread_mutex_unlock(&g_rd_mutex);
}

bool leo_rd_stop_all(void) {
    pthread_mutex_lock(&g_rd_mutex);
    if (g_rd_ctx) g_rd_ctx->should_stop = true;
    pthread_mutex_unlock(&g_rd_mutex);

    /* Attente bornée du thread de session — même raisonnement que
     * leo_conn_destroy() dans connection.c : l'appelant (leo_agent_stop) ne
     * doit jamais libérer ag (dont config/conn sont référencés par ce
     * thread) tant qu'on n'a pas la certitude qu'il s'est terminé.
     * lws_service() étant appelé avec un timeout de 10ms (voir
     * _rd_session_thread), should_stop est observé quasi immédiatement —
     * pas besoin de lws_cancel_service() ici. */
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += 5;

    pthread_mutex_lock(&g_rd_exit_mutex);
    bool exited = true;
    while (g_rd_thread_running) {
        if (pthread_cond_timedwait(&g_rd_exit_cond, &g_rd_exit_mutex, &deadline) == ETIMEDOUT) {
            exited = false;
            break;
        }
    }
    pthread_mutex_unlock(&g_rd_exit_mutex);

    if (!exited) {
        LOG_ERROR("Bureau à distance : thread de session non joignable après 5s — abandon "
                  "(fuite volontaire, évite un use-after-free tant qu'il tourne encore)");
    }
    return exited;
}
