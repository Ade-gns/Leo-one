# Leo-One — Serveur (backend + interface web)

Le serveur regroupe trois composants : l'API REST + passerelle WebSocket
(Go, `backend/`), l'interface d'administration (React, `frontend/`), et
PostgreSQL + TimescaleDB pour le stockage.

Ce dossier ne contient pas de copie du code — il documente comment déployer
ce qui se trouve à la racine du dépôt (`backend/`, `frontend/`,
`docker-compose.yml`), qui reste la source unique.

## Prérequis

- Docker + le plugin Docker Compose (`docker compose version`)
- Un nom de domaine ou une IP joignable par vos futurs agents/postes clients
  (les agents s'y connectent en WSS — voir la section TLS plus bas)

## Démarrage rapide (Docker)

Depuis la racine du dépôt :

```bash
cp .env.example .env
# Éditer .env : au minimum, changer JWT_SECRET (32+ caractères aléatoires)

docker compose --profile full up -d
```

Ça démarre PostgreSQL+TimescaleDB, applique les migrations automatiquement
(premier démarrage uniquement — voir `infra/docker/postgres/`), puis
démarre le backend (ports 8080 API REST / 8081 WebSocket agents) et le
frontend (port 5173).

Vérifier que tout tourne :

```bash
docker compose ps
docker compose logs -f backend
```

## Créer le premier compte administrateur

Il n'y a pas encore d'inscription en libre-service : le premier tenant
(organisation) et son compte admin se créent directement en base, une seule
fois. Les tenants suivants (si vous gérez plusieurs organisations) suivent
la même procédure.

**1. Générer le hash du mot de passe** (argon2id — le format exact attendu
par le backend) :

```bash
cd backend
cat > /tmp/leo-hash.go <<'EOF'
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

func main() {
	salt := make([]byte, 16)
	rand.Read(salt)
	hash := argon2.IDKey([]byte(os.Args[1]), salt, 3, 65536, 2, 32)
	fmt.Printf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s\n",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
}
EOF
go run /tmp/leo-hash.go 'VotreMotDePasseSolide'
```

Copiez le hash affiché (commence par `$argon2id$...`).

**2. Créer le tenant, le compte, et lui attribuer le rôle Admin** — via
`psql` (adapter la connexion : `docker compose exec postgres psql -U leo -d
leo_one`, ou `psql postgres://leo:leo_dev@localhost:5432/leo_one` en accès
direct) :

```sql
INSERT INTO tenants (name, slug) VALUES ('Mon Entreprise', 'mon-entreprise')
RETURNING id;
-- notez l'id retourné : TENANT_ID

SELECT seed_system_roles('TENANT_ID');  -- crée les rôles Admin/Technicien/Lecture seule

INSERT INTO users (tenant_id, email, password_hash, full_name)
VALUES ('TENANT_ID', 'admin@example.com', 'LE_HASH_COLLE_ICI', 'Administrateur')
RETURNING id;
-- notez l'id retourné : USER_ID

INSERT INTO user_roles (user_id, role_id)
SELECT 'USER_ID', id FROM roles WHERE tenant_id = 'TENANT_ID' AND name = 'Admin';
```

**3. Se connecter** sur `http://<votre-serveur>:5173` avec l'email et le mot
de passe choisis.

## Générer un token d'enrollment pour un agent

Une fois connecté : **Machines → Générer un token d'enrollment**. Le token
est à usage unique et de courte durée — à utiliser immédiatement dans la
configuration de l'agent (voir `release/windows-client/` ou
`release/linux-client/`).

## Passer en production

La configuration ci-dessus est pensée pour un premier déploiement/test :

- **`JWT_SECRET`** (dans `.env`) — à changer obligatoirement, sinon les
  sessions ne sont pas sûres.
- **Mot de passe PostgreSQL** — `docker-compose.yml` le fixe en dur
  (`leo`/`leo_dev`) pour le confort en développement. Pour le changer,
  éditez le bloc `environment:` du service `postgres` dans
  `docker-compose.yml` **et** mettez à jour `DATABASE_URL` en conséquence
  (dans `.env` et dans le bloc `environment:` du service `backend`).
- **TLS** — le backend écoute en clair (HTTP/WS) sur 8080/8081 et le
  frontend sert du HTTP simple sur 5173 ; en production, placez tout ça
  derrière un reverse proxy (nginx/Caddy/Traefik) qui termine le TLS. C'est
  important en particulier pour le port 8081 (WSS) : les agents refusent de
  se connecter en clair (mTLS/pinning de certificat requis côté agent).
- **Ne pas exposer directement** le port PostgreSQL (5432) à Internet — il
  n'est utile qu'en local/debug.

## Alternative : lancer sans Docker (développement)

Voir `scripts/dev.sh` à la racine du dépôt (`./scripts/dev.sh postgres`,
`./scripts/dev.sh backend`, `./scripts/dev.sh frontend`) — utile si vous
développez sur le backend/frontend plutôt que de simplement déployer.
