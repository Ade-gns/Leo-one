// Package websocket gère les connexions persistantes des agents via WebSocket.
package websocket

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Taille max d'un message agent entrant (64 KB).
	maxMessageSize = 64 * 1024

	// Délai de grâce pour la réponse PONG. Au-delà → déconnexion.
	pongWait = 90 * time.Second

	// Intervalle d'envoi des PING WebSocket (doit être < pongWait).
	pingPeriod = 60 * time.Second

	// Timeout d'écriture sur la socket.
	writeWait = 10 * time.Second

	// Taille du canal d'envoi par client.
	sendChanSize = 64
)

// NewClient crée un Client prêt à être enregistré dans le Hub.
func NewClient(agentID, tenantID string, conn *websocket.Conn, hub *Hub, logger *slog.Logger) *Client {
	return &Client{
		AgentID:  agentID,
		TenantID: tenantID,
		conn:     conn,
		send:     make(chan []byte, sendChanSize),
		hub:      hub,
		logger:   logger.With("agent_id", agentID),
	}
}

// Client représente une connexion WebSocket active d'un agent.
// Une goroutine ReadPump et une WritePump sont lancées pour chaque client.
type Client struct {
	AgentID  string
	TenantID string

	conn   *websocket.Conn
	send   chan []byte
	hub    *Hub
	logger *slog.Logger
}

// Send envoie un message JSON à l'agent de façon non-bloquante.
// Retourne false si le canal d'envoi est plein (agent trop lent).
func (c *Client) Send(msg any) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.Error("Impossible de sérialiser le message", "error", err)
		return false
	}

	select {
	case c.send <- data:
		return true
	default:
		c.logger.Warn("Canal d'envoi saturé — message abandonné",
			"agent_id", c.AgentID)
		return false
	}
}

// ReadPump lit les messages entrants de l'agent et les envoie au Hub.
// Doit être lancé dans une goroutine dédiée.
// Se termine proprement quand la connexion est fermée ou en erreur.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))

	// Renouvelle la deadline à chaque PONG reçu.
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				c.logger.Warn("Connexion agent fermée de façon inattendue",
					"agent_id", c.AgentID,
					"error", err)
			}
			break
		}

		// Dispatch du message vers le hub pour traitement métier.
		c.hub.HandleIncoming(c, message)
	}
}

// WritePump écrit les messages sortants vers l'agent.
// Envoie également des PING WebSocket périodiques pour détecter les déconnexions.
// Doit être lancé dans une goroutine dédiée.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {

		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			if !ok {
				// Le Hub a fermé le canal → fermeture propre.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Vider le canal en une seule passe (batch) pour réduire les syscalls.
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.logger.Debug("Échec envoi PING", "agent_id", c.AgentID, "error", err)
				return
			}
		}
	}
}
