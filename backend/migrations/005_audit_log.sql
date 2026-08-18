-- =============================================================================
-- Migration 005 : Journal d'audit (traçabilité des actions)
-- =============================================================================

-- ---------------------------------------------------------------------------
-- TABLE : audit_log
-- Une ligne par action d'écriture effectuée via l'API (agents, utilisateurs,
-- rôles, scripts, planifications, commandes, tokens d'enrollment, alertes).
-- resource_id est en TEXT (pas de FK) : la ressource visée dépend de
-- resource_type et n'a donc pas une table unique à référencer ; l'entrée
-- doit aussi survivre à la suppression de la ressource qu'elle documente.
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id       UUID        REFERENCES users(id) ON DELETE SET NULL,
    action        TEXT        NOT NULL,   -- ex: "agent.delete", "user.create", "alert.acknowledge"
    resource_type TEXT        NOT NULL,   -- ex: "agent", "user", "role", "script", "command"
    resource_id   TEXT,
    details       JSONB,
    ip_address    INET,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Filtre le plus courant de l'endpoint GET /api/v1/audit-log : par tenant,
-- trié/paginé par date. DESC car un journal d'audit se consulte du plus
-- récent au plus ancien.
CREATE INDEX idx_audit_log_tenant_created ON audit_log(tenant_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- Permission : ressource "audit" — réservée aux administrateurs
-- ---------------------------------------------------------------------------
INSERT INTO permissions (resource, action, description) VALUES
    ('audit', 'read', 'Consulter le journal d''audit (traçabilité des actions)')
ON CONFLICT (resource, action) DO NOTHING;

-- Redéfinit seed_system_roles : "audit:read" doit rester exclusif au rôle
-- Admin (qui l'obtient déjà via "SELECT ... FROM permissions" sans filtre,
-- inchangé). Sans intervention, le rôle "Lecture seule" l'aurait aussi
-- obtenu via son filtre générique "action = 'read'" — exclu explicitement
-- ci-dessous. Technicien n'a pas de liste à modifier : il n'y était pas
-- inclus. Voir aussi RequireAdmin dans internal/interfaces/http/
-- middleware.go, qui applique cette même restriction indépendamment de ce
-- filtre RBAC tant que RequirePermission reste un stub basé sur le seul
-- claim is_admin (cf. commentaire sur RequirePermission).
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
        ('scripts',    'execute')
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
