/**
 * crypto_linux.c — Opérations cryptographiques de l'agent Leo-One (Linux)
 *
 * Fournit :
 *  - leo_crypto_load_cert_key  : lecture des fichiers PEM (cert + clé privée)
 *  - leo_crypto_save_cert_key  : écriture des fichiers PEM (création répertoire)
 *  - leo_crypto_verify_ca_fingerprint : vérification SHA-256 du CA via OpenSSL
 *
 * Utilise OpenSSL pour le calcul d'empreinte (x509.h, evp.h).
 * La lecture/écriture des fichiers PEM ne nécessite pas OpenSSL.
 */
#include "crypto_linux.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <sys/stat.h>
#include <sys/types.h>

/* OpenSSL — utilisé uniquement pour la vérification d'empreinte */
#include <openssl/x509.h>
#include <openssl/pem.h>
#include <openssl/evp.h>
#include <openssl/bio.h>
#include <openssl/err.h>

/* ─── Helpers privés ─────────────────────────────────────────────────────── */

/**
 * Lit le contenu d'un fichier dans un buffer.
 * @param path     Chemin du fichier
 * @param buf      Buffer de destination
 * @param buf_max  Taille maximale (incluant '\0')
 * @return Nombre d'octets lus, ou -1 en cas d'erreur
 */
static ssize_t _read_file(const char *path, char *buf, size_t buf_max) {
    FILE *fp = fopen(path, "r");
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
    return (ssize_t)total;
}

/**
 * Écrit un buffer dans un fichier (mode écriture, remplace si existant).
 * @return 0 si succès, -1 si erreur
 */
static int _write_file(const char *path, const char *content) {
    FILE *fp = fopen(path, "w");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir '%s' en écriture : %s", path, strerror(errno));
        return -1;
    }

    /* Droits 600 : fichier de clé privée — lecture owner uniquement */
    fchmod(fileno(fp), 0600);

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
 * Crée récursivement les répertoires d'un chemin (équivalent de mkdir -p).
 * Modifie path temporairement (remise en état après).
 */
static int _mkdir_p(const char *path) {
    /* Copie locale pour modifier sans toucher l'original */
    char tmp[512];
    snprintf(tmp, sizeof(tmp), "%s", path);

    size_t len = strlen(tmp);
    /* Supprimer le slash final éventuel */
    if (len > 0 && tmp[len - 1] == '/') {
        tmp[len - 1] = '\0';
        len--;
    }

    /* Créer chaque composant du chemin */
    for (size_t i = 1; i <= len; i++) {
        if (tmp[i] == '/' || tmp[i] == '\0') {
            char saved = tmp[i];
            tmp[i] = '\0';
            if (mkdir(tmp, 0755) != 0 && errno != EEXIST) {
                LOG_ERROR("mkdir('%s') échoué : %s", tmp, strerror(errno));
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

/* ─── API publique ───────────────────────────────────────────────────────── */

leo_error_t leo_crypto_load_cert_key(char *cert_pem_out, size_t cert_sz,
                                      char *key_pem_out,  size_t key_sz)
{
    if (!cert_pem_out || cert_sz == 0 || !key_pem_out || key_sz == 0)
        return LEO_ERR_TLS;

    ssize_t cn = _read_file(LEO_CLIENT_CERT_FILE, cert_pem_out, cert_sz);
    if (cn < 0) {
        LOG_ERROR("Impossible de charger le certificat client depuis '%s'",
                  LEO_CLIENT_CERT_FILE);
        return LEO_ERR_TLS;
    }

    ssize_t kn = _read_file(LEO_CLIENT_KEY_FILE, key_pem_out, key_sz);
    if (kn < 0) {
        LOG_ERROR("Impossible de charger la clé privée depuis '%s'",
                  LEO_CLIENT_KEY_FILE);
        return LEO_ERR_TLS;
    }

    LOG_INFO("Certificat client chargé (%zd octets), clé privée (%zd octets)", cn, kn);
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

    /* Exporter le certificat en DER (format binaire canonique) */
    unsigned char *der_buf = NULL;
    int der_len = i2d_X509(cert, &der_buf);
    X509_free(cert);

    if (der_len <= 0 || !der_buf) {
        LOG_ERROR("i2d_X509 échoué : impossible d'exporter en DER");
        ERR_clear_error();
        return LEO_ERR_TLS;
    }

    /* Calculer le SHA-256 du DER */
    unsigned char digest[EVP_MAX_MD_SIZE];
    unsigned int  digest_len = 0;

    int ok = EVP_Digest(der_buf, (size_t)der_len,
                        digest, &digest_len,
                        EVP_sha256(), NULL);
    OPENSSL_free(der_buf);

    if (!ok) {
        LOG_ERROR("EVP_Digest (SHA-256) échoué");
        ERR_clear_error();
        return LEO_ERR_TLS;
    }

    /* Convertir en hexadécimal */
    char computed_fp[65];
    _bytes_to_hex(digest, digest_len, computed_fp);

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
