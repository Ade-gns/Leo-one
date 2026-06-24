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
//
//  POST   /api/v1/auth/refresh
//         Body    : {"refresh_token":"..."}
//         Resp 200: {"data":{"access_token":"...","expires_in":900}}
//         Resp 401: refresh token invalide ou expiré
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
// TICKETS  [JWT requis]
// ─────────────────────────────────────────────────────────────────────────────
//
//  GET    /api/v1/tickets
//         Auth    : tickets:read
//         Query   : ?status=&priority=&assigned_to=&agent_id=&cursor=&limit=
//         Resp 200: {"data":[{Ticket}],"meta":{...}}
//
//  POST   /api/v1/tickets
//         Auth    : tickets:write
//         Body    : {
//                     "title":       "Serveur Paris inaccessible",
//                     "description": "...",
//                     "priority":    "high",
//                     "agent_id":    "...",   // optionnel
//                     "alert_id":    "..."    // optionnel
//                   }
//         Resp 201: {"data":{Ticket}}
//
//  GET    /api/v1/tickets/:ticket_id
//         Auth    : tickets:read
//         Resp 200: {"data":{Ticket, "comments":[...]}}
//
//  PATCH  /api/v1/tickets/:ticket_id
//         Auth    : tickets:write
//         Body    : {"status":"...","priority":"...","assigned_to":"..."}
//         Resp 200: {"data":{Ticket}}
//
//  DELETE /api/v1/tickets/:ticket_id
//         Auth    : tickets:delete
//         Resp 204
//
//  POST   /api/v1/tickets/:ticket_id/comments
//         Auth    : tickets:write
//         Body    : {"body":"Problème identifié, intervention en cours."}
//         Resp 201: {"data":{Comment}}
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
//                       "tickets_open":   7,
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
		// Enrollment et auth : pas de JWT
		r.Post("/enroll", deps.AgentHandler.Enroll)
		r.Post("/auth/login", deps.AuthHandler.Login)
		r.Post("/auth/refresh", deps.AuthHandler.Refresh)
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

				r.Post("/{agentID}/commands", RequirePermission("agents", "execute")(deps.AgentHandler.CreateCommand))
				r.Get("/{agentID}/commands", RequirePermission("agents", "read")(deps.AgentHandler.ListCommands))
				r.Get("/{agentID}/commands/{commandID}", RequirePermission("agents", "read")(deps.AgentHandler.GetCommand))

				r.Get("/{agentID}/metrics", RequirePermission("metrics", "read")(deps.MetricHandler.Query))
				r.Get("/{agentID}/metrics/latest", RequirePermission("metrics", "read")(deps.MetricHandler.Latest))

				r.Get("/{agentID}/inventory/hardware", RequirePermission("inventory", "read")(deps.InventoryHandler.Hardware))
				r.Get("/{agentID}/inventory/software", RequirePermission("inventory", "read")(deps.InventoryHandler.Software))
			})

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

			// Tickets
			r.Route("/tickets", func(r chi.Router) {
				r.Get("/", RequirePermission("tickets", "read")(deps.TicketHandler.List))
				r.Post("/", RequirePermission("tickets", "write")(deps.TicketHandler.Create))
				r.Get("/{ticketID}", RequirePermission("tickets", "read")(deps.TicketHandler.Get))
				r.Patch("/{ticketID}", RequirePermission("tickets", "write")(deps.TicketHandler.Update))
				r.Delete("/{ticketID}", RequirePermission("tickets", "delete")(deps.TicketHandler.Delete))
				r.Post("/{ticketID}/comments", RequirePermission("tickets", "write")(deps.TicketHandler.AddComment))
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

			// Tenant
			r.Get("/tenant", RequirePermission("tenant", "read")(deps.TenantHandler.Get))
			r.Patch("/tenant", RequirePermission("tenant", "write")(deps.TenantHandler.Update))
		})
	})

	return r
}
