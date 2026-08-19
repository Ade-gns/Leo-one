// Package remotedesktop définit l'entité Session (bureau à distance) et son
// interface de domaine. Cette couche ne connaît aucune dépendance externe
// (pas de DB, pas de HTTP, pas de WebSocket).
package remotedesktop

import (
	"context"
	"time"
)

// Modes d'une session — voir migrations/008_remote_desktop.sql et la
// permission (resource, action) associée : "read" ouvre une session Mode
// (view = lecture seule), "execute" ouvre une session Mode Control.
const (
	ModeView    = "view"
	ModeControl = "control"
)

// États du cycle de vie d'une session.
const (
	StatusPending = "pending" // créée, en attente qu'agent ET navigateur se connectent au relais
	StatusActive  = "active"  // les deux côtés sont connectés, le relais pompe les messages
	StatusEnded   = "ended"   // terminée (normalement, par timeout d'appariement, ou agent déconnecté)
)

// Session est une session de bureau à distance : un pairage éphémère entre
// une connexion agent et une connexion navigateur, relayé en mémoire par
// internal/infrastructure/remotedesktop.Relay.
type Session struct {
	ID       string  `json:"id"`
	TenantID string  `json:"tenant_id"`
	AgentID  string  `json:"agent_id"`
	UserID   *string `json:"user_id,omitempty"` // nil si jamais rattachée à un utilisateur (ne devrait pas arriver en usage normal, toutes les sessions sont créées via l'API REST authentifiée)
	Mode     string  `json:"mode"`              // ModeView | ModeControl
	Status   string  `json:"status"`            // StatusPending | StatusActive | StatusEnded

	ExpiresAt time.Time `json:"expires_at"` // fenêtre pendant laquelle agent ET navigateur doivent se connecter, sans quoi la session expire sans jamais devenir active

	AgentConnectedAt  *time.Time `json:"agent_connected_at,omitempty"`
	ViewerConnectedAt *time.Time `json:"viewer_connected_at,omitempty"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	EndedReason       *string    `json:"ended_reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// Repository définit le contrat de persistance des sessions de bureau à
// distance et de leurs jetons d'appariement.
// Implémenté dans internal/infrastructure/persistence/postgres/remotedesktop_repo.go
type Repository interface {
	// Create insère une nouvelle session en statut StatusPending et génère
	// ses deux jetons d'appariement à courte durée de vie (ttl). Remplit
	// s.ID/CreatedAt en retour. Les jetons bruts ne sont jamais stockés tels
	// quels (voir ConsumeViewerToken/ConsumeAgentToken) : à transmettre
	// immédiatement, l'un dans l'URL WS renvoyée au navigateur, l'autre dans
	// la commande LEO_MSG_REMOTE_DESKTOP_START envoyée à l'agent.
	Create(ctx context.Context, s *Session, ttl time.Duration) (viewerToken, agentToken string, err error)

	// ConsumeViewerToken valide rawToken (hash connu, non expiré, jamais
	// encore consommé côté navigateur), marque ViewerConnectedAt, et
	// retourne la session associée. Retourne (nil, nil) si le jeton est
	// invalide/expiré/déjà consommé — jamais d'erreur applicative distincte,
	// pour ne pas révéler à l'appelant (non authentifié par JWT sur cette
	// route) la raison précise du rejet.
	ConsumeViewerToken(ctx context.Context, rawToken string) (*Session, error)

	// ConsumeAgentToken est le pendant côté agent de ConsumeViewerToken.
	ConsumeAgentToken(ctx context.Context, rawToken string) (*Session, error)

	// FindByID retourne une session appartenant au tenant donné, ou nil si absente.
	FindByID(ctx context.Context, tenantID, sessionID string) (*Session, error)

	// ActiveForAgent retourne la session non terminée (pending ou active) la
	// plus récente pour cet agent, ou nil s'il n'y en a aucune — utilisé pour
	// n'autoriser qu'une seule session de bureau à distance à la fois par
	// agent (voir RemoteDesktopHandler.createSession).
	ActiveForAgent(ctx context.Context, agentID string) (*Session, error)

	// MarkEnded transitionne une session vers StatusEnded avec la raison
	// donnée (ex: "viewer_closed", "agent_offline", "pair_timeout",
	// "stopped_by_operator"). Idempotent : ré-appeler sur une session déjà
	// StatusEnded ne fait rien.
	MarkEnded(ctx context.Context, sessionID, reason string) error
}
