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

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// ScriptHandler gère la bibliothèque de scripts réutilisables du tenant
// courant (CRUD complet — pas de notion de script "système").
type ScriptHandler struct {
	pool  *pgxpool.Pool
	audit *AuditLogger
}

// NewScriptHandler crée un ScriptHandler avec ses dépendances.
func NewScriptHandler(pool *pgxpool.Pool, audit *AuditLogger) *ScriptHandler {
	return &ScriptHandler{pool: pool, audit: audit}
}

// scriptAllowedInterpreters — mêmes valeurs que la whitelist de l'agent C
// (leo_exec_script) sur les deux plateformes supportées : bash/sh/python*
// sur Linux, cmd/powershell sur Windows. Un script de la bibliothèque
// n'étant pas lié à une plateforme au moment de sa création (il peut être
// envoyé à des agents Linux ou Windows selon la cible choisie plus tard),
// on accepte l'union des deux whitelists ici plutôt que de choisir.
var scriptAllowedInterpreters = map[string]bool{
	"bash": true, "sh": true, "python": true, "cmd": true, "powershell": true,
}

type scriptRow struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Interpreter string    `json:"interpreter"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

const scriptSelectColumns = `id, name, description, interpreter, content, created_at, updated_at`

func scanScriptRow(row interface{ Scan(...any) error }) (scriptRow, error) {
	var s scriptRow
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.Interpreter, &s.Content, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

// List retourne tous les scripts de la bibliothèque du tenant courant.
//
//	GET /api/v1/scripts
func (h *ScriptHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	rows, err := h.pool.Query(r.Context(), `
		SELECT `+scriptSelectColumns+`
		FROM scripts WHERE tenant_id = $1 ORDER BY name
	`, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	defer rows.Close()

	scripts := make([]scriptRow, 0)
	for rows.Next() {
		s, err := scanScriptRow(rows)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
			return
		}
		scripts = append(scripts, s)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
		return
	}

	response.JSON(w, http.StatusOK, scripts)
}

type createScriptRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Interpreter string  `json:"interpreter"`
	Content     string  `json:"content"`
}

// Create crée un script dans la bibliothèque du tenant courant.
//
//	POST /api/v1/scripts
func (h *ScriptHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())

	var req createScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name est requis")
		return
	}
	if !scriptAllowedInterpreters[req.Interpreter] {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "interpreter invalide : "+req.Interpreter)
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "content est requis")
		return
	}

	var scriptID string
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO scripts (tenant_id, name, description, interpreter, content, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, tenantID, req.Name, req.Description, req.Interpreter, req.Content, userID).Scan(&scriptID)
	if err != nil {
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "SCRIPT_ALREADY_EXISTS", "un script avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création du script")
		return
	}

	h.audit.Record(r.Context(), "script.create", "script", scriptID, req)
	h.respondScript(w, r, http.StatusCreated, scriptID)
}

// respondScript recharge un script et écrit la réponse.
func (h *ScriptHandler) respondScript(w http.ResponseWriter, r *http.Request, status int, scriptID string) {
	row := h.pool.QueryRow(r.Context(), `SELECT `+scriptSelectColumns+` FROM scripts WHERE id = $1`, scriptID)
	s, err := scanScriptRow(row)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	response.JSON(w, status, s)
}

type updateScriptRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Interpreter *string `json:"interpreter"`
	Content     *string `json:"content"`
}

// Update modifie un script existant.
//
//	PATCH /api/v1/scripts/:scriptID
func (h *ScriptHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	scriptID := chi.URLParam(r, "scriptID")

	var req updateScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	var name, interpreter, content string
	var description *string
	err := h.pool.QueryRow(r.Context(), `
		SELECT name, description, interpreter, content FROM scripts WHERE id = $1 AND tenant_id = $2
	`, scriptID, tenantID).Scan(&name, &description, &interpreter, &content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "script introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name ne peut pas être vide")
			return
		}
		name = trimmed
	}
	if req.Description != nil {
		description = req.Description
	}
	if req.Interpreter != nil {
		if !scriptAllowedInterpreters[*req.Interpreter] {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "interpreter invalide : "+*req.Interpreter)
			return
		}
		interpreter = *req.Interpreter
	}
	if req.Content != nil {
		if strings.TrimSpace(*req.Content) == "" {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "content ne peut pas être vide")
			return
		}
		content = *req.Content
	}

	_, err = h.pool.Exec(r.Context(), `
		UPDATE scripts SET name = $1, description = $2, interpreter = $3, content = $4
		WHERE id = $5 AND tenant_id = $6
	`, name, description, interpreter, content, scriptID, tenantID)
	if err != nil {
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "SCRIPT_ALREADY_EXISTS", "un script avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	h.audit.Record(r.Context(), "script.update", "script", scriptID, req)
	h.respondScript(w, r, http.StatusOK, scriptID)
}

// Delete supprime un script. Les planifications qui le référencent sont
// supprimées en cascade (script_schedules.script_id ON DELETE CASCADE).
//
//	DELETE /api/v1/scripts/:scriptID
func (h *ScriptHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	scriptID := chi.URLParam(r, "scriptID")

	tag, err := h.pool.Exec(r.Context(), `DELETE FROM scripts WHERE id = $1 AND tenant_id = $2`, scriptID, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}
	if tag.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "script introuvable")
		return
	}

	h.audit.Record(r.Context(), "script.delete", "script", scriptID, nil)
	w.WriteHeader(http.StatusNoContent)
}
