/**
 * service_linux.h — Interface de gestion du service systemd de l'agent Leo-One
 *
 * Fournit l'installation, la désinstallation et l'interrogation du statut
 * du service systemd "leo-agent".
 */
#ifndef LEO_SERVICE_LINUX_H
#define LEO_SERVICE_LINUX_H

#include "../../include/leo_agent.h"

typedef enum {
    LEO_SERVICE_UNKNOWN  = 0,
    LEO_SERVICE_ACTIVE,
    LEO_SERVICE_INACTIVE,
    LEO_SERVICE_FAILED
} leo_service_status_t;

/**
 * Installe le fichier unit systemd dans /etc/systemd/system/leo-agent.service.
 * Nécessite les droits root (uid == 0).
 * @return LEO_OK si succès, LEO_ERR_SYSTEM si échec (droits insuffisants ou I/O)
 */
leo_error_t leo_service_install(void);

/**
 * Supprime le fichier unit systemd si présent.
 * @return LEO_OK si succès ou fichier absent, LEO_ERR_SYSTEM si échec I/O
 */
leo_error_t leo_service_uninstall(void);

/**
 * Interroge le statut du service via "systemctl is-active leo-agent".
 * @return LEO_SERVICE_ACTIVE / INACTIVE / FAILED / UNKNOWN
 */
leo_service_status_t leo_service_status(void);

#endif /* LEO_SERVICE_LINUX_H */
