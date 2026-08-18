/**
 * http_url.h — Parsing minimal d'URL HTTP(S), partagé par les clients HTTP
 * jetables de l'agent (enroll.c pour /api/v1/enroll, file_transfer.c pour
 * le téléchargement de fichiers depuis une URL signée).
 */
#ifndef LEO_HTTP_URL_H
#define LEO_HTTP_URL_H

#include "../include/leo_agent.h"

/**
 * Parse "http[s]://host[:port][/chemin]" en use_ssl/host/port/path.
 * @param path_out  Peut être NULL si l'appelant n'a pas besoin du chemin
 *                   (ex: enroll.c, qui vise toujours /api/v1/enroll en dur).
 *                   Sinon reçoit le chemin (avec la query string), ou "/"
 *                   si l'URL n'en a pas.
 * @return true si l'URL est syntaxiquement valide, false sinon.
 */
bool leo_http_parse_url(const char *url, bool *use_ssl,
                         char *host, size_t host_sz, int *port,
                         char *path_out, size_t path_out_sz);

#endif /* LEO_HTTP_URL_H */
