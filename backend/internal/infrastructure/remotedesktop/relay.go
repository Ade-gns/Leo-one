// Package remotedesktop porte le relais de bureau à distance : il apparie,
// pour une session donnée, une connexion WebSocket agent (dédiée, ouverte
// sur le listener mTLS suite à une commande REMOTE_DESKTOP_START) et une
// connexion WebSocket navigateur (ouverte publiquement via un jeton signé),
// puis pompe les messages entre les deux tant que la session vit.
//
// Volontairement un package séparé de internal/infrastructure/websocket
// (le Hub agent existant) : le Hub est mono-connexion-par-agent, texte
// JSON, plafonné à 64KB, avec un seul goroutine/canal d'écriture — un choix
// délibéré et correct pour le canal de contrôle (HELLO, métriques,
// commandes), mais impropre à porter un flux vidéo JPEG haute fréquence
// sans risquer d'affamer ou de faire déborder ce canal. Le bureau à distance
// a donc sa propre connexion, son propre relais, indépendants du Hub.
package remotedesktop

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	rdDomain "github.com/yourorg/leo-one/internal/domain/remotedesktop"
)

// Types (premier octet) des messages binaires échangés sur la connexion
// dédiée — doivent correspondre aux constantes agent (voir
// agent/src/remote_desktop.h côté C, phase 2). Le relais ne décode que ce
// premier octet, pour appliquer le filtre mode=view ci-dessous ; le reste du
// message (frame JPEG, coordonnées d'input...) n'est jamais interprété côté
// Go, seulement transmis tel quel.
const (
	wireTypeFrame       = 0x01 // agent -> navigateur
	wireTypeInputMove   = 0x10 // navigateur -> agent
	wireTypeInputButton = 0x11
	wireTypeInputScroll = 0x12
	wireTypeInputKey    = 0x13
	// 0x20 (CONTROL, JSON) est laissé passer sans filtre dans les deux sens.
)

func isInputMessage(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	switch payload[0] {
	case wireTypeInputMove, wireTypeInputButton, wireTypeInputScroll, wireTypeInputKey:
		return true
	default:
		return false
	}
}

// pairTimeout borne le temps d'attente du second côté (agent ou navigateur)
// une fois le premier connecté — au-delà, la session est terminée plutôt que
// de laisser un goroutine/une connexion WS ouverte indéfiniment côté seul.
// Volontairement indépendant du TTL des jetons (qui borne l'attente *avant*
// la première connexion) : une fois un côté effectivement connecté, on ne
// veut pas dépendre d'une horloge d'expiration de jeton déjà consommé.
// var plutôt que const : les tests de ce package raccourcissent ce délai
// pour ne pas attendre 30 secondes réelles (voir relay_test.go).
var pairTimeout = 30 * time.Second

const (
	maxRemoteDesktopMessageBytes = 8 << 20
	remoteDesktopPongWait        = 45 * time.Second
	// remoteDesktopPingPeriod doit rester nettement inférieur à
	// remoteDesktopPongWait (idiome standard gorilla/websocket) : c'est le
	// Pong reçu en réponse à ce Ping qui repousse la deadline de lecture dans
	// SetPongHandler (voir copyOne) — sans émetteur de Ping, aucun Pong
	// n'arrive jamais et la deadline, posée une seule fois, finit par expirer
	// même sur une session saine avec un flux de frames continu (ReadMessage
	// ne renouvelle pas la deadline lui-même).
	remoteDesktopPingPeriod = 20 * time.Second
)

// Relay est le registre des sessions de bureau à distance en cours
// d'appariement ou actives. Thread-safe.
type Relay struct {
	mu            sync.Mutex
	sessions      map[string]*liveSession
	endedSessions map[string]struct{}

	repo   rdDomain.Repository
	logger *slog.Logger
}

type liveSession struct {
	sessionID string
	agentID   string
	mode      string

	agentConn  *websocket.Conn
	viewerConn *websocket.Conn
	ended      bool // protégé par Relay.mu

	endOnce sync.Once
}

// Register réserve une session dès sa création. Cela ferme la fenêtre où un
// STOP pouvait marquer la session terminée en base avant le premier upgrade,
// puis laisser AttachAgent/AttachViewer la remettre en circulation.
func (r *Relay) Register(sess *rdDomain.Session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sessions[sess.ID]; exists {
		return false
	}
	ls := &liveSession{sessionID: sess.ID, agentID: sess.AgentID, mode: sess.Mode}
	r.sessions[sess.ID] = ls
	go r.waitPairTimeout(ls)
	return true
}

// NewRelay crée un Relay.
func NewRelay(repo rdDomain.Repository, logger *slog.Logger) *Relay {
	return &Relay{
		sessions:      make(map[string]*liveSession),
		endedSessions: make(map[string]struct{}),
		repo:          repo,
		logger:        logger,
	}
}

// AttachAgent enregistre la connexion WS dédiée d'un agent pour la session
// résolue (voir remote_desktop_ws_handler.go, qui consomme le jeton AVANT
// d'appeler cette méthode — le relais lui-même ne connaît que des sessions
// déjà authentifiées).
func (r *Relay) AttachAgent(sess *rdDomain.Session, conn *websocket.Conn) {
	r.attach(sess, conn, true)
}

// AttachViewer est le pendant côté navigateur de AttachAgent.
func (r *Relay) AttachViewer(sess *rdDomain.Session, conn *websocket.Conn) {
	r.attach(sess, conn, false)
}

func (r *Relay) attach(sess *rdDomain.Session, conn *websocket.Conn, isAgent bool) {
	r.mu.Lock()
	if _, ended := r.endedSessions[sess.ID]; ended {
		r.mu.Unlock()
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session terminée"),
			time.Now().Add(time.Second))
		conn.Close()
		return
	}
	ls, ok := r.sessions[sess.ID]
	if !ok {
		ls = &liveSession{sessionID: sess.ID, agentID: sess.AgentID, mode: sess.Mode}
		r.sessions[sess.ID] = ls
	}
	if ls.ended {
		r.mu.Unlock()
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "session terminée"),
			time.Now().Add(time.Second))
		conn.Close()
		return
	}

	var duplicate bool
	if isAgent {
		duplicate = ls.agentConn != nil
		if !duplicate {
			ls.agentConn = conn
		}
	} else {
		duplicate = ls.viewerConn != nil
		if !duplicate {
			ls.viewerConn = conn
		}
	}
	ready := ls.agentConn != nil && ls.viewerConn != nil
	r.mu.Unlock()

	if duplicate {
		// Un jeton est à usage unique côté repo (ConsumeAgentToken /
		// ConsumeViewerToken) — ce cas ne devrait donc pas arriver en usage
		// normal. Filet de sécurité si jamais deux connexions concurrentes
		// franchissaient la fenêtre de consommation du jeton.
		_ = conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session déjà appariée"),
			time.Now().Add(time.Second))
		conn.Close()
		return
	}

	if ready {
		go r.pump(ls)
	} else if !ok {
		go r.waitPairTimeout(ls)
	}
}

func (r *Relay) waitPairTimeout(ls *liveSession) {
	time.Sleep(pairTimeout)

	r.mu.Lock()
	stillWaiting := r.sessions[ls.sessionID] == ls && !(ls.agentConn != nil && ls.viewerConn != nil)
	r.mu.Unlock()

	if stillWaiting {
		r.end(ls, "pair_timeout")
	}
}

// pump pompe les messages entre les deux côtés d'une session appariée
// jusqu'à ce que l'un des deux se ferme ou erre.
func (r *Relay) pump(ls *liveSession) {
	log := r.logger.With("session_id", ls.sessionID, "agent_id", ls.agentID, "mode", ls.mode)
	log.Info("Session de bureau à distance active")

	done := make(chan struct{}, 2)
	stop := make(chan struct{})
	defer close(stop)

	copyOne := func(from, to *websocket.Conn, filterInput bool) {
		defer func() { done <- struct{}{} }()
		from.SetReadLimit(maxRemoteDesktopMessageBytes)
		_ = from.SetReadDeadline(time.Now().Add(remoteDesktopPongWait))
		from.SetPongHandler(func(string) error {
			return from.SetReadDeadline(time.Now().Add(remoteDesktopPongWait))
		})
		for {
			msgType, payload, err := from.ReadMessage()
			if err != nil {
				return
			}
			if filterInput && isInputMessage(payload) {
				// Défense en profondeur : le navigateur en mode "view" ne
				// devrait jamais émettre ces messages (voir
				// RemoteDesktopViewer.tsx), mais le relais ne fait confiance
				// à aucun des deux côtés pour respecter le mode par lui-même.
				continue
			}
			if err := to.WriteMessage(msgType, payload); err != nil {
				return
			}
		}
	}

	// WriteControl utilise le même verrou d'écriture interne que
	// WriteMessage (voir gorilla/websocket conn.go) — l'appeler ici pendant
	// qu'une des deux goroutines copyOne écrit potentiellement sur la même
	// connexion (via `to.WriteMessage`) est documenté comme sûr ("The Close
	// and WriteControl methods can be called concurrently with all other
	// methods"), pas de section critique supplémentaire nécessaire.
	pingLoop := func(conn *websocket.Conn) {
		ticker := time.NewTicker(remoteDesktopPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}

	go copyOne(ls.agentConn, ls.viewerConn, false)
	go copyOne(ls.viewerConn, ls.agentConn, ls.mode == rdDomain.ModeView)
	go pingLoop(ls.agentConn)
	go pingLoop(ls.viewerConn)

	<-done // le premier sens à s'arrêter termine toute la session

	r.end(ls, "connection_closed")
}

// end termine une session : ferme les deux connexions, la retire du
// registre, et persiste la fin côté BDD. Sûr à appeler plusieurs fois
// concurremment (depuis pump, waitPairTimeout, ou EndSessionsForAgent) —
// seul le premier appel a un effet.
func (r *Relay) end(ls *liveSession, reason string) {
	ls.endOnce.Do(func() {
		r.mu.Lock()
		if r.sessions[ls.sessionID] == ls {
			delete(r.sessions, ls.sessionID)
		}
		ls.ended = true
		r.endedSessions[ls.sessionID] = struct{}{}
		r.mu.Unlock()
		// Les identifiants sont des UUID à usage unique ; conserver un court
		// tombstone ferme la course stop/upgrade sans faire croître la map à
		// l'infini sur un serveur de longue durée.
		time.AfterFunc(5*time.Minute, func() {
			r.mu.Lock()
			delete(r.endedSessions, ls.sessionID)
			r.mu.Unlock()
		})

		if ls.agentConn != nil {
			ls.agentConn.Close()
		}
		if ls.viewerConn != nil {
			ls.viewerConn.Close()
		}

		if err := r.repo.MarkEnded(context.Background(), ls.sessionID, reason); err != nil {
			r.logger.Warn("Échec marquage fin de session bureau à distance",
				"session_id", ls.sessionID, "error", err)
		}
		r.logger.Info("Session de bureau à distance terminée",
			"session_id", ls.sessionID, "agent_id", ls.agentID, "reason", reason)
	})
}

// EndSession termine de force une session précise si le relais la connaît
// (en attente d'appariement ou active) — utilisé par
// RemoteDesktopHandler.StopSession (arrêt explicite demandé par
// l'opérateur). Retourne false si la session est inconnue du relais (déjà
// terminée, ou jamais appariée côté agent) : dans ce cas l'appelant doit
// marquer la fin directement en base (le relais n'a rien à fermer).
func (r *Relay) EndSession(sessionID, reason string) bool {
	r.mu.Lock()
	ls, ok := r.sessions[sessionID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	r.end(ls, reason)
	return true
}

// EndSessionsForAgent termine de force toute session en cours (en attente
// d'appariement ou active) pour cet agent — appelé quand l'agent se
// déconnecte du canal de contrôle (voir websocket.Dispatcher.MarkOffline) :
// sans cela, une session de bureau à distance resterait ouverte côté
// navigateur (ou en attente indéfinie d'un agent qui ne se (re)connectera
// plus sur cette session précise) après que l'agent est passé hors ligne.
func (r *Relay) EndSessionsForAgent(agentID string) {
	r.mu.Lock()
	var toEnd []*liveSession
	for _, ls := range r.sessions {
		if ls.agentID == agentID {
			toEnd = append(toEnd, ls)
		}
	}
	r.mu.Unlock()

	for _, ls := range toEnd {
		r.end(ls, "agent_offline")
	}
}
