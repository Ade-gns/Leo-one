package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentDomain "github.com/yourorg/leo-one/internal/domain/agent"
	fileDomain "github.com/yourorg/leo-one/internal/domain/file"
	"github.com/yourorg/leo-one/internal/interfaces/http/httpctx"
	"github.com/yourorg/leo-one/internal/pkg/response"
)

// FileHandler gère la bibliothèque de fichiers déployables du tenant
// courant (upload/liste/suppression) et leur déploiement sur un agent
// (commande file_transfer + URL de téléchargement signée, voir Download).
type FileHandler struct {
	repo              fileDomain.Repository
	agentRepo         agentDomain.Repository
	agentHandler      *AgentHandler // réutilisé pour CreateAndDispatchCommand
	storageDir        string
	downloadTTL       time.Duration
	publicAPIEndpoint string
	maxUploadBytes    int64
	audit             *AuditLogger
}

// NewFileHandler crée un FileHandler avec ses dépendances.
func NewFileHandler(
	repo fileDomain.Repository,
	agentRepo agentDomain.Repository,
	agentHandler *AgentHandler,
	storageDir string,
	downloadTTL time.Duration,
	publicAPIEndpoint string,
	maxUploadBytes int64,
	audit *AuditLogger,
) *FileHandler {
	return &FileHandler{
		repo:              repo,
		agentRepo:         agentRepo,
		agentHandler:      agentHandler,
		storageDir:        storageDir,
		downloadTTL:       downloadTTL,
		publicAPIEndpoint: publicAPIEndpoint,
		maxUploadBytes:    maxUploadBytes,
		audit:             audit,
	}
}

// list : taille max du corps de la requête upload, appliquée en plus de la
// limite mémoire passée à ParseMultipartForm — voir Create.
const fileUploadMemoryBuffer = 32 << 20 // 32 Mio gardés en mémoire, le reste passe par des fichiers temporaires (comportement standard net/http)

// List retourne tous les fichiers de la bibliothèque du tenant courant.
//
//	GET /api/v1/files
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())

	files, err := h.repo.List(r.Context(), tenantID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}

	response.JSON(w, http.StatusOK, files)
}

// Create uploade un nouveau fichier dans la bibliothèque du tenant courant.
// Le contenu est écrit sur disque sous un nom généré (UUID) — jamais le nom
// fourni par le client, pour ne jamais construire un chemin disque à partir
// d'une entrée utilisateur non validée.
//
//	POST /api/v1/files  (multipart/form-data : champ "file", "name" optionnel)
func (h *FileHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())

	if h.maxUploadBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes)
	}

	if err := r.ParseMultipartForm(fileUploadMemoryBuffer); err != nil {
		response.Error(w, http.StatusRequestEntityTooLarge, "VALIDATION_ERROR",
			"corps de requête multipart invalide ou fichier trop volumineux")
		return
	}

	src, header, err := r.FormFile("file")
	if err != nil {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "champ 'file' requis")
		return
	}
	defer src.Close()

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSpace(header.Filename)
	}
	if name == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "nom de fichier requis")
		return
	}

	if err := os.MkdirAll(h.storageDir, 0o755); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de stockage")
		return
	}

	diskName := uuid.New().String()
	storagePath := filepath.Join(h.storageDir, diskName)

	dst, err := os.Create(storagePath)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de stockage")
		return
	}

	hasher := sha256.New()
	written, err := io.Copy(dst, io.TeeReader(src, hasher))
	closeErr := dst.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(storagePath)
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'écriture du fichier")
		return
	}

	f := &fileDomain.File{
		TenantID:       tenantID,
		Name:           name,
		SizeBytes:      written,
		ChecksumSHA256: hex.EncodeToString(hasher.Sum(nil)),
		StoragePath:    storagePath,
	}
	if userID != "" {
		f.UploadedBy = &userID
	}

	if err := h.repo.Create(r.Context(), f); err != nil {
		_ = os.Remove(storagePath)
		if isUniqueViolation(err) {
			response.Error(w, http.StatusConflict, "FILE_ALREADY_EXISTS", "un fichier avec ce nom existe déjà")
			return
		}
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de l'enregistrement du fichier")
		return
	}

	h.audit.Record(r.Context(), "file.create", "file", f.ID,
		map[string]any{"name": f.Name, "size_bytes": f.SizeBytes})
	response.JSON(w, http.StatusCreated, f)
}

// Delete supprime un fichier de la bibliothèque (métadonnées + contenu sur
// disque, best-effort pour ce dernier — une erreur d'os.Remove est loguée
// mais ne fait pas échouer la requête, les métadonnées sont déjà parties).
//
//	DELETE /api/v1/files/:fileID
func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	fileID := chi.URLParam(r, "fileID")

	f, err := h.repo.FindByID(r.Context(), tenantID, fileID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if f == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "fichier introuvable")
		return
	}

	if err := h.repo.Delete(r.Context(), tenantID, fileID); err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la suppression")
		return
	}
	_ = os.Remove(f.StoragePath)

	h.audit.Record(r.Context(), "file.delete", "file", fileID, map[string]any{"name": f.Name})
	w.WriteHeader(http.StatusNoContent)
}

type deployFileRequest struct {
	FileID string `json:"file_id"`
}

// DeployFile envoie une commande file_transfer à un agent, avec une URL de
// téléchargement signée à courte durée de vie pointant vers Download.
//
//	POST /api/v1/agents/:agentID/deploy-file
func (h *FileHandler) DeployFile(w http.ResponseWriter, r *http.Request) {
	tenantID := httpctx.TenantIDFromContext(r.Context())
	userID := httpctx.UserIDFromContext(r.Context())
	agentID := chi.URLParam(r, "agentID")

	agent, err := h.agentRepo.FindByID(r.Context(), tenantID, agentID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if agent == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "agent introuvable")
		return
	}

	var req deployFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.FileID == "" {
		response.Error(w, http.StatusBadRequest, "VALIDATION_ERROR", "file_id est requis")
		return
	}

	f, err := h.repo.FindByID(r.Context(), tenantID, req.FileID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if f == nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "fichier introuvable")
		return
	}

	token, err := h.repo.CreateDownloadToken(r.Context(), f.ID, h.downloadTTL)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la génération du lien de téléchargement")
		return
	}
	downloadURL := h.publicAPIEndpoint + "/api/v1/files/" + f.ID + "/download?token=" + token

	payload, _ := json.Marshal(map[string]any{
		"download_url": downloadURL,
		"filename":     f.Name,
		"sha256":       f.ChecksumSHA256,
		"size_bytes":   f.SizeBytes,
	})
	commandID, sent, err := h.agentHandler.CreateAndDispatchCommand(
		r.Context(), tenantID, agentID, &userID, nil, "file_transfer", payload)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur lors de la création de la commande")
		return
	}

	h.audit.Record(r.Context(), "file.deploy", "agent", agentID,
		map[string]any{"file_id": f.ID, "file_name": f.Name})
	response.JSON(w, http.StatusAccepted, map[string]any{
		"command_id": commandID,
		"status":     "pending",
		"sent":       sent,
	})
}

// Download sert le contenu d'un fichier à l'agent, à partir d'un token de
// téléchargement signé à usage unique — route PUBLIQUE (pas de JWT : l'agent
// n'en a pas, voir router.go). L'ID de fichier dans l'URL n'est
// qu'indicatif : la ressource réellement servie est celle résolue par le
// token (voir ConsumeDownloadToken) — un désaccord entre les deux est
// traité comme une requête invalide plutôt que silencieusement ignoré.
//
//	GET /api/v1/files/:fileID/download?token=...
func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "fileID")
	token := r.URL.Query().Get("token")
	if token == "" {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "token de téléchargement manquant")
		return
	}

	f, err := h.repo.ConsumeDownloadToken(r.Context(), token)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "erreur de base de données")
		return
	}
	if f == nil || f.ID != fileID {
		response.Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "token de téléchargement invalide, expiré, ou déjà utilisé")
		return
	}

	fp, err := os.Open(f.StoragePath)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "fichier introuvable sur le serveur")
		return
	}
	defer fp.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+f.Name+`"`)
	w.Header().Set("X-Checksum-SHA256", f.ChecksumSHA256)
	http.ServeContent(w, r, f.Name, f.CreatedAt, fp)
}
