/**
 * config.h — Lecture et écriture de la configuration de l'agent
 *
 * Format INI minimal :
 *   clé=valeur
 *   # commentaire
 * Lignes vides et commentaires sont ignorés.
 */
#ifndef LEO_CONFIG_H
#define LEO_CONFIG_H

#include "../include/leo_agent.h"

/**
 * Charge la configuration depuis le fichier agent.conf.
 * @param path   Chemin complet du fichier
 * @param out    Structure à remplir
 * @return LEO_OK ou LEO_ERR_CONFIG
 */
leo_error_t leo_config_load(const char *path, leo_config_t *out);

/**
 * Écrit la configuration dans agent.conf (mode "a+" tronqué avant écriture).
 * @return LEO_OK ou LEO_ERR_CONFIG
 */
leo_error_t leo_config_save(const char *path, const leo_config_t *cfg);

/**
 * Vérifie qu'une configuration minimale est valide :
 * agent_id, tenant_id et ws_endpoint non vides.
 * @return true si valide
 */
bool leo_config_is_valid(const leo_config_t *cfg);

#endif /* LEO_CONFIG_H */
