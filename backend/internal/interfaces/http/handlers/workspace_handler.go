package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	workspaceDomain "github.com/yourorg/leo-one/internal/domain/workspace"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// pgUniqueViolationCode est le code d'erreur Postgres pour une violation de
// contrainte UNIQUE (23505) — voir https://www.postgresql.org/docs/current/errcodes-appendix.html
const pgUniqueViolationCode = "23505"

// isUniqueViolation détecte une violation de contrainte UNIQUE (ici,
// workspaces.UNIQUE(tenant_id, name)) pour la distinguer d'une vraie erreur
// serveur et répondre 409 plutôt que 500.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode
}

// WorkspaceHandler gère les requêtes HTTP pour les workspaces du tenant courant.
type WorkspaceHandler struct {
	repo workspaceDomain.Repository
}

// NewWorkspaceHandler crée un WorkspaceHandler avec ses dépendances.
func NewWorkspaceHandler(repo workspaceDomain.Repository) *WorkspaceHandler {
	return &WorkspaceHandler{repo: repo}
}

// List retourne tous les workspaces du tenant courant.
//
//	GET /api/v1/workspaces
func (h *WorkspaceHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	workspaces, err := h.repo.List(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	response.JSON(w, http.StatusOK, workspaces)
}

type createWorkspaceRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

// Create crée un nouveau workspace dans le tenant courant.
//
//	POST /api/v1/workspaces
func (h *WorkspaceHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	var req createWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name est requis")
		return
	}

	ws := &workspaceDomain.Workspace{
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
	}
	if err := h.repo.Create(r.Context(), ws); err != nil {
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "WORKSPACE_ALREADY_EXISTS", "un workspace avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création du workspace")
		return
	}

	response.JSON(w, http.StatusCreated, ws)
}

type updateWorkspaceRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// Update met à jour partiellement un workspace (name/description).
//
//	PATCH /api/v1/workspaces/:workspaceID
func (h *WorkspaceHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	workspaceID := chi.URLParam(r, "workspaceID")

	var req updateWorkspaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	current, err := h.repo.FindByID(r.Context(), tenantID, workspaceID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if current == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "workspace introuvable")
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name ne peut pas être vide")
			return
		}
		current.Name = trimmed
	}
	if req.Description != nil {
		current.Description = req.Description
	}

	if err := h.repo.Update(r.Context(), current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "workspace introuvable")
			return
		}
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "WORKSPACE_ALREADY_EXISTS", "un workspace avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	response.JSON(w, http.StatusOK, current)
}

// Delete supprime un workspace. Les agents qui y étaient rattachés passent
// à workspace_id = NULL (ON DELETE SET NULL) — jamais supprimés.
//
//	DELETE /api/v1/workspaces/:workspaceID
func (h *WorkspaceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	workspaceID := chi.URLParam(r, "workspaceID")

	if err := h.repo.Delete(r.Context(), tenantID, workspaceID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "workspace introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
