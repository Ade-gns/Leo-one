-- =============================================================================
-- Migration 006 : Gestion des mises à jour (patch management)
-- =============================================================================

-- ---------------------------------------------------------------------------
-- TABLE : patches
-- Un patch/mise à jour connu pour un agent donné — remonté périodiquement
-- par l'agent (LEO_MSG_PATCH_INVENTORY, voir internal/infrastructure/
-- websocket/dispatcher.go) puis installé à la demande (POST .../patches/install).
--
-- native_id est l'identifiant tel que rapporté par l'agent : nom de paquet
-- (Debian/RPM, Linux) ou "KB1234567" (Windows) — pas de sémantique côté
-- serveur au-delà de sa stabilité, qui permet de retrouver le même patch
-- d'une collecte à l'autre (UNIQUE(agent_id, native_id)).
--
-- status ne repasse à 'available' qu'au prochain PATCH_INVENTORY qui le
-- rapporte encore présent (upsert, voir PatchRepo.Upsert) — une installation
-- réussie/échouée (via le CMD_RESULT de la commande install_patches, voir
-- dispatcher.go) le fait passer à 'installed'/'failed' sans attendre une
-- nouvelle collecte.
-- ---------------------------------------------------------------------------
CREATE TYPE patch_severity AS ENUM ('optional', 'important', 'critical');
CREATE TYPE patch_status   AS ENUM ('available', 'installed', 'ignored', 'failed');

CREATE TABLE patches (
    id           UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     UUID           NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    native_id    TEXT           NOT NULL,
    title        TEXT           NOT NULL,
    severity     patch_severity NOT NULL DEFAULT 'important',
    size_bytes   BIGINT,
    status       patch_status   NOT NULL DEFAULT 'available',
    detected_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    installed_at TIMESTAMPTZ,
    UNIQUE(agent_id, native_id)
);

-- Filtre le plus courant : liste des patchs d'un agent, éventuellement par
-- statut (l'UI n'affiche par défaut que les patchs "available").
CREATE INDEX idx_patches_agent          ON patches(agent_id, status);
-- GET /api/v1/patches/summary : agrégats par tenant, filtrés par statut/sévérité.
CREATE INDEX idx_patches_tenant_status  ON patches(tenant_id, status, severity);

-- ---------------------------------------------------------------------------
-- Nouveau type de commande : install_patches (voir commands.type)
-- ---------------------------------------------------------------------------
ALTER TYPE command_type ADD VALUE 'install_patches';

-- ---------------------------------------------------------------------------
-- Permissions : ressource "patches"
-- ---------------------------------------------------------------------------
INSERT INTO permissions (resource, action, description) VALUES
    ('patches', 'read',    'Voir les mises à jour disponibles pour un agent'),
    ('patches', 'execute', 'Installer une sélection de mises à jour')
ON CONFLICT (resource, action) DO NOTHING;

-- Redéfinit seed_system_roles pour inclure "patches" dans le rôle
-- Technicien (Admin les a déjà toutes ; Lecture seule obtient patches:read
-- via son filtre générique "action = 'read'", inchangé).
CREATE OR REPLACE FUNCTION seed_system_roles(p_tenant_id UUID)
RETURNS VOID AS $$
DECLARE
    role_admin_id      UUID;
    role_tech_id       UUID;
    role_readonly_id   UUID;
BEGIN
    -- Rôle : Admin (toutes les permissions)
    INSERT INTO roles (tenant_id, name, description, is_system)
    VALUES (p_tenant_id, 'Admin', 'Accès complet à toutes les fonctionnalités', TRUE)
    RETURNING id INTO role_admin_id;

    INSERT INTO role_permissions (role_id, permission_id)
    SELECT role_admin_id, id FROM permissions;

    -- Rôle : Technicien (tout sauf gestion utilisateurs/tenant/audit)
    INSERT INTO roles (tenant_id, name, description, is_system)
    VALUES (p_tenant_id, 'Technicien', 'Gestion des agents et des alertes', TRUE)
    RETURNING id INTO role_tech_id;

    INSERT INTO role_permissions (role_id, permission_id)
    SELECT role_tech_id, id FROM permissions
    WHERE (resource, action) IN (
        ('agents',     'read'),
        ('agents',     'write'),
        ('agents',     'execute'),
        ('metrics',    'read'),
        ('alerts',     'read'),
        ('alerts',     'acknowledge'),
        ('alerts',     'write'),
        ('inventory',  'read'),
        ('workspaces', 'read'),
        ('scripts',    'read'),
        ('scripts',    'write'),
        ('scripts',    'execute'),
        ('patches',    'read'),
        ('patches',    'execute')
    );

    -- Rôle : Lecture seule (consultation uniquement, hors audit)
    INSERT INTO roles (tenant_id, name, description, is_system)
    VALUES (p_tenant_id, 'Lecture seule', 'Consultation uniquement, aucune action', TRUE)
    RETURNING id INTO role_readonly_id;

    INSERT INTO role_permissions (role_id, permission_id)
    SELECT role_readonly_id, id FROM permissions
    WHERE action = 'read' AND resource <> 'audit';

END;
$$ LANGUAGE plpgsql;
