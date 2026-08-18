// Package ratelimit fournit un limiteur de débit en mémoire, à fenêtre
// glissante, par clé arbitraire (IP, email, ...). Adapté à un déploiement
// mono-instance du backend (voir docker-compose.yml : un seul service
// "backend", pas de Redis dans la stack) — un déploiement multi-instance
// nécessiterait un compteur partagé (Redis) pour que la limite reste
// cohérente entre instances.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter limite le nombre de tentatives par clé dans une fenêtre glissante.
type Limiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	max      int
	window   time.Duration
}

// New crée un Limiter autorisant au plus max tentatives par clé sur la
// fenêtre glissante window.
func New(max int, window time.Duration) *Limiter {
	return &Limiter{attempts: make(map[string][]time.Time), max: max, window: window}
}

// purgeLocked retire les tentatives de key sorties de la fenêtre glissante
// et réenregistre le résultat (ou retire la clé si elle est vide, pour ne
// pas garder indéfiniment une entrée par IP/email jamais revue) — le mutex
// doit déjà être tenu par l'appelant.
func (l *Limiter) purgeLocked(key string) []time.Time {
	cutoff := time.Now().Add(-l.window)
	kept := l.attempts[key][:0]
	for _, t := range l.attempts[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.attempts, key)
		return nil
	}
	l.attempts[key] = kept
	return kept
}

// Allow enregistre une tentative pour key et retourne false si le nombre de
// tentatives dans la fenêtre glissante dépasse déjà max — chaque appel
// compte, qu'il s'agisse d'un succès ou d'un échec. Utilisé pour le
// throttling générique par IP (voir RateLimitByIP).
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	kept := l.purgeLocked(key)
	if len(kept) >= l.max {
		return false
	}
	l.attempts[key] = append(kept, time.Now())
	return true
}

// Blocked retourne true si key a déjà atteint max tentatives enregistrées
// dans la fenêtre glissante, sans compter d'appel supplémentaire — à
// combiner avec RecordFailure pour un verrouillage par compte qui ne
// compte que les échecs (un login réussi ne doit pas y contribuer, voir
// Reset).
func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.purgeLocked(key)) >= l.max
}

// RecordFailure enregistre un échec pour key.
func (l *Limiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.purgeLocked(key)
	l.attempts[key] = append(kept, time.Now())
}

// Reset efface les tentatives enregistrées pour key — utilisé après une
// authentification réussie pour ne pas laisser d'anciens échecs contribuer
// à un futur verrouillage.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
