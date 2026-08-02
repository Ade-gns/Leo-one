# Page Alertes (liste + acquittement) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remplacer les stubs 501 d'alertes par une implémentation réelle (backend + frontend) permettant de lister les alertes déclenchées d'un tenant, de les filtrer par statut/sévérité, et de les acquitter — plus la page React correspondante, accessible depuis la sidebar existante.

**Architecture:** Le backend suit exactement le pattern Clean Architecture déjà en place pour `agent`/`metric` : `internal/domain/alert` (types + interface `Repository`), `internal/infrastructure/persistence/postgres/alert_repo.go` (implémentation pgx), puis `AlertHandler` HTTP qui consomme l'interface (jamais le pool directement). Le frontend suit le pattern `agents` : `types/alert.ts` → `api/alerts.ts` → `hooks/useAlerts.ts` → composants → page, branchée sur la route `/alerts` déjà présente dans la sidebar.

**Tech Stack:** Go 1.23 (chi, pgx/v5), React 18 + TypeScript + Vite, TanStack Query, Tailwind.

## Global Constraints

- Toutes les routes API sont déjà spécifiées dans `backend/internal/interfaces/http/router.go` (commentaires en tête de fichier) — ne pas dévier du contrat documenté (`GET /api/v1/alerts`, `GET /api/v1/alerts/:alertID`, `POST /api/v1/alerts/:alertID/acknowledge`).
- La gestion des règles d'alerte (`ListRules`/`CreateRule`/`UpdateRule`/`DeleteRule`) reste hors périmètre : ces méthodes restent des stubs 501, ne pas les implémenter.
- Format de réponse : succès `{"data": ..., "meta": {...}}` via `response.JSON`/`response.JSONWithMeta`, erreur `{"error": {"code": ..., "message": ...}}` via `response.Error` (package `internal/pkg/response`).
- Isolation multi-tenant : toute requête BDD doit filtrer par `tenant_id` (extrait de `httpctx.TenantIDFromContext`), jamais faire confiance à un `tenant_id` venant du client.
- Pas de code mort : le corps optionnel `{"comment":"..."}` documenté pour `Acknowledge` n'est pas persisté (pas de colonne prévue en base) — ne pas le décoder côté handler, ne pas l'envoyer côté frontend.
- Le projet n'a pas de framework de tests côté frontend (pas de vitest/jest dans `package.json`) — la vérification frontend se fait via `npm run type-check` (tsc) et une vérification visuelle finale dans le navigateur, pas des tests unitaires JS.
- Le projet n'a pas de tests pour la couche repository Postgres (`agent_repo.go`/`metric_repo.go` n'ont pas de fichier de test — nécessiterait une vraie BDD). Ne pas introduire de test pour `alert_repo.go` non plus ; seule la couche handler HTTP est testée (via mocks), comme pour `agent`/`metric`.

### Environnement de dev local (déjà en place dans cette session)

Cette machine n'a ni Docker ni PostgreSQL système. Un environnement local a été construit sans droits root :
- Go 1.23.4 : `~/.local/go/bin` (ajouter au `PATH`)
- PostgreSQL 16 : binaires extraits dans `~/.local/pgsql`, cluster de données dans `~/.local/pgsql-data`, tourne sur le port **5433** (pas 5432), socket Unix `/tmp`, DB `leo_one`, user `leo` / mot de passe `leo_dev`
- Backend Go déjà lancé en arrière-plan (`nohup go run ./cmd/server`, PID dans `/tmp/leo-backend.pid`, logs dans `/tmp/leo-backend.log`), variables : `DATABASE_URL=postgres://leo:leo_dev@localhost:5433/leo_one`
- Frontend Vite déjà lancé en arrière-plan (`nohup npm run dev`, PID dans `/tmp/leo-frontend.pid`, logs dans `/tmp/leo-frontend.log`), sur `http://localhost:5174`
- Un tenant "Demo Corp" et un utilisateur `admin@leo-one.local` / mot de passe `admin123` existent déjà en base

Pour relancer le backend après une modification de code (le serveur ne recharge pas à chaud) :
```bash
kill $(cat /tmp/leo-backend.pid) 2>/dev/null
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
export DATABASE_URL="postgres://leo:leo_dev@localhost:5433/leo_one"
nohup go run ./cmd/server > /tmp/leo-backend.log 2>&1 &
echo $! > /tmp/leo-backend.pid
```

---

## File Structure

- Create: `backend/internal/domain/alert/alert.go` — types `Severity`, `Status`, `Alert`, `ListFilter`, interface `Repository`
- Create: `backend/internal/infrastructure/persistence/postgres/alert_repo.go` — implémentation pgx de `alert.Repository`
- Modify: `backend/internal/interfaces/http/handlers/alert_handler.go` — remplace les stubs `List`/`Get`/`Acknowledge` par une vraie implémentation ; `ListRules`/`CreateRule`/`UpdateRule`/`DeleteRule` restent stubs
- Modify: `backend/internal/interfaces/http/handlers/mocks_test.go` — ajoute `mockAlertRepo`
- Modify: `backend/internal/interfaces/http/handlers/alert_handler_test.go` — réécrit entièrement (tests `List`/`Get`/`Acknowledge` + stubs restants)
- Modify: `backend/internal/interfaces/http/handlers/auth_handler.go` — corrige `ro.name = 'admin'` → `'Admin'` (2 occurrences : `Login`, `Refresh`)
- Modify: `backend/cmd/server/main.go` — instancie `alertRepo` et le passe à `NewAlertHandler`
- Modify: `frontend/src/types/alert.ts` — aligne `AlertSeverity`/`AlertStatus`/`Alert` sur le contrat backend réel
- Create: `frontend/src/api/alerts.ts` — `alertsApi.list`, `alertsApi.acknowledge`
- Create: `frontend/src/hooks/useAlerts.ts` — `useAlerts`, `useAcknowledgeAlert`
- Create: `frontend/src/components/alerts/AlertSeverityBadge.tsx`
- Create: `frontend/src/components/alerts/AlertStatusBadge.tsx`
- Create: `frontend/src/components/alerts/AlertTable.tsx`
- Create: `frontend/src/pages/AlertsPage.tsx`
- Modify: `frontend/src/components/dashboard/RecentAlerts.tsx` — utilise le hook `useAlerts` et les champs corrigés (`title` au lieu de `message`, `status=open` au lieu de `firing`)
- Modify: `frontend/src/App.tsx` — ajoute l'import lazy et la route `/alerts`

---

### Task 1: Domaine + repository Postgres pour les alertes

**Files:**
- Create: `backend/internal/domain/alert/alert.go`
- Create: `backend/internal/infrastructure/persistence/postgres/alert_repo.go`

**Interfaces:**
- Produces: `alert.Severity` (`SeverityInfo`, `SeverityWarning`, `SeverityCritical`), `alert.Status` (`StatusOpen`, `StatusAcknowledged`, `StatusResolved`), `alert.Alert` struct, `alert.ListFilter{Status *Status, Severity *Severity, AgentID *string, Cursor string, Limit int}`, `alert.Repository` interface avec `List(ctx, tenantID string, filter ListFilter) ([]*Alert, string, error)`, `FindByID(ctx, tenantID, alertID string) (*Alert, error)`, `Acknowledge(ctx, tenantID, alertID, userID string) (*Alert, error)`. `postgres.NewAlertRepo(pool *pgxpool.Pool) *AlertRepo` qui implémente `alert.Repository`. Réutilise `ensureCtx` et `itoa`, déjà définis dans `agent_repo.go` (même package `postgres`).
- Consumes: rien (première task du plan).

- [ ] **Step 1: Créer le package domaine**

Fichier `backend/internal/domain/alert/alert.go` :

```go
// Package alert définit l'entité Alert et son interface de domaine.
// Cette couche ne connaît aucune dépendance externe (pas de DB, pas de HTTP).
package alert

import (
	"context"
	"time"
)

// Severity représente le niveau de gravité d'une alerte.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Status représente l'état de traitement d'une alerte.
type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusResolved     Status = "resolved"
)

// Alert est une instance d'alerte déclenchée pour un agent.
type Alert struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	AgentID        string     `json:"agent_id"`
	AgentHostname  string     `json:"agent_hostname"`
	RuleID         *string    `json:"rule_id,omitempty"`
	Severity       Severity   `json:"severity"`
	Status         Status     `json:"status"`
	Title          string     `json:"title"`
	Description    *string    `json:"description,omitempty"`
	MetricValue    *float64   `json:"metric_value,omitempty"`
	TriggeredAt    time.Time  `json:"triggered_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedBy *string    `json:"acknowledged_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ListFilter contient les critères optionnels pour lister les alertes.
type ListFilter struct {
	Status   *Status
	Severity *Severity
	AgentID  *string
	Cursor   string
	Limit    int
}

// Repository définit le contrat de persistance pour les alertes.
// Implémenté dans internal/infrastructure/persistence/postgres/alert_repo.go
type Repository interface {
	// List retourne la liste paginée des alertes d'un tenant.
	List(ctx context.Context, tenantID string, filter ListFilter) ([]*Alert, string, error)

	// FindByID retourne une alerte appartenant au tenant donné (nil si absente).
	FindByID(ctx context.Context, tenantID, alertID string) (*Alert, error)

	// Acknowledge marque une alerte comme acquittée par l'utilisateur donné.
	// Retourne l'alerte mise à jour, ou nil si elle n'existe pas (ou hors tenant).
	Acknowledge(ctx context.Context, tenantID, alertID, userID string) (*Alert, error)
}
```

- [ ] **Step 2: Créer le repository Postgres**

Fichier `backend/internal/infrastructure/persistence/postgres/alert_repo.go` :

```go
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
)

// AlertRepo implémente alert.Repository via pgx/v5.
type AlertRepo struct {
	pool *pgxpool.Pool
}

// NewAlertRepo crée un AlertRepo avec le pool de connexions fourni.
func NewAlertRepo(pool *pgxpool.Pool) *AlertRepo {
	return &AlertRepo{pool: pool}
}

// FindByID retourne une alerte appartenant au tenant donné.
func (r *AlertRepo) FindByID(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
	ctx = ensureCtx(ctx)

	var a alertDomain.Alert
	err := r.pool.QueryRow(ctx, `
		SELECT al.id, al.tenant_id, al.agent_id, ag.hostname, al.rule_id,
		       al.severity::text, al.status::text, al.title, al.description,
		       al.metric_value, al.triggered_at, al.acknowledged_at,
		       al.acknowledged_by, al.resolved_at, al.created_at
		FROM alerts al
		JOIN agents ag ON ag.id = al.agent_id
		WHERE al.id = $1 AND al.tenant_id = $2
	`, alertID, tenantID).Scan(
		&a.ID, &a.TenantID, &a.AgentID, &a.AgentHostname, &a.RuleID,
		&a.Severity, &a.Status, &a.Title, &a.Description,
		&a.MetricValue, &a.TriggeredAt, &a.AcknowledgedAt,
		&a.AcknowledgedBy, &a.ResolvedAt, &a.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &a, nil
}

// List retourne la liste paginée des alertes d'un tenant (cursor-based pagination).
func (r *AlertRepo) List(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
	ctx = ensureCtx(ctx)

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	args := []any{tenantID}
	where := `WHERE al.tenant_id = $1`
	argN := 2

	if filter.Status != nil {
		where += ` AND al.status = $` + itoa(argN) + `::alert_status`
		args = append(args, string(*filter.Status))
		argN++
	}

	if filter.Severity != nil {
		where += ` AND al.severity = $` + itoa(argN) + `::alert_severity`
		args = append(args, string(*filter.Severity))
		argN++
	}

	if filter.AgentID != nil {
		where += ` AND al.agent_id = $` + itoa(argN)
		args = append(args, *filter.AgentID)
		argN++
	}

	if filter.Cursor != "" {
		where += ` AND al.id > $` + itoa(argN)
		args = append(args, filter.Cursor)
		argN++
	}

	args = append(args, limit+1)
	query := `
		SELECT al.id, al.tenant_id, al.agent_id, ag.hostname, al.rule_id,
		       al.severity::text, al.status::text, al.title, al.description,
		       al.metric_value, al.triggered_at, al.acknowledged_at,
		       al.acknowledged_by, al.resolved_at, al.created_at
		FROM alerts al
		JOIN agents ag ON ag.id = al.agent_id
		` + where + `
		ORDER BY al.id ASC
		LIMIT $` + itoa(argN)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	alerts := make([]*alertDomain.Alert, 0, limit)
	for rows.Next() {
		var a alertDomain.Alert
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.AgentID, &a.AgentHostname, &a.RuleID,
			&a.Severity, &a.Status, &a.Title, &a.Description,
			&a.MetricValue, &a.TriggeredAt, &a.AcknowledgedAt,
			&a.AcknowledgedBy, &a.ResolvedAt, &a.CreatedAt,
		); err != nil {
			return nil, "", err
		}
		alerts = append(alerts, &a)
	}
	if rows.Err() != nil {
		return nil, "", rows.Err()
	}

	var nextCursor string
	if len(alerts) > limit {
		nextCursor = alerts[limit-1].ID
		alerts = alerts[:limit]
	}

	return alerts, nextCursor, nil
}

// Acknowledge marque une alerte comme acquittée par l'utilisateur donné.
func (r *AlertRepo) Acknowledge(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
	ctx = ensureCtx(ctx)

	tag, err := r.pool.Exec(ctx, `
		UPDATE alerts
		SET status = 'acknowledged', acknowledged_at = NOW(), acknowledged_by = $1
		WHERE id = $2 AND tenant_id = $3
	`, userID, alertID, tenantID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}

	return r.FindByID(ctx, tenantID, alertID)
}

var _ alertDomain.Repository = (*AlertRepo)(nil)
```

- [ ] **Step 3: Vérifier la compilation**

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
go build ./... && go vet ./...
```
Expected: aucune sortie (succès).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/domain/alert/alert.go backend/internal/infrastructure/persistence/postgres/alert_repo.go
git commit -m "$(cat <<'EOF'
feat: domaine et repository Postgres pour les alertes

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Correction du bug is_admin (casse du rôle)

**Files:**
- Modify: `backend/internal/interfaces/http/handlers/auth_handler.go:89` et `:186`

**Interfaces:**
- Consumes: rien.
- Produces: rien (correctif de comportement, pas de nouvelle API).

**Contexte :** `seed_system_roles()` (migration 003) crée un rôle nommé `'Admin'` (majuscule). Les requêtes SQL de `Login` et `Refresh` comparent `ro.name = 'admin'` (minuscule) — comparaison sensible à la casse en Postgres, donc `is_admin` est toujours `false`. Or `RequirePermission` (middleware.go) n'autorise les actions d'écriture qu'aux `is_admin=true` : sans ce correctif, `POST /api/v1/alerts/:id/acknowledge` retournera 403 même pour un admin réel.

- [ ] **Step 1: Corriger la requête dans `Login`**

Dans `backend/internal/interfaces/http/handlers/auth_handler.go`, remplacer :
```go
	err := h.pool.QueryRow(r.Context(), `
		SELECT u.id, u.tenant_id, u.password_hash, u.is_active,
		       EXISTS(
		         SELECT 1 FROM user_roles ur
		         JOIN roles ro ON ro.id = ur.role_id
		         WHERE ur.user_id = u.id AND ro.name = 'admin' AND ro.is_system = true
		       ) AS is_admin
		FROM users u
		WHERE u.email = $1
	`, req.Email).Scan(&u.ID, &u.TenantID, &u.PasswordHash, &u.IsActive, &u.IsAdmin)
```
par :
```go
	err := h.pool.QueryRow(r.Context(), `
		SELECT u.id, u.tenant_id, u.password_hash, u.is_active,
		       EXISTS(
		         SELECT 1 FROM user_roles ur
		         JOIN roles ro ON ro.id = ur.role_id
		         WHERE ur.user_id = u.id AND ro.name = 'Admin' AND ro.is_system = true
		       ) AS is_admin
		FROM users u
		WHERE u.email = $1
	`, req.Email).Scan(&u.ID, &u.TenantID, &u.PasswordHash, &u.IsActive, &u.IsAdmin)
```

- [ ] **Step 2: Corriger la requête dans `Refresh`**

Dans le même fichier, remplacer :
```go
	err = h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
		  SELECT 1 FROM user_roles ur
		  JOIN roles ro ON ro.id = ur.role_id
		  WHERE ur.user_id = u.id AND ro.name = 'admin' AND ro.is_system = true
		)
		FROM users u WHERE u.id = $1 AND u.is_active = true
	`, userID).Scan(&isAdmin)
```
par :
```go
	err = h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
		  SELECT 1 FROM user_roles ur
		  JOIN roles ro ON ro.id = ur.role_id
		  WHERE ur.user_id = u.id AND ro.name = 'Admin' AND ro.is_system = true
		)
		FROM users u WHERE u.id = $1 AND u.is_active = true
	`, userID).Scan(&isAdmin)
```

- [ ] **Step 3: Vérifier la compilation**

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
go build ./... && go vet ./...
```
Expected: aucune sortie.

- [ ] **Step 4: Vérification manuelle (nécessite le backend relancé — voir Task 7)**

Ce correctif ne peut être vérifié qu'après redémarrage du serveur (Task 7). Ne pas bloquer dessus ici ; le `go build` suffit pour ce commit.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/interfaces/http/handlers/auth_handler.go
git commit -m "$(cat <<'EOF'
fix: is_admin toujours false — comparaison de casse sur le nom du rôle

Le rôle système seedé s'appelle 'Admin' (migration 003) mais Login et
Refresh comparaient 'admin' en minuscule. Postgres compare les chaînes
de façon sensible à la casse, donc RequirePermission bloquait toutes
les actions d'écriture pour un admin réel.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: AlertHandler réel + tests + câblage

**Files:**
- Modify: `backend/internal/interfaces/http/handlers/alert_handler.go`
- Modify: `backend/internal/interfaces/http/handlers/mocks_test.go`
- Modify: `backend/internal/interfaces/http/handlers/alert_handler_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `alertDomain.Repository`, `alertDomain.Alert`, `alertDomain.ListFilter`, `alertDomain.Status`, `alertDomain.Severity` (Task 1) ; `httpctx.TenantIDFromContext`, `httpctx.UserIDFromContext`, `httpctx.WithTenantID`, `httpctx.WithUserID` (existants) ; `response.JSON`, `response.JSONWithMeta`, `response.Error` (existants) ; helpers de test `withURLParam`, `decodeEnvelope` (définis dans `agent_handler_test.go`, même package `handlers`).
- Produces: `handlers.NewAlertHandler(repo alertDomain.Repository) *AlertHandler` (signature changée — `main.go` doit être mis à jour).

- [ ] **Step 1: Ajouter `mockAlertRepo` dans `mocks_test.go`**

Ouvrir `backend/internal/interfaces/http/handlers/mocks_test.go`, ajouter à l'import :
```go
	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
```
(sur sa propre ligne, à côté des imports `agentDomain`/`metricDomain` existants), puis ajouter à la fin du fichier :

```go
// ─── mockAlertRepo ──────────────────────────────────────────────────────────

type mockAlertRepo struct {
	listFunc        func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error)
	findByIDFunc    func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error)
	acknowledgeFunc func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error)
}

func (m *mockAlertRepo) List(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, tenantID, filter)
	}
	return nil, "", nil
}

func (m *mockAlertRepo) FindByID(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(ctx, tenantID, alertID)
	}
	return nil, nil
}

func (m *mockAlertRepo) Acknowledge(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
	if m.acknowledgeFunc != nil {
		return m.acknowledgeFunc(ctx, tenantID, alertID, userID)
	}
	return nil, nil
}

var _ alertDomain.Repository = (*mockAlertRepo)(nil)
```

- [ ] **Step 2: Réécrire `alert_handler_test.go` (les tests, avant l'implémentation)**

Remplacer tout le contenu de `backend/internal/interfaces/http/handlers/alert_handler_test.go` par :

```go
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
)

func TestAlertHandler_List(t *testing.T) {
	t.Run("tenant_id manquant retourne 401", func(t *testing.T) {
		h := NewAlertHandler(&mockAlertRepo{})
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("succès retourne la liste", func(t *testing.T) {
		repo := &mockAlertRepo{
			listFunc: func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
				if tenantID != "tenant-1" {
					t.Errorf("tenantID = %q, attendu tenant-1", tenantID)
				}
				return []*alertDomain.Alert{{ID: "a1", TenantID: tenantID}}, "next-cursor", nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
		env := decodeEnvelope(t, rec.Body)
		meta, ok := env["meta"].(map[string]any)
		if !ok {
			t.Fatalf("meta absent ou invalide: %v", env)
		}
		if meta["cursor"] != "next-cursor" {
			t.Errorf("cursor = %v, attendu next-cursor", meta["cursor"])
		}
	})

	t.Run("filtres status/severity/agent_id transmis au repo", func(t *testing.T) {
		var gotFilter alertDomain.ListFilter
		repo := &mockAlertRepo{
			listFunc: func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
				gotFilter = filter
				return nil, "", nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?status=open&severity=critical&agent_id=a1", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if gotFilter.Status == nil || *gotFilter.Status != alertDomain.StatusOpen {
			t.Errorf("Status = %v, attendu open", gotFilter.Status)
		}
		if gotFilter.Severity == nil || *gotFilter.Severity != alertDomain.SeverityCritical {
			t.Errorf("Severity = %v, attendu critical", gotFilter.Severity)
		}
		if gotFilter.AgentID == nil || *gotFilter.AgentID != "a1" {
			t.Errorf("AgentID = %v, attendu a1", gotFilter.AgentID)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAlertRepo{
			listFunc: func(ctx context.Context, tenantID string, filter alertDomain.ListFilter) ([]*alertDomain.Alert, string, error) {
				return nil, "", errors.New("boom")
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
		req = req.WithContext(httpctx.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		h.List(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAlertHandler_Get(t *testing.T) {
	t.Run("trouvé retourne 200", func(t *testing.T) {
		repo := &mockAlertRepo{
			findByIDFunc: func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
				return &alertDomain.Alert{ID: alertID, TenantID: tenantID}, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/al1", nil)
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("introuvable retourne 404", func(t *testing.T) {
		repo := &mockAlertRepo{
			findByIDFunc: func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
				return nil, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/unknown", nil)
		req = withURLParam(req, "alertID", "unknown")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("erreur repo retourne 500 (pas 404)", func(t *testing.T) {
		repo := &mockAlertRepo{
			findByIDFunc: func(ctx context.Context, tenantID, alertID string) (*alertDomain.Alert, error) {
				return nil, errors.New("db down")
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/al1", nil)
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Get(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAlertHandler_Acknowledge(t *testing.T) {
	t.Run("succès retourne 200 avec l'alerte acquittée", func(t *testing.T) {
		repo := &mockAlertRepo{
			acknowledgeFunc: func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
				if userID != "user-1" {
					t.Errorf("userID = %q, attendu user-1", userID)
				}
				return &alertDomain.Alert{ID: alertID, Status: alertDomain.StatusAcknowledged}, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al1/acknowledge", nil)
		req = req.WithContext(httpctx.WithUserID(req.Context(), "user-1"))
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Acknowledge(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d, attendu %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	t.Run("introuvable retourne 404", func(t *testing.T) {
		repo := &mockAlertRepo{
			acknowledgeFunc: func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
				return nil, nil
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/unknown/acknowledge", nil)
		req = withURLParam(req, "alertID", "unknown")
		rec := httptest.NewRecorder()

		h.Acknowledge(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("erreur repo retourne 500", func(t *testing.T) {
		repo := &mockAlertRepo{
			acknowledgeFunc: func(ctx context.Context, tenantID, alertID, userID string) (*alertDomain.Alert, error) {
				return nil, errors.New("boom")
			},
		}
		h := NewAlertHandler(repo)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/al1/acknowledge", nil)
		req = withURLParam(req, "alertID", "al1")
		rec := httptest.NewRecorder()

		h.Acknowledge(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestAlertHandler_RulesStubsReturn501(t *testing.T) {
	h := NewAlertHandler(&mockAlertRepo{})

	methods := map[string]func(http.ResponseWriter, *http.Request){
		"ListRules":  h.ListRules,
		"CreateRule": h.CreateRule,
		"UpdateRule": h.UpdateRule,
		"DeleteRule": h.DeleteRule,
	}

	for name, fn := range methods {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
			rec := httptest.NewRecorder()

			fn(rec, req)

			if rec.Code != http.StatusNotImplemented {
				t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNotImplemented)
			}
		})
	}
}
```

- [ ] **Step 3: Lancer les tests, vérifier qu'ils échouent (à la compilation)**

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
go test ./internal/interfaces/http/handlers/... 2>&1 | head -20
```
Expected: échec de compilation — `NewAlertHandler()` prend actuellement 0 argument, les tests en passent 1 (`repo`). C'est le signal attendu avant d'implémenter.

- [ ] **Step 4: Réécrire `alert_handler.go` (l'implémentation)**

Remplacer tout le contenu de `backend/internal/interfaces/http/handlers/alert_handler.go` par :

```go
package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	alertDomain "github.com/yourorg/leo-one/internal/domain/alert"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// AlertHandler gère les requêtes HTTP pour les alertes et règles d'alerte.
type AlertHandler struct {
	repo alertDomain.Repository
}

// NewAlertHandler crée un AlertHandler avec ses dépendances.
func NewAlertHandler(repo alertDomain.Repository) *AlertHandler {
	return &AlertHandler{repo: repo}
}

// List retourne la liste paginée des alertes du tenant.
//
//	GET /api/v1/alerts
func (h *AlertHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	if tenantID == "" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant_id manquant dans le contexte")
		return
	}

	q := r.URL.Query()
	filter := alertDomain.ListFilter{
		Cursor: q.Get("cursor"),
		Limit:  50,
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 200 {
			filter.Limit = n
		}
	}

	if statusStr := q.Get("status"); statusStr != "" {
		s := alertDomain.Status(statusStr)
		filter.Status = &s
	}

	if severityStr := q.Get("severity"); severityStr != "" {
		s := alertDomain.Severity(severityStr)
		filter.Severity = &s
	}

	if agentID := q.Get("agent_id"); agentID != "" {
		filter.AgentID = &agentID
	}

	alerts, nextCursor, err := h.repo.List(r.Context(), tenantID, filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la récupération des alertes")
		return
	}

	response.JSONWithMeta(w, http.StatusOK, alerts, map[string]any{
		"cursor": nextCursor,
	})
}

// Get retourne une alerte par son ID.
//
//	GET /api/v1/alerts/:alertID
func (h *AlertHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	alertID := chi.URLParam(r, "alertID")

	alert, err := h.repo.FindByID(r.Context(), tenantID, alertID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if alert == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "alerte introuvable")
		return
	}

	response.JSON(w, http.StatusOK, alert)
}

// Acknowledge acquitte une alerte.
//
//	POST /api/v1/alerts/:alertID/acknowledge
func (h *AlertHandler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())
	alertID := chi.URLParam(r, "alertID")

	alert, err := h.repo.Acknowledge(r.Context(), tenantID, alertID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'acquittement")
		return
	}
	if alert == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "alerte introuvable")
		return
	}

	response.JSON(w, http.StatusOK, alert)
}

func alertStub(w http.ResponseWriter, r *http.Request) {
	response.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "alerts: non encore implémenté")
}

// ListRules retourne la liste des règles d'alerte.
func (h *AlertHandler) ListRules(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// CreateRule crée une nouvelle règle d'alerte.
func (h *AlertHandler) CreateRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// UpdateRule met à jour une règle d'alerte existante.
func (h *AlertHandler) UpdateRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// DeleteRule supprime une règle d'alerte.
func (h *AlertHandler) DeleteRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }
```

- [ ] **Step 5: Câbler `alertRepo` dans `main.go`**

Dans `backend/cmd/server/main.go`, remplacer :
```go
	agentRepo  := postgres.NewAgentRepo(pool)
	metricRepo := postgres.NewMetricRepo(pool)
	tenantRepo := postgres.NewTenantRepo(pool)
```
par :
```go
	agentRepo  := postgres.NewAgentRepo(pool)
	metricRepo := postgres.NewMetricRepo(pool)
	tenantRepo := postgres.NewTenantRepo(pool)
	alertRepo  := postgres.NewAlertRepo(pool)
```
puis remplacer :
```go
	alertHandler     := handlers.NewAlertHandler()
```
par :
```go
	alertHandler     := handlers.NewAlertHandler(alertRepo)
```

- [ ] **Step 6: Lancer les tests, vérifier qu'ils passent**

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
go build ./... && go vet ./... && go test ./...
```
Expected: `go build`/`go vet` sans sortie, `go test` affiche `ok` pour `internal/interfaces/http/handlers` (aucun `FAIL`).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/interfaces/http/handlers/alert_handler.go \
        backend/internal/interfaces/http/handlers/mocks_test.go \
        backend/internal/interfaces/http/handlers/alert_handler_test.go \
        backend/cmd/server/main.go
git commit -m "$(cat <<'EOF'
feat: implémente List/Get/Acknowledge pour les alertes

Remplace les stubs 501 par une vraie implémentation branchée sur
alertDomain.Repository. ListRules/CreateRule/UpdateRule/DeleteRule
restent des stubs (hors périmètre de cette itération).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Types, client API et hook React Query

**Files:**
- Modify: `frontend/src/types/alert.ts`
- Create: `frontend/src/api/alerts.ts`
- Create: `frontend/src/hooks/useAlerts.ts`

**Interfaces:**
- Consumes: `get`, `post` de `frontend/src/api/client.ts` (existants) ; `ApiResponse<T>`, `PaginationParams` de `frontend/src/types/api.ts` (existants).
- Produces: `AlertSeverity`, `AlertStatus`, `Alert`, `AlertListFilter` (types) ; `alertsApi.list(filter?)`, `alertsApi.acknowledge(alertID)` ; `useAlerts(filter?)`, `useAcknowledgeAlert()`, `alertKeys`.

- [ ] **Step 1: Réécrire `types/alert.ts`**

Remplacer tout le contenu de `frontend/src/types/alert.ts` par :

```ts
import type { MetricType } from './metric'

export type AlertSeverity = 'info' | 'warning' | 'critical'
export type AlertStatus   = 'open' | 'acknowledged' | 'resolved'

export interface Alert {
  id:               string
  tenant_id:        string
  agent_id:         string
  agent_hostname:   string
  rule_id?:         string
  severity:         AlertSeverity
  status:           AlertStatus
  title:            string
  description?:     string
  metric_value?:    number
  triggered_at:     string
  acknowledged_at?: string
  acknowledged_by?: string
  resolved_at?:     string
  created_at:       string
}

export interface AlertListFilter {
  status?:   AlertStatus
  severity?: AlertSeverity
  agent_id?: string
}

export interface AlertRule {
  id:             string
  tenant_id:      string
  workspace_id?:  string
  agent_id?:      string
  name:           string
  description?:   string
  metric_type:    MetricType
  operator:       '>' | '>=' | '<' | '<=' | '='
  threshold:      number
  duration_secs:  number
  severity:       AlertSeverity
  is_active:      boolean
  created_by?:    string
  created_at:     string
  updated_at:     string
}
```

- [ ] **Step 2: Créer le client API**

Fichier `frontend/src/api/alerts.ts` :

```ts
import { get, post } from './client'
import type { ApiResponse, PaginationParams } from '@/types/api'
import type { Alert, AlertListFilter } from '@/types/alert'

const BASE = '/api/v1/alerts'

export const alertsApi = {
  list: (filter?: AlertListFilter & PaginationParams) =>
    get<ApiResponse<Alert[]>>(BASE, filter as Record<string, string>),

  acknowledge: (alertID: string) =>
    post<ApiResponse<Alert>>(`${BASE}/${alertID}/acknowledge`),
}
```

- [ ] **Step 3: Créer le hook React Query**

Fichier `frontend/src/hooks/useAlerts.ts` :

```ts
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { alertsApi } from '@/api/alerts'
import type { AlertListFilter } from '@/types/alert'

export const alertKeys = {
  all:  ['alerts'] as const,
  list: (filter?: AlertListFilter) => [...alertKeys.all, 'list', filter] as const,
}

/** Liste des alertes avec filtres optionnels */
export function useAlerts(filter?: AlertListFilter) {
  return useQuery({
    queryKey: alertKeys.list(filter),
    queryFn:  () => alertsApi.list(filter),
    staleTime: 15_000,
  })
}

/** Mutation : acquittement d'une alerte */
export function useAcknowledgeAlert() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (alertID: string) => alertsApi.acknowledge(alertID),
    onSuccess:  () => qc.invalidateQueries({ queryKey: alertKeys.all }),
  })
}
```

- [ ] **Step 4: Vérifier le typage**

```bash
cd /home/adelphe/Git/Leo-one/frontend
npm run type-check
```
Expected: aucune erreur (`AlertRule` référence toujours `MetricType`, déjà importé).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/types/alert.ts frontend/src/api/alerts.ts frontend/src/hooks/useAlerts.ts
git commit -m "$(cat <<'EOF'
feat: types, client API et hook React Query pour les alertes

Corrige au passage AlertSeverity/AlertStatus/Alert, désynchronisés du
contrat backend réel depuis le boilerplate initial (firing/message
n'existent pas côté API, ce sont open/title).

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: Composants et page Alertes

**Files:**
- Create: `frontend/src/components/alerts/AlertSeverityBadge.tsx`
- Create: `frontend/src/components/alerts/AlertStatusBadge.tsx`
- Create: `frontend/src/components/alerts/AlertTable.tsx`
- Create: `frontend/src/pages/AlertsPage.tsx`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Consumes: `AlertSeverity`, `AlertStatus`, `Alert` (Task 4), `useAlerts`, `useAcknowledgeAlert` (Task 4), `cn` de `@/lib/utils` (existant).
- Produces: composants `AlertSeverityBadge`, `AlertStatusBadge`, `AlertTable`, page par défaut `AlertsPage`, route `/alerts`.

- [ ] **Step 1: Créer `AlertSeverityBadge.tsx`**

```tsx
/**
 * AlertSeverityBadge.tsx — Badge coloré indiquant la sévérité d'une alerte
 */
import { cn } from '@/lib/utils'
import type { AlertSeverity } from '@/types/alert'

const SEVERITY_CONFIG: Record<AlertSeverity, { label: string; bg: string; text: string }> = {
  info:     { label: 'Info',     bg: 'bg-blue-50',   text: 'text-blue-700'   },
  warning:  { label: 'Warning',  bg: 'bg-yellow-50', text: 'text-yellow-700' },
  critical: { label: 'Critique', bg: 'bg-red-50',    text: 'text-red-700'    },
}

interface AlertSeverityBadgeProps {
  severity: AlertSeverity
}

export function AlertSeverityBadge({ severity }: AlertSeverityBadgeProps) {
  const cfg = SEVERITY_CONFIG[severity]

  return (
    <span className={cn('inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold', cfg.bg, cfg.text)}>
      {cfg.label}
    </span>
  )
}
```

- [ ] **Step 2: Créer `AlertStatusBadge.tsx`**

```tsx
/**
 * AlertStatusBadge.tsx — Badge coloré indiquant le statut d'une alerte
 */
import { cn } from '@/lib/utils'
import type { AlertStatus } from '@/types/alert'

const STATUS_CONFIG: Record<AlertStatus, { label: string; bg: string; text: string }> = {
  open:         { label: 'Ouverte',   bg: 'bg-red-50',    text: 'text-red-700'    },
  acknowledged: { label: 'Acquittée', bg: 'bg-yellow-50', text: 'text-yellow-700' },
  resolved:     { label: 'Résolue',   bg: 'bg-green-50',  text: 'text-green-700'  },
}

interface AlertStatusBadgeProps {
  status: AlertStatus
}

export function AlertStatusBadge({ status }: AlertStatusBadgeProps) {
  const cfg = STATUS_CONFIG[status]

  return (
    <span className={cn('inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold', cfg.bg, cfg.text)}>
      {cfg.label}
    </span>
  )
}
```

- [ ] **Step 3: Créer `AlertTable.tsx`**

```tsx
/**
 * AlertTable.tsx — Table des alertes avec filtres et acquittement
 */
import { useState } from 'react'
import { Bell, Check } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useAlerts, useAcknowledgeAlert } from '@/hooks/useAlerts'
import { AlertSeverityBadge } from './AlertSeverityBadge'
import { AlertStatusBadge } from './AlertStatusBadge'
import type { AlertStatus, AlertSeverity } from '@/types/alert'

export function AlertTable() {
  const [statusFilter, setStatusFilter]     = useState<AlertStatus | ''>('')
  const [severityFilter, setSeverityFilter] = useState<AlertSeverity | ''>('')

  const { data, isLoading, refetch } = useAlerts({
    ...(statusFilter   ? { status: statusFilter }     : {}),
    ...(severityFilter ? { severity: severityFilter } : {}),
  })
  const acknowledge = useAcknowledgeAlert()

  const alerts = data?.data ?? []

  return (
    <div className="flex flex-col gap-4">

      <div className="flex items-center gap-3 flex-wrap">
        <select
          value={statusFilter}
          onChange={e => setStatusFilter(e.target.value as AlertStatus | '')}
          className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
        >
          <option value="">Tous les statuts</option>
          <option value="open">Ouverte</option>
          <option value="acknowledged">Acquittée</option>
          <option value="resolved">Résolue</option>
        </select>

        <select
          value={severityFilter}
          onChange={e => setSeverityFilter(e.target.value as AlertSeverity | '')}
          className="rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500"
        >
          <option value="">Toutes les sévérités</option>
          <option value="info">Info</option>
          <option value="warning">Warning</option>
          <option value="critical">Critique</option>
        </select>

        <button
          onClick={() => refetch()}
          className="ml-auto flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 hover:bg-gray-50"
        >
          Actualiser
        </button>
      </div>

      <div className="overflow-x-auto rounded-xl border border-gray-200 bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-100 bg-gray-50">
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Sévérité</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Titre</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Machine</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Statut</th>
              <th className="px-4 py-3 text-left font-semibold text-gray-600">Déclenchée</th>
              <th className="px-4 py-3 text-right font-semibold text-gray-600">Actions</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              Array.from({ length: 5 }).map((_, i) => (
                <tr key={i} className="border-b border-gray-50">
                  {Array.from({ length: 6 }).map((_, j) => (
                    <td key={j} className="px-4 py-3">
                      <div className="h-4 w-full animate-pulse rounded bg-gray-100" />
                    </td>
                  ))}
                </tr>
              ))
            )}

            {!isLoading && alerts.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-12 text-center text-gray-400">
                  <Bell className="mx-auto h-8 w-8 mb-2 opacity-40" />
                  Aucune alerte
                </td>
              </tr>
            )}

            {!isLoading && alerts.map(alert => (
              <tr key={alert.id} className="border-b border-gray-50 hover:bg-gray-50">
                <td className="px-4 py-3"><AlertSeverityBadge severity={alert.severity} /></td>
                <td className="px-4 py-3 font-medium text-gray-900">{alert.title}</td>
                <td className="px-4 py-3 text-gray-500">{alert.agent_hostname}</td>
                <td className="px-4 py-3"><AlertStatusBadge status={alert.status} /></td>
                <td className="px-4 py-3 text-gray-400 text-xs">
                  {formatDistanceToNow(new Date(alert.triggered_at), { addSuffix: true, locale: fr })}
                </td>
                <td className="px-4 py-3 text-right">
                  <button
                    disabled={alert.status !== 'open' || acknowledge.isPending}
                    onClick={() => acknowledge.mutate(alert.id)}
                    className="inline-flex items-center gap-1.5 rounded-lg border border-gray-200 px-3 py-1.5 text-xs font-medium text-gray-600 hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    <Check className="h-3.5 w-3.5" />
                    Acquitter
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {!isLoading && (
          <div className="border-t border-gray-100 px-4 py-2 text-xs text-gray-400">
            {alerts.length} alerte{alerts.length > 1 ? 's' : ''}
          </div>
        )}
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Créer `AlertsPage.tsx`**

```tsx
/**
 * AlertsPage.tsx — Liste des alertes déclenchées sur l'infrastructure
 */
import { Bell } from 'lucide-react'
import { AlertTable } from '@/components/alerts/AlertTable'

export default function AlertsPage() {
  return (
    <div className="flex flex-col gap-6 p-6">
      <div className="flex items-center gap-3">
        <Bell className="h-6 w-6 text-brand-600" />
        <div>
          <h1 className="text-xl font-bold text-gray-900">Alertes</h1>
          <p className="text-sm text-gray-500 mt-0.5">Supervision des alertes déclenchées sur votre infrastructure</p>
        </div>
      </div>

      <AlertTable />
    </div>
  )
}
```

- [ ] **Step 5: Enregistrer la route dans `App.tsx`**

Dans `frontend/src/App.tsx`, remplacer :
```tsx
const LoginPage      = lazy(() => import('@/pages/LoginPage'))
const DashboardPage  = lazy(() => import('@/pages/DashboardPage'))
const AgentsPage     = lazy(() => import('@/pages/AgentsPage'))
const AgentDetailPage = lazy(() => import('@/pages/AgentDetailPage'))
```
par :
```tsx
const LoginPage      = lazy(() => import('@/pages/LoginPage'))
const DashboardPage  = lazy(() => import('@/pages/DashboardPage'))
const AgentsPage     = lazy(() => import('@/pages/AgentsPage'))
const AgentDetailPage = lazy(() => import('@/pages/AgentDetailPage'))
const AlertsPage     = lazy(() => import('@/pages/AlertsPage'))
```
et remplacer :
```tsx
              <Route index                      element={<DashboardPage />}   />
              <Route path="agents"              element={<AgentsPage />}      />
              <Route path="agents/:agentId"     element={<AgentDetailPage />} />
              {/* Routes futures */}
```
par :
```tsx
              <Route index                      element={<DashboardPage />}   />
              <Route path="agents"              element={<AgentsPage />}      />
              <Route path="agents/:agentId"     element={<AgentDetailPage />} />
              <Route path="alerts"              element={<AlertsPage />}      />
              {/* Routes futures */}
```

- [ ] **Step 6: Vérifier le typage**

```bash
cd /home/adelphe/Git/Leo-one/frontend
npm run type-check
```
Expected: aucune erreur.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/alerts frontend/src/pages/AlertsPage.tsx frontend/src/App.tsx
git commit -m "$(cat <<'EOF'
feat: page Alertes (liste, filtres, acquittement)

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: Correction du widget RecentAlerts du dashboard

**Files:**
- Modify: `frontend/src/components/dashboard/RecentAlerts.tsx`

**Interfaces:**
- Consumes: `useAlerts` (Task 4), `AlertSeverity` (Task 4).
- Produces: rien (composant terminal, déjà consommé par `DashboardPage.tsx`).

**Contexte :** Ce widget existant interroge `?status=firing` et affiche `alert.message` — deux champs qui n'ont jamais existé côté backend (`AlertStatus` incluait `firing` par erreur, `Alert` n'a jamais eu de champ `message`, seulement `title`). Corrigé pour utiliser le hook et les champs réels.

- [ ] **Step 1: Réécrire le composant**

Remplacer tout le contenu de `frontend/src/components/dashboard/RecentAlerts.tsx` par :

```tsx
/**
 * RecentAlerts.tsx — Liste des dernières alertes non acquittées
 */
import { AlertTriangle, AlertCircle, Info, CheckCircle } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { fr } from 'date-fns/locale'
import { useAlerts } from '@/hooks/useAlerts'
import type { AlertSeverity } from '@/types/alert'

const SEVERITY_CONFIG: Record<AlertSeverity, { icon: React.ElementType; color: string }> = {
  critical: { icon: AlertCircle,   color: 'text-red-500'    },
  warning:  { icon: AlertTriangle, color: 'text-yellow-500' },
  info:     { icon: Info,          color: 'text-blue-500'   },
}

export function RecentAlerts() {
  const { data, isLoading } = useAlerts({ status: 'open' })

  const alerts = (data?.data ?? []).slice(0, 5)

  if (isLoading) {
    return (
      <div className="space-y-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-12 animate-pulse rounded-lg bg-gray-100" />
        ))}
      </div>
    )
  }

  if (alerts.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-gray-400">
        <CheckCircle className="h-8 w-8 mb-2 text-green-400" />
        <span className="text-sm">Aucune alerte active</span>
      </div>
    )
  }

  return (
    <ul className="space-y-2">
      {alerts.map(alert => {
        const cfg = SEVERITY_CONFIG[alert.severity]
        const Icon = cfg.icon
        return (
          <li
            key={alert.id}
            className="flex items-start gap-3 rounded-lg border border-gray-100 p-3 hover:bg-gray-50"
          >
            <Icon className={`h-5 w-5 mt-0.5 shrink-0 ${cfg.color}`} />
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-gray-800 truncate">{alert.title}</p>
              <p className="text-xs text-gray-400 mt-0.5">
                {formatDistanceToNow(new Date(alert.triggered_at), { addSuffix: true, locale: fr })}
              </p>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
```

- [ ] **Step 2: Vérifier le typage**

```bash
cd /home/adelphe/Git/Leo-one/frontend
npm run type-check
```
Expected: aucune erreur.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/dashboard/RecentAlerts.tsx
git commit -m "$(cat <<'EOF'
fix: RecentAlerts utilisait des champs/valeurs jamais renvoyés par l'API

status=firing et alert.message n'ont jamais existé côté backend
(open et title). Bascule sur le hook useAlerts partagé avec AlertsPage.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 7: Vérification de bout en bout

**Files:** aucun (vérification uniquement).

**Interfaces:** aucune.

- [ ] **Step 1: Relancer le backend avec le code à jour**

```bash
kill $(cat /tmp/leo-backend.pid) 2>/dev/null
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
export DATABASE_URL="postgres://leo:leo_dev@localhost:5433/leo_one"
nohup go run ./cmd/server > /tmp/leo-backend.log 2>&1 &
echo $! > /tmp/leo-backend.pid
sleep 2
cat /tmp/leo-backend.log
```
Expected : logs montrant `PostgreSQL connecté` et `Serveur API REST démarré` sans erreur.

- [ ] **Step 2: Vérifier que le correctif is_admin fonctionne**

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@leo-one.local","password":"admin123"}' \
| grep -o '"access_token":"[^"]*"' | cut -d'"' -f4 \
| { read -r TOKEN; echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null; echo; }
```
Expected : le JSON décodé contient `"is_admin":true` (auparavant `false`).

- [ ] **Step 3: Semer une alerte de test en base**

Aucun agent n'existe encore en base (0 agent enregistré). Comme `alerts.agent_id` référence `agents(id)` en `NOT NULL`, créer d'abord un agent factice, puis une alerte :

```bash
export PGBIN="$HOME/.local/pgsql/usr/lib/postgresql/16/bin"
export LD_LIBRARY_PATH="$HOME/.local/pgsql/usr/lib/x86_64-linux-gnu:$LD_LIBRARY_PATH"

"$PGBIN/psql" -h /tmp -p 5433 -U leo -d leo_one -v ON_ERROR_STOP=1 <<'SQL'
INSERT INTO agents (id, tenant_id, hostname, os, os_version, arch, hardware_id, agent_version, status)
SELECT gen_random_uuid(), t.id, 'PARIS-SRV-01', 'linux', 'Ubuntu 24.04', 'amd64', 'demo-hw-001', '1.0.0', 'online'
FROM tenants t WHERE t.slug = 'demo-corp'
RETURNING id \gset agent_

INSERT INTO alerts (id, tenant_id, agent_id, severity, status, title, description, metric_value)
SELECT gen_random_uuid(), t.id, :'agent_id', 'critical', 'open',
       'CPU au-dessus de 90%', 'Charge CPU soutenue depuis 5 minutes', 94.2
FROM tenants t WHERE t.slug = 'demo-corp';

INSERT INTO alerts (id, tenant_id, agent_id, severity, status, title, description, metric_value)
SELECT gen_random_uuid(), t.id, :'agent_id', 'warning', 'open',
       'Espace disque faible', 'Moins de 10% d''espace libre sur /', 8.4
FROM tenants t WHERE t.slug = 'demo-corp';
SQL
```
Expected : deux `INSERT 0 1`.

- [ ] **Step 4: Vérifier l'API alertes via curl**

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@leo-one.local","password":"admin123"}' \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

curl -s http://localhost:8080/api/v1/alerts -H "Authorization: Bearer $TOKEN"
```
Expected : `{"data":[{...2 alertes...}],"meta":{"cursor":""}}`, chaque alerte avec `"agent_hostname":"PARIS-SRV-01"`.

- [ ] **Step 5: Vérification visuelle dans le navigateur**

Le frontend tourne déjà sur `http://localhost:5174` (session Claude en cours, extension Chrome connectée). Naviguer vers `http://localhost:5174/alerts`, confirmer :
- Le tableau affiche les 2 alertes de test avec les bons badges de sévérité/statut et le nom de machine `PARIS-SRV-01`.
- Le filtre "Tous les statuts" / "Ouverte" fonctionne.
- Cliquer "Acquitter" sur une ligne change son statut en "Acquittée" et désactive le bouton.
- Retourner sur `/` (dashboard) : le widget "Alertes actives" reflète le changement (1 alerte restante en `open`).

- [ ] **Step 6: Suite de tests complète**

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd /home/adelphe/Git/Leo-one/backend
go build ./... && go vet ./... && go test ./...
```
Expected : `ok` pour `internal/interfaces/http/handlers`, aucun `FAIL` ailleurs.

---

## Self-Review

- **Couverture du spec** : domaine+repo (Task 1), correctif is_admin (Task 2), handler+tests+câblage (Task 3), types+API+hook (Task 4), composants+page+route (Task 5), correctif RecentAlerts (Task 6), vérification bout-en-bout incluant le navigateur (Task 7). Les règles d'alerte restent explicitement des stubs, conformément à la décision de périmètre.
- **Placeholders** : aucun "TBD"/"TODO" — chaque step contient soit du code complet, soit une commande exacte avec sortie attendue.
- **Cohérence des types/signatures** : `NewAlertHandler(repo alertDomain.Repository)` (Task 3) correspond à l'appel dans `main.go` (même task) et aux tests (`NewAlertHandler(&mockAlertRepo{})` / `NewAlertHandler(repo)`). `alertsApi.list`/`acknowledge` (Task 4) correspondent aux appels dans `AlertTable.tsx`/`RecentAlerts.tsx` (Tasks 5-6) via les hooks `useAlerts`/`useAcknowledgeAlert`. Les champs `Alert` (Task 1 Go / Task 4 TS) correspondent terme à terme (`agent_hostname`, `title`, `status: open|acknowledged|resolved`, `severity: info|warning|critical`).
