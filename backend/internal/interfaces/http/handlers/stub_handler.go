// Package handlers contient les handlers HTTP pour l'API Leo-One RMM.
package handlers

import (
	"net/http"

	"github.com/yourorg/leo-one/internal/pkg/response"
)

// StubHandler est un handler générique qui retourne 501 Not Implemented.
// Il est utilisé pour les fonctionnalités non encore implémentées.
type StubHandler struct{}

func stub501(w http.ResponseWriter, r *http.Request) {
	response.Error(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", "cette fonctionnalité n'est pas encore disponible")
}

func (h *StubHandler) List(w http.ResponseWriter, r *http.Request)            { stub501(w, r) }
func (h *StubHandler) Get(w http.ResponseWriter, r *http.Request)             { stub501(w, r) }
func (h *StubHandler) Create(w http.ResponseWriter, r *http.Request)          { stub501(w, r) }
func (h *StubHandler) Update(w http.ResponseWriter, r *http.Request)          { stub501(w, r) }
func (h *StubHandler) Delete(w http.ResponseWriter, r *http.Request)          { stub501(w, r) }
func (h *StubHandler) Acknowledge(w http.ResponseWriter, r *http.Request)     { stub501(w, r) }
func (h *StubHandler) CreateRule(w http.ResponseWriter, r *http.Request)      { stub501(w, r) }
func (h *StubHandler) UpdateRule(w http.ResponseWriter, r *http.Request)      { stub501(w, r) }
func (h *StubHandler) DeleteRule(w http.ResponseWriter, r *http.Request)      { stub501(w, r) }
func (h *StubHandler) ListRules(w http.ResponseWriter, r *http.Request)       { stub501(w, r) }
func (h *StubHandler) AddComment(w http.ResponseWriter, r *http.Request)      { stub501(w, r) }
func (h *StubHandler) Hardware(w http.ResponseWriter, r *http.Request)        { stub501(w, r) }
func (h *StubHandler) Software(w http.ResponseWriter, r *http.Request)        { stub501(w, r) }
func (h *StubHandler) MFAEnable(w http.ResponseWriter, r *http.Request)       { stub501(w, r) }
func (h *StubHandler) MFAConfirm(w http.ResponseWriter, r *http.Request)      { stub501(w, r) }
func (h *StubHandler) MFADisable(w http.ResponseWriter, r *http.Request)      { stub501(w, r) }
func (h *StubHandler) ListPermissions(w http.ResponseWriter, r *http.Request) { stub501(w, r) }
func (h *StubHandler) Summary(w http.ResponseWriter, r *http.Request)         { stub501(w, r) }
