#!/usr/bin/env bash
# Leo-One RMM — Helper de démarrage de l'environnement de développement
#
# Usage : ./scripts/dev.sh [postgres|backend|frontend|all]
#
#   postgres   Lance uniquement PostgreSQL (Docker)
#   backend    Lance le serveur Go directement (sans Docker)
#   frontend   Lance Vite en mode dev (sans Docker)
#   all        Lance PostgreSQL + affiche les commandes pour backend/frontend
#              (défaut si aucun argument fourni)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# ── Crée .env si inexistant ─────────────────────────────────────
if [[ ! -f "$PROJECT_ROOT/.env" ]]; then
    cp "$PROJECT_ROOT/.env.example" "$PROJECT_ROOT/.env"
    echo "✓ .env créé depuis .env.example (pense à le personnaliser)"
fi

# ── Dispatch ────────────────────────────────────────────────────
case "${1:-all}" in

    postgres)
        echo "Démarrage de PostgreSQL..."
        docker compose -f "$PROJECT_ROOT/docker-compose.yml" up -d postgres
        echo "✓ PostgreSQL disponible sur localhost:5432"
        echo "  DB: leo_one  User: leo  Password: leo_dev"
        ;;

    backend)
        echo "Démarrage du backend Go..."
        # Charge les variables d'environnement depuis .env
        set -a
        # shellcheck disable=SC1091
        source "$PROJECT_ROOT/.env" 2>/dev/null || true
        set +a
        cd "$PROJECT_ROOT/backend"
        exec go run ./cmd/server
        ;;

    frontend)
        echo "Démarrage du frontend Vite..."
        cd "$PROJECT_ROOT/frontend"
        exec npm run dev
        ;;

    all)
        echo "Démarrage de l'environnement de dev Leo-One..."
        docker compose -f "$PROJECT_ROOT/docker-compose.yml" up -d postgres
        echo "✓ PostgreSQL démarré sur localhost:5432"
        echo ""
        echo "Lance ensuite dans des terminaux séparés :"
        echo "  ./scripts/dev.sh backend    # Serveur Go (port 8080 / 8081)"
        echo "  ./scripts/dev.sh frontend   # Vite dev server (port 5173)"
        ;;

    *)
        echo "Usage : $0 [postgres|backend|frontend|all]" >&2
        exit 1
        ;;
esac
