// Test d'intégration du verrouillage par compte sur /api/v1/auth/login —
// nécessite une base Postgres de test réelle (voir internal/testutil.TestDB).
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
	"github.com/yourorg/leo-one/internal/pkg/ratelimit"
	"github.com/yourorg/leo-one/internal/testutil"
)

// TestAuthHandler_Login_AccountLockout_Integration vérifie qu'après N
// tentatives échouées sur un même compte, la requête suivante retourne 429
// RATE_LIMITED plutôt que 401 — même si le mot de passe finit par être
// correct (voir plus bas) : c'est tout l'intérêt du verrouillage.
func TestAuthHandler_Login_AccountLockout_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Login Lockout Corp", 10)
	email := "lockout@example.com"
	// SeedUser fixe password_hash='x' : verifyArgon2id échoue toujours contre
	// ce compte, quel que soit le mot de passe essayé — pratique ici, ça
	// garantit que chaque tentative de ce test est bien un échec.
	testutil.SeedUser(t, pool, tenantID, email)

	const maxFailures = 3
	limiter := ratelimit.New(maxFailures, time.Minute)
	verifier := pkgauth.NewJWTVerifier("test-secret")
	h := NewAuthHandler(pool, verifier, 15*time.Minute, 7*24*time.Hour, limiter)

	loginOnce := func() int {
		body, _ := json.Marshal(map[string]any{"email": email, "password": "wrong-password"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.Login(rec, req)
		return rec.Code
	}

	for i := 0; i < maxFailures; i++ {
		if code := loginOnce(); code != http.StatusUnauthorized {
			t.Fatalf("tentative %d : code = %d, attendu %d (échec d'authentification)", i+1, code, http.StatusUnauthorized)
		}
	}

	if code := loginOnce(); code != http.StatusTooManyRequests {
		t.Fatalf("après %d échecs, code = %d, attendu %d (RATE_LIMITED)", maxFailures, code, http.StatusTooManyRequests)
	}
}

// TestAuthHandler_Login_AccountLockout_ResetsOnSuccess_Integration vérifie
// qu'un login réussi remet le compteur d'échecs à zéro — un utilisateur qui
// se trompe puis retrouve son mot de passe ne doit pas rester pénalisé par
// les tentatives précédentes.
func TestAuthHandler_Login_AccountLockout_ResetsOnSuccess_Integration(t *testing.T) {
	pool := testutil.TestDB(t)
	tenantID := testutil.SeedTenant(t, pool, "Login Reset Corp", 10)
	email := "reset@example.com"
	userID := testutil.SeedUser(t, pool, tenantID, email)

	// Remplace le hash 'x' de SeedUser par un hash valide pour un mot de
	// passe connu, afin de pouvoir provoquer un succès dans ce test.
	validHash := makeArgon2idHash("correct-password", []byte("0123456789abcdef"))
	if _, err := pool.Exec(context.Background(), `UPDATE users SET password_hash = $1 WHERE id = $2`, validHash, userID); err != nil {
		t.Fatalf("setup : échec de mise à jour du hash : %v", err)
	}

	const maxFailures = 2
	limiter := ratelimit.New(maxFailures, time.Minute)
	verifier := pkgauth.NewJWTVerifier("test-secret")
	h := NewAuthHandler(pool, verifier, 15*time.Minute, 7*24*time.Hour, limiter)

	login := func(password string) int {
		body, _ := json.Marshal(map[string]any{"email": email, "password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.Login(rec, req)
		return rec.Code
	}

	if code := login("wrong-password"); code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu %d (échec)", code, http.StatusUnauthorized)
	}
	if code := login("correct-password"); code != http.StatusOK {
		t.Fatalf("code = %d, attendu %d (succès)", code, http.StatusOK)
	}

	// Le compteur ayant été remis à zéro par le succès, un nouvel échec ne
	// doit pas suffire à déclencher le verrouillage (il en faudrait 2 de plus).
	if code := login("wrong-password"); code != http.StatusUnauthorized {
		t.Fatalf("code = %d, attendu %d (le succès précédent doit avoir remis le compteur à zéro)", code, http.StatusUnauthorized)
	}
}
