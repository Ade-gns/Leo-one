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
	orig := pairTimeout
	pairTimeout = 100 * time.Millisecond
	defer func() { pairTimeout = orig }()

	repo := newFakeRepo()
	relay := NewRelay(repo, testLogger())
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
