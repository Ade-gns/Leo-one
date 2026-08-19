package http

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	tenantDomain "github.com/yourorg/leo-one/internal/domain/tenant"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
	"github.com/yourorg/leo-one/internal/pkg/ratelimit"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

type rbacPoolKey struct{}

// RBACMiddleware rend le pool disponible aux guards de permission sans
// l'exposer aux handlers. Un pool absent est traité fail-closed.
func RBACMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), rbacPoolKey{}, pool)))
		})
	}
}

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
			ctx = httpctx.WithIP(ctx, clientIP(r))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RateLimitByIP limite le nombre de requêtes par IP cliente sur la route
// décorée (fenêtre glissante portée par rl) — utilisé sur les routes
// publiques sensibles (login, refresh, enroll) pour limiter le brute force.
// Voir aussi le verrouillage par compte dans AuthHandler.Login : une limite
// distincte, par email, qui ne compte que les échecs d'authentification.
func RateLimitByIP(rl *ratelimit.Limiter) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow(clientIP(r)) {
				response.Error(w, http.StatusTooManyRequests, "RATE_LIMITED", "trop de requêtes, réessayez plus tard")
				return
			}
			next(w, r)
		}
	}
}

// clientIP extrait l'adresse IP seule de r.RemoteAddr — normalisé en amont
// par middleware.RealIP (voir router.go) selon X-Forwarded-For/X-Real-IP
// quand présents, sinon adresse de la connexion TCP brute (host:port). Utilisé
// pour la traçabilité du journal d'audit (voir internal/domain/audit).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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

// RequirePermission vérifie que l'utilisateur possède réellement la
// permission demandée via ses rôles du tenant courant. Les administrateurs
// gardent l'accès complet, mais les autres comptes sont refusés par défaut.
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

			pool, _ := r.Context().Value(rbacPoolKey{}).(*pgxpool.Pool)
			if pool == nil {
				response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "RBAC non configuré")
				return
			}
			var allowed bool
			err := pool.QueryRow(r.Context(), `
				SELECT EXISTS(
					SELECT 1
					FROM user_roles ur
					JOIN roles ro ON ro.id = ur.role_id
					JOIN role_permissions rp ON rp.role_id = ro.id
					JOIN permissions p ON p.id = rp.permission_id
					WHERE ur.user_id = $1 AND ro.tenant_id = $2
					  AND p.resource = $3 AND p.action = $4
				)
			`, httpctx.UserIDFromContext(r.Context()), httpctx.TenantIDFromContext(r.Context()), resource, action).Scan(&allowed)
			if err != nil {
				response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de vérification des permissions")
				return
			}
			if allowed {
				next(w, r)
				return
			}

			response.Error(w, http.StatusForbidden, "FORBIDDEN",
				"permission insuffisante: "+resource+":"+action+" requise")
		}
	}
}

// RequireAdmin restreint une route aux utilisateurs administrateurs (claim
// is_admin du JWT), quel que soit leur rôle personnalisé. Utilisé pour le
// journal d'audit : même si un rôle personnalisé se voit accorder la
// permission "audit:read" via RequirePermission, l'historique d'audit
// touche potentiellement les actions de TOUS les utilisateurs du tenant
// (pas seulement les siennes) — un choix de politique délibéré (pas un
// contournement d'un stub) de garder cette route strictement réservée aux
// administrateurs plutôt que RBAC-granulaire comme le reste de l'API.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !httpctx.IsAdminFromContext(r.Context()) {
			response.Error(w, http.StatusForbidden, "FORBIDDEN", "réservé aux administrateurs")
			return
		}
		next(w, r)
	}
}
