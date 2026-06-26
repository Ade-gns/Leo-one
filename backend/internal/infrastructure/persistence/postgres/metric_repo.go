package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	metricDomain "github.com/yourorg/leo-one/internal/domain/metric"
)

// MetricRepo implémente metric.Repository via pgx/v5 et TimescaleDB.
type MetricRepo struct {
	pool *pgxpool.Pool
}

// NewMetricRepo crée un MetricRepo avec le pool de connexions fourni.
func NewMetricRepo(pool *pgxpool.Pool) *MetricRepo {
	return &MetricRepo{pool: pool}
}

// InsertBatch insère plusieurs points en utilisant pgx COPY pour de meilleures performances.
func (r *MetricRepo) InsertBatch(ctx context.Context, points []metricDomain.Point) error {
	ctx = ensureCtx(ctx)

	if len(points) == 0 {
		return nil
	}

	// Préparer les lignes pour COPY
	rows := make([][]any, len(points))
	for i, p := range points {
		rows[i] = []any{
			p.Time,
			p.AgentID,
			p.TenantID,
			string(p.Type),
			p.Value,
			nil, // labels : nil → NULL en BDD (JSONB)
		}
	}

	// Acquérir une connexion pour utiliser CopyFrom
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	_, err = conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{"metrics"},
		[]string{"time", "agent_id", "tenant_id", "type", "value", "labels"},
		pgx.CopyFromRows(rows),
	)

	return err
}

// Query retourne les métriques d'un agent sur une plage de temps.
// La résolution est choisie automatiquement via metricDomain.ChooseResolution.
func (r *MetricRepo) Query(
	ctx context.Context,
	tenantID, agentID string,
	metricType metricDomain.Type,
	from, to time.Time,
) ([]metricDomain.QueryResult, metricDomain.Resolution, error) {
	ctx = ensureCtx(ctx)

	resolution := metricDomain.ChooseResolution(from, to)

	var query string
	switch resolution {
	case metricDomain.ResolutionRaw:
		query = `
			SELECT time, value, value AS avg_val, value AS max_val, value AS min_val
			FROM metrics
			WHERE tenant_id = $1 AND agent_id = $2 AND type = $3::metric_type
			  AND time >= $4 AND time <= $5
			ORDER BY time ASC
		`
	case metricDomain.Resolution1h:
		query = `
			SELECT time_bucket('1 hour', time) AS bucket,
			       AVG(value)  AS value,
			       AVG(value)  AS avg_val,
			       MAX(value)  AS max_val,
			       MIN(value)  AS min_val
			FROM metrics
			WHERE tenant_id = $1 AND agent_id = $2 AND type = $3::metric_type
			  AND time >= $4 AND time <= $5
			GROUP BY bucket
			ORDER BY bucket ASC
		`
	default: // Resolution1d
		query = `
			SELECT time_bucket('1 day', time) AS bucket,
			       AVG(value)  AS value,
			       AVG(value)  AS avg_val,
			       MAX(value)  AS max_val,
			       MIN(value)  AS min_val
			FROM metrics
			WHERE tenant_id = $1 AND agent_id = $2 AND type = $3::metric_type
			  AND time >= $4 AND time <= $5
			GROUP BY bucket
			ORDER BY bucket ASC
		`
	}

	rows, err := r.pool.Query(ctx, query, tenantID, agentID, string(metricType), from, to)
	if err != nil {
		return nil, resolution, err
	}
	defer rows.Close()

	results := make([]metricDomain.QueryResult, 0)
	for rows.Next() {
		var qr metricDomain.QueryResult
		if err := rows.Scan(&qr.Time, &qr.Value, &qr.AvgValue, &qr.MaxValue, &qr.MinValue); err != nil {
			return nil, resolution, err
		}
		results = append(results, qr)
	}

	if rows.Err() != nil {
		return nil, resolution, rows.Err()
	}

	return results, resolution, nil
}

// Latest retourne la dernière valeur connue pour chaque type de métrique d'un agent.
func (r *MetricRepo) Latest(ctx context.Context, tenantID, agentID string) (map[metricDomain.Type]float64, time.Time, error) {
	ctx = ensureCtx(ctx)

	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (type) type::text, value, time
		FROM metrics
		WHERE tenant_id = $1 AND agent_id = $2
		ORDER BY type, time DESC
	`, tenantID, agentID)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()

	metrics := make(map[metricDomain.Type]float64)
	var latestTime time.Time

	for rows.Next() {
		var typeStr string
		var value float64
		var ts time.Time

		if err := rows.Scan(&typeStr, &value, &ts); err != nil {
			return nil, time.Time{}, err
		}

		metrics[metricDomain.Type(typeStr)] = value
		if ts.After(latestTime) {
			latestTime = ts
		}
	}

	if rows.Err() != nil {
		return nil, time.Time{}, rows.Err()
	}

	return metrics, latestTime, nil
}
