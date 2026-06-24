// Package metric définit l'entité MetricPoint et l'interface de persistance TimescaleDB.
package metric

import (
	"context"
	"time"
)

// Type identifie la nature d'une métrique.
type Type string

const (
	TypeCPUPercent      Type = "cpu_percent"
	TypeRAMUsedBytes    Type = "ram_used_bytes"
	TypeRAMTotalBytes   Type = "ram_total_bytes"
	TypeDiskUsedBytes   Type = "disk_used_bytes"
	TypeDiskTotalBytes  Type = "disk_total_bytes"
	TypeNetBytesIn      Type = "net_bytes_in"
	TypeNetBytesOut     Type = "net_bytes_out"
	TypeProcessCount    Type = "process_count"
)

// Point est un point de mesure unique dans la série temporelle.
type Point struct {
	Time     time.Time         `json:"time"`
	AgentID  string            `json:"agent_id"`
	TenantID string            `json:"tenant_id"`
	Type     Type              `json:"type"`
	Value    float64           `json:"value"`
	Labels   map[string]string `json:"labels,omitempty"` // ex: {"interface":"eth0"}
}

// Snapshot regroupe toutes les métriques d'un agent à un instant T.
// C'est le format reçu depuis l'agent via WebSocket.
type Snapshot struct {
	AgentID          string            `json:"agent_id"`
	TenantID         string            `json:"tenant_id"`
	Timestamp        time.Time         `json:"timestamp"`
	CPUPercent       float64           `json:"cpu_percent"`
	CPUPerCore       []float64         `json:"cpu_per_core,omitempty"`
	RAMTotalBytes    uint64            `json:"ram_total_bytes"`
	RAMUsedBytes     uint64            `json:"ram_used_bytes"`
	RAMAvailableBytes uint64           `json:"ram_available_bytes"`
	DiskTotalBytes   uint64            `json:"disk_total_bytes"`
	DiskUsedBytes    uint64            `json:"disk_used_bytes"`
	NetBytesIn       uint64            `json:"net_bytes_in"`
	NetBytesOut      uint64            `json:"net_bytes_out"`
	ProcessCount     uint32            `json:"process_count"`
}

// ToPoints convertit un Snapshot en slice de Points individuels
// prêts pour l'insertion en batch dans TimescaleDB.
func (s *Snapshot) ToPoints() []Point {
	ts := s.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	return []Point{
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeCPUPercent,     Value: s.CPUPercent},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeRAMUsedBytes,   Value: float64(s.RAMUsedBytes)},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeRAMTotalBytes,  Value: float64(s.RAMTotalBytes)},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeDiskUsedBytes,  Value: float64(s.DiskUsedBytes)},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeDiskTotalBytes, Value: float64(s.DiskTotalBytes)},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeNetBytesIn,     Value: float64(s.NetBytesIn)},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeNetBytesOut,    Value: float64(s.NetBytesOut)},
		{Time: ts, AgentID: s.AgentID, TenantID: s.TenantID, Type: TypeProcessCount,   Value: float64(s.ProcessCount)},
	}
}

// QueryResult est un point renvoyé par l'API après agrégation.
type QueryResult struct {
	Time     time.Time `json:"time"`
	Value    float64   `json:"value"`
	AvgValue float64   `json:"avg,omitempty"`
	MaxValue float64   `json:"max,omitempty"`
	MinValue float64   `json:"min,omitempty"`
}

// Resolution indique la granularité temporelle utilisée.
type Resolution string

const (
	ResolutionRaw Resolution = "raw" // données brutes (~60s)
	Resolution1h  Resolution = "1h"  // agrégat horaire
	Resolution1d  Resolution = "1d"  // agrégat journalier
)

// ChooseResolution détermine la résolution optimale selon la plage temporelle.
func ChooseResolution(from, to time.Time) Resolution {
	d := to.Sub(from)
	switch {
	case d <= 6*time.Hour:
		return ResolutionRaw
	case d <= 7*24*time.Hour:
		return Resolution1h
	default:
		return Resolution1d
	}
}

// Repository définit le contrat de persistance pour les métriques.
// Implémenté dans internal/infrastructure/persistence/postgres/metric_repo.go
type Repository interface {
	// InsertBatch insère plusieurs points en une seule transaction (COPY).
	InsertBatch(ctx context.Context, points []Point) error

	// Query retourne les métriques d'un agent sur une plage de temps.
	// La résolution est choisie automatiquement selon la plage.
	Query(ctx context.Context, tenantID, agentID string, metricType Type,
		from, to time.Time) ([]QueryResult, Resolution, error)

	// Latest retourne la dernière valeur connue pour chaque type de métrique d'un agent.
	Latest(ctx context.Context, tenantID, agentID string) (map[Type]float64, time.Time, error)
}
