-- =============================================================================
-- Migration 002 : TimescaleDB — Hypertables, Continuous Aggregates, Rétention
-- À exécuter après 001_init_schema.sql
-- =============================================================================

-- ---------------------------------------------------------------------------
-- Hypertable principale : métriques brutes
-- Partitionnée par jour. Données brutes conservées 30 jours.
-- ---------------------------------------------------------------------------
SELECT create_hypertable(
    'metrics',
    'time',
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists       => TRUE
);

-- Index composé pour les requêtes par agent sur une plage de temps
CREATE INDEX idx_metrics_agent_time  ON metrics(agent_id, time DESC);
CREATE INDEX idx_metrics_tenant_time ON metrics(tenant_id, time DESC);
CREATE INDEX idx_metrics_type_time   ON metrics(type, time DESC);

-- ---------------------------------------------------------------------------
-- Continuous Aggregate : agrégation horaire
-- Résolution 1h — conservée 1 an
-- Utilisée pour les graphiques > 24h
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW metrics_1h
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    agent_id,
    tenant_id,
    type,
    AVG(value)   AS avg_value,
    MAX(value)   AS max_value,
    MIN(value)   AS min_value,
    COUNT(*)     AS sample_count
FROM metrics
GROUP BY bucket, agent_id, tenant_id, type
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'metrics_1h',
    start_offset => INTERVAL '2 hours',
    end_offset   => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour'
);

-- ---------------------------------------------------------------------------
-- Continuous Aggregate : agrégation journalière
-- Résolution 1j — conservée 2 ans
-- Utilisée pour les graphiques > 7 jours
-- ---------------------------------------------------------------------------
CREATE MATERIALIZED VIEW metrics_1d
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    agent_id,
    tenant_id,
    type,
    AVG(value)   AS avg_value,
    MAX(value)   AS max_value,
    MIN(value)   AS min_value,
    COUNT(*)     AS sample_count
FROM metrics
GROUP BY bucket, agent_id, tenant_id, type
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'metrics_1d',
    start_offset => INTERVAL '2 days',
    end_offset   => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day'
);

-- ---------------------------------------------------------------------------
-- Politiques de rétention (data lifecycle)
-- ---------------------------------------------------------------------------

-- Métriques brutes : 30 jours
SELECT add_retention_policy('metrics', INTERVAL '30 days');

-- Agrégats horaires : 365 jours
SELECT add_retention_policy('metrics_1h', INTERVAL '365 days');

-- Agrégats journaliers : 730 jours (2 ans)
SELECT add_retention_policy('metrics_1d', INTERVAL '730 days');

-- ---------------------------------------------------------------------------
-- Vue utilitaire : résolution automatique selon la plage demandée
-- Utilisée par le backend Go pour router la requête vers la bonne table.
-- Logique côté application — commentaire de référence uniquement.
--
-- Règle :
--   range <= 6h   → metrics       (résolution brute ~60s)
--   range <= 7j   → metrics_1h    (résolution 1h)
--   range > 7j    → metrics_1d    (résolution 1j)
-- ---------------------------------------------------------------------------
