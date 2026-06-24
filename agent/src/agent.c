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

#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <unistd.h>
#include <time.h>

/* ─── Structure interne ─────────────────────────────────────────────────── */

struct leo_agent {
    leo_config_t          config;
    leo_conn_t           *conn;
    volatile leo_agent_state_t state;

    /* Threads */
    pthread_t             heartbeat_thread;
    pthread_t             metrics_thread;
    volatile bool         threads_stop;
};

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
        /* TODO Phase suivante : executor.c */
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

    /* ── Chargement de la configuration ── */
    if (leo_config_load(config_path, &ag->config) != LEO_OK) {
        LOG_FATAL("Impossible de charger la configuration depuis %s", config_path);
        free(ag);
        return NULL;
    }

    /* ── Initialisation du sous-système métriques ── */
    if (leo_metrics_init() != LEO_OK) {
        LOG_FATAL("Impossible d'initialiser le sous-système métriques");
        free(ag);
        return NULL;
    }

    /* ── Connexion WSS ── */
    ag->state = LEO_STATE_CONNECTING;
    ag->conn  = leo_conn_create(&ag->config, _on_message, ag);
    if (!ag->conn) {
        LOG_FATAL("Impossible de créer la connexion WSS");
        leo_metrics_destroy();
        free(ag);
        return NULL;
    }

    /* ── Lancement des threads ── */
    if (pthread_create(&ag->heartbeat_thread, NULL, _heartbeat_thread, ag) != 0) {
        LOG_FATAL("Impossible de créer le thread heartbeat");
        leo_conn_destroy(ag->conn);
        leo_metrics_destroy();
        free(ag);
        return NULL;
    }

    if (pthread_create(&ag->metrics_thread, NULL, _metrics_thread, ag) != 0) {
        LOG_FATAL("Impossible de créer le thread métriques");
        ag->threads_stop = true;
        pthread_join(ag->heartbeat_thread, NULL);
        leo_conn_destroy(ag->conn);
        leo_metrics_destroy();
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

    leo_conn_destroy(ag->conn);
    leo_metrics_destroy();

    LOG_INFO("Agent arrêté proprement");
    free(ag);
}

leo_agent_state_t leo_agent_get_state(const leo_agent_t *ag) {
    return ag ? ag->state : LEO_STATE_INIT;
}
