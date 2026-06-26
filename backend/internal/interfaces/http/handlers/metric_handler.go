package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	metricDomain "github.com/yourorg/leo-one/internal/domain/metric"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// MetricHandler gère les requêtes HTTP pour les métriques des agents.
type MetricHandler struct {
	metricRepo metricDomain.Repository
}

// NewMetricHandler crée un MetricHandler avec ses dépendances.
func NewMetricHandler(metricRepo metricDomain.Repository) *MetricHandler {
	return &MetricHandler{metricRepo: metricRepo}
}

// Query retourne les métriques d'un agent sur une plage de temps.
//
//	GET /api/v1/agents/:agentID/metrics
func (h *MetricHandler) Query(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	q := r.URL.Query()
	metricType := metricDomain.Type(q.Get("type"))
	if metricType == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "paramètre 'type' requis")
		return
	}

	// Plage temporelle par défaut : dernières 24h
	to := time.Now().UTC()
	from := to.Add(-24 * time.Hour)

	if fromStr := q.Get("from"); fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			from = t
		}
	}
	if toStr := q.Get("to"); toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			to = t
		}
	}

	results, resolution, err := h.metricRepo.Query(r.Context(), tenantID, agentID, metricType, from, to)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération des métriques")
		return
	}

	response.JSONWithMeta(w, http.StatusOK, results, map[string]any{
		"resolution": string(resolution),
		"from":       from.Format(time.RFC3339),
		"to":         to.Format(time.RFC3339),
	})
}

// Latest retourne la dernière valeur connue pour chaque type de métrique d'un agent.
//
//	GET /api/v1/agents/:agentID/metrics/latest
func (h *MetricHandler) Latest(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	metrics, ts, err := h.metricRepo.Latest(r.Context(), tenantID, agentID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération des métriques")
		return
	}

	data := make(map[string]any, len(metrics)+1)
	for k, v := range metrics {
		data[string(k)] = v
	}
	if !ts.IsZero() {
		data["ts"] = ts.Format(time.RFC3339)
	}

	response.JSON(w, http.StatusOK, data)
}
