// Package postgres implémente les interfaces de repository via pgx/v5.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	tenantDomain "github.com/yourorg/leo-one/internal/domain/tenant"
)

// TenantRepo implémente tenant.Repository via pgx/v5.
type TenantRepo struct {
	pool *pgxpool.Pool
}

// NewTenantRepo crée un TenantRepo avec le pool de connexions fourni.
func NewTenantRepo(pool *pgxpool.Pool) *TenantRepo {
	return &TenantRepo{pool: pool}
}

// FindByID retourne un tenant par son ID.
func (r *TenantRepo) FindByID(ctx context.Context, id string) (*tenantDomain.Tenant, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	var t tenantDomain.Tenant
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, slug, plan, max_agents, is_active, created_at, updated_at
		FROM tenants
		WHERE id = $1
	`, id).Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.MaxAgents, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &t, nil
}

// Update met à jour le nom d'un tenant existant.
func (r *TenantRepo) Update(ctx context.Context, t *tenantDomain.Tenant) error {
	if ctx == nil {
		ctx = context.Background()
	}

	tag, err := r.pool.Exec(ctx, `
		UPDATE tenants SET name = $1, updated_at = NOW() WHERE id = $2
	`, t.Name, t.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

var _ tenantDomain.Repository = (*TenantRepo)(nil)
