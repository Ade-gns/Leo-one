// Package tenant définit l'entité Tenant et son interface de persistance.
package tenant

import (
	"context"
	"time"
)

// Tenant représente un client/locataire dans le système multi-tenant.
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Plan      string    `json:"plan"`
	MaxAgents int       `json:"max_agents"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Repository définit le contrat de persistance pour les tenants.
type Repository interface {
	// FindByID retourne un tenant par son ID.
	FindByID(ctx context.Context, id string) (*Tenant, error)

	// Update met à jour un tenant existant (seul le nom est modifiable via
	// l'API — voir TenantHandler.Update ; plan/max_agents/is_active ne sont
	// pas exposés en self-service).
	Update(ctx context.Context, t *Tenant) error
}
