// Package httpctx définit les clés de contexte HTTP partagées entre les middlewares
// et les handlers. Ce package séparé évite les imports circulaires.
package httpctx

import "context"

// contextKey est un type opaque pour les clés de contexte — évite les collisions.
type contextKey string

const (
	keyUserID   contextKey = "user_id"
	keyTenantID contextKey = "tenant_id"
	keyIsAdmin  contextKey = "is_admin"
)

// WithUserID stocke le user_id dans le contexte.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, keyUserID, userID)
}

// WithTenantID stocke le tenant_id dans le contexte.
func WithTenantID(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, keyTenantID, tenantID)
}

// WithIsAdmin stocke le flag is_admin dans le contexte.
func WithIsAdmin(ctx context.Context, isAdmin bool) context.Context {
	return context.WithValue(ctx, keyIsAdmin, isAdmin)
}

// UserIDFromContext extrait le user_id du contexte.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyUserID).(string)
	return v
}

// TenantIDFromContext extrait le tenant_id du contexte.
func TenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(keyTenantID).(string)
	return v
}

// IsAdminFromContext extrait le flag is_admin du contexte.
func IsAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(keyIsAdmin).(bool)
	return v
}
