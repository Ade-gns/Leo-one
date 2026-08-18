// Tests d'intégration PatchHandler — nécessitent une base Postgres de test
// réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	patchDomain "github.com/yourorg/leo-one/internal/domain/patch"
	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestPatchHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "PatchH List Corp", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "list-01")
	patchRepo := postgres.NewPatchRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewPatchHandler(patchRepo, agentRepo, agentHandler, pool, nil)

	if err := patchRepo.Upsert(context.Background(), tenantID, agentID, []patchDomain.Report{
		{NativeID: "bash", Title: "bash → 5.1.1", Severity: patchDomain.SeverityImportant},
	}); err != nil {
		t.Fatalf("setup Upsert a échoué : %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/"+agentID+"/patches", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = withURLParam(req, "agentID", agentID)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("len(data) = %d, attendu 1", len(data))
	}
}

func TestPatchHandler_Install_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "PatchH Install Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "patch-install@example.com")
	agentID := testutil.SeedAgent(t, pool, tenantID, "install-01")
	patchRepo := postgres.NewPatchRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewPatchHandler(patchRepo, agentRepo, agentHandler, pool, nil)

	t.Run("patch_ids manquant retourne 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/patches/install", bytes.NewReader([]byte(`{}`)))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
		req = withURLParam(req, "agentID", agentID)
		rec := httptest.NewRecorder()

		h.Install(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("agent introuvable retourne 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"patch_ids": []string{"bash"}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/00000000-0000-0000-0000-000000000000/patches/install", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
		req = withURLParam(req, "agentID", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		h.Install(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("succès crée une commande install_patches", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"patch_ids": []string{"bash", "curl"}, "reboot_after": true})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+agentID+"/patches/install", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
		req = withURLParam(req, "agentID", agentID)
		rec := httptest.NewRecorder()

		h.Install(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["command_id"] == nil || data["command_id"] == "" {
			t.Errorf("command_id manquant dans la réponse : %v", data)
		}

		var cmdType, payload string
		err := pool.QueryRow(context.Background(),
			`SELECT type::text, payload::text FROM commands WHERE id = $1`, data["command_id"]).Scan(&cmdType, &payload)
		if err != nil {
			t.Fatalf("lecture de la commande créée a échoué : %v", err)
		}
		if cmdType != "install_patches" {
			t.Errorf("type = %s, attendu install_patches", cmdType)
		}
	})
}

func TestPatchHandler_BulkInstall_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "PatchH Bulk Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "patch-bulk@example.com")
	agentA := testutil.SeedAgent(t, pool, tenantID, "bulk-a")
	agentB := testutil.SeedAgent(t, pool, tenantID, "bulk-b")
	patchRepo := postgres.NewPatchRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewPatchHandler(patchRepo, agentRepo, agentHandler, pool, nil)

	// agentA a un patch disponible, agentB n'en a aucun.
	if err := patchRepo.Upsert(context.Background(), tenantID, agentA, []patchDomain.Report{
		{NativeID: "bash", Title: "bash", Severity: patchDomain.SeverityCritical},
	}); err != nil {
		t.Fatalf("setup Upsert a échoué : %v", err)
	}

	body, _ := json.Marshal(map[string]any{"agent_ids": []string{agentA, agentB}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/bulk-patches/install", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.BulkInstall(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	results := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, attendu 2", len(results))
	}

	byAgent := map[string]map[string]any{}
	for _, raw := range results {
		r := raw.(map[string]any)
		byAgent[r["agent_id"].(string)] = r
	}

	if byAgent[agentA]["command_id"] == nil || byAgent[agentA]["command_id"] == "" {
		t.Errorf("agentA aurait dû recevoir une commande : %v", byAgent[agentA])
	}
	if byAgent[agentB]["error"] == nil || byAgent[agentB]["error"] == "" {
		t.Errorf("agentB (aucun patch disponible) aurait dû retourner une erreur : %v", byAgent[agentB])
	}
}

func TestPatchHandler_BulkInstall_RequiresExactlyOneTarget_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	patchRepo := postgres.NewPatchRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewPatchHandler(patchRepo, agentRepo, agentHandler, pool, nil)

	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/bulk-patches/install", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	h.BulkInstall(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPatchHandler_Summary_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "PatchH Summary Corp", 10)
	agentID := testutil.SeedAgent(t, pool, tenantID, "summary-01")
	patchRepo := postgres.NewPatchRepo(pool)
	agentRepo := postgres.NewAgentRepo(pool)
	agentHandler := NewAgentHandler(agentRepo, pool, newTestHub(pool), nil, "", "", nil)
	h := NewPatchHandler(patchRepo, agentRepo, agentHandler, pool, nil)

	if err := patchRepo.Upsert(context.Background(), tenantID, agentID, []patchDomain.Report{
		{NativeID: "bash", Title: "bash", Severity: patchDomain.SeverityCritical},
	}); err != nil {
		t.Fatalf("setup Upsert a échoué : %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/patches/summary", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Summary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["agents_with_critical_pending"].(float64) != 1 {
		t.Errorf("agents_with_critical_pending = %v, attendu 1", data["agents_with_critical_pending"])
	}
}
