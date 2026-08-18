package http

// Routes API Leo-One RMM — spécification complète
//
// Conventions :
//   - Toutes les routes REST sont préfixées /api/v1
//   - Authentification : JWT Bearer (RS256) sauf routes publiques
//   - tenant_id extrait du JWT, jamais dans l'URL (isolation multi-tenant)
//   - Format de réponse succès  : {"data": ..., "meta": {...}}
//   - Format de réponse erreur  : {"error": {"code": "...", "message": "..."}}
//   - Pagination : cursor-based via ?cursor=&limit= (défaut limit=50, max=200)
//
// ─────────────────────────────────────────────────────────────────────────────
// ROUTES PUBLIQUES (pas de JWT requis)
// ─────────────────────────────────────────────────────────────────────────────
//
//  POST   /api/v1/auth/login
//         Body    : {"email":"...","password":"..."}
//         Body MFA: {"email":"...","password":"...","mfa_code":"123456"}
//         Resp 200: {"data":{"access_token":"...","refresh_token":"...","expires_in":900}}
//         Resp 401: email/mot de passe invalide
//         Resp 403: compte désactivé
//         Resp 429: trop de requêtes depuis cette IP, ou trop d'échecs
//                   récents sur ce compte (rate limiting, voir RateLimitByIP
//                   et le verrouillage par compte dans AuthHandler.Login)
//
//  POST   /api/v1/auth/refresh
//         Body    : {"refresh_token":"..."}
//         Resp 200: {"data":{"access_token":"...","expires_in":900}}
//         Resp 401: refresh token invalide ou expiré
//         Resp 429: trop de requêtes depuis cette IP (rate limiting)
//
//  POST   /api/v1/auth/logout
//         Body    : {"refresh_token":"..."}
//         Resp 204: token invalidé côté serveur
//
//  GET    /health
//         Resp 200: {"status":"ok","version":"1.0.0","db":"ok"}
//
//  POST   /api/v1/enroll
//         Body    : {
//                     "enrollment_token": "eyJ...",
//                     "public_key":       "-----BEGIN PUBLIC KEY-----...",
//                     "hostname":         "DESKTOP-ABC123",
//                     "os":               "windows",
//                     "os_version":       "Windows 11 23H2",
//                     "arch":             "amd64",
//                     "hardware_id":      "550e8400-e29b-41d4-a716-446655440000",
//                     "agent_version":    "1.0.0",
//                     "fqdn":             "desktop-abc123.domain.local"
//                   }
//         Resp 201: {
//                     "data": {
//                       "agent_id":    "...",
//                       "tenant_id":   "...",
//                       "client_cert": "-----BEGIN CERTIFICATE-----...",
//                       "ws_endpoint": "wss://rmm.example.com/ws/agent"
//                     }
//                   }
//         Resp 400: token malformé
//         Resp 401: token invalide, expiré, ou déjà utilisé
//         Resp 429: trop de requêtes depuis cette IP (rate limiting)
//
// ─────────────────────────────────────────────────────────────────────────────
// WEBSOCKET — Connexion des agents (authentification mTLS, pas JWT)
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /ws/agent
//         Upgrade : WebSocket
//         Auth    : mTLS (certificat client présenté dans le handshake TLS)
//         Headers : X-Agent-ID, X-Agent-Version
//         Resp 101: connexion WSS établie
//         Resp 401: certificat client manquant ou révoqué
//         Resp 403: agent inconnu ou tenant désactivé
//
// ─────────────────────────────────────────────────────────────────────────────
// AGENTS  [JWT requis — permission agents:read minimum]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/agents
//         Auth    : agents:read
//         Query   : ?workspace_id=&status=online|offline|...&cursor=&limit=
//         Resp 200: {"data":[{Agent}],"meta":{"cursor":"...","total":42}}
//
//  GET    /api/v1/agents/:agent_id
//         Auth    : agents:read
//         Resp 200: {"data":{Agent}}
//         Resp 404: agent non trouvé (ou hors tenant)
//
//  PATCH  /api/v1/agents/:agent_id
//         Auth    : agents:write
//         Body    : {"workspace_id":"...","hostname":"..."}  (champs partiels)
//         Resp 200: {"data":{Agent}}
//
//  DELETE /api/v1/agents/:agent_id
//         Auth    : agents:delete
//         Resp 204: agent supprimé, certificat révoqué
//
//  DELETE /api/v1/agents/:agent_id/certificate
//         Auth    : agents:delete
//         Révoque le(s) certificat(s) mTLS actif(s) sans supprimer l'agent
//         (rotation de certificat, agent compromis) — coupe aussi toute
//         connexion WSS en cours. Un nouvel enrollment est requis ensuite.
//         Resp 200: {"data":{"revoked_count":1}}
//
//  POST   /api/v1/agents/:agent_id/commands
//         Auth    : agents:execute
//         Body    : {
//                     "type":    "exec_script",           // exec_script | install_pkg | reboot | ping
//                     "payload": {
//                       "interpreter": "powershell",      // powershell | bash | cmd
//                       "script":      "Get-Process",
//                       "timeout_secs": 30
//                     }
//                   }
//         Resp 202: {"data":{"command_id":"...","status":"pending"}}
//         Resp 409: agent hors ligne, commande non envoyée
//
//  GET    /api/v1/agents/:agent_id/commands
//         Auth    : agents:read
//         Query   : ?status=&limit=&cursor=
//         Resp 200: {"data":[{Command}],"meta":{...}}
//
//  GET    /api/v1/agents/:agent_id/commands/:command_id
//         Auth    : agents:read
//         Resp 200: {"data":{Command}}  (inclut stdout, stderr, exit_code)
//
//  POST   /api/v1/agents/bulk-commands
//         Auth    : agents:execute
//         Body    : {
//                     "agent_ids":    ["...","..."],  // OU workspace_id — l'un des deux
//                     "workspace_id": null,
//                     "type":         "exec_script",
//                     "payload":      { "interpreter": "powershell", "script": "...", "timeout_sec": 30 }
//                   }
//         Resp 202: {"data":[{"agent_id":"...","command_id":"...","sent":true}, ...]}
//
//  GET    /api/v1/agents/:agent_id/inventory/hardware
//         Auth    : inventory:read
//         Resp 200: {"data":{HardwareInventory}}
//
//  GET    /api/v1/agents/:agent_id/inventory/software
//         Auth    : inventory:read
//         Query   : ?search=&cursor=&limit=
//         Resp 200: {"data":[{SoftwareItem}],"meta":{...}}
//
// ─────────────────────────────────────────────────────────────────────────────
// PATCHS (gestion des mises à jour)  [JWT requis — permission patches:*]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/agents/:agent_id/patches
//         Auth    : patches:read
//         Query   : ?status=available|installed|ignored|failed&cursor=&limit=
//         Resp 200: {"data":[{Patch}],"meta":{"cursor":"..."}}
//
//  POST   /api/v1/agents/:agent_id/patches/install
//         Auth    : patches:execute
//         Body    : {"patch_ids": ["KB5031354", "..."], "reboot_after": false}
//         Resp 202: {"data":{"command_id":"...","status":"pending","sent":true}}
//
//  POST   /api/v1/agents/bulk-patches/install
//         Auth    : patches:execute
//         Body    : {
//                     "agent_ids":    ["...","..."],  // OU workspace_id — l'un des deux
//                     "workspace_id": null,
//                     "min_severity": "important",     // optionnel, défaut "optional" (= tous)
//                     "reboot_after": false
//                   }
//         Resp 202: {"data":[{"agent_id":"...","command_id":"...","sent":true}, ...]}
//         Note    : contrairement à bulk-commands, la sélection de patchs
//                   n'est PAS partagée entre agents — chaque agent cible
//                   reçoit ses propres patchs disponibles (>= min_severity),
//                   les identifiants de patch étant spécifiques à
//                   l'inventaire de chaque machine.
//
//  GET    /api/v1/patches/summary
//         Auth    : patches:read
//         Resp 200: {
//                     "data": {
//                       "agents_with_critical_pending": 3,
//                       "agents_with_pending_patches":   12,
//                       "total_pending_patches":         47
//                     }
//                   }
//
// ─────────────────────────────────────────────────────────────────────────────
// MÉTRIQUES  [JWT requis — permission metrics:read]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/agents/:agent_id/metrics
//         Auth    : metrics:read
//         Query   : ?type=cpu_percent&from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z
//                   Le backend choisit automatiquement la résolution (brute/1h/1d)
//                   selon la plage demandée (cf. migration 002).
//         Resp 200: {
//                     "data": [
//                       {"time":"2024-01-01T00:00:00Z","value":45.2,"avg":44.1,"max":92.0,"min":12.3}
//                     ],
//                     "meta": {"resolution":"1h","from":"...","to":"..."}
//                   }
//
//  GET    /api/v1/agents/:agent_id/metrics/latest
//         Auth    : metrics:read
//         Resp 200: {"data":{"cpu_percent":45.2,"ram_used_bytes":4294967296,...,"ts":"..."}}
//
// ─────────────────────────────────────────────────────────────────────────────
// ALERTES  [JWT requis]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/alerts
//         Auth    : alerts:read
//         Query   : ?status=open|acknowledged|resolved&severity=&agent_id=&cursor=&limit=
//         Resp 200: {"data":[{Alert}],"meta":{...}}
//
//  GET    /api/v1/alerts/:alert_id
//         Auth    : alerts:read
//         Resp 200: {"data":{Alert}}
//
//  POST   /api/v1/alerts/:alert_id/acknowledge
//         Auth    : alerts:acknowledge
//         Body    : {"comment":"..."}  (optionnel)
//         Resp 200: {"data":{Alert}}  (status = acknowledged)
//
//  GET    /api/v1/alert-rules
//         Auth    : alerts:read
//         Query   : ?workspace_id=&is_active=&cursor=&limit=
//         Resp 200: {"data":[{AlertRule}],"meta":{...}}
//
//  POST   /api/v1/alert-rules
//         Auth    : alerts:write
//         Body    : {
//                     "name":          "CPU critique",
//                     "metric_type":   "cpu_percent",
//                     "operator":      ">",
//                     "threshold":     90,
//                     "duration_secs": 120,
//                     "severity":      "critical",
//                     "workspace_id":  "...",   // optionnel, null = tout le tenant
//                     "agent_id":      "..."    // optionnel, null = tout le workspace
//                   }
//         Resp 201: {"data":{AlertRule}}
//
//  PATCH  /api/v1/alert-rules/:rule_id
//         Auth    : alerts:write
//         Body    : champs partiels de AlertRule
//         Resp 200: {"data":{AlertRule}}
//
//  DELETE /api/v1/alert-rules/:rule_id
//         Auth    : alerts:delete
//         Resp 204
//
// ─────────────────────────────────────────────────────────────────────────────
// WORKSPACES  [JWT requis]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/workspaces
//         Auth    : workspaces:read
//         Resp 200: {"data":[{Workspace}]}
//
//  POST   /api/v1/workspaces
//         Auth    : workspaces:write
//         Body    : {"name":"Paris","description":"..."}
//         Resp 201: {"data":{Workspace}}
//
//  PATCH  /api/v1/workspaces/:workspace_id
//         Auth    : workspaces:write
//         Body    : {"name":"...","description":"..."}
//         Resp 200: {"data":{Workspace}}
//
//  DELETE /api/v1/workspaces/:workspace_id
//         Auth    : workspaces:delete
//         Resp 204  (les agents sont déplacés dans workspace_id = NULL)
//
// ─────────────────────────────────────────────────────────────────────────────
// ENROLLMENT TOKENS  [JWT requis — permission agents:write]
// ─────────────────────────────────────────────────────────────────────────────
//
//  POST   /api/v1/enrollment-tokens
//         Auth    : agents:write
//         Body    : {"label":"Déploiement Paris Jan 2025","workspace_id":"..."}
//         Resp 201: {
//                     "data": {
//                       "id":      "...",
//                       "token":   "eyJ...",   // valeur brute — affiché UNE seule fois
//                       "expires_at": "..."
//                     }
//                   }
//
//  GET    /api/v1/enrollment-tokens
//         Auth    : agents:write
//         Resp 200: {"data":[{EnrollmentToken sans valeur brute}]}
//
//  DELETE /api/v1/enrollment-tokens/:token_id
//         Auth    : agents:write
//         Resp 204: token révoqué
//
// ─────────────────────────────────────────────────────────────────────────────
// UTILISATEURS  [JWT requis]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/users
//         Auth    : users:read
//         Resp 200: {"data":[{User}]}
//
//  POST   /api/v1/users
//         Auth    : users:write
//         Body    : {"email":"...","full_name":"...","password":"...","role_ids":["..."]}
//         Resp 201: {"data":{User}}
//
//  GET    /api/v1/users/:user_id
//         Auth    : users:read
//         Resp 200: {"data":{User, "roles":[...]}}
//
//  PATCH  /api/v1/users/:user_id
//         Auth    : users:write
//         Body    : {"full_name":"...","is_active":true,"role_ids":["..."]}
//         Resp 200: {"data":{User}}
//
//  DELETE /api/v1/users/:user_id
//         Auth    : users:delete
//         Resp 204
//
//  POST   /api/v1/users/me/mfa/enable
//         Auth    : JWT (utilisateur courant)
//         Resp 200: {"data":{"qr_uri":"otpauth://...","secret":"..."}}
//
//  POST   /api/v1/users/me/mfa/confirm
//         Auth    : JWT (utilisateur courant)
//         Body    : {"code":"123456"}
//         Resp 200: {"data":{"backup_codes":["...x8"]}}
//
//  POST   /api/v1/users/me/mfa/disable
//         Auth    : JWT (utilisateur courant)
//         Body    : {"code":"123456"}
//         Resp 204
//
// ─────────────────────────────────────────────────────────────────────────────
// RÔLES  [JWT requis — permission users:write]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/roles
//         Auth    : users:read
//         Resp 200: {"data":[{Role, "permissions":[...]}]}
//
//  POST   /api/v1/roles
//         Auth    : users:write
//         Body    : {"name":"Superviseur","permission_ids":["..."]}
//         Resp 201: {"data":{Role}}
//
//  PATCH  /api/v1/roles/:role_id
//         Auth    : users:write
//         Resp 200: {"data":{Role}}
//         Note    : Les rôles système (is_system=true) sont non modifiables → 403
//
//  DELETE /api/v1/roles/:role_id
//         Auth    : users:write
//         Resp 204
//
//  GET    /api/v1/permissions
//         Auth    : users:read
//         Resp 200: {"data":[{Permission}]}
//
// ─────────────────────────────────────────────────────────────────────────────
// SCRIPTS (bibliothèque)  [JWT requis — permission scripts:*]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/scripts
//         Auth    : scripts:read
//         Resp 200: {"data":[{Script}]}
//
//  POST   /api/v1/scripts
//         Auth    : scripts:write
//         Body    : {"name":"...","interpreter":"powershell","content":"..."}
//         Resp 201: {"data":{Script}}
//
//  PATCH  /api/v1/scripts/:script_id
//         Auth    : scripts:write
//         Resp 200: {"data":{Script}}
//
//  DELETE /api/v1/scripts/:script_id
//         Auth    : scripts:delete
//         Resp 204
//
// ─────────────────────────────────────────────────────────────────────────────
// PLANIFICATIONS DE SCRIPTS (récurrentes, cron)  [JWT requis — permission scripts:*]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/script-schedules
//         Auth    : scripts:read
//         Resp 200: {"data":[{ScriptSchedule}]}
//
//  POST   /api/v1/script-schedules
//         Auth    : scripts:write
//         Body    : {
//                     "script_id":       "...",
//                     "name":            "Nettoyage nocturne",
//                     "agent_id":        null,      // XOR workspace_id
//                     "workspace_id":    "...",
//                     "cron_expression": "0 2 * * *",
//                     "timeout_sec":     60
//                   }
//         Resp 201: {"data":{ScriptSchedule}}
//         Note    : cron_expression validée (format standard à 5 champs) ;
//                   next_run_at calculé automatiquement à la création
//
//  PATCH  /api/v1/script-schedules/:schedule_id
//         Auth    : scripts:write
//         Body    : champs optionnels, dont "enabled" pour activer/désactiver
//         Resp 200: {"data":{ScriptSchedule}}
//
//  DELETE /api/v1/script-schedules/:schedule_id
//         Auth    : scripts:delete
//         Resp 204
//
// ─────────────────────────────────────────────────────────────────────────────
// TENANT (paramètres du compte)  [JWT requis — permission tenant:read/write]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/tenant
//         Auth    : tenant:read
//         Resp 200: {"data":{Tenant, "agent_count":42, "plan_limits":{...}}}
//
//  PATCH  /api/v1/tenant
//         Auth    : tenant:write
//         Body    : {"name":"..."}
//         Resp 200: {"data":{Tenant}}
//
// ─────────────────────────────────────────────────────────────────────────────
// JOURNAL D'AUDIT  [JWT requis — réservé aux administrateurs]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/audit-log
//         Auth    : administrateur (claim is_admin du JWT — voir RequireAdmin ;
//                   la permission "audit:read" existe dans le catalogue RBAC
//                   pour le catalogue/futurs rôles personnalisés, mais n'est
//                   pas encore ce qui gate cette route tant que RequirePermission
//                   reste un stub, voir son commentaire dans middleware.go)
//         Query   : ?user_id=&action=&resource_type=&from=&to=&cursor=&limit=
//                   from/to au format RFC3339 (ex: 2024-01-01T00:00:00Z)
//         Resp 200: {"data":[{AuditLogEntry}],"meta":{"cursor":"..."}}
//
// ─────────────────────────────────────────────────────────────────────────────
// TABLEAU DE BORD  [JWT requis — aggregats pour la page d'accueil]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/dashboard/summary
//         Auth    : agents:read + alerts:read
//         Resp 200: {
//                     "data": {
//                       "agents_total":   150,
//                       "agents_online":  142,
//                       "alerts_open":    3,
//                       "alerts_critical":1,
//                       "cpu_avg_percent": 34.2,
//                       "ram_avg_percent": 61.0
//                     }
//                   }
//
// ─────────────────────────────────────────────────────────────────────────────
// CODES D'ERREUR APPLICATIFS (champ error.code)
// ─────────────────────────────────────────────────────────────────────────────
//
//  UNAUTHORIZED          : JWT manquant ou invalide
//  FORBIDDEN             : permission insuffisante
//  NOT_FOUND             : ressource introuvable ou hors tenant
//  VALIDATION_ERROR      : payload invalide (détails dans error.details)
//  AGENT_OFFLINE         : commande envoyée à un agent hors ligne
//  ENROLLMENT_TOKEN_USED : token d'enrollment déjà consommé
//  ENROLLMENT_TOKEN_EXPIRED : token expiré
//  MFA_REQUIRED          : authentification MFA requise
//  MFA_INVALID           : code TOTP incorrect
//  SYSTEM_ROLE_IMMUTABLE : tentative de modifier un rôle système
//  TENANT_LIMIT_REACHED  : quota d'agents du plan atteint
//  RATE_LIMITED          : trop de requêtes (par IP) ou trop d'échecs
//                          d'authentification récents (par compte) — voir
//                          RateLimitByIP et AuthHandler.Login
//

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter construit et retourne le routeur Chi avec tous les middlewares et routes.
// Les handlers et use cases sont injectés via les paramètres (Dependency Injection).
func NewRouter(deps *Dependencies) http.Handler {
	r := chi.NewRouter()

	// ── Middlewares globaux ───────────────────────────────────────────────────
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(LoggerMiddleware(deps.Logger))
	r.Use(middleware.Recoverer)

	// ── Routes publiques ──────────────────────────────────────────────────────
	r.Get("/health", deps.AuthHandler.Health)

	r.Route("/api/v1", func(r chi.Router) {
		// Enrollment et auth : pas de JWT — rate limitées par IP (voir
		// RateLimitByIP et le point 5 du cahier des charges : brute force sur
		// /auth/login notamment). /auth/logout n'a pas de valeur pour un
		// attaquant (idempotent, ne révèle rien) et n'est pas limitée.
		r.Post("/enroll", RateLimitByIP(deps.AuthRateLimiter)(deps.AgentHandler.Enroll))
		r.Post("/auth/login", RateLimitByIP(deps.AuthRateLimiter)(deps.AuthHandler.Login))
		r.Post("/auth/refresh", RateLimitByIP(deps.AuthRateLimiter)(deps.AuthHandler.Refresh))
		r.Post("/auth/logout", deps.AuthHandler.Logout)

		// ── Routes protégées par JWT ──────────────────────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(JWTMiddleware(deps.JWTVerifier))
			r.Use(TenantMiddleware(deps.TenantRepo))

			// Dashboard
			r.Get("/dashboard/summary", deps.DashboardHandler.Summary)

			// Agents
			r.Route("/agents", func(r chi.Router) {
				r.Get("/", RequirePermission("agents", "read")(deps.AgentHandler.List))
				r.Get("/{agentID}", RequirePermission("agents", "read")(deps.AgentHandler.Get))
				r.Patch("/{agentID}", RequirePermission("agents", "write")(deps.AgentHandler.Update))
				r.Delete("/{agentID}", RequirePermission("agents", "delete")(deps.AgentHandler.Delete))
				r.Delete("/{agentID}/certificate", RequirePermission("agents", "delete")(deps.AgentHandler.RevokeCertificate))

				r.Post("/{agentID}/commands", RequirePermission("agents", "execute")(deps.AgentHandler.CreateCommand))
				r.Get("/{agentID}/commands", RequirePermission("agents", "read")(deps.AgentHandler.ListCommands))
				r.Get("/{agentID}/commands/{commandID}", RequirePermission("agents", "read")(deps.AgentHandler.GetCommand))
				r.Post("/bulk-commands", RequirePermission("agents", "execute")(deps.AgentHandler.BulkCreateCommand))
				r.Post("/{agentID}/wake-up", RequirePermission("agents", "execute")(deps.AgentHandler.WakeUp))

				r.Get("/{agentID}/metrics", RequirePermission("metrics", "read")(deps.MetricHandler.Query))
				r.Get("/{agentID}/metrics/latest", RequirePermission("metrics", "read")(deps.MetricHandler.Latest))

				r.Get("/{agentID}/inventory/hardware", RequirePermission("inventory", "read")(deps.InventoryHandler.Hardware))
				r.Get("/{agentID}/inventory/software", RequirePermission("inventory", "read")(deps.InventoryHandler.Software))

				r.Get("/{agentID}/patches", RequirePermission("patches", "read")(deps.PatchHandler.List))
				r.Post("/{agentID}/patches/install", RequirePermission("patches", "execute")(deps.PatchHandler.Install))
				r.Post("/bulk-patches/install", RequirePermission("patches", "execute")(deps.PatchHandler.BulkInstall))
			})

			// Patchs — vue d'ensemble tenant (dashboard)
			r.Get("/patches/summary", RequirePermission("patches", "read")(deps.PatchHandler.Summary))

			// Enrollment tokens
			r.Route("/enrollment-tokens", func(r chi.Router) {
				r.Get("/", RequirePermission("agents", "write")(deps.EnrollmentHandler.List))
				r.Post("/", RequirePermission("agents", "write")(deps.EnrollmentHandler.Create))
				r.Delete("/{tokenID}", RequirePermission("agents", "write")(deps.EnrollmentHandler.Delete))
			})

			// Alertes
			r.Route("/alerts", func(r chi.Router) {
				r.Get("/", RequirePermission("alerts", "read")(deps.AlertHandler.List))
				r.Get("/{alertID}", RequirePermission("alerts", "read")(deps.AlertHandler.Get))
				r.Post("/{alertID}/acknowledge", RequirePermission("alerts", "acknowledge")(deps.AlertHandler.Acknowledge))
			})

			r.Route("/alert-rules", func(r chi.Router) {
				r.Get("/", RequirePermission("alerts", "read")(deps.AlertHandler.ListRules))
				r.Post("/", RequirePermission("alerts", "write")(deps.AlertHandler.CreateRule))
				r.Patch("/{ruleID}", RequirePermission("alerts", "write")(deps.AlertHandler.UpdateRule))
				r.Delete("/{ruleID}", RequirePermission("alerts", "delete")(deps.AlertHandler.DeleteRule))
			})

			// Workspaces
			r.Route("/workspaces", func(r chi.Router) {
				r.Get("/", RequirePermission("workspaces", "read")(deps.WorkspaceHandler.List))
				r.Post("/", RequirePermission("workspaces", "write")(deps.WorkspaceHandler.Create))
				r.Patch("/{workspaceID}", RequirePermission("workspaces", "write")(deps.WorkspaceHandler.Update))
				r.Delete("/{workspaceID}", RequirePermission("workspaces", "delete")(deps.WorkspaceHandler.Delete))
			})

			// Utilisateurs
			r.Route("/users", func(r chi.Router) {
				r.Get("/", RequirePermission("users", "read")(deps.UserHandler.List))
				r.Post("/", RequirePermission("users", "write")(deps.UserHandler.Create))
				r.Get("/{userID}", RequirePermission("users", "read")(deps.UserHandler.Get))
				r.Patch("/{userID}", RequirePermission("users", "write")(deps.UserHandler.Update))
				r.Delete("/{userID}", RequirePermission("users", "delete")(deps.UserHandler.Delete))

				// MFA (opère sur l'utilisateur courant via le JWT)
				r.Post("/me/mfa/enable", deps.UserHandler.MFAEnable)
				r.Post("/me/mfa/confirm", deps.UserHandler.MFAConfirm)
				r.Post("/me/mfa/disable", deps.UserHandler.MFADisable)
			})

			// Rôles et permissions
			r.Route("/roles", func(r chi.Router) {
				r.Get("/", RequirePermission("users", "read")(deps.RoleHandler.List))
				r.Post("/", RequirePermission("users", "write")(deps.RoleHandler.Create))
				r.Patch("/{roleID}", RequirePermission("users", "write")(deps.RoleHandler.Update))
				r.Delete("/{roleID}", RequirePermission("users", "write")(deps.RoleHandler.Delete))
			})
			r.Get("/permissions", RequirePermission("users", "read")(deps.RoleHandler.ListPermissions))

			// Bibliothèque de scripts
			r.Route("/scripts", func(r chi.Router) {
				r.Get("/", RequirePermission("scripts", "read")(deps.ScriptHandler.List))
				r.Post("/", RequirePermission("scripts", "write")(deps.ScriptHandler.Create))
				r.Patch("/{scriptID}", RequirePermission("scripts", "write")(deps.ScriptHandler.Update))
				r.Delete("/{scriptID}", RequirePermission("scripts", "delete")(deps.ScriptHandler.Delete))
			})

			// Planifications récurrentes de scripts
			r.Route("/script-schedules", func(r chi.Router) {
				r.Get("/", RequirePermission("scripts", "read")(deps.ScheduleHandler.List))
				r.Post("/", RequirePermission("scripts", "write")(deps.ScheduleHandler.Create))
				r.Patch("/{scheduleID}", RequirePermission("scripts", "write")(deps.ScheduleHandler.Update))
				r.Delete("/{scheduleID}", RequirePermission("scripts", "delete")(deps.ScheduleHandler.Delete))
			})

			// Tenant
			r.Get("/tenant", RequirePermission("tenant", "read")(deps.TenantHandler.Get))
			r.Patch("/tenant", RequirePermission("tenant", "write")(deps.TenantHandler.Update))

			// Journal d'audit — administrateurs uniquement (voir RequireAdmin)
			r.Get("/audit-log", RequireAdmin(deps.AuditHandler.List))
		})
	})

	return r
}
