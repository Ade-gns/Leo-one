/**
 * protocol.c — Sérialisation/désérialisation des messages WSS de l'agent
 */
#include "protocol.h"
#include "logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

/* ─── Helpers privés ────────────────────────────────────────────────────── */

/** Timestamp Unix en millisecondes. */
static uint64_t _now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (uint64_t)ts.tv_sec * 1000ULL + (uint64_t)(ts.tv_nsec / 1000000ULL);
}

/**
 * Génère un UUID v4 pseudo-aléatoire.
 * En production, remplacer par un générateur cryptographique (getrandom, BCryptGenRandom).
 */
static void _gen_uuid(char *out, size_t sz) {
    static const char hex[] = "0123456789abcdef";
    unsigned char rnd[16];

    /* Lecture de /dev/urandom — disponible sur Linux/macOS */
    FILE *fp = fopen("/dev/urandom", "rb");
    if (fp) {
        (void)fread(rnd, 1, sizeof(rnd), fp);
        fclose(fp);
    } else {
        /* Fallback dégradé : ne pas utiliser en production */
        for (int i = 0; i < 16; i++) rnd[i] = (unsigned char)(rand() & 0xFF);
    }

    /* Forcer les bits de version (4) et variant (10xx) */
    rnd[6] = (rnd[6] & 0x0F) | 0x40;
    rnd[8] = (rnd[8] & 0x3F) | 0x80;

    snprintf(out, sz,
        "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
        rnd[0],  rnd[1],  rnd[2],  rnd[3],
        rnd[4],  rnd[5],  rnd[6],  rnd[7],
        rnd[8],  rnd[9],  rnd[10], rnd[11],
        rnd[12], rnd[13], rnd[14], rnd[15]);
}

/**
 * Construit l'enveloppe JSON et sérialise en buffer.
 * @param type  Type de message
 * @param body  Objet JSON body (ownership transféré : sera libéré ici)
 * @return nombre d'octets écrits dans buf, -1 si erreur
 */
static int _serialize(leo_msg_type_t type, cJSON *body, char *buf, size_t bufsz) {
    char msg_id[LEO_UUID_STR_LEN];
    _gen_uuid(msg_id, sizeof(msg_id));

    cJSON *root = cJSON_CreateObject();
    if (!root) { cJSON_Delete(body); return -1; }

    cJSON_AddNumberToObject(root, "v",    LEO_PROTOCOL_VERSION);
    cJSON_AddNumberToObject(root, "type", (double)type);
    cJSON_AddStringToObject(root, "id",   msg_id);
    cJSON_AddNumberToObject(root, "ts",   (double)_now_ms());

    if (body) {
        cJSON_AddItemToObject(root, "body", body);
    } else {
        cJSON_AddObjectToObject(root, "body");
    }

    char *json_str = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);

    if (!json_str) return -1;

    int written = snprintf(buf, bufsz, "%s", json_str);
    cJSON_free(json_str);

    if (written < 0 || (size_t)written >= bufsz) {
        LOG_ERROR("Buffer trop petit pour sérialiser le message type=%d", type);
        return -1;
    }

    return written;
}

/* ─── Sérialisation ─────────────────────────────────────────────────────── */

int leo_proto_build_hello(const leo_config_t *cfg, char *buf, size_t bufsz) {
    cJSON *body = cJSON_CreateObject();
    if (!body) return -1;

    cJSON_AddStringToObject(body, "agent_id",      cfg->agent_id);
    cJSON_AddStringToObject(body, "tenant_id",     cfg->tenant_id);
    cJSON_AddStringToObject(body, "hostname",      cfg->hostname);
    cJSON_AddStringToObject(body, "os",            cfg->os_name);
    cJSON_AddStringToObject(body, "os_version",    cfg->os_version);
    cJSON_AddStringToObject(body, "arch",          cfg->arch);
    cJSON_AddStringToObject(body, "agent_version", LEO_AGENT_VERSION);

    return _serialize(LEO_MSG_HELLO, body, buf, bufsz);
}

int leo_proto_build_heartbeat(uint64_t seq, char *buf, size_t bufsz) {
    cJSON *body = cJSON_CreateObject();
    if (!body) return -1;

    cJSON_AddNumberToObject(body, "seq", (double)seq);

    return _serialize(LEO_MSG_HEARTBEAT, body, buf, bufsz);
}

int leo_proto_build_metrics(const leo_metrics_t *m, char *buf, size_t bufsz) {
    cJSON *body = cJSON_CreateObject();
    if (!body) return -1;

    cJSON_AddNumberToObject(body, "cpu_percent",        m->cpu_total_percent);
    cJSON_AddNumberToObject(body, "ram_total_bytes",    (double)m->ram_total_bytes);
    cJSON_AddNumberToObject(body, "ram_used_bytes",     (double)m->ram_used_bytes);
    cJSON_AddNumberToObject(body, "ram_available_bytes",(double)m->ram_available_bytes);
    cJSON_AddNumberToObject(body, "disk_total_bytes",   (double)m->disk_total_bytes);
    cJSON_AddNumberToObject(body, "disk_used_bytes",    (double)m->disk_used_bytes);
    cJSON_AddNumberToObject(body, "net_bytes_in",       (double)m->net_bytes_in);
    cJSON_AddNumberToObject(body, "net_bytes_out",      (double)m->net_bytes_out);
    cJSON_AddNumberToObject(body, "process_count",      (double)m->process_count);

    /* Tableau CPU par cœur */
    cJSON *cores = cJSON_CreateArray();
    for (int i = 0; i < m->cpu_core_count && i < LEO_MAX_CPU_CORES; i++) {
        cJSON_AddItemToArray(cores, cJSON_CreateNumber(m->cpu_per_core[i]));
    }
    cJSON_AddItemToObject(body, "cpu_per_core", cores);

    return _serialize(LEO_MSG_METRICS, body, buf, bufsz);
}

int leo_proto_build_cmd_result(const char *cmd_id, int exit_code,
                               const char *stdout_s, const char *stderr_s,
                               char *buf, size_t bufsz) {
    cJSON *body = cJSON_CreateObject();
    if (!body) return -1;

    cJSON_AddStringToObject(body, "command_id", cmd_id ? cmd_id : "");
    cJSON_AddNumberToObject(body, "exit_code",  (double)exit_code);
    cJSON_AddStringToObject(body, "stdout",     stdout_s ? stdout_s : "");
    cJSON_AddStringToObject(body, "stderr",     stderr_s ? stderr_s : "");

    return _serialize(LEO_MSG_CMD_RESULT, body, buf, bufsz);
}

int leo_proto_build_pong(const char *ping_id, char *buf, size_t bufsz) {
    cJSON *body = cJSON_CreateObject();
    if (!body) return -1;

    cJSON_AddStringToObject(body, "ping_id", ping_id ? ping_id : "");

    return _serialize(LEO_MSG_PONG, body, buf, bufsz);
}

/* ─── Désérialisation ───────────────────────────────────────────────────── */

leo_error_t leo_proto_parse(const char *json_str, leo_incoming_msg_t *out) {
    if (!json_str || !out) return LEO_ERR_PROTOCOL;

    memset(out, 0, sizeof(*out));
    out->type = LEO_MSG_UNKNOWN;

    cJSON *root = cJSON_Parse(json_str);
    if (!root) {
        LOG_WARN("Impossible de parser le JSON entrant");
        return LEO_ERR_PROTOCOL;
    }

    /* Champ "type" */
    cJSON *jtype = cJSON_GetObjectItemCaseSensitive(root, "type");
    if (!cJSON_IsNumber(jtype)) {
        LOG_WARN("Message sans champ 'type'");
        cJSON_Delete(root);
        return LEO_ERR_PROTOCOL;
    }
    out->type = (leo_msg_type_t)(int)jtype->valuedouble;

    /* Champ "id" */
    cJSON *jid = cJSON_GetObjectItemCaseSensitive(root, "id");
    if (cJSON_IsString(jid) && jid->valuestring) {
        strncpy(out->id, jid->valuestring, sizeof(out->id) - 1);
    }

    /* Champ "body" : on détache l'objet pour qu'il survive à la suppression de root */
    cJSON *jbody = cJSON_DetachItemFromObjectCaseSensitive(root, "body");
    out->body = jbody;  /* peut être NULL si pas de body */

    cJSON_Delete(root);
    return LEO_OK;
}

void leo_proto_msg_free(leo_incoming_msg_t *msg) {
    if (!msg) return;
    if (msg->body) {
        cJSON_Delete(msg->body);
        msg->body = NULL;
    }
    memset(msg, 0, sizeof(*msg));
}
