// Package file définit l'entité File (bibliothèque de fichiers déployables)
// et son interface de domaine. Cette couche ne connaît aucune dépendance
// externe (pas de DB, pas de HTTP).
package file

import (
	"context"
	"time"
)

// File est un fichier déployable de la bibliothèque du tenant.
type File struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Name           string    `json:"name"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	UploadedBy     *string   `json:"uploaded_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`

	// StoragePath est le chemin sur disque local — jamais sérialisé (voir
	// json:"-") : résolu uniquement côté serveur pour servir le
	// téléchargement (voir FileHandler.Download), jamais exposé au client.
	StoragePath string `json:"-"`
}

// Repository définit le contrat de persistance pour les fichiers et leurs
// tokens de téléchargement signés.
// Implémenté dans internal/infrastructure/persistence/postgres/file_repo.go
type Repository interface {
	// Create insère les métadonnées d'un fichier déjà écrit sur disque
	// (voir f.StoragePath). Remplit f.ID/CreatedAt en retour.
	Create(ctx context.Context, f *File) error

	// FindByID retourne un fichier (avec StoragePath) appartenant au tenant
	// donné, ou nil si absent.
	FindByID(ctx context.Context, tenantID, fileID string) (*File, error)

	// List retourne tous les fichiers de la bibliothèque du tenant, triés
	// par nom.
	List(ctx context.Context, tenantID string) ([]*File, error)

	// Delete supprime les métadonnées d'un fichier — l'appelant (handler)
	// est responsable de la suppression du fichier sur disque (voir
	// StoragePath, obtenu via FindByID avant l'appel).
	Delete(ctx context.Context, tenantID, fileID string) error

	// CreateDownloadToken génère un token de téléchargement à usage unique
	// pour fileID, valable ttl. Retourne le token brut (jamais stocké tel
	// quel — voir ConsumeDownloadToken) à inclure dans l'URL signée.
	CreateDownloadToken(ctx context.Context, fileID string, ttl time.Duration) (rawToken string, err error)

	// ConsumeDownloadToken valide rawToken (hash connu, non expiré, non
	// déjà utilisé), le marque utilisé, et retourne le fichier associé
	// (avec StoragePath). Retourne (nil, nil) si le token est invalide,
	// expiré, ou déjà consommé — jamais d'erreur applicative distincte,
	// pour ne pas révéler à l'appelant (non authentifié) la raison précise
	// du rejet.
	ConsumeDownloadToken(ctx context.Context, rawToken string) (*File, error)
}
