// Package agent définit l'entité Agent et ses interfaces de domaine.
// Cette couche ne connaît aucune dépendance externe (pas de DB, pas de HTTP).
package agent

import (
	"context"
	"time"
)

// Status représente l'état de connexion d'un agent.
type Status string

const (
	StatusOnline       Status = "online"
	StatusOffline      Status = "offline"
	StatusMaintenance  Status = "maintenance"
	StatusUnresponsive Status = "unresponsive"
)

// OS représente le système d'exploitation de la machine cible.
type OS string

const (
	OSWindows OS = "windows"
	OSLinux   OS = "linux"
	OSMacOS   OS = "macos"
)

// Agent est l'entité centrale représentant une machine gérée.
type Agent struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	WorkspaceID  *string    `json:"workspace_id,omitempty"`
	Hostname     string     `json:"hostname"`
	FQDN         *string    `json:"fqdn,omitempty"`
	OS           OS         `json:"os"`
	OSVersion    string     `json:"os_version"`
	Arch         string     `json:"arch"`
	HardwareID   string     `json:"hardware_id"`
	IPAddress    *string    `json:"ip_address,omitempty"`
	AgentVersion string     `json:"agent_version"`
	Status       Status     `json:"status"`
	LastSeenAt   *time.Time `json:"last_seen_at,omitempty"`
	EnrolledAt   time.Time  `json:"enrolled_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// IsOnline retourne true si l'agent est actuellement connecté.
func (a *Agent) IsOnline() bool {
	return a.Status == StatusOnline
}

// MarkSeen met à jour le statut et le timestamp de dernière activité.
func (a *Agent) MarkSeen() {
	now := time.Now().UTC()
	a.Status     = StatusOnline
	a.LastSeenAt = &now
	a.UpdatedAt  = now
}

// MarkOffline passe l'agent en état offline.
func (a *Agent) MarkOffline() {
	a.Status    = StatusOffline
	a.UpdatedAt = time.Now().UTC()
}

// ─── Filtres de liste ─────────────────────────────────────────────────────────

// ListFilter contient les critères optionnels pour lister les agents.
type ListFilter struct {
	WorkspaceID *string
	Status      *Status
	Cursor      string
	Limit       int
}

// ─── Interfaces de domaine (ports) ───────────────────────────────────────────

// Repository définit le contrat de persistance pour les agents.
// Implémenté dans internal/infrastructure/persistence/postgres/agent_repo.go
type Repository interface {
	// FindByID retourne un agent appartenant au tenant donné.
	FindByID(ctx context.Context, tenantID, agentID string) (*Agent, error)

	// FindByHardwareID retourne un agent par son hardware_id (pour éviter les doublons d'enrollment).
	FindByHardwareID(ctx context.Context, tenantID, hardwareID string) (*Agent, error)

	// List retourne la liste paginée des agents d'un tenant.
	List(ctx context.Context, tenantID string, filter ListFilter) ([]*Agent, string, error)

	// Create insère un nouvel agent.
	Create(ctx context.Context, agent *Agent) error

	// Update met à jour un agent existant.
	Update(ctx context.Context, agent *Agent) error

	// UpdateStatus met à jour uniquement le statut et last_seen_at (appelé fréquemment).
	UpdateStatus(ctx context.Context, agentID string, status Status, lastSeen *time.Time) error

	// Delete supprime un agent et révoque son certificat.
	Delete(ctx context.Context, tenantID, agentID string) error

	// CountByTenant retourne le nombre d'agents pour un tenant (quota check).
	CountByTenant(ctx context.Context, tenantID string) (int, error)
}
