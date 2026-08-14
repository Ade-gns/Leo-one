/**
 * metrics_win.c — Collecte de métriques système sous Windows
 *
 * Sources de données :
 *   CPU  : NtQuerySystemInformation(SystemProcessorPerformanceInformation)
 *          (calcul différentiel entre deux lectures, par cœur — même
 *          principe que /proc/stat côté Linux). Fonction non documentée
 *          publiquement mais stable et largement utilisée par les outils de
 *          supervision Windows (Process Explorer, etc.) faute d'API PDH
 *          plus légère ; on résout son adresse dynamiquement depuis ntdll
 *          plutôt que de dépendre d'un import statique.
 *   RAM  : GlobalMemoryStatusEx
 *   Disk : GetDiskFreeSpaceExA, sommé sur tous les volumes fixes
 *          (GetLogicalDrives + GetDriveTypeA == DRIVE_FIXED)
 *   Net  : GetIfTable2 (iphlpapi), sommé sur les interfaces up hors loopback
 *   Proc : CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS)
 *
 * La mesure CPU nécessite deux snapshots séparés dans le temps.
 * On conserve le snapshot précédent dans un état statique global.
 */
#include "../../src/metrics.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* winsock2.h doit être inclus avant windows.h (sinon windows.h entraîne
 * l'ancien winsock.h, incompatible avec netioapi.h/iphlpapi.h qui attendent
 * les types Winsock2 — ADDRESS_FAMILY, SOCKET_ADDRESS, SOCKADDR_STORAGE). */
#define WIN32_LEAN_AND_MEAN
#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <iphlpapi.h>
#include <netioapi.h>
#include <tlhelp32.h>

/* ─── NtQuerySystemInformation : résolution dynamique ────────────────────── */
/* SYSTEM_PROCESSOR_PERFORMANCE_INFORMATION n'est pas exposée dans les headers
 * publics de mingw-w64 — on la redéclare localement (layout stable depuis
 * NT4, documenté de facto par tous les outils qui l'utilisent). */
typedef struct {
    LARGE_INTEGER IdleTime;
    LARGE_INTEGER KernelTime;   /* inclut IdleTime, comme GetSystemTimes */
    LARGE_INTEGER UserTime;
    LARGE_INTEGER DpcTime;
    LARGE_INTEGER InterruptTime;
    ULONG         InterruptCount;
} _leo_sppi_t;

#define _LEO_SystemProcessorPerformanceInformation 8

typedef LONG (WINAPI *_NtQuerySystemInformationFn)(
    ULONG SystemInformationClass,
    PVOID SystemInformation,
    ULONG SystemInformationLength,
    PULONG ReturnLength);

static bool _query_cpu_times(_leo_sppi_t *buf, int max_cores, int *out_count) {
    static _NtQuerySystemInformationFn fn = NULL;
    static bool                        resolved = false;

    if (!resolved) {
        HMODULE ntdll = GetModuleHandleA("ntdll.dll");
        if (ntdll) {
            fn = (_NtQuerySystemInformationFn)(void *)
                 GetProcAddress(ntdll, "NtQuerySystemInformation");
        }
        resolved = true;
    }
    if (!fn) return false;

    ULONG needed = 0;
    LONG  status = fn(_LEO_SystemProcessorPerformanceInformation, buf,
                       (ULONG)(max_cores * (int)sizeof(*buf)), &needed);
    if (status != 0 /* STATUS_SUCCESS */) return false;

    *out_count = (int)(needed / sizeof(*buf));
    return true;
}

/* ─── État différentiel CPU (snapshot précédent) ─────────────────────────── */

typedef struct {
    ULONGLONG idle;
    ULONGLONG kernel;
    ULONGLONG user;
} _cpu_times_t;

static _cpu_times_t g_prev_core[LEO_MAX_CPU_CORES];
static int          g_prev_core_count = 0;
static bool         g_prev_valid      = false;

static double _cpu_usage_pct(const _cpu_times_t *prev, const _cpu_times_t *curr) {
    ULONGLONG idle_delta  = curr->idle - prev->idle;
    ULONGLONG total_delta = (curr->kernel - prev->kernel) + (curr->user - prev->user);
    if (total_delta == 0) return 0.0;
    return (double)(total_delta - idle_delta) / (double)total_delta * 100.0;
}

/* ─── Helpers privés ──────────────────────────────────────────────────────── */

/** Timestamp en millisecondes depuis l'epoch Unix. FILETIME est en
 *  intervalles de 100ns depuis 1601-01-01 ; écart avec 1970-01-01 :
 *  11644473600 secondes, soit 116444736000000000 en unités de 100ns. */
static uint64_t _now_ms(void) {
    FILETIME ft;
    GetSystemTimeAsFileTime(&ft);
    ULARGE_INTEGER uli;
    uli.LowPart  = ft.dwLowDateTime;
    uli.HighPart = ft.dwHighDateTime;
    return (uli.QuadPart - 116444736000000000ULL) / 10000ULL;
}

static void _read_ram(uint64_t *total, uint64_t *available, uint64_t *used) {
    *total = *available = *used = 0;

    MEMORYSTATUSEX ms;
    ms.dwLength = sizeof(ms);
    if (!GlobalMemoryStatusEx(&ms)) {
        LOG_WARN("GlobalMemoryStatusEx échoué (code %lu)", GetLastError());
        return;
    }

    *total     = (uint64_t)ms.ullTotalPhys;
    *available = (uint64_t)ms.ullAvailPhys;
    *used      = (*total > *available) ? (*total - *available) : 0;
}

/** Somme l'usage disque sur tous les volumes fixes (DRIVE_FIXED) — exclut
 *  CD-ROM/amovible/réseau/ramdisk, équivalent du filtre de systèmes de
 *  fichiers virtuels côté Linux (_read_disk_usage sur /proc/mounts). */
static bool _read_disk_usage(uint64_t *total_out, uint64_t *used_out) {
    *total_out = *used_out = 0;
    bool any = false;

    DWORD drives = GetLogicalDrives();
    for (int i = 0; i < 26; i++) {
        if (!(drives & (1u << i))) continue;

        char root[4] = { (char)('A' + i), ':', '\\', '\0' };
        if (GetDriveTypeA(root) != DRIVE_FIXED) continue;

        ULARGE_INTEGER free_avail, total_bytes, total_free;
        if (!GetDiskFreeSpaceExA(root, &free_avail, &total_bytes, &total_free))
            continue;

        *total_out += total_bytes.QuadPart;
        *used_out  += total_bytes.QuadPart - total_free.QuadPart;
        any = true;
    }
    return any;
}

/** Additionne les octets réseau IN/OUT de toutes les interfaces actives,
 *  en excluant la boucle locale (équivalent d'exclure "lo"). */
static bool _read_net_stats(uint64_t *bytes_in, uint64_t *bytes_out) {
    *bytes_in = *bytes_out = 0;

    MIB_IF_TABLE2 *table = NULL;
    if (GetIfTable2(&table) != NO_ERROR || !table) {
        LOG_WARN("GetIfTable2 échoué");
        return false;
    }

    for (ULONG i = 0; i < table->NumEntries; i++) {
        MIB_IF_ROW2 *row = &table->Table[i];
        if (row->Type == IF_TYPE_SOFTWARE_LOOPBACK) continue;
        if (row->OperStatus != IfOperStatusUp) continue;

        *bytes_in  += row->InOctets;
        *bytes_out += row->OutOctets;
    }

    FreeMibTable(table);
    return true;
}

static uint32_t _read_process_count(void) {
    HANDLE snap = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
    if (snap == INVALID_HANDLE_VALUE) {
        LOG_WARN("CreateToolhelp32Snapshot échoué (code %lu)", GetLastError());
        return 0;
    }

    PROCESSENTRY32 pe;
    pe.dwSize = sizeof(pe);

    uint32_t count = 0;
    if (Process32First(snap, &pe)) {
        do { count++; } while (Process32Next(snap, &pe));
    }
    CloseHandle(snap);
    return count;
}

/* ─── API publique (implémente metrics.h) ───────────────────────────────── */

leo_error_t leo_metrics_init(void) {
    _leo_sppi_t raw[LEO_MAX_CPU_CORES];
    int count = 0;

    if (_query_cpu_times(raw, LEO_MAX_CPU_CORES, &count)) {
        for (int i = 0; i < count; i++) {
            g_prev_core[i].idle   = (ULONGLONG)raw[i].IdleTime.QuadPart;
            g_prev_core[i].kernel = (ULONGLONG)raw[i].KernelTime.QuadPart;
            g_prev_core[i].user   = (ULONGLONG)raw[i].UserTime.QuadPart;
        }
        g_prev_core_count = count;
        g_prev_valid      = true;
    } else {
        LOG_WARN("Impossible d'initialiser le snapshot CPU initial "
                 "(NtQuerySystemInformation indisponible)");
    }

    LOG_INFO("Sous-système métriques Windows initialisé (%d cœurs)", g_prev_core_count);
    return LEO_OK;
}

leo_error_t leo_metrics_collect(leo_metrics_t *out) {
    if (!out) return LEO_ERR_SYSTEM;

    memset(out, 0, sizeof(*out));
    out->timestamp_ms = _now_ms();

    /* ── CPU : snapshot courant − snapshot précédent ── */
    _leo_sppi_t raw[LEO_MAX_CPU_CORES];
    int count = 0;
    if (_query_cpu_times(raw, LEO_MAX_CPU_CORES, &count)) {
        _cpu_times_t curr_core[LEO_MAX_CPU_CORES];
        for (int i = 0; i < count; i++) {
            curr_core[i].idle   = (ULONGLONG)raw[i].IdleTime.QuadPart;
            curr_core[i].kernel = (ULONGLONG)raw[i].KernelTime.QuadPart;
            curr_core[i].user   = (ULONGLONG)raw[i].UserTime.QuadPart;
        }

        if (g_prev_valid && count == g_prev_core_count) {
            int cores = (count < LEO_MAX_CPU_CORES) ? count : LEO_MAX_CPU_CORES;
            out->cpu_core_count = cores;

            _cpu_times_t total_prev = {0}, total_curr = {0};
            for (int i = 0; i < cores; i++) {
                out->cpu_per_core[i] = _cpu_usage_pct(&g_prev_core[i], &curr_core[i]);
                total_prev.idle   += g_prev_core[i].idle;
                total_prev.kernel += g_prev_core[i].kernel;
                total_prev.user   += g_prev_core[i].user;
                total_curr.idle   += curr_core[i].idle;
                total_curr.kernel += curr_core[i].kernel;
                total_curr.user   += curr_core[i].user;
            }
            out->cpu_total_percent = _cpu_usage_pct(&total_prev, &total_curr);
        }

        memcpy(g_prev_core, curr_core, sizeof(curr_core[0]) * (size_t)count);
        g_prev_core_count = count;
        g_prev_valid      = true;
    }

    /* ── RAM ── */
    _read_ram(&out->ram_total_bytes, &out->ram_available_bytes, &out->ram_used_bytes);

    /* ── Disque ── */
    if (!_read_disk_usage(&out->disk_total_bytes, &out->disk_used_bytes)) {
        LOG_WARN("Échec lecture de l'usage disque");
    }

    /* ── Réseau ── */
    if (!_read_net_stats(&out->net_bytes_in, &out->net_bytes_out)) {
        LOG_WARN("Échec lecture des statistiques réseau");
    }

    /* ── Processus ── */
    out->process_count = _read_process_count();

    LOG_DEBUG("Métriques collectées : CPU=%.1f%% RAM=%lluMB/%lluMB procs=%u",
              out->cpu_total_percent,
              (unsigned long long)(out->ram_used_bytes / (1024 * 1024)),
              (unsigned long long)(out->ram_total_bytes / (1024 * 1024)),
              out->process_count);

    return LEO_OK;
}

void leo_metrics_destroy(void) {
    g_prev_core_count = 0;
    g_prev_valid      = false;
    LOG_INFO("Sous-système métriques Windows arrêté");
}
