// Tests d'intégration AuditHandler.List + AuditLogger.Record — nécessitent
// une base Postgres de test réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	auditDomain "github.com/yourorg/leo-one/internal/domain/audit"
	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestAuditHandler_List_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Audit List Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "audit-list@example.com")
	repo := postgres.NewAuditRepo(pool)
	h := NewAuditHandler(repo)

	for _, action := range []string{"script.create", "script.delete"} {
		entry := &auditDomain.Entry{TenantID: tenantID, UserID: &userID, Action: action, ResourceType: "script"}
		if err := repo.Create(context.Background(), entry); err != nil {
			t.Fatalf("setup repo.Create a échoué : %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("len(data) = %d, attendu 2", len(data))
	}
}

func TestAuditHandler_List_FilterByAction_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Audit Filter Corp", 10)
	repo := postgres.NewAuditRepo(pool)
	h := NewAuditHandler(repo)

	for _, action := range []string{"script.create", "script.delete", "script.create"} {
		entry := &auditDomain.Entry{TenantID: tenantID, Action: action, ResourceType: "script"}
		if err := repo.Create(context.Background(), entry); err != nil {
			t.Fatalf("setup repo.Create a échoué : %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log?action=script.create", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("len(data) = %d, attendu 2 (filtré sur action=script.create)", len(data))
	}
}

func TestAuditHandler_List_IsolatedByTenant_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantA := testutil.SeedTenant(t, pool, "Audit Tenant A", 10)
	tenantB := testutil.SeedTenant(t, pool, "Audit Tenant B", 10)
	repo := postgres.NewAuditRepo(pool)
	h := NewAuditHandler(repo)

	if err := repo.Create(context.Background(), &auditDomain.Entry{TenantID: tenantA, Action: "agent.delete", ResourceType: "agent"}); err != nil {
		t.Fatalf("setup repo.Create a échoué : %v", err)
	}
	if err := repo.Create(context.Background(), &auditDomain.Entry{TenantID: tenantB, Action: "agent.delete", ResourceType: "agent"}); err != nil {
		t.Fatalf("setup repo.Create a échoué : %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit-log", nil)
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantA))
	rec := httptest.NewRecorder()

	h.List(rec, req)

	data := decodeEnvelope(t, rec.Body)["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("len(data) = %d, attendu 1 (isolation multi-tenant)", len(data))
	}
}

// TestAuditLogger_Record_WritesEntry_Integration vérifie le câblage bout en
// bout : ScriptHandler.Create, construit avec un AuditLogger réel (pas nil
// comme dans les autres tests de ce paquet), doit produire une entrée dans
// audit_log — pas seulement compiler avec le paramètre en plus.
func TestAuditLogger_Record_WritesEntry_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Audit Wiring Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "audit-wiring@example.com")
	auditRepo := postgres.NewAuditRepo(pool)
	auditLogger := NewAuditLogger(auditRepo, nil)
	scriptHandler := NewScriptHandler(pool, auditLogger)

	body, _ := json.Marshal(map[string]any{"name": "Audité", "interpreter": "bash", "content": "echo 1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scripts", bytes.NewReader(body))
	req = req.WithContext(httpctx.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpctx.WithUserID(req.Context(), userID))
	req = req.WithContext(httpctx.WithIP(req.Context(), "203.0.113.5"))
	rec := httptest.NewRecorder()

	scriptHandler.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup a échoué : %d %s", rec.Code, rec.Body.String())
	}

	entries, _, err := auditRepo.List(context.Background(), tenantID, auditDomain.ListFilter{})
	if err != nil {
		t.Fatalf("auditRepo.List a échoué : %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, attendu 1", len(entries))
	}

	entry := entries[0]
	if entry.Action != "script.create" || entry.ResourceType != "script" {
		t.Errorf("action/resource_type inattendus : %s / %s", entry.Action, entry.ResourceType)
	}
	if entry.UserID == nil || *entry.UserID != userID {
		t.Errorf("user_id inattendu : %v", entry.UserID)
	}
	if entry.IPAddress == nil || *entry.IPAddress != "203.0.113.5" {
		t.Errorf("ip_address inattendue : %v", entry.IPAddress)
	}
	if entry.Details == nil {
		t.Error("details ne devrait pas être nil (contenu de la requête de création)")
	}
}
