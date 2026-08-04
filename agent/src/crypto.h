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

#include <openssl/x509.h>

/**
 * Compare le SHA-256 (DER) d'un certificat X509 déjà parsé (typiquement
 * obtenu pendant le handshake TLS, dans le callback de vérification du
 * certificat serveur) avec l'empreinte hexadécimale attendue (pinning).
 * @return true si le fingerprint correspond, false sinon (ou erreur OpenSSL).
 */
bool leo_crypto_x509_fingerprint_matches(X509 *cert, const char *expected_fp);

#endif /* LEO_CRYPTO_H */
