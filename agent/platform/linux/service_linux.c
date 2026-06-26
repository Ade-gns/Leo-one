/**
 * service_linux.c — Gestion du service systemd de l'agent Leo-One
 *
 * Installe, désinstalle et interroge le service systemd "leo-agent".
 * Implémentation fonctionnelle minimale :
 *  - install   : écrit /etc/systemd/system/leo-agent.service (root requis)
 *  - uninstall : supprime le fichier unit si présent
 *  - status    : vérifie que systemd est actif, puis parse "systemctl is-active"
 */
#include "service_linux.h"
#include "../../src/logger.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <errno.h>
#include <sys/stat.h>
#include <sys/types.h>

/* ─── Constantes ─────────────────────────────────────────────────────────── */

#define LEO_SYSTEMD_UNIT_PATH  "/etc/systemd/system/leo-agent.service"
#define LEO_SYSTEMD_RUNTIME_DIR "/run/systemd/system"

/** Contenu du fichier unit systemd. */
static const char LEO_SERVICE_UNIT[] =
    "[Unit]\n"
    "Description=Leo-One RMM Agent\n"
    "After=network-online.target\n"
    "Wants=network-online.target\n"
    "\n"
    "[Service]\n"
    "Type=simple\n"
    "ExecStart=/opt/leo-one/agent/leo-agent\n"
    "Restart=on-failure\n"
    "RestartSec=10\n"
    "User=root\n"
    "StandardOutput=journal\n"
    "StandardError=journal\n"
    "\n"
    "[Install]\n"
    "WantedBy=multi-user.target\n";

/* ─── Helpers privés ─────────────────────────────────────────────────────── */

/**
 * Vérifie si systemd est le gestionnaire d'init en contrôlant
 * l'existence du répertoire /run/systemd/system.
 */
static bool _systemd_available(void) {
    struct stat st;
    return stat(LEO_SYSTEMD_RUNTIME_DIR, &st) == 0 && S_ISDIR(st.st_mode);
}

/* ─── API publique ───────────────────────────────────────────────────────── */

leo_error_t leo_service_install(void) {
    /* Droits root requis */
    if (geteuid() != 0) {
        LOG_ERROR("leo_service_install : droits root requis (uid=%d)", (int)geteuid());
        return LEO_ERR_SYSTEM;
    }

    if (!_systemd_available()) {
        LOG_ERROR("leo_service_install : systemd non détecté sur ce système");
        return LEO_ERR_SYSTEM;
    }

    FILE *fp = fopen(LEO_SYSTEMD_UNIT_PATH, "w");
    if (!fp) {
        LOG_ERROR("Impossible d'ouvrir '%s' en écriture : %s",
                  LEO_SYSTEMD_UNIT_PATH, strerror(errno));
        return LEO_ERR_SYSTEM;
    }

    size_t unit_len = sizeof(LEO_SERVICE_UNIT) - 1;  /* -1 : pas le \0 terminal */
    size_t written  = fwrite(LEO_SERVICE_UNIT, 1, unit_len, fp);
    fclose(fp);

    if (written != unit_len) {
        LOG_ERROR("Écriture incomplète du fichier unit (%zu/%zu octets)",
                  written, unit_len);
        return LEO_ERR_SYSTEM;
    }

    /* Droits 644 : lecture pour tous, écriture owner */
    if (chmod(LEO_SYSTEMD_UNIT_PATH, 0644) != 0) {
        LOG_WARN("chmod 644 échoué sur '%s' : %s", LEO_SYSTEMD_UNIT_PATH, strerror(errno));
    }

    LOG_INFO("Service systemd installé : %s", LEO_SYSTEMD_UNIT_PATH);
    LOG_INFO("Lancer 'systemctl daemon-reload && systemctl enable --now leo-agent' pour activer");
    return LEO_OK;
}

leo_error_t leo_service_uninstall(void) {
    /* Si le fichier n'existe pas, on considère ça comme un succès */
    if (access(LEO_SYSTEMD_UNIT_PATH, F_OK) != 0) {
        LOG_INFO("Service systemd non présent, rien à désinstaller");
        return LEO_OK;
    }

    if (unlink(LEO_SYSTEMD_UNIT_PATH) != 0) {
        LOG_ERROR("Impossible de supprimer '%s' : %s",
                  LEO_SYSTEMD_UNIT_PATH, strerror(errno));
        return LEO_ERR_SYSTEM;
    }

    LOG_INFO("Service systemd supprimé : %s", LEO_SYSTEMD_UNIT_PATH);
    return LEO_OK;
}

leo_service_status_t leo_service_status(void) {
    if (!_systemd_available()) {
        LOG_WARN("leo_service_status : systemd non disponible");
        return LEO_SERVICE_UNKNOWN;
    }

    /* popen est acceptable ici : la commande est statique, pas de données utilisateur */
    FILE *fp = popen("systemctl is-active leo-agent 2>/dev/null", "r");
    if (!fp) {
        LOG_ERROR("popen systemctl échoué : %s", strerror(errno));
        return LEO_SERVICE_UNKNOWN;
    }

    char out[64];
    memset(out, 0, sizeof(out));
    if (fgets(out, sizeof(out) - 1, fp) == NULL) {
        pclose(fp);
        return LEO_SERVICE_UNKNOWN;
    }
    pclose(fp);

    /* Supprimer le saut de ligne final */
    size_t len = strlen(out);
    if (len > 0 && out[len - 1] == '\n')
        out[len - 1] = '\0';

    LOG_DEBUG("systemctl is-active leo-agent → '%s'", out);

    if (strcmp(out, "active") == 0)
        return LEO_SERVICE_ACTIVE;
    if (strcmp(out, "inactive") == 0)
        return LEO_SERVICE_INACTIVE;
    if (strcmp(out, "failed") == 0)
        return LEO_SERVICE_FAILED;

    return LEO_SERVICE_UNKNOWN;
}
