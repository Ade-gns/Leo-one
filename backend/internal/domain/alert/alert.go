// Package alert définit l'entité Alert et son interface de domaine.
// Cette couche ne connaît aucune dépendance externe (pas de DB, pas de HTTP).
package alert

import (
	"context"
	"time"
)

// Severity représente le niveau de gravité d'une alerte.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Status représente l'état de traitement d'une alerte.
type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusResolved     Status = "resolved"
)

// Alert est une instance d'alerte déclenchée pour un agent.
type Alert struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	AgentID        string     `json:"agent_id"`
	AgentHostname  string     `json:"agent_hostname"`
	RuleID         *string    `json:"rule_id,omitempty"`
	Severity       Severity   `json:"severity"`
	Status         Status     `json:"status"`
	Title          string     `json:"title"`
	Description    *string    `json:"description,omitempty"`
	MetricValue    *float64   `json:"metric_value,omitempty"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ListFilter contient les critères optionnels pour lister les alertes.
type ListFilter struct {
	Status   *Status
	Severity *Severity
	AgentID  *string
	Cursor   string
	Limit    int
}

// Repository définit le contrat de persistance pour les alertes.
// Implémenté dans internal/infrastructure/persistence/postgres/alert_repo.go
type Repository interface {
	// List retourne la liste paginée des alertes d'un tenant.
	List(ctx context.Context, tenantID string, filter ListFilter) ([]*Alert, string, error)

	// FindByID retourne une alerte appartenant au tenant donné (nil si absente).
	FindByID(ctx context.Context, tenantID, alertID string) (*Alert, error)

	// Acknowledge marque une alerte comme acquittée par l'utilisateur donné.
	// Retourne l'alerte mise à jour, ou nil si elle n'existe pas (ou hors tenant).
	Acknowledge(ctx context.Context, tenantID, alertID, userID string) (*Alert, error)
}
