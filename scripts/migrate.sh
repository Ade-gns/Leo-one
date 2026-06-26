#!/usr/bin/env bash
# Leo-One RMM — Application manuelle des migrations SQL
#
# Usage : ./scripts/migrate.sh [DATABASE_URL]
#
# Si DATABASE_URL n'est pas passé en argument, il est lu depuis .env
# ou depuis la variable d'environnement courante.
#
# Exemples :
#   ./scripts/migrate.sh
#   ./scripts/migrate.sh "postgres://leo:leo_dev@localhost:5432/leo_one?sslmode=disable"
#   DATABASE_URL=postgres://... ./scripts/migrate.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
MIGRATIONS_DIR="$PROJECT_ROOT/backend/migrations"

# ── Résolution de DATABASE_URL ──────────────────────────────────
if [[ -n "${1:-}" ]]; then
    DATABASE_URL="$1"
elif [[ -z "${DATABASE_URL:-}" ]]; then
    # Charge depuis .env si disponible
    if [[ -f "$PROJECT_ROOT/.env" ]]; then
        # shellcheck disable=SC1091
        set -a
        source "$PROJECT_ROOT/.env"
        set +a
    fi
fi

if [[ -z "${DATABASE_URL:-}" ]]; then
    echo "Erreur : DATABASE_URL non définie." >&2
    echo "Usage : $0 [DATABASE_URL]" >&2
    echo "  ou bien : export DATABASE_URL=... && $0" >&2
    exit 1
fi

# ── Vérification que psql est disponible ────────────────────────
if ! command -v psql &>/dev/null; then
    echo "Erreur : psql n'est pas installé ou n'est pas dans le PATH." >&2
    echo "  Sous Ubuntu/Debian : sudo apt install postgresql-client" >&2
    echo "  Ou lance les migrations via Docker : docker compose up -d postgres" >&2
    exit 1
fi

# ── Application des migrations dans l'ordre ─────────────────────
echo "Application des migrations Leo-One..."
echo "  DB : $DATABASE_URL"
echo "  Dossier : $MIGRATIONS_DIR"
echo ""

for f in $(ls "$MIGRATIONS_DIR"/*.sql | sort); do
    echo "  -> $(basename "$f")"
    psql "$DATABASE_URL" -f "$f"
done

echo ""
echo "✓ Migrations appliquées avec succès."
