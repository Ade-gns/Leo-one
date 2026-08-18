/**
 * leo_agent.h — Types globaux et constantes de l'agent Leo-One
 *
 * Inclus par tous les modules C de l'agent.
 * Aucune dépendance externe dans ce header.
 */
#ifndef LEO_AGENT_H
#define LEO_AGENT_H

#include <stdint.h>
#include <stdbool.h>
#include <stddef.h>

/* ─────────────────────────────────────────────────────────────────────────
 * Version et paramètres protocole
 * ───────────────────────────────────────────────────────────────────────── */
#define LEO_AGENT_VERSION        "1.0.0"
#define LEO_PROTOCOL_VERSION     1

#define LEO_HEARTBEAT_INTERVAL_SEC  10800   /* 3 hours for low resource consumption */
#define LEO_METRICS_INTERVAL_SEC    300     /* 5 minutes for low resource consumption */
#define LEO_PATCH_CHECK_INTERVAL_SEC 21600  /* 6 hours : interroger le gestionnaire de
                                              * paquets / Windows Update est plus coûteux
                                              * qu'un heartbeat, pas besoin d'une fraîcheur
                                              * à la minute près pour des correctifs */
#define LEO_RECONNECT_INIT_MS     5000
#define LEO_RECONNECT_STEP_MS     5000
#define LEO_RECONNECT_MAX_MS     60000

#define LEO_MAX_MSG_SIZE         65536   /* taille max d'un message WS (64 KB) */
#define LEO_SEND_QUEUE_DEPTH        32   /* profondeur de la file d'envoi thread-safe */
#define LEO_UUID_STR_LEN            37   /* UUID v4 + \0 */

/* ─────────────────────────────────────────────────────────────────────────
 * Chemins de fichiers (Windows / POSIX)
 * ───────────────────────────────────────────────────────────────────────── */
#ifdef _WIN32
#  define LEO_CONFIG_DIR          "C:\\ProgramData\\LeoOne\\"
#  define LEO_CERTS_DIR           "C:\\ProgramData\\LeoOne\\certs\\"
#  define LEO_LOG_PATH            "C:\\ProgramData\\LeoOne\\logs\\agent.log"
#  define LEO_WORKDIR             "C:\\ProgramData\\LeoOne\\work\\"
#else
#  define LEO_CONFIG_DIR          "/opt/leo-one/"
#  define LEO_CERTS_DIR           "/opt/leo-one/certs/"
#  define LEO_LOG_PATH            "/opt/leo-one/logs/agent.log"
#  define LEO_WORKDIR             "/opt/leo-one/work/"
#endif

#define LEO_CONFIG_FILE           LEO_CONFIG_DIR "agent.conf"
#define LEO_BOOTSTRAP_FILE        LEO_CONFIG_DIR "agent_bootstrap.conf"
#define LEO_CLIENT_CERT_FILE      LEO_CERTS_DIR  "client_cert.pem"
#define LEO_CLIENT_KEY_FILE       LEO_CERTS_DIR  "client_key.pem"
#define LEO_CA_FINGERPRINT_FILE   LEO_CERTS_DIR  "ca.fingerprint"

/* ─────────────────────────────────────────────────────────────────────────
 * Types de messages (protocole WSS JSON)
 * ───────────────────────────────────────────────────────────────────────── */
typedef enum {
    /* Sortants : agent → backend */
    LEO_MSG_HELLO              =   1,
    LEO_MSG_HEARTBEAT          =   2,
    LEO_MSG_METRICS            =   3,
    LEO_MSG_INVENTORY          =   4,
    LEO_MSG_CMD_RESULT         =   5,
    LEO_MSG_LOG                =   6,
    LEO_MSG_PONG               =   7,
    LEO_MSG_PATCH_INVENTORY    =   8,   /* agent→backend : liste des patchs disponibles */
    LEO_MSG_FILE_TRANSFER_PROGRESS = 9, /* agent→backend : avancement d'un téléchargement en cours */
    /* Entrants : backend → agent */
    LEO_MSG_HELLO_ACK          = 100,
    LEO_MSG_EXEC_SCRIPT        = 101,
    LEO_MSG_INSTALL_PKG        = 102,
    LEO_MSG_REBOOT             = 103,
    LEO_MSG_COLLECT_INVENTORY  = 104,
    LEO_MSG_PING               = 105,
    LEO_MSG_CONFIG_UPDATE      = 106,
    LEO_MSG_FORCE_HEARTBEAT    = 107,   /* backend→agent : force immediate heartbeat */
    LEO_MSG_INSTALL_PATCHES    = 108,   /* backend→agent : installer une sélection de patchs */
    LEO_MSG_FILE_TRANSFER      = 109,   /* backend→agent : télécharger un fichier depuis une URL signée */
    /* Sentinel */
    LEO_MSG_UNKNOWN            =  -1
} leo_msg_type_t;

/* ─────────────────────────────────────────────────────────────────────────
 * Configuration persistante de l'agent (agent.conf)
 * ───────────────────────────────────────────────────────────────────────── */
typedef struct {
    char agent_id[LEO_UUID_STR_LEN];
    char tenant_id[LEO_UUID_STR_LEN];
    char ws_endpoint[512];          /* wss://rmm.example.com/ws/agent */
    char hostname[256];
    char os_name[32];               /* "windows" | "linux" | "macos" */
    char os_version[128];
    char arch[16];                  /* "amd64" | "arm64" */
    char hardware_id[LEO_UUID_STR_LEN];
    char ca_fingerprint[65];        /* SHA-256 hex du CA interne */
    int  metrics_interval_sec;      /* override serveur, défaut 60 */
    int  heartbeat_interval_sec;    /* override serveur, défaut 30 */
} leo_config_t;

/* ─────────────────────────────────────────────────────────────────────────
 * Snapshot de métriques système
 * ───────────────────────────────────────────────────────────────────────── */
#define LEO_MAX_CPU_CORES 256

typedef struct {
    double   cpu_total_percent;
    double   cpu_per_core[LEO_MAX_CPU_CORES];
    int      cpu_core_count;
    uint64_t ram_total_bytes;
    uint64_t ram_used_bytes;
    uint64_t ram_available_bytes;
    uint64_t disk_total_bytes;
    uint64_t disk_used_bytes;
    uint64_t net_bytes_in;
    uint64_t net_bytes_out;
    uint32_t process_count;
    uint64_t timestamp_ms;
} leo_metrics_t;

/* ─────────────────────────────────────────────────────────────────────────
 * Inventaire matériel / logiciel (LEO_MSG_INVENTORY)
 * ───────────────────────────────────────────────────────────────────────── */

/** Nombre max d'entrées logicielles remontées par collecte. Borne la taille
 *  du message WS (LEO_MAX_MSG_SIZE = 64 Ko) : au-delà, le message INVENTORY
 *  complet (matériel + logiciel) échouerait à sérialiser et serait perdu —
 *  150 × ~300 octets JSON/entrée (pire cas réaliste) reste sous ~45 Ko, avec
 *  de la marge pour l'objet "hardware" et l'enveloppe. Un système avec plus
 *  de paquets voit sa liste tronquée plutôt que l'inventaire entier perdu. */
#define LEO_INVENTORY_MAX_SW_ITEMS  150
#define LEO_INVENTORY_MAX_SOCKETS    16

typedef struct {
    char     cpu_model[128];
    int      cpu_cores;          /* 0 si indéterminable (ex: cpuinfo sans "physical id") */
    int      cpu_threads;        /* toujours renseigné : sysconf(_SC_NPROCESSORS_ONLN) */
    uint64_t ram_total_bytes;
    int      disk_count;         /* nombre de disques physiques (hors loop/optique) */
    char     bios_version[64];
    char     bios_vendor[64];
    char     motherboard[128];
    char     serial_number[128]; /* souvent vide si l'agent ne tourne pas en root */
} leo_hw_inventory_t;

typedef struct {
    char name[128];
    char version[64];
    char publisher[128];
    char install_path[256];
} leo_sw_item_t;

/* ─────────────────────────────────────────────────────────────────────────
 * Patchs / mises à jour système (LEO_MSG_PATCH_INVENTORY, LEO_MSG_INSTALL_PATCHES)
 * ───────────────────────────────────────────────────────────────────────── */

/** Nombre max de patchs remontés par collecte — même raisonnement que
 *  LEO_INVENTORY_MAX_SW_ITEMS pour la taille du message WS. */
#define LEO_INVENTORY_MAX_PATCHES  200
#define LEO_PATCH_ID_MAX_LEN       160  /* nom+version de paquet (Linux) ou "KB1234567" (Windows) */
#define LEO_PATCH_TITLE_MAX_LEN    256

/** Borne le nombre de patchs installables en une seule commande
 *  LEO_MSG_INSTALL_PATCHES — dimensionne aussi les buffers argv des
 *  implémentations platform/ (voir patches_linux.c, patches_win.c). Même
 *  ordre de grandeur que LEO_PKG_MAX_COUNT (agent.c, INSTALL_PKG). */
#define LEO_PATCH_INSTALL_MAX_COUNT  64

/* Ordre croissant volontaire (optional < important < critical) : permet des
 * comparaisons numériques directes côté serveur/UI (ex: "au moins important"). */
typedef enum {
    LEO_PATCH_SEVERITY_OPTIONAL  = 0,
    LEO_PATCH_SEVERITY_IMPORTANT = 1,
    LEO_PATCH_SEVERITY_CRITICAL  = 2
} leo_patch_severity_t;

typedef struct {
    /* Identifiant stable côté OS : nom de paquet Debian/RPM (Linux) ou
     * identifiant KB (Windows) — utilisé tel quel par le backend comme
     * native_id, et renvoyé par LEO_MSG_INSTALL_PATCHES pour désigner les
     * patchs à installer (voir leo_patches_install). */
    char                  id[LEO_PATCH_ID_MAX_LEN];
    char                  title[LEO_PATCH_TITLE_MAX_LEN];
    leo_patch_severity_t  severity;
    uint64_t              size_bytes;  /* 0 si indéterminable (best-effort) */
} leo_patch_t;

/* ─────────────────────────────────────────────────────────────────────────
 * Transfert de fichiers (LEO_MSG_FILE_TRANSFER, LEO_MSG_FILE_TRANSFER_PROGRESS)
 * ───────────────────────────────────────────────────────────────────────── */

#define LEO_FILE_URL_MAX_LEN       1024  /* URL signée, avec token en query string */
#define LEO_FILE_NAME_MAX_LEN       255  /* nom de fichier final dans LEO_WORKDIR */
#define LEO_FILE_SHA256_HEX_LEN      65  /* 64 caractères hex + \0 */

/* Reflète le statut publié dans LEO_MSG_FILE_TRANSFER_PROGRESS — voir
 * patch_status côté backend pour le même style de cycle de vie textuel. */
typedef enum {
    LEO_FILE_TRANSFER_DOWNLOADING = 0,
    LEO_FILE_TRANSFER_VERIFYING,
    LEO_FILE_TRANSFER_COMPLETED,
    LEO_FILE_TRANSFER_FAILED
} leo_file_transfer_status_t;

/* ─────────────────────────────────────────────────────────────────────────
 * État interne de l'agent (machine d'état)
 * ───────────────────────────────────────────────────────────────────────── */
typedef enum {
    LEO_STATE_INIT         = 0,
    LEO_STATE_ENROLLING,
    LEO_STATE_CONNECTING,
    LEO_STATE_CONNECTED,
    LEO_STATE_RECONNECTING,
    LEO_STATE_STOPPING
} leo_agent_state_t;

/* ─────────────────────────────────────────────────────────────────────────
 * Codes d'erreur internes
 * ───────────────────────────────────────────────────────────────────────── */
typedef enum {
    LEO_OK                  =  0,
    LEO_ERR_CONFIG          = -1,
    LEO_ERR_NETWORK         = -2,
    LEO_ERR_TLS             = -3,
    LEO_ERR_PROTOCOL        = -4,
    LEO_ERR_SYSTEM          = -5,
    LEO_ERR_QUEUE_FULL      = -6,
    LEO_ERR_TIMEOUT         = -7
} leo_error_t;

#endif /* LEO_AGENT_H */
