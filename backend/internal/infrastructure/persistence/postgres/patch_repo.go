package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	patchDomain "github.com/yourorg/leo-one/internal/domain/patch"
)

// PatchRepo implémente patch.Repository via pgx/v5.
type PatchRepo struct {
	pool *pgxpool.Pool
}

// NewPatchRepo crée un PatchRepo avec le pool de connexions fourni.
func NewPatchRepo(pool *pgxpool.Pool) *PatchRepo {
	return &PatchRepo{pool: pool}
}

// ListByAgent retourne la liste paginée des patchs connus pour un agent
// (cursor-based pagination — même convention que AlertRepo.List : tri par
// id croissant, curseur = dernier id de la page précédente).
func (r *PatchRepo) ListByAgent(ctx context.Context, tenantID, agentID string, filter patchDomain.ListFilter) ([]*patchDomain.Patch, string, error) {
	ctx = ensureCtx(ctx)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	args := []any{tenantID, agentID}
	where := `WHERE tenant_id = $1 AND agent_id = $2`
	argN := 3

	if filter.Status != nil {
		where += ` AND status = $` + itoa(argN) + `::patch_status`
		args = append(args, string(*filter.Status))
		argN++
	}

	if filter.Cursor != "" {
		where += ` AND id > $` + itoa(argN)
		args = append(args, filter.Cursor)
		argN++
	}

	args = append(args, limit+1)
	query := `
		SELECT id, tenant_id, agent_id, native_id, title, severity::text, size_bytes,
		       status::text, detected_at, installed_at
		FROM patches
		` + where + `
		ORDER BY id ASC
		LIMIT $` + itoa(argN)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	patches := make([]*patchDomain.Patch, 0, limit)
	for rows.Next() {
		var p patchDomain.Patch
		var severity, status string
		if err := rows.Scan(
			&p.ID, &p.TenantID, &p.AgentID, &p.NativeID, &p.Title, &severity, &p.SizeBytes,
			&status, &p.DetectedAt, &p.InstalledAt,
		); err != nil {
			return nil, "", err
		}
		p.Severity = patchDomain.Severity(severity)
		p.Status = patchDomain.Status(status)
		patches = append(patches, &p)
	}
	if rows.Err() != nil {
		return nil, "", rows.Err()
	}

	var nextCursor string
	if len(patches) > limit {
		nextCursor = patches[limit-1].ID
		patches = patches[:limit]
	}

	return patches, nextCursor, nil
}

// Upsert insère ou met à jour les patchs rapportés par un agent. Un patch
// déjà connu (même agent_id+native_id, voir la contrainte UNIQUE) repasse
// en status='available' avec ses métadonnées à jour et installed_at remis à
// NULL — un patch qui réapparaît dans un rapport n'est, par définition, plus
// dans l'état "installé" du point de vue de l'agent, quel que soit son
// historique.
func (r *PatchRepo) Upsert(ctx context.Context, tenantID, agentID string, reports []patchDomain.Report) error {
	ctx = ensureCtx(ctx)
	if len(reports) == 0 {
		return nil
	}

	const upsertSQL = `
		INSERT INTO patches (tenant_id, agent_id, native_id, title, severity, size_bytes, status, detected_at, installed_at)
		VALUES ($1, $2, $3, $4, $5::patch_severity, $6, 'available', NOW(), NULL)
		ON CONFLICT (agent_id, native_id) DO UPDATE SET
			title        = EXCLUDED.title,
			severity     = EXCLUDED.severity,
			size_bytes   = EXCLUDED.size_bytes,
			status       = 'available',
			detected_at  = NOW(),
			installed_at = NULL
	`

	batch := &pgx.Batch{}
	for _, rep := range reports {
		batch.Queue(upsertSQL, tenantID, agentID, rep.NativeID, rep.Title, string(rep.Severity), int64(rep.SizeBytes))
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range reports {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// AvailableNativeIDs retourne les native_id des patchs disponibles pour un
// agent, de sévérité >= minSeverity (comparaison native de l'ENUM Postgres,
// ordonné optional < important < critical à la déclaration du type).
func (r *PatchRepo) AvailableNativeIDs(ctx context.Context, tenantID, agentID string, minSeverity patchDomain.Severity) ([]string, error) {
	ctx = ensureCtx(ctx)

	rows, err := r.pool.Query(ctx, `
		SELECT native_id FROM patches
		WHERE tenant_id = $1 AND agent_id = $2 AND status = 'available'
		  AND severity >= $3::patch_severity
		ORDER BY native_id
	`, tenantID, agentID, string(minSeverity))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkInstallResult passe les patchs dont le native_id figure dans nativeIDs
// à 'installed' ou 'failed' selon success.
func (r *PatchRepo) MarkInstallResult(ctx context.Context, tenantID, agentID string, nativeIDs []string, success bool) error {
	ctx = ensureCtx(ctx)
	if len(nativeIDs) == 0 {
		return nil
	}

	if success {
		_, err := r.pool.Exec(ctx, `
			UPDATE patches SET status = 'installed', installed_at = NOW()
			WHERE tenant_id = $1 AND agent_id = $2 AND native_id = ANY($3)
		`, tenantID, agentID, nativeIDs)
		return err
	}

	_, err := r.pool.Exec(ctx, `
		UPDATE patches SET status = 'failed'
		WHERE tenant_id = $1 AND agent_id = $2 AND native_id = ANY($3)
	`, tenantID, agentID, nativeIDs)
	return err
}

// Summary retourne l'agrégat par tenant utilisé par le dashboard.
func (r *PatchRepo) Summary(ctx context.Context, tenantID string) (patchDomain.TenantSummary, error) {
	ctx = ensureCtx(ctx)

	var s patchDomain.TenantSummary
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(DISTINCT agent_id) FILTER (WHERE severity = 'critical'),
			COUNT(DISTINCT agent_id),
			COUNT(*)
		FROM patches
		WHERE tenant_id = $1 AND status = 'available'
	`, tenantID).Scan(&s.AgentsWithCriticalPending, &s.AgentsWithPendingPatches, &s.TotalPendingPatches)
	return s, err
}

var _ patchDomain.Repository = (*PatchRepo)(nil)
