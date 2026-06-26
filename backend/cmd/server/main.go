// main.go — Point d'entrée du serveur Leo-One Backend
//
// Responsabilités :
//  1. Chargement de la configuration (variables d'environnement)
//  2. Initialisation du logger structuré
//  3. Connexion à la base de données (pgx pool)
//  4. Construction du graphe de dépendances (Dependency Injection manuelle)
//  5. Démarrage des serveurs HTTP (API REST) et WSS (agents) sur deux ports
//  6. Gestion des signaux pour un arrêt propre (graceful shutdown)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/infrastructure/persistence/postgres"
	"github.com/yourorg/leo-one/internal/infrastructure/websocket"
	chiRouter "github.com/yourorg/leo-one/internal/interfaces/http"
	"github.com/yourorg/leo-one/internal/interfaces/http/handlers"
	wsHandler "github.com/yourorg/leo-one/internal/interfaces/ws"
	pkgauth "github.com/yourorg/leo-one/internal/pkg/auth"
	"github.com/yourorg/leo-one/pkg/config"
	"github.com/yourorg/leo-one/pkg/logger"
)

func main() {
	// ── Configuration ──────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Erreur de configuration", "error", err)
		os.Exit(1)
	}

	// JWT_SECRET : lu directement depuis l'environnement (HS256)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "leo-one-dev-secret-change-in-production"
		slog.Warn("JWT_SECRET non défini — utilisation d'un secret de développement")
	}

	// ── Logger ─────────────────────────────────────────────────────────────
	log := logger.New(cfg.LogLevel, cfg.Env)
	slog.SetDefault(log)

	log.Info("═══════════════════════════════════════════════")
	log.Info(" Leo-One Backend démarrage",
		"version", cfg.Version,
		"env", cfg.Env)
	log.Info("═══════════════════════════════════════════════")

	// ── Pool de connexions PostgreSQL ──────────────────────────────────────
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		log.Error("URL de base de données invalide", "error", err)
		os.Exit(1)
	}
	poolCfg.MaxConns = int32(cfg.DBMaxOpenConns)
	poolCfg.MinConns = int32(cfg.DBMaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.DBConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		log.Error("Impossible de se connecter à PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("PostgreSQL inaccessible", "error", err)
		os.Exit(1)
	}
	log.Info("PostgreSQL connecté", "url_host", poolCfg.ConnConfig.Host)

	// ── Injection de dépendances ────────────────────────────────────────────
	//
	// Ordre d'initialisation :
	//   repos (persistence) → dispatcher → hub → handlers → routers

	// Repos
	agentRepo  := postgres.NewAgentRepo(pool)
	metricRepo := postgres.NewMetricRepo(pool)
	tenantRepo := postgres.NewTenantRepo(pool)

	// WebSocket
	dispatcher := websocket.NewDispatcher(agentRepo, metricRepo, log)
	hub         := websocket.NewHub(dispatcher, log)
	dispatcher.SetHub(hub)

	agentWSH := wsHandler.NewAgentWSHandler(hub, log)

	// Auth
	jwtVerifier := pkgauth.NewJWTVerifier(jwtSecret)

	// Handlers
	authHandler      := handlers.NewAuthHandler(pool, jwtVerifier, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	agentHandler     := handlers.NewAgentHandler(agentRepo, pool, hub)
	metricHandler    := handlers.NewMetricHandler(metricRepo)
	dashboardHandler := handlers.NewDashboardHandler(pool)
	alertHandler     := handlers.NewAlertHandler()
	stubHandler      := &handlers.StubHandler{}

	// Routeur API REST (Chi)
	deps := &chiRouter.Dependencies{
		AuthHandler:       authHandler,
		AgentHandler:      agentHandler,
		DashboardHandler:  dashboardHandler,
		MetricHandler:     metricHandler,
		InventoryHandler:  stubHandler,
		AlertHandler:      alertHandler,
		TicketHandler:     stubHandler,
		WorkspaceHandler:  stubHandler,
		UserHandler:       stubHandler,
		RoleHandler:       stubHandler,
		TenantHandler:     stubHandler,
		EnrollmentHandler: stubHandler,
		JWTVerifier:       jwtVerifier,
		TenantRepo:        tenantRepo,
		Logger:            log,
	}
	apiRouter := chiRouter.NewRouter(deps)

	// ── Serveur HTTP — API REST (:8080) ────────────────────────────────────
	httpServer := &http.Server{
		Addr:         cfg.ServerAddr,
		Handler:      apiRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// ── Serveur HTTP — WebSocket agents (:8081) ────────────────────────────
	wsMux := http.NewServeMux()
	wsMux.Handle("/ws/agent", agentWSH)
	wsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	wsServer := &http.Server{
		Addr:        cfg.WSAgentAddr,
		Handler:     wsMux,
		ReadTimeout: 0,  // pas de timeout de lecture pour les connexions WS longues
		IdleTimeout: 0,
	}

	// ── Démarrage des serveurs ─────────────────────────────────────────────
	go func() {
		log.Info("Serveur API REST démarré", "addr", cfg.ServerAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Erreur serveur HTTP", "error", err)
			os.Exit(1)
		}
	}()

	go func() {
		log.Info("Serveur WebSocket agents démarré", "addr", cfg.WSAgentAddr)
		if err := wsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Erreur serveur WebSocket", "error", err)
			os.Exit(1)
		}
	}()

	// ── Attente du signal d'arrêt ──────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit

	log.Info("Signal reçu — arrêt en cours", "signal", sig)

	// Graceful shutdown : 30 secondes max pour finir les requêtes en cours.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("Erreur lors de l'arrêt du serveur HTTP", "error", err)
	}
	if err := wsServer.Shutdown(shutdownCtx); err != nil {
		log.Error("Erreur lors de l'arrêt du serveur WebSocket", "error", err)
	}

	log.Info("Serveur arrêté proprement")
}
