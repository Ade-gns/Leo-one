/**
 * metrics_linux.c — Collecte de métriques système sous Linux
 *
 * Sources de données :
 *   CPU  : /proc/stat        (calcul différentiel entre deux lectures)
 *   RAM  : /proc/meminfo
 *   Disk : /proc/diskstats   + /proc/mounts pour les partitions
 *   Net  : /proc/net/dev
 *   Proc : /proc/loadavg     (nombre de processus)
 *
 * La mesure CPU nécessite deux snapshots séparés dans le temps.
 * On conserve le snapshot précédent dans un état statique global.
 */
#include "../../src/metrics.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <dirent.h>
#include <sys/statvfs.h>

/* ─── Structures internes pour le calcul différentiel CPU ────────────── */

#define MAX_CPUS 256

typedef struct {
    unsigned long long user;
    unsigned long long nice;
    unsigned long long system;
    unsigned long long idle;
    unsigned long long iowait;
    unsigned long long irq;
    unsigned long long softirq;
    unsigned long long steal;
} cpu_stats_t;

typedef struct {
    cpu_stats_t total;
    cpu_stats_t per_core[MAX_CPUS];
    int         core_count;
    bool        valid;
} cpu_snapshot_t;

/* État statique : snapshot précédent pour le calcul différentiel */
static cpu_snapshot_t g_prev_cpu = {0};

/* ─── Helpers privés ────────────────────────────────────────────────────── */

/** Timestamp en millisecondes (CLOCK_MONOTONIC). */
static uint64_t _now_ms(void) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (uint64_t)ts.tv_sec * 1000ULL + (uint64_t)(ts.tv_nsec / 1000000ULL);
}

/**
 * Lit /proc/stat et remplit cpu_snapshot_t.
 * Format de ligne : "cpu  user nice system idle iowait irq softirq steal ..."
 */
static bool _read_cpu_stats(cpu_snapshot_t *snap) {
    FILE *fp = fopen("/proc/stat", "r");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir /proc/stat");
        return false;
    }

    memset(snap, 0, sizeof(*snap));
    char line[256];
    int  core_idx = -1;  /* -1 = ligne "cpu" globale */

    while (fgets(line, sizeof(line), fp)) {
        if (strncmp(line, "cpu", 3) != 0) break;

        unsigned long long u, n, s, id, iow, irq, sirq, steal,
                           guest, guest_nice;
        char label[16];
        int  fields = sscanf(line,
            "%15s %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu",
            label, &u, &n, &s, &id, &iow, &irq, &sirq, &steal,
            &guest, &guest_nice);

        if (fields < 5) continue;

        cpu_stats_t *st = NULL;
        if (strcmp(label, "cpu") == 0) {
            st = &snap->total;
        } else {
            core_idx++;
            if (core_idx >= MAX_CPUS) continue;
            st = &snap->per_core[core_idx];
        }

        st->user    = u;
        st->nice    = n;
        st->system  = s;
        st->idle    = id;
        st->iowait  = iow;
        st->irq     = irq;
        st->softirq = sirq;
        st->steal   = steal;
    }

    snap->core_count = (core_idx >= 0) ? core_idx + 1 : 0;
    snap->valid      = true;
    fclose(fp);
    return true;
}

/**
 * Calcule le pourcentage d'utilisation CPU à partir de deux snapshots.
 * usage = (total_non_idle_delta) / (total_delta) * 100
 */
static double _cpu_usage(const cpu_stats_t *prev, const cpu_stats_t *curr) {
    unsigned long long prev_idle  = prev->idle + prev->iowait;
    unsigned long long curr_idle  = curr->idle + curr->iowait;
    unsigned long long prev_total = prev->user + prev->nice + prev->system
                                  + prev->idle + prev->iowait + prev->irq
                                  + prev->softirq + prev->steal;
    unsigned long long curr_total = curr->user + curr->nice + curr->system
                                  + curr->idle + curr->iowait + curr->irq
                                  + curr->softirq + curr->steal;

    unsigned long long total_delta = curr_total - prev_total;
    unsigned long long idle_delta  = curr_idle  - prev_idle;

    if (total_delta == 0) return 0.0;
    return (double)(total_delta - idle_delta) / (double)total_delta * 100.0;
}

/**
 * Lit /proc/meminfo pour RAM totale, disponible et utilisée.
 */
static bool _read_meminfo(uint64_t *total, uint64_t *available, uint64_t *used) {
    FILE *fp = fopen("/proc/meminfo", "r");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir /proc/meminfo");
        return false;
    }

    *total = *available = 0;
    char line[128];
    unsigned long long kb;

    while (fgets(line, sizeof(line), fp)) {
        if (sscanf(line, "MemTotal: %llu kB", &kb) == 1)
            *total = (uint64_t)kb * 1024;
        else if (sscanf(line, "MemAvailable: %llu kB", &kb) == 1)
            *available = (uint64_t)kb * 1024;

        if (*total && *available) break;
    }

    fclose(fp);
    *used = (*total > *available) ? (*total - *available) : 0;
    return (*total > 0);
}

/**
 * Parcourt les partitions montées et additionne usage disque.
 * On ignore les systèmes de fichiers virtuels (proc, sys, devtmpfs, tmpfs...).
 */
static bool _read_disk_usage(uint64_t *total_out, uint64_t *used_out) {
    FILE *fp = fopen("/proc/mounts", "r");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir /proc/mounts");
        return false;
    }

    *total_out = *used_out = 0;

    /* Systèmes de fichiers virtuels à ignorer */
    static const char *IGNORE_FS[] = {
        "proc", "sysfs", "devtmpfs", "tmpfs", "devpts", "securityfs",
        "cgroup", "cgroup2", "pstore", "bpf", "tracefs", "debugfs",
        "fusectl", "hugetlbfs", "mqueue", "overlay", "squashfs",
        NULL
    };

    char device[256], mount[256], fstype[64], opts[512];
    int  dummy;

    while (fscanf(fp, "%255s %255s %63s %511s %d %d",
                  device, mount, fstype, opts, &dummy, &dummy) == 6) {

        /* Ignorer les FS virtuels */
        bool skip = false;
        for (int i = 0; IGNORE_FS[i]; i++) {
            if (strcmp(fstype, IGNORE_FS[i]) == 0) { skip = true; break; }
        }
        if (skip) continue;

        struct statvfs st;
        if (statvfs(mount, &st) != 0) continue;

        uint64_t blk = (uint64_t)st.f_frsize;
        *total_out += blk * st.f_blocks;
        *used_out  += blk * (st.f_blocks - st.f_bfree);
    }

    fclose(fp);
    return true;
}

/**
 * Additionne les octets réseau IN/OUT de toutes les interfaces
 * dans /proc/net/dev (en excluant "lo").
 */
static bool _read_net_stats(uint64_t *bytes_in, uint64_t *bytes_out) {
    FILE *fp = fopen("/proc/net/dev", "r");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir /proc/net/dev");
        return false;
    }

    *bytes_in = *bytes_out = 0;

    char line[512];
    /* Sauter les deux lignes d'en-tête */
    fgets(line, sizeof(line), fp);
    fgets(line, sizeof(line), fp);

    while (fgets(line, sizeof(line), fp)) {
        char iface[32];
        unsigned long long rx_bytes, rx_pkts, rx_err, rx_drop,
                           rx_fifo, rx_frame, rx_comp, rx_mc,
                           tx_bytes;
        int n = sscanf(line,
            " %31[^:]: %llu %llu %llu %llu %llu %llu %llu %llu %llu",
            iface, &rx_bytes, &rx_pkts, &rx_err, &rx_drop,
            &rx_fifo, &rx_frame, &rx_comp, &rx_mc, &tx_bytes);

        if (n < 10) continue;
        if (strcmp(iface, "lo") == 0) continue;

        *bytes_in  += rx_bytes;
        *bytes_out += tx_bytes;
    }

    fclose(fp);
    return true;
}

/** Lit le nombre de processus depuis /proc/loadavg (champ "X/Y"). */
static uint32_t _read_process_count(void) {
    FILE *fp = fopen("/proc/loadavg", "r");
    if (!fp) return 0;

    float f1, f5, f15;
    unsigned int running, total;
    int pid;

    if (fscanf(fp, "%f %f %f %u/%u %d", &f1, &f5, &f15,
               &running, &total, &pid) >= 5) {
        fclose(fp);
        return (uint32_t)total;
    }

    fclose(fp);
    return 0;
}

/* ─── API publique (implémente metrics.h) ───────────────────────────────── */

leo_error_t leo_metrics_init(void) {
    /* Premier snapshot CPU pour initialiser l'état différentiel */
    if (!_read_cpu_stats(&g_prev_cpu)) {
        LOG_WARN("Impossible d'initialiser le snapshot CPU initial");
        /* Non bloquant : le premier collect retournera 0% CPU */
    }
    LOG_INFO("Sous-système métriques Linux initialisé (%d cœurs)",
             g_prev_cpu.core_count);
    return LEO_OK;
}

leo_error_t leo_metrics_collect(leo_metrics_t *out) {
    if (!out) return LEO_ERR_SYSTEM;

    memset(out, 0, sizeof(*out));
    out->timestamp_ms = _now_ms();

    /* ── CPU : snapshot courant − snapshot précédent ── */
    cpu_snapshot_t curr_cpu;
    if (_read_cpu_stats(&curr_cpu) && g_prev_cpu.valid) {
        out->cpu_total_percent = _cpu_usage(&g_prev_cpu.total, &curr_cpu.total);

        int cores = (curr_cpu.core_count < LEO_MAX_CPU_CORES)
                  ? curr_cpu.core_count : LEO_MAX_CPU_CORES;
        out->cpu_core_count = cores;
        for (int i = 0; i < cores; i++) {
            out->cpu_per_core[i] = _cpu_usage(
                &g_prev_cpu.per_core[i],
                &curr_cpu.per_core[i]
            );
        }

        /* Le snapshot courant devient le précédent pour la prochaine collecte */
        g_prev_cpu = curr_cpu;
    }

    /* ── RAM ── */
    if (!_read_meminfo(&out->ram_total_bytes,
                       &out->ram_available_bytes,
                       &out->ram_used_bytes)) {
        LOG_WARN("Échec lecture /proc/meminfo");
    }

    /* ── Disque ── */
    if (!_read_disk_usage(&out->disk_total_bytes, &out->disk_used_bytes)) {
        LOG_WARN("Échec lecture de l'usage disque");
    }

    /* ── Réseau ── */
    if (!_read_net_stats(&out->net_bytes_in, &out->net_bytes_out)) {
        LOG_WARN("Échec lecture /proc/net/dev");
    }

    /* ── Processus ── */
    out->process_count = _read_process_count();

    LOG_DEBUG("Métriques collectées : CPU=%.1f%% RAM=%lluMB/%lluMB procs=%u",
              out->cpu_total_percent,
              (unsigned long long)(out->ram_used_bytes / (1024*1024)),
              (unsigned long long)(out->ram_total_bytes / (1024*1024)),
              out->process_count);

    return LEO_OK;
}

void leo_metrics_destroy(void) {
    memset(&g_prev_cpu, 0, sizeof(g_prev_cpu));
    LOG_INFO("Sous-système métriques Linux arrêté");
}
