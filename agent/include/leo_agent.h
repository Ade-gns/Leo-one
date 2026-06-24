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

#define LEO_HEARTBEAT_INTERVAL_SEC  30
#define LEO_METRICS_INTERVAL_SEC    60
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
#else
#  define LEO_CONFIG_DIR          "/opt/leo-one/"
#  define LEO_CERTS_DIR           "/opt/leo-one/certs/"
#  define LEO_LOG_PATH            "/opt/leo-one/logs/agent.log"
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
    /* Entrants : backend → agent */
    LEO_MSG_HELLO_ACK          = 100,
    LEO_MSG_EXEC_SCRIPT        = 101,
    LEO_MSG_INSTALL_PKG        = 102,
    LEO_MSG_REBOOT             = 103,
    LEO_MSG_COLLECT_INVENTORY  = 104,
    LEO_MSG_PING               = 105,
    LEO_MSG_CONFIG_UPDATE      = 106,
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
