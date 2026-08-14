/**
 * service_win.h — Interface de gestion du service Windows de l'agent Leo-One
 *
 * Fournit l'installation, la désinstallation et l'interrogation du statut
 * du service Windows "leo-agent" (Service Control Manager), plus le point
 * d'entrée du mode service utilisé par main.c. Miroir fonctionnel de
 * platform/linux/service_linux.h (même enum leo_service_status_t).
 */
#ifndef LEO_SERVICE_WIN_H
#define LEO_SERVICE_WIN_H

#include "../../include/leo_agent.h"

typedef enum {
    LEO_SERVICE_UNKNOWN  = 0,
    LEO_SERVICE_ACTIVE,
    LEO_SERVICE_INACTIVE,
    LEO_SERVICE_FAILED
} leo_service_status_t;

/**
 * Installe le service Windows "leo-agent" (SERVICE_AUTO_START, démarrage
 * différé). Nécessite les droits administrateur.
 * @return LEO_OK si succès (ou déjà installé), LEO_ERR_SYSTEM si échec
 */
leo_error_t leo_service_install(void);

/**
 * Arrête (best-effort) puis supprime le service Windows s'il existe.
 * @return LEO_OK si succès ou service absent, LEO_ERR_SYSTEM si échec
 */
leo_error_t leo_service_uninstall(void);

/**
 * Interroge le statut du service via QueryServiceStatusEx.
 * @return LEO_SERVICE_ACTIVE / INACTIVE / FAILED / UNKNOWN
 */
leo_service_status_t leo_service_status(void);

/**
 * Tente de s'enregistrer auprès du Service Control Manager
 * (StartServiceCtrlDispatcherA) et, si le processus a bien été lancé par
 * lui, exécute l'agent comme service Windows — bloquant pour toute la durée
 * de vie du service, StartServiceCtrlDispatcherA ne retourne qu'à l'arrêt.
 * @return true si le service a tourné puis s'est arrêté normalement (main()
 *         doit alors simplement quitter) ; false si le processus n'a PAS été
 *         lancé par le SCM (ERROR_FAILED_SERVICE_CONTROLLER_CONNECT) — dans
 *         ce cas main() doit basculer sur le mode console interactif.
 */
bool leo_service_run_dispatcher(void);

#endif /* LEO_SERVICE_WIN_H */
