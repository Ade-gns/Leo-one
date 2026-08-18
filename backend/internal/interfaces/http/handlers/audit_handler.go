package handlers

import (
	"net/http"
	"strconv"
	"time"

	auditDomain "github.com/yourorg/leo-one/internal/domain/audit"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// AuditHandler expose la consultation du journal d'audit du tenant courant.
// Lecture seule — les entrées sont écrites par AuditLogger, jamais via
// cette route (voir RequireAdmin dans router.go : accès réservé aux
// administrateurs).
type AuditHandler struct {
	repo auditDomain.Repository
}

// NewAuditHandler crée un AuditHandler avec ses dépendances.
func NewAuditHandler(repo auditDomain.Repository) *AuditHandler {
	return &AuditHandler{repo: repo}
}

// List retourne la liste paginée des entrées d'audit du tenant.
//
//	GET /api/v1/audit-log
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	if tenantID == "" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant_id manquant dans le contexte")
		return
	}

	q := r.URL.Query()
	filter := auditDomain.ListFilter{
		Cursor: q.Get("cursor"),
		Limit:  50,
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			filter.Limit = n
		}
	}

	if userID := q.Get("user_id"); userID != "" {
		filter.UserID = &userID
	}
	if action := q.Get("action"); action != "" {
		filter.Action = &action
	}
	if resourceType := q.Get("resource_type"); resourceType != "" {
		filter.ResourceType = &resourceType
	}
	if fromStr := q.Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			filter.From = &t
		}
	}
	if toStr := q.Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			filter.To = &t
		}
	}

	entries, nextCursor, err := h.repo.List(r.Context(), tenantID, filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération du journal d'audit")
		return
	}

	response.JSONWithMeta(w, http.StatusOK, entries, map[string]any{
		"cursor": nextCursor,
	})
}
