// Tests d'intégration UserHandler/RoleHandler — nécessitent une base
// Postgres de test réelle (voir internal/testutil.TestDB). UserRepo/
// RoleHandler exécutent des requêtes SQL réelles (jointures user_roles,
// transaction dans SetRoles), pas mockables de façon utile sans base.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func roleIDByName(t *testing.T, pool *pgxpool.Pool, tenantID, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM roles WHERE tenant_id = $1 AND name = $2`, tenantID, name,
	).Scan(&id); err != nil {
		t.Fatalf("roleIDByName(%q) a échoué : %v", name, err)
	}
	return id
}

func TestUserHandler_Create_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Users Corp", 10)
	testutil.SeedSystemRoles(t, pool, tenantID)
	adminRoleID := roleIDByName(t, pool, tenantID, "Admin")
	h := NewUserHandler(postgres.NewUserRepo(pool))

	body, _ := json.Marshal(map[string]any{
		"email":     "Alice@Example.com", // casse volontaire : doit être normalisée
		"full_name": "Alice Dupont",
		"password":  "correct-horse-battery",
		"role_ids":  []string{adminRoleID},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body)
	data := env["data"].(map[string]any)

	if data["email"] != "alice@example.com" {
		t.Errorf("email = %v, attendu normalisé en minuscules", data["email"])
	}
	roles, _ := data["roles"].([]any)
	if len(roles) != 1 || roles[0].(map[string]any)["name"] != "Admin" {
		t.Errorf("roles = %v, attendu [Admin]", roles)
	}

	userID, _ := data["id"].(string)
	var storedHash string
	err := pool.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&storedHash)
	if err != nil {
		t.Fatalf("lecture BDD a échoué : %v", err)
	}
	if storedHash == "correct-horse-battery" {
		t.Error("le mot de passe ne devrait jamais être stocké en clair")
	}
}

func TestUserHandler_Create_DuplicateEmail_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Dup Email Corp", 10)
	h := NewUserHandler(postgres.NewUserRepo(pool))

	body, _ := json.Marshal(map[string]any{
		"email": "bob@example.com", "full_name": "Bob", "password": "password123",
	})
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req1 = req1.WithContext(httpctx.WithTenantID(req1.Context(), tenantID))
	h.Create(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("setup a échoué : %d %s", rec1.Code, rec1.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req2 = req2.WithContext(httpctx.WithTenantID(req2.Context(), tenantID))
	h.Create(rec2, req2)

	if rec2.Code != http.StatusConflict {
		t.Fatalf("code = %d, attendu %d, body=%s", rec2.Code, http.StatusConflict, rec2.Body.String())
	}
}

func TestUserHandler_Create_ShortPassword_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Short Pw Corp", 10)
	h := NewUserHandler(postgres.NewUserRepo(pool))

	body, _ := json.Marshal(map[string]any{"email": "c@example.com", "full_name": "C", "password": "short"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUserHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantA := testutil.SeedTenant(t, pool, "List Users A", 10)
	tenantB := testutil.SeedTenant(t, pool, "List Users B", 10)
	h := NewUserHandler(postgres.NewUserRepo(pool))

	create := func(tenantID, email string) {
		body, _ := json.Marshal(map[string]any{"email": email, "full_name": "N", "password": "password123"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup Create a échoué : %d %s", rec.Code, rec.Body.String())
		}
	}
	create(tenantA, "a1@example.com")
	create(tenantA, "a2@example.com")
	create(tenantB, "b1@example.com")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantA))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	env := decodeEnvelope(t, rec.Body)
	items, _ := env["data"].([]any)
	if len(items) != 2 {
		t.Fatalf("nombre d'utilisateurs = %d, attendu 2 (isolation tenant)", len(items))
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		if _, hasHash := item["password_hash"]; hasHash {
			t.Error("List ne doit jamais renvoyer le hash de mot de passe")
		}
	}
}

func TestUserHandler_Get_NotFound_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Get NotFound Corp", 10)
	h := NewUserHandler(postgres.NewUserRepo(pool))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000000", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = withURLParam(req, "userID", "00000000-0000-0000-0000-000000000000")
	rec := httptest.NewRecorder()

	h.Get(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
	}
}

func TestUserHandler_Update_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Update Users Corp", 10)
	testutil.SeedSystemRoles(t, pool, tenantID)
	h := NewUserHandler(postgres.NewUserRepo(pool))

	createBody, _ := json.Marshal(map[string]any{"email": "u@example.com", "full_name": "Original Name", "password": "password123"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup a échoué : %d %s", createRec.Code, createRec.Body.String())
	}
	userID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("modifie full_name/is_active/role_ids", func(t *testing.T) {
		techRoleID := roleIDByName(t, pool, tenantID, "Technicien")
		body, _ := json.Marshal(map[string]any{
			"full_name": "Nouveau Nom",
			"is_active": false,
			"role_ids":  []string{techRoleID},
		})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+userID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "userID", userID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		data := decodeEnvelope(t, rec.Body)["data"].(map[string]any)
		if data["full_name"] != "Nouveau Nom" {
			t.Errorf("full_name = %v, attendu \"Nouveau Nom\"", data["full_name"])
		}
		if data["is_active"] != false {
			t.Errorf("is_active = %v, attendu false", data["is_active"])
		}
		roles, _ := data["roles"].([]any)
		if len(roles) != 1 || roles[0].(map[string]any)["name"] != "Technicien" {
			t.Errorf("roles = %v, attendu [Technicien] (remplace, ne cumule pas)", roles)
		}
	})

	t.Run("un role_id invalide est rejeté avec 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"role_ids": []string{"00000000-0000-0000-0000-000000000000"}})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+userID, bytes.NewReader(body))
		req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
		req = withURLParam(req, "userID", userID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("un utilisateur ne peut pas se désactiver lui-même", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"is_active": false})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+userID, bytes.NewReader(body))
		ctx := httpctx.WithTenantID(req.Context(), tenantID)
		ctx = httpctx.WithUserID(ctx, userID) // l'utilisateur courant EST la cible
		req = req.WithContext(ctx)
		req = withURLParam(req, "userID", userID)
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestUserHandler_Delete_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Delete Users Corp", 10)
	h := NewUserHandler(postgres.NewUserRepo(pool))

	createBody, _ := json.Marshal(map[string]any{"email": "d@example.com", "full_name": "D", "password": "password123"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(createBody))
	createReq = createReq.WithContext(httpctx.WithTenantID(createReq.Context(), tenantID))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	userID := decodeEnvelope(t, createRec.Body)["data"].(map[string]any)["id"].(string)

	t.Run("ne peut pas se supprimer soi-même", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+userID, nil)
		ctx := httpctx.WithTenantID(req.Context(), tenantID)
		ctx = httpctx.WithUserID(ctx, userID)
		req = req.WithContext(ctx)
		req = withURLParam(req, "userID", userID)
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("succès retourne 204, utilisateur introuvable ensuite", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+userID, nil)
		ctx := httpctx.WithTenantID(req.Context(), tenantID)
		ctx = httpctx.WithUserID(ctx, "un-autre-user-id")
		req = req.WithContext(ctx)
		req = withURLParam(req, "userID", userID)
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+userID, nil)
		getReq = getReq.WithContext(httpctx.WithTenantID(getReq.Context(), tenantID))
		getReq = withURLParam(getReq, "userID", userID)
		getRec := httptest.NewRecorder()
		h.Get(getRec, getReq)
		if getRec.Code != http.StatusNotFound {
			t.Errorf("code après suppression = %d, attendu %d", getRec.Code, http.StatusNotFound)
		}
	})
}

func TestRoleHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Roles Corp", 10)
	testutil.SeedSystemRoles(t, pool, tenantID)
	h := NewRoleHandler(pool)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/roles", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items, _ := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("nombre de rôles = %d, attendu 3 (Admin/Technicien/Lecture seule)", len(items))
	}
}

func TestRoleHandler_ListPermissions_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	testutil.SeedTenant(t, pool, "Perms Corp", 10)
	h := NewRoleHandler(pool)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/permissions", nil)
	rec := httptest.NewRecorder()

	h.ListPermissions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items, _ := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(items) == 0 {
		t.Error("le catalogue de permissions ne devrait pas être vide (seedé par migrations/003)")
	}
}
