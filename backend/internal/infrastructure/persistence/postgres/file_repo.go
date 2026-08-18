package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fileDomain "github.com/yourorg/leo-one/internal/domain/file"
)

// FileRepo implémente file.Repository via pgx/v5.
type FileRepo struct {
	pool *pgxpool.Pool
}

// NewFileRepo crée un FileRepo avec le pool de connexions fourni.
func NewFileRepo(pool *pgxpool.Pool) *FileRepo {
	return &FileRepo{pool: pool}
}

// Create insère les métadonnées d'un fichier déjà écrit sur disque.
func (r *FileRepo) Create(ctx context.Context, f *fileDomain.File) error {
	ctx = ensureCtx(ctx)

	return r.pool.QueryRow(ctx, `
		INSERT INTO files (tenant_id, name, size_bytes, checksum_sha256, storage_path, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`, f.TenantID, f.Name, f.SizeBytes, f.ChecksumSHA256, f.StoragePath, f.UploadedBy).Scan(&f.ID, &f.CreatedAt)
}

// FindByID retourne un fichier appartenant au tenant donné.
func (r *FileRepo) FindByID(ctx context.Context, tenantID, fileID string) (*fileDomain.File, error) {
	ctx = ensureCtx(ctx)

	var f fileDomain.File
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, size_bytes, checksum_sha256, storage_path, uploaded_by, created_at
		FROM files WHERE id = $1 AND tenant_id = $2
	`, fileID, tenantID).Scan(
		&f.ID, &f.TenantID, &f.Name, &f.SizeBytes, &f.ChecksumSHA256, &f.StoragePath, &f.UploadedBy, &f.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// List retourne tous les fichiers de la bibliothèque du tenant, triés par nom.
func (r *FileRepo) List(ctx context.Context, tenantID string) ([]*fileDomain.File, error) {
	ctx = ensureCtx(ctx)

	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, size_bytes, checksum_sha256, storage_path, uploaded_by, created_at
		FROM files WHERE tenant_id = $1 ORDER BY name
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := make([]*fileDomain.File, 0)
	for rows.Next() {
		var f fileDomain.File
		if err := rows.Scan(
			&f.ID, &f.TenantID, &f.Name, &f.SizeBytes, &f.ChecksumSHA256, &f.StoragePath, &f.UploadedBy, &f.CreatedAt,
		); err != nil {
			return nil, err
		}
		files = append(files, &f)
	}
	return files, rows.Err()
}

// Delete supprime les métadonnées d'un fichier (jamais le fichier sur
// disque — voir file.Repository.Delete).
func (r *FileRepo) Delete(ctx context.Context, tenantID, fileID string) error {
	ctx = ensureCtx(ctx)

	tag, err := r.pool.Exec(ctx, `DELETE FROM files WHERE id = $1 AND tenant_id = $2`, fileID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// CreateDownloadToken génère un token de téléchargement à usage unique.
func (r *FileRepo) CreateDownloadToken(ctx context.Context, fileID string, ttl time.Duration) (string, error) {
	ctx = ensureCtx(ctx)

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])

	_, err := r.pool.Exec(ctx, `
		INSERT INTO file_download_tokens (file_id, token_hash, expires_at)
		VALUES ($1, $2, NOW() + $3::interval)
	`, fileID, tokenHash, ttl.String())
	if err != nil {
		return "", err
	}
	return token, nil
}

// ConsumeDownloadToken valide et consomme un token de téléchargement. Une
// seule requête UPDATE ... WHERE ... RETURNING, atomique : deux
// téléchargements concurrents avec le même token ne peuvent pas tous deux
// réussir (le second UPDATE ne trouve plus de ligne à used_at IS NULL).
func (r *FileRepo) ConsumeDownloadToken(ctx context.Context, rawToken string) (*fileDomain.File, error) {
	ctx = ensureCtx(ctx)

	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])

	var fileID string
	err := r.pool.QueryRow(ctx, `
		UPDATE file_download_tokens SET used_at = NOW()
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > NOW()
		RETURNING file_id
	`, tokenHash).Scan(&fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var f fileDomain.File
	err = r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, size_bytes, checksum_sha256, storage_path, uploaded_by, created_at
		FROM files WHERE id = $1
	`, fileID).Scan(
		&f.ID, &f.TenantID, &f.Name, &f.SizeBytes, &f.ChecksumSHA256, &f.StoragePath, &f.UploadedBy, &f.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

var _ fileDomain.Repository = (*FileRepo)(nil)
