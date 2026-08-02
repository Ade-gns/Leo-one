package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
)

func TestAlertHandler_List(t *testing.T) {
	t.Run("tenant_id manquant retourne 401", func(t *testing.T) {
		h := NewAlertHandler(&mockAlertRepo{})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("succès retourne la liste", func(t *testing.T) {
		repo := &mockAlertRepo{
			listFunc: func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
				if tenantID != "tenant-1" {
					t.Errorf("tenantID = %q, attendu tenant-1", tenantID)
				}
				return []*alertDomain.Alert{{ID: "a1", TenantID: tenantID}}, "next-cursor", nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		env := decodeEnvelope(t, rec.Body)
		meta, ok := env["meta"].(map[string]any)
		if !ok {
			t.Fatalf("meta absent ou invalide: %v", env)
		}
		if meta["cursor"] != "next-cursor" {
			t.Errorf("cursor = %v, attendu next-cursor", meta["cursor"])
		}
	})

	t.Run("filtres status/severity/agent_id transmis au repo", func(t *testing.T) {
		var gotFilter alertDomain.ListFilter
		repo := &mockAlertRepo{
			listFunc: func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
				gotFilter = filter
				return nil, "", nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?status=open&severity=critical&agent_id=a1", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if gotFilter.Status == nil || *gotFilter.Status != alertDomain.StatusOpen {
			t.Errorf("Status = %v, attendu open", gotFilter.Status)
		}
		if gotFilter.Severity == nil || *gotFilter.Severity != alertDomain.SeverityCritical {
			t.Errorf("Severity = %v, attendu critical", gotFilter.Severity)
		}
		if gotFilter.AgentID == nil || *gotFilter.AgentID != "a1" {
			t.Errorf("AgentID = %v, attendu a1", gotFilter.AgentID)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAlertRepo{
			listFunc: func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
				return nil, "", errors.New("boom")
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAlertHandler_Get(t *testing.T) {
	t.Run("trouvé retourne 200", func(t *testing.T) {
		repo := &mockAlertRepo{
			findByIDFunc: func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
				return &alertDomain.Alert{ID: alertID, TenantID: tenantID}, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/al1", nil)
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("introuvable retourne 404", func(t *testing.T) {
		repo := &mockAlertRepo{
			findByIDFunc: func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
				return nil, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/unknown", nil)
		req = withURLParam(req, "alertID", "unknown")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("erreur repo retourne 500 (pas 404)", func(t *testing.T) {
		repo := &mockAlertRepo{
			findByIDFunc: func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
				return nil, errors.New("db down")
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/al1", nil)
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAlertHandler_Acknowledge(t *testing.T) {
	t.Run("succès retourne 200 avec l'alerte acquittée", func(t *testing.T) {
		repo := &mockAlertRepo{
			acknowledgeFunc: func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
				if userID != "user-1" {
					t.Errorf("userID = %q, attendu user-1", userID)
				}
				return &alertDomain.Alert{ID: alertID, Status: alertDomain.StatusAcknowledged}, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al1/acknowledge", nil)
		req = req.WithContext(httpctx.WithUserID(req.Context(), "user-1"))
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Acknowledge(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	t.Run("introuvable retourne 404", func(t *testing.T) {
		repo := &mockAlertRepo{
			acknowledgeFunc: func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
				return nil, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/unknown/acknowledge", nil)
		req = withURLParam(req, "alertID", "unknown")
		rec := httptest.NewRecorder()

		h.Acknowledge(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAlertRepo{
			acknowledgeFunc: func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
				return nil, errors.New("boom")
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al1/acknowledge", nil)
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Acknowledge(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAlertHandler_RulesStubsReturn501(t *testing.T) {
	h := NewAlertHandler(&mockAlertRepo{})

	methods := map[string]func(http.ResponseWriter, *http.Request){
		"ListRules":  h.ListRules,
		"CreateRule": h.CreateRule,
		"UpdateRule": h.UpdateRule,
		"DeleteRule": h.DeleteRule,
	}

	for name, fn := range methods {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
			rec := httptest.NewRecorder()

			fn(rec, req)

			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotImplemented)
			}
		})
	}
}
