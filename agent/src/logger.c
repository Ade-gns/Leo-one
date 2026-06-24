/**
 * logger.c — Implémentation du logger thread-safe de l'agent Leo-One
 *
 * Rotation : quand le fichier dépasse max_bytes, il est renommé .1
 * et un nouveau fichier est créé. On garde une seule archive (.1).
 */
#include "logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <pthread.h>
#include <sys/stat.h>

#ifdef _WIN32
#  include <windows.h>
#  define leo_rename(a, b) MoveFileExA((a), (b), MOVEFILE_REPLACE_EXISTING)
#else
#  include <unistd.h>
#  define leo_rename(a, b) rename((a), (b))
#endif

/* ─── État global du logger (private) ──────────────────────────────────── */
static struct {
    FILE           *fp;
    leo_log_level_t min_level;
    long            max_bytes;
    char            path[512];
    pthread_mutex_t mutex;
    bool            initialized;
} g_logger = {0};

static const char *LEVEL_NAMES[] = {
    "DEBUG", "INFO ", "WARN ", "ERROR", "FATAL"
};

/* Taille courante du fichier de log */
static long _file_size(void) {
    if (!g_logger.fp) return 0;
    long pos = ftell(g_logger.fp);
    return (pos > 0) ? pos : 0;
}

/* Rotation du fichier : renomme .log → .log.1 et recrée le fichier */
static void _rotate(void) {
    if (!g_logger.fp || g_logger.path[0] == '\0') return;

    fclose(g_logger.fp);
    g_logger.fp = NULL;

    char archive[520];
    snprintf(archive, sizeof(archive), "%s.1", g_logger.path);
    leo_rename(g_logger.path, archive);

    g_logger.fp = fopen(g_logger.path, "a");
    /* Si fopen échoue, les messages continueront d'aller sur stderr */
}

/* ─── API publique ────────────────────────────────────────────────────── */

int leo_log_init(const char *path, leo_log_level_t level, long max_bytes) {
    pthread_mutex_init(&g_logger.mutex, NULL);
    g_logger.min_level = level;
    g_logger.max_bytes = (max_bytes > 0) ? max_bytes : 10L * 1024 * 1024;

    if (path && path[0] != '\0') {
        strncpy(g_logger.path, path, sizeof(g_logger.path) - 1);
        g_logger.fp = fopen(path, "a");
        if (!g_logger.fp) {
            fprintf(stderr, "[leo-agent] WARN: impossible d'ouvrir le log '%s', "
                    "utilisation de stderr\n", path);
            /* Non fatal : on continue sur stderr */
        }
    }

    g_logger.initialized = true;
    return (g_logger.fp || path == NULL) ? 0 : -1;
}

void leo_log_write(leo_log_level_t level, const char *file, int line,
                   const char *fmt, ...) {
    if (!g_logger.initialized || level < g_logger.min_level) return;

    /* Timestamp ISO-8601 */
    char ts[32];
    time_t now = time(NULL);
    struct tm *tm_info = gmtime(&now);
    strftime(ts, sizeof(ts), "%Y-%m-%dT%H:%M:%SZ", tm_info);

    /* Nom de fichier court (dernière composante) */
    const char *base = file ? strrchr(file, '/') : NULL;
    base = base ? base + 1 : (file ? file : "?");

    /* Formatage du message utilisateur */
    char msg[4096];
    va_list args;
    va_start(args, fmt);
    vsnprintf(msg, sizeof(msg), fmt, args);
    va_end(args);

    pthread_mutex_lock(&g_logger.mutex);

    /* Rotation si le fichier est trop grand */
    if (g_logger.fp && _file_size() >= g_logger.max_bytes) {
        _rotate();
    }

    FILE *out = g_logger.fp ? g_logger.fp : stderr;
    fprintf(out, "%s [%s] %s:%d — %s\n", ts, LEVEL_NAMES[level], base, line, msg);
    fflush(out);

    pthread_mutex_unlock(&g_logger.mutex);

    /* Pour FATAL : flush stderr aussi, puis on laisse l'appelant décider */
    if (level == LOG_FATAL) {
        fprintf(stderr, "%s [FATAL] %s:%d — %s\n", ts, base, line, msg);
        fflush(stderr);
    }
}

void leo_log_destroy(void) {
    if (!g_logger.initialized) return;

    pthread_mutex_lock(&g_logger.mutex);
    if (g_logger.fp) {
        fflush(g_logger.fp);
        fclose(g_logger.fp);
        g_logger.fp = NULL;
    }
    pthread_mutex_unlock(&g_logger.mutex);
    pthread_mutex_destroy(&g_logger.mutex);
    g_logger.initialized = false;
}
