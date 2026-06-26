/**
 * metrics.c — Utilitaires communs pour les métriques (indépendant de la plateforme)
 *
 * Ce fichier fournit des fonctions utilitaires partagées par toutes les plateformes.
 * Les fonctions leo_metrics_init / leo_metrics_collect / leo_metrics_destroy
 * sont implémentées dans platform/<os>/metrics_<os>.c et ne doivent PAS être
 * redéfinies ici sous peine de "multiple definition" au link.
 */
#include "metrics.h"
#include "logger.h"
#include <string.h>
#include <stdio.h>

/**
 * Vérifie qu'un snapshot est valide (a été rempli).
 */
bool leo_metrics_is_valid(const leo_metrics_t *m) {
    return m != NULL && m->timestamp_ms > 0;
}

/**
 * Formate un résumé lisible pour les logs.
 * Utile dans agent.c pour le log du thread métriques.
 */
void leo_metrics_log_summary(const leo_metrics_t *m) {
    if (!leo_metrics_is_valid(m)) {
        LOG_WARN("Snapshot de métriques invalide");
        return;
    }
    LOG_DEBUG("Métriques: CPU=%.1f%% RAM=%lluMB/%lluMB Disk=%lluGB/%lluGB Procs=%u",
              m->cpu_total_percent,
              (unsigned long long)(m->ram_used_bytes  / (1024ULL * 1024ULL)),
              (unsigned long long)(m->ram_total_bytes  / (1024ULL * 1024ULL)),
              (unsigned long long)(m->disk_used_bytes  / (1024ULL * 1024ULL * 1024ULL)),
              (unsigned long long)(m->disk_total_bytes / (1024ULL * 1024ULL * 1024ULL)),
              m->process_count);
}
