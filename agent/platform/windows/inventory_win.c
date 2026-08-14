/**
 * inventory_win.c — Collecte d'inventaire matériel/logiciel sous Windows
 *
 * Sources de données :
 *   CPU        : registre HKLM\HARDWARE\DESCRIPTION\System\CentralProcessor\0
 *                (modèle) + GetSystemInfo/GetLogicalProcessorInformation
 *                (threads/cœurs physiques)
 *   RAM        : GlobalMemoryStatusEx
 *   Disques    : ouverture successive de \\.\PhysicalDriveN (N=0..63) — un
 *                handle s'ouvre même sans droits admin tant que
 *                dwDesiredAccess=0 ("vérifier l'existence" uniquement)
 *   BIOS/carte : registre HKLM\HARDWARE\DESCRIPTION\System\BIOS
 *                (renseigné par Windows depuis le SMBIOS au démarrage)
 *   Logiciels  : énumération HKLM\...\CurrentVersion\Uninstall, vue 64 bits
 *                ET WOW6432Node (applications 32 bits sur un OS 64 bits) —
 *                même filtre que "Programmes et fonctionnalités" (on ignore
 *                les clés sans DisplayName)
 *
 * Toute source indisponible laisse le champ correspondant vide/à zéro plutôt
 * que de faire échouer la collecte entière — l'inventaire est best-effort,
 * miroir de platform/linux/inventory_linux.c.
 */
#include "../../src/inventory.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include <windows.h>

/* ─── CPU ─────────────────────────────────────────────────────────────────── */

static void _read_cpu_model(leo_hw_inventory_t *out) {
    HKEY hkey;
    if (RegOpenKeyExA(HKEY_LOCAL_MACHINE,
                       "HARDWARE\\DESCRIPTION\\System\\CentralProcessor\\0",
                       0, KEY_READ, &hkey) != ERROR_SUCCESS) {
        return;  /* laissé vide */
    }

    DWORD sz = (DWORD)sizeof(out->cpu_model);
    RegGetValueA(hkey, NULL, "ProcessorNameString", RRF_RT_REG_SZ, NULL, out->cpu_model, &sz);
    RegCloseKey(hkey);
}

/**
 * Threads = GetSystemInfo (nombre de processeurs logiques, fiable).
 * Cœurs physiques = nombre d'entrées RelationProcessorCore retournées par
 * GetLogicalProcessorInformation. Peut échouer/rester à 0 sur une machine à
 * plus de 64 processeurs logiques répartis sur plusieurs groupes de
 * processeurs — laissé à 0 dans ce cas plutôt que d'échouer la collecte
 * (même philosophie que le cpu_cores=0 "indéterminable" côté Linux).
 */
static void _read_cpu_topology(leo_hw_inventory_t *out) {
    SYSTEM_INFO si;
    GetSystemInfo(&si);
    out->cpu_threads = (int)si.dwNumberOfProcessors;

    DWORD len = 0;
    GetLogicalProcessorInformation(NULL, &len);
    if (len == 0) return;

    SYSTEM_LOGICAL_PROCESSOR_INFORMATION *buf = malloc(len);
    if (!buf) return;

    if (GetLogicalProcessorInformation(buf, &len)) {
        int cores = 0;
        DWORD count = len / sizeof(*buf);
        for (DWORD i = 0; i < count; i++) {
            if (buf[i].Relationship == RelationProcessorCore) cores++;
        }
        out->cpu_cores = cores;
    }
    free(buf);
}

/* ─── RAM ─────────────────────────────────────────────────────────────────── */

static void _read_ram_total(leo_hw_inventory_t *out) {
    MEMORYSTATUSEX ms;
    ms.dwLength = sizeof(ms);
    if (GlobalMemoryStatusEx(&ms)) {
        out->ram_total_bytes = (uint64_t)ms.ullTotalPhys;
    } else {
        LOG_WARN("GlobalMemoryStatusEx échoué (code %lu)", GetLastError());
    }
}

/* ─── Disques ─────────────────────────────────────────────────────────────── */

/** Compte les disques physiques en essayant d'ouvrir \\.\PhysicalDriveN
 *  pour N croissant jusqu'au premier ERROR_FILE_NOT_FOUND (fin de liste). */
static void _read_disk_count(leo_hw_inventory_t *out) {
    int count = 0;
    char path[32];

    for (int i = 0; i < 64; i++) {
        snprintf(path, sizeof(path), "\\\\.\\PhysicalDrive%d", i);
        HANDLE h = CreateFileA(path, 0, FILE_SHARE_READ | FILE_SHARE_WRITE,
                                NULL, OPEN_EXISTING, 0, NULL);
        if (h == INVALID_HANDLE_VALUE) {
            if (GetLastError() == ERROR_FILE_NOT_FOUND) break;
            continue;
        }
        CloseHandle(h);
        count++;
    }
    out->disk_count = count;
}

/* ─── BIOS / carte mère ───────────────────────────────────────────────────── */

static void _read_bios_field(const char *value_name, char *out, size_t out_sz) {
    HKEY hkey;
    if (RegOpenKeyExA(HKEY_LOCAL_MACHINE, "HARDWARE\\DESCRIPTION\\System\\BIOS",
                       0, KEY_READ, &hkey) != ERROR_SUCCESS) {
        return;  /* absent — laissé vide, comme _read_dmi_field côté Linux */
    }

    DWORD sz = (DWORD)out_sz;
    RegGetValueA(hkey, NULL, value_name, RRF_RT_REG_SZ, NULL, out, &sz);
    RegCloseKey(hkey);
}

/* ─── Logiciels installés (registre Uninstall) ───────────────────────────── */

/**
 * Énumère les sous-clés d'un répertoire "Uninstall" (vue 64 bits ou
 * WOW6432Node) et remplit out[start_idx..] avec les entrées ayant un
 * DisplayName (filtre identique à "Programmes et fonctionnalités").
 * @return Index suivant disponible dans out (start_idx + nombre ajouté).
 */
static int _collect_uninstall_key(const char *subkey, leo_sw_item_t *out,
                                   int max_items, int start_idx)
{
    HKEY hkey;
    if (RegOpenKeyExA(HKEY_LOCAL_MACHINE, subkey, 0, KEY_READ, &hkey) != ERROR_SUCCESS)
        return start_idx;

    int idx = start_idx;
    char name[256];

    for (DWORD i = 0; idx < max_items; i++) {
        DWORD name_len = sizeof(name);
        LONG rc = RegEnumKeyExA(hkey, i, name, &name_len, NULL, NULL, NULL, NULL);
        if (rc == ERROR_NO_MORE_ITEMS) break;
        if (rc != ERROR_SUCCESS) continue;

        HKEY sub;
        if (RegOpenKeyExA(hkey, name, 0, KEY_READ, &sub) != ERROR_SUCCESS) continue;

        leo_sw_item_t *item = &out[idx];
        memset(item, 0, sizeof(*item));

        DWORD sz = (DWORD)sizeof(item->name);
        LONG got = RegGetValueA(sub, NULL, "DisplayName", RRF_RT_REG_SZ, NULL, item->name, &sz);
        if (got != ERROR_SUCCESS || item->name[0] == '\0') {
            RegCloseKey(sub);
            continue;  /* pas une entrée "logiciel" affichable */
        }

        sz = (DWORD)sizeof(item->version);
        RegGetValueA(sub, NULL, "DisplayVersion", RRF_RT_REG_SZ, NULL, item->version, &sz);
        sz = (DWORD)sizeof(item->publisher);
        RegGetValueA(sub, NULL, "Publisher", RRF_RT_REG_SZ, NULL, item->publisher, &sz);
        sz = (DWORD)sizeof(item->install_path);
        RegGetValueA(sub, NULL, "InstallLocation", RRF_RT_REG_SZ, NULL, item->install_path, &sz);

        RegCloseKey(sub);
        idx++;
    }

    RegCloseKey(hkey);
    return idx;
}

/* ─── API publique (implémente inventory.h) ─────────────────────────────── */

leo_error_t leo_inventory_collect_hw(leo_hw_inventory_t *out) {
    if (!out) return LEO_ERR_SYSTEM;
    memset(out, 0, sizeof(*out));

    _read_cpu_model(out);
    _read_cpu_topology(out);
    _read_ram_total(out);
    _read_disk_count(out);

    _read_bios_field("BIOSVersion",        out->bios_version,  sizeof(out->bios_version));
    _read_bios_field("BIOSVendor",         out->bios_vendor,   sizeof(out->bios_vendor));
    _read_bios_field("BaseBoardProduct",   out->motherboard,   sizeof(out->motherboard));
    _read_bios_field("SystemSerialNumber", out->serial_number, sizeof(out->serial_number));

    LOG_DEBUG("Inventaire matériel collecté : cpu='%s' threads=%d cores=%d ram=%lluMB disks=%d",
              out->cpu_model, out->cpu_threads, out->cpu_cores,
              (unsigned long long)(out->ram_total_bytes / (1024 * 1024)), out->disk_count);

    return LEO_OK;
}

int leo_inventory_collect_sw(leo_sw_item_t *out, int max_items) {
    if (!out || max_items <= 0) return -1;

    int n = _collect_uninstall_key(
        "SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Uninstall", out, max_items, 0);
    n = _collect_uninstall_key(
        "SOFTWARE\\WOW6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall", out, max_items, n);

    LOG_DEBUG("Inventaire logiciel collecté : %d entrées", n);
    return n;
}
