// Package patch définit l'entité Patch (mise à jour système connue pour un
// agent) et son interface de domaine. Cette couche ne connaît aucune
// dépendance externe (pas de DB, pas de HTTP).
package patch

import (
	"context"
	"time"
)

// Severity reflète leo_patch_severity_t côté agent (voir agent/include/leo_agent.h).
type Severity string

const (
	SeverityOptional  Severity = "optional"
	SeverityImportant Severity = "important"
	SeverityCritical  Severity = "critical"
)

// Status suit le cycle de vie d'un patch pour un agent donné : détecté
// disponible, installé (avec succès), ignoré (jamais posé automatiquement —
// réservé à un usage futur), ou en échec d'installation.
type Status string

const (
	StatusAvailable Status = "available"
	StatusInstalled Status = "installed"
	StatusIgnored   Status = "ignored"
	StatusFailed    Status = "failed"
)

// Patch est un correctif/mise à jour connu pour un agent.
type Patch struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	AgentID     string     `json:"agent_id"`
	NativeID    string     `json:"native_id"`
	Title       string     `json:"title"`
	Severity    Severity   `json:"severity"`
	SizeBytes   *int64     `json:"size_bytes,omitempty"`
	Status      Status     `json:"status"`
	DetectedAt  time.Time  `json:"detected_at"`
	InstalledAt *time.Time `json:"installed_at,omitempty"`
}

// Report est un patch tel que rapporté par l'agent dans LEO_MSG_PATCH_INVENTORY
// — uniquement les champs que l'agent connaît, sans les métadonnées serveur
// (tenant/statut/dates) déjà gérées par PatchRepo.Upsert.
type Report struct {
	NativeID  string
	Title     string
	Severity  Severity
	SizeBytes uint64
}

// ListFilter contient les critères optionnels pour lister les patchs d'un agent.
type ListFilter struct {
	Status *Status
	Cursor string
	Limit  int
}

// TenantSummary agrège l'état des patchs à l'échelle du tenant — utilisé par
// le widget "patchs critiques en attente" du dashboard.
type TenantSummary struct {
	AgentsWithCriticalPending int `json:"agents_with_critical_pending"`
	AgentsWithPendingPatches  int `json:"agents_with_pending_patches"`
	TotalPendingPatches       int `json:"total_pending_patches"`
}

// Repository définit le contrat de persistance pour les patchs.
// Implémenté dans internal/infrastructure/persistence/postgres/patch_repo.go
type Repository interface {
	// ListByAgent retourne la liste paginée des patchs connus pour un agent
	// (cursor-based pagination, même convention que alert.Repository.List).
	ListByAgent(ctx context.Context, tenantID, agentID string, filter ListFilter) ([]*Patch, string, error)

	// Upsert insère ou met à jour les patchs rapportés par un agent — un
	// patch déjà connu (même native_id) repasse en status='available' avec
	// les métadonnées à jour ; un patch absent du rapport n'est jamais
	// touché ici (voir MarkInstallResult pour les transitions installé/échoué).
	Upsert(ctx context.Context, tenantID, agentID string, reports []Report) error

	// AvailableNativeIDs retourne les native_id des patchs actuellement
	// disponibles pour un agent, dont la sévérité est >= minSeverity — utilisé
	// par l'installation groupée (chaque agent peut avoir un jeu de patchs
	// différent, contrairement à une commande générique).
	AvailableNativeIDs(ctx context.Context, tenantID, agentID string, minSeverity Severity) ([]string, error)

	// MarkInstallResult passe les patchs dont le native_id figure dans
	// nativeIDs à 'installed' (et installed_at=NOW()) si success, ou
	// 'failed' sinon — appelé depuis le CMD_RESULT qui clôt une commande
	// install_patches (voir dispatcher.go).
	MarkInstallResult(ctx context.Context, tenantID, agentID string, nativeIDs []string, success bool) error

	// Summary retourne l'agrégat par tenant utilisé par le dashboard.
	Summary(ctx context.Context, tenantID string) (TenantSummary, error)
}
