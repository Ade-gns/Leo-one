/**
 * agent.h — Orchestrateur principal de l'agent Leo-One
 *
 * Gère les threads de heartbeat et métriques, dispatch des commandes entrantes,
 * et la machine d'état de connexion.
 */
#ifndef LEO_AGENT_CORE_H
#define LEO_AGENT_CORE_H

#include "../include/leo_agent.h"

/** Opaque handle de l'agent. */
typedef struct leo_agent leo_agent_t;

/**
 * Crée et démarre l'agent complet (connexion + threads de collecte).
 * @param config_path  Chemin vers agent.conf
 * @return Handle alloué, NULL si erreur fatale
 */
leo_agent_t *leo_agent_start(const char *config_path);

/**
 * Demande l'arrêt propre de l'agent.
 * Bloquant : attend que tous les threads soient terminés.
 */
void leo_agent_stop(leo_agent_t *agent);

/**
 * Retourne l'état courant de l'agent.
 * Thread-safe (lecture volatile).
 */
leo_agent_state_t leo_agent_get_state(const leo_agent_t *agent);

#endif /* LEO_AGENT_CORE_H */
