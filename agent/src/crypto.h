/**
 * crypto.h — Interface générique pour les opérations crypto utilisées par
 * le code commun (connection.c), implémentée par chaque module platform/ :
 *   platform/linux/crypto_linux.c
 *   platform/windows/crypto_win.c
 *   platform/macos/crypto_macos.c
 *
 * OpenSSL est déjà une dépendance cross-platform du projet (voir
 * CMakeLists.txt), donc les types X509/X509_STORE_CTX sont utilisables
 * directement ici sans casser la portabilité.
 */
#ifndef LEO_CRYPTO_H
#define LEO_CRYPTO_H

#include "../include/leo_agent.h"

#include <openssl/x509.h>
#include <stddef.h>

#define LEO_CERT_BUF_SIZE  8192
#define LEO_KEY_BUF_SIZE   8192

/**
 * Compare le SHA-256 (DER) d'un certificat X509 déjà parsé (typiquement
 * obtenu pendant le handshake TLS, dans le callback de vérification du
 * certificat serveur) avec l'empreinte hexadécimale attendue (pinning).
 * @return true si le fingerprint correspond, false sinon (ou erreur OpenSSL).
 */
bool leo_crypto_x509_fingerprint_matches(X509 *cert, const char *expected_fp);

/**
 * Charge le certificat client et la clé privée depuis les fichiers PEM
 * (LEO_CLIENT_CERT_FILE / LEO_CLIENT_KEY_FILE).
 * Les buffers doivent être de taille >= LEO_CERT_BUF_SIZE / LEO_KEY_BUF_SIZE.
 * @return LEO_OK ou LEO_ERR_TLS si lecture impossible
 */
leo_error_t leo_crypto_load_cert_key(char *cert_pem_out, size_t cert_sz,
                                      char *key_pem_out,  size_t key_sz);

/**
 * Sauvegarde le certificat et la clé dans les fichiers configurés
 * (LEO_CLIENT_CERT_FILE / LEO_CLIENT_KEY_FILE) — utilisé par l'enrollment
 * (voir enroll.c) pour persister le certificat émis par le backend.
 * Crée le répertoire LEO_CERTS_DIR si nécessaire.
 * @return LEO_OK ou LEO_ERR_TLS si écriture impossible
 */
leo_error_t leo_crypto_save_cert_key(const char *cert_pem, const char *key_pem);

#endif /* LEO_CRYPTO_H */
