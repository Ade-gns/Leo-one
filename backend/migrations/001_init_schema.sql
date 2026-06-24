-- =============================================================================
-- Migration 001 : Schéma initial Leo-One RMM
-- Extensions, types ENUM, tables principales
-- =============================================================================

-- ---------------------------------------------------------------------------
-- Extensions
-- ---------------------------------------------------------------------------
CREATE EXTENSION IF NOT EXISTS "pgcrypto";      -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "timescaledb";   -- hypertables, continuous aggs

-- ---------------------------------------------------------------------------
-- Types ENUM
-- ---------------------------------------------------------------------------
CREATE TYPE agent_status    AS ENUM ('online', 'offline', 'maintenance', 'unresponsive');
CREATE TYPE agent_os        AS ENUM ('windows', 'linux', 'macos');
CREATE TYPE metric_type     AS ENUM (
    'cpu_percent',
    'ram_used_bytes',
    'ram_total_bytes',
    'disk_used_bytes',
    'disk_total_bytes',
    'net_bytes_in',
    'net_bytes_out',
    'process_count'
);
CREATE TYPE alert_severity  AS ENUM ('info', 'warning', 'critical');
CREATE TYPE alert_status    AS ENUM ('open', 'acknowledged', 'resolved');
CREATE TYPE ticket_status   AS ENUM ('open', 'in_progress', 'resolved', 'closed');
CREATE TYPE ticket_priority AS ENUM ('low', 'medium', 'high', 'critical');
CREATE TYPE command_status  AS ENUM ('pending', 'running', 'success', 'failed', 'timeout');
CREATE TYPE command_type    AS ENUM ('exec_script', 'install_pkg', 'reboot', 'collect_inventory', 'ping');

-- ---------------------------------------------------------------------------
-- Fonction utilitaire : mise à jour automatique de updated_at
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- =============================================================================
-- TABLE : tenants
-- Isolation multi-tenant racine. Chaque ligne représente un client MSP.
-- =============================================================================
CREATE TABLE tenants (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    slug        TEXT        NOT NULL UNIQUE,            -- identifiant URL-friendly
    plan        TEXT        NOT NULL DEFAULT 'starter', -- starter | pro | enterprise
    max_agents  INTEGER     NOT NULL DEFAULT 10,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_tenants_updated_at
    BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =============================================================================
-- TABLE : users
-- Techniciens et administrateurs MSP. Liés à un tenant.
-- =============================================================================
CREATE TABLE users (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email           TEXT        NOT NULL,
    password_hash   TEXT        NOT NULL,   -- argon2id
    full_name       TEXT        NOT NULL,
    is_active       BOOLEAN     NOT NULL DEFAULT TRUE,
    mfa_enabled     BOOLEAN     NOT NULL DEFAULT FALSE,
    mfa_secret_enc  TEXT,                   -- secret TOTP chiffré (AES-256-GCM, clé applicative)
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, email)
);

CREATE INDEX idx_users_tenant_id ON users(tenant_id);
CREATE INDEX idx_users_email     ON users(email);

CREATE TRIGGER trg_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =============================================================================
-- TABLES : roles, permissions, role_permissions, user_roles (RBAC)
-- =============================================================================

-- Rôles par tenant (+ rôles système non modifiables : is_system = true)
CREATE TABLE roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT,
    is_system   BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_roles_tenant_id ON roles(tenant_id);

-- Permissions atomiques : ressource + action
CREATE TABLE permissions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource    TEXT NOT NULL,  -- 'agents' | 'metrics' | 'alerts' | 'tickets' | 'users' | 'tenants' | 'scripts'
    action      TEXT NOT NULL,  -- 'read' | 'write' | 'delete' | 'execute'
    description TEXT,
    UNIQUE(resource, action)
);

-- Association rôle ↔ permissions
CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- Association utilisateur ↔ rôles
CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

-- =============================================================================
-- TABLE : workspaces
-- Groupes logiques de machines au sein d'un tenant.
-- =============================================================================
CREATE TABLE workspaces (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

CREATE INDEX idx_workspaces_tenant_id ON workspaces(tenant_id);

CREATE TRIGGER trg_workspaces_updated_at
    BEFORE UPDATE ON workspaces
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =============================================================================
-- TABLE : agents
-- Une ligne par machine cible enrollée.
-- =============================================================================
CREATE TABLE agents (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id    UUID         REFERENCES workspaces(id) ON DELETE SET NULL,
    hostname        TEXT         NOT NULL,
    os              agent_os     NOT NULL,
    os_version      TEXT         NOT NULL,
    arch            TEXT         NOT NULL,    -- 'amd64' | 'arm64'
    hardware_id     TEXT         NOT NULL,    -- UUID BIOS / machine-id / IOPlatformUUID
    ip_address      INET,
    fqdn            TEXT,
    agent_version   TEXT         NOT NULL,
    status          agent_status NOT NULL DEFAULT 'offline',
    last_seen_at    TIMESTAMPTZ,
    enrolled_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, hardware_id)
);

CREATE INDEX idx_agents_tenant_id    ON agents(tenant_id);
CREATE INDEX idx_agents_workspace_id ON agents(workspace_id);
CREATE INDEX idx_agents_status       ON agents(tenant_id, status);
CREATE INDEX idx_agents_last_seen    ON agents(tenant_id, last_seen_at DESC);

CREATE TRIGGER trg_agents_updated_at
    BEFORE UPDATE ON agents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =============================================================================
-- TABLE : agent_certificates
-- Certificats mTLS émis par le CA interne pour chaque agent.
-- =============================================================================
CREATE TABLE agent_certificates (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id        UUID        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    serial_number   TEXT        NOT NULL UNIQUE,
    thumbprint      TEXT        NOT NULL UNIQUE,  -- SHA-256 du certificat
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    revoke_reason   TEXT
);

CREATE INDEX idx_agent_certs_agent_id   ON agent_certificates(agent_id);
CREATE INDEX idx_agent_certs_thumbprint ON agent_certificates(thumbprint);

-- =============================================================================
-- TABLE : enrollment_tokens
-- Tokens JWT one-shot pour l'enrollment initial des agents.
-- =============================================================================
CREATE TABLE enrollment_tokens (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id UUID        REFERENCES workspaces(id) ON DELETE SET NULL,
    token_hash   TEXT        NOT NULL UNIQUE,  -- SHA-256 du token brut
    label        TEXT,                          -- libellé lisible pour le technicien
    used_at      TIMESTAMPTZ,
    used_by      UUID        REFERENCES agents(id) ON DELETE SET NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_by   UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_enrollment_tokens_tenant ON enrollment_tokens(tenant_id);

-- =============================================================================
-- TABLE : metrics  [TimescaleDB hypertable — voir migration 002]
-- Séries temporelles : CPU, RAM, disque, réseau.
-- =============================================================================
CREATE TABLE metrics (
    time      TIMESTAMPTZ        NOT NULL,
    agent_id  UUID               NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tenant_id UUID               NOT NULL,
    type      metric_type        NOT NULL,
    value     DOUBLE PRECISION   NOT NULL,
    labels    JSONB                        -- ex: {"interface":"eth0"} ou {"mount":"/"}
);

-- L'index sera créé par TimescaleDB lors de create_hypertable (migration 002)

-- =============================================================================
-- TABLE : alert_rules
-- Règles de déclenchement d'alertes (évaluées côté backend toutes les N secondes).
-- =============================================================================
CREATE TABLE alert_rules (
    id             UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workspace_id   UUID           REFERENCES workspaces(id) ON DELETE SET NULL,
    agent_id       UUID           REFERENCES agents(id) ON DELETE CASCADE,  -- NULL = s'applique au workspace
    name           TEXT           NOT NULL,
    description    TEXT,
    metric_type    metric_type    NOT NULL,
    operator       TEXT           NOT NULL CHECK (operator IN ('>', '>=', '<', '<=', '=')),
    threshold      DOUBLE PRECISION NOT NULL,
    duration_secs  INTEGER        NOT NULL DEFAULT 60,
    severity       alert_severity NOT NULL DEFAULT 'warning',
    is_active      BOOLEAN        NOT NULL DEFAULT TRUE,
    created_by     UUID           REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alert_rules_tenant    ON alert_rules(tenant_id);
CREATE INDEX idx_alert_rules_active    ON alert_rules(tenant_id, is_active);

CREATE TRIGGER trg_alert_rules_updated_at
    BEFORE UPDATE ON alert_rules
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =============================================================================
-- TABLE : alerts
-- Instances d'alertes déclenchées.
-- =============================================================================
CREATE TABLE alerts (
    id               UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID           NOT NULL,
    agent_id         UUID           NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    rule_id          UUID           REFERENCES alert_rules(id) ON DELETE SET NULL,
    severity         alert_severity NOT NULL,
    status           alert_status   NOT NULL DEFAULT 'open',
    title            TEXT           NOT NULL,
    description      TEXT,
    metric_value     DOUBLE PRECISION,
    triggered_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    acknowledged_at  TIMESTAMPTZ,
    acknowledged_by  UUID           REFERENCES users(id) ON DELETE SET NULL,
    resolved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_alerts_tenant_status ON alerts(tenant_id, status);
CREATE INDEX idx_alerts_agent_id      ON alerts(agent_id);
CREATE INDEX idx_alerts_triggered_at  ON alerts(tenant_id, triggered_at DESC);

-- =============================================================================
-- TABLE : commands
-- Historique de toutes les commandes envoyées aux agents.
-- =============================================================================
CREATE TABLE commands (
    id           UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID           NOT NULL,
    agent_id     UUID           NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_by   UUID           REFERENCES users(id) ON DELETE SET NULL,
    type         command_type   NOT NULL,
    payload      JSONB          NOT NULL,   -- script, args, pkg_url, etc.
    status       command_status NOT NULL DEFAULT 'pending',
    stdout       TEXT,
    stderr       TEXT,
    exit_code    INTEGER,
    sent_at      TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_commands_agent     ON commands(agent_id, created_at DESC);
CREATE INDEX idx_commands_tenant    ON commands(tenant_id, created_at DESC);
CREATE INDEX idx_commands_status    ON commands(status) WHERE status IN ('pending', 'running');

-- =============================================================================
-- TABLE : hardware_inventory
-- Snapshot matériel par agent (mis à jour à chaque collect_inventory).
-- =============================================================================
CREATE TABLE hardware_inventory (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id         UUID        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tenant_id        UUID        NOT NULL,
    cpu_model        TEXT,
    cpu_cores        INTEGER,
    cpu_threads      INTEGER,
    ram_total_bytes  BIGINT,
    disk_count       INTEGER,
    bios_version     TEXT,
    bios_vendor      TEXT,
    motherboard      TEXT,
    serial_number    TEXT,
    collected_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    raw              JSONB       -- données brutes complètes pour extensibilité future
);

CREATE INDEX idx_hw_inventory_agent ON hardware_inventory(agent_id, collected_at DESC);

-- =============================================================================
-- TABLE : software_inventory
-- Liste des logiciels installés par agent.
-- =============================================================================
CREATE TABLE software_inventory (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id     UUID        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tenant_id    UUID        NOT NULL,
    name         TEXT        NOT NULL,
    version      TEXT,
    publisher    TEXT,
    install_date DATE,
    install_path TEXT,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sw_inventory_agent ON software_inventory(agent_id, collected_at DESC);
CREATE INDEX idx_sw_inventory_name  ON software_inventory(tenant_id, name);

-- =============================================================================
-- TABLE : tickets
-- Tickets de support intégrés, liés optionnellement à un agent ou une alerte.
-- =============================================================================
CREATE TABLE tickets (
    id           UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID            NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     UUID            REFERENCES agents(id) ON DELETE SET NULL,
    alert_id     UUID            REFERENCES alerts(id) ON DELETE SET NULL,
    title        TEXT            NOT NULL,
    description  TEXT,
    status       ticket_status   NOT NULL DEFAULT 'open',
    priority     ticket_priority NOT NULL DEFAULT 'medium',
    assigned_to  UUID            REFERENCES users(id) ON DELETE SET NULL,
    created_by   UUID            REFERENCES users(id) ON DELETE SET NULL,
    resolved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tickets_tenant_status ON tickets(tenant_id, status);
CREATE INDEX idx_tickets_assigned      ON tickets(assigned_to);
CREATE INDEX idx_tickets_agent         ON tickets(agent_id);

CREATE TRIGGER trg_tickets_updated_at
    BEFORE UPDATE ON tickets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- =============================================================================
-- TABLE : ticket_comments
-- Commentaires et historique d'activité sur les tickets.
-- =============================================================================
CREATE TABLE ticket_comments (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id  UUID        NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    tenant_id  UUID        NOT NULL,
    author_id  UUID        REFERENCES users(id) ON DELETE SET NULL,
    body       TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ticket_comments_ticket ON ticket_comments(ticket_id);

CREATE TRIGGER trg_ticket_comments_updated_at
    BEFORE UPDATE ON ticket_comments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
