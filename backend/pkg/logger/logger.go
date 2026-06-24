// Package logger fournit un logger structuré basé sur slog (Go 1.21+).
// Chaque log inclut le niveau, le timestamp, le message et les attributs clé=valeur.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// contextKey est un type privé pour les clés de contexte.
type contextKey struct{ name string }

var loggerKey = contextKey{"logger"}

// New crée un *slog.Logger configuré selon le niveau et l'environnement.
// En développement : format texte coloré (TextHandler).
// En production    : format JSON (JSONHandler) pour ingestion par les outils de log.
func New(level, env string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: lvl == slog.LevelDebug,
	}

	var handler slog.Handler
	if env == "development" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// WithContext retourne un contexte enrichi avec le logger.
func WithContext(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext extrait le logger du contexte.
// Retourne un logger par défaut si absent.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// WithRequestID retourne un logger enrichi avec le request ID (pour le middleware HTTP).
func WithRequestID(logger *slog.Logger, requestID string) *slog.Logger {
	return logger.With("request_id", requestID)
}

// WithTenant retourne un logger enrichi avec le tenant_id.
func WithTenant(logger *slog.Logger, tenantID string) *slog.Logger {
	return logger.With("tenant_id", tenantID)
}

// WithAgent retourne un logger enrichi avec l'agent_id.
func WithAgent(logger *slog.Logger, agentID string) *slog.Logger {
	return logger.With("agent_id", agentID)
}
