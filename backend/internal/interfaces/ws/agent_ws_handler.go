// Package ws expose le handler HTTP qui upgradie les connexions WebSocket des agents.
package ws

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

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
	logger *slog.Logger
}

// NewAgentWSHandler crée le handler WebSocket.
func NewAgentWSHandler(hub *leoWS.Hub, logger *slog.Logger) *AgentWSHandler {
	return &AgentWSHandler{hub: hub, logger: logger}
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
	agentID, tenantID, err := h.extractIdentityFromCert(r)
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

// extractIdentityFromCert extrait l'agent_id (CN) et le tenant_id (OU[0])
// du certificat client présenté dans le handshake mTLS.
//
// Convention de nommage des certificats émis par le CA interne :
//   CN  = agent_id (UUID v4)
//   OU  = tenant_id (UUID v4)
//   O   = "leo-one"
func (h *AgentWSHandler) extractIdentityFromCert(r *http.Request) (agentID, tenantID string, err error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		// En développement sans mTLS, on accepte les headers X-Agent-ID / X-Tenant-ID.
		// JAMAIS en production.
		agentID  = r.Header.Get("X-Agent-ID")
		tenantID = r.Header.Get("X-Tenant-ID")
		if agentID != "" && tenantID != "" {
			h.logger.Warn("Authentification par header (mode dev) — désactiver en production")
			return agentID, tenantID, nil
		}
		return "", "", errorf("pas de certificat client TLS présenté")
	}

	cert := r.TLS.PeerCertificates[0]
	return h.parseCert(cert)
}

func (h *AgentWSHandler) parseCert(cert *x509.Certificate) (agentID, tenantID string, err error) {
	agentID = cert.Subject.CommonName
	if agentID == "" {
		return "", "", errorf("CN vide dans le certificat client")
	}

	if len(cert.Subject.OrganizationalUnit) == 0 {
		return "", "", errorf("OU manquant dans le certificat client")
	}
	tenantID = cert.Subject.OrganizationalUnit[0]

	return agentID, tenantID, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func errorf(msg string) error {
	return &wsError{msg: msg}
}

type wsError struct{ msg string }

func (e *wsError) Error() string { return e.msg }

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
