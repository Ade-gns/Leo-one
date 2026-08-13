/**
 * crypto_linux.h — Interface cryptographique de l'agent Leo-One (Linux)
 *
 * leo_crypto_load_cert_key/leo_crypto_save_cert_key sont déclarées dans
 * l'interface générique (../../src/crypto.h), implémentées ici — ce header
 * n'ajoute que ce qui est spécifique à cette implémentation Linux.
 */
#ifndef LEO_CRYPTO_LINUX_H
#define LEO_CRYPTO_LINUX_H

#include "../../include/leo_agent.h"
#include "../../src/crypto.h"

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
