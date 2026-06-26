-- Leo-One RMM — Extensions PostgreSQL
-- Ce script s'exécute en premier (ordre alphabétique dans initdb.d)
-- TimescaleDB DOIT être activé avant les migrations (002_timescaledb_hypertables.sql)

CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
