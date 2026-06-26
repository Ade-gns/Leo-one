#!/bin/bash
# Leo-One RMM — Application des migrations SQL dans l'ordre
# Ce script s'exécute automatiquement via /docker-entrypoint-initdb.d/
# Il est appelé après 01_init.sql (extensions TimescaleDB + pgcrypto déjà actives)

set -e

echo "Leo-One: application des migrations depuis /migrations/"

for f in $(ls /migrations/*.sql | sort); do
    echo "  -> $f"
    psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f "$f"
done

echo "Leo-One: migrations appliquées avec succès."
