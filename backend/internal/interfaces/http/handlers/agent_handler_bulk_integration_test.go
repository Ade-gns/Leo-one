// Tests d'intégration AgentHandler.BulkCreateCommand — nécessitent une base
// Postgres de test réelle (voir internal/testutil.TestDB). Réutilise
// newTestHub/nopWriter définis dans agent_handler_enroll_integration_test.go.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestAgentHandler_BulkCreateCommand_AgentIDs_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Bulk Cmd Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "bulk-agentids@example.com")
	agent1 := testutil.SeedAgent(t, pool, tenantID, "bulk-agent-1")
	agent2 := testutil.SeedAgent(t, pool, tenantID, "bulk-agent-2")

	h := NewAgentHandler(nil, pool, newTestHub(pool), nil, "", "", nil)

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{agent1, agent2},
		"type":      "exec_script",
		"payload":   map[string]any{"interpreter": "bash", "script": "echo 1", "timeout_sec": 30},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/bulk-commands", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.BulkCreateCommand(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	results := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, attendu 2", len(results))
	}
	for _, r := range results {
		res := r.(map[string]any)
		if res["sent"] != false {
			t.Errorf("sent = %v, attendu false (aucun agent connecté dans ce test)", res["sent"])
		}
		if res["command_id"] == nil || res["command_id"] == "" {
			t.Errorf("command_id manquant : %v", res)
		}
	}

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM commands WHERE tenant_id = $1`, tenantID,
	).Scan(&count); err != nil {
		t.Fatalf("vérification BDD a échoué : %v", err)
	}
	if count != 2 {
		t.Errorf("count(commands) = %d, attendu 2 (une par agent ciblé)", count)
	}
}

func TestAgentHandler_BulkCreateCommand_Workspace_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Bulk Cmd Workspace Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "bulk-workspace@example.com")

	var workspaceID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO workspaces (tenant_id, name) VALUES ($1, 'Atelier') RETURNING id
	`, tenantID).Scan(&workspaceID); err != nil {
		t.Fatalf("setup workspace a échoué : %v", err)
	}

	agentIn := testutil.SeedAgent(t, pool, tenantID, "bulk-ws-agent-in")
	if _, err := pool.Exec(context.Background(),
		`UPDATE agents SET workspace_id = $1 WHERE id = $2`, workspaceID, agentIn,
	); err != nil {
		t.Fatalf("setup : assignation workspace a échoué : %v", err)
	}
	testutil.SeedAgent(t, pool, tenantID, "bulk-ws-agent-out") // hors du workspace, ne doit pas être ciblé

	h := NewAgentHandler(nil, pool, newTestHub(pool), nil, "", "", nil)

	body, _ := json.Marshal(map[string]any{
		"workspace_id": workspaceID,
		"type":         "exec_script",
		"payload":      map[string]any{"interpreter": "bash", "script": "echo 1", "timeout_sec": 30},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/bulk-commands", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.BulkCreateCommand(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	results := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, attendu 1 (seul l'agent du workspace ciblé)", len(results))
	}
	if results[0].(map[string]any)["agent_id"] != agentIn {
		t.Errorf("agent_id = %v, attendu %s", results[0].(map[string]any)["agent_id"], agentIn)
	}
}

func TestAgentHandler_BulkCreateCommand_BothTargets_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Bulk Cmd Both Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "bulk-both@example.com")
	agentID := testutil.SeedAgent(t, pool, tenantID, "bulk-both-agent")

	h := NewAgentHandler(nil, pool, newTestHub(pool), nil, "", "", nil)

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{agentID}, "workspace_id": "22222222-2222-2222-2222-222222222222",
		"type": "exec_script", "payload": map[string]any{"interpreter": "bash", "script": "echo 1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/bulk-commands", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.BulkCreateCommand(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d (agent_ids ET workspace_id fournis), body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAgentHandler_BulkCreateCommand_InvalidType_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Bulk Cmd Bad Type Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "bulk-badtype@example.com")
	agentID := testutil.SeedAgent(t, pool, tenantID, "bulk-badtype-agent")

	h := NewAgentHandler(nil, pool, newTestHub(pool), nil, "", "", nil)

	body, _ := json.Marshal(map[string]any{
		"agent_ids": []string{agentID}, "type": "nimportequoi", "payload": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/bulk-commands", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	rec := httptest.NewRecorder()

	h.BulkCreateCommand(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
