// Package tenant définit l'entité Tenant et son interface de persistance.
package tenant

import "context"

// Tenant représente un client/locataire dans le système multi-tenant.
type Tenant struct {
	ID       string
	Name     string
	IsActive bool
}

// Repository définit le contrat de persistance pour les tenants.
type Repository interface {
	FindByID(ctx context.Context, id string) (*Tenant, error)
}
