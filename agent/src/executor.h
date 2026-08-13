/**
 * executor.h — Interface générique d'exécution de scripts, implémentée par
 * chaque module platform/ :
 *   platform/linux/executor_linux.c
 *   platform/windows/executor_win.c
 *   platform/macos/executor_macos.c
 *
 * Le code appelant (agent.c) n'a jamais connaissance de la plateforme.
 */
#ifndef LEO_EXECUTOR_H
#define LEO_EXECUTOR_H

#include "../include/leo_agent.h"

#define LEO_EXEC_STDOUT_MAX  16384
#define LEO_EXEC_STDERR_MAX   4096

typedef struct {
    int  exit_code;
    char stdout_buf[LEO_EXEC_STDOUT_MAX];
    char stderr_buf[LEO_EXEC_STDERR_MAX];
} leo_exec_result_t;

/**
 * Exécute un script via l'interpréteur donné, dans un processus isolé.
 * @param interpreter Nom de l'interpréteur (whitelisté par l'implémentation
 *                    platform/ — ex: "bash", "sh", "python3")
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

/**
 * Exécute directement argv[0] avec les arguments argv[1..] via fork()+execvp(),
 * sans passer par un shell ni un fichier temporaire. À utiliser pour toute
 * commande STRUCTURÉE construite par l'agent lui-même à partir de paramètres
 * déjà validés (ex: INSTALL_PKG, REBOOT) — contrairement à leo_exec_script(),
 * il n'y a ici aucune surface d'injection shell à défendre : argv est passé
 * tel quel à execvp(), jamais interprété par /bin/sh.
 * @param argv        Tableau terminé par NULL ; argv[0] est le binaire
 *                     (résolu via $PATH, comme execvp).
 * @param extra_env   Tableau de chaînes "CLE=valeur" terminé par NULL, à
 *                     ajouter à l'environnement du processus enfant
 *                     uniquement (peut être NULL).
 * @param timeout_secs Timeout en secondes (0 = pas de timeout)
 * @param result       Résultat à remplir (alloué par l'appelant)
 * @return LEO_OK, LEO_ERR_SYSTEM, ou LEO_ERR_TIMEOUT. Jamais LEO_ERR_PROTOCOL :
 *         il n'y a pas de whitelist ici, argv[0] est exécuté tel quel — c'est
 *         à l'appelant de ne construire argv qu'à partir de données validées.
 */
leo_error_t leo_exec_argv(char *const argv[],
                           const char *const extra_env[],
                           int timeout_secs,
                           leo_exec_result_t *result);

#endif /* LEO_EXECUTOR_H */
