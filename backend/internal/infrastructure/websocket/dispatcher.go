package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	inventoryDomain "github.com/yourorg/leo-one/internal/domain/inventory"
	metricDomain "github.com/yourorg/leo-one/internal/domain/metric"
	patchDomain "github.com/yourorg/leo-one/internal/domain/patch"
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
	msgTypeHello                = 1
	msgTypeHeartbeat            = 2
	msgTypeMetrics              = 3
	msgTypeInventory            = 4
	msgTypeCmdResult            = 5
	msgTypePong                 = 7
	msgTypePatchInventory       = 8
	msgTypeFileTransferProgress = 9
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

// patchInventoryBody est le body du message PATCH_INVENTORY — voir
// agent/src/protocol.c leo_proto_build_patch_inventory() côté agent.
type patchInventoryBody struct {
	Patches []patchItemBody `json:"patches"`
}

type patchItemBody struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	SizeBytes uint64 `json:"size_bytes"`
}

// fileTransferProgressBody est le body du message FILE_TRANSFER_PROGRESS —
// voir agent/src/protocol.c leo_proto_build_file_transfer_progress() côté agent.
type fileTransferProgressBody struct {
	CommandID     string `json:"command_id"`
	Status        string `json:"status"`
	Percent       int    `json:"percent"`
	BytesReceived uint64 `json:"bytes_received"`
	BytesTotal    uint64 `json:"bytes_total"`
	Error         string `json:"error"`
}

// Dispatcher route les messages entrants des agents vers les use cases appropriés.
type Dispatcher struct {
	agentRepo     agentDomain.Repository
	metricRepo    metricDomain.Repository
	inventoryRepo inventoryDomain.Repository
	patchRepo     patchDomain.Repository
	pool          *pgxpool.Pool // accès direct pour la table commands (pas de repo dédié)
	hub           *Hub          // référence arrière pour envoyer HELLO_ACK, etc.
	logger        *slog.Logger
}

// NewDispatcher crée un Dispatcher avec ses dépendances injectées.
func NewDispatcher(
	agentRepo agentDomain.Repository,
	metricRepo metricDomain.Repository,
	inventoryRepo inventoryDomain.Repository,
	patchRepo patchDomain.Repository,
	pool *pgxpool.Pool,
	logger *slog.Logger,
) *Dispatcher {
	return &Dispatcher{
		agentRepo:     agentRepo,
		metricRepo:    metricRepo,
		inventoryRepo: inventoryRepo,
		patchRepo:     patchRepo,
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
	case msgTypePatchInventory:
		d.handlePatchInventory(client, env, log)
	case msgTypeFileTransferProgress:
		d.handleFileTransferProgress(client, env, log)
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

	d.redeliverPendingCommands(client, log)
}

// pendingCommandWSType mappe le command_type SQL vers le type numérique du
// protocole agent (voir leo_agent.h — LEO_MSG_*). Même mapping que
// commandWSType dans interfaces/http/handlers/agent_handler.go — dupliqué
// ici plutôt que partagé via un import : package différent, 5 entrées
// statiques, pas assez pour justifier un couplage inter-packages.
var pendingCommandWSType = map[string]int{
	"exec_script":       101, // LEO_MSG_EXEC_SCRIPT
	"install_pkg":       102, // LEO_MSG_INSTALL_PKG
	"reboot":            103, // LEO_MSG_REBOOT
	"collect_inventory": 104, // LEO_MSG_COLLECT_INVENTORY
	"ping":              105, // LEO_MSG_PING
	"install_patches":   108, // LEO_MSG_INSTALL_PATCHES
	"file_transfer":     109, // LEO_MSG_FILE_TRANSFER
}

// redeliverPendingCommands renvoie à l'agent, à sa (re)connexion, les
// commandes créées pendant qu'il était hors ligne (jamais transmises —
// sent_at NULL). Sans ceci, une commande créée alors que le poste est
// éteint/en veille (typiquement une exécution planifiée) ne serait jamais
// délivrée : CreateCommand ne l'envoie que si l'agent est connecté au
// moment de la création, et rien d'autre ne retente l'envoi par la suite.
func (d *Dispatcher) redeliverPendingCommands(client *Client, log *slog.Logger) {
	rows, err := d.pool.Query(context.Background(), `
		SELECT id, type, payload FROM commands
		WHERE agent_id = $1 AND status = 'pending' AND sent_at IS NULL
		ORDER BY created_at
	`, client.AgentID)
	if err != nil {
		log.Error("Échec lecture des commandes en attente", "error", err)
		return
	}

	type pendingCommand struct {
		id      string
		cmdType string
		payload json.RawMessage
	}
	var toSend []pendingCommand
	for rows.Next() {
		var p pendingCommand
		if err := rows.Scan(&p.id, &p.cmdType, &p.payload); err != nil {
			log.Error("Échec lecture d'une commande en attente", "error", err)
			continue
		}
		toSend = append(toSend, p)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		log.Error("Échec lecture des commandes en attente", "error", rowsErr)
		return
	}

	for _, p := range toSend {
		wsType, ok := pendingCommandWSType[p.cmdType]
		if !ok {
			log.Warn("Commande en attente de type inconnu — ignorée", "command_id", p.id, "type", p.cmdType)
			continue
		}

		client.Send(map[string]any{
			"v":    1,
			"type": wsType,
			"id":   p.id,
			"ts":   time.Now().UnixMilli(),
			"body": p.payload,
		})

		if _, err := d.pool.Exec(context.Background(), `
			UPDATE commands SET sent_at = NOW() WHERE id = $1
		`, p.id); err != nil {
			log.Error("Échec marquage sent_at sur redélivrance", "command_id", p.id, "error", err)
		}
	}

	if len(toSend) > 0 {
		log.Info("Commandes en attente redélivrées", "count", len(toSend))
	}
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

	ctx := context.Background()
	var cmdType string
	var payload json.RawMessage
	err := d.pool.QueryRow(ctx, `
		UPDATE commands
		SET status = $1::command_status, exit_code = $2, stdout = $3, stderr = $4,
		    completed_at = NOW()
		WHERE id = $5 AND tenant_id = $6
		RETURNING type::text, payload
	`, status, body.ExitCode, body.Stdout, body.Stderr, body.CommandID, client.TenantID).Scan(&cmdType, &payload)
	if err != nil {
		log.Error("Échec mise à jour du statut de la commande", "error", err)
		return
	}

	// Une commande install_patches réussie/échouée fait transitionner les
	// patchs ciblés vers installed/failed sans attendre la prochaine collecte
	// périodique (voir handlePatchInventory) — patch_ids vient du payload
	// stocké à la création de la commande (voir PatchHandler.Install/BulkInstall).
	if cmdType == "install_patches" {
		var p struct {
			PatchIDs []string `json:"patch_ids"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			log.Error("Impossible de décoder le payload install_patches", "error", err)
			return
		}
		if err := d.patchRepo.MarkInstallResult(ctx, client.TenantID, client.AgentID, p.PatchIDs, body.ExitCode == 0); err != nil {
			log.Error("Échec mise à jour du statut des patchs", "error", err)
		}
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

// handlePatchInventory persiste la liste des patchs disponibles remontée
// périodiquement par l'agent (voir agent/src/agent.c _patch_thread). Upsert
// uniquement : un patch qui n'est plus rapporté n'est jamais supprimé ou
// changé de statut ici (voir patch.Repository.Upsert et le commentaire sur
// migrations/006_patches.sql) — seul un CMD_RESULT d'install_patches (voir
// handleCmdResult) fait transitionner un patch vers installed/failed.
func (d *Dispatcher) handlePatchInventory(client *Client, env envelope, log *slog.Logger) {
	var body patchInventoryBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		log.Error("Impossible de décoder PATCH_INVENTORY body", "error", err)
		return
	}

	reports := make([]patchDomain.Report, 0, len(body.Patches))
	for _, p := range body.Patches {
		if p.ID == "" {
			continue
		}
		reports = append(reports, patchDomain.Report{
			NativeID:  p.ID,
			Title:     p.Title,
			Severity:  normalizePatchSeverity(p.Severity),
			SizeBytes: p.SizeBytes,
		})
	}

	if err := d.patchRepo.Upsert(context.Background(), client.TenantID, client.AgentID, reports); err != nil {
		log.Error("Échec enregistrement de l'inventaire des patchs", "error", err)
		return
	}

	log.Info("Inventaire des patchs ingéré", "count", len(reports))
}

// handleFileTransferProgress met à jour commands.progress_percent au fil du
// téléchargement d'un fichier par l'agent (voir FILE_TRANSFER dans
// agent/src/file_transfer.c) — consulté par le frontend via un polling de
// GET /api/v1/agents/:id/commands/:commandID pendant un déploiement (voir
// AgentHandler.GetCommand). Le CMD_RESULT final (succès/échec) arrive
// séparément, via handleCmdResult.
func (d *Dispatcher) handleFileTransferProgress(client *Client, env envelope, log *slog.Logger) {
	var body fileTransferProgressBody
	if err := json.Unmarshal(env.Body, &body); err != nil {
		log.Error("Impossible de décoder FILE_TRANSFER_PROGRESS body", "error", err)
		return
	}
	if body.CommandID == "" {
		return
	}

	_, err := d.pool.Exec(context.Background(), `
		UPDATE commands SET progress_percent = $1
		WHERE id = $2 AND tenant_id = $3
	`, body.Percent, body.CommandID, client.TenantID)
	if err != nil {
		log.Error("Échec mise à jour de l'avancement du transfert", "error", err)
		return
	}

	log.Debug("Avancement du transfert de fichier", "command_id", body.CommandID,
		"status", body.Status, "percent", body.Percent)
}

// normalizePatchSeverity ramène toute valeur inattendue à "important" —
// l'ENUM Postgres patch_severity rejetterait une valeur hors énumération
// (INSERT en échec), ce qui ferait échouer tout le batch Upsert pour un
// simple champ malformé d'une seule entrée envoyée par un agent bogué/futur.
func normalizePatchSeverity(s string) patchDomain.Severity {
	switch patchDomain.Severity(s) {
	case patchDomain.SeverityOptional, patchDomain.SeverityImportant, patchDomain.SeverityCritical:
		return patchDomain.Severity(s)
	default:
		return patchDomain.SeverityImportant
	}
}
