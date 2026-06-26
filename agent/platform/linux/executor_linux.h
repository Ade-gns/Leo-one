/**
 * executor_linux.h — Interface d'exécution de scripts sous Linux
 *
 * Permet d'exécuter un script via un interpréteur whitelisté
 * (bash, sh, python3, python, python2) dans un processus enfant isolé,
 * avec timeout et capture de stdout/stderr.
 */
#ifndef LEO_EXECUTOR_LINUX_H
#define LEO_EXECUTOR_LINUX_H

#include "../../include/leo_agent.h"

#define LEO_EXEC_STDOUT_MAX  16384
#define LEO_EXEC_STDERR_MAX   4096

typedef struct {
    int  exit_code;
    char stdout_buf[LEO_EXEC_STDOUT_MAX];
    char stderr_buf[LEO_EXEC_STDERR_MAX];
} leo_exec_result_t;

/**
 * Exécute un script via l'interpréteur donné.
 * @param interpreter "bash", "sh", "python3", "python", "python2"
 * @param script      Contenu du script à exécuter
 * @param timeout_secs Timeout en secondes (0 = pas de timeout)
 * @param result       Résultat à remplir (alloué par l'appelant)
 * @return LEO_OK en cas de succès, LEO_ERR_PROTOCOL si interpréteur non autorisé,
 *         LEO_ERR_SYSTEM en cas d'erreur système, LEO_ERR_TIMEOUT si timeout
 */
leo_error_t leo_exec_script(const char *interpreter,
                             const char *script,
                             int         timeout_secs,
                             leo_exec_result_t *result);

#endif /* LEO_EXECUTOR_LINUX_H */
