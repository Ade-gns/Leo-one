/**
 * file_transfer.h — Téléchargement de fichiers depuis une URL signée fournie
 * par le backend (LEO_MSG_FILE_TRANSFER), avec vérification SHA-256 et
 * rapport d'avancement périodique (LEO_MSG_FILE_TRANSFER_PROGRESS).
 *
 * Portable — pas de module platform/ dédié : le téléchargement passe par
 * libwebsockets (déjà utilisé pour l'enrollment, voir enroll.c) et le hachage
 * par OpenSSL EVP, tous deux des dépendances cross-platform du projet.
 */
#ifndef LEO_FILE_TRANSFER_H
#define LEO_FILE_TRANSFER_H

#include "../include/leo_agent.h"
#include "connection.h"

/**
 * Télécharge download_url vers LEO_WORKDIR/<filename>, en rapportant
 * l'avancement via conn (LEO_MSG_FILE_TRANSFER_PROGRESS) et en vérifiant le
 * SHA-256 du fichier téléchargé avant de le renommer vers son emplacement
 * final — voir le commentaire sur _finalize() dans file_transfer.c pour le
 * détail de la séquence "téléchargement vers un .part → vérification →
 * renommage".
 *
 * Bloquant : appelé depuis un thread détaché par commande (voir
 * _EXEC_KIND_FILE_TRANSFER dans agent.c), jamais depuis le thread WSS.
 *
 * @param conn                 Connexion WSS pour les messages de progression
 * @param cmd_id                UUID de la commande FILE_TRANSFER
 * @param download_url          URL signée (http ou https), déjà validée non
 *                               vide par l'appelant
 * @param filename               Nom de fichier final — jamais utilisé
 *                               directement dans un chemin sans validation
 *                               (voir _filename_valid dans file_transfer.c :
 *                               rejette tout séparateur de chemin ou "..")
 * @param expected_sha256_hex    Empreinte attendue (64 car. hex), ou chaîne
 *                               vide pour ne pas vérifier
 * @param expected_size          Taille attendue en octets, 0 si inconnue
 *                               (utilisée pour le pourcentage si le serveur
 *                               ne renvoie pas Content-Length)
 * @param timeout_secs           Timeout global du transfert
 * @param final_path_out         Rempli avec le chemin final en cas de succès
 * @param err_out                Rempli avec un message d'erreur en cas d'échec
 * @return LEO_OK, LEO_ERR_NETWORK, LEO_ERR_TIMEOUT, LEO_ERR_PROTOCOL
 *         (URL/nom de fichier invalide, ou empreinte SHA-256 non
 *         correspondante), ou LEO_ERR_SYSTEM (écriture disque)
 */
leo_error_t leo_file_transfer_run(leo_conn_t *conn, const char *cmd_id,
                                   const char *download_url,
                                   const char *filename,
                                   const char *expected_sha256_hex,
                                   uint64_t expected_size,
                                   int timeout_secs,
                                   char *final_path_out, size_t final_path_out_sz,
                                   char *err_out, size_t err_out_sz);

#endif /* LEO_FILE_TRANSFER_H */
