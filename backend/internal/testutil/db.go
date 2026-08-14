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

// testDBLockKey identifie le verrou consultatif Postgres (pg_advisory_lock)
// qui sérialise l'accès à la base de test entre packages — voir le
// commentaire sur son acquisition dans TestDB. Valeur arbitraire, doit juste
// être stable et partagée par tous les appelants visant la même base.
const testDBLockKey = 894127001

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

	// `go test ./...` lance un processus séparé par package, chacun avec son
	// propre pool (poolOnce/pool sont des singletons PAR PROCESSUS), mais
	// tous les packages de tests d'intégration visent la même base
	// leo_one_test. Sans synchronisation inter-processus, deux packages dont
	// les tests touchent la BDD en parallèle (ex: handlers + scheduler)
	// peuvent se TRUNCATEr mutuellement en cours de test, ou provoquer un
	// deadlock Postgres (SQLSTATE 40P01) — observé en pratique dès qu'un
	// deuxième package a commencé à écrire dans la base de test. Un verrou
	// consultatif Postgres, tenu le temps du test courant, sérialise cet
	// accès entre processus sans affecter la vitesse des tests au sein d'un
	// même package (déjà séquentiels par défaut, sans t.Parallel()).
	//
	// Pris sur une connexion dédiée (pool.Acquire), pas via pool.Exec : un
	// verrou consultatif de session est lié à LA connexion qui l'a pris, et
	// pool.Exec peut resservir une connexion différente pour le libérer —
	// ce qui laisserait le verrou tenu indéfiniment sur la connexion
	// d'origine, remise dans le pool, bloquant tous les tests suivants.
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("échec d'acquisition d'une connexion pour le verrou de test : %v", err)
	}
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, testDBLockKey); err != nil {
		conn.Release()
		t.Fatalf("échec d'acquisition du verrou de test : %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, testDBLockKey)
		conn.Release()
	})

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
// 003_rbac_seed.sql — c'est ce que ferait la création d'un tenant en
// production (pas encore implémentée : TenantHandler n'a que Get/Update,
// un tenant ne se crée pas en self-service), donc les tests qui en ont
// besoin (rôles, assignation de rôles à un utilisateur) l'appellent
// explicitement.
func SeedSystemRoles(t *testing.T, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `SELECT seed_system_roles($1)`, tenantID); err != nil {
		t.Fatalf("SeedSystemRoles a échoué : %v", err)
	}
}

// SeedUser insère un utilisateur minimal pour les tests et retourne son ID
// — nécessaire pour tout test touchant une colonne created_by (scripts,
// script_schedules, commands…), qui référence users(id) par clé étrangère :
// une chaîne vide ou un UUID inventé y échoue à l'insertion.
func SeedUser(t *testing.T, pool *pgxpool.Pool, tenantID, email string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (tenant_id, email, password_hash, full_name)
		VALUES ($1, $2, 'x', 'Test User')
		RETURNING id
	`, tenantID, email).Scan(&id)
	if err != nil {
		t.Fatalf("SeedUser a échoué : %v", err)
	}
	return id
}

// SeedAgent insère un agent minimal pour les tests et retourne son ID —
// nécessaire pour tout test touchant une colonne agent_id qui référence
// agents(id) par clé étrangère (script_schedules, commands…).
func SeedAgent(t *testing.T, pool *pgxpool.Pool, tenantID, hostname string) string {
	t.Helper()

	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO agents (tenant_id, hostname, os, os_version, arch, hardware_id, agent_version)
		VALUES ($1, $2, 'linux', '24.04', 'amd64', $3, '1.0.0')
		RETURNING id
	`, tenantID, hostname, hostname+"-hwid").Scan(&id)
	if err != nil {
		t.Fatalf("SeedAgent a échoué : %v", err)
	}
	return id
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
