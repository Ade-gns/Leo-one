package remotedesktop

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	rdDomain "github.com/yourorg/leo-one/internal/domain/remotedesktop"
)

// fakeRepo est un rdDomain.Repository minimal en mémoire — seul MarkEnded
// est réellement exercé par Relay (AttachAgent/AttachViewer reçoivent des
// *Session déjà résolues par l'appelant, voir le commentaire en tête de
// remote_desktop_ws_handler.go/remotedesktop_handler.go).
type fakeRepo struct {
	mu    sync.Mutex
	ended map[string]string
}

func newFakeRepo() *fakeRepo { return &fakeRepo{ended: make(map[string]string)} }

func (f *fakeRepo) Create(context.Context, *rdDomain.Session, time.Duration) (string, string, error) {
	return "", "", nil
}
func (f *fakeRepo) ConsumeViewerToken(context.Context, string) (*rdDomain.Session, error) {
	return nil, nil
}
func (f *fakeRepo) ConsumeAgentToken(context.Context, string) (*rdDomain.Session, error) {
	return nil, nil
}
func (f *fakeRepo) FindByID(context.Context, string, string) (*rdDomain.Session, error) {
	return nil, nil
}
func (f *fakeRepo) ActiveForAgent(context.Context, string) (*rdDomain.Session, error) {
	return nil, nil
}

func (f *fakeRepo) MarkEnded(_ context.Context, sessionID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended[sessionID] = reason
	return nil
}

func (f *fakeRepo) reasonFor(id string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.ended[id]
	return r, ok
}

var testUpgrader = websocket.Upgrader{}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer expose deux routes qui rejouent exactement ce que font
// RemoteDesktopWSHandler.ServeHTTP (agent) et
// RemoteDesktopHandler.ServeViewerWS (navigateur) une fois le jeton déjà
// résolu en *rdDomain.Session — la résolution du jeton elle-même est testée
// séparément (voir remotedesktop_repo_test.go), pas ici.
func newTestServer(t *testing.T, relay *Relay) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/agent", func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade agent : %v", err)
			return
		}
		relay.AttachAgent(sessionFromQuery(r), conn)
	})
	mux.HandleFunc("/viewer", func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade viewer : %v", err)
			return
		}
		relay.AttachViewer(sessionFromQuery(r), conn)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func sessionFromQuery(r *http.Request) *rdDomain.Session {
	q := r.URL.Query()
	return &rdDomain.Session{ID: q.Get("id"), AgentID: q.Get("agent_id"), Mode: q.Get("mode")}
}

func dialWS(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s : %v", path, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestRelay_PairsAndForwardsMessagesBothWays(t *testing.T) {
	relay := NewRelay(newFakeRepo(), testLogger())
	srv := newTestServer(t, relay)

	agentConn := dialWS(t, srv, "/agent?id=sess-1&agent_id=agent-1&mode=control")
	viewerConn := dialWS(t, srv, "/viewer?id=sess-1&agent_id=agent-1&mode=control")

	frame := []byte{wireTypeFrame, 'h', 'i'}
	if err := agentConn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatal(err)
	}
	viewerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got, err := viewerConn.ReadMessage()
	if err != nil {
		t.Fatalf("le navigateur n'a pas reçu la frame : %v", err)
	}
	if !bytes.Equal(got, frame) {
		t.Fatalf("frame altérée en transit : %v", got)
	}

	// Mode "control" : un message d'input navigateur -> agent doit passer.
	input := []byte{wireTypeInputMove, 0, 0, 0, 0}
	if err := viewerConn.WriteMessage(websocket.BinaryMessage, input); err != nil {
		t.Fatal(err)
	}
	agentConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got2, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("l'agent n'a pas reçu l'input : %v", err)
	}
	if !bytes.Equal(got2, input) {
		t.Fatalf("input altéré en transit : %v", got2)
	}
}

func TestRelay_ViewModeDropsInputMessages(t *testing.T) {
	relay := NewRelay(newFakeRepo(), testLogger())
	srv := newTestServer(t, relay)

	agentConn := dialWS(t, srv, "/agent?id=sess-2&agent_id=agent-2&mode=view")
	viewerConn := dialWS(t, srv, "/viewer?id=sess-2&agent_id=agent-2&mode=view")

	// Message d'input : doit être filtré par le relais, jamais livré.
	if err := viewerConn.WriteMessage(websocket.BinaryMessage, []byte{wireTypeInputKey, 1}); err != nil {
		t.Fatal(err)
	}
	// Message autorisé (CONTROL, 0x20) envoyé juste après : sert de témoin —
	// s'il arrive, on sait que le pump tourne et que l'input a bien été
	// filtré plutôt que perdu pour une autre raison (connexion cassée...).
	allowed := []byte{0x20, '{', '}'}
	if err := viewerConn.WriteMessage(websocket.BinaryMessage, allowed); err != nil {
		t.Fatal(err)
	}

	agentConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got, err := agentConn.ReadMessage()
	if err != nil {
		t.Fatalf("l'agent aurait dû recevoir le message autorisé : %v", err)
	}
	if !bytes.Equal(got, allowed) {
		t.Fatalf("l'agent a reçu %v en premier — l'input aurait dû être filtré avant lui", got)
	}
}

func TestRelay_EndSessionsForAgent_ClosesConnectionsAndMarksEnded(t *testing.T) {
	repo := newFakeRepo()
	relay := NewRelay(repo, testLogger())
	srv := newTestServer(t, relay)

	dialWS(t, srv, "/agent?id=sess-3&agent_id=agent-3&mode=control")
	viewerConn := dialWS(t, srv, "/viewer?id=sess-3&agent_id=agent-3&mode=control")

	// Laisser le temps au pump de démarrer côté serveur avant de couper.
	time.Sleep(100 * time.Millisecond)

	relay.EndSessionsForAgent("agent-3")

	viewerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := viewerConn.ReadMessage(); err == nil {
		t.Fatal("le navigateur aurait dû voir sa connexion se fermer")
	}

	reason, ok := repo.reasonFor("sess-3")
	if !ok || reason != "agent_offline" {
		t.Fatalf("MarkEnded attendu avec reason=agent_offline, obtenu (%q, %v)", reason, ok)
	}
}

func TestRelay_PairTimeout_EndsUnpairedSession(t *testing.T) {
	repo := newFakeRepo()
	relay := NewRelay(repo, testLogger())
	relay.pairTimeout = 100 * time.Millisecond
	srv := newTestServer(t, relay)

	agentConn := dialWS(t, srv, "/agent?id=sess-4&agent_id=agent-4&mode=control")

	agentConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := agentConn.ReadMessage(); err == nil {
		t.Fatal("l'agent aurait dû voir sa connexion se fermer après le délai d'appariement")
	}

	reason, ok := repo.reasonFor("sess-4")
	if !ok || reason != "pair_timeout" {
		t.Fatalf("MarkEnded attendu avec reason=pair_timeout, obtenu (%q, %v)", reason, ok)
	}
}

// TestRelay_KeepAlivePersistsBeyondPongWait prouve que le correctif de
// deadline (émetteur de Ping périodique, voir pingLoop dans relay.go) tient
// sur la durée, pas seulement au démarrage de la session. Sans émetteur de
// Ping, la deadline de lecture posée une seule fois par copyOne au début de
// la session expirerait au bout de pongWait, MÊME avec un flux de frames
// continu — ReadMessage ne renouvelle jamais la deadline lui-même, seul un
// Pong reçu (via SetPongHandler) le fait. pongWait/pingPeriod sont
// raccourcis ici uniquement pour ne pas attendre 45 secondes réelles ; le
// mécanisme exercé (ping périodique -> pong automatique du pair -> deadline
// repoussée) est identique à celui de production.
func TestRelay_KeepAlivePersistsBeyondPongWait(t *testing.T) {
	repo := newFakeRepo()
	relay := NewRelay(repo, testLogger())
	relay.pongWait = 200 * time.Millisecond
	relay.pingPeriod = 50 * time.Millisecond
	srv := newTestServer(t, relay)

	agentConn := dialWS(t, srv, "/agent?id=sess-5&agent_id=agent-5&mode=view")
	viewerConn := dialWS(t, srv, "/viewer?id=sess-5&agent_id=agent-5&mode=view")

	// Un vrai client WS (agent C ou navigateur) lit en continu, ce qui est
	// ce qui lui permet de répondre automatiquement (au niveau protocole,
	// géré par gorilla/websocket) aux Ping reçus. Sans ce lecteur en arrière-
	// plan côté agent, le Pong ne serait jamais émis et le test échouerait
	// pour une raison indépendante du correctif testé.
	go func() {
		for {
			if _, _, err := agentConn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// 4.5x pongWait : sans le correctif, la deadline expirerait dès le
	// premier pongWait écoulé et la connexion serait coupée bien avant la
	// fin de cet envoi.
	const sendDuration = 900 * time.Millisecond
	stopSending := make(chan struct{})
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		seq := byte(0)
		for {
			select {
			case <-stopSending:
				return
			case <-ticker.C:
				frame := []byte{wireTypeFrame, seq}
				if err := agentConn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					return
				}
				seq++
			}
		}
	}()
	time.AfterFunc(sendDuration, func() { close(stopSending) })
	go func() {
		<-sendDone
		time.Sleep(100 * time.Millisecond) // laisse les dernières frames en vol arriver côté viewer
		agentConn.Close()                  // termine promptement le pump plutôt que d'attendre la deadline de lecture
	}()

	viewerConn.SetReadDeadline(time.Now().Add(sendDuration + 2*time.Second))
	start := time.Now()
	received := 0
	sawFrameAfterMultiplePongWaits := false
	for {
		_, _, err := viewerConn.ReadMessage()
		if err != nil {
			break
		}
		received++
		if time.Since(start) > 3*relay.pongWait {
			sawFrameAfterMultiplePongWaits = true
		}
	}
	<-sendDone

	// ~45 frames attendues sur 900ms à un envoi/20ms ; on garde une marge
	// large pour ne pas rendre le test sensible au timing du CI.
	if received < 20 {
		t.Fatalf("trop peu de frames reçues (%d) — la connexion semble avoir été coupée prématurément (le correctif de keepalive ne tiendrait pas)", received)
	}
	if !sawFrameAfterMultiplePongWaits {
		t.Fatalf("aucune frame reçue après 3x pongWait (%v écoulés) — le correctif de deadline ne tient pas dans la durée", 3*relay.pongWait)
	}
}
