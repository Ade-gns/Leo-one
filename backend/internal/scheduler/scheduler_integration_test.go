// Test d'intégration du scheduler — nécessite une base Postgres de test
// réelle (voir internal/testutil.TestDB). Vérifie spécifiquement le
// comportement propre aux planifications ponctuelles (run_at) : un seul
// déclenchement, puis désactivation — contrairement aux planifications
// récurrentes (cron_expression), couvertes par TestComputeNextRun
// (scheduler_test.go, pur, sans dépendance BDD).
package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/infrastructure/websocket"
	"github.com/yourorg/leo-one/internal/interfaces/http/handlers"
	"github.com/yourorg/leo-one/internal/testutil"
)

func TestRunDueSchedules_OneTime_DisablesAfterFiring(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Scheduler OneTime Corp", 10)
	userID := testutil.SeedUser(t, pool, tenantID, "scheduler-onetime@example.com")
	agentID := testutil.SeedAgent(t, pool, tenantID, "scheduler-onetime-agent")

	var scriptID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO scripts (tenant_id, name, interpreter, content, created_by)
		VALUES ($1, 'Scheduler test script', 'bash', 'echo 1', $2)
		RETURNING id
	`, tenantID, userID).Scan(&scriptID); err != nil {
		t.Fatalf("setup script a échoué : %v", err)
	}

	// run_at dans le passé : due immédiatement pour ce test.
	runAt := time.Now().Add(-time.Minute)
	var scheduleID string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO script_schedules (tenant_id, script_id, name, agent_id, run_at, timeout_sec, created_by, next_run_at)
		VALUES ($1, $2, 'Test ponctuel', $3, $4, 60, $5, $4)
		RETURNING id
	`, tenantID, scriptID, agentID, runAt, userID).Scan(&scheduleID); err != nil {
		t.Fatalf("setup schedule a échoué : %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dispatcher := websocket.NewDispatcher(
		postgres.NewAgentRepo(pool), postgres.NewMetricRepo(pool), postgres.NewInventoryRepo(pool), pool, log,
	)
	hub := websocket.NewHub(dispatcher, log)
	dispatcher.SetHub(hub)
	agentHandler := handlers.NewAgentHandler(postgres.NewAgentRepo(pool), pool, hub, nil, "", "")

	runDueSchedules(context.Background(), pool, agentHandler, log)

	var enabled bool
	var lastRunAt *time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT enabled, last_run_at FROM script_schedules WHERE id = $1
	`, scheduleID).Scan(&enabled, &lastRunAt); err != nil {
		t.Fatalf("vérification a échoué : %v", err)
	}
	if enabled {
		t.Error("planification ponctuelle encore activée après son déclenchement — attendu désactivée")
	}
	if lastRunAt == nil {
		t.Error("last_run_at non renseigné après déclenchement")
	}

	var cmdCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT count(*) FROM commands WHERE schedule_id = $1
	`, scheduleID).Scan(&cmdCount); err != nil {
		t.Fatalf("vérification commands a échoué : %v", err)
	}
	if cmdCount != 1 {
		t.Errorf("count(commands) = %d, attendu 1", cmdCount)
	}
}
