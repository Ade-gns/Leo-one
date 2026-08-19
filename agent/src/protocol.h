/**
 * protocol.h — Sérialisation/désérialisation des messages WSS (JSON)
 *
 * Enveloppe canonique :
 *   { "v":1, "type":2, "id":"<uuid>", "ts":<ms>, "body":{...} }
 *
 * Utilise cJSON (third_party/cjson) pour la génération et le parsing.
 */
#ifndef LEO_PROTOCOL_H
#define LEO_PROTOCOL_H

#include "../include/leo_agent.h"
#include "../third_party/cjson/cJSON.h"

/* ─── Sérialisation (agent → backend) ──────────────────────────────────── */

/**
 * Construit le message HELLO initial.
 * @param cfg    Configuration de l'agent
 * @param buf    Buffer de sortie
 * @param bufsz  Taille du buffer
 * @return nombre d'octets écrits, -1 si erreur
 */
int leo_proto_build_hello(const leo_config_t *cfg, char *buf, size_t bufsz);

/**
 * Construit un message HEARTBEAT.
 * @param seq    Numéro de séquence incrémental
 */
int leo_proto_build_heartbeat(uint64_t seq, char *buf, size_t bufsz);

/**
 * Construit un message METRICS à partir d'un snapshot.
 */
int leo_proto_build_metrics(const leo_metrics_t *m, char *buf, size_t bufsz);

/**
 * Construit un message CMD_RESULT.
 * @param cmd_id    UUID de la commande
 * @param exit_code Code de sortie du processus
 * @param stdout_s  Sortie standard (peut être NULL)
 * @param stderr_s  Sortie d'erreur (peut être NULL)
 */
int leo_proto_build_cmd_result(const char *cmd_id, int exit_code,
                               const char *stdout_s, const char *stderr_s,
                               char *buf, size_t bufsz);

/**
 * Construit un message PONG (réponse à PING).
 * @param ping_id  Valeur du champ "id" du message PING reçu
 */
int leo_proto_build_pong(const char *ping_id, char *buf, size_t bufsz);

/**
 * Construit un message INVENTORY à partir d'un snapshot matériel et d'une
 * liste de logiciels installés.
 * @param hw       Snapshot matériel (jamais NULL)
 * @param sw       Tableau des logiciels installés (peut être NULL si sw_count == 0)
 * @param sw_count Nombre d'entrées valides dans sw
 */
int leo_proto_build_inventory(const leo_hw_inventory_t *hw,
                              const leo_sw_item_t *sw, int sw_count,
                              char *buf, size_t bufsz);

/**
 * Construit un message PATCH_INVENTORY à partir de la liste des mises à
 * jour disponibles (voir leo_patches_collect).
 * @param patches  Tableau des patchs (peut être NULL si count == 0)
 * @param count    Nombre d'entrées valides dans patches
 */
int leo_proto_build_patch_inventory(const leo_patch_t *patches, int count,
                                     char *buf, size_t bufsz);

/**
 * Construit un message FILE_TRANSFER_PROGRESS pour signaler l'avancement
 * d'un téléchargement en cours (voir file_transfer.h).
 * @param cmd_id         UUID de la commande FILE_TRANSFER
 * @param status         Étape courante
 * @param percent        0-100 (indéterminé si bytes_total == 0 : envoyé à 0)
 * @param bytes_received Octets déjà écrits sur disque
 * @param bytes_total    Taille totale attendue (0 si inconnue)
 * @param error_msg      Détail de l'échec si status == LEO_FILE_TRANSFER_FAILED
 *                       (peut être NULL sinon)
 */
int leo_proto_build_file_transfer_progress(const char *cmd_id,
                                            leo_file_transfer_status_t status,
                                            int percent,
                                            uint64_t bytes_received, uint64_t bytes_total,
                                            const char *error_msg,
                                            char *buf, size_t bufsz);

/**
 * Construit un message REMOTE_DESKTOP_STATUS, envoyé sur le canal de
 * contrôle (jamais sur la connexion dédiée) pour remonter au backend un
 * échec ou la fin d'une session de bureau à distance — voir
 * remote_desktop.h.
 * @param status     "failed" ou "ended"
 * @param error_msg  Détail de l'échec (peut être NULL si status == "ended")
 */
int leo_proto_build_remote_desktop_status(const char *session_id,
                                           const char *status,
                                           const char *error_msg,
                                           char *buf, size_t bufsz);

/* ─── Désérialisation (backend → agent) ──────────────────────────────────── */

/**
 * Type d'un message entrant parsé.
 */
typedef struct {
    leo_msg_type_t type;
    char           id[LEO_UUID_STR_LEN];   /* champ "id" du message */
    cJSON         *body;                   /* objet JSON body (à libérer avec cJSON_Delete) */
} leo_incoming_msg_t;

/**
 * Parse un buffer JSON reçu du backend.
 * @param json_str  Chaîne JSON brute
 * @param out       Structure à remplir
 * @return LEO_OK si parsé, LEO_ERR_PROTOCOL si malformé
 *
 * ATTENTION : out->body est alloué par cJSON, libérer avec leo_proto_msg_free().
 */
leo_error_t leo_proto_parse(const char *json_str, leo_incoming_msg_t *out);

/** Libère les ressources d'un message parsé. */
void leo_proto_msg_free(leo_incoming_msg_t *msg);

#endif /* LEO_PROTOCOL_H */
