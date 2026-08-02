package handlers

import (
	"context"
	"time"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	metricDomain "github.com/yourorg/leo-one/internal/domain/metric"
)

// ─── mockAgentRepo ──────────────────────────────────────────────────────────

type mockAgentRepo struct {
	findByIDFunc         func(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error)
	findByHardwareIDFunc func(ctx context.Context, tenantID, hardwareID string) (*agentDomain.Agent, error)
	listFunc             func(ctx context.Context, tenantID string, filter agentDomain.ListFilter) ([]*agentDomain.Agent, string, error)
	createFunc           func(ctx context.Context, agent *agentDomain.Agent) error
	updateFunc           func(ctx context.Context, agent *agentDomain.Agent) error
	updateStatusFunc     func(ctx context.Context, agentID string, status agentDomain.Status, lastSeen *time.Time) error
	deleteFunc           func(ctx context.Context, tenantID, agentID string) error
	countByTenantFunc    func(ctx context.Context, tenantID string) (int, error)
}

func (m *mockAgentRepo) FindByID(ctx context.Context, tenantID, agentID string) (*agentDomain.Agent, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(ctx, tenantID, agentID)
	}
	return nil, nil
}

func (m *mockAgentRepo) FindByHardwareID(ctx context.Context, tenantID, hardwareID string) (*agentDomain.Agent, error) {
	if m.findByHardwareIDFunc != nil {
		return m.findByHardwareIDFunc(ctx, tenantID, hardwareID)
	}
	return nil, nil
}

func (m *mockAgentRepo) List(ctx context.Context, tenantID string, filter agentDomain.ListFilter) ([]*agentDomain.Agent, string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, tenantID, filter)
	}
	return nil, "", nil
}

func (m *mockAgentRepo) Create(ctx context.Context, agent *agentDomain.Agent) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, agent)
	}
	return nil
}

func (m *mockAgentRepo) Update(ctx context.Context, agent *agentDomain.Agent) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, agent)
	}
	return nil
}

func (m *mockAgentRepo) UpdateStatus(ctx context.Context, agentID string, status agentDomain.Status, lastSeen *time.Time) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, agentID, status, lastSeen)
	}
	return nil
}

func (m *mockAgentRepo) Delete(ctx context.Context, tenantID, agentID string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, tenantID, agentID)
	}
	return nil
}

func (m *mockAgentRepo) CountByTenant(ctx context.Context, tenantID string) (int, error) {
	if m.countByTenantFunc != nil {
		return m.countByTenantFunc(ctx, tenantID)
	}
	return 0, nil
}

var _ agentDomain.Repository = (*mockAgentRepo)(nil)

// ─── mockMetricRepo ─────────────────────────────────────────────────────────

type mockMetricRepo struct {
	insertBatchFunc func(ctx context.Context, points []metricDomain.Point) error
	queryFunc       func(ctx context.Context, tenantID, agentID string, metricType metricDomain.Type, from, to time.Time) ([]metricDomain.QueryResult, metricDomain.Resolution, error)
	latestFunc      func(ctx context.Context, tenantID, agentID string) (map[metricDomain.Type]float64, time.Time, error)
}

func (m *mockMetricRepo) InsertBatch(ctx context.Context, points []metricDomain.Point) error {
	if m.insertBatchFunc != nil {
		return m.insertBatchFunc(ctx, points)
	}
	return nil
}

func (m *mockMetricRepo) Query(ctx context.Context, tenantID, agentID string, metricType metricDomain.Type, from, to time.Time) ([]metricDomain.QueryResult, metricDomain.Resolution, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, tenantID, agentID, metricType, from, to)
	}
	return nil, "", nil
}

func (m *mockMetricRepo) Latest(ctx context.Context, tenantID, agentID string) (map[metricDomain.Type]float64, time.Time, error) {
	if m.latestFunc != nil {
		return m.latestFunc(ctx, tenantID, agentID)
	}
	return nil, time.Time{}, nil
}

var _ metricDomain.Repository = (*mockMetricRepo)(nil)
