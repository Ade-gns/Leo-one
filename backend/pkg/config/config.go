// Package config charge la configuration du serveur Leo-One depuis les variables
// d'environnement. Aucun fichier de config n'est requis : 12-factor app.
package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

// Config contient tous les paramètres de configuration du serveur.
type Config struct {
	// Application
	Env     string // "development" | "production"
	Version string

	// Serveur HTTP (API REST)
	ServerAddr string
	// Serveur WebSocket agents
	WSAgentAddr string

	// Base de données
	DatabaseURL       string
	DBMaxOpenConns    int
	DBMaxIdleConns    int
	DBConnMaxLifetime time.Duration

	// JWT (RS256 — clés en PEM)
	JWTPrivateKeyPath string
	JWTPublicKeyPath  string
	JWTAccessTTL      time.Duration
	JWTRefreshTTL     time.Duration

	// CA interne (signe les certificats agents et le certificat du listener WSS)
	CACertPath     string
	CAKeyPath      string
	ServerCertPath string
	ServerKeyPath  string

	// Nom d'hôte ou IP publique du serveur — utilisé comme SAN du certificat
	// WSS et pour construire le ws_endpoint renvoyé aux agents à l'enrollment.
	PublicHost string

	// Logging
	LogLevel string // "debug" | "info" | "warn" | "error"

	// Rate limiting (routes publiques sensibles : /auth/login, /auth/refresh,
	// /enroll) — voir internal/pkg/ratelimit. Compteur en mémoire, adapté à un
	// déploiement mono-instance (pas de Redis dans docker-compose.yml).
	RateLimitIPMaxAttempts      int // par IP, toutes les routes ci-dessus
	RateLimitIPWindow           time.Duration
	RateLimitAccountMaxFailures int // par email, échecs de /auth/login uniquement
	RateLimitAccountWindow      time.Duration
}

// PublicWSEndpoint construit l'URL wss:// que les agents utilisent pour se
// connecter, à partir de PublicHost et du port de WSAgentAddr.
func (c *Config) PublicWSEndpoint() string {
	_, port, err := net.SplitHostPort(c.WSAgentAddr)
	if err != nil {
		port = "8081"
	}
	return fmt.Sprintf("wss://%s:%s/ws/agent", c.PublicHost, port)
}

// Load lit les variables d'environnement et retourne une Config.
// Retourne une erreur si une variable obligatoire est manquante.
func Load() (*Config, error) {
	cfg := &Config{
		Env:     getEnv("APP_ENV", "development"),
		Version: getEnv("APP_VERSION", "dev"),

		ServerAddr:  getEnv("SERVER_ADDR", "0.0.0.0:8080"),
		WSAgentAddr: getEnv("WS_AGENT_ADDR", "0.0.0.0:8081"),

		DBMaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 5),
		DBConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 5*time.Minute),

		JWTPrivateKeyPath: getEnv("JWT_PRIVATE_KEY_PATH", ""),
		JWTPublicKeyPath:  getEnv("JWT_PUBLIC_KEY_PATH", ""),
		JWTAccessTTL:      getEnvDuration("JWT_ACCESS_TTL", 15*time.Minute),
		JWTRefreshTTL:     getEnvDuration("JWT_REFRESH_TTL", 7*24*time.Hour),

		// Défauts relatifs (./data/pki/...) : fonctionnent aussi bien en dev
		// local (go run) qu'en conteneur — le Dockerfile/docker-compose fixe
		// ces variables sur un chemin absolu monté en volume (/data/pki) pour
		// que la CA survive aux redéploiements.
		CACertPath:     getEnv("CA_CERT_PATH", "./data/pki/ca-cert.pem"),
		CAKeyPath:      getEnv("CA_KEY_PATH", "./data/pki/ca-key.pem"),
		ServerCertPath: getEnv("SERVER_CERT_PATH", "./data/pki/server-cert.pem"),
		ServerKeyPath:  getEnv("SERVER_KEY_PATH", "./data/pki/server-key.pem"),

		PublicHost: getEnv("PUBLIC_HOST", "localhost"),

		LogLevel: getEnv("LOG_LEVEL", "info"),

		RateLimitIPMaxAttempts:      getEnvInt("RATE_LIMIT_IP_MAX_ATTEMPTS", 20),
		RateLimitIPWindow:           getEnvDuration("RATE_LIMIT_IP_WINDOW", time.Minute),
		RateLimitAccountMaxFailures: getEnvInt("RATE_LIMIT_ACCOUNT_MAX_FAILURES", 5),
		RateLimitAccountWindow:      getEnvDuration("RATE_LIMIT_ACCOUNT_WINDOW", 15*time.Minute),
	}

	// Variables obligatoires
	required := map[string]string{
		"DATABASE_URL": "",
	}
	for key := range required {
		val := os.Getenv(key)
		if val == "" {
			return nil, fmt.Errorf("variable d'environnement obligatoire manquante : %s", key)
		}
		required[key] = val
	}
	cfg.DatabaseURL = required["DATABASE_URL"]

	return cfg, nil
}

// IsDevelopment retourne true si l'environnement est "development".
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
