-- =============================================================================
-- Migration 004 : Bibliothèque de scripts + planification récurrente
-- =============================================================================

-- ---------------------------------------------------------------------------
-- TABLE : scripts
-- Bibliothèque de scripts réutilisables (nom, interpréteur, contenu).
-- ---------------------------------------------------------------------------
CREATE TABLE scripts (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT,
    interpreter TEXT        NOT NULL,   -- bash | sh | python | cmd | powershell
    content     TEXT        NOT NULL,
    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_scripts_tenant_id ON scripts(tenant_id);

CREATE TRIGGER trg_scripts_updated_at
    BEFORE UPDATE ON scripts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ---------------------------------------------------------------------------
-- TABLE : script_schedules
-- Planification d'un script vers un agent ou un workspace entier, soit
-- récurrente (cron_expression), soit ponctuelle à une date/heure précise
-- (run_at) — exactement l'une des deux, comme pour le ciblage
-- agent_id/workspace_id, imposé par les contraintes CHECK ci-dessous. Une
-- planification ponctuelle se désactive elle-même après son unique
-- déclenchement (voir internal/scheduler) plutôt que d'être supprimée —
-- l'historique (last_run_at) reste consultable.
-- ---------------------------------------------------------------------------
CREATE TABLE script_schedules (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    script_id       UUID        NOT NULL REFERENCES scripts(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    agent_id        UUID        REFERENCES agents(id) ON DELETE CASCADE,
    workspace_id    UUID        REFERENCES workspaces(id) ON DELETE CASCADE,
    cron_expression TEXT,
    run_at          TIMESTAMPTZ,
    timeout_sec     INTEGER     NOT NULL DEFAULT 60,
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by      UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_run_at     TIMESTAMPTZ NOT NULL,
    last_run_at     TIMESTAMPTZ,
    CHECK ((agent_id IS NULL) <> (workspace_id IS NULL)),
    CHECK ((cron_expression IS NULL) <> (run_at IS NULL))
);

CREATE INDEX idx_script_schedules_tenant_id ON script_schedules(tenant_id);
CREATE INDEX idx_script_schedules_due ON script_schedules(next_run_at) WHERE enabled;

CREATE TRIGGER trg_script_schedules_updated_at
    BEFORE UPDATE ON script_schedules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Traçabilité : quelle planification a déclenché telle commande (NULL pour
-- les commandes ad-hoc, envoi groupé compris — pas de FK schedule_id dans
-- ce cas).
ALTER TABLE commands ADD COLUMN schedule_id UUID REFERENCES script_schedules(id) ON DELETE SET NULL;
CREATE INDEX idx_commands_schedule ON commands(schedule_id) WHERE schedule_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- Permissions : ressource "scripts" (bibliothèque + planification)
-- ---------------------------------------------------------------------------
INSERT INTO permissions (resource, action, description) VALUES
    ('scripts', 'read',    'Voir la bibliothèque de scripts et les planifications'),
    ('scripts', 'write',   'Créer et modifier des scripts et des planifications'),
    ('scripts', 'delete',  'Supprimer des scripts et des planifications'),
    ('scripts', 'execute', 'Envoyer un script (ad-hoc, groupé, ou via planification)')
ON CONFLICT (resource, action) DO NOTHING;

-- Redéfinit seed_system_roles pour inclure les permissions "scripts" dans le
-- rôle Technicien (Admin les a déjà toutes via "SELECT ... FROM permissions"
-- sans filtre ; Lecture seule les a déjà via son filtre générique
-- "action = 'read'" — seul Technicien a une liste explicite à mettre à jour).
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

    -- Rôle : Technicien (tout sauf gestion utilisateurs/tenant)
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
        ('scripts',    'execute')
    );

    -- Rôle : Lecture seule (consultation uniquement)
    INSERT INTO roles (tenant_id, name, description, is_system)
    VALUES (p_tenant_id, 'Lecture seule', 'Consultation uniquement, aucune action', TRUE)
    RETURNING id INTO role_readonly_id;

    INSERT INTO role_permissions (role_id, permission_id)
    SELECT role_readonly_id, id FROM permissions
    WHERE action = 'read';

END;
$$ LANGUAGE plpgsql;
