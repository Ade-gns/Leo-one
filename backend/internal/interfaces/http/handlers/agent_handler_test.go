package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
)

func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decodeEnvelope(t *testing.T, body *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(body.Bytes(), &env); err != nil {
		t.Fatalf("réponse JSON invalide: %v (body=%s)", err, body.String())
	}
	return env
}

func TestAgentHandler_List(t *testing.T) {
	t.Run("tenant_id manquant retourne 401", func(t *testing.T) {
		h := NewAgentHandler(&mockAgentRepo{}, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("succès retourne la liste et le total", func(t *testing.T) {
		repo := &mockAgentRepo{
			listFunc: func(ctx context.Context, tenantID string, filter agentDomain.ListFilter) ([]*agentDomain.Agent, string, error) {
				if tenantID != "tenant-1" {
					t.Errorf("tenantID = %q, attendu tenant-1", tenantID)
				}
				return []*agentDomain.Agent{{ID: "a1", TenantID: tenantID}}, "next-cursor", nil
			},
			countByTenantFunc: func(ctx context.Context, tenantID string) (int, error) {
				return 1, nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
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
		if meta["total"] != float64(1) {
			t.Errorf("total = %v, attendu 1", meta["total"])
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAgentRepo{
			listFunc: func(ctx context.Context, tenantID string, filter agentDomain.ListFilter) ([]*agentDomain.Agent, string, error) {
				return nil, "", errors.New("boom")
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("limit hors bornes est ignorée", func(t *testing.T) {
		var gotLimit int
		repo := &mockAgentRepo{
			listFunc: func(ctx context.Context, tenantID string, filter agentDomain.ListFilter) ([]*agentDomain.Agent, string, error) {
				gotLimit = filter.Limit
				return nil, "", nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?limit=9999", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if gotLimit != 50 {
			t.Errorf("limit = %d, attendu 50 (valeur par défaut car hors bornes)", gotLimit)
		}
	})
}

func TestAgentHandler_Get(t *testing.T) {
	t.Run("trouvé retourne 200", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return &agentDomain.Agent{ID: agentID, TenantID: tenantID}, nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("introuvable retourne 404", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return nil, nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/unknown", nil)
		req = withURLParam(req, "agentID", "unknown")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return nil, errors.New("db down")
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAgentHandler_Update(t *testing.T) {
	t.Run("introuvable retourne 404 sans appeler Update", func(t *testing.T) {
		updateCalled := false
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return nil, nil
			},
			updateFunc: func(ctx context.Context, agent *agentDomain.Agent) error {
				updateCalled = true
				return nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`{"hostname":"new-name"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/a1", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
		if updateCalled {
			t.Error("Update ne doit pas être appelé si l'agent est introuvable")
		}
	})

	t.Run("corps invalide retourne 400", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return &agentDomain.Agent{ID: agentID}, nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`not-json`)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/a1", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("succès applique les champs fournis", func(t *testing.T) {
		var updated *agentDomain.Agent
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return &agentDomain.Agent{ID: agentID, Hostname: "old-name"}, nil
			},
			updateFunc: func(ctx context.Context, agent *agentDomain.Agent) error {
				updated = agent
				return nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`{"hostname":"new-name"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/a1", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		if updated == nil || updated.Hostname != "new-name" {
			t.Fatalf("hostname non mis à jour: %+v", updated)
		}
	})

	t.Run("erreur update retourne 500", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return &agentDomain.Agent{ID: agentID}, nil
			},
			updateFunc: func(ctx context.Context, agent *agentDomain.Agent) error {
				return errors.New("boom")
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`{"hostname":"new-name"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/a1", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Update(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAgentHandler_Delete(t *testing.T) {
	t.Run("succès retourne 204", func(t *testing.T) {
		repo := &mockAgentRepo{
			deleteFunc: func(ctx context.Context, tenantID, agentID string) error {
				return nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/a1", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNoContent)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAgentRepo{
			deleteFunc: func(ctx context.Context, tenantID, agentID string) error {
				return errors.New("boom")
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/a1", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Delete(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

// CreateCommand : seules les branches ne touchant pas la BDD sont testables
// sans base de données réelle (le handler exécute une requête SQL directe
// via *pgxpool.Pool dès que l'agent est trouvé et le corps décodé).
func TestAgentHandler_CreateCommand_WithoutDB(t *testing.T) {
	t.Run("agent introuvable retourne 404", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return nil, nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`{"type":"reboot"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/a1/commands", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.CreateCommand(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return nil, errors.New("db down")
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`{"type":"reboot"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/a1/commands", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.CreateCommand(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})

	t.Run("corps invalide retourne 400 avant tout accès BDD", func(t *testing.T) {
		repo := &mockAgentRepo{
			findByIDFunc: func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
				return &agentDomain.Agent{ID: agentID}, nil
			},
		}
		h := NewAgentHandler(repo, nil, nil, nil, "", "")
		body := bytes.NewBufferString(`not-json`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/a1/commands", body)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.CreateCommand(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})
}
