/**
 * patches_linux.c — Détection et installation des mises à jour système sous
 * Linux, via le gestionnaire de paquets détecté sur la machine :
 *   apt (Debian/Ubuntu) : `apt list --upgradable`, `apt-get install --only-upgrade`
 *   dnf (Fedora/RHEL)   : `dnf check-update`,        `dnf update`
 *
 * Sévérité : ni apt ni dnf n'exposent de classification vendor propre (à la
 * différence de Windows Update). Best-effort ici : un paquet dont le
 * pocket/repo contient "security" est classé critique, sinon important —
 * aucun paquet n'est jamais classé "optional" par cette heuristique (pas de
 * source fiable pour distinguer "important" d'"optional" sans un flux
 * d'avis de sécurité dédié, hors périmètre).
 *
 * Toute commande passe par leo_exec_argv() (argv structuré, jamais un
 * shell) — même discipline que _EXEC_KIND_INSTALL_PKG dans agent.c.
 */
#include "../../src/patches.h"
#include "../../src/logger.h"

#include <ctype.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef enum { _PKGMGR_NONE, _PKGMGR_APT, _PKGMGR_DNF } _pkgmgr_t;

/** Variables d'environnement communes aux invocations apt-get — supprime les
 *  invites interactives (debconf), voir la même justification dans agent.c. */
static const char *const _APT_ENV[] = { "DEBIAN_FRONTEND=noninteractive", NULL };

static _pkgmgr_t _detect_pkgmgr(void) {
    if (access("/usr/bin/apt-get", X_OK) == 0) return _PKGMGR_APT;
    if (access("/usr/bin/dnf",     X_OK) == 0) return _PKGMGR_DNF;
    return _PKGMGR_NONE;
}

/* ─── Collecte ────────────────────────────────────────────────────────────── */

/** true si `word` contient "security" (recherche insensible à la casse,
 *  triviale — les noms de pocket/repo sont en anglais). */
static bool _looks_security(const char *word) {
    char lower[128];
    size_t n = strlen(word);
    if (n >= sizeof(lower)) n = sizeof(lower) - 1;
    for (size_t i = 0; i < n; i++)
        lower[i] = (char)tolower((unsigned char)word[i]);
    lower[n] = '\0';
    return strstr(lower, "security") != NULL;
}

/**
 * Parse une ligne de `apt list --upgradable`, ex :
 *   bash/jammy-updates 5.1-6ubuntu1.1 amd64 [upgradable from: 5.1-6ubuntu1]
 * Ignore silencieusement les lignes qui ne correspondent pas au format
 * attendu (ex : "Listing... Done").
 */
static bool _parse_apt_line(const char *line, leo_patch_t *out) {
    const char *slash = strchr(line, '/');
    if (!slash || slash == line) return false;

    const char *pocket = slash + 1;
    const char *space1  = strchr(pocket, ' ');
    if (!space1) return false;
    const char *version = space1 + 1;
    const char *space2  = strchr(version, ' ');
    if (!space2) return false;

    size_t name_len   = (size_t)(slash - line);
    size_t pocket_len = (size_t)(space1 - pocket);
    size_t ver_len     = (size_t)(space2 - version);
    if (name_len == 0 || name_len >= sizeof(out->id) || ver_len == 0) return false;

    memset(out, 0, sizeof(*out));
    memcpy(out->id, line, name_len);
    out->id[name_len] = '\0';

    char pocket_buf[128] = "";
    if (pocket_len < sizeof(pocket_buf)) {
        memcpy(pocket_buf, pocket, pocket_len);
        pocket_buf[pocket_len] = '\0';
    }
    out->severity = _looks_security(pocket_buf) ? LEO_PATCH_SEVERITY_CRITICAL
                                                 : LEO_PATCH_SEVERITY_IMPORTANT;

    char ver_buf[64] = "";
    if (ver_len < sizeof(ver_buf)) {
        memcpy(ver_buf, version, ver_len);
        ver_buf[ver_len] = '\0';
    }
    snprintf(out->title, sizeof(out->title), "%s → %s", out->id, ver_buf);

    return true;
}

static int _collect_apt(leo_patch_t *out, int max_items) {
    FILE *fp = popen("apt list --upgradable 2>/dev/null", "r");
    if (!fp) {
        LOG_WARN("popen 'apt list --upgradable' échoué");
        return 0;
    }

    int n = 0;
    char line[512];
    while (n < max_items && fgets(line, sizeof(line), fp)) {
        size_t len = strlen(line);
        while (len > 0 && (line[len-1] == '\n' || line[len-1] == '\r'))
            line[--len] = '\0';
        if (_parse_apt_line(line, &out[n])) n++;
    }
    pclose(fp);
    return n;
}

/**
 * Parse une ligne de `dnf check-update`, ex :
 *   bash.x86_64          5.1.16-1.fc38          updates
 * Ignore les lignes vides ou d'en-tête (ex : "Last metadata expiration...").
 */
static bool _parse_dnf_line(const char *line, leo_patch_t *out) {
    char name_arch[160], version[64], repo[64];
    /* %159s etc. laissent la place au \0 — cohérent avec la taille des buffers. */
    if (sscanf(line, "%159s %63s %63s", name_arch, version, repo) != 3)
        return false;
    /* Filtre les lignes qui ne sont manifestement pas "paquet version repo"
     * (ex: la ligne d'en-tête se termine par ':' et n'a pas 3 tokens
     * plausibles) — une version dnf attendue ressemble à "1.2.3-1.fc38". */
    if (!strchr(version, '.') && !strchr(version, '-')) return false;

    memset(out, 0, sizeof(*out));
    strncpy(out->id, name_arch, sizeof(out->id) - 1);
    out->severity = _looks_security(repo) ? LEO_PATCH_SEVERITY_CRITICAL
                                           : LEO_PATCH_SEVERITY_IMPORTANT;
    snprintf(out->title, sizeof(out->title), "%s → %s", name_arch, version);
    return true;
}

static int _collect_dnf(leo_patch_t *out, int max_items) {
    /* --quiet : supprime le bandeau de progression du téléchargement des
     * métadonnées, qui polluerait le parsing ligne à ligne. Code de retour
     * dnf check-update : 100 si des mises à jour existent, 0 sinon — sans
     * incidence ici, popen() ne remonte pas le code de sortie. */
    FILE *fp = popen("dnf check-update --quiet 2>/dev/null", "r");
    if (!fp) {
        LOG_WARN("popen 'dnf check-update' échoué");
        return 0;
    }

    int n = 0;
    char line[512];
    while (n < max_items && fgets(line, sizeof(line), fp)) {
        size_t len = strlen(line);
        while (len > 0 && (line[len-1] == '\n' || line[len-1] == '\r'))
            line[--len] = '\0';
        if (line[0] == '\0') continue;
        if (_parse_dnf_line(line, &out[n])) n++;
    }
    pclose(fp);
    return n;
}

int leo_patches_collect(leo_patch_t *out, int max_items) {
    if (!out || max_items <= 0) return -1;

    switch (_detect_pkgmgr()) {
    case _PKGMGR_APT: {
        int n = _collect_apt(out, max_items);
        LOG_DEBUG("Patchs disponibles (apt) : %d", n);
        return n;
    }
    case _PKGMGR_DNF: {
        int n = _collect_dnf(out, max_items);
        LOG_DEBUG("Patchs disponibles (dnf) : %d", n);
        return n;
    }
    default:
        LOG_WARN("Aucun gestionnaire de paquets supporté détecté (ni apt-get ni dnf)");
        return 0;
    }
}

/* ─── Installation ────────────────────────────────────────────────────────── */

/** Redémarre au mieux : `shutdown -r +1` avec repli `systemctl reboot` —
 *  même séquence que _EXEC_KIND_REBOOT dans agent.c, dupliquée ici plutôt
 *  que partagée pour ne pas faire dépendre patches.h de l'API interne
 *  d'agent.c (patches.h n'a pas connaissance de _exec_ctx_t). Best-effort :
 *  un échec de planification n'invalide pas l'installation déjà réussie. */
static void _schedule_reboot(void) {
    leo_exec_result_t r;
    char *shutdown_argv[] = { "shutdown", "-r", "+1", NULL };
    if (leo_exec_argv(shutdown_argv, NULL, 15, &r) == LEO_OK && r.exit_code == 0) return;

    char *systemctl_argv[] = { "systemctl", "reboot", NULL };
    leo_exec_argv(systemctl_argv, NULL, 15, &r);
}

static leo_error_t _install_apt(const char *const ids[], int count, int timeout_secs,
                                 leo_exec_result_t *result) {
    char *update_argv[] = { "apt-get", "update", "-qq", NULL };
    leo_error_t rc = leo_exec_argv(update_argv, _APT_ENV, timeout_secs, result);
    if (rc != LEO_OK || result->exit_code != 0) return rc == LEO_OK ? LEO_OK : rc;

    /* +6 : "apt-get" "install" "-y" "--only-upgrade" "--" NULL */
    char *install_argv[LEO_PATCH_INSTALL_MAX_COUNT + 6];
    int   ac = 0;
    install_argv[ac++] = "apt-get";
    install_argv[ac++] = "install";
    install_argv[ac++] = "-y";
    /* --only-upgrade : n'installe jamais un paquet absent du système — un
     * id de patch qui ne correspond plus à un paquet déjà installé (ex :
     * inventaire de patchs périmé) échoue proprement plutôt que d'installer
     * un nouveau paquet non demandé. */
    install_argv[ac++] = "--only-upgrade";
    install_argv[ac++] = "--";
    for (int i = 0; i < count && ac < LEO_PATCH_INSTALL_MAX_COUNT + 5; i++)
        install_argv[ac++] = (char *)ids[i];
    install_argv[ac] = NULL;

    return leo_exec_argv(install_argv, _APT_ENV, timeout_secs, result);
}

static leo_error_t _install_dnf(const char *const ids[], int count, int timeout_secs,
                                 leo_exec_result_t *result) {
    /* "dnf update <pkgs>" ne met à jour que des paquets déjà installés —
     * même garde-fou que --only-upgrade pour apt, sans option dédiée. */
    char *argv[LEO_PATCH_INSTALL_MAX_COUNT + 4];
    int   ac = 0;
    argv[ac++] = "dnf";
    argv[ac++] = "update";
    argv[ac++] = "-y";
    for (int i = 0; i < count && ac < LEO_PATCH_INSTALL_MAX_COUNT + 3; i++)
        argv[ac++] = (char *)ids[i];
    argv[ac] = NULL;

    return leo_exec_argv(argv, NULL, timeout_secs, result);
}

leo_error_t leo_patches_install(const char *const ids[], int count, bool reboot_after,
                                 int timeout_secs, leo_exec_result_t *result) {
    if (!ids || count <= 0 || !result) return LEO_ERR_SYSTEM;

    leo_error_t rc;
    switch (_detect_pkgmgr()) {
    case _PKGMGR_APT: rc = _install_apt(ids, count, timeout_secs, result); break;
    case _PKGMGR_DNF: rc = _install_dnf(ids, count, timeout_secs, result); break;
    default:
        memset(result, 0, sizeof(*result));
        snprintf(result->stderr_buf, sizeof(result->stderr_buf),
                 "Aucun gestionnaire de paquets supporté détecté");
        result->exit_code = -1;
        return LEO_OK;
    }

    if (rc == LEO_OK && result->exit_code == 0 && reboot_after) {
        LOG_WARN("Redémarrage planifié après installation des patchs");
        _schedule_reboot();
    }
    return rc;
}
