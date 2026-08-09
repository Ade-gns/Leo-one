// Package inventory définit les entités d'inventaire matériel/logiciel et
// leur interface de domaine. Cette couche ne connaît aucune dépendance
// externe (pas de DB, pas de HTTP).
package inventory

import (
	"context"
	"time"
)

// Hardware est le dernier snapshot matériel connu d'un agent.
type Hardware struct {
	ID            string    `json:"id"`
	AgentID       string    `json:"agent_id"`
	CPUModel      *string   `json:"cpu_model,omitempty"`
	CPUCores      *int      `json:"cpu_cores,omitempty"`
	CPUThreads    *int      `json:"cpu_threads,omitempty"`
	RAMTotalBytes *int64    `json:"ram_total_bytes,omitempty"`
	DiskCount     *int      `json:"disk_count,omitempty"`
	BIOSVersion   *string   `json:"bios_version,omitempty"`
	BIOSVendor    *string   `json:"bios_vendor,omitempty"`
	Motherboard   *string   `json:"motherboard,omitempty"`
	SerialNumber  *string   `json:"serial_number,omitempty"`
	CollectedAt   time.Time `json:"collected_at"`
}

// SoftwareItem est un logiciel installé recensé lors d'une collecte.
type SoftwareItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Version     *string    `json:"version,omitempty"`
	Publisher   *string    `json:"publisher,omitempty"`
	InstallDate *time.Time `json:"install_date,omitempty"`
	InstallPath *string    `json:"install_path,omitempty"`
	CollectedAt time.Time  `json:"collected_at"`
}

// HardwareSnapshot est le sous-ensemble du body du message INVENTORY
// (LEO_MSG_INVENTORY) reçu de l'agent — voir agent/src/protocol.c
// leo_proto_build_inventory(). Champs vides/à zéro = non disponible côté agent.
type HardwareSnapshot struct {
	CPUModel      string
	CPUCores      int
	CPUThreads    int
	RAMTotalBytes int64
	DiskCount     int
	BIOSVersion   string
	BIOSVendor    string
	Motherboard   string
	SerialNumber  string
}

// SoftwareSnapshotItem est une entrée du tableau "software" du message INVENTORY.
type SoftwareSnapshotItem struct {
	Name        string
	Version     string
	Publisher   string
	InstallPath string
}

// Repository définit le contrat de persistance pour l'inventaire.
// Implémenté dans internal/infrastructure/persistence/postgres/inventory_repo.go
type Repository interface {
	// SaveHardware enregistre un nouveau snapshot matériel (append-only :
	// hardware_inventory garde un historique, voir LatestHardware).
	SaveHardware(ctx context.Context, tenantID, agentID string, hw HardwareSnapshot) error

	// ReplaceSoftware remplace la liste des logiciels installés d'un agent
	// par celle fournie (delete + insert atomique) — contrairement au
	// matériel, on ne garde pas d'historique du logiciel installé.
	ReplaceSoftware(ctx context.Context, tenantID, agentID string, items []SoftwareSnapshotItem) error

	// LatestHardware retourne le snapshot matériel le plus récent d'un agent
	// (nil si aucune collecte n'a encore eu lieu).
	LatestHardware(ctx context.Context, tenantID, agentID string) (*Hardware, error)

	// ListSoftware retourne les logiciels installés d'un agent, filtrés par
	// un motif optionnel (recherche insensible à la casse sur le nom).
	ListSoftware(ctx context.Context, tenantID, agentID, search string) ([]*SoftwareItem, error)
}
