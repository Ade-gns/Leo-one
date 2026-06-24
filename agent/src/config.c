/**
 * config.c — Lecture/écriture de la configuration de l'agent Leo-One
 */
#include "config.h"
#include "logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

/* Taille max d'une ligne INI */
#define INI_LINE_MAX 1024

/* ─── Helpers privés ────────────────────────────────────────────────────── */

/** Retire les espaces en début et fin de chaîne (in-place). */
static char *_trim(char *s) {
    if (!s) return s;
    while (isspace((unsigned char)*s)) s++;
    char *end = s + strlen(s) - 1;
    while (end > s && isspace((unsigned char)*end)) *end-- = '\0';
    return s;
}

/** Copie val dans dst si la clé correspond. */
static void _set_field(const char *key, const char *val,
                       const char *target_key, char *dst, size_t dst_sz) {
    if (strcmp(key, target_key) == 0) {
        strncpy(dst, val, dst_sz - 1);
        dst[dst_sz - 1] = '\0';
    }
}

/* ─── API publique ────────────────────────────────────────────────────── */

leo_error_t leo_config_load(const char *path, leo_config_t *out) {
    if (!path || !out) return LEO_ERR_CONFIG;

    FILE *fp = fopen(path, "r");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir le fichier de config : %s", path);
        return LEO_ERR_CONFIG;
    }

    memset(out, 0, sizeof(*out));

    /* Valeurs par défaut des intervalles */
    out->metrics_interval_sec   = LEO_METRICS_INTERVAL_SEC;
    out->heartbeat_interval_sec = LEO_HEARTBEAT_INTERVAL_SEC;

    char line[INI_LINE_MAX];
    int  line_num = 0;

    while (fgets(line, sizeof(line), fp)) {
        line_num++;
        char *p = _trim(line);

        /* Ignorer commentaires et lignes vides */
        if (p[0] == '#' || p[0] == ';' || p[0] == '\0') continue;

        /* Chercher le séparateur '=' */
        char *sep = strchr(p, '=');
        if (!sep) {
            LOG_WARN("config:%d : ligne malformée ignorée : %s", line_num, p);
            continue;
        }

        *sep = '\0';
        char *key = _trim(p);
        char *val = _trim(sep + 1);

        _set_field(key, val, "agent_id",
                   out->agent_id, sizeof(out->agent_id));
        _set_field(key, val, "tenant_id",
                   out->tenant_id, sizeof(out->tenant_id));
        _set_field(key, val, "ws_endpoint",
                   out->ws_endpoint, sizeof(out->ws_endpoint));
        _set_field(key, val, "hostname",
                   out->hostname, sizeof(out->hostname));
        _set_field(key, val, "os_name",
                   out->os_name, sizeof(out->os_name));
        _set_field(key, val, "os_version",
                   out->os_version, sizeof(out->os_version));
        _set_field(key, val, "arch",
                   out->arch, sizeof(out->arch));
        _set_field(key, val, "hardware_id",
                   out->hardware_id, sizeof(out->hardware_id));
        _set_field(key, val, "ca_fingerprint",
                   out->ca_fingerprint, sizeof(out->ca_fingerprint));

        if (strcmp(key, "metrics_interval_sec") == 0)
            out->metrics_interval_sec = atoi(val);
        if (strcmp(key, "heartbeat_interval_sec") == 0)
            out->heartbeat_interval_sec = atoi(val);
    }

    fclose(fp);

    if (!leo_config_is_valid(out)) {
        LOG_ERROR("Configuration incomplète dans %s "
                  "(agent_id, tenant_id ou ws_endpoint manquant)", path);
        return LEO_ERR_CONFIG;
    }

    LOG_INFO("Configuration chargée — agent_id=%s", out->agent_id);
    return LEO_OK;
}

leo_error_t leo_config_save(const char *path, const leo_config_t *cfg) {
    if (!path || !cfg) return LEO_ERR_CONFIG;

    FILE *fp = fopen(path, "w");
    if (!fp) {
        LOG_ERROR("Impossible d'écrire le fichier de config : %s", path);
        return LEO_ERR_CONFIG;
    }

    fprintf(fp, "# Leo-One Agent — configuration générée automatiquement\n");
    fprintf(fp, "# Ne pas modifier manuellement sauf agent_id et ws_endpoint\n\n");

    fprintf(fp, "agent_id=%s\n",               cfg->agent_id);
    fprintf(fp, "tenant_id=%s\n",              cfg->tenant_id);
    fprintf(fp, "ws_endpoint=%s\n",            cfg->ws_endpoint);
    fprintf(fp, "hostname=%s\n",               cfg->hostname);
    fprintf(fp, "os_name=%s\n",                cfg->os_name);
    fprintf(fp, "os_version=%s\n",             cfg->os_version);
    fprintf(fp, "arch=%s\n",                   cfg->arch);
    fprintf(fp, "hardware_id=%s\n",            cfg->hardware_id);
    fprintf(fp, "ca_fingerprint=%s\n",         cfg->ca_fingerprint);
    fprintf(fp, "metrics_interval_sec=%d\n",   cfg->metrics_interval_sec);
    fprintf(fp, "heartbeat_interval_sec=%d\n", cfg->heartbeat_interval_sec);

    fflush(fp);
    fclose(fp);

    LOG_INFO("Configuration sauvegardée dans %s", path);
    return LEO_OK;
}

bool leo_config_is_valid(const leo_config_t *cfg) {
    if (!cfg) return false;
    return cfg->agent_id[0] != '\0'
        && cfg->tenant_id[0] != '\0'
        && cfg->ws_endpoint[0] != '\0';
}
