/**
 * inventory.h — Interface générique de collecte d'inventaire matériel/logiciel,
 * implémentée par chaque module platform/ :
 *   platform/linux/inventory_linux.c
 *   platform/windows/inventory_win.c
 *   platform/macos/inventory_macos.c
 *
 * Le code appelant (agent.c) n'a jamais connaissance de la plateforme.
 */
#ifndef LEO_INVENTORY_H
#define LEO_INVENTORY_H

#include "../include/leo_agent.h"

/**
 * Collecte un snapshot de l'inventaire matériel.
 * Best-effort : les champs indisponibles (ex: pas de privilège root pour lire
 * le numéro de série) sont laissés vides plutôt que de faire échouer l'appel.
 * @return LEO_OK, ou LEO_ERR_SYSTEM en cas d'échec total (rare).
 */
leo_error_t leo_inventory_collect_hw(leo_hw_inventory_t *out);

/**
 * Collecte la liste des logiciels installés, jusqu'à max_items entrées.
 * @param out        Tableau alloué par l'appelant, taille >= max_items
 * @param max_items  Capacité de out
 * @return nombre d'entrées écrites (0..max_items), ou -1 en cas d'erreur système
 */
int leo_inventory_collect_sw(leo_sw_item_t *out, int max_items);

#endif /* LEO_INVENTORY_H */
