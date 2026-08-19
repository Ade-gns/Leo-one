# Leo-One

Leo-One est un outil de supervision et de gestion à distance de parc
informatique (RMM — Remote Monitoring & Management), dans l'esprit
d'Action1/NinjaOne/ConnectWise Automate : un agent léger installé sur chaque
poste remonte métriques et inventaire, et permet l'exécution de commandes à
distance (scripts, installation de paquets, redémarrage), le tout piloté
depuis une interface web centralisée.

## Fonctionnalités

- **Métriques temps réel** — CPU, RAM, disque, réseau (séries temporelles
  via TimescaleDB) et inventaire matériel/logiciel de chaque poste.
- **Bureau à distance** — vue live de l'écran d'un agent (streaming JPEG) en
  lecture seule ou avec prise de contrôle clavier/souris.
- **Transfert de fichiers** — bibliothèque de fichiers déployables
  (installeurs, configs) envoyés à la demande sur un ou plusieurs postes.
- **Gestion des mises à jour** — patch management Windows Update / apt-dnf,
  visibilité sur les postes à jour ou non.
- **Scripts à distance** — bibliothèque de scripts, exécution ponctuelle ou
  planifications récurrentes (cron).
- **Alertes** — règles configurables (seuils métriques, statut agent…),
  historique et accusé de réception.
- **Workspaces** — regroupement des machines par site/client/usage.
- **Comptes, rôles et permissions (RBAC)** — multi-tenant, rôles Admin /
  Technicien / Lecture seule prédéfinis, rôles personnalisés par permission
  fine (`ressource:action`).
- **Journal d'audit** — historique des actions sensibles, réservé aux
  administrateurs.
- **Sécurité** — mTLS pour chaque agent (certificat client propre, émis par
  une CA interne à l'enrollment), JWT + rate limiting sur les routes
  d'authentification, MFA disponible sur les comptes utilisateurs.
- **Tableau de bord** — vue d'ensemble agrégée du parc (statuts, alertes
  actives, patchs en attente…).

## Installation

Ordre : déployez d'abord le serveur (il faut un token d'enrollment et une
adresse joignable avant de pouvoir installer le moindre agent), puis
installez l'agent sur chaque poste à superviser.

### 1. Serveur

**Prérequis** : Docker + le plugin Docker Compose (`docker compose version`),
et un nom de domaine ou une IP joignable par les futurs agents/postes clients.

```bash
git clone <url-du-dépôt> leo-one && cd leo-one
cp .env.example .env
# Éditer .env : changer au minimum JWT_SECRET (32+ caractères aléatoires)

docker compose --profile full up -d
```

Ça démarre PostgreSQL+TimescaleDB (migrations appliquées automatiquement au
premier démarrage), le backend (API REST sur 8080, WebSocket agents sur
8081) et le frontend (port 5173). Vérifier que tout tourne :

```bash
docker compose ps
docker compose logs -f backend
```

**Créer le premier compte administrateur** — pas d'inscription en
libre-service : le premier tenant (organisation) et son compte admin se
créent une seule fois, directement en base. Procédure détaillée (génération
du hash de mot de passe argon2id, requêtes SQL exactes) :
**[`release/server/README.md`](release/server/README.md#créer-le-premier-compte-administrateur)**.

Se connecter ensuite sur `http://<votre-serveur>:5173`, puis **Machines →
Enrôler un agent → Générer un token** pour préparer l'installation du
premier agent.

Pour un déploiement en production (TLS derrière un reverse proxy, mot de
passe PostgreSQL, `PUBLIC_API_URL`/`PUBLIC_VIEWER_WS_URL`…), voir la section
dédiée dans **[`release/server/README.md`](release/server/README.md#passer-en-production)**.

### 2. Agents

| Composant | À installer sur | Prérequis |
|---|---|---|
| [Agent Windows](release/windows-client/README.md) | Chaque poste Windows à superviser | Windows 7 ou plus récent, 64 bits — binaire autonome (lié statiquement), rien d'autre à installer |
| [Agent Linux](release/linux-client/README.md) | Chaque machine Linux à superviser | Ubuntu/Debian x86-64 glibc + `libx11-6 libxext6 libxtst6 libturbojpeg0` (le binaire ne démarre pas sans elles, même hors usage du bureau à distance) — voir le détail dans son README |

Chaque guide détaille la copie du binaire, la configuration du token
d'enrollment (`agent_bootstrap.conf`), et l'installation comme service
(systemd / service Windows) pour un démarrage automatique.

→ Vue d'ensemble complète des trois packages : **[`release/`](release/README.md)**.

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
