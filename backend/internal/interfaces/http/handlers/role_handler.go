package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// RoleHandler expose en lecture seule les rôles et permissions du tenant
// courant, pour l'assignation de rôles à un utilisateur (voir
// UserHandler.Create/Update, champ role_ids). La création/modification/
// suppression de rôles personnalisés (Create/Update/Delete ci-dessous) est
// une fonctionnalité à part entière, pas encore implémentée — renvoie 501,
// comme StubHandler.
type RoleHandler struct {
	pool *pgxpool.Pool
}

// NewRoleHandler crée un RoleHandler avec le pool de connexions fourni.
func NewRoleHandler(pool *pgxpool.Pool) *RoleHandler {
	return &RoleHandler{pool: pool}
}

type roleRow struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	IsSystem    bool    `json:"is_system"`
}

// List retourne tous les rôles du tenant courant (système + personnalisés,
// aucun n'existant encore côté personnalisé tant que Create renvoie 501).
//
//	GET /api/v1/roles
func (h *RoleHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	rows, err := h.pool.Query(r.Context(), `
		SELECT id, name, description, is_system
		FROM roles
		WHERE tenant_id = $1
		ORDER BY is_system DESC, name
	`, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	defer rows.Close()

	roles := make([]roleRow, 0)
	for rows.Next() {
		var role roleRow
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem); err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
			return
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
		return
	}

	response.JSON(w, http.StatusOK, roles)
}

type permissionRow struct {
	ID          string  `json:"id"`
	Resource    string  `json:"resource"`
	Action      string  `json:"action"`
	Description *string `json:"description,omitempty"`
}

// ListPermissions retourne le catalogue complet des permissions atomiques
// (resource × action) — global, pas scopé par tenant (voir migrations/
// 003_rbac_seed.sql : les permissions sont partagées entre tous les tenants,
// seuls les rôles et leurs assignations le sont).
//
//	GET /api/v1/permissions
func (h *RoleHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, resource, action, description
		FROM permissions
		ORDER BY resource, action
	`)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	defer rows.Close()

	perms := make([]permissionRow, 0)
	for rows.Next() {
		var p permissionRow
		if err := rows.Scan(&p.ID, &p.Resource, &p.Action, &p.Description); err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
			return
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
		return
	}

	response.JSON(w, http.StatusOK, perms)
}

// ─── Gestion des rôles personnalisés ───────────────────────────────────────
//
// Pas encore implémenté : création/modification/suppression de rôles avec
// assignation de permissions, protection des rôles système (is_system=true
// → 403). Une fonctionnalité à part entière, hors périmètre de
// l'implémentation initiale de RoleHandler (lecture seule). Renvoie 501,
// comme StubHandler.

func (h *RoleHandler) Create(w http.ResponseWriter, r *http.Request) { stub501(w, r) }
func (h *RoleHandler) Update(w http.ResponseWriter, r *http.Request) { stub501(w, r) }
func (h *RoleHandler) Delete(w http.ResponseWriter, r *http.Request) { stub501(w, r) }
