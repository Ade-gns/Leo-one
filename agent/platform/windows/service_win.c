/**
 * service_win.c — Gestion du service Windows de l'agent Leo-One
 *
 * Installe, désinstalle, interroge le statut du service "leo-agent" auprès
 * du Service Control Manager (SCM), et fournit le point d'entrée du mode
 * service (ServiceMain) que main.c utilise via leo_service_run_dispatcher().
 *
 * Cycle de vie du mode service :
 *   main() → leo_service_run_dispatcher() → StartServiceCtrlDispatcherA()
 *     → (appelé par le SCM) _service_main()
 *       → RegisterServiceCtrlHandlerExA (reçoit SERVICE_CONTROL_STOP)
 *       → leo_agent_start()
 *       → attend g_stop_event (signalé par le handler de contrôle)
 *       → leo_agent_stop()
 *   StartServiceCtrlDispatcherA() ne retourne qu'une fois _service_main()
 *   revenue, c'est-à-dire à l'arrêt complet du service.
 */
#include "service_win.h"
#include "../../src/agent.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <string.h>

#include <windows.h>

#define LEO_SERVICE_NAME         "leo-agent"
#define LEO_SERVICE_DISPLAY_NAME "Leo-One RMM Agent"

/* ─── État du mode service (un seul service par processus) ───────────────── */

static SERVICE_STATUS_HANDLE g_status_handle = NULL;
static SERVICE_STATUS        g_status;
static HANDLE                g_stop_event    = NULL;
static leo_agent_t           *g_agent        = NULL;

static void _report_status(DWORD state, DWORD exit_code, DWORD wait_hint) {
    static DWORD check_point = 1;

    g_status.dwCurrentState = state;
    g_status.dwWin32ExitCode = exit_code;
    g_status.dwWaitHint = wait_hint;
    g_status.dwControlsAccepted = (state == SERVICE_START_PENDING)
        ? 0 : (SERVICE_ACCEPT_STOP | SERVICE_ACCEPT_SHUTDOWN);
    g_status.dwCheckPoint = (state == SERVICE_RUNNING || state == SERVICE_STOPPED)
        ? 0 : check_point++;

    SetServiceStatus(g_status_handle, &g_status);
}

static DWORD WINAPI _service_ctrl_handler(DWORD ctrl, DWORD event_type,
                                           LPVOID event_data, LPVOID context)
{
    (void)event_type; (void)event_data; (void)context;

    switch (ctrl) {
    case SERVICE_CONTROL_STOP:
    case SERVICE_CONTROL_SHUTDOWN:
        _report_status(SERVICE_STOP_PENDING, NO_ERROR, 3000);
        SetEvent(g_stop_event);
        return NO_ERROR;
    case SERVICE_CONTROL_INTERROGATE:
        return NO_ERROR;
    default:
        return ERROR_CALL_NOT_IMPLEMENTED;
    }
}

static void WINAPI _service_main(DWORD argc, LPSTR *argv) {
    /* Le SCM ne passe pas d'arguments personnalisés par défaut — argv[0] est
     * le nom du service. Repli sur LEO_CONFIG_FILE, comme main() en mode
     * console. */
    const char *config_path = (argc > 1) ? argv[1] : LEO_CONFIG_FILE;

    g_status_handle = RegisterServiceCtrlHandlerExA(LEO_SERVICE_NAME, _service_ctrl_handler, NULL);
    if (!g_status_handle) {
        LOG_FATAL("RegisterServiceCtrlHandlerExA échoué (code %lu)", GetLastError());
        return;
    }

    memset(&g_status, 0, sizeof(g_status));
    g_status.dwServiceType = SERVICE_WIN32_OWN_PROCESS;
    _report_status(SERVICE_START_PENDING, NO_ERROR, 3000);

    g_stop_event = CreateEventA(NULL, TRUE, FALSE, NULL);
    if (!g_stop_event) {
        LOG_FATAL("CreateEventA échoué (code %lu)", GetLastError());
        _report_status(SERVICE_STOPPED, ERROR_SERVICE_SPECIFIC_ERROR, 0);
        return;
    }

    g_agent = leo_agent_start(config_path);
    if (!g_agent) {
        LOG_FATAL("Impossible de démarrer l'agent — arrêt du service");
        _report_status(SERVICE_STOPPED, ERROR_SERVICE_SPECIFIC_ERROR, 0);
        CloseHandle(g_stop_event);
        g_stop_event = NULL;
        return;
    }

    _report_status(SERVICE_RUNNING, NO_ERROR, 0);
    LOG_INFO("Service Windows actif (PID=%lu)", GetCurrentProcessId());

    WaitForSingleObject(g_stop_event, INFINITE);

    LOG_INFO("Arrêt du service demandé — arrêt de l'agent…");
    leo_agent_stop(g_agent);
    g_agent = NULL;

    CloseHandle(g_stop_event);
    g_stop_event = NULL;

    _report_status(SERVICE_STOPPED, NO_ERROR, 0);
    LOG_INFO("Service Windows arrêté proprement");
}

/* ─── API publique ───────────────────────────────────────────────────────── */

bool leo_service_run_dispatcher(void) {
    SERVICE_TABLE_ENTRYA table[] = {
        { (LPSTR)LEO_SERVICE_NAME, _service_main },
        { NULL, NULL }
    };

    if (!StartServiceCtrlDispatcherA(table)) {
        if (GetLastError() == ERROR_FAILED_SERVICE_CONTROLLER_CONNECT) {
            /* Pas lancé par le SCM : exécution interactive normale, main()
             * doit basculer sur le mode console. */
            return false;
        }
        LOG_ERROR("StartServiceCtrlDispatcherA échoué (code %lu)", GetLastError());
        return false;
    }

    /* Ne revient ici qu'après l'arrêt complet du service (_service_main
     * terminée) : le cycle de vie entier vient de s'exécuter. */
    return true;
}

leo_error_t leo_service_install(void) {
    SC_HANDLE scm = OpenSCManagerA(NULL, NULL, SC_MANAGER_CREATE_SERVICE);
    if (!scm) {
        LOG_ERROR("OpenSCManagerA échoué (code %lu) — droits administrateur requis",
                  GetLastError());
        return LEO_ERR_SYSTEM;
    }

    char exe_path[MAX_PATH];
    if (!GetModuleFileNameA(NULL, exe_path, sizeof(exe_path))) {
        LOG_ERROR("GetModuleFileNameA échoué (code %lu)", GetLastError());
        CloseServiceHandle(scm);
        return LEO_ERR_SYSTEM;
    }

    SC_HANDLE svc = CreateServiceA(
        scm, LEO_SERVICE_NAME, LEO_SERVICE_DISPLAY_NAME,
        SERVICE_ALL_ACCESS, SERVICE_WIN32_OWN_PROCESS,
        SERVICE_AUTO_START, SERVICE_ERROR_NORMAL,
        exe_path, NULL, NULL, NULL, NULL, NULL);

    if (!svc) {
        DWORD err = GetLastError();
        CloseServiceHandle(scm);
        if (err == ERROR_SERVICE_EXISTS) {
            LOG_INFO("Service '%s' déjà installé", LEO_SERVICE_NAME);
            return LEO_OK;
        }
        LOG_ERROR("CreateServiceA échoué (code %lu)", err);
        return LEO_ERR_SYSTEM;
    }

    /* Description (cosmétique, best-effort) + démarrage automatique différé
     * : évite de concurrencer l'initialisation réseau au boot — l'agent se
     * reconnectera de lui-même si le réseau n'est pas encore prêt (voir la
     * boucle de reconnexion dans connection.c). */
    SERVICE_DESCRIPTIONA desc = { (LPSTR)"Agent de supervision et gestion a distance Leo-One" };
    ChangeServiceConfig2A(svc, SERVICE_CONFIG_DESCRIPTION, &desc);

    SERVICE_DELAYED_AUTO_START_INFO delayed = { TRUE };
    ChangeServiceConfig2A(svc, SERVICE_CONFIG_DELAYED_AUTO_START_INFO, &delayed);

    CloseServiceHandle(svc);
    CloseServiceHandle(scm);

    LOG_INFO("Service Windows installé : %s", LEO_SERVICE_NAME);
    LOG_INFO("Lancer 'sc start %s' ou redémarrer pour l'activer", LEO_SERVICE_NAME);
    return LEO_OK;
}

leo_error_t leo_service_uninstall(void) {
    SC_HANDLE scm = OpenSCManagerA(NULL, NULL, SC_MANAGER_CONNECT);
    if (!scm) {
        LOG_ERROR("OpenSCManagerA échoué (code %lu)", GetLastError());
        return LEO_ERR_SYSTEM;
    }

    SC_HANDLE svc = OpenServiceA(scm, LEO_SERVICE_NAME,
                                  DELETE | SERVICE_STOP | SERVICE_QUERY_STATUS);
    if (!svc) {
        DWORD err = GetLastError();
        CloseServiceHandle(scm);
        if (err == ERROR_SERVICE_DOES_NOT_EXIST) {
            LOG_INFO("Service Windows non présent, rien à désinstaller");
            return LEO_OK;
        }
        LOG_ERROR("OpenServiceA échoué (code %lu)", err);
        return LEO_ERR_SYSTEM;
    }

    /* Best-effort : on tente d'arrêter le service avant suppression, mais on
     * n'échoue pas la désinstallation si le service était déjà arrêté ou ne
     * répond pas — DeleteService marquera le service pour suppression dès
     * son prochain arrêt de toute façon. */
    SERVICE_STATUS status;
    ControlService(svc, SERVICE_CONTROL_STOP, &status);

    bool ok = DeleteService(svc);
    DWORD err = GetLastError();
    CloseServiceHandle(svc);
    CloseServiceHandle(scm);

    if (!ok) {
        LOG_ERROR("DeleteService échoué (code %lu)", err);
        return LEO_ERR_SYSTEM;
    }

    LOG_INFO("Service Windows supprimé : %s", LEO_SERVICE_NAME);
    return LEO_OK;
}

leo_service_status_t leo_service_status(void) {
    SC_HANDLE scm = OpenSCManagerA(NULL, NULL, SC_MANAGER_CONNECT);
    if (!scm) {
        LOG_WARN("leo_service_status : OpenSCManagerA échoué (code %lu)", GetLastError());
        return LEO_SERVICE_UNKNOWN;
    }

    SC_HANDLE svc = OpenServiceA(scm, LEO_SERVICE_NAME, SERVICE_QUERY_STATUS);
    if (!svc) {
        CloseServiceHandle(scm);
        return LEO_SERVICE_UNKNOWN;
    }

    leo_service_status_t result = LEO_SERVICE_UNKNOWN;
    SERVICE_STATUS_PROCESS ssp;
    DWORD needed = 0;
    if (QueryServiceStatusEx(svc, SC_STATUS_PROCESS_INFO, (LPBYTE)&ssp, sizeof(ssp), &needed)) {
        switch (ssp.dwCurrentState) {
        case SERVICE_RUNNING:
            result = LEO_SERVICE_ACTIVE;
            break;
        case SERVICE_STOPPED:
            result = (ssp.dwWin32ExitCode != NO_ERROR) ? LEO_SERVICE_FAILED : LEO_SERVICE_INACTIVE;
            break;
        default:
            result = LEO_SERVICE_UNKNOWN;
            break;
        }
    }

    CloseServiceHandle(svc);
    CloseServiceHandle(scm);
    return result;
}
