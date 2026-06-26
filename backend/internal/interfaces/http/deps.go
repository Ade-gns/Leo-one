package http

import (
	"log/slog"

	tenantDomain "github.com/yourorg/leo-one/internal/domain/tenant"
	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"

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
	InventoryHandler  *handlers.StubHandler
	AlertHandler      *handlers.AlertHandler
	TicketHandler     *handlers.StubHandler
	WorkspaceHandler  *handlers.StubHandler
	UserHandler       *handlers.StubHandler
	RoleHandler       *handlers.StubHandler
	TenantHandler     *handlers.StubHandler
	EnrollmentHandler *handlers.StubHandler

	// Infrastructure
	JWTVerifier *pkgauth.JWTVerifier
	TenantRepo  tenantDomain.Repository

	// Observabilité
	Logger *slog.Logger
}
