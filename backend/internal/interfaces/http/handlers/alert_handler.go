package handlers

import (
	"net/http"

	"github.com/yourorg/leo-one/internal/pkg/response"
)

// AlertHandler gère les requêtes HTTP pour les alertes et règles d'alerte.
// Les méthodes retournent 501 en attendant l'implémentation complète.
type AlertHandler struct{}

// NewAlertHandler crée un AlertHandler.
func NewAlertHandler() *AlertHandler {
	return &AlertHandler{}
}

func alertStub(w http.ResponseWriter, r *http.Request) {
	response.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "alerts: non encore implémenté")
}

// List retourne la liste des alertes du tenant.
func (h *AlertHandler) List(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// Get retourne une alerte par ID.
func (h *AlertHandler) Get(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// Acknowledge acquitte une alerte.
func (h *AlertHandler) Acknowledge(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// ListRules retourne la liste des règles d'alerte.
func (h *AlertHandler) ListRules(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// CreateRule crée une nouvelle règle d'alerte.
func (h *AlertHandler) CreateRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// UpdateRule met à jour une règle d'alerte existante.
func (h *AlertHandler) UpdateRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }

// DeleteRule supprime une règle d'alerte.
func (h *AlertHandler) DeleteRule(w http.ResponseWriter, r *http.Request) { alertStub(w, r) }
