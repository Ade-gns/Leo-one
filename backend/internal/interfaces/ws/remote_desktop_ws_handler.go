package ws

import (
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	rdDomain "github.com/yourorg/leo-one/internal/domain/remotedesktop"
	rdInfra "github.com/yourorg/leo-one/internal/infrastructure/remotedesktop"
)

// RemoteDesktopWSHandler gère l'upgrade WebSocket de la connexion dédiée
// qu'un agent ouvre pour transmettre un flux de bureau à distance, suite à
// une commande LEO_MSG_REMOTE_DESKTOP_START reçue sur le canal de contrôle
// (voir handlers.RemoteDesktopHandler.createSession). Même listener mTLS
// que AgentWSHandler (:8081, voir cmd/server/main.go), route distincte
// (/ws/remote-desktop) — voir relay.go pour pourquoi cette connexion est
// volontairement séparée du canal de contrôle plutôt que multiplexée dessus.
type RemoteDesktopWSHandler struct {
	relay  *rdInfra.Relay
	repo   rdDomain.Repository
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewRemoteDesktopWSHandler crée le handler WebSocket.
func NewRemoteDesktopWSHandler(relay *rdInfra.Relay, repo rdDomain.Repository, pool *pgxpool.Pool, logger *slog.Logger) *RemoteDesktopWSHandler {
	return &RemoteDesktopWSHandler{relay: relay, repo: repo, pool: pool, logger: logger}
}

// ServeHTTP est le point d'entrée HTTP de la route GET /ws/remote-desktop.
//
// Double vérification avant l'upgrade : le certificat client mTLS identifie
// l'agent qui se connecte (même mécanisme que AgentWSHandler), et le jeton
// de query string résout la session — les deux doivent désigner le même
// agent_id, sans quoi la connexion est refusée. Le jeton est consommé (à
// usage unique) même en cas de désaccord : un jeton présenté par un agent
// qui n'est manifestement pas le bon ne doit de toute façon plus pouvoir
// resservir.
func (h *RemoteDesktopWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := h.logger.With("remote_addr", r.RemoteAddr)

	agentID, _, err := extractIdentityFromCert(r, h.pool, h.logger)
	if err != nil {
		log.Warn("Certificat client invalide ou absent (bureau à distance)", "error", err)
		http.Error(w, "Unauthorized: invalid client certificate", http.StatusUnauthorized)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Unauthorized: missing token", http.StatusUnauthorized)
		return
	}

	sess, err := h.repo.ConsumeAgentToken(r.Context(), token)
	if err != nil {
		log.Error("Échec consommation du jeton agent (bureau à distance)", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if sess == nil {
		http.Error(w, "Unauthorized: invalid, expired or already used token", http.StatusUnauthorized)
		return
	}
	if sess.AgentID != agentID {
		log.Warn("Jeton bureau à distance valide mais émis pour un autre agent",
			"token_agent_id", sess.AgentID, "cert_agent_id", agentID)
		http.Error(w, "Unauthorized: token/agent mismatch", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Échec upgrade WebSocket (bureau à distance)", "error", err)
		return
	}

	log.Info("Agent connecté pour une session de bureau à distance",
		"session_id", sess.ID, "mode", sess.Mode)
	h.relay.AttachAgent(sess, conn)
}
