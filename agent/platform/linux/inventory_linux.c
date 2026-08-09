/**
 * inventory_linux.c — Collecte d'inventaire matériel/logiciel sous Linux
 *
 * Sources de données :
 *   CPU        : /proc/cpuinfo ("model name", "physical id", "cpu cores")
 *   RAM        : /proc/meminfo ("MemTotal")
 *   Disques    : /sys/block/ (exclut loop*, ram*, sr* — pas des disques physiques)
 *   BIOS/carte : /sys/class/dmi/id/{bios_version,bios_vendor,board_name,product_serial}
 *                (nécessite root pour product_serial sur la plupart des distros —
 *                l'agent tourne déjà en root pour INSTALL_PKG/REBOOT)
 *   Logiciels  : dpkg-query (Debian/Ubuntu uniquement pour l'instant)
 *
 * Toute source indisponible laisse le champ correspondant vide/à zéro plutôt
 * que de faire échouer la collecte entière — l'inventaire est best-effort.
 */
#include "../../src/inventory.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <dirent.h>

/* ─── CPU ─────────────────────────────────────────────────────────────────── */

/**
 * Parcourt /proc/cpuinfo pour le modèle, le nombre de threads (= nombre de
 * lignes "processor") et une estimation du nombre de cœurs physiques.
 *
 * L'estimation des cœurs suppose des paquets homogènes : cpu_cores =
 * (valeur du champ "cpu cores") × (nombre d'"physical id" distincts). Ce
 * champ est absent sur beaucoup de VM/conteneurs cloud (chaque vCPU y
 * apparaît sans info de socket) — dans ce cas cpu_cores reste à 0 et seul
 * cpu_threads (fiable via sysconf) est renseigné.
 */
static void _read_cpuinfo(leo_hw_inventory_t *out) {
    out->cpu_threads = (int)sysconf(_SC_NPROCESSORS_ONLN);
    if (out->cpu_threads < 0) out->cpu_threads = 0;

    FILE *fp = fopen("/proc/cpuinfo", "r");
    if (!fp) {
        LOG_WARN("Impossible d'ouvrir /proc/cpuinfo");
        return;
    }

    int seen_physical_ids[LEO_INVENTORY_MAX_SOCKETS];
    int socket_count = 0;
    int cores_per_socket = 0;
    bool have_model = false;

    char line[256];
    while (fgets(line, sizeof(line), fp)) {
        char *colon = strchr(line, ':');
        if (!colon) continue;
        char *value = colon + 1;
        while (*value == ' ' || *value == '\t') value++;
        size_t vlen = strlen(value);
        while (vlen > 0 && (value[vlen-1] == '\n' || value[vlen-1] == '\r'))
            value[--vlen] = '\0';

        if (!have_model && strncmp(line, "model name", 10) == 0) {
            strncpy(out->cpu_model, value, sizeof(out->cpu_model) - 1);
            have_model = true;
        } else if (strncmp(line, "physical id", 11) == 0) {
            int pid = atoi(value);
            bool known = false;
            for (int i = 0; i < socket_count; i++) {
                if (seen_physical_ids[i] == pid) { known = true; break; }
            }
            if (!known && socket_count < LEO_INVENTORY_MAX_SOCKETS) {
                seen_physical_ids[socket_count++] = pid;
            }
        } else if (strncmp(line, "cpu cores", 9) == 0) {
            cores_per_socket = atoi(value);
        }
    }
    fclose(fp);

    if (socket_count > 0 && cores_per_socket > 0)
        out->cpu_cores = socket_count * cores_per_socket;
}

/* ─── RAM ─────────────────────────────────────────────────────────────────── */

static void _read_ram_total(leo_hw_inventory_t *out) {
    FILE *fp = fopen("/proc/meminfo", "r");
    if (!fp) {
        LOG_WARN("Impossible d'ouvrir /proc/meminfo");
        return;
    }

    unsigned long long kb;
    char line[128];
    while (fgets(line, sizeof(line), fp)) {
        if (sscanf(line, "MemTotal: %llu kB", &kb) == 1) {
            out->ram_total_bytes = (uint64_t)kb * 1024;
            break;
        }
    }
    fclose(fp);
}

/* ─── Disques ─────────────────────────────────────────────────────────────── */

/** Compte les disques physiques dans /sys/block, en excluant loopback,
 *  RAM disks et lecteurs optiques. */
static void _read_disk_count(leo_hw_inventory_t *out) {
    DIR *d = opendir("/sys/block");
    if (!d) {
        LOG_WARN("Impossible d'ouvrir /sys/block");
        return;
    }

    int count = 0;
    struct dirent *ent;
    while ((ent = readdir(d)) != NULL) {
        const char *name = ent->d_name;
        if (name[0] == '.') continue;
        if (strncmp(name, "loop", 4) == 0) continue;
        if (strncmp(name, "ram", 3) == 0) continue;
        if (strncmp(name, "sr", 2) == 0) continue;
        count++;
    }
    closedir(d);
    out->disk_count = count;
}

/* ─── BIOS / carte mère (DMI) ─────────────────────────────────────────────── */

static void _read_dmi_field(const char *path, char *out, size_t out_sz) {
    FILE *fp = fopen(path, "r");
    if (!fp) return; /* champ absent ou non lisible (pas root) — laissé vide */

    if (fgets(out, (int)out_sz, fp)) {
        size_t len = strlen(out);
        while (len > 0 && (out[len-1] == '\n' || out[len-1] == '\r'))
            out[--len] = '\0';
    }
    fclose(fp);
}

/* ─── API publique (implémente inventory.h) ─────────────────────────────── */

leo_error_t leo_inventory_collect_hw(leo_hw_inventory_t *out) {
    if (!out) return LEO_ERR_SYSTEM;
    memset(out, 0, sizeof(*out));

    _read_cpuinfo(out);
    _read_ram_total(out);
    _read_disk_count(out);

    _read_dmi_field("/sys/class/dmi/id/bios_version",   out->bios_version,   sizeof(out->bios_version));
    _read_dmi_field("/sys/class/dmi/id/bios_vendor",    out->bios_vendor,    sizeof(out->bios_vendor));
    _read_dmi_field("/sys/class/dmi/id/board_name",     out->motherboard,    sizeof(out->motherboard));
    _read_dmi_field("/sys/class/dmi/id/product_serial", out->serial_number, sizeof(out->serial_number));

    LOG_DEBUG("Inventaire matériel collecté : cpu='%s' threads=%d cores=%d ram=%lluMB disks=%d",
              out->cpu_model, out->cpu_threads, out->cpu_cores,
              (unsigned long long)(out->ram_total_bytes / (1024*1024)), out->disk_count);

    return LEO_OK;
}

int leo_inventory_collect_sw(leo_sw_item_t *out, int max_items) {
    if (!out || max_items <= 0) return -1;

    /* popen est acceptable ici : la commande est statique, pas de données
     * utilisateur (voir la même justification dans service_linux.c). */
    FILE *fp = popen("dpkg-query -W -f='${Package}\t${Version}\t${Maintainer}\n' 2>/dev/null", "r");
    if (!fp) {
        LOG_WARN("popen dpkg-query échoué — inventaire logiciel vide (dpkg absent ?)");
        return 0;
    }

    int n = 0;
    char line[512];
    while (n < max_items && fgets(line, sizeof(line), fp)) {
        size_t len = strlen(line);
        while (len > 0 && (line[len-1] == '\n' || line[len-1] == '\r'))
            line[--len] = '\0';

        char *pkg_end = strchr(line, '\t');
        if (!pkg_end) continue;
        *pkg_end = '\0';
        char *ver_start = pkg_end + 1;

        char *ver_end = strchr(ver_start, '\t');
        char *pub_start = "";
        if (ver_end) {
            *ver_end = '\0';
            pub_start = ver_end + 1;
        }

        leo_sw_item_t *item = &out[n];
        memset(item, 0, sizeof(*item));
        strncpy(item->name,      line,      sizeof(item->name) - 1);
        strncpy(item->version,   ver_start, sizeof(item->version) - 1);
        strncpy(item->publisher, pub_start, sizeof(item->publisher) - 1);
        n++;
    }

    pclose(fp);
    LOG_DEBUG("Inventaire logiciel collecté : %d paquets", n);
    return n;
}
