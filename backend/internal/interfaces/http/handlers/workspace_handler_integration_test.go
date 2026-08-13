// Tests d'intégration WorkspaceHandler — nécessitent une base Postgres de
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

func TestWorkspaceHandler_Create_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Workspace Corp", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	body, _ := json.Marshal(map[string]any{"name": "Paris", "description": "Site parisien"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["name"] != "Paris" {
		t.Errorf("name = %v, attendu \"Paris\"", data["name"])
	}
	if data["description"] != "Site parisien" {
		t.Errorf("description = %v, attendu \"Site parisien\"", data["description"])
	}
}

func TestWorkspaceHandler_Create_EmptyName_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Empty Name Corp", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	body, _ := json.Marshal(map[string]any{"name": "   "})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWorkspaceHandler_Create_DuplicateName_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Dup Name Corp", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	body, _ := json.Marshal(map[string]any{"name": "Lyon"})
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body))
	req1 = req1.WithContext(httpctx.WithTenantID(req1.Context(), tenantID))
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("setup a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body))
	req2 = req2.WithContext(httpctx.WithTenantID(req2.Context(), tenantID))
	h.Create(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("code = %d, attendu %d, body=%s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

func TestWorkspaceHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantA := testutil.SeedTenant(t, pool, "List Workspace A", 10)
	tenantB := testutil.SeedTenant(t, pool, "List Workspace B", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	create := func(tenantID, name string) {
		body, _ := json.Marshal(map[string]any{"name": name})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup Create a échoué : %d %s", rec.Code, rec.Body.String())
		}
	}
	create(tenantA, "Site A1")
	create(tenantA, "Site A2")
	create(tenantB, "Site B1")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantA))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items, _ := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(items) != 2 {
		t.Fatalf("nombre de workspaces = %d, attendu 2 (isolation tenant)", len(items))
	}
}

func TestWorkspaceHandler_Update_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Update Workspace Corp", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	createBody, _ := json.Marshal(map[string]any{"name": "Original"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	workspaceID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("modifie name/description", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Renamed", "description": "Nouvelle description"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/"+workspaceID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "workspaceID", workspaceID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["name"] != "Renamed" {
			t.Errorf("name = %v, attendu \"Renamed\"", data["name"])
		}
	})

	t.Run("workspace introuvable retourne 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "workspaceID", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("workspace d'un autre tenant retourne 404 (isolation)", func(t *testing.T) {
		otherTenant := testutil.SeedTenant(t, pool, "Update Workspace Other Corp", 10)
		body, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/workspaces/"+workspaceID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), otherTenant))
		req = withURLParam(req, "workspaceID", workspaceID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestWorkspaceHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Delete Workspace Corp", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	createBody, _ := json.Marshal(map[string]any{"name": "À supprimer"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	workspaceID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("succès retourne 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/"+workspaceID, nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "workspaceID", workspaceID)
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
		}
	})

	t.Run("suppression répétée retourne 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/"+workspaceID, nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "workspaceID", workspaceID)
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}

// TestWorkspaceHandler_Delete_DetachesAgents vérifie que la suppression d'un
// workspace ne supprime jamais les agents qui y étaient rattachés — voir la
// note dans router.go ("les agents sont déplacés dans workspace_id = NULL"),
// garantie par ON DELETE SET NULL (migrations/001_init_schema.sql).
func TestWorkspaceHandler_Delete_DetachesAgents(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Detach Corp", 10)
	h := NewWorkspaceHandler(postgres.NewWorkspaceRepo(pool))

	createBody, _ := json.Marshal(map[string]any{"name": "Site avec agents"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	workspaceID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	var agentID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO agents (tenant_id, workspace_id, hostname, os, os_version, arch, hardware_id, agent_version)
		VALUES ($1, $2, 'test-host', 'linux', '24.04', 'amd64', 'hw-detach-test', '1.0.0')
		RETURNING id
	`, tenantID, workspaceID).Scan(&agentID)
	if err != nil {
		t.Fatalf("setup agent a échoué : %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/"+workspaceID, nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = withURLParam(req, "workspaceID", workspaceID)
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	var stillExists bool
	var workspaceIDAfter *string
	err = pool.QueryRow(context.Background(),
		`SELECT true, workspace_id FROM agents WHERE id = $1`, agentID,
	).Scan(&stillExists, &workspaceIDAfter)
	if err != nil {
		t.Fatalf("l'agent ne devrait pas avoir été supprimé : %v", err)
	}
	if !stillExists {
		t.Fatal("l'agent devrait toujours exister après suppression de son workspace")
	}
	if workspaceIDAfter != nil {
		t.Errorf("workspace_id = %v, attendu nil (détaché, pas supprimé)", *workspaceIDAfter)
	}
}
