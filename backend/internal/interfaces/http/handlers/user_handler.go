package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	userDomain "github.com/yourorg/leo-one/internal/domain/user"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// minPasswordLen borne basse volontairement modeste (pas de politique de
// complexité) — un mot de passe créé par un admin pour un tiers, à changer
// au premier login dans un futur flux ; le vrai gate de sécurité reste
// argon2id + MFA (quand implémenté), pas la longueur seule.
const minPasswordLen = 8

// UserHandler gère les requêtes HTTP pour les utilisateurs du tenant courant.
type UserHandler struct {
	repo userDomain.Repository
}

// NewUserHandler crée un UserHandler avec ses dépendances.
func NewUserHandler(repo userDomain.Repository) *UserHandler {
	return &UserHandler{repo: repo}
}

// List retourne tous les utilisateurs du tenant courant.
//
//	GET /api/v1/users
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	users, err := h.repo.List(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	response.JSON(w, http.StatusOK, users)
}

// Get retourne un utilisateur par son ID.
//
//	GET /api/v1/users/:userID
func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := chi.URLParam(r, "userID")

	u, err := h.repo.FindByID(r.Context(), tenantID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if u == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "utilisateur introuvable")
		return
	}

	response.JSON(w, http.StatusOK, u)
}

type createUserRequest struct {
	Email    string   `json:"email"`
	FullName string   `json:"full_name"`
	Password string   `json:"password"`
	RoleIDs  []string `json:"role_ids"`
}

// Create crée un nouvel utilisateur dans le tenant courant. Pas de flux
// d'invitation par email (aucun service d'envoi de mail dans ce projet) —
// l'admin fixe directement le mot de passe initial, à faire changer par
// l'utilisateur à la première connexion (pas encore forcé côté backend).
//
//	POST /api/v1/users
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.FullName = strings.TrimSpace(req.FullName)
	if req.Email == "" || req.FullName == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "email et full_name sont requis")
		return
	}
	if len(req.Password) < minPasswordLen {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR",
			fmt.Sprintf("le mot de passe doit faire au moins %d caractères", minPasswordLen))
		return
	}

	existing, err := h.repo.FindByEmail(r.Context(), tenantID, req.Email)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if existing != nil {
		response.Error(w, http.StatusConflict, "USER_ALREADY_EXISTS", "un utilisateur avec cet email existe déjà")
		return
	}

	hash, err := hashArgon2id(req.Password)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors du hachage du mot de passe")
		return
	}

	u := &userDomain.User{
		TenantID: tenantID,
		Email:    req.Email,
		FullName: req.FullName,
	}
	if err := h.repo.Create(r.Context(), u, hash); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de l'utilisateur")
		return
	}

	if len(req.RoleIDs) > 0 {
		if !h.assignRoles(w, r, tenantID, u.ID, req.RoleIDs) {
			return
		}
		// Recharge pour renvoyer les rôles nouvellement assignés.
		if reloaded, err := h.repo.FindByID(r.Context(), tenantID, u.ID); err == nil && reloaded != nil {
			u = reloaded
		}
	}

	response.JSON(w, http.StatusCreated, u)
}

type updateUserRequest struct {
	FullName *string   `json:"full_name"`
	IsActive *bool     `json:"is_active"`
	RoleIDs  *[]string `json:"role_ids"`
}

// Update met à jour partiellement un utilisateur (full_name/is_active/
// role_ids — jamais l'email ni le mot de passe via cette route).
//
//	PATCH /api/v1/users/:userID
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := chi.URLParam(r, "userID")

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "corps de requête invalide")
		return
	}

	current, err := h.repo.FindByID(r.Context(), tenantID, userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if current == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "utilisateur introuvable")
		return
	}

	// Empêche de se désactiver soi-même — sans ça un admin seul dans son
	// tenant pourrait se verrouiller lui-même hors de la console.
	if req.IsActive != nil && !*req.IsActive && userID == httpctx.UserIDFromContext(r.Context()) {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "impossible de désactiver son propre compte")
		return
	}

	if req.FullName != nil {
		trimmed := strings.TrimSpace(*req.FullName)
		if trimmed == "" {
			response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "full_name ne peut pas être vide")
			return
		}
		current.FullName = trimmed
	}
	if req.IsActive != nil {
		current.IsActive = *req.IsActive
	}

	if err := h.repo.Update(r.Context(), current); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la mise à jour")
		return
	}

	if req.RoleIDs != nil {
		if !h.assignRoles(w, r, tenantID, userID, *req.RoleIDs) {
			return
		}
	}

	updated, err := h.repo.FindByID(r.Context(), tenantID, userID)
	if err != nil || updated == nil {
		response.JSON(w, http.StatusOK, current)
		return
	}
	response.JSON(w, http.StatusOK, updated)
}

// assignRoles appelle SetRoles et écrit la réponse d'erreur appropriée si
// un role_id est invalide/étranger au tenant. Retourne false si une réponse
// d'erreur a déjà été écrite (l'appelant doit alors arrêter immédiatement).
func (h *UserHandler) assignRoles(w http.ResponseWriter, r *http.Request, tenantID, userID string, roleIDs []string) bool {
	assigned, err := h.repo.SetRoles(r.Context(), tenantID, userID, roleIDs)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'assignation des rôles")
		return false
	}
	if assigned != len(roleIDs) {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "un ou plusieurs role_ids sont invalides")
		return false
	}
	return true
}

// Delete supprime définitivement un utilisateur.
//
//	DELETE /api/v1/users/:userID
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := chi.URLParam(r, "userID")

	if userID == httpctx.UserIDFromContext(r.Context()) {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "impossible de supprimer son propre compte")
		return
	}

	if err := h.repo.Delete(r.Context(), tenantID, userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "NOT_FOUND", "utilisateur introuvable")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ─── MFA ────────────────────────────────────────────────────────────────────
//
// Pas encore implémenté (TOTP : génération/chiffrement du secret, QR code,
// codes de secours — une fonctionnalité à part entière, hors périmètre de
// l'implémentation initiale de UserHandler). Renvoie 501 (voir stub501).

func (h *UserHandler) MFAEnable(w http.ResponseWriter, r *http.Request)  { stub501(w, r) }
func (h *UserHandler) MFAConfirm(w http.ResponseWriter, r *http.Request) { stub501(w, r) }
func (h *UserHandler) MFADisable(w http.ResponseWriter, r *http.Request) { stub501(w, r) }
