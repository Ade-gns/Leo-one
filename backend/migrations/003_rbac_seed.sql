-- =============================================================================
-- Migration 003 : Données initiales RBAC
-- Permissions atomiques + rôles système par défaut
-- =============================================================================

-- ---------------------------------------------------------------------------
-- Permissions atomiques (resource × action)
-- ---------------------------------------------------------------------------
INSERT INTO permissions (resource, action, description) VALUES
    -- Agents
    ('agents',  'read',    'Voir la liste et le détail des agents'),
    ('agents',  'write',   'Modifier les paramètres d''un agent'),
    ('agents',  'delete',  'Supprimer un agent'),
    ('agents',  'execute', 'Exécuter des scripts ou commandes sur un agent'),
    -- Métriques
    ('metrics', 'read',    'Consulter les métriques et graphiques'),
    -- Alertes
    ('alerts',  'read',          'Voir les alertes déclenchées'),
    ('alerts',  'acknowledge',   'Acquitter une alerte'),
    ('alerts',  'write',         'Créer/modifier des règles d''alerte'),
    ('alerts',  'delete',        'Supprimer des règles d''alerte'),
    -- Tickets
    ('tickets', 'read',   'Voir les tickets'),
    ('tickets', 'write',  'Créer et modifier des tickets'),
    ('tickets', 'delete', 'Supprimer des tickets'),
    -- Utilisateurs
    ('users',   'read',   'Voir la liste des utilisateurs'),
    ('users',   'write',  'Créer et modifier des utilisateurs'),
    ('users',   'delete', 'Supprimer des utilisateurs'),
    -- Inventaire
    ('inventory', 'read', 'Consulter l''inventaire HW/SW'),
    -- Workspaces
    ('workspaces', 'read',   'Voir les workspaces'),
    ('workspaces', 'write',  'Créer et modifier des workspaces'),
    ('workspaces', 'delete', 'Supprimer des workspaces'),
    -- Tenant (administration globale)
    ('tenant',  'read',   'Voir les paramètres du tenant'),
    ('tenant',  'write',  'Modifier les paramètres du tenant')
ON CONFLICT (resource, action) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Fonction helper : crée les rôles système pour un tenant donné
-- Appelée lors de la création d'un nouveau tenant (depuis le backend Go).
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION seed_system_roles(p_tenant_id UUID)
RETURNS VOID AS $$
DECLARE
    role_admin_id      UUID;
    role_tech_id       UUID;
    role_readonly_id   UUID;
    perm               RECORD;
BEGIN
    -- Rôle : Admin (toutes les permissions)
    INSERT INTO roles (tenant_id, name, description, is_system)
    VALUES (p_tenant_id, 'Admin', 'Accès complet à toutes les fonctionnalités', TRUE)
    RETURNING id INTO role_admin_id;

    INSERT INTO role_permissions (role_id, permission_id)
    SELECT role_admin_id, id FROM permissions;

    -- Rôle : Technicien (tout sauf gestion utilisateurs/tenant)
    INSERT INTO roles (tenant_id, name, description, is_system)
    VALUES (p_tenant_id, 'Technicien', 'Gestion des agents, alertes et tickets', TRUE)
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
        ('tickets',    'read'),
        ('tickets',    'write'),
        ('inventory',  'read'),
        ('workspaces', 'read')
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
