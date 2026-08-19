// Package ws expose le handler HTTP qui upgradie les connexions WebSocket des agents.
package ws

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	leoWS "github.com/yourorg/leo-one/internal/infrastructure/websocket"
)

// upgrader configure l'upgrade HTTP → WebSocket.
// CheckOrigin est désactivé côté agent (les agents ne sont pas des navigateurs).
var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024,
	WriteBufferSize: 64 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		// L'authentification est faite via mTLS : l'origine n'a pas de sens ici.
		return true
	},
}

// AgentWSHandler gère l'upgrade WebSocket et l'enregistrement des agents dans le Hub.
type AgentWSHandler struct {
	hub    *leoWS.Hub
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewAgentWSHandler crée le handler WebSocket.
func NewAgentWSHandler(hub *leoWS.Hub, pool *pgxpool.Pool, logger *slog.Logger) *AgentWSHandler {
	return &AgentWSHandler{hub: hub, pool: pool, logger: logger}
}

// ServeHTTP est le point d'entrée HTTP de la route GET /ws/agent.
//
// Flux d'authentification :
//  1. Le TLS est terminé en amont (Reverse Proxy ou ce serveur).
//  2. Le certificat client mTLS est validé par Go TLS → accessible via r.TLS.PeerCertificates.
//  3. On extrait l'agent_id et le tenant_id du certificat (CN et OU).
//  4. On upgrade en WebSocket et on enregistre le client dans le Hub.
func (h *AgentWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log := h.logger.With("remote_addr", r.RemoteAddr)

	// ── Extraction de l'identité depuis le certificat mTLS ──
	agentID, tenantID, err := extractIdentityFromCert(r, h.pool, h.logger)
	if err != nil {
		log.Warn("Certificat client invalide ou absent", "error", err)
		http.Error(w, "Unauthorized: invalid client certificate", http.StatusUnauthorized)
		return
	}

	log = log.With("agent_id", agentID, "tenant_id", tenantID)

	// ── Upgrade WebSocket ──
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Échec upgrade WebSocket", "error", err)
		return
	}

	// ── Création et enregistrement du client ──
	client := leoWS.NewClient(agentID, tenantID, conn, h.hub, log)
	h.hub.Register(client)

	log.Info("Agent enregistré dans le Hub")

	// Lancement des goroutines de lecture et d'écriture.
	// WritePump tourne dans une goroutine séparée.
	// ReadPump bloque dans la goroutine courante jusqu'à déconnexion.
	go client.WritePump()
	client.ReadPump() // bloquant — se termine quand la connexion se ferme
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// ConfigureMTLS retourne une *tls.Config pour le serveur WebSocket agent
// avec vérification du certificat client (mTLS).
// CACertPEM est le PEM du CA interne qui signe les certificats agents.
func ConfigureMTLS(caCertPEM []byte) (*tls.Config, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		return nil, errorf("impossible de charger le CA dans le pool de confiance")
	}

	return &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  pool,
		MinVersion: tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}, nil
}
