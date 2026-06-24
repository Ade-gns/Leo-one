/**
 * main.c — Point d'entrée de l'agent Leo-One
 *
 * Responsabilités :
 *   1. Initialisation du logger
 *   2. Gestion des signaux SIGTERM / SIGINT pour arrêt propre
 *   3. Démarrage de l'agent (leo_agent_start)
 *   4. Boucle d'attente jusqu'au signal d'arrêt
 *
 * Sur Linux/macOS : exécuté comme démon par systemd / launchd.
 * Sur Windows     : service_win.c wrappera ce main() via SCM.
 */
#include "agent.h"
#include "logger.h"
#include "../include/leo_agent.h"

#include <stdio.h>
#include <stdlib.h>
#include <signal.h>
#include <string.h>
#include <unistd.h>
#include <time.h>

/* ─── Signal handling ───────────────────────────────────────────────────── */

static volatile sig_atomic_t g_stop_requested = 0;

static void _signal_handler(int sig) {
    (void)sig;
    g_stop_requested = 1;
}

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
}
