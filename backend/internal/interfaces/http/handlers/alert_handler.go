package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// AlertHandler gère les requêtes HTTP pour les alertes et règles d'alerte.
type AlertHandler struct {
	repo  alertDomain.Repository
	audit *AuditLogger
}

// NewAlertHandler crée un AlertHandler avec ses dépendances.
func NewAlertHandler(repo alertDomain.Repository, audit *AuditLogger) *AlertHandler {
	return &AlertHandler{repo: repo, audit: audit}
}

// List retourne la liste paginée des alertes du tenant.
//
//	GET /api/v1/alerts
func (h *AlertHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	if tenantID == "" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant_id manquant dans le contexte")
		return
	}

	q := r.URL.Query()
	filter := alertDomain.ListFilter{
		Cursor: q.Get("cursor"),
		Limit:  50,
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			filter.Limit = n
		}
	}

	if statusStr := q.Get("status"); statusStr != "" {
		s := alertDomain.Status(statusStr)
		filter.Status = &s
	}

	if severityStr := q.Get("severity"); severityStr != "" {
		s := alertDomain.Severity(severityStr)
		filter.Severity = &s
	}

	if agentID := q.Get("agent_id"); agentID != "" {
		filter.AgentID = &agentID
	}

	alerts, nextCursor, err := h.repo.List(r.Context(), tenantID, filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération des alertes")
		return
	}

	response.JSONWithMeta(w, http.StatusOK, alerts, map[string]any{
		"cursor": nextCursor,
	})
}

// Get retourne une alerte par son ID.
//
//	GET /api/v1/alerts/:alertID
func (h *AlertHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	alertID := chi.URLParam(r, "alertID")

	alert, err := h.repo.FindByID(r.Context(), tenantID, alertID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if alert == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "alerte introuvable")
		return
	}

	response.JSON(w, http.StatusOK, alert)
}

// Acknowledge acquitte une alerte.
//
//	POST /api/v1/alerts/:alertID/acknowledge
func (h *AlertHandler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())
	alertID := chi.URLParam(r, "alertID")

	alert, err := h.repo.Acknowledge(r.Context(), tenantID, alertID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'acquittement")
		return
	}
	if alert == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "alerte introuvable")
		return
	}

	h.audit.Record(r.Context(), "alert.acknowledge", "alert", alertID, nil)
	response.JSON(w, http.StatusOK, alert)
}

func alertStub(w http.ResponseWriter, _ *http.Request) {
	response.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "alerts: non encore implémenté")
}

// ListRules retourne la liste des règles d'alerte.
func (h *AlertHandler) ListRules(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// CreateRule crée une nouvelle règle d'alerte.
func (h *AlertHandler) CreateRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// UpdateRule met à jour une règle d'alerte existante.
func (h *AlertHandler) UpdateRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// DeleteRule supprime une règle d'alerte.
func (h *AlertHandler) DeleteRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }
