/**
 * agent.c — Orchestrateur principal de l'agent Leo-One
 *
 * Threads lancés :
 *   _heartbeat_thread  : envoie LEO_MSG_HEARTBEAT toutes les N secondes
 *   _metrics_thread    : collecte et envoie LEO_MSG_METRICS toutes les N secondes
 *
 * Les commandes entrantes sont dispatchées dans _on_message()
 * appelé depuis le thread WSS (connection.c).
 */
#include "agent.h"
#include "config.h"
#include "connection.h"
#include "metrics.h"
#include "protocol.h"
#include "logger.h"
#include "executor.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <pthread.h>
#include <unistd.h>
#include <time.h>

/* ─── Exécution de scripts : bornes de sécurité ──────────────────────────── */

/* Nombre max de scripts exécutés en parallèle — évite qu'un backend compromis
 * ou buggé ne fasse exploser le nombre de threads/processus de l'agent. */
#define LEO_EXEC_MAX_CONCURRENT       4
#define LEO_EXEC_DEFAULT_TIMEOUT_SEC  300
/* Borne haute : aussi la durée max que leo_agent_stop() attendra les threads
 * d'exécution en cours avant de continuer l'arrêt (voir plus bas). */
#define LEO_EXEC_MAX_TIMEOUT_SEC      300

/* ─── Structure interne ─────────────────────────────────────────────────── */

struct leo_agent {
    leo_config_t          config;
    leo_conn_t           *conn;
    volatile leo_agent_state_t state;

    /* Threads */
    pthread_t             heartbeat_thread;
    pthread_t             metrics_thread;
    volatile bool         threads_stop;

    /* Exécution de scripts (threads détachés, un par commande EXEC_SCRIPT) */
    pthread_mutex_t        exec_mutex;
    pthread_cond_t         exec_cond;
    int                    exec_active_count;
};

/* Contexte transmis à un thread d'exécution — alloué par _on_message(),
 * libéré par _exec_thread() une fois la commande terminée et le résultat
 * envoyé. */
typedef struct {
    leo_agent_t *ag;
    char         cmd_id[LEO_UUID_STR_LEN];
    char         interpreter[32];
    char        *script;   /* alloué via strdup(), possédé par ce contexte */
    int          timeout_secs;
} _exec_ctx_t;

/* ─── Helpers ───────────────────────────────────────────────────────────── */

/** Attend N secondes en vérifiant threads_stop toutes les 100ms. */
static void _interruptible_sleep(const struct leo_agent *ag, int secs) {
    int elapsed_ms = 0;
    while (!ag->threads_stop && elapsed_ms < secs * 1000) {
        struct timespec ts = { .tv_sec = 0, .tv_nsec = 100 * 1000000L };
        nanosleep(&ts, NULL);
        elapsed_ms += 100;
    }
}

/* ─── Thread : Heartbeat ─────────────────────────────────────────────────── */

static void *_heartbeat_thread(void *arg) {
    leo_agent_t *ag  = (leo_agent_t *)arg;
    uint64_t     seq = 0;
    char         buf[512];

    LOG_INFO("Thread heartbeat démarré (intervalle=%ds)",
             ag->config.heartbeat_interval_sec);

    while (!ag->threads_stop) {
        _interruptible_sleep(ag, ag->config.heartbeat_interval_sec);
        if (ag->threads_stop) break;

        if (!leo_conn_is_connected(ag->conn)) {
            LOG_DEBUG("Heartbeat ignoré — pas connecté");
            continue;
        }

        int len = leo_proto_build_heartbeat(seq++, buf, sizeof(buf));
        if (len > 0) {
            leo_error_t rc = leo_conn_send(ag->conn, buf, (size_t)len);
            if (rc == LEO_OK) {
                LOG_DEBUG("Heartbeat envoyé (seq=%llu)", (unsigned long long)seq - 1);
            } else {
                LOG_WARN("Échec envoi heartbeat (rc=%d)", rc);
            }
        }
    }

    LOG_INFO("Thread heartbeat arrêté");
    return NULL;
}

/* ─── Thread : Métriques ─────────────────────────────────────────────────── */

static void *_metrics_thread(void *arg) {
    leo_agent_t  *ag = (leo_agent_t *)arg;
    leo_metrics_t metrics;
    char          buf[LEO_MAX_MSG_SIZE];

    LOG_INFO("Thread métriques démarré (intervalle=%ds)",
             ag->config.metrics_interval_sec);

    while (!ag->threads_stop) {
        _interruptible_sleep(ag, ag->config.metrics_interval_sec);
        if (ag->threads_stop) break;

        if (!leo_conn_is_connected(ag->conn)) {
            LOG_DEBUG("Métriques ignorées — pas connecté");
            continue;
        }

        leo_error_t rc = leo_metrics_collect(&metrics);
        if (rc != LEO_OK) {
            LOG_WARN("Échec de collecte des métriques (rc=%d)", rc);
            continue;
        }

        int len = leo_proto_build_metrics(&metrics, buf, sizeof(buf));
        if (len > 0) {
            rc = leo_conn_send(ag->conn, buf, (size_t)len);
            if (rc != LEO_OK) {
                LOG_WARN("Échec envoi métriques (rc=%d)", rc);
            } else {
                LOG_DEBUG("Métriques envoyées (CPU=%.1f%%)", metrics.cpu_total_percent);
            }
        }
    }

    LOG_INFO("Thread métriques arrêté");
    return NULL;
}

/* ─── Thread : exécution d'un script (un thread détaché par commande) ───── */

/**
 * Exécute le script du contexte puis envoie le CMD_RESULT correspondant.
 * Ne bloque jamais le thread WSS : c'est tout l'intérêt d'un thread dédié,
 * puisque leo_exec_script() peut bloquer jusqu'à timeout_secs.
 */
static void *_exec_thread(void *arg) {
    _exec_ctx_t *ctx = (_exec_ctx_t *)arg;
    leo_agent_t *ag  = ctx->ag;

    leo_exec_result_t result;
    leo_error_t rc = leo_exec_script(ctx->interpreter, ctx->script,
                                     ctx->timeout_secs, &result);

    int         exit_code = -1;
    const char *stdout_s  = "";
    const char *stderr_s  = "";
    char        err_buf[96];

    switch (rc) {
    case LEO_OK:
        exit_code = result.exit_code;
        stdout_s  = result.stdout_buf;
        stderr_s  = result.stderr_buf;
        break;
    case LEO_ERR_TIMEOUT:
        stderr_s = "Timeout d'exécution dépassé — script interrompu (SIGKILL)";
        break;
    case LEO_ERR_PROTOCOL:
        snprintf(err_buf, sizeof(err_buf), "Interpréteur non autorisé : %s", ctx->interpreter);
        stderr_s = err_buf;
        break;
    default:
        stderr_s = "Erreur système lors de l'exécution du script";
        break;
    }

    char buf[LEO_MAX_MSG_SIZE];
    int  wlen = leo_proto_build_cmd_result(ctx->cmd_id, exit_code, stdout_s, stderr_s,
                                           buf, sizeof(buf));
    if (wlen > 0) {
        leo_error_t send_rc = leo_conn_send(ag->conn, buf, (size_t)wlen);
        if (send_rc != LEO_OK) {
            LOG_WARN("Échec envoi CMD_RESULT (cmd_id=%s, rc=%d)", ctx->cmd_id, send_rc);
        }
    }

    LOG_INFO("EXEC_SCRIPT terminé (cmd_id=%s, exit_code=%d)", ctx->cmd_id, exit_code);

    free(ctx->script);
    free(ctx);

    pthread_mutex_lock(&ag->exec_mutex);
    ag->exec_active_count--;
    pthread_cond_signal(&ag->exec_cond);
    pthread_mutex_unlock(&ag->exec_mutex);

    return NULL;
}

/**
 * Lance un thread détaché qui exécute {interpreter, script} via leo_exec_script()
 * puis renvoie le CMD_RESULT correspondant. Commun à EXEC_SCRIPT, INSTALL_PKG et
 * REBOOT — ces deux dernières ne sont que des scripts construits côté agent à
 * partir de paramètres validés, plutôt que fournis tels quels par le backend.
 *
 * Envoie un CMD_RESULT d'erreur immédiat si le nombre max de commandes
 * concurrentes est atteint ou en cas d'échec d'allocation/thread (jamais de
 * blocage du thread WSS ici).
 */
static void _launch_exec(leo_agent_t *ag, const char *cmd_id,
                          const char *interpreter, const char *script,
                          int timeout_secs) {
    char buf[LEO_MAX_MSG_SIZE];
    int  wlen;

    if (timeout_secs <= 0)
        timeout_secs = LEO_EXEC_DEFAULT_TIMEOUT_SEC;
    if (timeout_secs > LEO_EXEC_MAX_TIMEOUT_SEC)
        timeout_secs = LEO_EXEC_MAX_TIMEOUT_SEC;

    pthread_mutex_lock(&ag->exec_mutex);
    if (ag->exec_active_count >= LEO_EXEC_MAX_CONCURRENT) {
        pthread_mutex_unlock(&ag->exec_mutex);
        LOG_WARN("Trop de commandes en cours (max=%d) — commande rejetée (cmd_id=%s)",
                 LEO_EXEC_MAX_CONCURRENT, cmd_id);
        wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                   "Trop de commandes en cours d'exécution sur l'agent", buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        return;
    }
    ag->exec_active_count++;
    pthread_mutex_unlock(&ag->exec_mutex);

    _exec_ctx_t *ctx = calloc(1, sizeof(*ctx));
    if (ctx) ctx->script = strdup(script);

    if (!ctx || !ctx->script) {
        LOG_ERROR("Allocation échouée pour la commande (cmd_id=%s)", cmd_id);
        free(ctx);
        pthread_mutex_lock(&ag->exec_mutex);
        ag->exec_active_count--;
        pthread_mutex_unlock(&ag->exec_mutex);
        wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                   "Erreur interne de l'agent (allocation)", buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        return;
    }

    ctx->ag = ag;
    strncpy(ctx->cmd_id, cmd_id, sizeof(ctx->cmd_id) - 1);
    strncpy(ctx->interpreter, interpreter, sizeof(ctx->interpreter) - 1);
    ctx->timeout_secs = timeout_secs;

    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED);

    pthread_t th;
    int prc = pthread_create(&th, &attr, _exec_thread, ctx);
    pthread_attr_destroy(&attr);

    if (prc != 0) {
        LOG_ERROR("pthread_create a échoué pour la commande (cmd_id=%s) : %d", cmd_id, prc);
        pthread_mutex_lock(&ag->exec_mutex);
        ag->exec_active_count--;
        pthread_mutex_unlock(&ag->exec_mutex);
        free(ctx->script);
        free(ctx);
        wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                   "Erreur interne de l'agent (thread)", buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        return;
    }

    LOG_INFO("Commande lancée (cmd_id=%s, interpreter=%s, timeout=%ds)",
             cmd_id, interpreter, timeout_secs);
}

/**
 * Dispatch d'une commande EXEC_SCRIPT : script arbitraire fourni par le
 * backend, exécuté tel quel via _launch_exec().
 */
static void _dispatch_exec_script(leo_agent_t *ag, const char *cmd_id, cJSON *body) {
    const char *interpreter = NULL;
    const char *script      = NULL;
    int         timeout_secs = LEO_EXEC_DEFAULT_TIMEOUT_SEC;

    if (body) {
        cJSON *ji = cJSON_GetObjectItemCaseSensitive(body, "interpreter");
        cJSON *js = cJSON_GetObjectItemCaseSensitive(body, "script");
        cJSON *jt = cJSON_GetObjectItemCaseSensitive(body, "timeout_sec");
        if (cJSON_IsString(ji)) interpreter = ji->valuestring;
        if (cJSON_IsString(js)) script      = js->valuestring;
        if (cJSON_IsNumber(jt) && jt->valuedouble > 0)
            timeout_secs = (int)jt->valuedouble;
    }

    if (!interpreter || !script) {
        LOG_WARN("EXEC_SCRIPT sans 'interpreter'/'script' — ignoré (cmd_id=%s)", cmd_id);
        char buf[LEO_MAX_MSG_SIZE];
        int wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                       "Requête invalide : 'interpreter' et 'script' requis", buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        return;
    }

    _launch_exec(ag, cmd_id, interpreter, script, timeout_secs);
}

/**
 * Longueur maximale d'un nom de paquet accepté (charset restreint plus bas) —
 * large pour les noms qualifiés par architecture (ex: "libfoo:amd64").
 */
#define LEO_PKG_NAME_MAX_LEN  128
/** Nombre max de paquets par commande INSTALL_PKG. */
#define LEO_PKG_MAX_COUNT     32

/**
 * Valide un nom de paquet contre un charset restreint (alphanumérique + . + -
 * _ :) avant de l'insérer dans un script shell — ce nom vient du backend et
 * potentiellement, en amont, d'un utilisateur de la console web. Sans cette
 * validation, un nom comme "foo; rm -rf /" serait exécuté tel quel puisque le
 * script est passé à sh sans échappement supplémentaire.
 */
static bool _pkg_name_valid(const char *name) {
    size_t len = strlen(name);
    if (len == 0 || len > LEO_PKG_NAME_MAX_LEN)
        return false;
    for (size_t i = 0; i < len; i++) {
        unsigned char c = (unsigned char)name[i];
        if (!isalnum(c) && c != '.' && c != '+' && c != '-' && c != '_' && c != ':')
            return false;
    }
    return true;
}

/**
 * Dispatch d'une commande INSTALL_PKG : { "packages": ["a","b"] } ou
 * { "package": "a" }, "timeout_sec" optionnel. Construit un script
 * "apt-get install" à partir de noms de paquets validés (voir _pkg_name_valid)
 * puis délègue à _launch_exec() — même mécanisme que EXEC_SCRIPT.
 */
static void _dispatch_install_pkg(leo_agent_t *ag, const char *cmd_id, cJSON *body) {
    char buf[LEO_MAX_MSG_SIZE];
    int  wlen;

    const char *names[LEO_PKG_MAX_COUNT];
    int         n = 0;
    int         timeout_secs = LEO_EXEC_DEFAULT_TIMEOUT_SEC;

    if (body) {
        cJSON *jpkgs = cJSON_GetObjectItemCaseSensitive(body, "packages");
        cJSON *jpkg  = cJSON_GetObjectItemCaseSensitive(body, "package");
        cJSON *jt    = cJSON_GetObjectItemCaseSensitive(body, "timeout_sec");

        if (cJSON_IsArray(jpkgs)) {
            cJSON *item;
            cJSON_ArrayForEach(item, jpkgs) {
                if (n >= LEO_PKG_MAX_COUNT) break;
                if (cJSON_IsString(item)) names[n++] = item->valuestring;
            }
        } else if (cJSON_IsString(jpkg)) {
            names[n++] = jpkg->valuestring;
        }
        if (cJSON_IsNumber(jt) && jt->valuedouble > 0)
            timeout_secs = (int)jt->valuedouble;
    }

    if (n == 0) {
        LOG_WARN("INSTALL_PKG sans 'package'/'packages' — ignoré (cmd_id=%s)", cmd_id);
        wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                   "Requête invalide : 'package' ou 'packages' requis", buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        return;
    }

    /* Construit "apt-get ... install -- <pkg1> <pkg2> ..." dans un buffer
     * fixe. "--" arrête l'analyse d'options : un nom de paquet ne peut donc
     * pas être interprété comme un flag d'apt-get même s'il commence par
     * '-' (le charset autorisé par _pkg_name_valid le permettrait). */
    char script[512] =
        "export DEBIAN_FRONTEND=noninteractive\n"
        "apt-get update -qq && apt-get install -y --no-install-recommends -- ";
    size_t off = strlen(script);

    for (int i = 0; i < n; i++) {
        if (!_pkg_name_valid(names[i])) {
            LOG_WARN("INSTALL_PKG : nom de paquet invalide, commande rejetée (cmd_id=%s)", cmd_id);
            wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                       "Requête invalide : nom de paquet non autorisé", buf, sizeof(buf));
            if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
            return;
        }
        size_t plen = strlen(names[i]);
        /* +2 pour l'espace séparateur et le terminateur nul. */
        if (off + plen + 2 >= sizeof(script)) {
            LOG_WARN("INSTALL_PKG : trop de paquets pour le buffer de script (cmd_id=%s)", cmd_id);
            wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                       "Requête invalide : trop de paquets", buf, sizeof(buf));
            if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
            return;
        }
        if (i > 0) script[off++] = ' ';
        memcpy(script + off, names[i], plen);
        off += plen;
    }
    script[off] = '\0';
    strncat(script, " 2>&1", sizeof(script) - strlen(script) - 1);

    _launch_exec(ag, cmd_id, "sh", script, timeout_secs);
}

/** Délai par défaut avant redémarrage, en secondes — laisse le temps à
 * l'utilisateur de la machine de sauvegarder son travail. */
#define LEO_REBOOT_DEFAULT_DELAY_SEC  60
/** Borne haute du délai accepté. */
#define LEO_REBOOT_MAX_DELAY_SEC      3600
/* La commande "shutdown" planifie le redémarrage et retourne immédiatement :
 * ce timeout ne couvre que la planification, pas l'attente du délai lui-même. */
#define LEO_REBOOT_SCHEDULE_TIMEOUT_SEC  15

/**
 * Dispatch d'une commande REBOOT : { "delay_sec": <int> } optionnel (défaut
 * LEO_REBOOT_DEFAULT_DELAY_SEC). Construit un appel à `shutdown -r`, qui
 * planifie le redémarrage et rend la main tout de suite — le CMD_RESULT
 * confirme donc la planification, pas l'exécution effective du redémarrage
 * (qui coupera la connexion de l'agent de toute façon).
 */
static void _dispatch_reboot(leo_agent_t *ag, const char *cmd_id, cJSON *body) {
    int delay_sec = LEO_REBOOT_DEFAULT_DELAY_SEC;

    if (body) {
        cJSON *jd = cJSON_GetObjectItemCaseSensitive(body, "delay_sec");
        if (cJSON_IsNumber(jd) && jd->valuedouble >= 0)
            delay_sec = (int)jd->valuedouble;
    }
    if (delay_sec > LEO_REBOOT_MAX_DELAY_SEC)
        delay_sec = LEO_REBOOT_MAX_DELAY_SEC;

    char script[160];
    if (delay_sec <= 0) {
        snprintf(script, sizeof(script),
                 "shutdown -r now 2>&1 || systemctl reboot 2>&1");
    } else {
        /* shutdown(8) ne prend un délai qu'en minutes entières, arrondi au
         * supérieur pour ne jamais planifier plus tôt que demandé. */
        int minutes = (delay_sec + 59) / 60;
        snprintf(script, sizeof(script),
                 "shutdown -r +%d 2>&1 || systemctl reboot 2>&1", minutes);
    }

    LOG_WARN("REBOOT planifié dans %ds (cmd_id=%s)", delay_sec, cmd_id);
    _launch_exec(ag, cmd_id, "sh", script, LEO_REBOOT_SCHEDULE_TIMEOUT_SEC);
}

/* ─── Dispatch des messages entrants ─────────────────────────────────────── */

static void _on_message(const char *json_str, size_t len, void *userdata) {
    leo_agent_t *ag = (leo_agent_t *)userdata;
    (void)len;

    leo_incoming_msg_t msg;
    if (leo_proto_parse(json_str, &msg) != LEO_OK) {
        LOG_WARN("Message entrant non parsable, ignoré");
        return;
    }

    char buf[LEO_MAX_MSG_SIZE];
    int  wlen;

    switch (msg.type) {

    case LEO_MSG_HELLO_ACK:
        LOG_INFO("HELLO_ACK reçu — session validée par le backend");
        ag->state = LEO_STATE_CONNECTED;

        /* Appliquer les paramètres envoyés par le serveur (ex: intervalles) */
        if (msg.body) {
            cJSON *jhi = cJSON_GetObjectItemCaseSensitive(msg.body, "heartbeat_interval_sec");
            cJSON *jmi = cJSON_GetObjectItemCaseSensitive(msg.body, "metrics_interval_sec");
            if (cJSON_IsNumber(jhi) && jhi->valuedouble > 0)
                ag->config.heartbeat_interval_sec = (int)jhi->valuedouble;
            if (cJSON_IsNumber(jmi) && jmi->valuedouble > 0)
                ag->config.metrics_interval_sec = (int)jmi->valuedouble;
        }
        break;

    case LEO_MSG_PING:
        LOG_DEBUG("PING reçu, envoi PONG");
        wlen = leo_proto_build_pong(msg.id, buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        break;

    case LEO_MSG_EXEC_SCRIPT:
        LOG_INFO("Commande EXEC_SCRIPT reçue (cmd_id=%s)", msg.id);
        _dispatch_exec_script(ag, msg.id, msg.body);
        break;

    case LEO_MSG_INSTALL_PKG:
        LOG_INFO("Commande INSTALL_PKG reçue (cmd_id=%s)", msg.id);
        _dispatch_install_pkg(ag, msg.id, msg.body);
        break;

    case LEO_MSG_REBOOT:
        LOG_WARN("Commande REBOOT reçue (cmd_id=%s)", msg.id);
        _dispatch_reboot(ag, msg.id, msg.body);
        break;

    case LEO_MSG_COLLECT_INVENTORY:
        LOG_INFO("Demande d'inventaire reçue");
        break;

    case LEO_MSG_CONFIG_UPDATE:
        LOG_INFO("Mise à jour de configuration reçue");
        if (msg.body) {
            cJSON *jmi = cJSON_GetObjectItemCaseSensitive(msg.body, "metrics_interval_sec");
            if (cJSON_IsNumber(jmi) && jmi->valuedouble > 0) {
                ag->config.metrics_interval_sec = (int)jmi->valuedouble;
                LOG_INFO("Intervalle métriques mis à jour : %ds",
                         ag->config.metrics_interval_sec);
            }
        }
        break;

    default:
        LOG_WARN("Message de type inconnu reçu : %d", (int)msg.type);
        break;
    }

    leo_proto_msg_free(&msg);
}

/* ─── API publique ────────────────────────────────────────────────────── */

leo_agent_t *leo_agent_start(const char *config_path) {
    leo_agent_t *ag = calloc(1, sizeof(*ag));
    if (!ag) return NULL;

    ag->state        = LEO_STATE_INIT;
    ag->threads_stop = false;

    pthread_mutex_init(&ag->exec_mutex, NULL);
    pthread_cond_init(&ag->exec_cond, NULL);
    ag->exec_active_count = 0;

    /* ── Chargement de la configuration ── */
    if (leo_config_load(config_path, &ag->config) != LEO_OK) {
        LOG_FATAL("Impossible de charger la configuration depuis %s", config_path);
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    /* ── Initialisation du sous-système métriques ── */
    if (leo_metrics_init() != LEO_OK) {
        LOG_FATAL("Impossible d'initialiser le sous-système métriques");
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    /* ── Connexion WSS ── */
    ag->state = LEO_STATE_CONNECTING;
    ag->conn  = leo_conn_create(&ag->config, _on_message, ag);
    if (!ag->conn) {
        LOG_FATAL("Impossible de créer la connexion WSS");
        leo_metrics_destroy();
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    /* ── Lancement des threads ── */
    if (pthread_create(&ag->heartbeat_thread, NULL, _heartbeat_thread, ag) != 0) {
        LOG_FATAL("Impossible de créer le thread heartbeat");
        leo_conn_destroy(ag->conn);
        leo_metrics_destroy();
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    if (pthread_create(&ag->metrics_thread, NULL, _metrics_thread, ag) != 0) {
        LOG_FATAL("Impossible de créer le thread métriques");
        ag->threads_stop = true;
        pthread_join(ag->heartbeat_thread, NULL);
        leo_conn_destroy(ag->conn);
        leo_metrics_destroy();
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    LOG_INFO("Agent Leo-One v%s démarré — agent_id=%s",
             LEO_AGENT_VERSION, ag->config.agent_id);
    return ag;
}

void leo_agent_stop(leo_agent_t *ag) {
    if (!ag) return;

    LOG_INFO("Arrêt de l'agent…");
    ag->state        = LEO_STATE_STOPPING;
    ag->threads_stop = true;

    pthread_join(ag->heartbeat_thread, NULL);
    pthread_join(ag->metrics_thread,   NULL);

    /* Attendre les scripts encore en cours d'exécution : leurs threads sont
     * détachés et utilisent ag->conn (et ag lui-même) pour envoyer leur
     * CMD_RESULT — les détruire/libérer pendant qu'un thread les utilise
     * encore serait un use-after-free. Chaque exécution est bornée par
     * LEO_EXEC_MAX_TIMEOUT_SEC (ou moins), donc cette attente l'est aussi
     * en temps normal ; en cas de dépassement (cas pathologique), on
     * préfère abandonner sans libérer plutôt que risquer l'UAF — le
     * processus va de toute façon se terminer juste après (voir main.c),
     * ce qui réclamera la mémoire proprement. */
    pthread_mutex_lock(&ag->exec_mutex);
    if (ag->exec_active_count > 0) {
        LOG_INFO("Attente de %d script(s) en cours avant l'arrêt…", ag->exec_active_count);

        struct timespec deadline;
        clock_gettime(CLOCK_REALTIME, &deadline);
        deadline.tv_sec += LEO_EXEC_MAX_TIMEOUT_SEC + 5;

        while (ag->exec_active_count > 0) {
            if (pthread_cond_timedwait(&ag->exec_cond, &ag->exec_mutex, &deadline) != 0) {
                LOG_ERROR("%d script(s) toujours en cours après le délai d'attente — "
                         "abandon de l'arrêt propre (fuite volontaire, évite un "
                         "use-after-free tant qu'un thread d'exécution est actif)",
                         ag->exec_active_count);
                pthread_mutex_unlock(&ag->exec_mutex);
                return;
            }
        }
    }
    pthread_mutex_unlock(&ag->exec_mutex);

    leo_conn_destroy(ag->conn);
    leo_metrics_destroy();

    pthread_cond_destroy(&ag->exec_cond);
    pthread_mutex_destroy(&ag->exec_mutex);

    LOG_INFO("Agent arrêté proprement");
    free(ag);
}

leo_agent_state_t leo_agent_get_state(const leo_agent_t *ag) {
    return ag ? ag->state : LEO_STATE_INIT;
}
