package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// EnrollmentHandler gère la génération et le cycle de vie des tokens
// d'enrollment (POST /api/v1/enrollment-tokens et associés) — consommés une
// seule fois par AgentHandler.Enroll pour provisionner un nouvel agent.
type EnrollmentHandler struct {
	pool  *pgxpool.Pool
	audit *AuditLogger
}

// NewEnrollmentHandler crée un EnrollmentHandler avec ses dépendances.
func NewEnrollmentHandler(pool *pgxpool.Pool, audit *AuditLogger) *EnrollmentHandler {
	return &EnrollmentHandler{pool: pool, audit: audit}
}

type createEnrollmentTokenRequest struct {
	Label          string `json:"label"`
	ExpiresInHours int    `json:"expires_in_hours"`
	WorkspaceID    string `json:"workspace_id"`
}

const defaultEnrollmentTokenTTLHours = 24

// Create génère un nouveau token d'enrollment pour le tenant courant.
//
//	POST /api/v1/enrollment-tokens
//	Resp 201: {"data":{"id":"...","token":"...","expires_at":"..."}}
//
// Le token brut n'est renvoyé qu'ici, une seule fois — seul son hash SHA-256
// est conservé en BDD (voir AgentHandler.Enroll pour la vérification).
func (h *EnrollmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())

	var req createEnrollmentTokenRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body optionnel : valeurs par défaut si absent/invalide

	ttlHours := req.ExpiresInHours
	if ttlHours <= 0 {
		ttlHours = defaultEnrollmentTokenTTLHours
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de génération du token")
		return
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	var workspaceID *string
	if req.WorkspaceID != "" {
		workspaceID = &req.WorkspaceID
	}
	var label *string
	if req.Label != "" {
		label = &req.Label
	}

	id := uuid.New().String()
	expiresAt := time.Now().UTC().Add(time.Duration(ttlHours) * time.Hour)

	_, err := h.pool.Exec(r.Context(), `
		INSERT INTO enrollment_tokens (id, tenant_id, workspace_id, token_hash, label, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, tenantID, workspaceID, tokenHash, label, expiresAt, nullableUUID(userID))
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création du token")
		return
	}

	// Le token brut n'est jamais inclus dans les détails d'audit — seul son
	// hash est persisté en BDD (voir plus haut), pour la même raison.
	h.audit.Record(r.Context(), "enrollment_token.create", "enrollment_token", id,
		map[string]any{"label": req.Label, "expires_in_hours": ttlHours, "workspace_id": workspaceID})
	response.JSON(w, http.StatusCreated, map[string]any{
		"id":         id,
		"token":      token,
		"expires_at": expiresAt,
	})
}

type enrollmentTokenRow struct {
	ID        string     `json:"id"`
	Label     *string    `json:"label,omitempty"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// List retourne les tokens d'enrollment du tenant courant (sans le token brut).
//
//	GET /api/v1/enrollment-tokens
func (h *EnrollmentHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	rows, err := h.pool.Query(r.Context(), `
		SELECT id, label, expires_at, used_at, created_at
		FROM enrollment_tokens
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	defer rows.Close()

	tokens := make([]enrollmentTokenRow, 0)
	for rows.Next() {
		var t enrollmentTokenRow
		if err := rows.Scan(&t.ID, &t.Label, &t.ExpiresAt, &t.UsedAt, &t.CreatedAt); err != nil {
			response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de lecture")
			return
		}
		tokens = append(tokens, t)
	}

	response.JSON(w, http.StatusOK, tokens)
}

// Delete révoque un token d'enrollment (le supprime — s'il a déjà été
// consommé, ceci n'a aucun effet sur l'agent déjà enrôlé).
//
//	DELETE /api/v1/enrollment-tokens/:tokenID
func (h *EnrollmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	tokenID := chi.URLParam(r, "tokenID")

	tag, err := h.pool.Exec(r.Context(), `
		DELETE FROM enrollment_tokens WHERE id = $1 AND tenant_id = $2
	`, tokenID, tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}
	if tag.RowsAffected() == 0 {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "token introuvable")
		return
	}

	h.audit.Record(r.Context(), "enrollment_token.delete", "enrollment_token", tokenID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// nullableUUID renvoie nil pour une chaîne vide, afin que pgx insère NULL
// plutôt qu'une chaîne UUID invalide (created_by est nullable en BDD).
func nullableUUID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
