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

	// Transfert de fichiers (bibliothèque de fichiers déployables, voir
	// internal/domain/file) — stockage sur disque local, pas de dépendance
	// S3/MinIO (cohérent avec docker-compose.yml, qui n'en a pas).
	FileStorageDir     string        // répertoire de stockage des fichiers uploadés
	FileDownloadTTL    time.Duration // durée de vie du token de téléchargement signé (voir deploy-file)
	FileMaxUploadBytes int64         // taille max acceptée par POST /api/v1/files

	// Documentation API interactive (Swagger UI, voir DocsHandler) — routes
	// GET /docs et /docs/openapi.yaml. Activée par défaut en développement
	// uniquement ; à activer explicitement (ENABLE_API_DOCS=true) pour
	// l'exposer ailleurs.
	EnableAPIDocs   bool
	OpenAPISpecPath string

	// Bureau à distance (voir internal/domain/remotedesktop et
	// internal/infrastructure/remotedesktop.Relay) — fenêtre pendant
	// laquelle agent ET navigateur doivent tous deux se connecter au relais
	// après la création d'une session, sans quoi elle expire sans jamais
	// devenir active.
	RemoteDesktopSessionTTL time.Duration
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

// PublicRemoteDesktopWSEndpoint construit l'URL wss:// que l'agent utilise
// pour ouvrir sa connexion dédiée de bureau à distance, suite à une commande
// LEO_MSG_REMOTE_DESKTOP_START (voir RemoteDesktopHandler.createSession) —
// même listener mTLS que PublicWSEndpoint (:8081), route distincte.
func (c *Config) PublicRemoteDesktopWSEndpoint() string {
	_, port, err := net.SplitHostPort(c.WSAgentAddr)
	if err != nil {
		port = "8081"
	}
	return fmt.Sprintf("wss://%s:%s/ws/remote-desktop", c.PublicHost, port)
}

// PublicViewerWSEndpoint construit l'URL ws:// que le navigateur utilise
// pour se connecter au relais de bureau à distance (voir
// RemoteDesktopHandler.ServeViewerWS) — même serveur/port que
// PublicAPIEndpoint, jamais TLS ici non plus (voir son commentaire :
// terminaison TLS éventuelle par un reverse proxy en amont, hors scope de
// ce serveur).
func (c *Config) PublicViewerWSEndpoint() string {
	_, port, err := net.SplitHostPort(c.ServerAddr)
	if err != nil {
		port = "8080"
	}
	return fmt.Sprintf("ws://%s:%s/api/v1/remote-desktop/ws", c.PublicHost, port)
}

// PublicAPIEndpoint construit l'URL http:// de l'API REST que les agents
// utilisent pour télécharger un fichier déployé (voir FileHandler.DeployFile)
// — même serveur que /api/v1/enroll, jamais TLS (voir cmd/server/main.go :
// httpServer.ListenAndServe(), pas ListenAndServeTLS — seul le listener WSS
// agents l'est).
func (c *Config) PublicAPIEndpoint() string {
	_, port, err := net.SplitHostPort(c.ServerAddr)
	if err != nil {
		port = "8080"
	}
	return fmt.Sprintf("http://%s:%s", c.PublicHost, port)
}

// Load lit les variables d'environnement et retourne une Config.
// Retourne une erreur si une variable obligatoire est manquante.
func Load() (*Config, error) {
	env := getEnv("APP_ENV", "development")

	cfg := &Config{
		Env:     env,
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

		// Défaut relatif (./data/files) : même raisonnement que les chemins
		// PKI ci-dessus — le Dockerfile/docker-compose fixe un chemin absolu
		// monté en volume en production.
		FileStorageDir:     getEnv("FILE_STORAGE_DIR", "./data/files"),
		FileDownloadTTL:    getEnvDuration("FILE_DOWNLOAD_TTL", 2*time.Hour),
		FileMaxUploadBytes: int64(getEnvInt("FILE_MAX_UPLOAD_MB", 512)) * 1024 * 1024,

		// Défaut activé seulement en dev (voir env ci-dessus) — jamais en
		// production sauf ENABLE_API_DOCS=true explicite.
		EnableAPIDocs: getEnvBool("ENABLE_API_DOCS", env == "development"),
		// Chemin relatif : fonctionne depuis backend/ (go run ./cmd/server,
		// voir scripts/dev.sh) où docs/ est le répertoire parent immédiat.
		OpenAPISpecPath: getEnv("OPENAPI_SPEC_PATH", "../docs/openapi.yaml"),

		RemoteDesktopSessionTTL: getEnvDuration("REMOTE_DESKTOP_SESSION_TTL", 30*time.Second),
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

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
