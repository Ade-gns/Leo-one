package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	patchDomain "github.com/yourorg/leo-one/internal/domain/patch"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// PatchHandler gère les requêtes HTTP pour les mises à jour système. La
// remontée périodique des patchs disponibles arrive hors de ce handler (voir
// internal/infrastructure/websocket/dispatcher.go, LEO_MSG_PATCH_INVENTORY) —
// ce handler ne fait que lire cet inventaire et déclencher des installations.
type PatchHandler struct {
	repo         patchDomain.Repository
	agentRepo    agentDomain.Repository
	agentHandler *AgentHandler // réutilisé pour CreateAndDispatchCommand (même chemin que bulk-commands)
	pool         *pgxpool.Pool // résolution agent_ids/workspace_id pour l'installation groupée
	audit        *AuditLogger
}

// NewPatchHandler crée un PatchHandler avec ses dépendances.
func NewPatchHandler(repo patchDomain.Repository, agentRepo agentDomain.Repository, agentHandler *AgentHandler, pool *pgxpool.Pool, audit *AuditLogger) *PatchHandler {
	return &PatchHandler{repo: repo, agentRepo: agentRepo, agentHandler: agentHandler, pool: pool, audit: audit}
}

// List retourne la liste paginée des patchs connus pour un agent.
//
//	GET /api/v1/agents/:agentID/patches
func (h *PatchHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	q := r.URL.Query()
	filter := patchDomain.ListFilter{
		Cursor: q.Get("cursor"),
		Limit:  50,
	}
	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			filter.Limit = n
		}
	}
	if statusStr := q.Get("status"); statusStr != "" {
		s := patchDomain.Status(statusStr)
		filter.Status = &s
	}

	patches, nextCursor, err := h.repo.ListByAgent(r.Context(), tenantID, agentID, filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération des patchs")
		return
	}

	response.JSONWithMeta(w, http.StatusOK, patches, map[string]any{
		"cursor": nextCursor,
	})
}

// Summary retourne l'agrégat des patchs en attente pour le tenant courant
// (widget dashboard).
//
//	GET /api/v1/patches/summary
func (h *PatchHandler) Summary(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	summary, err := h.repo.Summary(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors du calcul du résumé des patchs")
		return
	}

	response.JSON(w, http.StatusOK, summary)
}

type installPatchesRequest struct {
	PatchIDs    []string `json:"patch_ids"`
	RebootAfter bool     `json:"reboot_after"`
}

// Install installe une sélection de patchs sur un agent.
//
//	POST /api/v1/agents/:agentID/patches/install
func (h *PatchHandler) Install(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	var req installPatchesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}
	if len(req.PatchIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "patch_ids est requis")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"patch_ids":    req.PatchIDs,
		"reboot_after": req.RebootAfter,
	})
	commandID, sent, err := h.agentHandler.CreateAndDispatchCommand(
		r.Context(), tenantID, agentID, &userID, nil, "install_patches", payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de la commande")
		return
	}

	h.audit.Record(r.Context(), "patch.install", "agent", agentID,
		map[string]any{"patch_ids": req.PatchIDs, "reboot_after": req.RebootAfter})
	response.JSON(w, http.StatusAccepted, map[string]any{
		"command_id": commandID,
		"status":     "pending",
		"sent":       sent,
	})
}

type bulkInstallPatchesRequest struct {
	AgentIDs    []string `json:"agent_ids"`
	WorkspaceID *string  `json:"workspace_id"`
	MinSeverity string   `json:"min_severity"` // "optional" (défaut, = tous) | "important" | "critical"
	RebootAfter bool     `json:"reboot_after"`
}

// BulkInstall installe, sur plusieurs agents ou un workspace entier, les
// patchs actuellement disponibles pour CHAQUE agent cible (au moins
// min_severity) — pas une liste de patch_ids partagée : les identifiants de
// patch sont spécifiques à l'inventaire de chaque machine (nom de paquet
// Linux, KB Windows), donc une même liste n'aurait de sens que pour un parc
// parfaitement homogène. Même logique de ciblage que BulkCreateCommand.
//
//	POST /api/v1/agents/bulk-patches/install
func (h *PatchHandler) BulkInstall(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())

	var req bulkInstallPatchesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	minSeverity := patchDomain.SeverityOptional
	switch req.MinSeverity {
	case "", string(patchDomain.SeverityOptional):
		minSeverity = patchDomain.SeverityOptional
	case string(patchDomain.SeverityImportant):
		minSeverity = patchDomain.SeverityImportant
	case string(patchDomain.SeverityCritical):
		minSeverity = patchDomain.SeverityCritical
	default:
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "min_severity invalide : "+req.MinSeverity)
		return
	}

	hasAgentIDs := len(req.AgentIDs) > 0
	hasWorkspace := req.WorkspaceID != nil && *req.WorkspaceID != ""
	if hasAgentIDs == hasWorkspace {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"préciser soit agent_ids, soit workspace_id (l'un des deux, pas les deux)")
		return
	}

	var (
		targetIDs []string
		err       error
	)
	if hasAgentIDs {
		var rows pgx.Rows
		rows, err = h.pool.Query(r.Context(), `
			SELECT id FROM agents WHERE tenant_id = $1 AND id = ANY($2)
		`, tenantID, req.AgentIDs)
		if err == nil {
			targetIDs, err = scanAgentIDs(rows)
		}
	} else {
		var rows pgx.Rows
		rows, err = h.pool.Query(r.Context(), `
			SELECT id FROM agents WHERE tenant_id = $1 AND workspace_id = $2
		`, tenantID, *req.WorkspaceID)
		if err == nil {
			targetIDs, err = scanAgentIDs(rows)
		}
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if len(targetIDs) == 0 {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "aucun agent cible trouvé")
		return
	}

	results := make([]bulkCommandResult, 0, len(targetIDs))
	for _, agentID := range targetIDs {
		ids, err := h.repo.AvailableNativeIDs(r.Context(), tenantID, agentID, minSeverity)
		if err != nil {
			results = append(results, bulkCommandResult{AgentID: agentID, Error: err.Error()})
			continue
		}
		if len(ids) == 0 {
			results = append(results, bulkCommandResult{AgentID: agentID, Error: "aucun patch disponible pour cet agent"})
			continue
		}

		payload, _ := json.Marshal(map[string]any{"patch_ids": ids, "reboot_after": req.RebootAfter})
		commandID, sent, err := h.agentHandler.CreateAndDispatchCommand(
			r.Context(), tenantID, agentID, &userID, nil, "install_patches", payload)
		if err != nil {
			results = append(results, bulkCommandResult{AgentID: agentID, Error: err.Error()})
			continue
		}
		results = append(results, bulkCommandResult{AgentID: agentID, CommandID: commandID, Sent: sent})
	}

	h.audit.Record(r.Context(), "patch.bulk_install", "agent", "",
		map[string]any{"min_severity": string(minSeverity), "target_count": len(targetIDs), "results": results})
	response.JSON(w, http.StatusAccepted, results)
}
