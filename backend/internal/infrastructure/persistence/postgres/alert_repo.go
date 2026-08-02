package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
)

// AlertRepo implémente alert.Repository via pgx/v5.
type AlertRepo struct {
	pool *pgxpool.Pool
}

// NewAlertRepo crée un AlertRepo avec le pool de connexions fourni.
func NewAlertRepo(pool *pgxpool.Pool) *AlertRepo {
	return &AlertRepo{pool: pool}
}

// FindByID retourne une alerte appartenant au tenant donné.
func (r *AlertRepo) FindByID(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
	ctx = ensureCtx(ctx)

	var a alertDomain.Alert
	err := r.pool.QueryRow(ctx, `
		SELECT al.id, al.tenant_id, al.agent_id, ag.hostname, al.rule_id,
		       al.severity::text, al.status::text, al.title, al.description,
		       al.metric_value, al.triggered_at, al.acknowledged_at,
		       al.acknowledged_by, al.resolved_at, al.created_at
		FROM alerts al
		JOIN agents ag ON ag.id = al.agent_id
		WHERE al.id = $1 AND al.tenant_id = $2
	`, alertID, tenantID).Scan(
		&a.ID, &a.TenantID, &a.AgentID, &a.AgentHostname, &a.RuleID,
		&a.Severity, &a.Status, &a.Title, &a.Description,
		&a.MetricValue, &a.TriggeredAt, &a.AcknowledgedAt,
		&a.AcknowledgedBy, &a.ResolvedAt, &a.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &a, nil
}

// List retourne la liste paginée des alertes d'un tenant (cursor-based pagination).
func (r *AlertRepo) List(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
	ctx = ensureCtx(ctx)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	args := []any{tenantID}
	where := `WHERE al.tenant_id = $1`
	argN := 2

	if filter.Status != nil {
		where += ` AND al.status = $` + itoa(argN) + `::alert_status`
		args = append(args, string(*filter.Status))
		argN++
	}

	if filter.Severity != nil {
		where += ` AND al.severity = $` + itoa(argN) + `::alert_severity`
		args = append(args, string(*filter.Severity))
		argN++
	}

	if filter.AgentID != nil {
		where += ` AND al.agent_id = $` + itoa(argN)
		args = append(args, *filter.AgentID)
		argN++
	}

	if filter.Cursor != "" {
		where += ` AND al.id > $` + itoa(argN)
		args = append(args, filter.Cursor)
		argN++
	}

	args = append(args, limit+1)
	query := `
		SELECT al.id, al.tenant_id, al.agent_id, ag.hostname, al.rule_id,
		       al.severity::text, al.status::text, al.title, al.description,
		       al.metric_value, al.triggered_at, al.acknowledged_at,
		       al.acknowledged_by, al.resolved_at, al.created_at
		FROM alerts al
		JOIN agents ag ON ag.id = al.agent_id
		` + where + `
		ORDER BY al.id ASC
		LIMIT $` + itoa(argN)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	alerts := make([]*alertDomain.Alert, 0, limit)
	for rows.Next() {
		var a alertDomain.Alert
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.AgentID, &a.AgentHostname, &a.RuleID,
			&a.Severity, &a.Status, &a.Title, &a.Description,
			&a.MetricValue, &a.TriggeredAt, &a.AcknowledgedAt,
			&a.AcknowledgedBy, &a.ResolvedAt, &a.CreatedAt,
		); err != nil {
			return nil, "", err
		}
		alerts = append(alerts, &a)
	}
	if rows.Err() != nil {
		return nil, "", rows.Err()
	}

	var nextCursor string
	if len(alerts) > limit {
		nextCursor = alerts[limit-1].ID
		alerts = alerts[:limit]
	}

	return alerts, nextCursor, nil
}

// Acknowledge marque une alerte comme acquittée par l'utilisateur donné.
func (r *AlertRepo) Acknowledge(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
	ctx = ensureCtx(ctx)

	tag, err := r.pool.Exec(ctx, `
		UPDATE alerts
		SET status = 'acknowledged', acknowledged_at = NOW(), acknowledged_by = $1
		WHERE id = $2 AND tenant_id = $3
	`, userID, alertID, tenantID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}

	return r.FindByID(ctx, tenantID, alertID)
}

var _ alertDomain.Repository = (*AlertRepo)(nil)
