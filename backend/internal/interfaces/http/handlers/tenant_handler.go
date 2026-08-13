package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	tenantDomain "github.com/yourorg/leo-one/internal/domain/tenant"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// TenantHandler gère les requêtes HTTP pour les paramètres du tenant courant
// (pas un CRUD multi-ressources comme Users/Workspaces : un tenant ne se
// crée/supprime pas via cette API en self-service, seul GET/PATCH existent).
type TenantHandler struct {
	tenantRepo tenantDomain.Repository
	agentRepo  agentDomain.Repository
}

// NewTenantHandler crée un TenantHandler avec ses dépendances.
func NewTenantHandler(tenantRepo tenantDomain.Repository, agentRepo agentDomain.Repository) *TenantHandler {
	return &TenantHandler{tenantRepo: tenantRepo, agentRepo: agentRepo}
}

type tenantResponse struct {
	tenantDomain.Tenant
	AgentCount int            `json:"agent_count"`
	PlanLimits map[string]any `json:"plan_limits"`
}

// Get retourne les paramètres du tenant courant, le nombre d'agents
// utilisés et les limites du plan.
//
//	GET /api/v1/tenant
func (h *TenantHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	t, err := h.tenantRepo.FindByID(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if t == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "tenant introuvable")
		return
	}

	count, err := h.agentRepo.CountByTenant(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	response.JSON(w, http.StatusOK, tenantResponse{
		Tenant:     *t,
		AgentCount: count,
		// max_agents=0 signifie "illimité" (voir AgentHandler.Enroll, la
		// vérification de quota) — même convention reprise ici.
		PlanLimits: map[string]any{"max_agents": t.MaxAgents},
	})
}

type updateTenantRequest struct {
	Name *string `json:"name"`
}

// Update modifie le nom du tenant courant (seul champ modifiable en
// self-service — plan/max_agents/is_active ne sont pas exposés ici).
//
//	PATCH /api/v1/tenant
func (h *TenantHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	var req updateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	current, err := h.tenantRepo.FindByID(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if current == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "tenant introuvable")
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

	if err := h.tenantRepo.Update(r.Context(), current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "tenant introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	response.JSON(w, http.StatusOK, current)
}
