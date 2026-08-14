/**
 * executor_win.c — Exécution sécurisée de scripts sous Windows
 *
 * Implémente leo_exec_script()/leo_exec_argv() via CreateProcessA() :
 *  1. (leo_exec_script) écrit le script dans un fichier temporaire avec
 *     l'extension attendue par l'interpréteur (.bat pour cmd, .ps1 pour
 *     powershell)
 *  2. Construit une ligne de commande unique correctement échappée à partir
 *     d'argv (règles de quoting CreateProcess/CommandLineToArgvW — voir
 *     _append_quoted_arg)
 *  3. CreatePipe() pour stdout/stderr, deux threads lecteurs dédiés
 *     (nécessaires sous Windows : lire seulement après la fin du process
 *     provoquerait un blocage réciproque si l'enfant remplit le buffer du
 *     pipe avant de se terminer — contrairement à select() côté Linux, il
 *     n'y a pas d'attente non-bloquante native sur un HANDLE de pipe
 *     anonyme ET sur la fin de process en une seule primitive)
 *  4. WaitForSingleObject() avec timeout, TerminateProcess() si dépassé
 *  5. GetExitCodeProcess() pour le code de sortie
 *
 * Sécurité :
 *  - L'interpréteur est validé contre une whitelist (pas d'injection de commande)
 *  - La ligne de commande est construite par échappement correct par
 *    argument, jamais par concaténation naïve (évite l'injection d'argument)
 */
#include "executor_win.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>
#include <errno.h>

#include <windows.h>
#include <pthread.h>

/* ─── Constantes internes ────────────────────────────────────────────────── */

/** Interpréteurs autorisés. Tout autre valeur → LEO_ERR_PROTOCOL. */
static const char *ALLOWED_INTERPRETERS[] = { "cmd", "powershell", NULL };

/** Taille max d'une ligne de commande Windows (limite CreateProcess ~32767). */
#define LEO_EXEC_CMDLINE_MAX 32768

/* ─── Helpers privés : whitelist et fichier temporaire ──────────────────── */

static bool _interpreter_allowed(const char *interp) {
    if (!interp) return false;
    for (int i = 0; ALLOWED_INTERPRETERS[i]; i++) {
        if (strcmp(interp, ALLOWED_INTERPRETERS[i]) == 0)
            return true;
    }
    return false;
}

/**
 * Écrit le contenu du script dans un fichier temporaire unique, avec
 * l'extension attendue par l'interpréteur (nécessaire : cmd.exe n'exécute
 * un fichier via "/c" que s'il porte une extension reconnue comme .bat/.cmd,
 * et powershell.exe -File exige .ps1).
 * @param path_out Buffer de taille >= MAX_PATH
 * @return true si succès, false sinon
 */
static bool _write_script_tmp(const char *script, const char *ext, char *path_out, size_t path_out_sz) {
    char dir[MAX_PATH];
    DWORD dir_len = GetTempPathA(sizeof(dir), dir);
    if (dir_len == 0 || dir_len >= sizeof(dir)) {
        LOG_ERROR("GetTempPathA échoué (code %lu)", GetLastError());
        return false;
    }

    /* PID + compteur monotone + tick pour une unicité suffisante sans
     * dépendance à une primitive type mkstemp (absente sous Windows). */
    static volatile LONG counter = 0;
    LONG n = InterlockedIncrement(&counter);
    snprintf(path_out, path_out_sz, "%sleo_exec_%08lx_%04lx_%08lx%s",
             dir, (unsigned long)GetCurrentProcessId(),
             (unsigned long)n, (unsigned long)GetTickCount(), ext);

    FILE *fp = fopen(path_out, "wb");
    if (!fp) {
        LOG_ERROR("Impossible de créer '%s' : %s", path_out, strerror(errno));
        return false;
    }

    size_t len     = strlen(script);
    size_t written = fwrite(script, 1, len, fp);
    fclose(fp);

    if (written != len) {
        LOG_ERROR("Écriture du script dans '%s' incomplète (%zu/%zu octets)",
                  path_out, written, len);
        DeleteFileA(path_out);
        return false;
    }

    return true;
}

/* ─── Helpers privés : construction de la ligne de commande ─────────────── */

/**
 * Ajoute argv[i] (échappé si nécessaire) à la ligne de commande en cours de
 * construction, en respectant les règles de parsing de CommandLineToArgvW /
 * du runtime C Microsoft : un argument est entre guillemets s'il contient un
 * espace, une tabulation, un guillemet, ou s'il est vide ; les backslashes
 * ne sont doublés que s'ils précèdent immédiatement un guillemet (littéral
 * ou celui de fermeture).
 */
static void _append_quoted_arg(char *dest, size_t dest_sz, size_t *pos, const char *arg) {
    bool needs_quotes = (arg[0] == '\0');
    for (const char *p = arg; *p && !needs_quotes; p++) {
        if (*p == ' ' || *p == '\t' || *p == '"') needs_quotes = true;
    }

    if (*pos > 0 && *pos < dest_sz) dest[(*pos)++] = ' ';

    if (!needs_quotes) {
        size_t l = strlen(arg);
        if (*pos + l < dest_sz) { memcpy(dest + *pos, arg, l); *pos += l; }
        else                    { *pos = dest_sz; }  /* force l'échec de _build_cmdline */
        return;
    }

    if (*pos < dest_sz) dest[(*pos)++] = '"';

    size_t backslashes = 0;
    for (const char *p = arg; *p; p++) {
        if (*p == '\\') {
            backslashes++;
            continue;
        }
        if (*p == '"') {
            for (size_t i = 0; i < backslashes * 2 + 1 && *pos < dest_sz; i++) dest[(*pos)++] = '\\';
            if (*pos < dest_sz) dest[(*pos)++] = '"';
        } else {
            for (size_t i = 0; i < backslashes && *pos < dest_sz; i++) dest[(*pos)++] = '\\';
            if (*pos < dest_sz) dest[(*pos)++] = *p;
        }
        backslashes = 0;
    }
    /* Backslashes en fin d'argument, juste avant le guillemet fermant :
     * doublées elles aussi (sinon elles échapperaient ce guillemet). */
    for (size_t i = 0; i < backslashes * 2 && *pos < dest_sz; i++) dest[(*pos)++] = '\\';
    if (*pos < dest_sz) dest[(*pos)++] = '"';
}

static bool _build_cmdline(char *dest, size_t dest_sz, char *const argv[]) {
    size_t pos = 0;
    for (int i = 0; argv[i]; i++) {
        _append_quoted_arg(dest, dest_sz, &pos, argv[i]);
    }
    if (pos >= dest_sz) {
        LOG_ERROR("Ligne de commande trop longue (> %zu octets)", dest_sz);
        return false;
    }
    dest[pos] = '\0';
    return true;
}

/* ─── Helpers privés : environnement enfant ──────────────────────────────── */

/**
 * Construit un bloc d'environnement = environnement courant du process +
 * extra_env, au format attendu par CreateProcess (séquence de "CLE=valeur\0"
 * terminée par un octet nul supplémentaire). Alloué par malloc — libéré par
 * l'appelant avec free() (PAS FreeEnvironmentStringsA, qui ne s'applique
 * qu'au bloc retourné par GetEnvironmentStringsA lui-même).
 * @return bloc alloué, ou NULL si extra_env est vide/NULL et qu'on peut se
 *         contenter d'hériter l'environnement (CreateProcess avec
 *         lpEnvironment=NULL hérite de l'environnement de l'appelant).
 */
static char *_build_env_block(const char *const extra_env[]) {
    if (!extra_env || !extra_env[0]) return NULL;

    char *base = GetEnvironmentStringsA();
    if (!base) {
        LOG_WARN("GetEnvironmentStringsA échoué — extra_env ignoré, héritage standard");
        return NULL;
    }

    /* Le bloc existant est terminé par deux octets nuls consécutifs. */
    size_t i = 0;
    while (!(base[i] == '\0' && base[i + 1] == '\0')) i++;
    size_t base_len = i + 1;  /* toutes les chaînes, chacune \0-terminée, sans le \0 de fin de bloc */

    size_t extra_len = 0;
    for (int j = 0; extra_env[j]; j++) extra_len += strlen(extra_env[j]) + 1;

    char *block = malloc(base_len + extra_len + 1);
    if (!block) {
        FreeEnvironmentStringsA(base);
        return NULL;
    }

    memcpy(block, base, base_len);
    FreeEnvironmentStringsA(base);

    size_t off = base_len;
    for (int j = 0; extra_env[j]; j++) {
        size_t l = strlen(extra_env[j]) + 1;
        memcpy(block + off, extra_env[j], l);
        off += l;
    }
    block[off] = '\0';
    return block;
}

/* ─── Helpers privés : lecture non-bloquante des pipes ───────────────────── */

typedef struct {
    HANDLE pipe_read;
    char  *buf;
    size_t buf_max;
    size_t off;
} _reader_ctx_t;

/**
 * Lit pipe_read jusqu'à EOF (fermeture de tous les côtés écriture, donc à la
 * fin du process enfant) et accumule dans buf, en tronquant silencieusement
 * au-delà de buf_max (comme _drain_fd côté Linux) — mais continue de lire
 * pour ne jamais bloquer l'écriture de l'enfant même une fois le buffer plein.
 */
static void *_reader_thread(void *arg) {
    _reader_ctx_t *ctx = (_reader_ctx_t *)arg;
    char chunk[4096];
    DWORD n;

    while (ReadFile(ctx->pipe_read, chunk, sizeof(chunk), &n, NULL) && n > 0) {
        if (ctx->off < ctx->buf_max - 1) {
            size_t space = ctx->buf_max - 1 - ctx->off;
            size_t copy  = ((size_t)n < space) ? (size_t)n : space;
            memcpy(ctx->buf + ctx->off, chunk, copy);
            ctx->off += copy;
        }
    }
    return NULL;
}

/* ─── Cœur partagé : CreateProcess + pipes + timeout ─────────────────────── */

/**
 * Exécute la ligne de commande donnée dans un processus enfant dont
 * stdout/stderr sont capturés via pipes anonymes. Partagé par
 * leo_exec_script() et leo_exec_argv() — seule la construction de la ligne
 * de commande (fichier temporaire + whitelist, ou argv fourni tel quel)
 * diffère entre les deux appelants.
 */
static leo_error_t _run_child(char *cmdline, const char *const extra_env[],
                               int timeout_secs, leo_exec_result_t *result)
{
    memset(result, 0, sizeof(*result));
    result->exit_code = -1;

    SECURITY_ATTRIBUTES sa;
    sa.nLength              = sizeof(sa);
    sa.lpSecurityDescriptor  = NULL;
    sa.bInheritHandle        = TRUE;

    HANDLE stdout_r = NULL, stdout_w = NULL;
    HANDLE stderr_r = NULL, stderr_w = NULL;
    if (!CreatePipe(&stdout_r, &stdout_w, &sa, 0)) {
        LOG_ERROR("CreatePipe(stdout) échoué (code %lu)", GetLastError());
        return LEO_ERR_SYSTEM;
    }
    if (!CreatePipe(&stderr_r, &stderr_w, &sa, 0)) {
        LOG_ERROR("CreatePipe(stderr) échoué (code %lu)", GetLastError());
        CloseHandle(stdout_r);
        CloseHandle(stdout_w);
        return LEO_ERR_SYSTEM;
    }

    /* Seul le côté écriture doit être hérité par l'enfant — le côté lecture
     * ne doit surtout pas l'être (sinon la copie héritée dans l'enfant garde
     * le pipe "ouvert en écriture" ouvert de son point de vue, et le parent
     * ne verrait jamais EOF après la fin de l'enfant). */
    SetHandleInformation(stdout_r, HANDLE_FLAG_INHERIT, 0);
    SetHandleInformation(stderr_r, HANDLE_FLAG_INHERIT, 0);

    /* Entrée standard explicitement redirigée vers NUL (équivalent du
     * dup2(/dev/null) côté Linux) plutôt que hStdInput=NULL, dont le
     * comportement de repli (hérite ou non de la console de l'appelant)
     * n'est pas garanti selon les versions de Windows. */
    HANDLE stdin_null = CreateFileA("NUL", GENERIC_READ, FILE_SHARE_READ,
                                     &sa, OPEN_EXISTING, 0, NULL);
    if (stdin_null == INVALID_HANDLE_VALUE) stdin_null = NULL;  /* repli acceptable */

    char *env_block = _build_env_block(extra_env);

    STARTUPINFOA si;
    memset(&si, 0, sizeof(si));
    si.cb         = sizeof(si);
    si.dwFlags    = STARTF_USESTDHANDLES;
    si.hStdOutput = stdout_w;
    si.hStdError  = stderr_w;
    si.hStdInput  = stdin_null;

    PROCESS_INFORMATION pi;
    memset(&pi, 0, sizeof(pi));

    BOOL ok = CreateProcessA(
        NULL,               /* lpApplicationName : résolu depuis cmdline (comme execvp+$PATH) */
        cmdline,
        NULL, NULL,
        TRUE,               /* bInheritHandles : nécessaire pour stdout_w/stderr_w */
        CREATE_NO_WINDOW,   /* pas de fenêtre console visible pour un script piloté */
        env_block,
        NULL,
        &si, &pi);

    /* Le parent n'a plus besoin de ses copies des côtés écriture — les
     * fermer ici est ce qui permet à ReadFile() de voir EOF quand l'enfant
     * (seul détenteur restant) se termine. */
    CloseHandle(stdout_w);
    CloseHandle(stderr_w);
    if (stdin_null) CloseHandle(stdin_null);
    free(env_block);

    if (!ok) {
        LOG_ERROR("CreateProcessA échoué (code %lu) : %s", GetLastError(), cmdline);
        CloseHandle(stdout_r);
        CloseHandle(stderr_r);
        return LEO_ERR_SYSTEM;
    }
    CloseHandle(pi.hThread);

    /* Threads lecteurs : voir le commentaire sur _reader_thread — nécessaire
     * pour ne jamais bloquer l'enfant pendant qu'on attend sa fin. */
    _reader_ctx_t out_ctx = { stdout_r, result->stdout_buf, LEO_EXEC_STDOUT_MAX, 0 };
    _reader_ctx_t err_ctx = { stderr_r, result->stderr_buf, LEO_EXEC_STDERR_MAX, 0 };
    pthread_t out_thread, err_thread;
    bool out_thread_ok = (pthread_create(&out_thread, NULL, _reader_thread, &out_ctx) == 0);
    bool err_thread_ok = (pthread_create(&err_thread, NULL, _reader_thread, &err_ctx) == 0);
    if (!out_thread_ok) LOG_WARN("pthread_create (lecteur stdout) échoué — stdout non capturé");
    if (!err_thread_ok) LOG_WARN("pthread_create (lecteur stderr) échoué — stderr non capturé");

    DWORD wait_ms = (timeout_secs > 0) ? (DWORD)timeout_secs * 1000 : INFINITE;
    DWORD wait_rc = WaitForSingleObject(pi.hProcess, wait_ms);

    leo_error_t ret = LEO_OK;
    if (wait_rc == WAIT_TIMEOUT) {
        LOG_WARN("Timeout d'exécution (%ds) — TerminateProcess PID %lu",
                 timeout_secs, pi.dwProcessId);
        TerminateProcess(pi.hProcess, 1);
        WaitForSingleObject(pi.hProcess, 5000);
        ret = LEO_ERR_TIMEOUT;
    }

    /* Le process est terminé (normalement ou tué) : les threads lecteurs
     * vont voir EOF sous peu (dernière copie du côté écriture fermée à la
     * terminaison de l'enfant) et se terminer d'eux-mêmes. */
    if (out_thread_ok) pthread_join(out_thread, NULL);
    if (err_thread_ok) pthread_join(err_thread, NULL);

    result->stdout_buf[out_ctx.off < LEO_EXEC_STDOUT_MAX ? out_ctx.off : LEO_EXEC_STDOUT_MAX - 1] = '\0';
    result->stderr_buf[err_ctx.off < LEO_EXEC_STDERR_MAX ? err_ctx.off : LEO_EXEC_STDERR_MAX - 1] = '\0';

    DWORD exit_code = 0;
    GetExitCodeProcess(pi.hProcess, &exit_code);
    result->exit_code = (int)exit_code;

    CloseHandle(pi.hProcess);
    CloseHandle(stdout_r);
    CloseHandle(stderr_r);

    LOG_DEBUG("Commande exécutée : exit_code=%d stdout=%zu stderr=%zu octets",
              result->exit_code, out_ctx.off, err_ctx.off);

    return ret;
}

/* ─── API publique ───────────────────────────────────────────────────────── */

leo_error_t leo_exec_script(const char *interpreter,
                             const char *script,
                             int         timeout_secs,
                             leo_exec_result_t *result)
{
    if (!interpreter || !script || !result)
        return LEO_ERR_SYSTEM;

    if (!_interpreter_allowed(interpreter)) {
        LOG_ERROR("Interpréteur non autorisé : '%s'", interpreter);
        return LEO_ERR_PROTOCOL;
    }

    bool is_powershell = (strcmp(interpreter, "powershell") == 0);
    const char *ext = is_powershell ? ".ps1" : ".bat";

    char tmppath[MAX_PATH];
    if (!_write_script_tmp(script, ext, tmppath, sizeof(tmppath)))
        return LEO_ERR_SYSTEM;

    /* cmd.exe n'exécute un fichier via "/c" que s'il a une extension
     * reconnue (.bat/.cmd) ; powershell.exe a besoin de "-File" explicite
     * pour exécuter un .ps1 plutôt que de le traiter comme une simple
     * chaîne de commande. */
    char *argv[8];
    int   argc = 0;
    if (is_powershell) {
        argv[argc++] = "powershell.exe";
        argv[argc++] = "-NoProfile";
        argv[argc++] = "-ExecutionPolicy";
        argv[argc++] = "Bypass";
        argv[argc++] = "-File";
        argv[argc++] = tmppath;
    } else {
        argv[argc++] = "cmd.exe";
        argv[argc++] = "/c";
        argv[argc++] = tmppath;
    }
    argv[argc] = NULL;

    char cmdline[LEO_EXEC_CMDLINE_MAX];
    leo_error_t ret;
    if (!_build_cmdline(cmdline, sizeof(cmdline), argv)) {
        ret = LEO_ERR_SYSTEM;
    } else {
        ret = _run_child(cmdline, NULL, timeout_secs, result);
    }

    DeleteFileA(tmppath);
    return ret;
}

leo_error_t leo_exec_argv(char *const argv[],
                           const char *const extra_env[],
                           int timeout_secs,
                           leo_exec_result_t *result)
{
    if (!argv || !argv[0] || !result)
        return LEO_ERR_SYSTEM;

    char cmdline[LEO_EXEC_CMDLINE_MAX];
    if (!_build_cmdline(cmdline, sizeof(cmdline), argv))
        return LEO_ERR_SYSTEM;

    return _run_child(cmdline, extra_env, timeout_secs, result);
}
