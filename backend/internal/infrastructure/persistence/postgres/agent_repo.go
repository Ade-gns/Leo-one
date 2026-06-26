package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
)

// AgentRepo implémente agent.Repository via pgx/v5.
type AgentRepo struct {
	pool *pgxpool.Pool
}

// NewAgentRepo crée un AgentRepo avec le pool de connexions fourni.
func NewAgentRepo(pool *pgxpool.Pool) *AgentRepo {
	return &AgentRepo{pool: pool}
}

// ensureCtx retourne context.Background() si ctx est nil (compatibilité dispatcher.go).
func ensureCtx(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// FindByID retourne un agent appartenant au tenant donné.
// Isolation multi-tenant garantie par la clause WHERE tenant_id.
func (r *AgentRepo) FindByID(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
	ctx = ensureCtx(ctx)

	var a agentDomain.Agent
	var ipAddress *string
	var workspaceID *string
	var fqdn *string

	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, workspace_id, hostname, fqdn,
		       os::text, os_version, arch, hardware_id,
		       ip_address::text, agent_version, status::text,
		       last_seen_at, enrolled_at, created_at, updated_at
		FROM agents
		WHERE id = $1 AND tenant_id = $2
	`, agentID, tenantID).Scan(
		&a.ID, &a.TenantID, &workspaceID, &a.Hostname, &fqdn,
		&a.OS, &a.OSVersion, &a.Arch, &a.HardwareID,
		&ipAddress, &a.AgentVersion, &a.Status,
		&a.LastSeenAt, &a.EnrolledAt, &a.CreatedAt, &a.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	a.WorkspaceID = workspaceID
	a.FQDN = fqdn
	a.IPAddress = ipAddress

	return &a, nil
}

// FindByHardwareID retourne un agent par son hardware_id (pour éviter les doublons d'enrollment).
func (r *AgentRepo) FindByHardwareID(ctx context.Context, tenantID, hardwareID string) (*agentDomain.Agent, error) {
	ctx = ensureCtx(ctx)

	var a agentDomain.Agent
	var ipAddress *string
	var workspaceID *string
	var fqdn *string

	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, workspace_id, hostname, fqdn,
		       os::text, os_version, arch, hardware_id,
		       ip_address::text, agent_version, status::text,
		       last_seen_at, enrolled_at, created_at, updated_at
		FROM agents
		WHERE hardware_id = $1 AND tenant_id = $2
	`, hardwareID, tenantID).Scan(
		&a.ID, &a.TenantID, &workspaceID, &a.Hostname, &fqdn,
		&a.OS, &a.OSVersion, &a.Arch, &a.HardwareID,
		&ipAddress, &a.AgentVersion, &a.Status,
		&a.LastSeenAt, &a.EnrolledAt, &a.CreatedAt, &a.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	a.WorkspaceID = workspaceID
	a.FQDN = fqdn
	a.IPAddress = ipAddress

	return &a, nil
}

// List retourne la liste paginée des agents d'un tenant (cursor-based pagination).
func (r *AgentRepo) List(ctx context.Context, tenantID string, filter agentDomain.ListFilter) ([]*agentDomain.Agent, string, error) {
	ctx = ensureCtx(ctx)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	args := []any{tenantID}
	where := `WHERE a.tenant_id = $1`
	argN := 2

	if filter.Status != nil {
		where += ` AND a.status = $` + itoa(argN) + `::agent_status`
		args = append(args, string(*filter.Status))
		argN++
	}

	if filter.WorkspaceID != nil {
		where += ` AND a.workspace_id = $` + itoa(argN)
		args = append(args, *filter.WorkspaceID)
		argN++
	}

	if filter.Cursor != "" {
		where += ` AND a.id > $` + itoa(argN)
		args = append(args, filter.Cursor)
		argN++
	}

	// +1 pour détecter s'il y a une page suivante
	args = append(args, limit+1)
	query := `
		SELECT a.id, a.tenant_id, a.workspace_id, a.hostname, a.fqdn,
		       a.os::text, a.os_version, a.arch, a.hardware_id,
		       a.ip_address::text, a.agent_version, a.status::text,
		       a.last_seen_at, a.enrolled_at, a.created_at, a.updated_at
		FROM agents a
		` + where + `
		ORDER BY a.id ASC
		LIMIT $` + itoa(argN)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	agents := make([]*agentDomain.Agent, 0, limit)
	for rows.Next() {
		var a agentDomain.Agent
		var ipAddress *string
		var workspaceID *string
		var fqdn *string

		if err := rows.Scan(
			&a.ID, &a.TenantID, &workspaceID, &a.Hostname, &fqdn,
			&a.OS, &a.OSVersion, &a.Arch, &a.HardwareID,
			&ipAddress, &a.AgentVersion, &a.Status,
			&a.LastSeenAt, &a.EnrolledAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, "", err
		}

		a.WorkspaceID = workspaceID
		a.FQDN = fqdn
		a.IPAddress = ipAddress
		agents = append(agents, &a)
	}

	if rows.Err() != nil {
		return nil, "", rows.Err()
	}

	// Cursor-based pagination : si on a plus de `limit` résultats, il y a une page suivante
	var nextCursor string
	if len(agents) > limit {
		nextCursor = agents[limit-1].ID
		agents = agents[:limit]
	}

	return agents, nextCursor, nil
}

// Create insère un nouvel agent.
func (r *AgentRepo) Create(ctx context.Context, a *agentDomain.Agent) error {
	ctx = ensureCtx(ctx)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO agents (
			id, tenant_id, workspace_id, hostname, fqdn,
			os, os_version, arch, hardware_id, ip_address,
			agent_version, status, last_seen_at, enrolled_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6::agent_os, $7, $8, $9, $10::inet,
			$11, $12::agent_status, $13, $14, $15, $16
		)
	`,
		a.ID, a.TenantID, a.WorkspaceID, a.Hostname, a.FQDN,
		string(a.OS), a.OSVersion, a.Arch, a.HardwareID, a.IPAddress,
		a.AgentVersion, string(a.Status), a.LastSeenAt, a.EnrolledAt, a.CreatedAt, a.UpdatedAt,
	)

	return err
}

// Update met à jour un agent existant.
func (r *AgentRepo) Update(ctx context.Context, a *agentDomain.Agent) error {
	ctx = ensureCtx(ctx)

	_, err := r.pool.Exec(ctx, `
		UPDATE agents SET
			workspace_id  = $1,
			hostname      = $2,
			fqdn          = $3,
			agent_version = $4,
			updated_at    = $5
		WHERE id = $6 AND tenant_id = $7
	`,
		a.WorkspaceID, a.Hostname, a.FQDN,
		a.AgentVersion, a.UpdatedAt,
		a.ID, a.TenantID,
	)

	return err
}

// UpdateStatus met à jour uniquement le statut et last_seen_at (appelé fréquemment par le dispatcher).
func (r *AgentRepo) UpdateStatus(ctx context.Context, agentID string, status agentDomain.Status, lastSeen *time.Time) error {
	ctx = ensureCtx(ctx)

	_, err := r.pool.Exec(ctx, `
		UPDATE agents
		SET status = $1::agent_status, last_seen_at = $2, updated_at = NOW()
		WHERE id = $3
	`, string(status), lastSeen, agentID)

	return err
}

// Delete supprime un agent.
func (r *AgentRepo) Delete(ctx context.Context, tenantID, agentID string) error {
	ctx = ensureCtx(ctx)

	_, err := r.pool.Exec(ctx, `
		DELETE FROM agents WHERE id = $1 AND tenant_id = $2
	`, agentID, tenantID)

	return err
}

// CountByTenant retourne le nombre d'agents pour un tenant.
func (r *AgentRepo) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	ctx = ensureCtx(ctx)

	var count int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agents WHERE tenant_id = $1
	`, tenantID).Scan(&count)

	return count, err
}

// itoa convertit un entier en string (helper interne pour construire les requêtes SQL paramétrées).
func itoa(n int) string {
	var buf [10]byte
	pos := len(buf)
	for n >= 10 {
		pos--
		buf[pos] = byte(n%10) + '0'
		n /= 10
	}
	pos--
	buf[pos] = byte(n) + '0'
	return string(buf[pos:])
}
