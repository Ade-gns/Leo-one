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
#include "inventory.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <pthread.h>
#include <unistd.h>
#include <time.h>
#include <errno.h>

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
    volatile bool         force_heartbeat_pending;  /* Set by FORCE_HEARTBEAT msg */
    /* Protège threads_stop/force_heartbeat_pending pendant l'attente dans
     * _interruptible_sleep() et permet de réveiller un thread endormi
     * immédiatement (arrêt ou FORCE_HEARTBEAT) au lieu d'attendre jusqu'à
     * l'intervalle complet — crucial maintenant qu'il peut atteindre 3h. */
    pthread_mutex_t        wake_mutex;
    pthread_cond_t         wake_cond;

    /* Exécution de scripts (threads détachés, un par commande EXEC_SCRIPT) */
    pthread_mutex_t        exec_mutex;
    pthread_cond_t         exec_cond;
    int                    exec_active_count;
};

/* Chaque thread de commande n'a besoin que d'un cadre de pile modeste (le
 * plus gros élément empilé est un buf[LEO_MAX_MSG_SIZE]=64Ko + jusqu'à deux
 * leo_exec_result_t ~20Ko chacun pour les commandes en deux étapes) — la
 * pile par défaut du système (souvent 8 Mio) serait un gâchis pour
 * LEO_EXEC_MAX_CONCURRENT threads simultanés. 512 Ko laisse une marge large
 * (~4x le pic estimé) tout en réduisant la réservation ~16x. */
#define LEO_EXEC_THREAD_STACK_SIZE  (512 * 1024)

/** Longueur maximale d'un nom de paquet accepté (charset restreint plus
 *  bas) — large pour les noms qualifiés par architecture ("libfoo:amd64"). */
#define LEO_PKG_NAME_MAX_LEN  128
/** Nombre max de paquets par commande INSTALL_PKG. */
#define LEO_PKG_MAX_COUNT     32

/** Type de commande portée par un _exec_ctx_t — détermine quel(s) appel(s) à
 *  leo_exec_script()/leo_exec_argv() _exec_thread() effectue. */
typedef enum {
    _EXEC_KIND_SCRIPT,       /* EXEC_SCRIPT : interpréteur+script arbitraires
                               * fournis par le backend, exécutés tels quels. */
    _EXEC_KIND_INSTALL_PKG,  /* INSTALL_PKG : argv structuré (apt-get), construit
                               * par l'agent — jamais de shell, jamais d'injection. */
    _EXEC_KIND_REBOOT        /* REBOOT : argv structuré (shutdown/systemctl),
                               * idem, jamais de shell. */
} _exec_kind_t;

/* Contexte transmis à un thread d'exécution — alloué par les fonctions
 * _dispatch_*()/_launch_exec(), libéré par _exec_thread() une fois la
 * commande terminée et le résultat envoyé. Les champs utilisés dépendent de
 * `kind` ; les champs non pertinents restent à zéro (ctx est calloc'é). */
typedef struct {
    leo_agent_t  *ag;
    char          cmd_id[LEO_UUID_STR_LEN];
    int           timeout_secs;
    _exec_kind_t  kind;

    /* _EXEC_KIND_SCRIPT */
    char          interpreter[32];
    char         *script;   /* alloué via strdup(), possédé par ce contexte */

    /* _EXEC_KIND_INSTALL_PKG */
    char          pkg_names[LEO_PKG_MAX_COUNT][LEO_PKG_NAME_MAX_LEN + 1];
    int           pkg_count;

    /* _EXEC_KIND_REBOOT */
    int           reboot_delay_sec;
} _exec_ctx_t;

/* ─── Helpers ───────────────────────────────────────────────────────────── */

/** Attend jusqu'à `secs` secondes, réellement endormi (pthread_cond_timedwait,
 *  pas de polling) — important maintenant que les intervalles vont jusqu'à
 *  plusieurs heures : réveiller un thread toutes les 100ms pendant 3h coûte
 *  bien plus cher en énergie qu'un vrai sleep bloquant.
 *  Réveil anticipé si threads_stop passe à true, ou si wake_on_force est vrai
 *  et que force_heartbeat_pending passe à true (mis par _on_message sur
 *  réception de FORCE_HEARTBEAT — sans ce réveil anticipé, un heartbeat forcé
 *  ne partirait qu'au prochain tick naturel, jusqu'à 3h plus tard). */
static void _interruptible_sleep(leo_agent_t *ag, int secs, bool wake_on_force) {
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += secs;

    pthread_mutex_lock(&ag->wake_mutex);
    while (!ag->threads_stop && !(wake_on_force && ag->force_heartbeat_pending)) {
        if (pthread_cond_timedwait(&ag->wake_cond, &ag->wake_mutex, &deadline) == ETIMEDOUT)
            break;
    }
    pthread_mutex_unlock(&ag->wake_mutex);
}

/* ─── Thread : Heartbeat ─────────────────────────────────────────────────── */

static void *_heartbeat_thread(void *arg) {
    leo_agent_t *ag  = (leo_agent_t *)arg;
    uint64_t     seq = 0;
    char         buf[512];

    LOG_INFO("Thread heartbeat démarré (intervalle=%ds)",
             ag->config.heartbeat_interval_sec);

    while (!ag->threads_stop) {
        _interruptible_sleep(ag, ag->config.heartbeat_interval_sec, /*wake_on_force=*/true);
        if (ag->threads_stop) break;

        /* Check if forced heartbeat was requested */
        pthread_mutex_lock(&ag->wake_mutex);
        bool force = ag->force_heartbeat_pending;
        ag->force_heartbeat_pending = false;
        pthread_mutex_unlock(&ag->wake_mutex);
        if (force) {
            LOG_DEBUG("Forced heartbeat triggered");
        }

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
        _interruptible_sleep(ag, ag->config.metrics_interval_sec, /*wake_on_force=*/false);
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

/**
 * Construit un CMD_RESULT d'échec (exit_code=-1, stdout vide) et l'envoie
 * immédiatement. Centralise un pattern répété dans les dispatchers de
 * commandes et les chemins d'erreur de lancement/allocation.
 */
static void _send_cmd_error(leo_agent_t *ag, const char *cmd_id, const char *err_msg) {
    char buf[LEO_MAX_MSG_SIZE];
    int  wlen = leo_proto_build_cmd_result(cmd_id, -1, "", err_msg, buf, sizeof(buf));
    if (wlen > 0) leo_conn_send(ag->conn, buf, (size_t)wlen);
}

/**
 * Réserve un slot d'exécution (borne LEO_EXEC_MAX_CONCURRENT, commune à tous
 * les types de commandes qui tournent dans un thread détaché — scripts et
 * collecte d'inventaire). Envoie un CMD_RESULT d'erreur et retourne false si
 * la limite est atteinte.
 */
static bool _reserve_exec_slot(leo_agent_t *ag, const char *cmd_id) {
    pthread_mutex_lock(&ag->exec_mutex);
    if (ag->exec_active_count >= LEO_EXEC_MAX_CONCURRENT) {
        pthread_mutex_unlock(&ag->exec_mutex);
        LOG_WARN("Trop de commandes en cours (max=%d) — commande rejetée (cmd_id=%s)",
                 LEO_EXEC_MAX_CONCURRENT, cmd_id);
        _send_cmd_error(ag, cmd_id, "Trop de commandes en cours d'exécution sur l'agent");
        return false;
    }
    ag->exec_active_count++;
    pthread_mutex_unlock(&ag->exec_mutex);
    return true;
}

/** Libère un slot réservé par _reserve_exec_slot() — appelé à la fin de
 *  chaque thread de commande, y compris sur les chemins d'erreur avant
 *  lancement du thread. Réveille leo_agent_stop() s'il attend. */
static void _release_exec_slot(leo_agent_t *ag) {
    pthread_mutex_lock(&ag->exec_mutex);
    ag->exec_active_count--;
    pthread_cond_signal(&ag->exec_cond);
    pthread_mutex_unlock(&ag->exec_mutex);
}

/** Libère un _exec_ctx_t et les ressources qu'il possède (ctx->script). */
static void _free_exec_ctx(void *p) {
    _exec_ctx_t *ctx = (_exec_ctx_t *)p;
    if (ctx) free(ctx->script);
    free(ctx);
}

/** Borne timeout_secs à ]0, LEO_EXEC_MAX_TIMEOUT_SEC] — 0/négatif devient le
 *  défaut, tout excès est écrêté. Partagé par les trois dispatchers de
 *  commandes qui lancent un thread d'exécution. */
static int _clamp_timeout(int timeout_secs) {
    if (timeout_secs <= 0) return LEO_EXEC_DEFAULT_TIMEOUT_SEC;
    if (timeout_secs > LEO_EXEC_MAX_TIMEOUT_SEC) return LEO_EXEC_MAX_TIMEOUT_SEC;
    return timeout_secs;
}

/**
 * Lance un thread détaché exécutant start_fn(ctx), avec une pile réduite
 * (LEO_EXEC_THREAD_STACK_SIZE). Sur échec de pthread_create(), libère ctx
 * (via free_ctx si non-NULL, sinon free() simple), relâche le slot
 * d'exécution déjà réservé par l'appelant, et envoie un CMD_RESULT d'erreur.
 * Centralise le pattern dupliqué entre lancement de script/commande et
 * lancement de la collecte d'inventaire.
 * @return true si le thread a démarré (ctx est alors possédé par le thread,
 *         plus par l'appelant) ; false si le slot a été relâché et ctx libéré.
 */
static bool _spawn_detached(leo_agent_t *ag, const char *cmd_id,
                             void *(*start_fn)(void *), void *ctx,
                             void (*free_ctx)(void *)) {
    pthread_attr_t attr;
    pthread_attr_init(&attr);
    pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED);
    if (pthread_attr_setstacksize(&attr, LEO_EXEC_THREAD_STACK_SIZE) != 0) {
        LOG_WARN("pthread_attr_setstacksize a échoué — pile par défaut du système utilisée");
    }

    pthread_t th;
    int prc = pthread_create(&th, &attr, start_fn, ctx);
    pthread_attr_destroy(&attr);

    if (prc != 0) {
        LOG_ERROR("pthread_create a échoué pour la commande (cmd_id=%s) : %d", cmd_id, prc);
        _release_exec_slot(ag);
        if (free_ctx) free_ctx(ctx); else free(ctx);
        _send_cmd_error(ag, cmd_id, "Erreur interne de l'agent (thread)");
        return false;
    }
    return true;
}

/* ─── Thread : exécution d'une commande (un thread détaché par commande) ── */

/** Variables d'environnement communes aux invocations apt-get — supprime les
 *  invites interactives (debconf) qui bloqueraient indéfiniment un process
 *  sans terminal attaché. */
static const char *const LEO_APT_ENV[] = { "DEBIAN_FRONTEND=noninteractive", NULL };

/**
 * Exécute la commande portée par ctx (selon ctx->kind) puis envoie le
 * CMD_RESULT correspondant. Ne bloque jamais le thread WSS : c'est tout
 * l'intérêt d'un thread dédié, puisque l'exécution peut bloquer jusqu'à
 * ctx->timeout_secs (par étape, pour les commandes en plusieurs étapes).
 */
static void *_exec_thread(void *arg) {
    _exec_ctx_t *ctx = (_exec_ctx_t *)arg;
    leo_agent_t *ag  = ctx->ag;

    int         exit_code = -1;
    const char *stdout_s  = "";
    const char *stderr_s  = "";
    char        err_buf[128];
    leo_exec_result_t result;

    switch (ctx->kind) {

    case _EXEC_KIND_SCRIPT: {
        leo_error_t rc = leo_exec_script(ctx->interpreter, ctx->script,
                                         ctx->timeout_secs, &result);
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
        break;
    }

    case _EXEC_KIND_INSTALL_PKG: {
        /* Reproduit "apt-get update -qq && apt-get install ... -- pkgs" en
         * deux appels argv successifs, sans jamais passer par un shell :
         * si l'update échoue, l'install n'est pas tentée (même sémantique
         * que le "&&" shell qu'utilisait l'ancienne implémentation). */
        char *update_argv[] = { "apt-get", "update", "-qq", NULL };
        leo_error_t rc = leo_exec_argv(update_argv, LEO_APT_ENV,
                                        ctx->timeout_secs, &result);
        if (rc != LEO_OK || result.exit_code != 0) {
            if (rc == LEO_OK) {
                exit_code = result.exit_code;
                stdout_s  = result.stdout_buf;
                stderr_s  = result.stderr_buf;
            } else if (rc == LEO_ERR_TIMEOUT) {
                stderr_s = "Timeout pendant 'apt-get update'";
            } else {
                stderr_s = "Erreur système pendant 'apt-get update'";
            }
            break;
        }

        char *install_argv[LEO_PKG_MAX_COUNT + 6];
        int   ac = 0;
        install_argv[ac++] = "apt-get";
        install_argv[ac++] = "install";
        install_argv[ac++] = "-y";
        install_argv[ac++] = "--no-install-recommends";
        install_argv[ac++] = "--";
        for (int i = 0; i < ctx->pkg_count; i++)
            install_argv[ac++] = ctx->pkg_names[i];
        install_argv[ac] = NULL;

        leo_exec_result_t install_result;
        leo_error_t irc = leo_exec_argv(install_argv, LEO_APT_ENV,
                                         ctx->timeout_secs, &install_result);
        if (irc == LEO_OK) {
            exit_code = install_result.exit_code;
            stdout_s  = install_result.stdout_buf;
            stderr_s  = install_result.stderr_buf;
        } else if (irc == LEO_ERR_TIMEOUT) {
            stderr_s = "Timeout pendant 'apt-get install'";
        } else {
            stderr_s = "Erreur système pendant 'apt-get install'";
        }
        break;
    }

    case _EXEC_KIND_REBOOT: {
        /* Reproduit "shutdown -r ... || systemctl reboot" en deux appels
         * argv successifs, sans shell : si shutdown échoue, on retente via
         * systemctl (même sémantique que le "||" shell d'origine). */
        char  delay_arg[16];
        char *shutdown_argv[4] = { "shutdown", "-r", "now", NULL };
        if (ctx->reboot_delay_sec > 0) {
            int minutes = (ctx->reboot_delay_sec + 59) / 60;
            snprintf(delay_arg, sizeof(delay_arg), "+%d", minutes);
            shutdown_argv[2] = delay_arg;
        }

        leo_error_t rc = leo_exec_argv(shutdown_argv, NULL, ctx->timeout_secs, &result);
        if (rc == LEO_OK && result.exit_code == 0) {
            exit_code = result.exit_code;
            stdout_s  = result.stdout_buf;
            stderr_s  = result.stderr_buf;
            break;
        }

        char *systemctl_argv[] = { "systemctl", "reboot", NULL };
        leo_exec_result_t fallback_result;
        leo_error_t frc = leo_exec_argv(systemctl_argv, NULL, ctx->timeout_secs, &fallback_result);
        if (frc == LEO_OK) {
            exit_code = fallback_result.exit_code;
            stdout_s  = fallback_result.stdout_buf;
            stderr_s  = fallback_result.stderr_buf;
        } else if (frc == LEO_ERR_TIMEOUT) {
            stderr_s = "Timeout pendant la planification du redémarrage";
        } else {
            stderr_s = "Erreur système lors de la planification du redémarrage "
                       "(shutdown et systemctl ont tous deux échoué)";
        }
        break;
    }
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

    LOG_INFO("Commande terminée (cmd_id=%s, kind=%d, exit_code=%d)",
             ctx->cmd_id, (int)ctx->kind, exit_code);

    _free_exec_ctx(ctx);
    _release_exec_slot(ag);

    return NULL;
}

/**
 * Lance un thread détaché qui exécute {interpreter, script} via leo_exec_script()
 * puis renvoie le CMD_RESULT correspondant. Utilisé par EXEC_SCRIPT — seule
 * commande où le script est fourni tel quel par le backend (interpréteur
 * whitelisté, mais contenu du script non contrôlé par l'agent).
 *
 * Envoie un CMD_RESULT d'erreur immédiat si le nombre max de commandes
 * concurrentes est atteint ou en cas d'échec d'allocation/thread (jamais de
 * blocage du thread WSS ici).
 */
static void _launch_exec(leo_agent_t *ag, const char *cmd_id,
                          const char *interpreter, const char *script,
                          int timeout_secs) {
    timeout_secs = _clamp_timeout(timeout_secs);

    if (!_reserve_exec_slot(ag, cmd_id))
        return;

    _exec_ctx_t *ctx = calloc(1, sizeof(*ctx));
    if (ctx) ctx->script = strdup(script);

    if (!ctx || !ctx->script) {
        LOG_ERROR("Allocation échouée pour la commande (cmd_id=%s)", cmd_id);
        free(ctx);
        _release_exec_slot(ag);
        _send_cmd_error(ag, cmd_id, "Erreur interne de l'agent (allocation)");
        return;
    }

    ctx->ag   = ag;
    ctx->kind = _EXEC_KIND_SCRIPT;
    strncpy(ctx->cmd_id, cmd_id, sizeof(ctx->cmd_id) - 1);
    strncpy(ctx->interpreter, interpreter, sizeof(ctx->interpreter) - 1);
    ctx->timeout_secs = timeout_secs;

    if (_spawn_detached(ag, cmd_id, _exec_thread, ctx, _free_exec_ctx)) {
        LOG_INFO("Commande lancée (cmd_id=%s, interpreter=%s, timeout=%ds)",
                 cmd_id, interpreter, timeout_secs);
    }
}

/**
 * Convertit un cJSON number (double) en int en bornant AVANT le cast.
 * Un champ numérique venant du backend peut contenir n'importe quelle
 * valeur JSON (ex: 1e300) — caster un double hors de portée d'un int est un
 * comportement indéfini en C (pas juste une troncature "surprenante"), donc
 * le bornage doit avoir lieu sur le double, avant le cast, pas après.
 * NaN est également géré explicitement : "v < lo" et "v > hi" valent tous
 * deux false pour NaN (aucune comparaison n'est vraie avec NaN), donc sans
 * ce test le code tomberait jusqu'au cast (int)NaN — également UB. cJSON
 * ne produit normalement pas NaN depuis un nombre JSON valide (strtod ne
 * génère pas NaN à partir d'une syntaxe décimale), mais on ne dépend pas de
 * cette garantie pour éviter l'UB dans ce helper partagé.
 */
static int _json_number_clamped(double v, int lo, int hi) {
    if (v != v) return lo;  /* NaN */
    if (v < (double)lo) return lo;
    if (v > (double)hi) return hi;
    return (int)v;
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
            timeout_secs = _json_number_clamped(jt->valuedouble, 1, LEO_EXEC_MAX_TIMEOUT_SEC);
    }

    if (!interpreter || !script) {
        LOG_WARN("EXEC_SCRIPT sans 'interpreter'/'script' — ignoré (cmd_id=%s)", cmd_id);
        _send_cmd_error(ag, cmd_id, "Requête invalide : 'interpreter' et 'script' requis");
        return;
    }

    _launch_exec(ag, cmd_id, interpreter, script, timeout_secs);
}

/**
 * Valide un nom de paquet contre un charset restreint (alphanumérique + . + -
 * _ :), et sa longueur contre LEO_PKG_NAME_MAX_LEN (la place réservée dans
 * _exec_ctx_t.pkg_names[i]). Ce nom vient du backend et potentiellement, en
 * amont, d'un utilisateur de la console web. Contrairement à l'ancienne
 * implémentation (script shell construit à la main), INSTALL_PKG exécute
 * désormais apt-get directement via execvp() — il n'y a donc plus de shell
 * pour interpréter un ";" ou un "$(...)" dans un nom de paquet. Cette
 * validation reste une défense en profondeur utile (rejette des noms
 * absurdes tôt, avec un message clair) mais n'est plus la seule barrière
 * contre l'injection de commande.
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
 * { "package": "a" }, "timeout_sec" optionnel. Les noms de paquets validés
 * (voir _pkg_name_valid) sont exécutés via apt-get en argv structuré
 * (_EXEC_KIND_INSTALL_PKG dans _exec_thread) — jamais via un shell.
 */
static void _dispatch_install_pkg(leo_agent_t *ag, const char *cmd_id, cJSON *body) {
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
            timeout_secs = _json_number_clamped(jt->valuedouble, 1, LEO_EXEC_MAX_TIMEOUT_SEC);
    }

    if (n == 0) {
        LOG_WARN("INSTALL_PKG sans 'package'/'packages' — ignoré (cmd_id=%s)", cmd_id);
        _send_cmd_error(ag, cmd_id, "Requête invalide : 'package' ou 'packages' requis");
        return;
    }

    for (int i = 0; i < n; i++) {
        if (!_pkg_name_valid(names[i])) {
            LOG_WARN("INSTALL_PKG : nom de paquet invalide, commande rejetée (cmd_id=%s)", cmd_id);
            _send_cmd_error(ag, cmd_id, "Requête invalide : nom de paquet non autorisé");
            return;
        }
    }

    timeout_secs = _clamp_timeout(timeout_secs);
    if (!_reserve_exec_slot(ag, cmd_id))
        return;

    _exec_ctx_t *ctx = calloc(1, sizeof(*ctx));
    if (!ctx) {
        LOG_ERROR("Allocation échouée pour la commande (cmd_id=%s)", cmd_id);
        _release_exec_slot(ag);
        _send_cmd_error(ag, cmd_id, "Erreur interne de l'agent (allocation)");
        return;
    }
    ctx->ag   = ag;
    ctx->kind = _EXEC_KIND_INSTALL_PKG;
    strncpy(ctx->cmd_id, cmd_id, sizeof(ctx->cmd_id) - 1);
    ctx->timeout_secs = timeout_secs;
    ctx->pkg_count    = n;
    for (int i = 0; i < n; i++)
        strncpy(ctx->pkg_names[i], names[i], LEO_PKG_NAME_MAX_LEN);

    if (_spawn_detached(ag, cmd_id, _exec_thread, ctx, _free_exec_ctx)) {
        LOG_INFO("INSTALL_PKG lancé (cmd_id=%s, %d paquet(s), timeout=%ds)",
                 cmd_id, n, timeout_secs);
    }
}

/** Délai par défaut avant redémarrage, en secondes — laisse le temps à
 * l'utilisateur de la machine de sauvegarder son travail. */
#define LEO_REBOOT_DEFAULT_DELAY_SEC  60
/** Borne haute du délai accepté. */
#define LEO_REBOOT_MAX_DELAY_SEC      3600
/* shutdown planifie le redémarrage et retourne immédiatement (comme
 * systemctl reboot en cas de repli) : ce timeout ne couvre que la
 * planification, pas l'attente du délai lui-même. */
#define LEO_REBOOT_SCHEDULE_TIMEOUT_SEC  15

/**
 * Dispatch d'une commande REBOOT : { "delay_sec": <int> } optionnel (défaut
 * LEO_REBOOT_DEFAULT_DELAY_SEC). Planifie `shutdown -r` (avec repli
 * `systemctl reboot`) en argv structuré (_EXEC_KIND_REBOOT dans
 * _exec_thread) — jamais via un shell. `shutdown` rend la main tout de
 * suite : le CMD_RESULT confirme donc la planification, pas l'exécution
 * effective du redémarrage (qui coupera la connexion de l'agent de toute
 * façon).
 */
static void _dispatch_reboot(leo_agent_t *ag, const char *cmd_id, cJSON *body) {
    int delay_sec = LEO_REBOOT_DEFAULT_DELAY_SEC;

    if (body) {
        cJSON *jd = cJSON_GetObjectItemCaseSensitive(body, "delay_sec");
        if (cJSON_IsNumber(jd) && jd->valuedouble >= 0)
            delay_sec = _json_number_clamped(jd->valuedouble, 0, LEO_REBOOT_MAX_DELAY_SEC);
    }

    if (!_reserve_exec_slot(ag, cmd_id))
        return;

    _exec_ctx_t *ctx = calloc(1, sizeof(*ctx));
    if (!ctx) {
        LOG_ERROR("Allocation échouée pour la commande (cmd_id=%s)", cmd_id);
        _release_exec_slot(ag);
        _send_cmd_error(ag, cmd_id, "Erreur interne de l'agent (allocation)");
        return;
    }
    ctx->ag                = ag;
    ctx->kind               = _EXEC_KIND_REBOOT;
    strncpy(ctx->cmd_id, cmd_id, sizeof(ctx->cmd_id) - 1);
    ctx->timeout_secs      = LEO_REBOOT_SCHEDULE_TIMEOUT_SEC;
    ctx->reboot_delay_sec  = delay_sec;

    LOG_WARN("REBOOT planifié dans %ds (cmd_id=%s)", delay_sec, cmd_id);
    if (_spawn_detached(ag, cmd_id, _exec_thread, ctx, _free_exec_ctx)) {
        LOG_INFO("REBOOT lancé (cmd_id=%s)", cmd_id);
    }
}

/* ─── Thread : collecte d'inventaire (un thread détaché par commande) ───── */

typedef struct {
    leo_agent_t *ag;
    char         cmd_id[LEO_UUID_STR_LEN];
} _inventory_ctx_t;

/**
 * Collecte l'inventaire matériel + logiciel, envoie le message INVENTORY
 * (LEO_MSG_INVENTORY, séparé) puis le CMD_RESULT qui clôt la commande
 * COLLECT_INVENTORY. Dans un thread dédié : la collecte logicielle passe par
 * dpkg-query (popen), potentiellement lente sur un système avec beaucoup de
 * paquets — ne doit jamais bloquer le thread WSS.
 */
static void *_inventory_thread(void *arg) {
    _inventory_ctx_t *ctx = (_inventory_ctx_t *)arg;
    leo_agent_t      *ag  = ctx->ag;

    leo_hw_inventory_t hw;
    leo_error_t hw_rc = leo_inventory_collect_hw(&hw);

    leo_sw_item_t *sw = calloc(LEO_INVENTORY_MAX_SW_ITEMS, sizeof(leo_sw_item_t));
    int sw_count = sw ? leo_inventory_collect_sw(sw, LEO_INVENTORY_MAX_SW_ITEMS) : -1;
    if (sw_count < 0) sw_count = 0;

    char        buf[LEO_MAX_MSG_SIZE];
    int         exit_code = -1;
    const char *err_s     = "";
    char        summary[64] = "";

    if (hw_rc != LEO_OK) {
        err_s = "Échec de la collecte de l'inventaire matériel";
    } else {
        int wlen = leo_proto_build_inventory(&hw, sw, sw_count, buf, sizeof(buf));
        if (wlen <= 0) {
            err_s = "Échec de sérialisation de l'inventaire (trop volumineux ?)";
        } else if (leo_conn_send(ag->conn, buf, (size_t)wlen) != LEO_OK) {
            err_s = "Échec envoi du message INVENTORY";
        } else {
            exit_code = 0;
            snprintf(summary, sizeof(summary), "Inventaire envoyé (%d logiciel(s))", sw_count);
        }
    }

    free(sw);

    int wlen2 = leo_proto_build_cmd_result(ctx->cmd_id, exit_code, summary, err_s,
                                           buf, sizeof(buf));
    if (wlen2 > 0) leo_conn_send(ag->conn, buf, (size_t)wlen2);

    LOG_INFO("COLLECT_INVENTORY terminé (cmd_id=%s, exit_code=%d, sw_count=%d)",
             ctx->cmd_id, exit_code, sw_count);

    free(ctx);
    _release_exec_slot(ag);

    return NULL;
}

/**
 * Dispatch d'une commande COLLECT_INVENTORY : pas de paramètres. Lance un
 * thread détaché sous la même borne de concurrence que les autres commandes.
 */
static void _dispatch_collect_inventory(leo_agent_t *ag, const char *cmd_id) {
    if (!_reserve_exec_slot(ag, cmd_id))
        return;

    _inventory_ctx_t *ctx = calloc(1, sizeof(*ctx));
    if (!ctx) {
        LOG_ERROR("Allocation échouée pour COLLECT_INVENTORY (cmd_id=%s)", cmd_id);
        _release_exec_slot(ag);
        _send_cmd_error(ag, cmd_id, "Erreur interne de l'agent (allocation)");
        return;
    }
    ctx->ag = ag;
    strncpy(ctx->cmd_id, cmd_id, sizeof(ctx->cmd_id) - 1);

    if (_spawn_detached(ag, cmd_id, _inventory_thread, ctx, NULL)) {
        LOG_INFO("COLLECT_INVENTORY lancé (cmd_id=%s)", cmd_id);
    }
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
                ag->config.heartbeat_interval_sec = _json_number_clamped(jhi->valuedouble, 1, 86400);
            if (cJSON_IsNumber(jmi) && jmi->valuedouble > 0)
                ag->config.metrics_interval_sec = _json_number_clamped(jmi->valuedouble, 1, 86400);
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
        LOG_INFO("Demande d'inventaire reçue (cmd_id=%s)", msg.id);
        _dispatch_collect_inventory(ag, msg.id);
        break;

    case LEO_MSG_FORCE_HEARTBEAT:
        LOG_INFO("Force heartbeat reçue du serveur");
        pthread_mutex_lock(&ag->wake_mutex);
        ag->force_heartbeat_pending = true;
        pthread_cond_broadcast(&ag->wake_cond);
        pthread_mutex_unlock(&ag->wake_mutex);
        break;

    case LEO_MSG_CONFIG_UPDATE:
        LOG_INFO("Mise à jour de configuration reçue");
        if (msg.body) {
            cJSON *jmi = cJSON_GetObjectItemCaseSensitive(msg.body, "metrics_interval_sec");
            if (cJSON_IsNumber(jmi) && jmi->valuedouble > 0) {
                ag->config.metrics_interval_sec = _json_number_clamped(jmi->valuedouble, 1, 86400);
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

    pthread_mutex_init(&ag->wake_mutex, NULL);
    pthread_cond_init(&ag->wake_cond, NULL);

    /* ── Chargement de la configuration ── */
    if (leo_config_load(config_path, &ag->config) != LEO_OK) {
        LOG_FATAL("Impossible de charger la configuration depuis %s", config_path);
        pthread_cond_destroy(&ag->wake_cond);
        pthread_mutex_destroy(&ag->wake_mutex);
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    /* ── Initialisation du sous-système métriques ── */
    if (leo_metrics_init() != LEO_OK) {
        LOG_FATAL("Impossible d'initialiser le sous-système métriques");
        pthread_cond_destroy(&ag->wake_cond);
        pthread_mutex_destroy(&ag->wake_mutex);
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
        pthread_cond_destroy(&ag->wake_cond);
        pthread_mutex_destroy(&ag->wake_mutex);
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        free(ag);
        return NULL;
    }

    /* ── Lancement des threads ── */
    if (pthread_create(&ag->heartbeat_thread, NULL, _heartbeat_thread, ag) != 0) {
        LOG_FATAL("Impossible de créer le thread heartbeat");
        leo_metrics_destroy();
        pthread_cond_destroy(&ag->wake_cond);
        pthread_mutex_destroy(&ag->wake_mutex);
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        /* Si leo_conn_destroy() n'a pas pu joindre le thread WSS (conn->config
         * pointe vers ag->config), on abandonne ag SANS le libérer : un
         * free(ag) ici serait un use-after-free pour ce thread encore actif. */
        if (leo_conn_destroy(ag->conn)) free(ag);
        return NULL;
    }

    if (pthread_create(&ag->metrics_thread, NULL, _metrics_thread, ag) != 0) {
        LOG_FATAL("Impossible de créer le thread métriques");
        pthread_mutex_lock(&ag->wake_mutex);
        ag->threads_stop = true;
        pthread_cond_broadcast(&ag->wake_cond);
        pthread_mutex_unlock(&ag->wake_mutex);
        pthread_join(ag->heartbeat_thread, NULL);
        leo_metrics_destroy();
        pthread_cond_destroy(&ag->wake_cond);
        pthread_mutex_destroy(&ag->wake_mutex);
        pthread_cond_destroy(&ag->exec_cond);
        pthread_mutex_destroy(&ag->exec_mutex);
        /* Voir commentaire ci-dessus. */
        if (leo_conn_destroy(ag->conn)) free(ag);
        return NULL;
    }

    LOG_INFO("Agent Leo-One v%s démarré — agent_id=%s",
             LEO_AGENT_VERSION, ag->config.agent_id);
    return ag;
}

void leo_agent_stop(leo_agent_t *ag) {
    if (!ag) return;

    LOG_INFO("Arrêt de l'agent…");
    ag->state = LEO_STATE_STOPPING;

    /* Réveille immédiatement les threads heartbeat/métriques s'ils dorment
     * dans _interruptible_sleep — sans ce broadcast, l'arrêt attendrait
     * jusqu'à l'intervalle en cours (jusqu'à 3h pour le heartbeat). */
    pthread_mutex_lock(&ag->wake_mutex);
    ag->threads_stop = true;
    pthread_cond_broadcast(&ag->wake_cond);
    pthread_mutex_unlock(&ag->wake_mutex);

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

    /* leo_conn_destroy() peut abandonner (return false) si le thread WSS ne
     * rejoint pas dans son délai — dans ce cas conn (et le thread encore
     * actif) survivent délibérément, et conn->config pointe toujours vers
     * ag->config : libérer ag ci-dessous serait alors un use-after-free pour
     * ce thread. On abandonne tout l'arrêt plutôt que de libérer ag. */
    if (!leo_conn_destroy(ag->conn)) {
        LOG_ERROR("Connexion WSS non détruite proprement — abandon de l'arrêt "
                  "(fuite volontaire de l'agent entier, évite un "
                  "use-after-free sur ag->config depuis le thread WSS encore actif)");
        return;
    }
    leo_metrics_destroy();

    pthread_cond_destroy(&ag->wake_cond);
    pthread_mutex_destroy(&ag->wake_mutex);
    pthread_cond_destroy(&ag->exec_cond);
    pthread_mutex_destroy(&ag->exec_mutex);

    LOG_INFO("Agent arrêté proprement");
    free(ag);
}

leo_agent_state_t leo_agent_get_state(const leo_agent_t *ag) {
    return ag ? ag->state : LEO_STATE_INIT;
}
