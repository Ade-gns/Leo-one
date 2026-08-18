// Tests d'intégration EnrollmentHandler — nécessitent une base Postgres de
// test réelle (voir internal/testutil.TestDB) : EnrollmentHandler.Create/
// List/Delete exécutent des requêtes SQL directement sur *pgxpool.Pool, pas
// via une interface repository mockable (voir agent_handler_test.go pour la
// même limite documentée sur CreateCommand/RevokeCertificate).
package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestEnrollmentHandler_Create_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Create Corp", 10)
	h := NewEnrollmentHandler(pool, nil)

	body := bytes.NewBufferString(`{"label":"srv-01","expires_in_hours":48}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-tokens", body)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body)
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("data manquant ou mal typé : %#v", env)
	}
	token, _ := data["token"].(string)
	if len(token) != 64 {
		t.Errorf("longueur du token = %d, attendu 64 (hex de 32 octets aléatoires)", len(token))
	}

	// Le token brut ne doit jamais être stocké tel quel — seul son hash SHA-256.
	var storedHash, storedLabel string
	err := pool.QueryRow(context.Background(),
		`SELECT token_hash, label FROM enrollment_tokens WHERE tenant_id = $1`, tenantID,
	).Scan(&storedHash, &storedLabel)
	if err != nil {
		t.Fatalf("lecture BDD a échoué : %v", err)
	}
	if storedHash == token {
		t.Error("le token brut ne devrait jamais être stocké tel quel en base (seul son hash)")
	}
	if storedLabel != "srv-01" {
		t.Errorf("label stocké = %q, attendu \"srv-01\"", storedLabel)
	}
}

func TestEnrollmentHandler_Create_DefaultTTL_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Default TTL Corp", 10)
	h := NewEnrollmentHandler(pool, nil)

	// Corps vide : les valeurs par défaut (24h, pas de label) doivent s'appliquer.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-tokens", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var label *string
	var expiresAt time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT label, expires_at FROM enrollment_tokens WHERE tenant_id = $1`, tenantID,
	).Scan(&label, &expiresAt)
	if err != nil {
		t.Fatalf("lecture BDD a échoué : %v", err)
	}
	if label != nil {
		t.Errorf("label = %v, attendu nil (corps vide)", *label)
	}

	ttl := time.Until(expiresAt)
	if ttl < 23*time.Hour || ttl > 25*time.Hour {
		t.Errorf("TTL par défaut = %v, attendu ~24h (defaultEnrollmentTokenTTLHours)", ttl)
	}
}

func TestEnrollmentHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantA := testutil.SeedTenant(t, pool, "List Corp A", 10)
	tenantB := testutil.SeedTenant(t, pool, "List Corp B", 10)
	h := NewEnrollmentHandler(pool, nil)

	createFor := func(tenantID, label string) {
		body := bytes.NewBufferString(`{"label":"` + label + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-tokens", body)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup Create a échoué : %d %s", rec.Code, rec.Body.String())
		}
	}
	createFor(tenantA, "a1")
	createFor(tenantA, "a2")
	createFor(tenantB, "b1")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/enrollment-tokens", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantA))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body)
	items, ok := env["data"].([]any)
	if !ok {
		t.Fatalf("data manquant ou mal typé : %#v", env)
	}
	// Isolation multi-tenant : seuls les 2 tokens du tenant A, jamais celui du tenant B.
	if len(items) != 2 {
		t.Fatalf("nombre de tokens = %d, attendu 2 (isolation tenant)", len(items))
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		if _, hasToken := item["token"]; hasToken {
			t.Error("List ne doit jamais renvoyer le token brut")
		}
	}
}

func TestEnrollmentHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Delete Corp", 10)
	h := NewEnrollmentHandler(pool, nil)

	// Crée un token puis récupère son id via List (pas renvoyé par Create tel quel côté "id" seul, mais on le lit en base directement pour simplicité).
	body := bytes.NewBufferString(`{"label":"to-delete"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-tokens", body)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup Create a échoué : %d %s", rec.Code, rec.Body.String())
	}
	createEnv := decodeEnvelope(t, rec.Body)
	tokenID := createEnv["data"].(map[string]any)["id"].(string)

	t.Run("succès retourne 204 et le token n'apparaît plus en liste", func(t *testing.T) {
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/enrollment-tokens/"+tokenID, nil)
		delReq = delReq.WithContext(httpctx.WithTenantID(delReq.Context(), tenantID))
		delReq = withURLParam(delReq, "tokenID", tokenID)
		delRec := httptest.NewRecorder()

		h.Delete(delRec, delReq)

		if delRec.Code != http.StatusNoContent {
			t.Fatalf("code = %d, attendu %d, body=%s", delRec.Code, http.StatusNoContent, delRec.Body.String())
		}

		var count int
		err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM enrollment_tokens WHERE id = $1`, tokenID,
		).Scan(&count)
		if err != nil {
			t.Fatalf("lecture BDD a échoué : %v", err)
		}
		if count != 0 {
			t.Error("le token révoqué ne devrait plus exister en base")
		}
	})

	t.Run("token déjà supprimé retourne 404", func(t *testing.T) {
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/enrollment-tokens/"+tokenID, nil)
		delReq = delReq.WithContext(httpctx.WithTenantID(delReq.Context(), tenantID))
		delReq = withURLParam(delReq, "tokenID", tokenID)
		delRec := httptest.NewRecorder()

		h.Delete(delRec, delReq)

		if delRec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", delRec.Code, http.StatusNotFound)
		}
	})

	t.Run("token d'un autre tenant retourne 404 (isolation)", func(t *testing.T) {
		otherTenant := testutil.SeedTenant(t, pool, "Other Corp", 10)
		body := bytes.NewBufferString(`{}`)
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/enrollment-tokens", body)
		createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), otherTenant))
		createRec := httptest.NewRecorder()
		h.Create(createRec, createReq)
		otherTokenID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

		// tentative de suppression depuis tenantID (pas otherTenant)
		delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/enrollment-tokens/"+otherTokenID, nil)
		delReq = delReq.WithContext(httpctx.WithTenantID(delReq.Context(), tenantID))
		delReq = withURLParam(delReq, "tokenID", otherTokenID)
		delRec := httptest.NewRecorder()

		h.Delete(delRec, delReq)

		if delRec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d (un tenant ne doit pas pouvoir révoquer le token d'un autre)", delRec.Code, http.StatusNotFound)
		}
	})
}
