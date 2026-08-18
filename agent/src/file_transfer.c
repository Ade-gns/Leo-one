/**
 * file_transfer.c — voir file_transfer.h
 *
 * Client HTTP GET jetable (même schéma que _http_post_json dans enroll.c :
 * contexte lws créé/détruit pour cette seule requête, boucle lws_service()
 * bloquante) mais qui écrit la réponse en flux sur disque plutôt que de
 * l'accumuler en mémoire — un fichier déployé peut largement dépasser
 * LEO_MAX_MSG_SIZE (64 Ko), contrairement aux réponses JSON de l'enrollment.
 */
#include "file_transfer.h"
#include "http_url.h"
#include "protocol.h"
#include "logger.h"

#include <libwebsockets.h>
#include <openssl/evp.h>

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <errno.h>

#ifdef _WIN32
#  include <windows.h>
#  include <direct.h>
   /* mingw expose _stricmp (MSVCRT) dans <string.h> ; pas de strcasecmp
    * (POSIX) — même shim que platform/windows/crypto_win.c. */
#  define strcasecmp _stricmp
#else
#  include <sys/stat.h>
#  include <unistd.h>
#endif

#define _DL_WRITE_TIMEOUT_SEC_MIN  30  /* plancher : un gros fichier sur un lien lent
                                         * ne doit pas timeout sur le timeout_secs de
                                         * l'appelant s'il est court (hérité du défaut
                                         * générique des commandes, pensé pour des
                                         * scripts, pas des transferts de fichiers). */

/* ─── Répertoire de travail ──────────────────────────────────────────────── */

/** Crée récursivement LEO_WORKDIR si nécessaire (équivalent mkdir -p) — même
 *  logique que _mkdir_p dans crypto_linux.c/crypto_win.c, dupliquée ici pour
 *  ne pas exposer une fonction interne du module crypto en API publique
 *  juste pour ce seul appelant. */
static bool _ensure_workdir(void) {
    char tmp[512];
    strncpy(tmp, LEO_WORKDIR, sizeof(tmp) - 1);
    tmp[sizeof(tmp) - 1] = '\0';

    size_t len = strlen(tmp);
    if (len > 0 && (tmp[len - 1] == '/' || tmp[len - 1] == '\\'))
        tmp[len - 1] = '\0';

    for (char *p = tmp + 1; *p; p++) {
        if (*p == '/' || *p == '\\') {
            char sep = *p;
            *p = '\0';
#ifdef _WIN32
            if (!CreateDirectoryA(tmp, NULL) && GetLastError() != ERROR_ALREADY_EXISTS) {
                LOG_ERROR("CreateDirectoryA('%s') échoué (code %lu)", tmp, GetLastError());
                return false;
            }
#else
            if (mkdir(tmp, 0755) != 0 && errno != EEXIST) {
                LOG_ERROR("mkdir('%s') échoué : %s", tmp, strerror(errno));
                return false;
            }
#endif
            *p = sep;
        }
    }
#ifdef _WIN32
    if (!CreateDirectoryA(tmp, NULL) && GetLastError() != ERROR_ALREADY_EXISTS) {
        LOG_ERROR("CreateDirectoryA('%s') échoué (code %lu)", tmp, GetLastError());
        return false;
    }
#else
    if (mkdir(tmp, 0755) != 0 && errno != EEXIST) {
        LOG_ERROR("mkdir('%s') échoué : %s", tmp, strerror(errno));
        return false;
    }
#endif
    return true;
}

/** Rejette tout nom de fichier qui pourrait s'échapper de LEO_WORKDIR : pas
 *  de séparateur de chemin, pas de "..", pas de nom vide — le backend est
 *  de confiance (auth JWT + permission files:execute), mais un serveur
 *  compromis ou une erreur de configuration ne doit pas pouvoir écrire hors
 *  du répertoire de travail de l'agent. */
static bool _filename_valid(const char *name) {
    size_t len = strlen(name);
    if (len == 0 || len >= LEO_FILE_NAME_MAX_LEN) return false;
    if (strstr(name, "..")) return false;
    for (size_t i = 0; i < len; i++) {
        char c = name[i];
        if (c == '/' || c == '\\') return false;
    }
    return true;
}

/* ─── Client HTTP GET streaming ──────────────────────────────────────────── */

typedef struct {
    FILE       *fp;
    EVP_MD_CTX *mdctx;
    uint64_t    bytes_received;
    uint64_t    bytes_total;      /* Content-Length si connu, sinon expected_size (peut rester 0) */
    int         last_reported_pct;

    leo_conn_t *conn;
    char        cmd_id[LEO_UUID_STR_LEN];

    int         http_status;
    volatile bool done;
    volatile bool failed;
    char        err_buf[256];
} _dl_ctx_t;

/** Envoie un FILE_TRANSFER_PROGRESS — best-effort, une erreur d'envoi ne fait
 *  pas échouer le transfert lui-même (voir leo_conn_send, déjà tolérant à
 *  une file d'envoi pleine). */
static void _report_progress(_dl_ctx_t *dl, leo_file_transfer_status_t status, const char *err) {
    char buf[1024];
    int pct = dl->bytes_total > 0
        ? (int)((dl->bytes_received * 100) / dl->bytes_total)
        : 0;
    int wlen = leo_proto_build_file_transfer_progress(
        dl->cmd_id, status, pct, dl->bytes_received, dl->bytes_total, err, buf, sizeof(buf));
    if (wlen > 0) leo_conn_send(dl->conn, buf, (size_t)wlen);
}

static int _dl_lws_callback(struct lws *wsi, enum lws_callback_reasons reason,
                             void *user, void *in, size_t len) {
    (void)user;
    struct lws_context *ctx = lws_get_context(wsi);
    _dl_ctx_t *dl = (_dl_ctx_t *)lws_context_user(ctx);
    if (!dl) return 0;

    switch (reason) {

    case LWS_CALLBACK_CLIENT_CONNECTION_ERROR:
        snprintf(dl->err_buf, sizeof(dl->err_buf), "Connexion au serveur de fichiers échouée : %s",
                  in ? (const char *)in : "(inconnue)");
        dl->failed = true;
        dl->done   = true;
        break;

    case LWS_CALLBACK_ESTABLISHED_CLIENT_HTTP: {
        dl->http_status = (int)lws_http_client_http_response(wsi);
        if (dl->bytes_total == 0) {
            char clen[32];
            if (lws_hdr_copy(wsi, clen, sizeof(clen), WSI_TOKEN_HTTP_CONTENT_LENGTH) > 0) {
                dl->bytes_total = (uint64_t)strtoull(clen, NULL, 10);
            }
        }
        if (dl->http_status != 200) {
            snprintf(dl->err_buf, sizeof(dl->err_buf), "Réponse HTTP %d du serveur de fichiers", dl->http_status);
            dl->failed = true;
        }
        break;
    }

    case LWS_CALLBACK_RECEIVE_CLIENT_HTTP: {
        char  buf[8192];
        char *p = buf;
        int   n = (int)sizeof(buf);
        if (lws_http_client_read(wsi, &p, &n) < 0) return -1;
        return 0;
    }

    case LWS_CALLBACK_RECEIVE_CLIENT_HTTP_READ:
        if (in && len > 0 && !dl->failed) {
            if (fwrite(in, 1, len, dl->fp) != len) {
                snprintf(dl->err_buf, sizeof(dl->err_buf), "Écriture disque échouée : %s", strerror(errno));
                dl->failed = true;
                return -1;
            }
            EVP_DigestUpdate(dl->mdctx, in, len);
            dl->bytes_received += len;

            int pct = dl->bytes_total > 0 ? (int)((dl->bytes_received * 100) / dl->bytes_total) : 0;
            if (pct >= dl->last_reported_pct + 10 || (dl->bytes_total == 0 && dl->bytes_received % (1024 * 1024) < len)) {
                dl->last_reported_pct = pct;
                _report_progress(dl, LEO_FILE_TRANSFER_DOWNLOADING, NULL);
            }
        }
        return 0;

    case LWS_CALLBACK_COMPLETED_CLIENT_HTTP:
    case LWS_CALLBACK_CLOSED_CLIENT_HTTP:
        dl->done = true;
        break;

    default:
        break;
    }

    return lws_callback_http_dummy(wsi, reason, user, in, len);
}

static struct lws_protocols _dl_protocols[] = {
    { "http", _dl_lws_callback, 0, 0, 0, NULL, 0 },
    { NULL, NULL, 0, 0, 0, NULL, 0 }
};

/* ─── API publique ────────────────────────────────────────────────────────── */

leo_error_t leo_file_transfer_run(leo_conn_t *conn, const char *cmd_id,
                                   const char *download_url,
                                   const char *filename,
                                   const char *expected_sha256_hex,
                                   uint64_t expected_size,
                                   int timeout_secs,
                                   char *final_path_out, size_t final_path_out_sz,
                                   char *err_out, size_t err_out_sz) {
    err_out[0] = '\0';

    if (!_filename_valid(filename)) {
        snprintf(err_out, err_out_sz, "Nom de fichier invalide");
        return LEO_ERR_PROTOCOL;
    }

    bool use_ssl;
    char host[256];
    char path[LEO_FILE_URL_MAX_LEN];
    int  port;
    if (!leo_http_parse_url(download_url, &use_ssl, host, sizeof(host), &port, path, sizeof(path))) {
        snprintf(err_out, err_out_sz, "URL de téléchargement invalide");
        return LEO_ERR_PROTOCOL;
    }

    if (!_ensure_workdir()) {
        snprintf(err_out, err_out_sz, "Impossible de créer le répertoire de travail (%s)", LEO_WORKDIR);
        return LEO_ERR_SYSTEM;
    }

    char part_path[600];
    snprintf(part_path, sizeof(part_path), "%s%s.part", LEO_WORKDIR, cmd_id);

    _dl_ctx_t dl;
    memset(&dl, 0, sizeof(dl));
    dl.conn        = conn;
    dl.bytes_total = expected_size;
    strncpy(dl.cmd_id, cmd_id, sizeof(dl.cmd_id) - 1);

    dl.fp = fopen(part_path, "wb");
    if (!dl.fp) {
        snprintf(err_out, err_out_sz, "Impossible de créer '%s' : %s", part_path, strerror(errno));
        return LEO_ERR_SYSTEM;
    }

    dl.mdctx = EVP_MD_CTX_new();
    if (!dl.mdctx || EVP_DigestInit_ex(dl.mdctx, EVP_sha256(), NULL) != 1) {
        fclose(dl.fp);
        remove(part_path);
        if (dl.mdctx) EVP_MD_CTX_free(dl.mdctx);
        snprintf(err_out, err_out_sz, "Initialisation SHA-256 échouée");
        return LEO_ERR_SYSTEM;
    }

    _report_progress(&dl, LEO_FILE_TRANSFER_DOWNLOADING, NULL);

    struct lws_context_creation_info info = {0};
    info.port      = CONTEXT_PORT_NO_LISTEN;
    info.protocols = _dl_protocols;
    info.options   = LWS_SERVER_OPTION_DO_SSL_GLOBAL_INIT;
    info.user      = &dl;

    struct lws_context *ctx = lws_create_context(&info);
    if (!ctx) {
        fclose(dl.fp);
        remove(part_path);
        EVP_MD_CTX_free(dl.mdctx);
        snprintf(err_out, err_out_sz, "Impossible de créer le contexte HTTP libwebsockets");
        return LEO_ERR_NETWORK;
    }

    struct lws_client_connect_info ci = {0};
    ci.context        = ctx;
    ci.address        = host;
    ci.port           = port;
    ci.path           = path;
    ci.host           = host;
    ci.origin         = host;
    ci.method         = "GET";
    ci.protocol       = _dl_protocols[0].name;
    ci.ssl_connection = use_ssl ? LCCSCF_USE_SSL : 0;

    struct lws *wsi = lws_client_connect_via_info(&ci);
    if (!wsi) {
        lws_context_destroy(ctx);
        fclose(dl.fp);
        remove(part_path);
        EVP_MD_CTX_free(dl.mdctx);
        snprintf(err_out, err_out_sz, "Connexion au serveur de fichiers échouée (%s:%d)", host, port);
        return LEO_ERR_NETWORK;
    }

    int effective_timeout = timeout_secs > _DL_WRITE_TIMEOUT_SEC_MIN ? timeout_secs : _DL_WRITE_TIMEOUT_SEC_MIN;
    struct timespec deadline;
    clock_gettime(CLOCK_REALTIME, &deadline);
    deadline.tv_sec += effective_timeout;

    leo_error_t rc = LEO_OK;
    while (!dl.done) {
        if (lws_service(ctx, 50) < 0) {
            dl.failed = true;
            snprintf(dl.err_buf, sizeof(dl.err_buf), "Erreur du client HTTP pendant le transfert");
            break;
        }
        struct timespec now;
        clock_gettime(CLOCK_REALTIME, &now);
        if (now.tv_sec > deadline.tv_sec) {
            dl.failed = true;
            rc = LEO_ERR_TIMEOUT;
            snprintf(dl.err_buf, sizeof(dl.err_buf), "Timeout du transfert (%ds)", effective_timeout);
            break;
        }
    }

    lws_context_destroy(ctx);
    fclose(dl.fp);

    if (dl.failed || dl.http_status != 200) {
        EVP_MD_CTX_free(dl.mdctx);
        remove(part_path);
        strncpy(err_out, dl.err_buf, err_out_sz - 1);
        err_out[err_out_sz - 1] = '\0';
        _report_progress(&dl, LEO_FILE_TRANSFER_FAILED, err_out);
        return rc != LEO_OK ? rc : LEO_ERR_NETWORK;
    }

    /* Vérification du SHA-256 AVANT le renommage vers l'emplacement final :
     * un fichier corrompu/incomplet reste un ".part" et n'est jamais exposé
     * sous son nom définitif (voir file_transfer.h). */
    _report_progress(&dl, LEO_FILE_TRANSFER_VERIFYING, NULL);

    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int  digest_len = 0;
    EVP_DigestFinal_ex(dl.mdctx, digest, &digest_len);
    EVP_MD_CTX_free(dl.mdctx);

    char digest_hex[LEO_FILE_SHA256_HEX_LEN];
    for (unsigned int i = 0; i < digest_len && i * 2 < sizeof(digest_hex) - 1; i++)
        snprintf(digest_hex + i * 2, 3, "%02x", digest[i]);
    digest_hex[sizeof(digest_hex) - 1] = '\0';

    if (expected_sha256_hex && expected_sha256_hex[0] != '\0' &&
        strcasecmp(digest_hex, expected_sha256_hex) != 0) {
        remove(part_path);
        snprintf(err_out, err_out_sz, "Empreinte SHA-256 non correspondante (attendue %s, obtenue %s)",
                  expected_sha256_hex, digest_hex);
        _report_progress(&dl, LEO_FILE_TRANSFER_FAILED, err_out);
        return LEO_ERR_PROTOCOL;
    }

    char final_path[600];
    snprintf(final_path, sizeof(final_path), "%s%s", LEO_WORKDIR, filename);
    if (rename(part_path, final_path) != 0) {
        remove(part_path);
        snprintf(err_out, err_out_sz, "Renommage vers '%s' échoué : %s", final_path, strerror(errno));
        _report_progress(&dl, LEO_FILE_TRANSFER_FAILED, err_out);
        return LEO_ERR_SYSTEM;
    }

    strncpy(final_path_out, final_path, final_path_out_sz - 1);
    final_path_out[final_path_out_sz - 1] = '\0';

    dl.last_reported_pct = 100;
    _report_progress(&dl, LEO_FILE_TRANSFER_COMPLETED, NULL);

    LOG_INFO("Fichier transféré : %s (%llu octets, sha256=%s)",
             final_path, (unsigned long long)dl.bytes_received, digest_hex);

    return LEO_OK;
}
