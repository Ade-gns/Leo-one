// Tests d'intégration TenantHandler — nécessitent une base Postgres de
// test réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestTenantHandler_Get_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Tenant Corp", 5)
	h := NewTenantHandler(postgres.NewTenantRepo(pool), postgres.NewAgentRepo(pool))

	// Un agent enrôlé pour vérifier que agent_count reflète la réalité.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO agents (tenant_id, hostname, os, os_version, arch, hardware_id, agent_version)
		VALUES ($1, 'test-host', 'linux', '24.04', 'amd64', 'hw-tenant-get', '1.0.0')
	`, tenantID)
	if err != nil {
		t.Fatalf("setup agent a échoué : %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)

	if data["name"] != "Tenant Corp" {
		t.Errorf("name = %v, attendu \"Tenant Corp\"", data["name"])
	}
	if data["agent_count"].(float64) != 1 {
		t.Errorf("agent_count = %v, attendu 1", data["agent_count"])
	}
	planLimits, _ := data["plan_limits"].(map[string]any)
	if planLimits["max_agents"].(float64) != 5 {
		t.Errorf("plan_limits.max_agents = %v, attendu 5", planLimits["max_agents"])
	}
}

func TestTenantHandler_Update_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Original Name", 10)
	h := NewTenantHandler(postgres.NewTenantRepo(pool), postgres.NewAgentRepo(pool))

	t.Run("modifie le nom", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Nouveau Nom"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenant", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["name"] != "Nouveau Nom" {
			t.Errorf("name = %v, attendu \"Nouveau Nom\"", data["name"])
		}
	})

	t.Run("nom vide est rejeté avec 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "   "})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenant", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("plan/max_agents ne sont pas modifiables via cette route", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Encore un nom", "max_agents": 9999, "plan": "enterprise"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/tenant", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}

		var maxAgents int
		var plan string
		err := pool.QueryRow(context.Background(), `SELECT max_agents, plan FROM tenants WHERE id = $1`, tenantID).Scan(&maxAgents, &plan)
		if err != nil {
			t.Fatalf("lecture BDD a échoué : %v", err)
		}
		if maxAgents == 9999 {
			t.Error("max_agents n'aurait jamais dû changer via PATCH /tenant (non exposé en self-service)")
		}
		if plan == "enterprise" {
			t.Error("plan n'aurait jamais dû changer via PATCH /tenant (non exposé en self-service)")
		}
	})
}
