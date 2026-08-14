# Leo-One

Leo-One est un outil de supervision et de gestion à distance de parc
informatique (RMM — Remote Monitoring & Management), dans l'esprit
d'Action1/NinjaOne/ConnectWise Automate : un agent léger installé sur chaque
poste remonte métriques et inventaire, et permet l'exécution de commandes à
distance (scripts, installation de paquets, redémarrage), le tout piloté
depuis une interface web centralisée.

## Envie de l'installer ?

→ **[`release/`](release/README.md)** — packages prêts à l'emploi : serveur
(Docker) + agent Windows précompilé + agent Linux précompilé, avec un guide
d'installation pas à pas pour chacun.

## Architecture

| Composant | Techno | Rôle |
|---|---|---|
| `agent/` | C (C11), cross-platform | Tourne sur chaque poste supervisé : métriques, inventaire, exécution de commandes, connexion mTLS au serveur |
| `backend/` | Go | API REST + passerelle WebSocket agents, auth/RBAC/MFA |
| `frontend/` | React + TypeScript + Vite | Interface d'administration web |
| PostgreSQL + TimescaleDB | — | Stockage (métriques en séries temporelles, configuration, RBAC) |

Toutes les communications sont chiffrées (TLS 1.3, mTLS pour les agents —
chaque agent a son propre certificat client émis par une CA interne au
premier enrollment).

## Développement

Ce dépôt inclut un environnement de développement complet (Docker Compose,
scripts de démarrage, migrations SQL) :

```bash
./scripts/dev.sh          # PostgreSQL via Docker + instructions backend/frontend
./scripts/dev.sh backend  # lance le backend Go directement
./scripts/dev.sh frontend # lance Vite en mode dev
```

Voir `docker-compose.yml` et `.env.example` à la racine. Pour compiler
l'agent depuis les sources (Linux natif ou cross-compilation Windows via
mingw-w64), voir `agent/CMakeLists.txt`.

Documentation de conception (architecture, modèle de données, wireframes) :
`docs/`.
