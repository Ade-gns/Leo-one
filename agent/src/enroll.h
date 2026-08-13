/**
 * enroll.h — Enrollment initial de l'agent auprès du backend
 *
 * Avant son premier enrollment, un agent ne possède ni agent_id/tenant_id,
 * ni certificat client mTLS : agent.conf n'existe pas encore. Ce module lit
 * un fichier de bootstrap (token d'enrollment + URL de l'API REST, déposés
 * par l'installeur) et échange le token contre une identité complète :
 * POST /api/v1/enroll, réception du certificat client signé par la CA
 * interne, puis écriture de agent.conf et des fichiers PEM (voir
 * leo_crypto_save_cert_key). Après ça, l'agent démarre normalement (voir
 * agent.c:leo_agent_start) en se connectant en WSS/mTLS comme tout agent
 * déjà enrôlé.
 *
 * Contrairement à la connexion WSS (connection.c), cet échange n'a pas de
 * certificat client à présenter (c'est justement ce qu'on obtient) : c'est
 * une requête HTTP simple vers l'API REST, pas le listener WSS mTLS.
 */
#ifndef LEO_ENROLL_H
#define LEO_ENROLL_H

#include "../include/leo_agent.h"

/**
 * Effectue l'enrollment complet de l'agent :
 *   1. Charge le token d'enrollment et l'URL de l'API REST depuis
 *      bootstrap_path (format INI, clés "enrollment_token"/"api_endpoint").
 *   2. Détecte hostname/OS/architecture/hardware_id de la machine locale.
 *   3. POST /api/v1/enroll avec ces informations.
 *   4. Sauvegarde le certificat client reçu (leo_crypto_save_cert_key).
 *   5. Remplit out_cfg et l'écrit dans config_path (agent.conf).
 *
 * @param bootstrap_path  Chemin du fichier de bootstrap
 * @param config_path     Chemin où écrire agent.conf une fois enrôlé
 * @param out_cfg         Rempli avec la configuration résultante si LEO_OK
 * @return LEO_OK, ou LEO_ERR_CONFIG/LEO_ERR_NETWORK/LEO_ERR_PROTOCOL selon
 *         l'étape qui a échoué.
 */
leo_error_t leo_enroll(const char *bootstrap_path, const char *config_path,
                        leo_config_t *out_cfg);

#endif /* LEO_ENROLL_H */
