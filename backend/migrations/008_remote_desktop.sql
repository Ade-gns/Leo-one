-- =============================================================================
-- Migration 008 : Bureau à distance (partage d'écran + prise de contrôle)
-- =============================================================================

-- ---------------------------------------------------------------------------
-- TABLE : remote_desktop_sessions
-- Une session = un pairage éphémère agent<->navigateur, relayé en mémoire par
-- internal/infrastructure/remotedesktop.Relay (voir ce package pour le
-- pourquoi ce n'est PAS porté par le canal de contrôle WS existant : celui-ci
-- est texte/64KB/mono-connexion par agent, impropre à du flux vidéo).
--
-- viewer_token_hash / agent_token_hash : même schéma que
-- file_download_tokens (token brut jamais stocké, hash SHA-256, à usage
-- unique via *_connected_at IS NULL, expiration via expires_at) — chaque
-- côté (navigateur, agent) présente son propre jeton à sa propre connexion
-- WS dédiée pour être apparié par le Relay.
-- ---------------------------------------------------------------------------
CREATE TYPE remote_desktop_mode   AS ENUM ('view', 'control');
CREATE TYPE remote_desktop_status AS ENUM ('pending', 'active', 'ended');

CREATE TABLE remote_desktop_sessions (
    id                  UUID                  PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID                  NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id            UUID                  NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id             UUID                  REFERENCES users(id) ON DELETE SET NULL,
    mode                remote_desktop_mode   NOT NULL,
    status              remote_desktop_status NOT NULL DEFAULT 'pending',
    viewer_token_hash   TEXT                  NOT NULL UNIQUE,
    agent_token_hash    TEXT                  NOT NULL UNIQUE,
    expires_at          TIMESTAMPTZ           NOT NULL,
    agent_connected_at  TIMESTAMPTZ,
    viewer_connected_at TIMESTAMPTZ,
    ended_at            TIMESTAMPTZ,
    ended_reason        TEXT,
    created_at          TIMESTAMPTZ           NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_remote_desktop_sessions_tenant ON remote_desktop_sessions(tenant_id);

-- Utilisé pour la contrainte métier "une seule session active par agent"
-- (voir RemoteDesktopHandler.createSession / Repository.ActiveForAgent) —
-- index partiel : seules les sessions pas encore terminées comptent.
CREATE INDEX idx_remote_desktop_sessions_agent_active ON remote_desktop_sessions(agent_id)
    WHERE status <> 'ended';

-- ---------------------------------------------------------------------------
-- Permissions : ressource "remote_desktop"
-- read    -> ouvrir une session en lecture seule (voir la commande)
-- execute -> ouvrir une session en contrôle (clavier/souris)
-- Vocabulaire réutilisé tel quel (pas de "view"/"control" dédiés) pour rester
-- cohérent avec files/patches/agents:execute — "action à fort impact sur une
-- machine gérée" — et pour que Lecture seule obtienne "read" automatiquement
-- via son filtre générique existant, sans règle spéciale.
-- ---------------------------------------------------------------------------
INSERT INTO permissions (resource, action, description) VALUES
    ('remote_desktop', 'read',    'Voir l''écran d''un agent (lecture seule)'),
    ('remote_desktop', 'execute', 'Prendre le contrôle du clavier/souris d''un agent')
ON CONFLICT (resource, action) DO NOTHING;

-- Redéfinit seed_system_roles pour inclure "remote_desktop" dans le rôle
-- Technicien (Admin les a déjà toutes ; Lecture seule obtient
-- remote_desktop:read via son filtre générique "action = 'read'", inchangé).
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
        ('agents',         'read'),
        ('agents',         'write'),
        ('agents',         'execute'),
        ('metrics',        'read'),
        ('alerts',         'read'),
        ('alerts',         'acknowledge'),
        ('alerts',         'write'),
        ('inventory',      'read'),
        ('workspaces',     'read'),
        ('scripts',        'read'),
        ('scripts',        'write'),
        ('scripts',        'execute'),
        ('patches',        'read'),
        ('patches',        'execute'),
        ('files',          'read'),
        ('files',          'write'),
        ('files',          'execute'),
        ('remote_desktop', 'read'),
        ('remote_desktop', 'execute')
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
