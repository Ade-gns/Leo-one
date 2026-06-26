package http

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	tenantDomain "github.com/yourorg/leo-one/internal/domain/tenant"
	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// LoggerMiddleware logue chaque requête HTTP avec le temps de traitement et le status code.
func LoggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := middleware.GetReqID(r.Context())

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"latency_ms", time.Since(start).Milliseconds(),
				"bytes", ww.BytesWritten(),
				"request_id", reqID,
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

// JWTMiddleware vérifie le token Bearer JWT (HS256) et stocke les claims dans le contexte.
// Retourne 401 si le token est absent, malformé ou expiré.
func JWTMiddleware(verifier *pkgauth.JWTVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "token JWT manquant")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "format Authorization invalide, attendu: Bearer <token>")
				return
			}

			claims, err := verifier.Verify(parts[1])
			if err != nil {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "token JWT invalide ou expiré")
				return
			}

			// Vérifier que c'est un access token (pas un refresh token)
			if tokenType, _ := claims["type"].(string); tokenType != "" && tokenType != "access" {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "type de token incorrect")
				return
			}

			userID, _ := claims["sub"].(string)
			tenantID, _ := claims["tenant_id"].(string)
			isAdmin, _ := claims["is_admin"].(bool)

			ctx := r.Context()
			ctx = httpctx.WithUserID(ctx, userID)
			ctx = httpctx.WithTenantID(ctx, tenantID)
			ctx = httpctx.WithIsAdmin(ctx, isAdmin)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantMiddleware charge le tenant depuis le repo et le place dans le contexte.
// Le tenant_id est extrait des claims JWT mis en contexte par JWTMiddleware.
// Retourne 401 si le tenant est introuvable ou inactif.
func TenantMiddleware(repo tenantDomain.Repository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := httpctx.TenantIDFromContext(r.Context())
			if tenantID == "" {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant_id manquant dans le token JWT")
				return
			}

			tenant, err := repo.FindByID(r.Context(), tenantID)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors du chargement du tenant")
				return
			}
			if tenant == nil {
				response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "tenant introuvable")
				return
			}
			if !tenant.IsActive {
				response.Error(w, http.StatusForbidden, "FORBIDDEN", "tenant désactivé")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission vérifie que l'utilisateur possède la permission requise.
// Implémentation simplifiée : les admins ont toutes les permissions.
// Pour les non-admins, seules les actions "read" sont autorisées (RBAC complet = Phase suivante).
//
// Retourne func(http.HandlerFunc) http.HandlerFunc pour être compatible avec le
// pattern d'appel inline utilisé dans router.go :
//
//	r.Get("/", RequirePermission("agents", "read")(deps.AgentHandler.List))
func RequirePermission(resource, action string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			isAdmin := httpctx.IsAdminFromContext(r.Context())
			if isAdmin {
				next(w, r)
				return
			}

			// TODO : implémenter la vérification RBAC complète (rôles/permissions en BDD)
			// Pour l'instant, les non-admins ont accès aux routes en lecture seule.
			if action == "read" {
				next(w, r)
				return
			}

			response.Error(w, http.StatusForbidden, "FORBIDDEN",
				"permission insuffisante: "+resource+":"+action+" requise")
		}
	}
}
