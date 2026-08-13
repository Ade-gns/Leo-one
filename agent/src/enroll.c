/**
 * enroll.c — Enrollment initial de l'agent auprès du backend
 *
 * Voir enroll.h pour la vue d'ensemble du flux. Ce fichier :
 *   1. Charge le fichier de bootstrap (token + URL de l'API REST).
 *   2. Détecte hostname/OS/architecture/hardware_id de la machine locale.
 *   3. POST /api/v1/enroll via un client HTTP libwebsockets dédié, bloquant
 *      (contexte lws jetable, détruit après la requête — distinct du
 *      contexte WSS persistant de connection.c).
 *   4. Sauvegarde le certificat client reçu et écrit agent.conf.
 */
#include "enroll.h"
#include "config.h"
#include "crypto.h"
#include "logger.h"

#include "../third_party/cjson/cJSON.h"

#include <libwebsockets.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>
#include <time.h>

#if defined(LEO_PLATFORM_WINDOWS)
#  include <windows.h>
#else
#  include <unistd.h>
#  include <sys/utsname.h>
#endif

/* ─── Bootstrap (token + endpoint API) ──────────────────────────────────── */

typedef struct {
    char enrollment_token[128];
    char api_endpoint[256];    /* "http[s]://host[:port]", sans chemin */
} _bootstrap_t;

/** Retire les espaces en début et fin de chaîne (in-place). Même logique
 *  que config.c:_trim — pas de partage possible, cette fonction est static
 *  dans les deux fichiers. */
static char *_trim(char *s) {
    if (!s) return s;
    while (isspace((unsigned char)*s)) s++;
    char *end = s + strlen(s) - 1;
    while (end > s && isspace((unsigned char)*end)) *end-- = '\0';
    return s;
}

static leo_error_t _load_bootstrap(const char *path, _bootstrap_t *out) {
    FILE *fp = fopen(path, "r");
    if (!fp) {
        LOG_ERROR("Enrollment : impossible d'ouvrir le fichier de bootstrap '%s'", path);
        return LEO_ERR_CONFIG;
    }

    memset(out, 0, sizeof(*out));
    char line[512];
    while (fgets(line, sizeof(line), fp)) {
        char *p = _trim(line);
        if (p[0] == '#' || p[0] == ';' || p[0] == '\0') continue;

        char *sep = strchr(p, '=');
        if (!sep) continue;
        *sep = '\0';
        char *key = _trim(p);
        char *val = _trim(sep + 1);

        if (strcmp(key, "enrollment_token") == 0)
            strncpy(out->enrollment_token, val, sizeof(out->enrollment_token) - 1);
        else if (strcmp(key, "api_endpoint") == 0)
            strncpy(out->api_endpoint, val, sizeof(out->api_endpoint) - 1);
    }
    fclose(fp);

    if (out->enrollment_token[0] == '\0' || out->api_endpoint[0] == '\0') {
        LOG_ERROR("Enrollment : '%s' incomplet — 'enrollment_token' et "
                  "'api_endpoint' sont requis", path);
        return LEO_ERR_CONFIG;
    }
    return LEO_OK;
}

/** Parse "http[s]://host[:port][/chemin ignoré]" en use_ssl/host/port. Le
 *  chemin de l'API (/api/v1/enroll) est fixe côté agent, pas lu ici. */
static bool _parse_api_endpoint(const char *endpoint, bool *use_ssl,
                                 char *host, size_t hsz, int *port) {
    const char *p = endpoint;

    if (strncmp(p, "https://", 8) == 0)      { *use_ssl = true;  p += 8; }
    else if (strncmp(p, "http://", 7) == 0)  { *use_ssl = false; p += 7; }
    else                                     { *use_ssl = false; }

    const char *slash = strchr(p, '/');
    const char *colon = strchr(p, ':');
    size_t host_len;

    if (colon && (!slash || colon < slash)) {
        host_len = (size_t)(colon - p);
        if (host_len == 0 || host_len >= hsz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = atoi(colon + 1);
        if (*port <= 0 || *port > 65535) return false;
    } else {
        host_len = slash ? (size_t)(slash - p) : strlen(p);
        if (host_len == 0 || host_len >= hsz) return false;
        strncpy(host, p, host_len);
        host[host_len] = '\0';
        *port = *use_ssl ? 443 : 80;
    }
    return true;
}

/* ─── Détection de la machine locale ────────────────────────────────────── */

static void _detect_hostname(char *out, size_t sz) {
#if defined(LEO_PLATFORM_WINDOWS)
    /* TODO : GetComputerNameA — pas encore de support Windows dans l'agent
     * (platform/windows/ n'est pas encore implémenté, voir CMakeLists.txt). */
    snprintf(out, sz, "unknown-host");
#else
    if (gethostname(out, sz) != 0) {
        snprintf(out, sz, "unknown-host");
    }
    out[sz - 1] = '\0';
#endif
}

static void _detect_os_version(char *out, size_t sz) {
#if !defined(LEO_PLATFORM_WINDOWS)
    /* /etc/os-release (Linux) : PRETTY_NAME="Ubuntu 24.04.1 LTS" */
    FILE *fp = fopen("/etc/os-release", "r");
    if (fp) {
        char line[256];
        while (fgets(line, sizeof(line), fp)) {
            if (strncmp(line, "PRETTY_NAME=", 12) != 0) continue;

            char  *val = line + 12;
            size_t len = strlen(val);
            while (len > 0 && (val[len - 1] == '\n' || val[len - 1] == '\r'))
                val[--len] = '\0';
            if (len >= 2 && val[0] == '"' && val[len - 1] == '"') {
                val[len - 1] = '\0';
                val++;
            }
            snprintf(out, sz, "%s", val);
            fclose(fp);
            return;
        }
        fclose(fp);
    }

    /* Repli : uname() (Linux et macOS) */
    struct utsname u;
    if (uname(&u) == 0) {
        snprintf(out, sz, "%s", u.release);
        return;
    }
#endif
    snprintf(out, sz, "unknown");
}

static void _detect_arch(char *out, size_t sz) {
#if defined(__x86_64__) || defined(_M_X64)
    snprintf(out, sz, "amd64");
#elif defined(__aarch64__) || defined(_M_ARM64)
    snprintf(out, sz, "arm64");
#else
    snprintf(out, sz, "unknown");
#endif
}

/** Identifiant matériel stable, unique par machine — sert de clé
 *  d'unicité (tenant_id, hardware_id) côté backend pour rejeter un
 *  double-enrollment de la même machine. */
static leo_error_t _detect_hardware_id(char *out, size_t sz) {
#if defined(LEO_PLATFORM_LINUX)
    static const char *const paths[] = {
        "/etc/machine-id", "/var/lib/dbus/machine-id"
    };
    for (size_t i = 0; i < sizeof(paths) / sizeof(paths[0]); i++) {
        FILE *fp = fopen(paths[i], "r");
        if (!fp) continue;

        bool ok = fgets(out, (int)sz, fp) != NULL;
        fclose(fp);
        if (!ok) continue;

        size_t len = strlen(out);
        while (len > 0 && (out[len - 1] == '\n' || out[len - 1] == '\r'))
            out[--len] = '\0';
        if (out[0] != '\0') return LEO_OK;
    }

    LOG_ERROR("Enrollment : impossible de lire un identifiant matériel stable "
              "(/etc/machine-id absent ou vide)");
    return LEO_ERR_SYSTEM;
#else
    (void)out; (void)sz;
    LOG_ERROR("Enrollment : détection de l'identifiant matériel non "
              "implémentée sur cette plateforme");
    return LEO_ERR_SYSTEM;
#endif
}

/* ─── Construction de la requête JSON ────────────────────────────────────── */

/** Même schéma que enrollRequest côté backend (agent_handler.go). */
static int _build_enroll_request(char *buf, size_t bufsz, const char *token,
                                  const char *hostname, const char *os_name,
                                  const char *os_version, const char *arch,
                                  const char *hardware_id) {
    cJSON *root = cJSON_CreateObject();
    if (!root) return -1;

    cJSON_AddStringToObject(root, "enrollment_token", token);
    cJSON_AddStringToObject(root, "hostname",         hostname);
    cJSON_AddStringToObject(root, "os",               os_name);
    cJSON_AddStringToObject(root, "os_version",       os_version);
    cJSON_AddStringToObject(root, "arch",             arch);
    cJSON_AddStringToObject(root, "hardware_id",      hardware_id);
    cJSON_AddStringToObject(root, "agent_version",    LEO_AGENT_VERSION);

    char *json_str = cJSON_PrintUnformatted(root);
    cJSON_Delete(root);
    if (!json_str) return -1;

    int written = snprintf(buf, bufsz, "%s", json_str);
    cJSON_free(json_str);

    if (written < 0 || (size_t)written >= bufsz) {
        LOG_ERROR("Enrollment : requête JSON trop volumineuse pour le buffer");
        return -1;
    }
    return written;
}

/* ─── Client HTTP (POST /api/v1/enroll) ─────────────────────────────────── */

#define _ENROLL_HTTP_TIMEOUT_SEC  15
#define _ENROLL_WRITE_CHUNK       2048

typedef struct {
    /* Requête */
    const char *body;
    size_t      body_len;
    size_t      body_sent;

    /* Réponse */
    char   resp_buf[LEO_MAX_MSG_SIZE];
    size_t resp_len;
    int    http_status;

    volatile bool done;
    volatile bool failed;
} _enroll_http_ctx_t;

static int _enroll_lws_callback(struct lws *wsi, enum lws_callback_reasons reason,
                                 void *user, void *in, size_t len) {
    (void)user;
    struct lws_context *ctx = lws_get_context(wsi);
    _enroll_http_ctx_t *hc  = (_enroll_http_ctx_t *)lws_context_user(ctx);
    if (!hc) return 0;

    switch (reason) {

    case LWS_CALLBACK_CLIENT_CONNECTION_ERROR:
        LOG_ERROR("Enrollment : connexion à l'API REST échouée : %s",
                  in ? (const char *)in : "(inconnue)");
        hc->failed = true;
        hc->done   = true;
        break;

    case LWS_CALLBACK_ESTABLISHED_CLIENT_HTTP:
        hc->http_status = (int)lws_http_client_http_response(wsi);
        break;

    case LWS_CALLBACK_CLIENT_APPEND_HANDSHAKE_HEADER: {
        unsigned char **p   = (unsigned char **)in;
        unsigned char  *end = (*p) + len;

        if (lws_add_http_header_by_name(wsi,
                (const unsigned char *)"content-type:",
                (const unsigned char *)"application/json", 16, p, end))
            return -1;
        if (lws_add_http_header_content_length(wsi, (lws_filepos_t)hc->body_len, p, end))
            return -1;

        lws_client_http_body_pending(wsi, 1);
        lws_callback_on_writable(wsi);
        break;
    }

    case LWS_CALLBACK_CLIENT_HTTP_WRITEABLE: {
        unsigned char frame[LWS_PRE + _ENROLL_WRITE_CHUNK];
        size_t remaining = hc->body_len - hc->body_sent;
        size_t chunk     = remaining < _ENROLL_WRITE_CHUNK ? remaining : _ENROLL_WRITE_CHUNK;

        memcpy(frame + LWS_PRE, hc->body + hc->body_sent, chunk);
        hc->body_sent += chunk;
        bool last = (hc->body_sent >= hc->body_len);
        if (last) lws_client_http_body_pending(wsi, 0);

        enum lws_write_protocol wp = last ? LWS_WRITE_HTTP_FINAL : LWS_WRITE_HTTP;
        if (lws_write(wsi, frame + LWS_PRE, chunk, wp) < 0) return -1;
        if (!last) lws_callback_on_writable(wsi);
        return 0;
    }

    case LWS_CALLBACK_RECEIVE_CLIENT_HTTP: {
        char  buf[4096];
        char *p = buf;
        int   n = (int)sizeof(buf);
        /* Déclenche LWS_CALLBACK_RECEIVE_CLIENT_HTTP_READ avec les données
         * décodées (dé-chunkées si besoin). */
        if (lws_http_client_read(wsi, &p, &n) < 0) return -1;
        return 0;
    }

    case LWS_CALLBACK_RECEIVE_CLIENT_HTTP_READ:
        if (in && len > 0) {
            size_t space = sizeof(hc->resp_buf) - 1 - hc->resp_len;
            size_t copy  = len < space ? len : space;
            memcpy(hc->resp_buf + hc->resp_len, in, copy);
            hc->resp_len += copy;
        }
        return 0;

    case LWS_CALLBACK_COMPLETED_CLIENT_HTTP:
    case LWS_CALLBACK_CLOSED_CLIENT_HTTP:
        hc->done = true;
        break;

    default:
        break;
    }

    return lws_callback_http_dummy(wsi, reason, user, in, len);
}

static struct lws_protocols _enroll_protocols[] = {
    { "http", _enroll_lws_callback, 0, 0, 0, NULL, 0 },
    { NULL, NULL, 0, 0, 0, NULL, 0 }
};

/**
 * POST bloquant : crée un contexte lws jetable, envoie body, accumule la
 * réponse, détruit le contexte. Ne doit être appelé qu'au démarrage, avant
 * que le thread WSS persistant (connection.c) n'existe.
 */
static leo_error_t _http_post_json(const char *host, int port, bool use_ssl,
                                    const char *path, const char *body,
                                    char *resp_out, size_t resp_out_sz,
                                    int *status_out) {
    _enroll_http_ctx_t hc;
    memset(&hc, 0, sizeof(hc));
    hc.body     = body;
    hc.body_len = strlen(body);

    struct lws_context_creation_info info = {0};
    info.port      = CONTEXT_PORT_NO_LISTEN;
    info.protocols = _enroll_protocols;
    info.options   = LWS_SERVER_OPTION_DO_SSL_GLOBAL_INIT;
    info.user      = &hc;

    struct lws_context *ctx = lws_create_context(&info);
    if (!ctx) {
        LOG_ERROR("Enrollment : impossible de créer le contexte HTTP libwebsockets");
        return LEO_ERR_NETWORK;
    }

    struct lws_client_connect_info ci = {0};
    ci.context        = ctx;
    ci.address        = host;
    ci.port           = port;
    ci.path           = path;
    ci.host           = host;
    ci.origin         = host;
    ci.method         = "POST";
    ci.protocol       = _enroll_protocols[0].name;
    ci.ssl_connection = use_ssl ? LCCSCF_USE_SSL : 0;

    struct lws *wsi = lws_client_connect_via_info(&ci);
    if (!wsi) {
        LOG_ERROR("Enrollment : lws_client_connect_via_info a échoué (%s:%d)", host, port);
        lws_context_destroy(ctx);
        return LEO_ERR_NETWORK;
    }

    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += _ENROLL_HTTP_TIMEOUT_SEC;

    while (!hc.done) {
        if (lws_service(ctx, 50) < 0) {
            hc.failed = true;
            break;
        }
        struct timespec now;
        clock_gettime(CLOCK_REALTIME, &now);
        if (now.tv_sec > deadline.tv_sec) {
            LOG_ERROR("Enrollment : timeout de la requête HTTP (%ds)", _ENROLL_HTTP_TIMEOUT_SEC);
            hc.failed = true;
            break;
        }
    }

    lws_context_destroy(ctx);

    if (hc.failed || hc.http_status == 0) return LEO_ERR_NETWORK;

    size_t copy = hc.resp_len < resp_out_sz - 1 ? hc.resp_len : resp_out_sz - 1;
    memcpy(resp_out, hc.resp_buf, copy);
    resp_out[copy] = '\0';
    *status_out = hc.http_status;

    return LEO_OK;
}

/* ─── Parsing de la réponse ──────────────────────────────────────────────── */

/** Copie un champ chaîne obligatoire de `data` vers dst ; loggue et renvoie
 *  false si absent, vide, ou de type incorrect. */
static bool _copy_response_field(cJSON *data, const char *name, char *dst, size_t dstsz) {
    cJSON *j = cJSON_GetObjectItemCaseSensitive(data, name);
    if (!cJSON_IsString(j) || !j->valuestring[0]) {
        LOG_ERROR("Enrollment : champ '%s' manquant ou vide dans la réponse", name);
        return false;
    }
    strncpy(dst, j->valuestring, dstsz - 1);
    dst[dstsz - 1] = '\0';
    return true;
}

static leo_error_t _parse_enroll_response(const char *json_str, int http_status,
                                           leo_config_t *cfg,
                                           char *cert_out, size_t cert_sz,
                                           char *key_out, size_t key_sz) {
    cJSON *root = cJSON_Parse(json_str);
    if (!root) {
        LOG_ERROR("Enrollment : réponse JSON illisible (HTTP %d)", http_status);
        return LEO_ERR_PROTOCOL;
    }

    if (http_status != 200 && http_status != 201) {
        cJSON *jerr = cJSON_GetObjectItemCaseSensitive(root, "error");
        cJSON *jmsg = jerr ? cJSON_GetObjectItemCaseSensitive(jerr, "message") : NULL;
        LOG_ERROR("Enrollment refusé par le backend (HTTP %d) : %s", http_status,
                  cJSON_IsString(jmsg) ? jmsg->valuestring : "raison inconnue");
        cJSON_Delete(root);
        return LEO_ERR_PROTOCOL;
    }

    cJSON *data = cJSON_GetObjectItemCaseSensitive(root, "data");
    if (!cJSON_IsObject(data)) {
        LOG_ERROR("Enrollment : réponse sans champ 'data'");
        cJSON_Delete(root);
        return LEO_ERR_PROTOCOL;
    }

    bool ok = _copy_response_field(data, "agent_id", cfg->agent_id, sizeof(cfg->agent_id))
           && _copy_response_field(data, "tenant_id", cfg->tenant_id, sizeof(cfg->tenant_id))
           && _copy_response_field(data, "ws_endpoint", cfg->ws_endpoint, sizeof(cfg->ws_endpoint))
           && _copy_response_field(data, "server_cert_fingerprint",
                                    cfg->ca_fingerprint, sizeof(cfg->ca_fingerprint))
           && _copy_response_field(data, "client_cert_pem", cert_out, cert_sz)
           && _copy_response_field(data, "client_key_pem", key_out, key_sz);

    cJSON_Delete(root);
    return ok ? LEO_OK : LEO_ERR_PROTOCOL;
}

/* ─── API publique ────────────────────────────────────────────────────── */

leo_error_t leo_enroll(const char *bootstrap_path, const char *config_path,
                        leo_config_t *out_cfg) {
    if (!bootstrap_path || !config_path || !out_cfg) return LEO_ERR_CONFIG;

    _bootstrap_t bs;
    if (_load_bootstrap(bootstrap_path, &bs) != LEO_OK) return LEO_ERR_CONFIG;

    memset(out_cfg, 0, sizeof(*out_cfg));
    out_cfg->metrics_interval_sec   = LEO_METRICS_INTERVAL_SEC;
    out_cfg->heartbeat_interval_sec = LEO_HEARTBEAT_INTERVAL_SEC;

    _detect_hostname(out_cfg->hostname, sizeof(out_cfg->hostname));
    _detect_os_version(out_cfg->os_version, sizeof(out_cfg->os_version));
    _detect_arch(out_cfg->arch, sizeof(out_cfg->arch));
#if defined(LEO_PLATFORM_LINUX)
    snprintf(out_cfg->os_name, sizeof(out_cfg->os_name), "linux");
#elif defined(LEO_PLATFORM_WINDOWS)
    snprintf(out_cfg->os_name, sizeof(out_cfg->os_name), "windows");
#elif defined(LEO_PLATFORM_MACOS)
    snprintf(out_cfg->os_name, sizeof(out_cfg->os_name), "macos");
#else
    snprintf(out_cfg->os_name, sizeof(out_cfg->os_name), "unknown");
#endif

    if (_detect_hardware_id(out_cfg->hardware_id, sizeof(out_cfg->hardware_id)) != LEO_OK)
        return LEO_ERR_SYSTEM;

    LOG_INFO("Enrollment : hostname=%s os=%s/%s arch=%s hardware_id=%s",
             out_cfg->hostname, out_cfg->os_name, out_cfg->os_version,
             out_cfg->arch, out_cfg->hardware_id);

    char req_body[2048];
    if (_build_enroll_request(req_body, sizeof(req_body), bs.enrollment_token,
                               out_cfg->hostname, out_cfg->os_name, out_cfg->os_version,
                               out_cfg->arch, out_cfg->hardware_id) <= 0) {
        return LEO_ERR_PROTOCOL;
    }

    bool use_ssl;
    char host[256];
    int  port;
    if (!_parse_api_endpoint(bs.api_endpoint, &use_ssl, host, sizeof(host), &port)) {
        LOG_ERROR("Enrollment : api_endpoint invalide dans '%s' : %s",
                  bootstrap_path, bs.api_endpoint);
        return LEO_ERR_CONFIG;
    }

    LOG_INFO("Enrollment : POST %s://%s:%d/api/v1/enroll",
             use_ssl ? "https" : "http", host, port);

    char resp_buf[LEO_MAX_MSG_SIZE];
    int  http_status = 0;
    leo_error_t rc = _http_post_json(host, port, use_ssl, "/api/v1/enroll", req_body,
                                      resp_buf, sizeof(resp_buf), &http_status);
    if (rc != LEO_OK) {
        LOG_ERROR("Enrollment : requête HTTP vers l'API REST échouée");
        return rc;
    }

    char cert_pem[LEO_CERT_BUF_SIZE];
    char key_pem[LEO_KEY_BUF_SIZE];
    rc = _parse_enroll_response(resp_buf, http_status, out_cfg,
                                 cert_pem, sizeof(cert_pem), key_pem, sizeof(key_pem));
    if (rc != LEO_OK) return rc;

    if (leo_crypto_save_cert_key(cert_pem, key_pem) != LEO_OK) {
        LOG_ERROR("Enrollment : échec de sauvegarde du certificat client");
        return LEO_ERR_TLS;
    }

    if (leo_config_save(config_path, out_cfg) != LEO_OK) {
        LOG_ERROR("Enrollment : échec d'écriture de '%s'", config_path);
        return LEO_ERR_CONFIG;
    }

    LOG_INFO("Enrollment réussi — agent_id=%s tenant_id=%s",
             out_cfg->agent_id, out_cfg->tenant_id);

    /* Le token est à usage unique côté backend (voir EnrollmentHandler) —
     * agent.conf existe désormais et sera chargé directement au prochain
     * démarrage. Retirer le bootstrap évite toute confusion/réutilisation
     * si l'installeur laissait le fichier en place. */
    if (remove(bootstrap_path) != 0) {
        LOG_WARN("Enrollment : impossible de supprimer '%s' (non bloquant)", bootstrap_path);
    }

    return LEO_OK;
}
