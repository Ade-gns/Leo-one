// Package config charge la configuration du serveur Leo-One depuis les variables
// d'environnement. Aucun fichier de config n'est requis : 12-factor app.
package config

import (
	"fmt"
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
	DatabaseURL     string
	DBMaxOpenConns  int
	DBMaxIdleConns  int
	DBConnMaxLifetime time.Duration

	// JWT (RS256 — clés en PEM)
	JWTPrivateKeyPath string
	JWTPublicKeyPath  string
	JWTAccessTTL      time.Duration
	JWTRefreshTTL     time.Duration

	// CA interne (signe les certificats agents)
	CACertPath string
	CAKeyPath  string

	// Logging
	LogLevel string // "debug" | "info" | "warn" | "error"
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

		CACertPath: getEnv("CA_CERT_PATH", ""),
		CAKeyPath:  getEnv("CA_KEY_PATH", ""),

		LogLevel: getEnv("LOG_LEVEL", "info"),
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
