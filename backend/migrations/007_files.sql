-- =============================================================================
-- Migration 007 : Transfert de fichiers agent ↔ serveur
-- =============================================================================

-- ---------------------------------------------------------------------------
-- TABLE : files
-- Bibliothèque de fichiers déployables (installeurs, configs, ...), stockés
-- sur disque local (storage_path) — jamais exposé au client, résolu
-- uniquement côté serveur pour servir GET .../download.
-- ---------------------------------------------------------------------------
CREATE TABLE files (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    size_bytes      BIGINT      NOT NULL,
    checksum_sha256 TEXT        NOT NULL,
    storage_path    TEXT        NOT NULL,
    uploaded_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_files_tenant ON files(tenant_id);

-- ---------------------------------------------------------------------------
-- TABLE : file_download_tokens
-- Token de téléchargement signé, courte durée, à usage unique — même schéma
-- que enrollment_tokens (token_hash, expires_at, used_at) : l'agent n'a pas
-- de JWT, seule cette route de téléchargement est publique (voir router.go).
-- Généré à chaque POST .../deploy-file, jamais réutilisé entre deux
-- déploiements même du même fichier vers le même agent.
-- ---------------------------------------------------------------------------
CREATE TABLE file_download_tokens (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id    UUID        NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    token_hash TEXT        NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_file_download_tokens_hash ON file_download_tokens(token_hash);

-- ---------------------------------------------------------------------------
-- Nouveau type de commande + suivi d'avancement
-- ---------------------------------------------------------------------------
ALTER TYPE command_type ADD VALUE 'file_transfer';

-- Alimentée par LEO_MSG_FILE_TRANSFER_PROGRESS (voir dispatcher.go
-- handleFileTransferProgress) — NULL tant qu'aucun rapport d'avancement n'a
-- été reçu (commande pas encore démarrée côté agent, ou type de commande
-- différent).
ALTER TABLE commands ADD COLUMN progress_percent INTEGER;

-- ---------------------------------------------------------------------------
-- Permissions : ressource "files"
-- ---------------------------------------------------------------------------
INSERT INTO permissions (resource, action, description) VALUES
    ('files', 'read',    'Voir la bibliothèque de fichiers déployables'),
    ('files', 'write',   'Uploader ou supprimer des fichiers'),
    ('files', 'execute', 'Déployer un fichier sur un agent')
ON CONFLICT (resource, action) DO NOTHING;

-- Redéfinit seed_system_roles pour inclure "files" dans le rôle Technicien
-- (Admin les a déjà toutes ; Lecture seule obtient files:read via son
-- filtre générique "action = 'read'", inchangé).
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
        ('patches',    'execute'),
        ('files',      'read'),
        ('files',      'write'),
        ('files',      'execute')
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
