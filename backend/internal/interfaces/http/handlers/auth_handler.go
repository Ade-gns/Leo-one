package handlers

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"

	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// AuthHandler gère les requêtes d'authentification.
type AuthHandler struct {
	pool        *pgxpool.Pool
	jwtVerifier *pkgauth.JWTVerifier
	accessTTL   time.Duration
	refreshTTL  time.Duration
}

// NewAuthHandler crée un AuthHandler avec ses dépendances.
func NewAuthHandler(pool *pgxpool.Pool, jwtVerifier *pkgauth.JWTVerifier, accessTTL, refreshTTL time.Duration) *AuthHandler {
	return &AuthHandler{
		pool:        pool,
		jwtVerifier: jwtVerifier,
		accessTTL:   accessTTL,
		refreshTTL:  refreshTTL,
	}
}

// ─── Modèles de requête/réponse ───────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code,omitempty"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// Login authentifie un utilisateur et retourne des tokens JWT.
//
//	POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	if req.Email == "" || req.Password == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "email et mot de passe requis")
		return
	}

	// Lookup utilisateur en BDD
	type userRow struct {
		ID           string
		TenantID     string
		PasswordHash string
		IsActive     bool
		IsAdmin      bool
	}

	var u userRow
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

	if errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "email ou mot de passe invalide")
		return
	}
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	if !u.IsActive {
		response.Error(w, http.StatusForbidden, "FORBIDDEN", "compte désactivé")
		return
	}

	// Vérification du hash argon2id
	if !verifyArgon2id(req.Password, u.PasswordHash) {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "email ou mot de passe invalide")
		return
	}

	// Génération des tokens
	now := time.Now()
	accessClaims := jwt.MapClaims{
		"sub":       u.ID,
		"tenant_id": u.TenantID,
		"is_admin":  u.IsAdmin,
		"type":      "access",
		"iat":       now.Unix(),
		"exp":       now.Add(h.accessTTL).Unix(),
	}
	accessToken, err := h.jwtVerifier.Sign(accessClaims)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de génération du token")
		return
	}

	refreshClaims := jwt.MapClaims{
		"sub":       u.ID,
		"tenant_id": u.TenantID,
		"type":      "refresh",
		"iat":       now.Unix(),
		"exp":       now.Add(h.refreshTTL).Unix(),
	}
	refreshToken, err := h.jwtVerifier.Sign(refreshClaims)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de génération du refresh token")
		return
	}

	// Mettre à jour last_login_at
	_, _ = h.pool.Exec(r.Context(),
		`UPDATE users SET last_login_at = NOW() WHERE id = $1`, u.ID)

	response.JSON(w, http.StatusOK, map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_in":    int(h.accessTTL.Seconds()),
	})
}

// Refresh génère un nouvel access token depuis un refresh token valide.
//
//	POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	claims, err := h.jwtVerifier.Verify(req.RefreshToken)
	if err != nil {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "refresh token invalide ou expiré")
		return
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != "refresh" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "token de type incorrect")
		return
	}

	userID, _ := claims["sub"].(string)
	tenantID, _ := claims["tenant_id"].(string)

	// Vérifier que l'utilisateur existe toujours et est actif
	var isAdmin bool
	err = h.pool.QueryRow(r.Context(), `
		SELECT EXISTS(
		  SELECT 1 FROM user_roles ur
		  JOIN roles ro ON ro.id = ur.role_id
		  WHERE ur.user_id = u.id AND ro.name = 'admin' AND ro.is_system = true
		)
		FROM users u WHERE u.id = $1 AND u.is_active = true
	`, userID).Scan(&isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "utilisateur introuvable ou inactif")
		return
	}
	if err != nil {
		// Fallback: génère le token sans vérifier is_admin
		isAdmin = false
	}

	now := time.Now()
	accessClaims := jwt.MapClaims{
		"sub":       userID,
		"tenant_id": tenantID,
		"is_admin":  isAdmin,
		"type":      "access",
		"iat":       now.Unix(),
		"exp":       now.Add(h.accessTTL).Unix(),
	}
	accessToken, err := h.jwtVerifier.Sign(accessClaims)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de génération du token")
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"expires_in":   int(h.accessTTL.Seconds()),
	})
}

// Logout invalide un refresh token (stateless JWT — retourne simplement 204).
//
//	POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// JWT stateless : pas de blacklist pour l'instant.
	// Le client doit simplement supprimer ses tokens localement.
	w.WriteHeader(http.StatusNoContent)
}

// Health retourne l'état de santé du serveur et de la base de données.
//
//	GET /health
func (h *AuthHandler) Health(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	if err := h.pool.Ping(r.Context()); err != nil {
		dbStatus = "error"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"version": "dev",
		"db":      dbStatus,
	})
}

// ─── Argon2id ─────────────────────────────────────────────────────────────────

// verifyArgon2id vérifie un mot de passe contre un hash au format PHC standard :
// $argon2id$v=19$m=65536,t=3,p=2$<salt_b64>$<hash_b64>
func verifyArgon2id(password, encodedHash string) bool {
	p, salt, hash, err := parseArgon2idHash(encodedHash)
	if err != nil {
		return false
	}

	otherHash := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)
	return subtle.ConstantTimeCompare(hash, otherHash) == 1
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
	keyLen  uint32
}

func parseArgon2idHash(encodedHash string) (p argon2Params, salt, hash []byte, err error) {
	// Format: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
	vals := strings.Split(encodedHash, "$")
	if len(vals) != 6 {
		return p, nil, nil, fmt.Errorf("format de hash invalide")
	}

	if vals[1] != "argon2id" {
		return p, nil, nil, fmt.Errorf("algorithme non supporté: %s", vals[1])
	}

	// vals[2] = "v=19"
	// vals[3] = "m=65536,t=3,p=2"
	params := strings.Split(vals[3], ",")
	if len(params) != 3 {
		return p, nil, nil, fmt.Errorf("paramètres invalides")
	}

	var memory, t, threads int64
	for _, param := range params {
		kv := strings.SplitN(param, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "m":
			memory, err = strconv.ParseInt(kv[1], 10, 64)
		case "t":
			t, err = strconv.ParseInt(kv[1], 10, 64)
		case "p":
			threads, err = strconv.ParseInt(kv[1], 10, 64)
		}
		if err != nil {
			return p, nil, nil, fmt.Errorf("valeur de paramètre invalide: %w", err)
		}
	}

	p = argon2Params{
		memory:  uint32(memory),
		time:    uint32(t),
		threads: uint8(threads),
		keyLen:  32,
	}

	salt, err = base64.RawStdEncoding.DecodeString(vals[4])
	if err != nil {
		return p, nil, nil, fmt.Errorf("sel invalide: %w", err)
	}

	hash, err = base64.RawStdEncoding.DecodeString(vals[5])
	if err != nil {
		return p, nil, nil, fmt.Errorf("hash invalide: %w", err)
	}

	p.keyLen = uint32(len(hash))
	return p, salt, hash, nil
}
