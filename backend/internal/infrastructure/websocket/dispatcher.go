package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	agentDomain     "github.com/yourorg/leo-one/internal/domain/agent"
	inventoryDomain "github.com/yourorg/leo-one/internal/domain/inventory"
	metricDomain    "github.com/yourorg/leo-one/internal/domain/metric"
)

// Enveloppe du protocole WSS (doit correspondre au format de l'agent C).
type envelope struct {
	V    int             `json:"v"`
	Type int             `json:"type"`
	ID   string          `json:"id"`
	TS   int64           `json:"ts"` // epoch millisecondes
	Body json.RawMessage `json:"body"`
}

// Types de messages entrants (doivent correspondre aux constantes de leo_agent.h).
const (
	msgTypeHello      = 1
	msgTypeHeartbeat  = 2
	msgTypeMetrics    = 3
	msgTypeInventory  = 4
	msgTypeCmdResult  = 5
	msgTypePong       = 7
)

// Intervalles envoyés dans HELLO_ACK (doivent correspondre aux défauts de
// leo_agent.h — LEO_HEARTBEAT_INTERVAL_SEC / LEO_METRICS_INTERVAL_SEC —
// sans quoi l'agent applique la valeur serveur et écrase son propre défaut).
const (
	defaultHeartbeatIntervalSec = 10800
	defaultMetricsIntervalSec   = 300
)

// helloBody est le body du message HELLO envoyé par l'agent.
type helloBody struct {
	AgentID      string `json:"agent_id"`
	TenantID     string `json:"tenant_id"`
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version"`
	Arch         string `json:"arch"`
	AgentVersion string `json:"agent_version"`
}

// metricsBody est le body du message METRICS envoyé par l'agent.
type metricsBody struct {
	CPUPercent        float64   `json:"cpu_percent"`
	CPUPerCore        []float64 `json:"cpu_per_core"`
	RAMTotalBytes     uint64    `json:"ram_total_bytes"`
	RAMUsedBytes      uint64    `json:"ram_used_bytes"`
	RAMAvailableBytes uint64    `json:"ram_available_bytes"`
	DiskTotalBytes    uint64    `json:"disk_total_bytes"`
	DiskUsedBytes     uint64    `json:"disk_used_bytes"`
	NetBytesIn        uint64    `json:"net_bytes_in"`
	NetBytesOut       uint64    `json:"net_bytes_out"`
	ProcessCount      uint32    `json:"process_count"`
}

// cmdResultBody est le body du message CMD_RESULT.
type cmdResultBody struct {
	CommandID string `json:"command_id"`
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
}

// inventoryBody est le body du message INVENTORY — voir agent/src/protocol.c
// leo_proto_build_inventory() côté agent.
type inventoryBody struct {
	Hardware hardwareBody   `json:"hardware"`
	Software []softwareBody `json:"software"`
}

type hardwareBody struct {
	CPUModel      string `json:"cpu_model"`
	CPUCores      int    `json:"cpu_cores"`
	CPUThreads    int    `json:"cpu_threads"`
	RAMTotalBytes int64  `json:"ram_total_bytes"`
	DiskCount     int    `json:"disk_count"`
	BIOSVersion   string `json:"bios_version"`
	BIOSVendor    string `json:"bios_vendor"`
	Motherboard   string `json:"motherboard"`
	SerialNumber  string `json:"serial_number"`
}

type softwareBody struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	InstallPath string `json:"install_path"`
}

// Dispatcher route les messages entrants des agents vers les use cases appropriés.
type Dispatcher struct {
	agentRepo     agentDomain.Repository
	metricRepo    metricDomain.Repository
	inventoryRepo inventoryDomain.Repository
	pool          *pgxpool.Pool // accès direct pour la table commands (pas de repo dédié)
	hub           *Hub          // référence arrière pour envoyer HELLO_ACK, etc.
	logger        *slog.Logger
}

// NewDispatcher crée un Dispatcher avec ses dépendances injectées.
func NewDispatcher(
	agentRepo agentDomain.Repository,
	metricRepo metricDomain.Repository,
	inventoryRepo inventoryDomain.Repository,
	pool *pgxpool.Pool,
	logger *slog.Logger,
) *Dispatcher {
	return &Dispatcher{
		agentRepo:     agentRepo,
		metricRepo:    metricRepo,
		inventoryRepo: inventoryRepo,
		pool:          pool,
		logger:        logger,
	}
}

// SetHub est appelé par Hub après sa création pour éviter la dépendance circulaire.
func (d *Dispatcher) SetHub(hub *Hub) {
	d.hub = hub
}

// MarkOffline passe un agent en statut offline en BDD, sans toucher à
// last_seen_at (qui doit continuer à refléter le dernier contact réel).
// Appelé par Hub.Unregister à la déconnexion WS.
func (d *Dispatcher) MarkOffline(agentID string) {
	if err := d.agentRepo.UpdateStatus(context.TODO(), agentID, agentDomain.StatusOffline, nil); err != nil {
		d.logger.Warn("Échec passage offline", "agent_id", agentID, "error", err)
	}
}

// Dispatch décode l'enveloppe JSON et route vers le handler spécialisé.
func (d *Dispatcher) Dispatch(client *Client, raw []byte) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		d.logger.Warn("Message WSS malformé",
			"agent_id", client.AgentID,
			"error", err)
		return
	}

	log := d.logger.With(
		"agent_id", client.AgentID,
		"tenant_id", client.TenantID,
		"msg_type", env.Type,
		"msg_id", env.ID,
	)

	switch env.Type {
	case msgTypeHello:
		d.handleHello(client, env, log)
	case msgTypeHeartbeat:
		d.handleHeartbeat(client, env, log)
	case msgTypeMetrics:
		d.handleMetrics(client, env, log)
	case msgTypeCmdResult:
		d.handleCmdResult(client, env, log)
	case msgTypeInventory:
		d.handleInventory(client, env, log)
	case msgTypePong:
		log.Debug("PONG reçu")
	default:
		log.Warn("Type de message inconnu", "type", env.Type)
	}
}

// ─── Handlers par type de message ───────────────────────────────────────────

func (d *Dispatcher) handleHello(client *Client, env envelope, log *slog.Logger) {
	var body helloBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		log.Error("Impossible de décoder HELLO body", "error", err)
		return
	}

	log.Info("HELLO reçu",
		"hostname", body.Hostname,
		"os", body.OS,
		"version", body.AgentVersion)

	// Mise à jour du statut en BDD
	now := time.Now().UTC()
	if err := d.agentRepo.UpdateStatus(
		nil, // ctx — à remplacer par un vrai context
		client.AgentID,
		agentDomain.StatusOnline,
		&now,
	); err != nil {
		log.Error("Échec mise à jour statut agent", "error", err)
	}

	// Envoi du HELLO_ACK avec les paramètres serveur
	ack := map[string]any{
		"v":    1,
		"type": 100, // LEO_MSG_HELLO_ACK
		"id":   env.ID,
		"ts":   time.Now().UnixMilli(),
		"body": map[string]any{
			"heartbeat_interval_sec": defaultHeartbeatIntervalSec,
			"metrics_interval_sec":   defaultMetricsIntervalSec,
		},
	}
	client.Send(ack)
}

func (d *Dispatcher) handleHeartbeat(client *Client, env envelope, log *slog.Logger) {
	now := time.Now().UTC()
	if err := d.agentRepo.UpdateStatus(
		nil,
		client.AgentID,
		agentDomain.StatusOnline,
		&now,
	); err != nil {
		log.Warn("Échec mise à jour heartbeat", "error", err)
	}
	log.Debug("Heartbeat enregistré")
}

func (d *Dispatcher) handleMetrics(client *Client, env envelope, log *slog.Logger) {
	var body metricsBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		log.Error("Impossible de décoder METRICS body", "error", err)
		return
	}

	// Conversion du timestamp agent (ms) en time.Time
	ts := time.Now().UTC()
	if env.TS > 0 {
		ts = time.UnixMilli(env.TS).UTC()
	}

	snapshot := &metricDomain.Snapshot{
		AgentID:           client.AgentID,
		TenantID:          client.TenantID,
		Timestamp:         ts,
		CPUPercent:        body.CPUPercent,
		CPUPerCore:        body.CPUPerCore,
		RAMTotalBytes:     body.RAMTotalBytes,
		RAMUsedBytes:      body.RAMUsedBytes,
		RAMAvailableBytes: body.RAMAvailableBytes,
		DiskTotalBytes:    body.DiskTotalBytes,
		DiskUsedBytes:     body.DiskUsedBytes,
		NetBytesIn:        body.NetBytesIn,
		NetBytesOut:       body.NetBytesOut,
		ProcessCount:      body.ProcessCount,
	}

	points := snapshot.ToPoints()
	if err := d.metricRepo.InsertBatch(nil, points); err != nil {
		log.Error("Échec insertion métriques", "error", err, "points", len(points))
		return
	}

	log.Debug("Métriques ingérées",
		"cpu_percent", body.CPUPercent,
		"ram_used_mb", body.RAMUsedBytes/1024/1024,
		"points", len(points))
}

func (d *Dispatcher) handleCmdResult(client *Client, env envelope, log *slog.Logger) {
	var body cmdResultBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		log.Error("Impossible de décoder CMD_RESULT body", "error", err)
		return
	}

	log.Info("Résultat de commande reçu",
		"command_id", body.CommandID,
		"exit_code", body.ExitCode)

	if body.CommandID == "" {
		log.Warn("CMD_RESULT sans command_id — ignoré")
		return
	}

	status := "success"
	if body.ExitCode != 0 {
		status = "failed"
	}

	_, err := d.pool.Exec(context.Background(), `
		UPDATE commands
		SET status = $1::command_status, exit_code = $2, stdout = $3, stderr = $4,
		    completed_at = NOW()
		WHERE id = $5 AND tenant_id = $6
	`, status, body.ExitCode, body.Stdout, body.Stderr, body.CommandID, client.TenantID)
	if err != nil {
		log.Error("Échec mise à jour du statut de la commande", "error", err)
	}
}

// handleInventory persiste le snapshot matériel/logiciel envoyé par l'agent
// suite à une commande COLLECT_INVENTORY (le CMD_RESULT qui clôt cette
// commande arrive séparément, voir handleCmdResult).
func (d *Dispatcher) handleInventory(client *Client, env envelope, log *slog.Logger) {
	var body inventoryBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		log.Error("Impossible de décoder INVENTORY body", "error", err)
		return
	}

	ctx := context.Background()

	hw := inventoryDomain.HardwareSnapshot{
		CPUModel:      body.Hardware.CPUModel,
		CPUCores:      body.Hardware.CPUCores,
		CPUThreads:    body.Hardware.CPUThreads,
		RAMTotalBytes: body.Hardware.RAMTotalBytes,
		DiskCount:     body.Hardware.DiskCount,
		BIOSVersion:   body.Hardware.BIOSVersion,
		BIOSVendor:    body.Hardware.BIOSVendor,
		Motherboard:   body.Hardware.Motherboard,
		SerialNumber:  body.Hardware.SerialNumber,
	}
	if err := d.inventoryRepo.SaveHardware(ctx, client.TenantID, client.AgentID, hw); err != nil {
		log.Error("Échec enregistrement inventaire matériel", "error", err)
	}

	items := make([]inventoryDomain.SoftwareSnapshotItem, len(body.Software))
	for i, sw := range body.Software {
		items[i] = inventoryDomain.SoftwareSnapshotItem{
			Name:        sw.Name,
			Version:     sw.Version,
			Publisher:   sw.Publisher,
			InstallPath: sw.InstallPath,
		}
	}
	if err := d.inventoryRepo.ReplaceSoftware(ctx, client.TenantID, client.AgentID, items); err != nil {
		log.Error("Échec enregistrement inventaire logiciel", "error", err)
		return
	}

	log.Info("Inventaire ingéré", "cpu_model", body.Hardware.CPUModel, "software_count", len(items))
}
