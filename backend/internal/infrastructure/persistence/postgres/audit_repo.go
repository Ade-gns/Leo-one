package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	auditDomain "github.com/yourorg/leo-one/internal/domain/audit"
)

// AuditRepo implémente audit.Repository via pgx/v5.
type AuditRepo struct {
	pool *pgxpool.Pool
}

// NewAuditRepo crée un AuditRepo avec le pool de connexions fourni.
func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo {
	return &AuditRepo{pool: pool}
}

// Create insère une nouvelle entrée d'audit. Remplit e.ID/CreatedAt en retour.
func (r *AuditRepo) Create(ctx context.Context, e *auditDomain.Entry) error {
	ctx = ensureCtx(ctx)

	return r.pool.QueryRow(ctx, `
		INSERT INTO audit_log (tenant_id, user_id, action, resource_type, resource_id, details, ip_address)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`, e.TenantID, e.UserID, e.Action, e.ResourceType, e.ResourceID, e.Details, e.IPAddress).Scan(&e.ID, &e.CreatedAt)
}

// List retourne la liste paginée des entrées d'audit d'un tenant
// (cursor-based pagination — même convention que AlertRepo.List : tri par
// id croissant, curseur = dernier id de la page précédente).
func (r *AuditRepo) List(ctx context.Context, tenantID string, filter auditDomain.ListFilter) ([]*auditDomain.Entry, string, error) {
	ctx = ensureCtx(ctx)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	args := []any{tenantID}
	where := `WHERE tenant_id = $1`
	argN := 2

	if filter.UserID != nil {
		where += ` AND user_id = $` + itoa(argN)
		args = append(args, *filter.UserID)
		argN++
	}

	if filter.Action != nil {
		where += ` AND action = $` + itoa(argN)
		args = append(args, *filter.Action)
		argN++
	}

	if filter.ResourceType != nil {
		where += ` AND resource_type = $` + itoa(argN)
		args = append(args, *filter.ResourceType)
		argN++
	}

	if filter.From != nil {
		where += ` AND created_at >= $` + itoa(argN)
		args = append(args, *filter.From)
		argN++
	}

	if filter.To != nil {
		where += ` AND created_at <= $` + itoa(argN)
		args = append(args, *filter.To)
		argN++
	}

	if filter.Cursor != "" {
		where += ` AND id > $` + itoa(argN)
		args = append(args, filter.Cursor)
		argN++
	}

	args = append(args, limit+1)
	// host(ip_address), pas ip_address::text : le cast texte d'un inet inclut
	// le masque de sous-réseau ("203.0.113.5/32"), que host() retire.
	query := `
		SELECT id, tenant_id, user_id, action, resource_type, resource_id, details, host(ip_address), created_at
		FROM audit_log
		` + where + `
		ORDER BY id ASC
		LIMIT $` + itoa(argN)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	entries := make([]*auditDomain.Entry, 0, limit)
	for rows.Next() {
		var e auditDomain.Entry
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.UserID, &e.Action, &e.ResourceType, &e.ResourceID,
			&e.Details, &e.IPAddress, &e.CreatedAt,
		); err != nil {
			return nil, "", err
		}
		entries = append(entries, &e)
	}
	if rows.Err() != nil {
		return nil, "", rows.Err()
	}

	var nextCursor string
	if len(entries) > limit {
		nextCursor = entries[limit-1].ID
		entries = entries[:limit]
	}

	return entries, nextCursor, nil
}

var _ auditDomain.Repository = (*AuditRepo)(nil)
