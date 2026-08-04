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
 * Lance un thread détaché pour exécuter un script EXEC_SCRIPT.
 * Envoie un CMD_RESULT d'erreur immédiat si la requête est invalide ou si
 * le nombre max de commandes concurrentes est atteint (jamais de blocage
 * du thread WSS ici).
 */
static void _dispatch_exec_script(leo_agent_t *ag, const char *cmd_id, cJSON *body) {
    char buf[LEO_MAX_MSG_SIZE];
    int  wlen;

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
    if (timeout_secs > LEO_EXEC_MAX_TIMEOUT_SEC)
        timeout_secs = LEO_EXEC_MAX_TIMEOUT_SEC;

    if (!interpreter || !script) {
        LOG_WARN("EXEC_SCRIPT sans 'interpreter'/'script' — ignoré (cmd_id=%s)", cmd_id);
        wlen = leo_proto_build_cmd_result(cmd_id, -1, "",
                   "Requête invalide : 'interpreter' et 'script' requis", buf, sizeof(buf));
        if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
        return;
    }

    pthread_mutex_lock(&ag->exec_mutex);
    if (ag->exec_active_count >= LEO_EXEC_MAX_CONCURRENT) {
        pthread_mutex_unlock(&ag->exec_mutex);
        LOG_WARN("Trop de scripts en cours (max=%d) — commande rejetée (cmd_id=%s)",
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
        LOG_ERROR("Allocation échouée pour la commande EXEC_SCRIPT (cmd_id=%s)", cmd_id);
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
        LOG_ERROR("pthread_create a échoué pour EXEC_SCRIPT (cmd_id=%s) : %d", cmd_id, prc);
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

    LOG_INFO("EXEC_SCRIPT lancé (cmd_id=%s, interpreter=%s, timeout=%ds)",
             cmd_id, interpreter, timeout_secs);
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
        break;

    case LEO_MSG_REBOOT:
        LOG_WARN("Commande REBOOT reçue — redémarrage planifié");
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
