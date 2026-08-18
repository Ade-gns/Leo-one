/**
 * patches.h — Interface générique de gestion des mises à jour système,
 * implémentée par chaque module platform/ :
 *   platform/linux/patches_linux.c    (apt/dnf)
 *   platform/windows/patches_win.c    (Windows Update Agent / COM)
 *
 * Le code appelant (agent.c) n'a jamais connaissance de la plateforme.
 */
#ifndef LEO_PATCHES_H
#define LEO_PATCHES_H

#include "../include/leo_agent.h"
#include "executor.h"

/**
 * Liste les mises à jour disponibles, jusqu'à max_items entrées.
 * Best-effort, comme leo_inventory_collect_sw() : une source de données
 * indisponible (ex : ni apt ni dnf présents) retourne 0 plutôt que d'échouer.
 * @return nombre d'entrées écrites (0..max_items), ou -1 en cas d'erreur système
 */
int leo_patches_collect(leo_patch_t *out, int max_items);

/**
 * Installe les patchs dont l'identifiant (leo_patch_t.id) figure dans
 * ids[0..count). Bloquant — appelé depuis un thread dédié (voir
 * _EXEC_KIND_INSTALL_PATCHES dans agent.c), jamais depuis le thread WSS.
 * @param ids          Tableau d'identifiants natifs (voir leo_patch_t.id),
 *                      déjà validés par l'appelant (voir _patch_id_valid
 *                      dans agent.c) — jamais passés à un shell.
 * @param count         Nombre d'entrées dans ids
 * @param reboot_after  Si true, planifie un redémarrage une fois
 *                      l'installation terminée avec succès (voir
 *                      implémentation platform/ pour le délai appliqué)
 * @param result        Résultat à remplir (alloué par l'appelant)
 * @return LEO_OK, LEO_ERR_SYSTEM, ou LEO_ERR_TIMEOUT
 */
leo_error_t leo_patches_install(const char *const ids[], int count, bool reboot_after,
                                 int timeout_secs, leo_exec_result_t *result);

#endif /* LEO_PATCHES_H */
