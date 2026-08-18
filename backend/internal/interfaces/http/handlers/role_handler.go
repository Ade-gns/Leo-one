package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// RoleHandler gère les rôles et permissions du tenant courant : lecture
// (List/ListPermissions) et gestion des rôles personnalisés (Create/Update/
// Delete — les rôles système, is_system=true, ne sont ni modifiables ni
// supprimables).
type RoleHandler struct {
	pool  *pgxpool.Pool
	audit *AuditLogger
}

// NewRoleHandler crée un RoleHandler avec ses dépendances.
func NewRoleHandler(pool *pgxpool.Pool, audit *AuditLogger) *RoleHandler {
	return &RoleHandler{pool: pool, audit: audit}
}

type permissionRow struct {
	ID          string  `json:"id"`
	Resource    string  `json:"resource"`
	Action      string  `json:"action"`
	Description *string `json:"description,omitempty"`
}

type roleRow struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	IsSystem    bool            `json:"is_system"`
	Permissions []permissionRow `json:"permissions"`
}

// permissionsForRoles retourne les permissions de chaque rôle, groupées par
// role_id, en une seule requête (évite un aller-retour par rôle).
func (h *RoleHandler) permissionsForRoles(ctx context.Context, roleIDs []string) (map[string][]permissionRow, error) {
	result := make(map[string][]permissionRow, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}

	rows, err := h.pool.Query(ctx, `
		SELECT rp.role_id, p.id, p.resource, p.action, p.description
		FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = ANY($1)
		ORDER BY p.resource, p.action
	`, roleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var roleID string
		var p permissionRow
		if err := rows.Scan(&roleID, &p.ID, &p.Resource, &p.Action, &p.Description); err != nil {
			return nil, err
		}
		result[roleID] = append(result[roleID], p)
	}
	return result, rows.Err()
}

// List retourne tous les rôles du tenant courant (système + personnalisés),
// avec leurs permissions.
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

	roles := make([]roleRow, 0)
	ids := make([]string, 0)
	for rows.Next() {
		var role roleRow
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem); err != nil {
			rows.Close()
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
			return
		}
		roles = append(roles, role)
		ids = append(ids, role.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
		return
	}

	perms, err := h.permissionsForRoles(r.Context(), ids)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture des permissions")
		return
	}
	for i := range roles {
		roles[i].Permissions = perms[roles[i].ID]
	}

	response.JSON(w, http.StatusOK, roles)
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

type createRoleRequest struct {
	Name          string   `json:"name"`
	Description   *string  `json:"description"`
	PermissionIDs []string `json:"permission_ids"`
}

// setRolePermissions remplace l'ensemble des permissions d'un rôle, dans une
// transaction. Ne (ré)insère que les permission_ids qui existent réellement
// (jointure implicite dans le INSERT ... SELECT) — le nombre de lignes
// effectivement insérées permet à l'appelant de détecter un permission_id
// invalide.
func (h *RoleHandler) setRolePermissions(ctx context.Context, roleID string, permissionIDs []string) (int, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback après commit réussi est un no-op sans danger

	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return 0, err
	}

	assigned := 0
	if len(permissionIDs) > 0 {
		tag, err := tx.Exec(ctx, `
			INSERT INTO role_permissions (role_id, permission_id)
			SELECT $1, p.id FROM permissions p WHERE p.id = ANY($2)
		`, roleID, permissionIDs)
		if err != nil {
			return 0, err
		}
		assigned = int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return assigned, nil
}

// respondRole recharge un rôle (avec ses permissions) et écrit la réponse.
func (h *RoleHandler) respondRole(w http.ResponseWriter, r *http.Request, status int, roleID string) {
	var role roleRow
	err := h.pool.QueryRow(r.Context(), `
		SELECT id, name, description, is_system FROM roles WHERE id = $1
	`, roleID).Scan(&role.ID, &role.Name, &role.Description, &role.IsSystem)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	perms, err := h.permissionsForRoles(r.Context(), []string{roleID})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture des permissions")
		return
	}
	role.Permissions = perms[roleID]

	response.JSON(w, status, role)
}

// Create crée un rôle personnalisé (is_system=false) dans le tenant courant.
//
//	POST /api/v1/roles
func (h *RoleHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	var req createRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "name est requis")
		return
	}

	var roleID string
	err := h.pool.QueryRow(r.Context(), `
		INSERT INTO roles (tenant_id, name, description, is_system)
		VALUES ($1, $2, $3, FALSE)
		RETURNING id
	`, tenantID, req.Name, req.Description).Scan(&roleID)
	if err != nil {
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "ROLE_ALREADY_EXISTS", "un rôle avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création du rôle")
		return
	}

	if len(req.PermissionIDs) > 0 {
		assigned, err := h.setRolePermissions(r.Context(), roleID, req.PermissionIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'assignation des permissions")
			return
		}
		if assigned != len(req.PermissionIDs) {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "un ou plusieurs permission_ids sont invalides")
			return
		}
	}

	h.audit.Record(r.Context(), "role.create", "role", roleID, req)
	h.respondRole(w, r, http.StatusCreated, roleID)
}

type updateRoleRequest struct {
	Name          *string   `json:"name"`
	Description   *string   `json:"description"`
	PermissionIDs *[]string `json:"permission_ids"`
}

// Update modifie un rôle personnalisé (name/description/permission_ids).
// Les rôles système (is_system=true) sont immuables → 403.
//
//	PATCH /api/v1/roles/:roleID
func (h *RoleHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	roleID := chi.URLParam(r, "roleID")

	var req updateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	var name string
	var description *string
	var isSystem bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT name, description, is_system FROM roles WHERE id = $1 AND tenant_id = $2
	`, roleID, tenantID).Scan(&name, &description, &isSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "rôle introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if isSystem {
		response.Error(w, http.StatusForbidden, "SYSTEM_ROLE_IMMUTABLE", "les rôles système ne sont pas modifiables")
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

	_, err = h.pool.Exec(r.Context(), `
		UPDATE roles SET name = $1, description = $2 WHERE id = $3 AND tenant_id = $4
	`, name, description, roleID, tenantID)
	if err != nil {
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "ROLE_ALREADY_EXISTS", "un rôle avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	if req.PermissionIDs != nil {
		assigned, err := h.setRolePermissions(r.Context(), roleID, *req.PermissionIDs)
		if err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'assignation des permissions")
			return
		}
		if assigned != len(*req.PermissionIDs) {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "un ou plusieurs permission_ids sont invalides")
			return
		}
	}

	h.audit.Record(r.Context(), "role.update", "role", roleID, req)
	h.respondRole(w, r, http.StatusOK, roleID)
}

// Delete supprime un rôle personnalisé. Les rôles système sont immuables →
// 403. Les utilisateurs qui l'avaient assigné sont simplement désassignés
// (user_roles.role_id ON DELETE CASCADE), jamais supprimés.
//
//	DELETE /api/v1/roles/:roleID
func (h *RoleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	roleID := chi.URLParam(r, "roleID")

	var isSystem bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT is_system FROM roles WHERE id = $1 AND tenant_id = $2
	`, roleID, tenantID).Scan(&isSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "rôle introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if isSystem {
		response.Error(w, http.StatusForbidden, "SYSTEM_ROLE_IMMUTABLE", "les rôles système ne sont pas supprimables")
		return
	}

	if _, err := h.pool.Exec(r.Context(), `DELETE FROM roles WHERE id = $1 AND tenant_id = $2`, roleID, tenantID); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}

	h.audit.Record(r.Context(), "role.delete", "role", roleID, nil)
	w.WriteHeader(http.StatusNoContent)
}
