/**
 * crypto_win.c — Opérations cryptographiques de l'agent Leo-One (Windows)
 *
 * Fournit :
 *  - leo_crypto_load_cert_key  : lecture des fichiers PEM (cert + clé privée)
 *  - leo_crypto_save_cert_key  : écriture des fichiers PEM (création répertoire)
 *  - leo_crypto_verify_ca_fingerprint : vérification SHA-256 du CA via OpenSSL
 *
 * Miroir de platform/linux/crypto_linux.c : même logique OpenSSL (déjà
 * cross-platform), seuls les appels système (répertoires, permissions)
 * diffèrent — pas d'équivalent chmod/fchmod sous Windows (ACL NTFS plutôt
 * que bits rwx), la protection du répertoire des certificats repose sur les
 * ACL par défaut de C:\ProgramData (accessible en écriture aux admins/
 * SYSTEM uniquement), pas sur des permissions POSIX émulées ici.
 */
#include "crypto_win.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

#include <windows.h>

/* OpenSSL — utilisé uniquement pour la vérification d'empreinte */
#include <openssl/x509.h>
#include <openssl/pem.h>
#include <openssl/evp.h>
#include <openssl/bio.h>
#include <openssl/err.h>

/* mingw expose _stricmp (MSVCRT) dans <string.h> ; pas de strcasecmp
 * garanti selon les versions de mingw-w64. */
#define strcasecmp _stricmp

/* ─── Helpers privés ─────────────────────────────────────────────────────── */

/**
 * Lit le contenu d'un fichier dans un buffer.
 * @param path     Chemin du fichier
 * @param buf      Buffer de destination
 * @param buf_max  Taille maximale (incluant '\0')
 * @return Nombre d'octets lus, ou -1 en cas d'erreur
 */
static long _read_file(const char *path, char *buf, size_t buf_max) {
    FILE *fp = fopen(path, "rb");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir '%s' : %s", path, strerror(errno));
        return -1;
    }

    size_t total = 0;
    size_t n;
    while ((n = fread(buf + total, 1, buf_max - 1 - total, fp)) > 0) {
        total += n;
        if (total >= buf_max - 1) {
            LOG_WARN("Fichier '%s' tronqué à %zu octets (buffer plein)", path, buf_max - 1);
            break;
        }
    }

    if (ferror(fp)) {
        LOG_ERROR("Erreur de lecture sur '%s' : %s", path, strerror(errno));
        fclose(fp);
        return -1;
    }

    fclose(fp);
    buf[total] = '\0';
    return (long)total;
}

/**
 * Écrit un buffer dans un fichier (mode écriture, remplace si existant).
 * @return 0 si succès, -1 si erreur
 */
static int _write_file(const char *path, const char *content) {
    FILE *fp = fopen(path, "wb");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir '%s' en écriture : %s", path, strerror(errno));
        return -1;
    }

    size_t len     = strlen(content);
    size_t written = fwrite(content, 1, len, fp);
    fclose(fp);

    if (written != len) {
        LOG_ERROR("Écriture incomplète dans '%s' (%zu/%zu octets)", path, written, len);
        return -1;
    }

    return 0;
}

/**
 * Crée récursivement les répertoires d'un chemin (équivalent de mkdir -p),
 * chemin séparé par '\' (ex: LEO_CERTS_DIR = "C:\\ProgramData\\LeoOne\\certs\\").
 * Modifie path temporairement (remise en état après).
 */
static int _mkdir_p(const char *path) {
    char tmp[512];
    snprintf(tmp, sizeof(tmp), "%s", path);

    size_t len = strlen(tmp);
    if (len > 0 && tmp[len - 1] == '\\') {
        tmp[len - 1] = '\0';
        len--;
    }

    /* Créer chaque composant du chemin. Démarre après la lettre de lecteur
     * ("C:") : CreateDirectoryA sur un simple "C:" échoue et n'a de toute
     * façon aucun sens (la racine du lecteur existe déjà). */
    size_t start = (len >= 2 && tmp[1] == ':') ? 2 : 0;

    for (size_t i = start; i <= len; i++) {
        if (tmp[i] == '\\' || tmp[i] == '\0') {
            if (i == start) continue;  /* backslash immédiatement après "C:" */
            char saved = tmp[i];
            tmp[i] = '\0';
            if (!CreateDirectoryA(tmp, NULL) && GetLastError() != ERROR_ALREADY_EXISTS) {
                LOG_ERROR("CreateDirectoryA('%s') échoué (code %lu)", tmp, GetLastError());
                return -1;
            }
            tmp[i] = saved;
        }
    }
    return 0;
}

/**
 * Convertit un tableau d'octets en chaîne hexadécimale minuscule.
 * @param bytes   Données à convertir
 * @param len     Nombre d'octets
 * @param hex_out Buffer de sortie (doit être au moins 2*len+1 octets)
 */
static void _bytes_to_hex(const unsigned char *bytes, size_t len, char *hex_out) {
    for (size_t i = 0; i < len; i++) {
        snprintf(hex_out + i * 2, 3, "%02x", bytes[i]);
    }
    hex_out[len * 2] = '\0';
}

/**
 * Calcule le SHA-256 du DER d'un certificat X509 déjà parsé, en hexadécimal.
 * @param cert     Certificat (non libéré ici — reste à la charge de l'appelant)
 * @param out_hex  Buffer de sortie, doit faire au moins 65 octets
 * @return true si succès, false en cas d'erreur OpenSSL
 */
static bool _x509_sha256_hex(X509 *cert, char out_hex[65]) {
    unsigned char *der_buf = NULL;
    int der_len = i2d_X509(cert, &der_buf);
    if (der_len <= 0 || !der_buf) {
        LOG_ERROR("i2d_X509 échoué : impossible d'exporter en DER");
        ERR_clear_error();
        return false;
    }

    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int  digest_len = 0;
    int ok = EVP_Digest(der_buf, (size_t)der_len,
                        digest, &digest_len,
                        EVP_sha256(), NULL);
    OPENSSL_free(der_buf);

    if (!ok) {
        LOG_ERROR("EVP_Digest (SHA-256) échoué");
        ERR_clear_error();
        return false;
    }

    _bytes_to_hex(digest, digest_len, out_hex);
    return true;
}

/* ─── API publique ───────────────────────────────────────────────────────── */

leo_error_t leo_crypto_load_cert_key(char *cert_pem_out, size_t cert_sz,
                                      char *key_pem_out,  size_t key_sz)
{
    if (!cert_pem_out || cert_sz == 0 || !key_pem_out || key_sz == 0)
        return LEO_ERR_TLS;

    long cn = _read_file(LEO_CLIENT_CERT_FILE, cert_pem_out, cert_sz);
    if (cn < 0) {
        LOG_ERROR("Impossible de charger le certificat client depuis '%s'",
                  LEO_CLIENT_CERT_FILE);
        return LEO_ERR_TLS;
    }

    long kn = _read_file(LEO_CLIENT_KEY_FILE, key_pem_out, key_sz);
    if (kn < 0) {
        LOG_ERROR("Impossible de charger la clé privée depuis '%s'",
                  LEO_CLIENT_KEY_FILE);
        return LEO_ERR_TLS;
    }

    LOG_INFO("Certificat client chargé (%ld octets), clé privée (%ld octets)", cn, kn);
    return LEO_OK;
}

leo_error_t leo_crypto_save_cert_key(const char *cert_pem, const char *key_pem) {
    if (!cert_pem || !key_pem)
        return LEO_ERR_TLS;

    /* Créer le répertoire des certificats si nécessaire */
    if (_mkdir_p(LEO_CERTS_DIR) != 0) {
        LOG_ERROR("Impossible de créer le répertoire '%s'", LEO_CERTS_DIR);
        return LEO_ERR_TLS;
    }

    if (_write_file(LEO_CLIENT_CERT_FILE, cert_pem) != 0) {
        LOG_ERROR("Échec de la sauvegarde du certificat client");
        return LEO_ERR_TLS;
    }

    if (_write_file(LEO_CLIENT_KEY_FILE, key_pem) != 0) {
        LOG_ERROR("Échec de la sauvegarde de la clé privée");
        return LEO_ERR_TLS;
    }

    LOG_INFO("Certificat et clé privée sauvegardés dans '%s'", LEO_CERTS_DIR);
    return LEO_OK;
}

leo_error_t leo_crypto_verify_ca_fingerprint(const char *ca_cert_pem,
                                              const char *expected_fp)
{
    if (!ca_cert_pem || !expected_fp)
        return LEO_ERR_TLS;

    /* Vérification basique de la longueur de l'empreinte (SHA-256 = 64 hex chars) */
    if (strlen(expected_fp) != 64) {
        LOG_ERROR("Empreinte CA invalide : longueur %zu (attendu 64)", strlen(expected_fp));
        return LEO_ERR_TLS;
    }

    /* Créer un BIO mémoire pour parser le PEM */
    BIO *bio = BIO_new_mem_buf(ca_cert_pem, -1);
    if (!bio) {
        LOG_ERROR("BIO_new_mem_buf échoué");
        return LEO_ERR_TLS;
    }

    /* Parser le certificat X.509 PEM */
    X509 *cert = PEM_read_bio_X509(bio, NULL, NULL, NULL);
    BIO_free(bio);

    if (!cert) {
        LOG_ERROR("PEM_read_bio_X509 échoué : impossible de parser le certificat CA");
        ERR_clear_error();
        return LEO_ERR_TLS;
    }

    /* Calculer le SHA-256 du DER */
    char computed_fp[65];
    bool computed = _x509_sha256_hex(cert, computed_fp);
    X509_free(cert);

    if (!computed) return LEO_ERR_TLS;

    LOG_DEBUG("Empreinte CA calculée  : %s", computed_fp);
    LOG_DEBUG("Empreinte CA attendue  : %s", expected_fp);

    /* Comparaison insensible à la casse (expected peut être uppercase) */
    if (strcasecmp(computed_fp, expected_fp) != 0) {
        LOG_ERROR("Empreinte CA non correspondante — connexion refusée");
        return LEO_ERR_TLS;
    }

    LOG_INFO("Empreinte CA vérifiée avec succès");
    return LEO_OK;
}

bool leo_crypto_x509_fingerprint_matches(X509 *cert, const char *expected_fp) {
    if (!cert || !expected_fp) return false;

    if (strlen(expected_fp) != 64) {
        LOG_ERROR("Empreinte attendue invalide : longueur %zu (attendu 64)",
                  strlen(expected_fp));
        return false;
    }

    char computed_fp[65];
    if (!_x509_sha256_hex(cert, computed_fp)) return false;

    return strcasecmp(computed_fp, expected_fp) == 0;
}
