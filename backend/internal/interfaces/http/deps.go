package http

import (
	"log/slog"

	tenantDomain "github.com/yourorg/leo-one/internal/domain/tenant"
	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
	"github.com/yourorg/leo-one/internal/pkg/ratelimit"

	"github.com/yourorg/leo-one/internal/interfaces/http/handlers"
)

// Dependencies regroupe toutes les dépendances injectées dans le routeur Chi.
// Chaque champ correspond à un groupe de routes dans router.go.
type Dependencies struct {
	// Handlers métier
	AuthHandler       *handlers.AuthHandler
	AgentHandler      *handlers.AgentHandler
	DashboardHandler  *handlers.DashboardHandler
	MetricHandler     *handlers.MetricHandler
	InventoryHandler  *handlers.InventoryHandler
	AlertHandler      *handlers.AlertHandler
	WorkspaceHandler  *handlers.WorkspaceHandler
	UserHandler       *handlers.UserHandler
	RoleHandler       *handlers.RoleHandler
	TenantHandler     *handlers.TenantHandler
	EnrollmentHandler *handlers.EnrollmentHandler
	ScriptHandler     *handlers.ScriptHandler
	ScheduleHandler   *handlers.ScheduleHandler
	AuditHandler      *handlers.AuditHandler

	// Infrastructure
	JWTVerifier     *pkgauth.JWTVerifier
	TenantRepo      tenantDomain.Repository
	AuditLogger     *handlers.AuditLogger
	AuthRateLimiter *ratelimit.Limiter // rate limiting par IP sur /auth/*, /enroll

	// Observabilité
	Logger *slog.Logger
}
