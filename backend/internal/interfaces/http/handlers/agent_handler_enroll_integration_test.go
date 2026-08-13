// Tests d'intégration AgentHandler.Enroll et RevokeCertificate — nécessitent
// une base Postgres de test réelle (voir internal/testutil.TestDB).
//
// Enroll fait des requêtes SQL directement sur *pgxpool.Pool (lookup/marquage
// du token d'enrollment) en plus de h.agentRepo.Create, donc pas mockable
// sans base réelle — voir enrollment_handler_integration_test.go pour la
// même limite côté EnrollmentHandler.
package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	leoWS "github.com/yourorg/leo-one/internal/infrastructure/websocket"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
	"github.com/yourorg/leo-one/pkg/pki"
)

func newTestCA(t *testing.T) *pki.CA {
	t.Helper()
	dir := t.TempDir()
	ca, err := pki.EnsureCA(filepath.Join(dir, "ca-cert.pem"), filepath.Join(dir, "ca-key.pem"))
	if err != nil {
		t.Fatalf("pki.EnsureCA a échoué : %v", err)
	}
	return ca
}

func newTestHub(pool *pgxpool.Pool) *leoWS.Hub {
	log := slog.New(slog.NewTextHandler(nopWriter{}, nil))
	dispatcher := leoWS.NewDispatcher(
		postgres.NewAgentRepo(pool),
		postgres.NewMetricRepo(pool),
		postgres.NewInventoryRepo(pool),
		pool, log,
	)
	hub := leoWS.NewHub(dispatcher, log)
	dispatcher.SetHub(hub)
	return hub
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// createRawToken génère un token d'enrollment via EnrollmentHandler.Create
// (même chemin que la production) et retourne sa valeur brute.
func createRawToken(t *testing.T, pool *pgxpool.Pool, tenantID string) string {
	t.Helper()
	h := NewEnrollmentHandler(pool)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-tokens", bytes.NewBufferString(`{}`))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("createRawToken : Create a échoué : %d %s", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("createRawToken : réponse illisible : %v", err)
	}
	return env["data"].(map[string]any)["token"].(string)
}

// insertRawToken insère un token d'enrollment directement en base, pour les
// scénarios (expiré, déjà utilisé) que l'API ne permet pas de produire.
func insertRawToken(t *testing.T, pool *pgxpool.Pool, tenantID string, expiresAt time.Time, usedAt *time.Time) string {
	t.Helper()
	raw := "test-token-" + tenantID + "-" + expiresAt.String()
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	_, err := pool.Exec(context.Background(), `
		INSERT INTO enrollment_tokens (id, tenant_id, token_hash, expires_at, used_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
	`, tenantID, hash, expiresAt, usedAt)
	if err != nil {
		t.Fatalf("insertRawToken a échoué : %v", err)
	}
	return raw
}

func enrollRequestBody(token, hostname, hardwareID string) *bytes.Buffer {
	body, _ := json.Marshal(map[string]any{
		"enrollment_token": token,
		"hostname":         hostname,
		"os":               "linux",
		"os_version":       "24.04",
		"arch":             "amd64",
		"hardware_id":      hardwareID,
		"agent_version":    "1.0.0",
	})
	return bytes.NewBuffer(body)
}

func TestAgentHandler_Enroll_Success_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Enroll Corp", 10)
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "deadbeef", "wss://test/ws/agent")

	token := createRawToken(t, pool, tenantID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token, "srv-enroll-01", "hw-success-1"))
	rec := httptest.NewRecorder()

	h.Enroll(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body)
	data := env["data"].(map[string]any)

	agentID, _ := data["agent_id"].(string)
	if agentID == "" {
		t.Fatal("agent_id manquant dans la réponse")
	}
	if data["tenant_id"] != tenantID {
		t.Errorf("tenant_id = %v, attendu %q", data["tenant_id"], tenantID)
	}
	if data["server_cert_fingerprint"] != "deadbeef" {
		t.Errorf("server_cert_fingerprint = %v, attendu \"deadbeef\"", data["server_cert_fingerprint"])
	}
	certPEM, _ := data["client_cert_pem"].(string)
	if certPEM == "" {
		t.Error("client_cert_pem manquant")
	}

	// L'agent doit exister en base, dans le bon tenant.
	var status string
	err := pool.QueryRow(context.Background(), `SELECT status FROM agents WHERE id = $1 AND tenant_id = $2`, agentID, tenantID).Scan(&status)
	if err != nil {
		t.Fatalf("l'agent n'a pas été créé en base : %v", err)
	}
	if status != string(agentDomain.StatusOffline) {
		t.Errorf("statut initial = %q, attendu %q", status, agentDomain.StatusOffline)
	}

	// Le certificat doit être tracé dans agent_certificates (pour la
	// révocation — voir RevokeCertificate/AgentWSHandler.checkNotRevoked).
	var certCount int
	err = pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_certificates WHERE agent_id = $1 AND revoked_at IS NULL`, agentID).Scan(&certCount)
	if err != nil {
		t.Fatalf("lecture agent_certificates a échoué : %v", err)
	}
	if certCount != 1 {
		t.Errorf("nombre de certificats actifs = %d, attendu 1", certCount)
	}

	// Le token doit être marqué utilisé.
	var usedBy *string
	err = pool.QueryRow(context.Background(), `SELECT used_by FROM enrollment_tokens WHERE tenant_id = $1`, tenantID).Scan(&usedBy)
	if err != nil {
		t.Fatalf("lecture enrollment_tokens a échoué : %v", err)
	}
	if usedBy == nil || *usedBy != agentID {
		t.Errorf("used_by = %v, attendu %q", usedBy, agentID)
	}
}

func TestAgentHandler_Enroll_UnknownToken_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	testutil.SeedTenant(t, pool, "Unknown Token Corp", 10)
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "fp", "wss://test/ws/agent")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody("token-inexistant", "srv", "hw-unknown"))
	rec := httptest.NewRecorder()

	h.Enroll(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAgentHandler_Enroll_ExpiredToken_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Expired Token Corp", 10)
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "fp", "wss://test/ws/agent")

	token := insertRawToken(t, pool, tenantID, time.Now().Add(-time.Hour), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token, "srv", "hw-expired"))
	rec := httptest.NewRecorder()

	h.Enroll(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body)
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "ENROLLMENT_TOKEN_EXPIRED" {
		t.Errorf("code d'erreur = %v, attendu ENROLLMENT_TOKEN_EXPIRED", errObj["code"])
	}
}

func TestAgentHandler_Enroll_UsedToken_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Used Token Corp", 10)
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "fp", "wss://test/ws/agent")

	token := createRawToken(t, pool, tenantID)

	// Premier enrollment : succès.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token, "srv-1", "hw-used-1"))
	rec1 := httptest.NewRecorder()
	h.Enroll(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("premier enrollment a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	// Réutilisation du même token : doit être rejetée.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token, "srv-2", "hw-used-2"))
	rec2 := httptest.NewRecorder()
	h.Enroll(rec2, req2)

	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu %d, body=%s", rec2.Code, http.StatusUnauthorized, rec2.Body.String())
	}
	env := decodeEnvelope(t, rec2.Body)
	errObj := env["error"].(map[string]any)
	if errObj["code"] != "ENROLLMENT_TOKEN_USED" {
		t.Errorf("code d'erreur = %v, attendu ENROLLMENT_TOKEN_USED", errObj["code"])
	}
}

func TestAgentHandler_Enroll_DuplicateHardwareID_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Dup HW Corp", 10)
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "fp", "wss://test/ws/agent")

	token1 := createRawToken(t, pool, tenantID)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token1, "srv-1", "hw-dup"))
	rec1 := httptest.NewRecorder()
	h.Enroll(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("premier enrollment a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	token2 := createRawToken(t, pool, tenantID)
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token2, "srv-2", "hw-dup"))
	rec2 := httptest.NewRecorder()
	h.Enroll(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("code = %d, attendu %d, body=%s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

func TestAgentHandler_Enroll_QuotaReached_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Quota Corp", 1) // max_agents = 1
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "fp", "wss://test/ws/agent")

	token1 := createRawToken(t, pool, tenantID)
	rec1 := httptest.NewRecorder()
	h.Enroll(rec1, httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token1, "srv-1", "hw-quota-1")))
	if rec1.Code != http.StatusCreated {
		t.Fatalf("premier enrollment (dans le quota) a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	token2 := createRawToken(t, pool, tenantID)
	rec2 := httptest.NewRecorder()
	h.Enroll(rec2, httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token2, "srv-2", "hw-quota-2")))

	if rec2.Code != http.StatusForbidden {
		t.Fatalf("code = %d, attendu %d (quota d'agents atteint), body=%s", rec2.Code, http.StatusForbidden, rec2.Body.String())
	}
}

func TestAgentHandler_RevokeCertificate_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Revoke Corp", 10)
	ca := newTestCA(t)
	h := NewAgentHandler(postgres.NewAgentRepo(pool), pool, newTestHub(pool), ca, "fp", "wss://test/ws/agent")

	// Enrôle un agent pour obtenir un certificat actif à révoquer.
	token := createRawToken(t, pool, tenantID)
	enrollRec := httptest.NewRecorder()
	h.Enroll(enrollRec, httptest.NewRequest(http.MethodPost, "/api/v1/enroll", enrollRequestBody(token, "srv-revoke", "hw-revoke")))
	if enrollRec.Code != http.StatusCreated {
		t.Fatalf("setup enrollment a échoué : %d %s", enrollRec.Code, enrollRec.Body.String())
	}
	agentID := decodeEnvelope(t, enrollRec.Body)["data"].(map[string]any)["agent_id"].(string)

	t.Run("succès révoque et retourne revoked_count=1", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agentID+"/certificate", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "agentID", agentID)
		rec := httptest.NewRecorder()

		h.RevokeCertificate(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		env := decodeEnvelope(t, rec.Body)
		if env["data"].(map[string]any)["revoked_count"].(float64) != 1 {
			t.Errorf("revoked_count = %v, attendu 1", env["data"].(map[string]any)["revoked_count"])
		}

		var revokedAt *time.Time
		err := pool.QueryRow(context.Background(), `SELECT revoked_at FROM agent_certificates WHERE agent_id = $1`, agentID).Scan(&revokedAt)
		if err != nil {
			t.Fatalf("lecture BDD a échoué : %v", err)
		}
		if revokedAt == nil {
			t.Error("revoked_at devrait être renseigné après révocation")
		}

		// L'agent lui-même doit toujours exister (contrairement à Delete).
		var count int
		pool.QueryRow(context.Background(), `SELECT count(*) FROM agents WHERE id = $1`, agentID).Scan(&count)
		if count != 1 {
			t.Error("RevokeCertificate ne devrait jamais supprimer l'agent")
		}
	})

	t.Run("révocation répétée est idempotente (revoked_count=0)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agentID+"/certificate", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "agentID", agentID)
		rec := httptest.NewRecorder()

		h.RevokeCertificate(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}
		env := decodeEnvelope(t, rec.Body)
		if env["data"].(map[string]any)["revoked_count"].(float64) != 0 {
			t.Errorf("revoked_count = %v, attendu 0 (déjà révoqué)", env["data"].(map[string]any)["revoked_count"])
		}
	})

	t.Run("agent d'un autre tenant retourne 404 (isolation)", func(t *testing.T) {
		otherTenant := testutil.SeedTenant(t, pool, "Revoke Other Corp", 10)
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/"+agentID+"/certificate", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), otherTenant))
		req = withURLParam(req, "agentID", agentID)
		rec := httptest.NewRecorder()

		h.RevokeCertificate(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}
