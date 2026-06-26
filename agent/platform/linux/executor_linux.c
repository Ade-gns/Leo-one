/**
 * executor_linux.c — Exécution sécurisée de scripts sous Linux
 *
 * Implémente leo_exec_script() via fork()+execvp() :
 *  1. Écrit le script dans un fichier temporaire mkstemp
 *  2. Crée deux pipes (stdout, stderr)
 *  3. fork() : l'enfant redirige stdout/stderr et exec() l'interpréteur
 *  4. Le parent lit les pipes avec select() et gère le timeout
 *  5. Si timeout : SIGKILL sur l'enfant
 *  6. Récupère le code de sortie via waitpid()
 *  7. Nettoie le fichier temporaire
 *
 * Sécurité :
 *  - L'interpréteur est validé contre une whitelist (pas d'injection de commande)
 *  - On n'utilise jamais system() pour exécuter le script utilisateur
 *  - Le fichier temporaire a les droits 0700 (exécutable uniquement par l'owner)
 */
#include "executor_linux.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <signal.h>
#include <errno.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/select.h>

/* ─── Constantes internes ────────────────────────────────────────────────── */

/** Interpréteurs autorisés. Tout autre valeur → LEO_ERR_PROTOCOL. */
static const char *ALLOWED_INTERPRETERS[] = {
    "bash", "sh", "python3", "python", "python2", NULL
};

/** Préfixe du fichier temporaire dans /tmp. */
#define TMPFILE_TEMPLATE  "/tmp/leo_exec_XXXXXX"

/* ─── Helpers privés ─────────────────────────────────────────────────────── */

/**
 * Vérifie que l'interpréteur est dans la whitelist.
 * @return true si autorisé, false sinon.
 */
static bool _interpreter_allowed(const char *interp) {
    if (!interp) return false;
    for (int i = 0; ALLOWED_INTERPRETERS[i]; i++) {
        if (strcmp(interp, ALLOWED_INTERPRETERS[i]) == 0)
            return true;
    }
    return false;
}

/**
 * Écrit le contenu du script dans un fichier temporaire et le rend exécutable.
 * @param script    Contenu textuel du script
 * @param path_out  Buffer de taille au moins sizeof(TMPFILE_TEMPLATE)+1
 * @return fd du fichier ouvert (à fermer par l'appelant), ou -1 en cas d'erreur
 */
static int _write_script_tmp(const char *script, char *path_out) {
    /* mkstemp crée le fichier avec droits 0600 et retourne un fd ouvert */
    snprintf(path_out, sizeof(TMPFILE_TEMPLATE) + 1, "%s", TMPFILE_TEMPLATE);
    int fd = mkstemp(path_out);
    if (fd < 0) {
        LOG_ERROR("mkstemp échoué : %s", strerror(errno));
        return -1;
    }

    /* Rend le fichier exécutable (owner uniquement) */
    if (fchmod(fd, 0700) != 0) {
        LOG_WARN("fchmod 0700 échoué sur %s : %s", path_out, strerror(errno));
    }

    size_t len = strlen(script);
    ssize_t written = write(fd, script, len);
    if (written < 0 || (size_t)written != len) {
        LOG_ERROR("Écriture du script dans %s échouée : %s", path_out, strerror(errno));
        close(fd);
        unlink(path_out);
        return -1;
    }

    /* Repositionne en début de fichier pour que l'interpréteur puisse le lire */
    lseek(fd, 0, SEEK_SET);
    return fd;
}

/**
 * Lit jusqu'à max_bytes depuis fd et accumule dans buf (déjà partiellement rempli).
 * @param buf       Buffer destination
 * @param buf_max   Taille totale du buffer (octets)
 * @param offset    Pointeur sur le nombre d'octets déjà écrits dans buf
 */
static void _drain_fd(int fd, char *buf, size_t buf_max, size_t *offset) {
    if (*offset >= buf_max - 1) return;  /* Buffer plein */

    ssize_t n = read(fd, buf + *offset, buf_max - 1 - *offset);
    if (n > 0)
        *offset += (size_t)n;
}

/* ─── API publique ───────────────────────────────────────────────────────── */

leo_error_t leo_exec_script(const char *interpreter,
                             const char *script,
                             int         timeout_secs,
                             leo_exec_result_t *result)
{
    if (!interpreter || !script || !result)
        return LEO_ERR_SYSTEM;

    /* Validation de l'interpréteur */
    if (!_interpreter_allowed(interpreter)) {
        LOG_ERROR("Interpréteur non autorisé : '%s'", interpreter);
        return LEO_ERR_PROTOCOL;
    }

    /* Initialiser le résultat */
    memset(result, 0, sizeof(*result));
    result->exit_code = -1;

    /* Écrire le script dans un fichier temporaire */
    char tmppath[64];
    int  script_fd = _write_script_tmp(script, tmppath);
    if (script_fd < 0)
        return LEO_ERR_SYSTEM;
    close(script_fd);  /* L'enfant ouvrira le fichier via son chemin */

    /* Créer les deux pipes : [0]=lecture parent, [1]=écriture enfant */
    int stdout_pipe[2];
    int stderr_pipe[2];
    if (pipe(stdout_pipe) != 0 || pipe(stderr_pipe) != 0) {
        LOG_ERROR("pipe() échoué : %s", strerror(errno));
        unlink(tmppath);
        if (stdout_pipe[0] >= 0) { close(stdout_pipe[0]); close(stdout_pipe[1]); }
        return LEO_ERR_SYSTEM;
    }

    /* Mettre le côté lecture des pipes en non-bloquant */
    fcntl(stdout_pipe[0], F_SETFL, O_NONBLOCK);
    fcntl(stderr_pipe[0], F_SETFL, O_NONBLOCK);

    pid_t child = fork();
    if (child < 0) {
        LOG_ERROR("fork() échoué : %s", strerror(errno));
        close(stdout_pipe[0]); close(stdout_pipe[1]);
        close(stderr_pipe[0]); close(stderr_pipe[1]);
        unlink(tmppath);
        return LEO_ERR_SYSTEM;
    }

    if (child == 0) {
        /* ── Processus enfant ── */

        /* Rediriger stdout → stdout_pipe[1] */
        dup2(stdout_pipe[1], STDOUT_FILENO);
        close(stdout_pipe[0]);
        close(stdout_pipe[1]);

        /* Rediriger stderr → stderr_pipe[1] */
        dup2(stderr_pipe[1], STDERR_FILENO);
        close(stderr_pipe[0]);
        close(stderr_pipe[1]);

        /* Fermer stdin */
        int devnull = open("/dev/null", O_RDONLY);
        if (devnull >= 0) {
            dup2(devnull, STDIN_FILENO);
            close(devnull);
        }

        /* Exécuter : {interpreter} {tmppath} */
        char *argv[] = { (char *)interpreter, tmppath, NULL };
        execvp(interpreter, argv);

        /* Si execvp retourne, c'est une erreur */
        _exit(127);
    }

    /* ── Processus parent ── */

    /* Fermer les extrémités d'écriture côté parent */
    close(stdout_pipe[1]);
    close(stderr_pipe[1]);

    size_t stdout_off = 0;
    size_t stderr_off = 0;
    int    child_done = 0;
    leo_error_t ret   = LEO_OK;

    /* Boucle de lecture avec select() et timeout */
    while (!child_done) {
        fd_set rfds;
        FD_ZERO(&rfds);
        FD_SET(stdout_pipe[0], &rfds);
        FD_SET(stderr_pipe[0], &rfds);
        int nfds = (stdout_pipe[0] > stderr_pipe[0] ? stdout_pipe[0] : stderr_pipe[0]) + 1;

        struct timeval tv;
        struct timeval *tvp = NULL;
        if (timeout_secs > 0) {
            tv.tv_sec  = (long)timeout_secs;
            tv.tv_usec = 0;
            tvp = &tv;
        }

        int sel = select(nfds, &rfds, NULL, NULL, tvp);

        if (sel < 0) {
            if (errno == EINTR) continue;
            LOG_ERROR("select() échoué : %s", strerror(errno));
            ret = LEO_ERR_SYSTEM;
            break;
        }

        if (sel == 0) {
            /* Timeout : tuer l'enfant */
            LOG_WARN("Timeout d'exécution du script (%ds) — SIGKILL PID %d",
                     timeout_secs, (int)child);
            kill(child, SIGKILL);
            ret = LEO_ERR_TIMEOUT;
            break;
        }

        if (FD_ISSET(stdout_pipe[0], &rfds))
            _drain_fd(stdout_pipe[0], result->stdout_buf, LEO_EXEC_STDOUT_MAX, &stdout_off);

        if (FD_ISSET(stderr_pipe[0], &rfds))
            _drain_fd(stderr_pipe[0], result->stderr_buf, LEO_EXEC_STDERR_MAX, &stderr_off);

        /* Vérifier si l'enfant est terminé (non-bloquant) */
        int status;
        pid_t w = waitpid(child, &status, WNOHANG);
        if (w == child) {
            /* Drainer le reste des pipes avant de sortir */
            _drain_fd(stdout_pipe[0], result->stdout_buf, LEO_EXEC_STDOUT_MAX, &stdout_off);
            _drain_fd(stderr_pipe[0], result->stderr_buf, LEO_EXEC_STDERR_MAX, &stderr_off);

            if (WIFEXITED(status))
                result->exit_code = WEXITSTATUS(status);
            else if (WIFSIGNALED(status))
                result->exit_code = -(int)WTERMSIG(status);

            child_done = 1;
        }
    }

    /* Attente finale (bloquante) pour éviter les zombies */
    if (!child_done) {
        int status;
        waitpid(child, &status, 0);
        if (WIFEXITED(status))
            result->exit_code = WEXITSTATUS(status);
        else if (WIFSIGNALED(status))
            result->exit_code = -(int)WTERMSIG(status);
    }

    /* Null-terminer les buffers */
    result->stdout_buf[stdout_off < LEO_EXEC_STDOUT_MAX ? stdout_off : LEO_EXEC_STDOUT_MAX - 1] = '\0';
    result->stderr_buf[stderr_off < LEO_EXEC_STDERR_MAX ? stderr_off : LEO_EXEC_STDERR_MAX - 1] = '\0';

    /* Nettoyage */
    close(stdout_pipe[0]);
    close(stderr_pipe[0]);
    unlink(tmppath);

    LOG_DEBUG("Script exécuté via '%s' : exit_code=%d stdout=%zu stderr=%zu octets",
              interpreter, result->exit_code, stdout_off, stderr_off);

    return ret;
}
