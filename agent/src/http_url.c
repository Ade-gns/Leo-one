/**
 * http_url.c — voir http_url.h
 */
#include "http_url.h"

#include <string.h>
#include <stdlib.h>

bool leo_http_parse_url(const char *url, bool *use_ssl,
                         char *host, size_t host_sz, int *port,
                         char *path_out, size_t path_out_sz) {
    const char *p = url;

    if (strncmp(p, "https://", 8) == 0)      { *use_ssl = true;  p += 8; }
    else if (strncmp(p, "http://", 7) == 0)  { *use_ssl = false; p += 7; }
    else                                     { *use_ssl = false; }

    const char *slash = strchr(p, '/');
    const char *colon = strchr(p, ':');
    size_t host_len;

    if (colon && (!slash || colon < slash)) {
        host_len = (size_t)(colon - p);
        if (host_len == 0 || host_len >= host_sz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = atoi(colon + 1);
        if (*port <= 0 || *port > 65535) return false;
    } else {
        host_len = slash ? (size_t)(slash - p) : strlen(p);
        if (host_len == 0 || host_len >= host_sz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = *use_ssl ? 443 : 80;
    }

    if (path_out && path_out_sz > 0) {
        if (slash) {
            strncpy(path_out, slash, path_out_sz - 1);
            path_out[path_out_sz - 1] = '\0';
        } else {
            strncpy(path_out, "/", path_out_sz - 1);
            path_out[path_out_sz - 1] = '\0';
        }
    }

    return true;
}
