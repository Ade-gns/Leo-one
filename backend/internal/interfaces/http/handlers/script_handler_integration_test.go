// Tests d'intégration ScriptHandler.List/Create/Update/Delete — nécessitent
// une base Postgres de test réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestScriptHandler_Create_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Script Create Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "script-create@example.com")
	h := NewScriptHandler(pool, nil)

	body, _ := json.Marshal(map[string]any{
		"name": "Nettoyage disque", "interpreter": "bash", "content": "echo hi",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["name"] != "Nettoyage disque" || data["interpreter"] != "bash" {
		t.Errorf("script créé inattendu : %v", data)
	}
}

func TestScriptHandler_Create_InvalidInterpreter_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Script Bad Interp Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "script-badinterp@example.com")
	h := NewScriptHandler(pool, nil)

	body, _ := json.Marshal(map[string]any{
		"name": "X", "interpreter": "ruby", "content": "puts 1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestScriptHandler_Create_DuplicateName_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Script Dup Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "script-dup@example.com")
	h := NewScriptHandler(pool, nil)

	body, _ := json.Marshal(map[string]any{"name": "Doublon", "interpreter": "bash", "content": "echo 1"})

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
	req1 = req1.WithContext(httpctx.WithTenantID(req1.Context(), tenantID))
	req1 = req1.WithContext(httpctx.WithUserID(req1.Context(), userID))
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("setup a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
	req2 = req2.WithContext(httpctx.WithTenantID(req2.Context(), tenantID))
	req2 = req2.WithContext(httpctx.WithUserID(req2.Context(), userID))
	h.Create(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("code = %d, attendu %d, body=%s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

func TestScriptHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Script List Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "script-list@example.com")
	h := NewScriptHandler(pool, nil)

	for _, name := range []string{"Script A", "Script B"} {
		body, _ := json.Marshal(map[string]any{"name": name, "interpreter": "python", "content": "print(1)"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup a échoué : %d %s", rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/scripts", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(data) != 2 {
		t.Errorf("len(data) = %d, attendu 2", len(data))
	}
}

func TestScriptHandler_Update_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Script Update Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "script-update@example.com")
	h := NewScriptHandler(pool, nil)

	createBody, _ := json.Marshal(map[string]any{"name": "Original", "interpreter": "bash", "content": "echo 1"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createReq = createReq.WithContext(httpctx.WithUserID(createReq.Context(), userID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	scriptID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("modifie content", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"content": "echo 2"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/scripts/"+scriptID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "scriptID", scriptID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["content"] != "echo 2" || data["name"] != "Original" {
			t.Errorf("script mis à jour inattendu : %v", data)
		}
	})

	t.Run("script introuvable retourne 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"content": "x"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/scripts/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "scriptID", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestScriptHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Script Delete Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "script-delete@example.com")
	h := NewScriptHandler(pool, nil)

	createBody, _ := json.Marshal(map[string]any{"name": "À supprimer", "interpreter": "bash", "content": "echo 1"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createReq = createReq.WithContext(httpctx.WithUserID(createReq.Context(), userID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	scriptID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/scripts/"+scriptID, nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = withURLParam(req, "scriptID", scriptID)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}
