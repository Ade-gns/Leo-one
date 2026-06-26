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
		SELECT id, name, is_active
		FROM tenants
		WHERE id = $1
	`, id).Scan(&t.ID, &t.Name, &t.IsActive)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &t, nil
}
