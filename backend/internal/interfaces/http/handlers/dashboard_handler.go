package handlers

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// DashboardHandler gère les requêtes HTTP pour le tableau de bord.
type DashboardHandler struct {
	pool *pgxpool.Pool
}

// NewDashboardHandler crée un DashboardHandler avec ses dépendances.
func NewDashboardHandler(pool *pgxpool.Pool) *DashboardHandler {
	return &DashboardHandler{pool: pool}
}

// Summary retourne les agrégats pour la page d'accueil.
//
//	GET /api/v1/dashboard/summary
func (h *DashboardHandler) Summary(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	if tenantID == "" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant_id manquant")
		return
	}

	ctx := r.Context()

	// Comptage des agents par statut
	var agentsTotal, agentsOnline, agentsOffline, agentsUnresponsive int
	_ = h.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE status = 'online')       AS online,
			COUNT(*) FILTER (WHERE status = 'offline')      AS offline,
			COUNT(*) FILTER (WHERE status = 'unresponsive') AS unresponsive
		FROM agents WHERE tenant_id = $1
	`, tenantID).Scan(&agentsTotal, &agentsOnline, &agentsOffline, &agentsUnresponsive)

	// Comptage des alertes ouvertes
	var alertsOpen, alertsCritical int
	_ = h.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'open')                           AS alerts_open,
			COUNT(*) FILTER (WHERE status = 'open' AND severity = 'critical') AS alerts_critical
		FROM alerts WHERE tenant_id = $1
	`, tenantID).Scan(&alertsOpen, &alertsCritical)

	// Comptage des tickets ouverts
	var ticketsOpen int
	_ = h.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM tickets WHERE tenant_id = $1 AND status = 'open'
	`, tenantID).Scan(&ticketsOpen)

	response.JSON(w, http.StatusOK, map[string]any{
		"agents_total":        agentsTotal,
		"agents_online":       agentsOnline,
		"agents_offline":      agentsOffline,
		"agents_unresponsive": agentsUnresponsive,
		"alerts_open":         alertsOpen,
		"alerts_critical":     alertsCritical,
		"tickets_open":        ticketsOpen,
	})
}
