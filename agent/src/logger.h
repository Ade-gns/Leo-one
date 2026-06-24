/**
 * logger.h — Interface de journalisation de l'agent Leo-One
 *
 * Logger thread-safe avec rotation de fichier.
 * Niveaux : DEBUG < INFO < WARN < ERROR < FATAL
 */
#ifndef LEO_LOGGER_H
#define LEO_LOGGER_H

#include <stdarg.h>
#include <stdbool.h>

typedef enum {
    LOG_DEBUG = 0,
    LOG_INFO  = 1,
    LOG_WARN  = 2,
    LOG_ERROR = 3,
    LOG_FATAL = 4
} leo_log_level_t;

/**
 * Initialise le logger.
 * @param path      Chemin du fichier de log (NULL = stderr uniquement)
 * @param level     Niveau minimum à enregistrer
 * @param max_bytes Taille max avant rotation (ex: 10*1024*1024 pour 10 MB)
 * @return 0 si succès, -1 si erreur d'ouverture du fichier
 */
int  leo_log_init(const char *path, leo_log_level_t level, long max_bytes);

/**
 * Écrit un message de log.
 * Thread-safe via mutex interne.
 */
void leo_log_write(leo_log_level_t level, const char *file, int line,
                   const char *fmt, ...);

/** Ferme proprement le logger (flush + close). */
void leo_log_destroy(void);

/* Macros pratiques qui injectent automatiquement __FILE__ et __LINE__ */
#define LOG_DEBUG(fmt, ...) leo_log_write(LOG_DEBUG, __FILE__, __LINE__, fmt, ##__VA_ARGS__)
#define LOG_INFO(fmt, ...)  leo_log_write(LOG_INFO,  __FILE__, __LINE__, fmt, ##__VA_ARGS__)
#define LOG_WARN(fmt, ...)  leo_log_write(LOG_WARN,  __FILE__, __LINE__, fmt, ##__VA_ARGS__)
#define LOG_ERROR(fmt, ...) leo_log_write(LOG_ERROR, __FILE__, __LINE__, fmt, ##__VA_ARGS__)
#define LOG_FATAL(fmt, ...) leo_log_write(LOG_FATAL, __FILE__, __LINE__, fmt, ##__VA_ARGS__)

#endif /* LEO_LOGGER_H */
