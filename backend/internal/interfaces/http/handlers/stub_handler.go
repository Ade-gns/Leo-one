// Package handlers contient les handlers HTTP pour l'API Leo-One RMM.
package handlers

import (
	"net/http"

	"github.com/yourorg/leo-one/internal/pkg/response"
)

// stub501 répond 501 Not Implemented — utilisé par les endpoints d'une
// fonctionnalité à part entière pas encore implémentée (MFA sur
// UserHandler, gestion des rôles personnalisés sur RoleHandler).
func stub501(w http.ResponseWriter, _ *http.Request) {
	response.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "cette fonctionnalité n'est pas encore disponible")
}
