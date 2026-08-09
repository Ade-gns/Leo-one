package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	inventoryDomain "github.com/yourorg/leo-one/internal/domain/inventory"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// InventoryHandler gère les requêtes HTTP pour l'inventaire matériel/logiciel.
type InventoryHandler struct {
	repo inventoryDomain.Repository
}

// NewInventoryHandler crée un InventoryHandler avec ses dépendances.
func NewInventoryHandler(repo inventoryDomain.Repository) *InventoryHandler {
	return &InventoryHandler{repo: repo}
}

// Hardware retourne le dernier snapshot matériel connu d'un agent.
//
//	GET /api/v1/agents/:agentID/inventory/hardware
func (h *InventoryHandler) Hardware(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	hw, err := h.repo.LatestHardware(r.Context(), tenantID, agentID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if hw == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "aucun inventaire matériel disponible pour cet agent")
		return
	}

	response.JSON(w, http.StatusOK, hw)
}

// Software retourne les logiciels installés d'un agent, filtrables par nom (?search=).
//
//	GET /api/v1/agents/:agentID/inventory/software
func (h *InventoryHandler) Software(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")
	search := r.URL.Query().Get("search")

	items, err := h.repo.ListSoftware(r.Context(), tenantID, agentID, search)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	response.JSON(w, http.StatusOK, items)
}
