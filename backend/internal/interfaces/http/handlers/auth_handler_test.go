package handlers

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
)

// makeArgon2idHash construit un hash au format PHC avec des paramètres
// volontairement faibles pour garder les tests rapides.
func makeArgon2idHash(password string, salt []byte) string {
	const (
		memory  = 8 * 1024
		time_   = 1
		threads = 1
		keyLen  = 32
	)
	hash := argon2.IDKey([]byte(password), salt, time_, memory, threads, keyLen)
	return "$argon2id$v=19$m=" + itoa(memory) + ",t=" + itoa(time_) + ",p=" + itoa(threads) +
		"$" + base64.RawStdEncoding.EncodeToString(salt) +
		"$" + base64.RawStdEncoding.EncodeToString(hash)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestVerifyArgon2id(t *testing.T) {
	salt := []byte("0123456789abcdef")
	hash := makeArgon2idHash("correct-password", salt)

	t.Run("mot de passe correct", func(t *testing.T) {
		if !verifyArgon2id("correct-password", hash) {
			t.Error("attendu true pour le bon mot de passe")
		}
	})

	t.Run("mot de passe incorrect", func(t *testing.T) {
		if verifyArgon2id("wrong-password", hash) {
			t.Error("attendu false pour un mauvais mot de passe")
		}
	})

	t.Run("hash malformé", func(t *testing.T) {
		if verifyArgon2id("correct-password", "not-a-valid-hash") {
			t.Error("attendu false pour un hash malformé")
		}
	})
}

func TestParseArgon2idHash(t *testing.T) {
	salt := []byte("0123456789abcdef")
	valid := makeArgon2idHash("pw", salt)

	t.Run("format valide", func(t *testing.T) {
		p, gotSalt, gotHash, err := parseArgon2idHash(valid)
		if err != nil {
			t.Fatalf("erreur inattendue: %v", err)
		}
		if p.memory != 8*1024 || p.time != 1 || p.threads != 1 {
			t.Errorf("params = %+v, inattendus", p)
		}
		if !bytes.Equal(gotSalt, salt) {
			t.Errorf("salt = %v, attendu %v", gotSalt, salt)
		}
		if len(gotHash) != 32 {
			t.Errorf("hash len = %d, attendu 32", len(gotHash))
		}
	})

	t.Run("nombre de segments incorrect", func(t *testing.T) {
		_, _, _, err := parseArgon2idHash("$argon2id$v=19$m=8192,t=1,p=1$onlysalt")
		if err == nil {
			t.Error("erreur attendue")
		}
	})

	t.Run("algorithme non supporté", func(t *testing.T) {
		_, _, _, err := parseArgon2idHash("$bcrypt$v=19$m=8192,t=1,p=1$c2FsdA$aGFzaA")
		if err == nil {
			t.Error("erreur attendue")
		}
	})

	t.Run("paramètres invalides", func(t *testing.T) {
		_, _, _, err := parseArgon2idHash("$argon2id$v=19$m=8192,t=1$c2FsdA$aGFzaA")
		if err == nil {
			t.Error("erreur attendue")
		}
	})
}

func TestAuthHandler_Logout(t *testing.T) {
	h := NewAuthHandler(nil, nil, time.Minute, time.Hour)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()

	h.Logout(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusNoContent)
	}
}

// Login/Refresh : seules les branches de validation ne touchant pas la BDD
// sont testables sans base de données réelle (le handler utilise *pgxpool.Pool
// directement plutôt qu'une interface injectable).
func TestAuthHandler_Login_ValidationOnly(t *testing.T) {
	h := NewAuthHandler(nil, nil, time.Minute, time.Hour)

	t.Run("corps invalide retourne 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`not-json`))
		rec := httptest.NewRecorder()

		h.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("email manquant retourne 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"password":"secret"}`))
		rec := httptest.NewRecorder()

		h.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("mot de passe manquant retourne 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"a@b.com"}`))
		rec := httptest.NewRecorder()

		h.Login(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})
}

func TestAuthHandler_Refresh_ValidationOnly(t *testing.T) {
	verifier := pkgauth.NewJWTVerifier("test-secret")
	h := NewAuthHandler(nil, verifier, time.Minute, time.Hour)

	t.Run("corps invalide retourne 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBufferString(`not-json`))
		rec := httptest.NewRecorder()

		h.Refresh(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("token invalide retourne 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBufferString(`{"refresh_token":"garbage"}`))
		rec := httptest.NewRecorder()

		h.Refresh(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, attendu %d", rec.Code, http.StatusUnauthorized)
		}
	})

	t.Run("token de type access rejeté sans accès BDD", func(t *testing.T) {
		accessToken, err := verifier.Sign(map[string]any{
			"sub":       "user-1",
			"tenant_id": "tenant-1",
			"type":      "access",
			"exp":       time.Now().Add(time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("erreur de signature: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
			bytes.NewBufferString(`{"refresh_token":"`+accessToken+`"}`))
		rec := httptest.NewRecorder()

		h.Refresh(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d, attendu %d (body=%s)", rec.Code, http.StatusUnauthorized, rec.Body.String())
		}
	})
}
