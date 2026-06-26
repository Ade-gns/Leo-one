/**
 * crypto_linux.h — Interface cryptographique de l'agent Leo-One (Linux)
 *
 * Fournit le chargement/sauvegarde des certificats PEM client et la
 * vérification de l'empreinte SHA-256 du certificat CA via OpenSSL.
 */
#ifndef LEO_CRYPTO_LINUX_H
#define LEO_CRYPTO_LINUX_H

#include "../../include/leo_agent.h"

#define LEO_CERT_BUF_SIZE  8192
#define LEO_KEY_BUF_SIZE   8192

/**
 * Charge le certificat client et la clé privée depuis les fichiers PEM.
 * Les buffers doivent être de taille LEO_CERT_BUF_SIZE / LEO_KEY_BUF_SIZE.
 * @param cert_pem_out  Buffer de sortie pour le certificat PEM
 * @param cert_sz       Taille du buffer cert (>= LEO_CERT_BUF_SIZE)
 * @param key_pem_out   Buffer de sortie pour la clé privée PEM
 * @param key_sz        Taille du buffer clé (>= LEO_KEY_BUF_SIZE)
 * @return LEO_OK ou LEO_ERR_TLS si lecture impossible
 */
leo_error_t leo_crypto_load_cert_key(char *cert_pem_out, size_t cert_sz,
                                      char *key_pem_out,  size_t key_sz);

/**
 * Sauvegarde le certificat et la clé dans les fichiers configurés.
 * Crée le répertoire LEO_CERTS_DIR si nécessaire.
 * @return LEO_OK ou LEO_ERR_TLS si écriture impossible
 */
leo_error_t leo_crypto_save_cert_key(const char *cert_pem, const char *key_pem);

/**
 * Calcule le SHA-256 (DER) du certificat CA passé en PEM et compare
 * avec l'empreinte hexadécimale attendue.
 * @param ca_cert_pem  Contenu PEM du certificat CA
 * @param expected_fp  Empreinte SHA-256 hex attendue (64 caractères lowercase)
 * @return LEO_OK si correspondance, LEO_ERR_TLS si différence ou erreur OpenSSL
 */
leo_error_t leo_crypto_verify_ca_fingerprint(const char *ca_cert_pem,
                                              const char *expected_fp);

#endif /* LEO_CRYPTO_LINUX_H */
