// Package scheduler exécute en tâche de fond les planifications récurrentes
// de scripts ("script_schedules") : interroge périodiquement les échéances
// dues, résout les agents cible (agent unique ou workspace entier), et
// déclenche une commande exec_script pour chacun — en réutilisant le même
// chemin d'insertion/dispatch que l'exécution ad-hoc (voir
// handlers.AgentHandler.CreateAndDispatchCommand), pour bénéficier de la
// même logique d'envoi immédiat si l'agent est en ligne, et de la
// redélivrance automatique à la reconnexion sinon (voir
// infrastructure/websocket/dispatcher.go, handleHello).
package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"

	"github.com/yourorg/leo-one/internal/interfaces/http/handlers"
)

const pollInterval = 30 * time.Second

// Run interroge périodiquement les planifications dues et déclenche les
// commandes correspondantes. Bloquant — à lancer dans sa propre goroutine.
// S'arrête proprement quand ctx est annulé.
func Run(ctx context.Context, pool *pgxpool.Pool, agentHandler *handlers.AgentHandler, log *slog.Logger) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Info("Scheduler de scripts démarré", "poll_interval", pollInterval)

	for {
		select {
		case <-ctx.Done():
			log.Info("Scheduler de scripts arrêté")
			return
		case <-ticker.C:
			runDueSchedules(ctx, pool, agentHandler, log)
		}
	}
}

type dueSchedule struct {
	id             string
	tenantID       string
	scriptID       string
	agentID        *string
	workspaceID    *string
	cronExpression *string // nil pour une planification ponctuelle (run_at)
	timeoutSec     int
}

func runDueSchedules(ctx context.Context, pool *pgxpool.Pool, agentHandler *handlers.AgentHandler, log *slog.Logger) {
	rows, err := pool.Query(ctx, `
		SELECT id, tenant_id, script_id, agent_id, workspace_id, cron_expression, timeout_sec
		FROM script_schedules
		WHERE enabled AND next_run_at <= NOW()
	`)
	if err != nil {
		log.Error("scheduler : échec lecture des planifications dues", "error", err)
		return
	}

	var due []dueSchedule
	for rows.Next() {
		var d dueSchedule
		if err := rows.Scan(
			&d.id, &d.tenantID, &d.scriptID, &d.agentID, &d.workspaceID, &d.cronExpression, &d.timeoutSec,
		); err != nil {
			log.Error("scheduler : échec lecture d'une planification", "error", err)
			continue
		}
		due = append(due, d)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		log.Error("scheduler : échec lecture des planifications dues", "error", rowsErr)
		return
	}

	for _, d := range due {
		fireSchedule(ctx, pool, agentHandler, log, d)
	}
}

// fireSchedule déclenche une échéance : charge le script, résout les agents
// cible, crée une commande par agent, puis avance systématiquement
// next_run_at — même en cas d'erreur, pour ne jamais re-déclencher en boucle
// serrée une planification cassée (script supprimé, cron devenu invalide…).
func fireSchedule(ctx context.Context, pool *pgxpool.Pool, agentHandler *handlers.AgentHandler, log *slog.Logger, d dueSchedule) {
	log = log.With("schedule_id", d.id, "tenant_id", d.tenantID)
	defer advanceNextRun(ctx, pool, log, d)

	var interpreter, content string
	if err := pool.QueryRow(ctx, `
		SELECT interpreter, content FROM scripts WHERE id = $1
	`, d.scriptID).Scan(&interpreter, &content); err != nil {
		log.Error("scheduler : script introuvable, échéance ignorée", "error", err)
		return
	}

	payload, err := json.Marshal(map[string]any{
		"interpreter": interpreter,
		"script":      content,
		"timeout_sec": d.timeoutSec,
	})
	if err != nil {
		log.Error("scheduler : échec sérialisation du payload", "error", err)
		return
	}

	targetIDs, err := resolveTargets(ctx, pool, d)
	if err != nil {
		log.Error("scheduler : échec résolution des agents cible", "error", err)
		return
	}
	if len(targetIDs) == 0 {
		log.Warn("scheduler : aucun agent cible pour cette échéance")
		return
	}

	scheduleID := d.id
	sentCount := 0
	for _, agentID := range targetIDs {
		if _, _, err := agentHandler.CreateAndDispatchCommand(
			ctx, d.tenantID, agentID, nil, &scheduleID, "exec_script", payload,
		); err != nil {
			log.Error("scheduler : échec création de la commande", "agent_id", agentID, "error", err)
			continue
		}
		sentCount++
	}

	log.Info("scheduler : planification déclenchée", "agents_ciblés", len(targetIDs), "commandes_créées", sentCount)
}

func resolveTargets(ctx context.Context, pool *pgxpool.Pool, d dueSchedule) ([]string, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if d.agentID != nil {
		rows, err = pool.Query(ctx, `SELECT id FROM agents WHERE id = $1 AND tenant_id = $2`, *d.agentID, d.tenantID)
	} else {
		rows, err = pool.Query(ctx, `SELECT id FROM agents WHERE workspace_id = $1 AND tenant_id = $2`, *d.workspaceID, d.tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// advanceNextRun met à jour la planification après un déclenchement.
// Ponctuelle (cronExpression nil, run_at) : désactivée — un seul
// déclenchement, jamais reprogrammée ; last_run_at reste consultable comme
// historique plutôt que de supprimer la ligne. Récurrente : next_run_at
// recalculé à partir de maintenant. Un cron_expression devenu invalide (ne
// devrait pas arriver — validé à la création/modification — mais defense in
// depth) désactive la planification plutôt que de la laisser se
// re-déclencher indéfiniment.
func advanceNextRun(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, d dueSchedule) {
	if d.cronExpression == nil {
		if _, err := pool.Exec(ctx, `
			UPDATE script_schedules SET enabled = FALSE, last_run_at = NOW() WHERE id = $1
		`, d.id); err != nil {
			log.Error("scheduler : échec désactivation après exécution ponctuelle", "error", err)
		}
		return
	}

	next, err := computeNextRun(*d.cronExpression, time.Now())
	if err != nil {
		log.Error("scheduler : cron_expression invalide, planification désactivée", "error", err)
		_, _ = pool.Exec(ctx, `UPDATE script_schedules SET enabled = FALSE WHERE id = $1`, d.id)
		return
	}

	if _, err := pool.Exec(ctx, `
		UPDATE script_schedules SET next_run_at = $1, last_run_at = NOW() WHERE id = $2
	`, next, d.id); err != nil {
		log.Error("scheduler : échec mise à jour next_run_at", "error", err)
	}
}

// computeNextRun calcule la prochaine échéance d'une expression cron
// standard (5 champs) après `from`. Extrait en fonction pure pour être
// testable sans base de données.
func computeNextRun(cronExpression string, from time.Time) (time.Time, error) {
	schedule, err := cron.ParseStandard(cronExpression)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(from), nil
}
