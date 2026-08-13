package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	workspaceDomain "github.com/yourorg/leo-one/internal/domain/workspace"
)

// WorkspaceRepo implémente workspace.Repository via pgx/v5.
type WorkspaceRepo struct {
	pool *pgxpool.Pool
}

// NewWorkspaceRepo crée un WorkspaceRepo avec le pool de connexions fourni.
func NewWorkspaceRepo(pool *pgxpool.Pool) *WorkspaceRepo {
	return &WorkspaceRepo{pool: pool}
}

// FindByID retourne un workspace appartenant au tenant donné.
// Isolation multi-tenant garantie par la clause WHERE tenant_id.
func (r *WorkspaceRepo) FindByID(ctx context.Context, tenantID, workspaceID string) (*workspaceDomain.Workspace, error) {
	var w workspaceDomain.Workspace
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, description, created_at, updated_at
		FROM workspaces WHERE id = $1 AND tenant_id = $2
	`, workspaceID, tenantID).Scan(&w.ID, &w.TenantID, &w.Name, &w.Description, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// List retourne tous les workspaces d'un tenant, triés par nom.
func (r *WorkspaceRepo) List(ctx context.Context, tenantID string) ([]*workspaceDomain.Workspace, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, description, created_at, updated_at
		FROM workspaces WHERE tenant_id = $1
		ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workspaces := make([]*workspaceDomain.Workspace, 0)
	for rows.Next() {
		var w workspaceDomain.Workspace
		if err := rows.Scan(&w.ID, &w.TenantID, &w.Name, &w.Description, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, &w)
	}
	return workspaces, rows.Err()
}

// Create insère un nouveau workspace. Remplit w.ID/CreatedAt/UpdatedAt en retour.
func (r *WorkspaceRepo) Create(ctx context.Context, w *workspaceDomain.Workspace) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO workspaces (tenant_id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at
	`, w.TenantID, w.Name, w.Description).Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt)
}

// Update met à jour name/description d'un workspace existant.
func (r *WorkspaceRepo) Update(ctx context.Context, w *workspaceDomain.Workspace) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE workspaces SET name = $1, description = $2, updated_at = NOW()
		WHERE id = $3 AND tenant_id = $4
	`, w.Name, w.Description, w.ID, w.TenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Delete supprime un workspace. Les agents qui y étaient rattachés passent
// à workspace_id = NULL (ON DELETE SET NULL, voir migrations/001) —
// jamais supprimés.
func (r *WorkspaceRepo) Delete(ctx context.Context, tenantID, workspaceID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM workspaces WHERE id = $1 AND tenant_id = $2`, workspaceID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

var _ workspaceDomain.Repository = (*WorkspaceRepo)(nil)
