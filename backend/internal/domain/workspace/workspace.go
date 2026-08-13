// Package workspace définit l'entité Workspace et son interface de
// persistance. Cette couche ne connaît aucune dépendance externe (pas de
// DB, pas de HTTP).
package workspace

import (
	"context"
	"time"
)

// Workspace regroupe des agents au sein d'un tenant (site, département,
// client final pour un MSP, ...).
type Workspace struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Repository définit le contrat de persistance pour les workspaces.
// Implémenté dans internal/infrastructure/persistence/postgres/workspace_repo.go
type Repository interface {
	// FindByID retourne un workspace appartenant au tenant donné.
	FindByID(ctx context.Context, tenantID, workspaceID string) (*Workspace, error)

	// List retourne tous les workspaces d'un tenant, triés par nom.
	List(ctx context.Context, tenantID string) ([]*Workspace, error)

	// Create insère un nouveau workspace. Remplit w.ID/CreatedAt/UpdatedAt en retour.
	Create(ctx context.Context, w *Workspace) error

	// Update met à jour name/description d'un workspace existant.
	Update(ctx context.Context, w *Workspace) error

	// Delete supprime un workspace. Les agents qui y étaient rattachés
	// passent à workspace_id = NULL (ON DELETE SET NULL) — jamais supprimés.
	Delete(ctx context.Context, tenantID, workspaceID string) error
}
