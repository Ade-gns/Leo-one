package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
)

// AgentHandler gère les requêtes HTTP pour les agents et les commandes.
type AgentHandler struct {
	agentRepo agentDomain.Repository
	pool      *pgxpool.Pool
	hub       *leoWS.Hub
}

// NewAgentHandler crée un AgentHandler avec ses dépendances.
func NewAgentHandler(agentRepo agentDomain.Repository, pool *pgxpool.Pool, hub *leoWS.Hub) *AgentHandler {
	return &AgentHandler{
		agentRepo: agentRepo,
		pool:      pool,
		hub:       hub,
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
	if errors.Is(err, pgx.ErrNoRows) || agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
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
	if errors.Is(err, pgx.ErrNoRows) || agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
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

	w.WriteHeader(http.StatusNoContent)
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

	response.JSON(w, http.StatusCreated, map[string]any{
		"agent_id":    agentID,
		"tenant_id":   tok.TenantID,
		"ws_endpoint": "wss://rmm.example.com/ws/agent",
	})
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
	if errors.Is(err, pgx.ErrNoRows) || agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	var req createCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	commandID := uuid.New().String()
	payload := req.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	// Insérer la commande en BDD
	_, err = h.pool.Exec(r.Context(), `
		INSERT INTO commands (id, tenant_id, agent_id, created_by, type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', NOW())
	`, commandID, tenantID, agentID, userID, req.Type, payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de la commande")
		return
	}

	// Envoyer à l'agent si connecté
	agentOnline := h.hub.IsConnected(agentID)
	if agentOnline {
		cmdMsg := map[string]any{
			"v":    1,
			"type": 200, // LEO_MSG_COMMAND
			"id":   commandID,
			"ts":   time.Now().UnixMilli(),
			"body": map[string]any{
				"command_id": commandID,
				"type":       req.Type,
				"payload":    req.Payload,
			},
		}
		h.hub.SendToAgent(agentID, cmdMsg)

		// Marquer comme envoyé
		_, _ = h.pool.Exec(r.Context(), `
			UPDATE commands SET sent_at = NOW() WHERE id = $1
		`, commandID)
	}

	response.JSON(w, http.StatusAccepted, map[string]any{
		"command_id": commandID,
		"status":     "pending",
		"sent":       agentOnline,
	})
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
		ID          string          `json:"id"`
		AgentID     string          `json:"agent_id"`
		Type        string          `json:"type"`
		Payload     json.RawMessage `json:"payload"`
		Status      string          `json:"status"`
		Stdout      *string         `json:"stdout,omitempty"`
		Stderr      *string         `json:"stderr,omitempty"`
		ExitCode    *int            `json:"exit_code,omitempty"`
		CreatedAt   time.Time       `json:"created_at"`
		SentAt      *time.Time      `json:"sent_at,omitempty"`
		CompletedAt *time.Time      `json:"completed_at,omitempty"`
	}

	var cmd commandDetail
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, agent_id, type, payload, status, stdout, stderr, exit_code,
		       created_at, sent_at, completed_at
		FROM commands
		WHERE id = $1 AND tenant_id = $2 AND agent_id = $3
	`, commandID, tenantID, agentID).Scan(
		&cmd.ID, &cmd.AgentID, &cmd.Type, &cmd.Payload, &cmd.Status,
		&cmd.Stdout, &cmd.Stderr, &cmd.ExitCode,
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
