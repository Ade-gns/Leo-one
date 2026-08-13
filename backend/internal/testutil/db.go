// Package testutil fournit les helpers communs aux tests d'intégration
// (base Postgres réelle) du backend.
package testutil

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultTestDatabaseURL = "postgres://leo:leo_dev@localhost:5432/leo_one_test?sslmode=disable"

var (
	poolOnce sync.Once
	pool     *pgxpool.Pool
	poolErr  error
)

// TestDB retourne un pool connecté à la base de test Postgres (TEST_DATABASE_URL,
// défaut voir defaultTestDatabaseURL — même identifiants que la base de dev,
// base séparée) et tronque les tables scopées par tenant avant de rendre la
// main, pour une isolation complète entre tests indépendante de leur ordre
// d'exécution.
//
// t.Skip si la base de test n'est pas accessible : les tests d'intégration
// ne doivent jamais faire échouer `go test ./...` dans un environnement qui
// n'a pas cette base (dev sans Postgres, CI sans service configuré) — voir
// docs/ ou la mémoire du projet pour la commande de setup (créer
// leo_one_test, appliquer migrations/001 et 003).
func TestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	poolOnce.Do(func() {
		url := os.Getenv("TEST_DATABASE_URL")
		if url == "" {
			url = defaultTestDatabaseURL
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		p, err := pgxpool.New(ctx, url)
		if err != nil {
			poolErr = err
			return
		}
		if err := p.Ping(ctx); err != nil {
			poolErr = err
			p.Close()
			return
		}
		pool = p
	})

	if poolErr != nil {
		t.Skipf("base de test Postgres indisponible (%v) — voir TEST_DATABASE_URL", poolErr)
	}

	// tenants est le point d'ancrage de tout le schéma multi-tenant : toutes
	// les tables scopées par tenant référencent tenants(id) ON DELETE CASCADE
	// (voir migrations/001_init_schema.sql), donc un seul TRUNCATE CASCADE
	// ici nettoie agents/agent_certificates/enrollment_tokens/workspaces/...
	// sans avoir à connaître le graphe complet des FK.
	if _, err := pool.Exec(context.Background(), `TRUNCATE TABLE tenants RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("échec du nettoyage de la base de test : %v", err)
	}

	return pool
}

// SeedTenant insère un tenant minimal pour les tests et retourne son ID.
func SeedTenant(t *testing.T, pool *pgxpool.Pool, name string, maxAgents int) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tenants (name, slug, max_agents) VALUES ($1, $2, $3)
		RETURNING id
	`, name, slugify(name), maxAgents).Scan(&id)
	if err != nil {
		t.Fatalf("SeedTenant a échoué : %v", err)
	}
	return id
}

// SeedSystemRoles crée les rôles système (Admin/Technicien/Lecture seule)
// pour un tenant, via la fonction seed_system_roles() de migrations/
// 003_rbac_seed.sql — c'est ce que ferait TenantHandler.Create à
// l'enregistrement d'un nouveau tenant en production (pas encore implémenté,
// StubHandler pour l'instant), donc les tests qui en ont besoin (rôles,
// assignation de rôles à un utilisateur) l'appellent explicitement.
func SeedSystemRoles(t *testing.T, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `SELECT seed_system_roles($1)`, tenantID); err != nil {
		t.Fatalf("SeedSystemRoles a échoué : %v", err)
	}
}

// slugify produit un slug suffisamment unique pour l'usage des tests (pas
// une implémentation générale — évite juste les collisions entre appels
// successifs de SeedTenant dans un même test).
func slugify(name string) string {
	out := make([]byte, 0, len(name)+8)
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, byte(r))
		case r >= 'A' && r <= 'Z':
			out = append(out, byte(r-'A'+'a'))
		default:
			out = append(out, '-')
		}
	}
	out = append(out, '-')
	out = append(out, []byte(time.Now().Format("150405.000000000"))...)
	return string(out)
}
