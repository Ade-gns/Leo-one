package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// ScheduleHandler gère les planifications ("script_schedules") du tenant
// courant — CRUD + bascule activé/désactivé. Une planification est soit
// récurrente (cron_expression), soit ponctuelle à une date/heure précise
// (run_at) — exactement l'une des deux. Le déclenchement effectif
// (résolution des agents cible, envoi des commandes, calcul de la prochaine
// échéance ou désactivation après une exécution ponctuelle) est fait par la
// boucle de fond dans internal/scheduler, pas ici : ce handler ne fait
// qu'exposer le CRUD à l'interface web.
type ScheduleHandler struct {
	pool  *pgxpool.Pool
	audit *AuditLogger
}

// NewScheduleHandler crée un ScheduleHandler avec ses dépendances.
func NewScheduleHandler(pool *pgxpool.Pool, audit *AuditLogger) *ScheduleHandler {
	return &ScheduleHandler{pool: pool, audit: audit}
}

type scheduleRow struct {
	ID             string     `json:"id"`
	ScriptID       string     `json:"script_id"`
	Name           string     `json:"name"`
	AgentID        *string    `json:"agent_id,omitempty"`
	WorkspaceID    *string    `json:"workspace_id,omitempty"`
	CronExpression *string    `json:"cron_expression,omitempty"`
	RunAt          *time.Time `json:"run_at,omitempty"`
	TimeoutSec     int        `json:"timeout_sec"`
	Enabled        bool       `json:"enabled"`
	NextRunAt      time.Time  `json:"next_run_at"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const scheduleSelectColumns = `
	id, script_id, name, agent_id, workspace_id, cron_expression, run_at,
	timeout_sec, enabled, next_run_at, last_run_at, created_at, updated_at
`

func scanScheduleRow(row interface{ Scan(...any) error }) (scheduleRow, error) {
	var s scheduleRow
	err := row.Scan(
		&s.ID, &s.ScriptID, &s.Name, &s.AgentID, &s.WorkspaceID, &s.CronExpression, &s.RunAt,
		&s.TimeoutSec, &s.Enabled, &s.NextRunAt, &s.LastRunAt, &s.CreatedAt, &s.UpdatedAt,
	)
	return s, err
}

// List retourne toutes les planifications du tenant courant.
//
//	GET /api/v1/script-schedules
func (h *ScheduleHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	rows, err := h.pool.Query(r.Context(), `
		SELECT `+scheduleSelectColumns+`
		FROM script_schedules WHERE tenant_id = $1 ORDER BY name
	`, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	defer rows.Close()

	schedules := make([]scheduleRow, 0)
	for rows.Next() {
		s, err := scanScheduleRow(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
			return
		}
		schedules = append(schedules, s)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
		return
	}

	response.JSON(w, http.StatusOK, schedules)
}

type createScheduleRequest struct {
	ScriptID       string     `json:"script_id"`
	Name           string     `json:"name"`
	AgentID        *string    `json:"agent_id"`
	WorkspaceID    *string    `json:"workspace_id"`
	CronExpression *string    `json:"cron_expression"`
	RunAt          *time.Time `json:"run_at"`
	TimeoutSec     *int       `json:"timeout_sec"`
}

const defaultScheduleTimeoutSec = 60

// validateTarget vérifie qu'exactement une cible (agent_id XOR workspace_id)
// est fournie, non vide.
func validateTarget(agentID, workspaceID *string) bool {
	hasAgent := agentID != nil && *agentID != ""
	hasWorkspace := workspaceID != nil && *workspaceID != ""
	return hasAgent != hasWorkspace
}

// validateSchedule vérifie qu'exactement un mode de planification (cron
// récurrent XOR date/heure ponctuelle) est fourni, non vide.
func validateSchedule(cronExpr *string, runAt *time.Time) bool {
	hasCron := cronExpr != nil && strings.TrimSpace(*cronExpr) != ""
	hasRunAt := runAt != nil
	return hasCron != hasRunAt
}

// Create crée une planification, récurrente (cron_expression) ou ponctuelle
// (run_at, qui doit être dans le futur).
//
//	POST /api/v1/script-schedules
func (h *ScheduleHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())

	var req createScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name est requis")
		return
	}
	if !validateTarget(req.AgentID, req.WorkspaceID) {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"préciser soit agent_id, soit workspace_id (l'un des deux, pas les deux)")
		return
	}
	if !validateSchedule(req.CronExpression, req.RunAt) {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"préciser soit cron_expression (récurrent), soit run_at (une seule fois)")
		return
	}

	var nextRunAt time.Time
	if req.CronExpression != nil {
		schedule, err := cron.ParseStandard(*req.CronExpression)
		if err != nil {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "cron_expression invalide : "+err.Error())
			return
		}
		nextRunAt = schedule.Next(time.Now())
	} else {
		if !req.RunAt.After(time.Now()) {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "run_at doit être dans le futur")
			return
		}
		nextRunAt = *req.RunAt
	}

	timeoutSec := defaultScheduleTimeoutSec
	if req.TimeoutSec != nil && *req.TimeoutSec > 0 {
		timeoutSec = *req.TimeoutSec
	}

	// Vérifie que le script référencé appartient bien au tenant.
	var scriptExists bool
	if err := h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(SELECT 1 FROM scripts WHERE id = $1 AND tenant_id = $2)
	`, req.ScriptID, tenantID).Scan(&scriptExists); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if !scriptExists {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "script introuvable")
		return
	}

	var scheduleID string
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO script_schedules
			(tenant_id, script_id, name, agent_id, workspace_id, cron_expression, run_at, timeout_sec, created_by, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`, tenantID, req.ScriptID, req.Name, req.AgentID, req.WorkspaceID, req.CronExpression, req.RunAt, timeoutSec, userID, nextRunAt).
		Scan(&scheduleID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de la planification")
		return
	}

	h.audit.Record(r.Context(), "schedule.create", "script_schedule", scheduleID, req)
	h.respondSchedule(w, r, http.StatusCreated, scheduleID)
}

func (h *ScheduleHandler) respondSchedule(w http.ResponseWriter, r *http.Request, status int, scheduleID string) {
	row := h.pool.QueryRow(r.Context(), `SELECT `+scheduleSelectColumns+` FROM script_schedules WHERE id = $1`, scheduleID)
	s, err := scanScheduleRow(row)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	response.JSON(w, status, s)
}

type updateScheduleRequest struct {
	Name           *string    `json:"name"`
	AgentID        *string    `json:"agent_id"`
	WorkspaceID    *string    `json:"workspace_id"`
	CronExpression *string    `json:"cron_expression"`
	RunAt          *time.Time `json:"run_at"`
	TimeoutSec     *int       `json:"timeout_sec"`
	Enabled        *bool      `json:"enabled"`
}

// Update modifie une planification existante. Si la cible ou la
// planification (cron ou date/heure) changent, next_run_at est recalculé.
//
//	PATCH /api/v1/script-schedules/:scheduleID
func (h *ScheduleHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	scheduleID := chi.URLParam(r, "scheduleID")

	var req updateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	var name string
	var agentID, workspaceID, cronExpr *string
	var runAt *time.Time
	var timeoutSec int
	var enabled bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT name, agent_id, workspace_id, cron_expression, run_at, timeout_sec, enabled
		FROM script_schedules WHERE id = $1 AND tenant_id = $2
	`, scheduleID, tenantID).Scan(&name, &agentID, &workspaceID, &cronExpr, &runAt, &timeoutSec, &enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "planification introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	targetChanged := false
	if req.AgentID != nil || req.WorkspaceID != nil {
		if req.AgentID != nil {
			agentID = req.AgentID
			workspaceID = nil
		}
		if req.WorkspaceID != nil {
			workspaceID = req.WorkspaceID
			agentID = nil
		}
		if !validateTarget(agentID, workspaceID) {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"préciser soit agent_id, soit workspace_id (l'un des deux, pas les deux)")
			return
		}
		targetChanged = true
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name ne peut pas être vide")
			return
		}
		name = trimmed
	}
	if req.TimeoutSec != nil && *req.TimeoutSec > 0 {
		timeoutSec = *req.TimeoutSec
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	scheduleChanged := false
	if req.CronExpression != nil && req.RunAt != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"préciser soit cron_expression, soit run_at (pas les deux dans la même requête)")
		return
	}
	if req.CronExpression != nil {
		cronExpr = req.CronExpression
		runAt = nil
		scheduleChanged = true
	}
	if req.RunAt != nil {
		runAt = req.RunAt
		cronExpr = nil
		scheduleChanged = true
	}

	nextRunAt := time.Time{}
	if scheduleChanged || targetChanged {
		if !validateSchedule(cronExpr, runAt) {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"préciser soit cron_expression, soit run_at (l'un des deux, pas les deux)")
			return
		}
		if cronExpr != nil {
			schedule, err := cron.ParseStandard(*cronExpr)
			if err != nil {
				response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "cron_expression invalide : "+err.Error())
				return
			}
			nextRunAt = schedule.Next(time.Now())
		} else {
			if !runAt.After(time.Now()) {
				response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "run_at doit être dans le futur")
				return
			}
			nextRunAt = *runAt
		}
	}

	if nextRunAt.IsZero() {
		_, err = h.pool.Exec(r.Context(), `
			UPDATE script_schedules
			SET name = $1, agent_id = $2, workspace_id = $3, cron_expression = $4, run_at = $5, timeout_sec = $6, enabled = $7
			WHERE id = $8 AND tenant_id = $9
		`, name, agentID, workspaceID, cronExpr, runAt, timeoutSec, enabled, scheduleID, tenantID)
	} else {
		_, err = h.pool.Exec(r.Context(), `
			UPDATE script_schedules
			SET name = $1, agent_id = $2, workspace_id = $3, cron_expression = $4, run_at = $5, timeout_sec = $6, enabled = $7, next_run_at = $8
			WHERE id = $9 AND tenant_id = $10
		`, name, agentID, workspaceID, cronExpr, runAt, timeoutSec, enabled, nextRunAt, scheduleID, tenantID)
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	h.audit.Record(r.Context(), "schedule.update", "script_schedule", scheduleID, req)
	h.respondSchedule(w, r, http.StatusOK, scheduleID)
}

// Delete supprime une planification.
//
//	DELETE /api/v1/script-schedules/:scheduleID
func (h *ScheduleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	scheduleID := chi.URLParam(r, "scheduleID")

	tag, err := h.pool.Exec(r.Context(), `
		DELETE FROM script_schedules WHERE id = $1 AND tenant_id = $2
	`, scheduleID, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}
	if tag.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "planification introuvable")
		return
	}

	h.audit.Record(r.Context(), "schedule.delete", "script_schedule", scheduleID, nil)
	w.WriteHeader(http.StatusNoContent)
}
