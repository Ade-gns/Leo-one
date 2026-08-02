package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metricDomain "github.com/yourorg/leo-one/internal/domain/metric"
)

func TestMetricHandler_Query(t *testing.T) {
	t.Run("paramètre type manquant retourne 400", func(t *testing.T) {
		h := NewMetricHandler(&mockMetricRepo{})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1/metrics", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Query(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("plage par défaut est les dernières 24h", func(t *testing.T) {
		var gotFrom, gotTo time.Time
		repo := &mockMetricRepo{
			queryFunc: func(ctx context.Context, tenantID, agentID string, metricType metricDomain.Type, from, to time.Time) ([]metricDomain.QueryResult, metricDomain.Resolution, error) {
				gotFrom, gotTo = from, to
				return nil, metricDomain.ResolutionRaw, nil
			},
		}
		h := NewMetricHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1/metrics?type=cpu_percent", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Query(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}
		d := gotTo.Sub(gotFrom)
		if d < 23*time.Hour || d > 25*time.Hour {
			t.Errorf("plage par défaut = %v, attendu ~24h", d)
		}
	})

	t.Run("from/to explicites sont respectés", func(t *testing.T) {
		var gotFrom, gotTo time.Time
		repo := &mockMetricRepo{
			queryFunc: func(ctx context.Context, tenantID, agentID string, metricType metricDomain.Type, from, to time.Time) ([]metricDomain.QueryResult, metricDomain.Resolution, error) {
				gotFrom, gotTo = from, to
				return nil, metricDomain.ResolutionRaw, nil
			},
		}
		h := NewMetricHandler(repo)
		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/agents/a1/metrics?type=cpu_percent&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Query(rec, req)

		wantFrom, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
		wantTo, _ := time.Parse(time.RFC3339, "2026-01-02T00:00:00Z")
		if !gotFrom.Equal(wantFrom) || !gotTo.Equal(wantTo) {
			t.Errorf("from/to = %v/%v, attendu %v/%v", gotFrom, gotTo, wantFrom, wantTo)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockMetricRepo{
			queryFunc: func(ctx context.Context, tenantID, agentID string, metricType metricDomain.Type, from, to time.Time) ([]metricDomain.QueryResult, metricDomain.Resolution, error) {
				return nil, "", errors.New("boom")
			},
		}
		h := NewMetricHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1/metrics?type=cpu_percent", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Query(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestMetricHandler_Latest(t *testing.T) {
	t.Run("succès retourne les métriques et le timestamp", func(t *testing.T) {
		ts := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
		repo := &mockMetricRepo{
			latestFunc: func(ctx context.Context, tenantID, agentID string) (map[metricDomain.Type]float64, time.Time, error) {
				return map[metricDomain.Type]float64{metricDomain.TypeCPUPercent: 42.5}, ts, nil
			},
		}
		h := NewMetricHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1/metrics/latest", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Latest(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}
		env := decodeEnvelope(t, rec.Body)
		data, ok := env["data"].(map[string]any)
		if !ok {
			t.Fatalf("data absent ou invalide: %v", env)
		}
		if data["cpu_percent"] != 42.5 {
			t.Errorf("cpu_percent = %v, attendu 42.5", data["cpu_percent"])
		}
		if data["ts"] == nil {
			t.Error("ts absent alors que le timestamp n'est pas zéro")
		}
	})

	t.Run("timestamp zéro n'apparaît pas dans la réponse", func(t *testing.T) {
		repo := &mockMetricRepo{
			latestFunc: func(ctx context.Context, tenantID, agentID string) (map[metricDomain.Type]float64, time.Time, error) {
				return map[metricDomain.Type]float64{}, time.Time{}, nil
			},
		}
		h := NewMetricHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1/metrics/latest", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Latest(rec, req)

		env := decodeEnvelope(t, rec.Body)
		data := env["data"].(map[string]any)
		if _, present := data["ts"]; present {
			t.Error("ts ne devrait pas être présent quand le timestamp est zéro")
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockMetricRepo{
			latestFunc: func(ctx context.Context, tenantID, agentID string) (map[metricDomain.Type]float64, time.Time, error) {
				return nil, time.Time{}, errors.New("boom")
			},
		}
		h := NewMetricHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/a1/metrics/latest", nil)
		req = withURLParam(req, "agentID", "a1")
		rec := httptest.NewRecorder()

		h.Latest(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}
