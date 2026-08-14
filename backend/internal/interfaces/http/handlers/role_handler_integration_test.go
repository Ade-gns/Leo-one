// Tests d'intégration RoleHandler.Create/Update/Delete (rôles personnalisés)
// — nécessitent une base Postgres de test réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func firstPermissionID(t *testing.T, pool *pgxpool.Pool, resource, action string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM permissions WHERE resource = $1 AND action = $2`, resource, action,
	).Scan(&id); err != nil {
		t.Fatalf("firstPermissionID(%s,%s) a échoué : %v", resource, action, err)
	}
	return id
}

func TestRoleHandler_Create_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Role Create Corp", 10)
	permID := firstPermissionID(t, pool, "agents", "read")
	h := NewRoleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"name": "Superviseur", "description": "Vue globale", "permission_ids": []string{permID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
	if data["name"] != "Superviseur" {
		t.Errorf("name = %v, attendu \"Superviseur\"", data["name"])
	}
	if data["is_system"] != false {
		t.Errorf("is_system = %v, attendu false (rôle personnalisé)", data["is_system"])
	}
	perms, _ := data["permissions"].([]any)
	if len(perms) != 1 {
		t.Fatalf("permissions = %v, attendu 1 entrée", perms)
	}
}

func TestRoleHandler_Create_EmptyName_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Role Empty Name Corp", 10)
	h := NewRoleHandler(pool)

	body, _ := json.Marshal(map[string]any{"name": "   "})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRoleHandler_Create_InvalidPermissionID_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Role Invalid Perm Corp", 10)
	h := NewRoleHandler(pool)

	body, _ := json.Marshal(map[string]any{
		"name": "Rôle cassé", "permission_ids": []string{"00000000-0000-0000-0000-000000000000"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRoleHandler_Create_DuplicateName_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Role Dup Corp", 10)
	h := NewRoleHandler(pool)

	body, _ := json.Marshal(map[string]any{"name": "Doublon"})
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(body))
	req1 = req1.WithContext(httpctx.WithTenantID(req1.Context(), tenantID))
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("setup a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(body))
	req2 = req2.WithContext(httpctx.WithTenantID(req2.Context(), tenantID))
	h.Create(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("code = %d, attendu %d, body=%s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

func TestRoleHandler_Update_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Role Update Corp", 10)
	testutil.SeedSystemRoles(t, pool, tenantID)
	permRead := firstPermissionID(t, pool, "agents", "read")
	permWrite := firstPermissionID(t, pool, "agents", "write")
	h := NewRoleHandler(pool)

	createBody, _ := json.Marshal(map[string]any{"name": "Rôle initial", "permission_ids": []string{permRead}})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	roleID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("modifie name/permission_ids (remplace)", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "Rôle renommé", "permission_ids": []string{permWrite}})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/roles/"+roleID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "roleID", roleID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["name"] != "Rôle renommé" {
			t.Errorf("name = %v, attendu \"Rôle renommé\"", data["name"])
		}
		perms, _ := data["permissions"].([]any)
		if len(perms) != 1 || perms[0].(map[string]any)["id"] != permWrite {
			t.Errorf("permissions = %v, attendu [%s] (remplacé, pas cumulé)", perms, permWrite)
		}
	})

	t.Run("rôle système est immuable (403)", func(t *testing.T) {
		var adminRoleID string
		err := pool.QueryRow(context.Background(),
			`SELECT id FROM roles WHERE tenant_id = $1 AND name = 'Admin'`, tenantID,
		).Scan(&adminRoleID)
		if err != nil {
			t.Fatalf("lecture du rôle Admin a échoué : %v", err)
		}

		body, _ := json.Marshal(map[string]any{"name": "Admin modifié"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/roles/"+adminRoleID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "roleID", adminRoleID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
		}
		errObj := decodeEnvelope(t, rec.Body)["error"].(map[string]any)
		if errObj["code"] != "SYSTEM_ROLE_IMMUTABLE" {
			t.Errorf("code d'erreur = %v, attendu SYSTEM_ROLE_IMMUTABLE", errObj["code"])
		}
	})

	t.Run("rôle introuvable retourne 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/roles/00000000-0000-0000-0000-000000000000", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "roleID", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("rôle d'un autre tenant retourne 404 (isolation)", func(t *testing.T) {
		otherTenant := testutil.SeedTenant(t, pool, "Role Update Other Corp", 10)
		body, _ := json.Marshal(map[string]any{"name": "X"})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/roles/"+roleID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), otherTenant))
		req = withURLParam(req, "roleID", roleID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestRoleHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Role Delete Corp", 10)
	testutil.SeedSystemRoles(t, pool, tenantID)
	h := NewRoleHandler(pool)

	t.Run("rôle système est immuable (403)", func(t *testing.T) {
		var adminRoleID string
		err := pool.QueryRow(context.Background(),
			`SELECT id FROM roles WHERE tenant_id = $1 AND name = 'Admin'`, tenantID,
		).Scan(&adminRoleID)
		if err != nil {
			t.Fatalf("lecture du rôle Admin a échoué : %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/roles/"+adminRoleID, nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "roleID", adminRoleID)
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
		}
	})

	t.Run("succès désassigne les utilisateurs sans les supprimer", func(t *testing.T) {
		createBody, _ := json.Marshal(map[string]any{"name": "À supprimer"})
		createReq := httptest.NewRequest(http.MethodPost, "/api/v1/roles", bytes.NewReader(createBody))
		createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
		createRec := httptest.NewRecorder()
		h.Create(createRec, createReq)
		roleID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

		var userID string
		err := pool.QueryRow(context.Background(), `
			INSERT INTO users (tenant_id, email, password_hash, full_name)
			VALUES ($1, 'delete-role-test@example.com', 'x', 'Test User')
			RETURNING id
		`, tenantID).Scan(&userID)
		if err != nil {
			t.Fatalf("setup user a échoué : %v", err)
		}
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, userID, roleID,
		); err != nil {
			t.Fatalf("setup user_roles a échoué : %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/roles/"+roleID, nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "roleID", roleID)
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
		}

		var userCount int
		pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&userCount)
		if userCount != 1 {
			t.Error("l'utilisateur ne devrait jamais être supprimé par la suppression d'un rôle")
		}
		var assignCount int
		pool.QueryRow(context.Background(), `SELECT count(*) FROM user_roles WHERE user_id = $1`, userID).Scan(&assignCount)
		if assignCount != 0 {
			t.Error("l'assignation du rôle supprimé devrait avoir disparu (cascade)")
		}
	})

	t.Run("rôle introuvable retourne 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/roles/00000000-0000-0000-0000-000000000000", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "roleID", "00000000-0000-0000-0000-000000000000")
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})
}
