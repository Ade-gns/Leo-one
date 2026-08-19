package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	rdDomain "github.com/yourorg/leo-one/internal/domain/remotedesktop"
	rdInfra "github.com/yourorg/leo-one/internal/infrastructure/remotedesktop"
	leoWS "github.com/yourorg/leo-one/internal/infrastructure/websocket"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// Types de message du canal de contrôle agent (doivent correspondre aux
// constantes LEO_MSG_* de agent/include/leo_agent.h) utilisés pour piloter
// une session de bureau à distance. Volontairement PAS envoyés via
// AgentHandler.CreateAndDispatchCommand/la table commands : ce ne sont pas
// des commandes avec un CMD_RESULT à attendre, mais des messages de
// contrôle éphémères (même famille que HELLO_ACK), donc envoyés
// directement via le Hub.
const (
	msgTypeRemoteDesktopStart = 110
	msgTypeRemoteDesktopStop  = 111
)

// viewerUpgrader configure l'upgrade HTTP → WebSocket pour la connexion
// navigateur du bureau à distance. Route publique authentifiée par jeton
// (voir ServeViewerWS), pas par JWT — CheckOrigin permissif comme le reste
// de ce serveur, qui n'a par ailleurs aucune infrastructure CORS/origine à
// laquelle se raccrocher (aucune autre route WS/API du projet n'en fait).
var viewerUpgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024,
	WriteBufferSize: 64 * 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// RemoteDesktopHandler gère le cycle de vie des sessions de bureau à
// distance : création (côté REST, authentifiée par JWT+RBAC), pilotage de
// l'agent via le canal de contrôle existant, et upgrade de la connexion
// WebSocket navigateur qui rejoint le relais (voir
// internal/infrastructure/remotedesktop.Relay).
type RemoteDesktopHandler struct {
	repo       rdDomain.Repository
	agentRepo  agentDomain.Repository
	hub        *leoWS.Hub
	relay      *rdInfra.Relay
	sessionTTL time.Duration
	// wsEndpoint est l'URL wss:// que l'agent utilise pour ouvrir sa
	// connexion dédiée (voir Config.PublicRemoteDesktopWSEndpoint) ;
	// viewerWSEndpoint est l'URL ws:// que le navigateur utilise pour
	// rejoindre la même session côté relais (voir
	// Config.PublicViewerWSEndpoint).
	wsEndpoint       string
	viewerWSEndpoint string
	audit            *AuditLogger
	logger           *slog.Logger
}

// NewRemoteDesktopHandler crée un RemoteDesktopHandler avec ses dépendances.
func NewRemoteDesktopHandler(
	repo rdDomain.Repository,
	agentRepo agentDomain.Repository,
	hub *leoWS.Hub,
	relay *rdInfra.Relay,
	sessionTTL time.Duration,
	wsEndpoint, viewerWSEndpoint string,
	audit *AuditLogger,
	logger *slog.Logger,
) *RemoteDesktopHandler {
	return &RemoteDesktopHandler{
		repo:             repo,
		agentRepo:        agentRepo,
		hub:              hub,
		relay:            relay,
		sessionTTL:       sessionTTL,
		wsEndpoint:       wsEndpoint,
		viewerWSEndpoint: viewerWSEndpoint,
		audit:            audit,
		logger:           logger,
	}
}

type remoteDesktopSessionResponse struct {
	SessionID   string `json:"session_id"`
	Mode        string `json:"mode"`
	Status      string `json:"status"`
	ViewerToken string `json:"viewer_token"`
	ViewerWSURL string `json:"viewer_ws_url"`
	ExpiresAt   string `json:"expires_at"`
}

// ViewSession crée une session en lecture seule (permission remote_desktop:read).
//
//	POST /api/v1/agents/:agentID/remote-desktop/view-sessions
func (h *RemoteDesktopHandler) ViewSession(w http.ResponseWriter, r *http.Request) {
	h.createSession(w, r, rdDomain.ModeView)
}

// ControlSession crée une session avec prise de contrôle clavier/souris
// (permission remote_desktop:execute).
//
//	POST /api/v1/agents/:agentID/remote-desktop/control-sessions
func (h *RemoteDesktopHandler) ControlSession(w http.ResponseWriter, r *http.Request) {
	h.createSession(w, r, rdDomain.ModeControl)
}

func (h *RemoteDesktopHandler) createSession(w http.ResponseWriter, r *http.Request, mode string) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	if !h.hub.IsConnected(agentID) {
		response.Error(w, http.StatusConflict, "AGENT_OFFLINE", "l'agent n'est pas connecté")
		return
	}

	existing, err := h.repo.ActiveForAgent(r.Context(), agentID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if existing != nil {
		response.Error(w, http.StatusConflict, "SESSION_ALREADY_ACTIVE",
			"une session de bureau à distance est déjà en cours sur cet agent")
		return
	}

	sess := &rdDomain.Session{TenantID: tenantID, AgentID: agentID, Mode: mode}
	if userID != "" {
		sess.UserID = &userID
	}

	viewerToken, agentToken, err := h.repo.Create(r.Context(), sess, h.sessionTTL)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de la session")
		return
	}

	body, _ := json.Marshal(map[string]any{
		"session_id": sess.ID,
		"ws_url":     h.wsEndpoint + "?token=" + agentToken,
		"mode":       mode,
		"fps":        8,
		"quality":    60,
		"max_width":  1920,
		"max_height": 1080,
	})
	sent := h.hub.SendToAgent(agentID, map[string]any{
		"v":    1,
		"type": msgTypeRemoteDesktopStart,
		"id":   sess.ID,
		"ts":   time.Now().UnixMilli(),
		"body": json.RawMessage(body),
	})
	if !sent {
		// Course rare : l'agent s'est déconnecté entre le contrôle
		// IsConnected ci-dessus et cet envoi. La session ne pourra jamais
		// devenir active — on la termine tout de suite plutôt que de laisser
		// le navigateur attendre l'expiration du jeton.
		_ = h.repo.MarkEnded(r.Context(), sess.ID, "agent_offline")
		response.Error(w, http.StatusConflict, "AGENT_OFFLINE", "l'agent s'est déconnecté")
		return
	}

	h.audit.Record(r.Context(), "remote_desktop.session.start", "agent", agentID,
		map[string]any{"mode": mode, "session_id": sess.ID})

	response.JSON(w, http.StatusCreated, remoteDesktopSessionResponse{
		SessionID:   sess.ID,
		Mode:        sess.Mode,
		Status:      sess.Status,
		ViewerToken: viewerToken,
		ViewerWSURL: h.viewerWSEndpoint + "?token=" + viewerToken,
		ExpiresAt:   sess.ExpiresAt.Format(time.RFC3339),
	})
}

// GetSession retourne l'état courant d'une session (pending/active/ended) —
// utilisé par le viewer frontend pour un polling léger pendant
// l'établissement de la connexion WS, et après coup pour afficher la raison
// de fin.
//
//	GET /api/v1/agents/:agentID/remote-desktop/sessions/:sessionID
func (h *RemoteDesktopHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")
	sessionID := chi.URLParam(r, "sessionID")

	sess, err := h.repo.FindByID(r.Context(), tenantID, sessionID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if sess == nil || sess.AgentID != agentID {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "session introuvable")
		return
	}

	response.JSON(w, http.StatusOK, sess)
}

// StopSession termine explicitement une session, à la demande de
// l'opérateur (propriétaire de la session, ou tout utilisateur disposant de
// remote_desktop:read — même raisonnement d'accès que GetSession, pas
// besoin d'être celui qui l'a ouverte pour pouvoir la couper).
//
//	DELETE /api/v1/agents/:agentID/remote-desktop/sessions/:sessionID
func (h *RemoteDesktopHandler) StopSession(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")
	sessionID := chi.URLParam(r, "sessionID")

	sess, err := h.repo.FindByID(r.Context(), tenantID, sessionID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if sess == nil || sess.AgentID != agentID {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "session introuvable")
		return
	}

	if sess.Status != rdDomain.StatusEnded {
		body, _ := json.Marshal(map[string]any{"session_id": sess.ID})
		h.hub.SendToAgent(agentID, map[string]any{
			"v":    1,
			"type": msgTypeRemoteDesktopStop,
			"id":   sess.ID,
			"ts":   time.Now().UnixMilli(),
			"body": json.RawMessage(body),
		})

		if !h.relay.EndSession(sess.ID, "stopped_by_operator") {
			// Le relais ne connaissait pas (ou plus) cette session (ex :
			// arrêtée avant même que l'agent ou le navigateur ne se
			// connectent) — la fin est quand même actée en base.
			if err := h.repo.MarkEnded(r.Context(), sess.ID, "stopped_by_operator"); err != nil {
				response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'arrêt de la session")
				return
			}
		}

		h.audit.Record(r.Context(), "remote_desktop.session.stop", "agent", agentID,
			map[string]any{"session_id": sess.ID})
	}

	w.WriteHeader(http.StatusNoContent)
}

// ServeViewerWS upgrade la connexion WebSocket du navigateur et la rattache
// au relais — route PUBLIQUE (pas de JWT : le jeton dans l'URL, à usage
// unique et à courte durée de vie, EST l'authentification, même schéma que
// FileHandler.Download).
//
//	GET /api/v1/remote-desktop/ws?token=...
func (h *RemoteDesktopHandler) ServeViewerWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
		return
	}

	sess, err := h.repo.ConsumeViewerToken(r.Context(), token)
	if err != nil {
		h.logger.Error("Échec consommation du jeton navigateur (bureau à distance)", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "Unauthorized: invalid, expired or already used token", http.StatusUnauthorized)
		return
	}

	conn, err := viewerUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("Échec upgrade WebSocket navigateur (bureau à distance)", "error", err)
		return
	}

	h.logger.Info("Navigateur connecté pour une session de bureau à distance",
		"session_id", sess.ID, "agent_id", sess.AgentID, "mode", sess.Mode)
	h.relay.AttachViewer(sess, conn)
}
