package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	leoWS "github.com/yourorg/leo-one/internal/infrastructure/websocket"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
	"github.com/yourorg/leo-one/pkg/pki"
)

// AgentHandler gère les requêtes HTTP pour les agents et les commandes.
type AgentHandler struct {
	agentRepo         agentDomain.Repository
	pool              *pgxpool.Pool
	hub               *leoWS.Hub
	ca                *pki.CA
	serverFingerprint string // empreinte SHA-256 du certificat du listener WSS, à épingler côté agent
	wsEndpoint        string // wss://<host>:<port>/ws/agent, renvoyé aux agents à l'enrollment
	audit             *AuditLogger
}

// NewAgentHandler crée un AgentHandler avec ses dépendances.
func NewAgentHandler(
	agentRepo agentDomain.Repository,
	pool *pgxpool.Pool,
	hub *leoWS.Hub,
	ca *pki.CA,
	serverFingerprint string,
	wsEndpoint string,
	audit *AuditLogger,
) *AgentHandler {
	return &AgentHandler{
		agentRepo:         agentRepo,
		pool:              pool,
		hub:               hub,
		ca:                ca,
		serverFingerprint: serverFingerprint,
		wsEndpoint:        wsEndpoint,
		audit:             audit,
	}
}

// ─── Modèles de requête/réponse ───────────────────────────────────────────────

type enrollRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	PublicKey       string `json:"public_key,omitempty"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	OSVersion       string `json:"os_version"`
	Arch            string `json:"arch"`
	HardwareID      string `json:"hardware_id"`
	AgentVersion    string `json:"agent_version"`
	FQDN            string `json:"fqdn,omitempty"`
}

type agentUpdateRequest struct {
	Hostname    *string `json:"hostname,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
}

type createCommandRequest struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// List retourne la liste paginée des agents du tenant.
//
//	GET /api/v1/agents
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	if tenantID == "" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant_id manquant dans le contexte")
		return
	}

	q := r.URL.Query()
	filter := agentDomain.ListFilter{
		Cursor: q.Get("cursor"),
		Limit:  50,
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			filter.Limit = n
		}
	}

	if statusStr := q.Get("status"); statusStr != "" {
		s := agentDomain.Status(statusStr)
		filter.Status = &s
	}

	if wsID := q.Get("workspace_id"); wsID != "" {
		filter.WorkspaceID = &wsID
	}

	agents, nextCursor, err := h.agentRepo.List(r.Context(), tenantID, filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération des agents")
		return
	}

	total, _ := h.agentRepo.CountByTenant(r.Context(), tenantID)

	response.JSONWithMeta(w, http.StatusOK, agents, map[string]any{
		"cursor": nextCursor,
		"total":  total,
	})
}

// Get retourne un agent par son ID.
//
//	GET /api/v1/agents/:agentID
func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	response.JSON(w, http.StatusOK, agent)
}

// Update met à jour partiellement un agent.
//
//	PATCH /api/v1/agents/:agentID
func (h *AgentHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	var req agentUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	if req.Hostname != nil {
		agent.Hostname = *req.Hostname
	}
	if req.WorkspaceID != nil {
		agent.WorkspaceID = req.WorkspaceID
	}
	agent.UpdatedAt = time.Now().UTC()

	if err := h.agentRepo.Update(r.Context(), agent); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	h.audit.Record(r.Context(), "agent.update", "agent", agentID, req)
	response.JSON(w, http.StatusOK, agent)
}

// Delete supprime un agent.
//
//	DELETE /api/v1/agents/:agentID
func (h *AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	if err := h.agentRepo.Delete(r.Context(), tenantID, agentID); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}

	h.audit.Record(r.Context(), "agent.delete", "agent", agentID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// RevokeCertificate révoque le(s) certificat(s) mTLS actif(s) d'un agent
// sans supprimer l'agent — contrairement à Delete (qui révoque via cascade
// FK en supprimant la ligne agent_certificates), ceci garde l'agent et son
// historique intacts. Utile pour une rotation de certificat ou après
// compromission : l'agent devra être ré-enrôlé (nouveau token) pour obtenir
// un nouveau certificat et se reconnecter.
//
// Coupe aussi immédiatement toute connexion WSS en cours (h.hub.Disconnect) :
// AgentWSHandler.checkNotRevoked ne s'exécute qu'au handshake, donc sans ça
// un agent déjà connecté resterait actif jusqu'à sa prochaine reconnexion.
//
//	DELETE /api/v1/agents/:agentID/certificate
func (h *AgentHandler) RevokeCertificate(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	// Vérifie que l'agent appartient au tenant courant avant de toucher à ses
	// certificats — agent_certificates n'a pas de colonne tenant_id, la
	// requête de révocation ci-dessous est donc indexée sur agent_id seul.
	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	tag, err := h.pool.Exec(r.Context(), `
		UPDATE agent_certificates SET revoked_at = NOW(), revoke_reason = $1
		WHERE agent_id = $2 AND revoked_at IS NULL
	`, "révoqué manuellement via l'API", agentID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la révocation")
		return
	}

	h.hub.Disconnect(agentID)

	h.audit.Record(r.Context(), "agent.certificate.revoke", "agent", agentID, map[string]any{"revoked_count": tag.RowsAffected()})
	response.JSON(w, http.StatusOK, map[string]any{
		"revoked_count": tag.RowsAffected(),
	})
}

// Enroll inscrit un nouvel agent via un token d'enrollment.
//
//	POST /api/v1/enroll
func (h *AgentHandler) Enroll(w http.ResponseWriter, r *http.Request) {
	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	if req.EnrollmentToken == "" || req.Hostname == "" || req.HardwareID == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "champs obligatoires manquants")
		return
	}

	// Hachage du token reçu pour chercher en BDD
	h256 := sha256.Sum256([]byte(req.EnrollmentToken))
	tokenHash := hex.EncodeToString(h256[:])

	// Lookup du token d'enrollment
	type tokenRow struct {
		ID          string
		TenantID    string
		WorkspaceID *string
		UsedAt      *time.Time
		ExpiresAt   time.Time
	}
	var tok tokenRow
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, tenant_id, workspace_id, used_at, expires_at
		FROM enrollment_tokens
		WHERE token_hash = $1
	`, tokenHash).Scan(&tok.ID, &tok.TenantID, &tok.WorkspaceID, &tok.UsedAt, &tok.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "token d'enrollment invalide")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	if tok.UsedAt != nil {
		response.Error(w, http.StatusUnauthorized, "ENROLLMENT_TOKEN_USED", "token déjà utilisé")
		return
	}

	if time.Now().After(tok.ExpiresAt) {
		response.Error(w, http.StatusUnauthorized, "ENROLLMENT_TOKEN_EXPIRED", "token expiré")
		return
	}

	// Vérifier que l'agent n'existe pas déjà (hardware_id unique par tenant)
	existing, _ := h.agentRepo.FindByHardwareID(r.Context(), tok.TenantID, req.HardwareID)
	if existing != nil {
		response.Error(w, http.StatusConflict, "AGENT_ALREADY_EXISTS", "un agent avec ce hardware_id existe déjà")
		return
	}

	// Vérifier le quota d'agents
	count, _ := h.agentRepo.CountByTenant(r.Context(), tok.TenantID)
	var maxAgents int
	_ = h.pool.QueryRow(r.Context(), `SELECT max_agents FROM tenants WHERE id = $1`, tok.TenantID).Scan(&maxAgents)
	if maxAgents > 0 && count >= maxAgents {
		response.Error(w, http.StatusForbidden, "TENANT_LIMIT_REACHED", "quota d'agents atteint")
		return
	}

	// Créer l'agent
	now := time.Now().UTC()
	agentID := uuid.New().String()
	var fqdn *string
	if req.FQDN != "" {
		fqdn = &req.FQDN
	}

	newAgent := &agentDomain.Agent{
		ID:           agentID,
		TenantID:     tok.TenantID,
		WorkspaceID:  tok.WorkspaceID,
		Hostname:     req.Hostname,
		FQDN:         fqdn,
		OS:           agentDomain.OS(req.OS),
		OSVersion:    req.OSVersion,
		Arch:         req.Arch,
		HardwareID:   req.HardwareID,
		AgentVersion: req.AgentVersion,
		Status:       agentDomain.StatusOffline,
		EnrolledAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.agentRepo.Create(r.Context(), newAgent); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de l'agent")
		return
	}

	// Marquer le token comme utilisé
	_, _ = h.pool.Exec(r.Context(), `
		UPDATE enrollment_tokens SET used_at = NOW(), used_by = $1 WHERE id = $2
	`, agentID, tok.ID)

	// Certificat client mTLS de l'agent : CN=agentID, OU=tenantID, vérifiés
	// par AgentWSHandler.parseCert à chaque connexion WSS. Sans ce certificat
	// l'agent ne peut pas se connecter (client_ssl_cert_filepath est requis
	// côté agent — voir agent/src/connection.c).
	clientCertPEM, clientKeyPEM, clientCert, err := pki.IssueAgentCert(h.ca, agentID, tok.TenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'émission du certificat agent")
		return
	}

	// Traçabilité/révocation : voir AgentWSHandler.checkNotRevoked, qui
	// n'autorise la connexion WSS que pour un thumbprint présent ici et non
	// révoqué.
	_, err = h.pool.Exec(r.Context(), `
		INSERT INTO agent_certificates (agent_id, serial_number, thumbprint, expires_at)
		VALUES ($1, $2, $3, $4)
	`, agentID, clientCert.SerialNumber.String(), pki.Fingerprint(clientCert), clientCert.NotAfter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'enregistrement du certificat agent")
		return
	}

	response.JSON(w, http.StatusCreated, map[string]any{
		"agent_id":                agentID,
		"tenant_id":               tok.TenantID,
		"ws_endpoint":             h.wsEndpoint,
		"client_cert_pem":         string(clientCertPEM),
		"client_key_pem":          string(clientKeyPEM),
		"server_cert_fingerprint": h.serverFingerprint,
	})
}

// commandWSType mappe le command_type SQL (chaîne) vers le type numérique du
// protocole agent (voir leo_agent.h — LEO_MSG_*). L'agent C dispatche sur des
// types distincts au niveau de l'enveloppe (pas de wrapper générique "COMMAND").
var commandWSType = map[string]int{
	"exec_script":       101, // LEO_MSG_EXEC_SCRIPT
	"install_pkg":       102, // LEO_MSG_INSTALL_PKG
	"reboot":            103, // LEO_MSG_REBOOT
	"collect_inventory": 104, // LEO_MSG_COLLECT_INVENTORY
	"ping":              105, // LEO_MSG_PING
	"install_patches":   108, // LEO_MSG_INSTALL_PATCHES
	"file_transfer":     109, // LEO_MSG_FILE_TRANSFER
}

// CreateAndDispatchCommand insère une commande en BDD et l'envoie
// immédiatement si l'agent est connecté. Partagé par CreateCommand (un seul
// agent), BulkCreateCommand (plusieurs agents ou un workspace entier), et le
// scheduler de scripts programmés (internal/scheduler — d'où l'export).
// N'effectue PAS la vérification d'appartenance au tenant : à la charge de
// l'appelant (CreateCommand la fait via agentRepo.FindByID ; BulkCreateCommand
// et le scheduler la font en amont via la requête SQL qui résout les IDs
// cibles, toujours filtrée par tenant_id).
// userID/scheduleID sont nullable : userID est nil pour une commande
// déclenchée par le scheduler (pas d'utilisateur à l'origine) ; scheduleID
// est nil pour toute commande ad-hoc (création manuelle ou envoi groupé).
func (h *AgentHandler) CreateAndDispatchCommand(
	ctx context.Context, tenantID, agentID string, userID, scheduleID *string, cmdType string, payload json.RawMessage,
) (commandID string, sent bool, err error) {
	wsType, ok := commandWSType[cmdType]
	if !ok {
		return "", false, fmt.Errorf("type de commande inconnu : %s", cmdType)
	}

	commandID = uuid.New().String()
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	if _, err = h.pool.Exec(ctx, `
		INSERT INTO commands (id, tenant_id, agent_id, created_by, schedule_id, type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', NOW())
	`, commandID, tenantID, agentID, userID, scheduleID, cmdType, payload); err != nil {
		return "", false, err
	}

	sent = h.hub.IsConnected(agentID)
	if sent {
		// body = payload tel quel : l'agent C attend les champs de la commande
		// (ex: interpreter/script/timeout_sec pour exec_script) directement à
		// la racine de "body", pas sous un wrapper command_id/type/payload —
		// command_id est déjà porté par le champ "id" de l'enveloppe.
		cmdMsg := map[string]any{
			"v":    1,
			"type": wsType,
			"id":   commandID,
			"ts":   time.Now().UnixMilli(),
			"body": payload,
		}
		h.hub.SendToAgent(agentID, cmdMsg)

		// Marquer comme envoyé
		_, _ = h.pool.Exec(ctx, `
			UPDATE commands SET sent_at = NOW() WHERE id = $1
		`, commandID)
	}

	return commandID, sent, nil
}

// CreateCommand crée une commande pour un agent et l'envoie si l'agent est connecté.
//
//	POST /api/v1/agents/:agentID/commands
func (h *AgentHandler) CreateCommand(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	// Vérifier que l'agent appartient au tenant
	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	var req createCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	if _, ok := commandWSType[req.Type]; !ok {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "type de commande inconnu : "+req.Type)
		return
	}

	commandID, sent, err := h.CreateAndDispatchCommand(r.Context(), tenantID, agentID, &userID, nil, req.Type, req.Payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de la commande")
		return
	}

	h.audit.Record(r.Context(), "agent.command.create", "command", commandID, map[string]any{"agent_id": agentID, "type": req.Type})
	response.JSON(w, http.StatusAccepted, map[string]any{
		"command_id": commandID,
		"status":     "pending",
		"sent":       sent,
	})
}

// scanAgentIDs consomme un pgx.Rows d'une seule colonne "id" et le referme.
func scanAgentIDs(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type bulkCreateCommandRequest struct {
	AgentIDs    []string        `json:"agent_ids"`
	WorkspaceID *string         `json:"workspace_id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
}

type bulkCommandResult struct {
	AgentID   string `json:"agent_id"`
	CommandID string `json:"command_id,omitempty"`
	Sent      bool   `json:"sent"`
	Error     string `json:"error,omitempty"`
}

// BulkCreateCommand crée et envoie une même commande vers plusieurs agents à
// la fois — soit une liste explicite (agent_ids), soit tous les agents d'un
// workspace (workspace_id). Réutilise createAndDispatchCommand par agent
// cible ; un échec sur un agent n'empêche pas les autres d'être traités.
//
//	POST /api/v1/agents/bulk-commands
func (h *AgentHandler) BulkCreateCommand(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())

	var req bulkCreateCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	if _, ok := commandWSType[req.Type]; !ok {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "type de commande inconnu : "+req.Type)
		return
	}

	hasAgentIDs := len(req.AgentIDs) > 0
	hasWorkspace := req.WorkspaceID != nil && *req.WorkspaceID != ""
	if hasAgentIDs == hasWorkspace {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"préciser soit agent_ids, soit workspace_id (l'un des deux, pas les deux)")
		return
	}

	var (
		targetIDs []string
		err       error
	)
	if hasAgentIDs {
		// Ne jamais faire confiance à une liste d'IDs fournie par le client :
		// filtrée pour ne garder que les agents appartenant réellement au tenant.
		var rows pgx.Rows
		rows, err = h.pool.Query(r.Context(), `
			SELECT id FROM agents WHERE tenant_id = $1 AND id = ANY($2)
		`, tenantID, req.AgentIDs)
		if err == nil {
			targetIDs, err = scanAgentIDs(rows)
		}
	} else {
		var rows pgx.Rows
		rows, err = h.pool.Query(r.Context(), `
			SELECT id FROM agents WHERE tenant_id = $1 AND workspace_id = $2
		`, tenantID, *req.WorkspaceID)
		if err == nil {
			targetIDs, err = scanAgentIDs(rows)
		}
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if len(targetIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "aucun agent cible trouvé")
		return
	}

	results := make([]bulkCommandResult, 0, len(targetIDs))
	for _, agentID := range targetIDs {
		commandID, sent, err := h.CreateAndDispatchCommand(r.Context(), tenantID, agentID, &userID, nil, req.Type, req.Payload)
		if err != nil {
			results = append(results, bulkCommandResult{AgentID: agentID, Error: err.Error()})
			continue
		}
		results = append(results, bulkCommandResult{AgentID: agentID, CommandID: commandID, Sent: sent})
	}

	h.audit.Record(r.Context(), "agent.command.bulk_create", "command", "",
		map[string]any{"type": req.Type, "target_count": len(targetIDs), "results": results})
	response.JSON(w, http.StatusAccepted, results)
}

// ListCommands liste les commandes d'un agent.
//
//	GET /api/v1/agents/:agentID/commands
func (h *AgentHandler) ListCommands(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	q := r.URL.Query()
	limit := 50
	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT id, type, status, created_at, sent_at, completed_at
		FROM commands
		WHERE tenant_id = $1 AND agent_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`, tenantID, agentID, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	defer rows.Close()

	type commandSummary struct {
		ID          string     `json:"id"`
		Type        string     `json:"type"`
		Status      string     `json:"status"`
		CreatedAt   time.Time  `json:"created_at"`
		SentAt      *time.Time `json:"sent_at,omitempty"`
		CompletedAt *time.Time `json:"completed_at,omitempty"`
	}

	commands := make([]commandSummary, 0)
	for rows.Next() {
		var cmd commandSummary
		if err := rows.Scan(&cmd.ID, &cmd.Type, &cmd.Status, &cmd.CreatedAt, &cmd.SentAt, &cmd.CompletedAt); err != nil {
			continue
		}
		commands = append(commands, cmd)
	}

	response.JSON(w, http.StatusOK, commands)
}

// GetCommand retourne une commande par son ID.
//
//	GET /api/v1/agents/:agentID/commands/:commandID
func (h *AgentHandler) GetCommand(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")
	commandID := chi.URLParam(r, "commandID")

	type commandDetail struct {
		ID              string          `json:"id"`
		AgentID         string          `json:"agent_id"`
		Type            string          `json:"type"`
		Payload         json.RawMessage `json:"payload"`
		Status          string          `json:"status"`
		Stdout          *string         `json:"stdout,omitempty"`
		Stderr          *string         `json:"stderr,omitempty"`
		ExitCode        *int            `json:"exit_code,omitempty"`
		ProgressPercent *int            `json:"progress_percent,omitempty"`
		CreatedAt       time.Time       `json:"created_at"`
		SentAt          *time.Time      `json:"sent_at,omitempty"`
		CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	}

	var cmd commandDetail
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, agent_id, type, payload, status, stdout, stderr, exit_code, progress_percent,
		       created_at, sent_at, completed_at
		FROM commands
		WHERE id = $1 AND tenant_id = $2 AND agent_id = $3
	`, commandID, tenantID, agentID).Scan(
		&cmd.ID, &cmd.AgentID, &cmd.Type, &cmd.Payload, &cmd.Status,
		&cmd.Stdout, &cmd.Stderr, &cmd.ExitCode, &cmd.ProgressPercent,
		&cmd.CreatedAt, &cmd.SentAt, &cmd.CompletedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "commande introuvable")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	response.JSON(w, http.StatusOK, cmd)
}

// WakeUp envoie un message FORCE_HEARTBEAT à l'agent pour le réveiller immédiatement.
//
//	POST /api/v1/agents/:agentID/wake-up
func (h *AgentHandler) WakeUp(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	// Vérifier que l'agent appartient au tenant
	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	// Envoyer le message FORCE_HEARTBEAT (type 107) si l'agent est connecté
	// FORCE_HEARTBEAT body est vide
	wakeupMsg := map[string]any{
		"v":    1,
		"type": 107, // LEO_MSG_FORCE_HEARTBEAT
		"id":   uuid.New().String(),
		"ts":   time.Now().UnixMilli(),
		"body": map[string]any{},
	}

	agentOnline := h.hub.IsConnected(agentID)
	if agentOnline {
		h.hub.SendToAgent(agentID, wakeupMsg)
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"status":  "sent",
		"message": "Wake-up signal envoyé à l'agent",
		"online":  agentOnline,
	})
}
