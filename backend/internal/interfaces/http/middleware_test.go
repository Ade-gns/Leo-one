package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yourorg/leo-one/internal/pkg/ratelimit"
)

// TestRateLimitByIP_BlocksAfterMax vérifie qu'après N requêtes depuis la
// même IP, la requête suivante retourne 429 RATE_LIMITED — le scénario que
// RateLimitByIP est censé bloquer sur /auth/login, /auth/refresh, /enroll.
func TestRateLimitByIP_BlocksAfterMax(t *testing.T) {
	const max = 3
	limited := RateLimitByIP(ratelimit.New(max, time.Minute))(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	call := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = "203.0.113.9:54321"
		rec := httptest.NewRecorder()
		limited(rec, req)
		return rec.Code
	}

	for i := 0; i < max; i++ {
		if code := call(); code != http.StatusOK {
			t.Fatalf("requête %d : code = %d, attendu %d", i+1, code, http.StatusOK)
		}
	}

	if code := call(); code != http.StatusTooManyRequests {
		t.Fatalf("après %d requêtes, code = %d, attendu %d", max, code, http.StatusTooManyRequests)
	}
}

// TestRateLimitByIP_IndependentPerIP vérifie qu'une IP différente n'est pas
// affectée par la limite atteinte par une autre — sinon un seul client
// bruyant bloquerait tout le monde.
func TestRateLimitByIP_IndependentPerIP(t *testing.T) {
	limited := RateLimitByIP(ratelimit.New(1, time.Minute))(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	call := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		limited(rec, req)
		return rec.Code
	}

	if code := call("203.0.113.1"); code != http.StatusOK {
		t.Fatalf("première requête de 203.0.113.1 : code = %d, attendu %d", code, http.StatusOK)
	}
	if code := call("203.0.113.2"); code != http.StatusOK {
		t.Fatalf("première requête d'une autre IP : code = %d, attendu %d", code, http.StatusOK)
	}
	if code := call("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("deuxième requête de 203.0.113.1 : code = %d, attendu %d", code, http.StatusTooManyRequests)
	}
}
