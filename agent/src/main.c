/**
 * main.c — Point d'entrée de l'agent Leo-One
 *
 * Responsabilités :
 *   1. Initialisation du logger
 *   2. Démarrage de l'agent (leo_agent_start) et attente jusqu'à l'arrêt
 *   3. Arrêt propre (leo_agent_stop)
 *
 * Sur Linux/macOS : exécuté comme démon par systemd/launchd, arrêt propre
 * via SIGTERM/SIGINT (sigaction).
 * Sur Windows : tente d'abord de s'enregistrer auprès du Service Control
 * Manager (leo_service_run_dispatcher, voir platform/windows/service_win.c)
 * — si lancé par le SCM, celui-ci gère tout le cycle de vie de l'agent en
 * interne ; sinon (lancé depuis un terminal), repli sur un mode console
 * avec SetConsoleCtrlHandler pour Ctrl+C.
 */
#include "agent.h"
#include "logger.h"
#include "../include/leo_agent.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#ifdef _WIN32
#  include <windows.h>
#  include "../platform/windows/service_win.h"
#else
#  include <signal.h>
#  include <unistd.h>
#endif

#ifdef _WIN32

/* ─── Mode console (repli si pas lancé par le SCM) ───────────────────────── */

static volatile bool g_stop_requested = false;

static BOOL WINAPI _console_ctrl_handler(DWORD ctrl_type) {
    switch (ctrl_type) {
    case CTRL_C_EVENT:
    case CTRL_BREAK_EVENT:
    case CTRL_CLOSE_EVENT:
    case CTRL_SHUTDOWN_EVENT:
        g_stop_requested = true;
        return TRUE;
    default:
        return FALSE;
    }
}

static int _run_console_mode(const char *config_path) {
    SetConsoleCtrlHandler(_console_ctrl_handler, TRUE);

    leo_agent_t *agent = leo_agent_start(config_path);
    if (!agent) {
        LOG_FATAL("Impossible de démarrer l'agent — arrêt");
        return EXIT_FAILURE;
    }

    LOG_INFO("Agent actif — en attente (PID=%lu)", GetCurrentProcessId());

    while (!g_stop_requested) {
        Sleep(1000);
    }

    LOG_INFO("Signal d'arrêt reçu — arrêt de l'agent…");
    leo_agent_stop(agent);
    LOG_INFO("Agent terminé proprement");
    return EXIT_SUCCESS;
}

#else

/* ─── Signal handling (Linux/macOS) ──────────────────────────────────────── */

static volatile sig_atomic_t g_stop_requested = 0;

static void _signal_handler(int sig) {
    (void)sig;
    g_stop_requested = 1;
}

#endif

/* ─── Point d'entrée ──────────────────────────────────────────────────── */

int main(int argc, char **argv) {
    /* Chemin de config optionnel en argument (défaut = LEO_CONFIG_FILE) */
    const char *config_path = (argc > 1) ? argv[1] : LEO_CONFIG_FILE;

    /* ── Initialisation du logger ── */
    int log_rc = leo_log_init(LEO_LOG_PATH, LOG_INFO, 10L * 1024 * 1024);
    if (log_rc != 0) {
        fprintf(stderr, "[leo-agent] Logger initialisé sur stderr uniquement\n");
    }

    LOG_INFO("═══════════════════════════════════════════════");
    LOG_INFO(" Leo-One Agent v%s — démarrage", LEO_AGENT_VERSION);
    LOG_INFO("═══════════════════════════════════════════════");
    LOG_INFO("Configuration : %s", config_path);

#ifdef _WIN32
    /* leo_service_run_dispatcher() gère tout le cycle de vie de l'agent en
     * interne si lancé par le SCM (bloquant jusqu'à l'arrêt du service) —
     * il n'y a alors plus rien à faire ici une fois qu'elle retourne true. */
    if (leo_service_run_dispatcher()) {
        leo_log_destroy();
        return EXIT_SUCCESS;
    }

    int rc = _run_console_mode(config_path);
    leo_log_destroy();
    return rc;
#else
    /* ── Gestionnaire de signaux pour arrêt propre ── */
    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = _signal_handler;
    sigemptyset(&sa.sa_mask);
    sigaction(SIGTERM, &sa, NULL);
    sigaction(SIGINT,  &sa, NULL);
    signal(SIGPIPE, SIG_IGN);  /* Évite le crash sur write d'une socket fermée */

    /* ── Démarrage de l'agent ── */
    leo_agent_t *agent = leo_agent_start(config_path);
    if (!agent) {
        LOG_FATAL("Impossible de démarrer l'agent — arrêt");
        leo_log_destroy();
        return EXIT_FAILURE;
    }

    /* ── Boucle principale : attente du signal d'arrêt ── */
    LOG_INFO("Agent actif — en attente (PID=%d)", (int)getpid());

    while (!g_stop_requested) {
        struct timespec ts = { .tv_sec = 1, .tv_nsec = 0 };
        nanosleep(&ts, NULL);
    }

    /* ── Arrêt propre ── */
    LOG_INFO("Signal d'arrêt reçu — arrêt de l'agent…");
    leo_agent_stop(agent);

    LOG_INFO("Agent terminé proprement");
    leo_log_destroy();

    return EXIT_SUCCESS;
#endif
}
