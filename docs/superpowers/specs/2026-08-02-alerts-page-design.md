# Page Alertes — liste + acquittement

## Contexte

Leo-One a 4 pages listées dans la sidebar mais jamais implémentées : Alertes,
Tickets, Utilisateurs, Paramètres. Le schéma BDD et le contrat API
(`backend/internal/interfaces/http/router.go`) sont déjà entièrement
spécifiés pour les 4 ; seuls les handlers (actuellement des stubs 501) et
les pages React manquent.

On traite ces 4 sous-systèmes un par un. Ce document couvre le premier :
**Alertes**, avec un périmètre volontairement réduit à la liste des alertes
déclenchées et leur acquittement. La gestion des règles d'alerte (CRUD de
seuils type "CPU > 90%") est explicitement hors périmètre de cette
itération — `AlertHandler.ListRules/CreateRule/UpdateRule/DeleteRule`
restent des stubs 501.

## Bug bloquant à corriger au passage

`auth_handler.go` (`Login` et `Refresh`) calcule `is_admin` avec :
```sql
WHERE ... AND ro.name = 'admin' AND ro.is_system = true
```
Le rôle système seedé par `seed_system_roles()` (migration 003) s'appelle
`'Admin'` (majuscule). La comparaison est sensible à la casse en Postgres,
donc `is_admin` est toujours `false` pour un admin réel. Or
`RequirePermission` (middleware.go) n'autorise les actions d'écriture
(`acknowledge`, `write`, `delete`) qu'aux `is_admin=true` — sans ce
correctif, `Acknowledge` retournera systématiquement 403. Correction :
`ro.name = 'Admin'` dans les deux requêtes.

## Backend

### `internal/domain/alert/alert.go` (nouveau)

Mirroring de `internal/domain/agent/agent.go` :
- `type Severity string` — `info` | `warning` | `critical` (= enum SQL `alert_severity`)
- `type Status string` — `open` | `acknowledged` | `resolved` (= enum SQL `alert_status`)
- `type Alert struct` — `ID, TenantID, AgentID, AgentHostname string; RuleID *string; Severity Severity; Status Status; Title string; Description *string; MetricValue *float64; TriggeredAt time.Time; AcknowledgedAt *time.Time; AcknowledgedBy *string; ResolvedAt *time.Time; CreatedAt time.Time`
  - `AgentHostname` est un champ de lecture seule, peuplé par un JOIN dans le repo — évite au frontend de résoudre l'UUID d'agent en nom de machine avec un aller-retour supplémentaire.
- `type ListFilter struct` — `Status *Status; Severity *Severity; AgentID *string; Cursor string; Limit int`
- `type Repository interface`:
  - `List(ctx, tenantID string, filter ListFilter) ([]*Alert, string, error)` — pagination cursor-based identique à `AgentRepo.List` (tri par `id ASC`, `LIMIT+1` pour détecter la page suivante)
  - `FindByID(ctx, tenantID, alertID string) (*Alert, error)`
  - `Acknowledge(ctx, tenantID, alertID, userID string) (*Alert, error)` — met à jour `status='acknowledged', acknowledged_at=NOW(), acknowledged_by=userID` scoped par tenant, retourne l'alerte à jour (ou `nil` si non trouvée/hors tenant)

### `internal/infrastructure/persistence/postgres/alert_repo.go` (nouveau)

Implémente `alert.Repository` via pgx, sur le modèle exact de
`agent_repo.go` (même usage de `ensureCtx`, même construction dynamique de
`WHERE` pour les filtres optionnels, même pagination cursor).

Requête `List` (esquisse) :
```sql
SELECT al.id, al.tenant_id, al.agent_id, ag.hostname, al.rule_id,
       al.severity::text, al.status::text, al.title, al.description,
       al.metric_value, al.triggered_at, al.acknowledged_at,
       al.acknowledged_by, al.resolved_at, al.created_at
FROM alerts al
JOIN agents ag ON ag.id = al.agent_id
WHERE al.tenant_id = $1 [AND al.status = $n] [AND al.severity = $n] [AND al.agent_id = $n] [AND al.id > $n cursor]
ORDER BY al.id ASC
LIMIT $n
```

### `internal/interfaces/http/handlers/alert_handler.go` (modifié)

`AlertHandler` gagne un champ `repo alertDomain.Repository`,
`NewAlertHandler(repo)` change de signature en conséquence (mise à jour de
l'appel dans `main.go`).

- `List` : parse `status`, `severity`, `agent_id`, `cursor`, `limit` depuis
  la query string (même style que `AgentHandler.List`), appelle
  `repo.List`, répond via `response.JSONWithMeta`.
- `Get` : `repo.FindByID`, 404 si nil, 500 si erreur — **en appliquant le
  correctif d'ordre de conditions découvert dans `agent_handler.go`**
  (vérifier `err != nil` avant `agent == nil`, pas l'inverse).
- `Acknowledge` : lit `userID` depuis `httpctx.UserIDFromContext`, appelle
  `repo.Acknowledge(ctx, tenantID, alertID, userID)`, 404 si nil, répond
  200 avec l'alerte mise à jour. Le corps optionnel `{"comment": "..."}`
  documenté dans `router.go` n'est pas persisté (aucune colonne prévue en
  base pour ça) — non lu, pour éviter du code mort.
- `ListRules`/`CreateRule`/`UpdateRule`/`DeleteRule` : inchangés (501).

`main.go` : ajoute `alertRepo := postgres.NewAlertRepo(pool)` et passe
`alertRepo` à `handlers.NewAlertHandler`.

### Tests

`alert_handler_test.go` réécrit sur le modèle de `agent_handler_test.go` /
`metric_handler_test.go` : un `mockAlertRepo` dans `mocks_test.go`, tests
table-driven pour `List` (succès, tenant manquant, erreur repo), `Get`
(trouvé/404/500), `Acknowledge` (succès, 404, erreur repo). Les stubs de
règles gardent leur test 501 existant.

## Frontend

### `types/alert.ts` (corrigé)

Actuellement désynchronisé du backend (`AlertStatus` avait `firing` au lieu
de `open`, `AlertSeverity` avait `high`/`medium`/`low` qui n'existent pas
côté DB, `Alert.message` au lieu de `title`/`description`). Alignement sur
les enums SQL réels :
```ts
export type AlertSeverity = 'info' | 'warning' | 'critical'
export type AlertStatus   = 'open' | 'acknowledged' | 'resolved'

export interface Alert {
  id: string; tenant_id: string; agent_id: string; agent_hostname: string
  rule_id?: string; severity: AlertSeverity; status: AlertStatus
  title: string; description?: string; metric_value?: number
  triggered_at: string; acknowledged_at?: string; acknowledged_by?: string
  resolved_at?: string; created_at: string
}
```
`AlertRule` reste tel quel (déjà correct, pas utilisé pour l'instant).

### `api/alerts.ts` (nouveau)

`listAlerts(params)`, `acknowledgeAlert(id)` — mêmes conventions que
`api/agents.ts` (fonctions fines au-dessus de `get`/`post` de `client.ts`).

### Composants (nouveaux)

- `components/alerts/AlertSeverityBadge.tsx` et
  `components/alerts/AlertStatusBadge.tsx` — même structure que
  `AgentStatusBadge.tsx` (couleur par valeur d'enum).

### `pages/AlertsPage.tsx` (nouveau)

Même architecture que `AgentsPage.tsx` + `AgentTable.tsx` : recherche/filtres
en haut (statut, sévérité), tableau (Sévérité, Titre, Machine, Statut,
Déclenchée le, Actions), état vide ("Aucune alerte"), bouton "Acquitter"
par ligne (désactivé si déjà acquittée/résolue), React Query pour le
fetch + invalidation après acquittement.

### `components/dashboard/RecentAlerts.tsx` (corrigé)

Requête `?status=firing` → `?status=open`, affichage `alert.message` →
`alert.title`.

### `App.tsx`

Ajoute `const AlertsPage = lazy(() => import('@/pages/AlertsPage'))` et
`<Route path="alerts" element={<AlertsPage />} />`.

## Vérification

- `go build ./... && go vet ./... && go test ./...` côté backend.
- Redémarrage backend + frontend (déjà tournant en local sur cette
  machine), seed manuel d'une ou deux alertes de test en BDD (aucune
  alerte réelle n'existe tant qu'aucun agent ne tourne), puis navigation
  visuelle dans le navigateur (Chrome via l'extension) : page Alertes,
  filtre par statut, acquittement d'une alerte, vérification que le
  dashboard (`RecentAlerts`) reflète le changement.
