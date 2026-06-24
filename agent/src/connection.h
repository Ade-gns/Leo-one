/**
 * connection.h — Gestion de la connexion WSS persistante (libwebsockets)
 *
 * Architecture threading :
 *   - Un thread dédié exécute la boucle événementielle libwebsockets.
 *   - Les autres threads (heartbeat, métriques) appellent leo_conn_send()
 *     pour enqueuer des messages de façon thread-safe.
 *   - Le thread LWS est notifié via lws_cancel_service() et vide la file
 *     dans le callback LWS_CALLBACK_CLIENT_WRITEABLE.
 */
#ifndef LEO_CONNECTION_H
#define LEO_CONNECTION_H

#include "../include/leo_agent.h"

/** Opaque handle retourné par leo_conn_create(). */
typedef struct leo_conn leo_conn_t;

/** Callback appelé par la connexion quand un message est reçu du backend. */
typedef void (*leo_msg_handler_t)(const char *json_str, size_t len, void *userdata);

/**
 * Crée et démarre la connexion WSS.
 * Lance un thread dédié pour la boucle événementielle.
 *
 * @param cfg      Configuration de l'agent (ws_endpoint, ca_fingerprint, certs)
 * @param handler  Callback appelé à chaque message entrant
 * @param userdata Pointeur utilisateur passé au callback
 * @return Pointeur alloué sur le handle de connexion, NULL si erreur
 */
leo_conn_t *leo_conn_create(const leo_config_t *cfg,
                            leo_msg_handler_t   handler,
                            void               *userdata);

/**
 * Enqueue un message JSON à envoyer au backend.
 * Thread-safe. Non bloquant (retourne LEO_ERR_QUEUE_FULL si la file est pleine).
 *
 * @param conn    Handle de connexion
 * @param data    Buffer JSON (n'a pas besoin d'être persistant après l'appel)
 * @param len     Longueur du buffer
 * @return LEO_OK, LEO_ERR_QUEUE_FULL, ou LEO_ERR_NETWORK si déconnecté
 */
leo_error_t leo_conn_send(leo_conn_t *conn, const char *data, size_t len);

/**
 * Retourne true si la connexion WSS est établie et active.
 * Thread-safe (lecture volatile).
 */
bool leo_conn_is_connected(const leo_conn_t *conn);

/**
 * Demande l'arrêt propre de la connexion et libère les ressources.
 * Bloquant : attend que le thread LWS se termine (max 5s).
 */
void leo_conn_destroy(leo_conn_t *conn);

#endif /* LEO_CONNECTION_H */
