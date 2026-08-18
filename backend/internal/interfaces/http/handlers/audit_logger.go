package handlers

import (
	"context"
	"encoding/json"
	"log/slog"

	auditDomain "github.com/yourorg/leo-one/internal/domain/audit"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
)

// AuditLogger enregistre les actions d'écriture dans le journal d'audit
// (voir internal/domain/audit) — injecté dans les handlers concernés
// (agents, utilisateurs, rôles, scripts, planifications, commandes, tokens
// d'enrollment, alertes) pour un appel en fin de méthode, une fois l'action
// confirmée réussie.
type AuditLogger struct {
	repo auditDomain.Repository
	log  *slog.Logger
}

// NewAuditLogger crée un AuditLogger avec ses dépendances.
func NewAuditLogger(repo auditDomain.Repository, log *slog.Logger) *AuditLogger {
	return &AuditLogger{repo: repo, log: log}
}

// Record enregistre une entrée d'audit de façon best-effort : un échec
// d'écriture est logué mais ne fait jamais échouer le handler appelant — la
// traçabilité ne doit pas devenir un point de défaillance des actions
// métier qu'elle observe. tenant_id/user_id/IP sont extraits du contexte
// (voir JWTMiddleware), donc rien à faire remonter par l'appelant au-delà
// de l'action elle-même.
//
// Nil-safe : un *AuditLogger nil (handler construit sans audit logger, ex.
// dans les tests) fait de Record un no-op, pour éviter de fil-de-fer un
// logger factice dans tous les appels de test qui ne s'intéressent pas à
// l'audit.
//
// resourceID peut être vide (ex. action groupée sans ressource unique) ;
// details peut être nil — ne jamais y passer de secret (mot de passe, token
// brut...), voir les points d'appel.
func (a *AuditLogger) Record(ctx context.Context, action, resourceType, resourceID string, details any) {
	if a == nil {
		return
	}

	tenantID := httpctx.TenantIDFromContext(ctx)
	if tenantID == "" {
		return
	}

	entry := &auditDomain.Entry{
		TenantID:     tenantID,
		Action:       action,
		ResourceType: resourceType,
	}
	if userID := httpctx.UserIDFromContext(ctx); userID != "" {
		entry.UserID = &userID
	}
	if resourceID != "" {
		entry.ResourceID = &resourceID
	}
	if ip := httpctx.IPFromContext(ctx); ip != "" {
		entry.IPAddress = &ip
	}
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			entry.Details = b
		}
	}

	if err := a.repo.Create(ctx, entry); err != nil && a.log != nil {
		a.log.Error("audit: échec d'enregistrement", "error", err, "action", action, "resource_type", resourceType)
	}
}
