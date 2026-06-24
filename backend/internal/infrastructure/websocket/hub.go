package websocket

import (
	"log/slog"
	"sync"
)

// Hub est le registre central de toutes les connexions agents actives.
// Thread-safe via RWMutex.
//
// Cycle de vie d'un client :
//   AgentWSHandler.ServeHTTP → Register(client) → ReadPump + WritePump
//   Déconnexion → ReadPump se termine → Unregister(client)
type Hub struct {
	// clients indexés par agent_id. Protégé par mu.
	clients map[string]*Client
	mu      sync.RWMutex

	// Dispatcher reçoit les messages entrants pour traitement métier.
	dispatcher *Dispatcher

	logger *slog.Logger
}

// NewHub crée un Hub avec son Dispatcher.
func NewHub(dispatcher *Dispatcher, logger *slog.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// Register enregistre un nouveau client connecté.
// Si un client avec le même agent_id était déjà connecté (reconnexion),
// l'ancien est déconnecté proprement avant d'enregistrer le nouveau.
func (h *Hub) Register(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.clients[client.AgentID]; ok {
		h.logger.Warn("Agent déjà connecté — déconnexion de l'ancienne session",
			"agent_id", client.AgentID)
		close(existing.send)
		delete(h.clients, client.AgentID)
	}

	h.clients[client.AgentID] = client
	h.logger.Info("Agent connecté",
		"agent_id", client.AgentID,
		"tenant_id", client.TenantID,
		"total_connected", len(h.clients))
}

// Unregister supprime un client déconnecté du registre.
func (h *Hub) Unregister(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.clients[client.AgentID]; ok {
		close(client.send)
		delete(h.clients, client.AgentID)
		h.logger.Info("Agent déconnecté",
			"agent_id", client.AgentID,
			"total_connected", len(h.clients))
	}
}

// HandleIncoming reçoit un message brut d'un client et le passe au Dispatcher.
// Appelé depuis ReadPump (goroutine par client), donc doit être thread-safe.
func (h *Hub) HandleIncoming(client *Client, message []byte) {
	h.dispatcher.Dispatch(client, message)
}

// SendToAgent envoie un message JSON à un agent spécifique.
// Retourne false si l'agent n'est pas connecté ou si le canal est saturé.
func (h *Hub) SendToAgent(agentID string, msg any) bool {
	h.mu.RLock()
	client, ok := h.clients[agentID]
	h.mu.RUnlock()

	if !ok {
		h.logger.Debug("Tentative d'envoi à un agent non connecté",
			"agent_id", agentID)
		return false
	}

	return client.Send(msg)
}

// IsConnected retourne true si l'agent est actuellement connecté.
func (h *Hub) IsConnected(agentID string) bool {
	h.mu.RLock()
	_, ok := h.clients[agentID]
	h.mu.RUnlock()
	return ok
}

// ConnectedAgents retourne la liste des agent_id actuellement connectés.
// Utile pour le tableau de bord et les métriques de santé.
func (h *Hub) ConnectedAgents() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// ConnectedCount retourne le nombre d'agents actuellement connectés.
func (h *Hub) ConnectedCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
