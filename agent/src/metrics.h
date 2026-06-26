/**
 * metrics.h — Interface générique de collecte de métriques système
 *
 * Cette interface est implémentée par chaque module platform/ :
 *   platform/linux/metrics_linux.c
 *   platform/windows/metrics_win.c
 *   platform/macos/metrics_macos.c
 *
 * Le code appelant n'a jamais connaissance de la plateforme.
 */
#ifndef LEO_METRICS_H
#define LEO_METRICS_H

#include "../include/leo_agent.h"

/**
 * Initialise le sous-système de collecte.
 * Doit être appelé une fois au démarrage de l'agent.
 * @return LEO_OK ou LEO_ERR_SYSTEM
 */
leo_error_t leo_metrics_init(void);

/**
 * Collecte un snapshot complet des métriques système.
 * Bloquant, mais rapide (< 500ms sur toutes les plateformes).
 * @param out  Structure à remplir (allouée par l'appelant)
 * @return LEO_OK ou code d'erreur
 */
leo_error_t leo_metrics_collect(leo_metrics_t *out);

/** Libère les ressources du sous-système de collecte. */
void leo_metrics_destroy(void);

/* ─── Utilitaires communs (src/metrics.c) ─────────────────────────────────── */

/**
 * Retourne true si le snapshot a été rempli (timestamp non nul).
 */
bool leo_metrics_is_valid(const leo_metrics_t *m);

/**
 * Écrit un résumé lisible du snapshot dans les logs (niveau DEBUG).
 */
void leo_metrics_log_summary(const leo_metrics_t *m);

#endif /* LEO_METRICS_H */
