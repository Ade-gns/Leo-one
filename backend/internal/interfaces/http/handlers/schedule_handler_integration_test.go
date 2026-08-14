// Tests d'intégration ScheduleHandler.List/Create/Update/Delete —
// nécessitent une base Postgres de test réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

// seedScript crée un script minimal via ScriptHandler.Create et retourne son
// ID. Prend pool en paramètre (plutôt que de rappeler testutil.TestDB, qui
// tronque les tables à chaque appel — un second appel aurait effacé le
// tenant/utilisateur déjà créés par l'appelant).
func seedScript(t *testing.T, pool *pgxpool.Pool, tenantID, userID string) string {
	t.Helper()
	h := NewScriptHandler(pool)

	body, _ := json.Marshal(map[string]any{"name": "Script pour planif", "interpreter": "bash", "content": "echo 1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seedScript : setup a échoué : %d %s", rec.Code, rec.Body.String())
	}
	return decodeEnvelope(t, rec.Body)["data"].(map[string]any)["id"].(string)
}

func TestScheduleHandler_Create_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Create Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-create@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-create-agent")
	h := NewScheduleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "Nettoyage nocturne",
		"agent_id": agentID, "cron_expression": "0 2 * * *",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["name"] != "Nettoyage nocturne" {
		t.Errorf("name = %v", data["name"])
	}
	if data["next_run_at"] == nil || data["next_run_at"] == "" {
		t.Errorf("next_run_at aurait dû être calculé, obtenu %v", data["next_run_at"])
	}
	if data["timeout_sec"].(float64) != 60 {
		t.Errorf("timeout_sec = %v, attendu 60 (défaut)", data["timeout_sec"])
	}
}

func TestScheduleHandler_Create_RunAt_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule RunAt Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-runat@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-runat-agent")
	h := NewScheduleHandler(pool)

	runAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "Exécution ponctuelle",
		"agent_id": agentID, "run_at": runAt.Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["cron_expression"] != nil {
		t.Errorf("cron_expression = %v, attendu nil (planification ponctuelle)", data["cron_expression"])
	}
	gotNextRunAt, err := time.Parse(time.RFC3339, data["next_run_at"].(string))
	if err != nil {
		t.Fatalf("next_run_at illisible : %v", data["next_run_at"])
	}
	if !gotNextRunAt.Equal(runAt) {
		t.Errorf("next_run_at = %v, attendu %v (= run_at)", gotNextRunAt, runAt)
	}
}

func TestScheduleHandler_Create_RunAtInPast_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule RunAt Past Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-runatpast@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-runatpast-agent")
	h := NewScheduleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "X",
		"agent_id": agentID, "run_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d (run_at dans le passé), body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestScheduleHandler_Create_CronAndRunAtBoth_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Cron And RunAt Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-cronandrunat@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-cronandrunat-agent")
	h := NewScheduleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "X", "agent_id": agentID,
		"cron_expression": "0 2 * * *", "run_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d (cron_expression ET run_at fournis), body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestScheduleHandler_Create_NeitherCronNorRunAt_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Neither Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-neither@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-neither-agent")
	h := NewScheduleHandler(pool)

	body, _ := json.Marshal(map[string]any{"script_id": scriptID, "name": "X", "agent_id": agentID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d (ni cron_expression ni run_at), body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestScheduleHandler_Create_InvalidCron_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Bad Cron Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-badcron@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	h := NewScheduleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "X",
		"agent_id": "11111111-1111-1111-1111-111111111111", "cron_expression": "pas un cron",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestScheduleHandler_Create_BothTargets_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Both Targets Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-bothtargets@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	h := NewScheduleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "X",
		"agent_id": "11111111-1111-1111-1111-111111111111", "workspace_id": "22222222-2222-2222-2222-222222222222",
		"cron_expression": "0 2 * * *",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d (agent_id ET workspace_id fournis), body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestScheduleHandler_Update_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Update Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-update@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-update-agent")
	h := NewScheduleHandler(pool)

	createBody, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "Initiale",
		"agent_id": agentID, "cron_expression": "0 2 * * *",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createReq = createReq.WithContext(httpctx.WithUserID(createReq.Context(), userID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	scheduleID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("désactivation", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"enabled": false})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/script-schedules/"+scheduleID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "scheduleID", scheduleID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["enabled"] != false {
			t.Errorf("enabled = %v, attendu false", data["enabled"])
		}
	})

	t.Run("planification introuvable retourne 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"enabled": false})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/script-schedules/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "scheduleID", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestScheduleHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Schedule Delete Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "sched-delete@example.com")
	scriptID := seedScript(t, pool, tenantID, userID)
	agentID := testutil.SeedAgent(t, pool, tenantID, "sched-delete-agent")
	h := NewScheduleHandler(pool)

	createBody, _ := json.Marshal(map[string]any{
		"script_id": scriptID, "name": "À supprimer",
		"agent_id": agentID, "cron_expression": "0 2 * * *",
	})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/script-schedules", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createReq = createReq.WithContext(httpctx.WithUserID(createReq.Context(), userID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	scheduleID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/script-schedules/"+scheduleID, nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = withURLParam(req, "scheduleID", scheduleID)
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}
