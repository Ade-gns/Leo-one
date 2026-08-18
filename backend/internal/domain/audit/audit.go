// Package audit définit l'entité Entry (journal d'audit) et son interface
// de persistance. Cette couche ne connaît aucune dépendance externe (pas de
// DB, pas de HTTP) — en particulier pas de httpctx : l'extraction
// tenant_id/user_id/IP depuis le contexte HTTP se fait dans la couche
// interfaces (voir internal/interfaces/http/handlers/audit_logger.go).
package audit

import (
	"context"
	"encoding/json"
	"time"
)

// Entry est une ligne du journal d'audit — une action d'écriture effectuée
// via l'API (agent supprimé, rôle modifié, alerte acquittée, ...).
type Entry struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	UserID       *string         `json:"user_id,omitempty"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   *string         `json:"resource_id,omitempty"`
	Details      json.RawMessage `json:"details,omitempty"`
	IPAddress    *string         `json:"ip_address,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// ListFilter contient les critères optionnels pour lister le journal d'audit.
type ListFilter struct {
	UserID       *string
	Action       *string
	ResourceType *string
	From         *time.Time
	To           *time.Time
	Cursor       string
	Limit        int
}

// Repository définit le contrat de persistance pour le journal d'audit.
// Implémenté dans internal/infrastructure/persistence/postgres/audit_repo.go
type Repository interface {
	// Create insère une nouvelle entrée. Remplit e.ID/CreatedAt en retour.
	Create(ctx context.Context, e *Entry) error

	// List retourne la liste paginée des entrées d'un tenant (cursor-based
	// pagination, mêmes conventions que alert.Repository.List).
	List(ctx context.Context, tenantID string, filter ListFilter) ([]*Entry, string, error)
}
