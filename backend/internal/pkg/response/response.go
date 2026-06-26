// Package response fournit des helpers pour les réponses JSON standardisées de l'API.
package response

import (
	"encoding/json"
	"net/http"
)

// JSON écrit une réponse JSON avec le format {"data": ...}.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// JSONWithMeta écrit une réponse JSON avec le format {"data": ..., "meta": ...}.
func JSONWithMeta(w http.ResponseWriter, status int, data any, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": data,
		"meta": meta,
	})
}

// Error écrit une réponse d'erreur JSON avec le format {"error": {"code": ..., "message": ...}}.
func Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
