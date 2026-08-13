package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	userDomain "github.com/yourorg/leo-one/internal/domain/user"
)

// UserRepo implémente user.Repository via pgx/v5.
type UserRepo struct {
	pool *pgxpool.Pool
}

// NewUserRepo crée un UserRepo avec le pool de connexions fourni.
func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) scanRoles(ctx context.Context, userID string) ([]userDomain.Role, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ro.id, ro.name
		FROM user_roles ur
		JOIN roles ro ON ro.id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY ro.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]userDomain.Role, 0)
	for rows.Next() {
		var role userDomain.Role
		if err := rows.Scan(&role.ID, &role.Name); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// FindByID retourne un utilisateur (avec ses rôles) appartenant au tenant donné.
// Isolation multi-tenant garantie par la clause WHERE tenant_id.
func (r *UserRepo) FindByID(ctx context.Context, tenantID, userID string) (*userDomain.User, error) {
	var u userDomain.User
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, full_name, is_active, mfa_enabled, last_login_at, created_at, updated_at
		FROM users WHERE id = $1 AND tenant_id = $2
	`, userID, tenantID).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.IsActive, &u.MFAEnabled,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	roles, err := r.scanRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Roles = roles

	return &u, nil
}

// FindByEmail retourne un utilisateur par email, pour détecter les doublons à la création.
func (r *UserRepo) FindByEmail(ctx context.Context, tenantID, email string) (*userDomain.User, error) {
	var u userDomain.User
	err := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, email, full_name, is_active, mfa_enabled, last_login_at, created_at, updated_at
		FROM users WHERE email = $1 AND tenant_id = $2
	`, email, tenantID).Scan(
		&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.IsActive, &u.MFAEnabled,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// List retourne tous les utilisateurs d'un tenant, avec leurs rôles chargés
// en une seule requête supplémentaire (évite le N+1 d'un aller-retour par
// utilisateur).
func (r *UserRepo) List(ctx context.Context, tenantID string) ([]*userDomain.User, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, email, full_name, is_active, mfa_enabled, last_login_at, created_at, updated_at
		FROM users WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*userDomain.User, 0)
	byID := make(map[string]*userDomain.User)
	for rows.Next() {
		var u userDomain.User
		if err := rows.Scan(
			&u.ID, &u.TenantID, &u.Email, &u.FullName, &u.IsActive, &u.MFAEnabled,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		users = append(users, &u)
		byID[u.ID] = &u
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return users, nil
	}

	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}

	roleRows, err := r.pool.Query(ctx, `
		SELECT ur.user_id, ro.id, ro.name
		FROM user_roles ur
		JOIN roles ro ON ro.id = ur.role_id
		WHERE ur.user_id = ANY($1)
		ORDER BY ro.name
	`, ids)
	if err != nil {
		return nil, err
	}
	defer roleRows.Close()

	for roleRows.Next() {
		var userID string
		var role userDomain.Role
		if err := roleRows.Scan(&userID, &role.ID, &role.Name); err != nil {
			return nil, err
		}
		if u, ok := byID[userID]; ok {
			u.Roles = append(u.Roles, role)
		}
	}
	if err := roleRows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

// Create insère un nouvel utilisateur. Remplit u.ID/IsActive/MFAEnabled/
// CreatedAt/UpdatedAt en retour.
func (r *UserRepo) Create(ctx context.Context, u *userDomain.User, passwordHash string) error {
	return r.pool.QueryRow(ctx, `
		INSERT INTO users (tenant_id, email, password_hash, full_name)
		VALUES ($1, $2, $3, $4)
		RETURNING id, is_active, mfa_enabled, created_at, updated_at
	`, u.TenantID, u.Email, passwordHash, u.FullName).Scan(
		&u.ID, &u.IsActive, &u.MFAEnabled, &u.CreatedAt, &u.UpdatedAt,
	)
}

// Update met à jour full_name/is_active d'un utilisateur existant.
func (r *UserRepo) Update(ctx context.Context, u *userDomain.User) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE users SET full_name = $1, is_active = $2, updated_at = NOW()
		WHERE id = $3 AND tenant_id = $4
	`, u.FullName, u.IsActive, u.ID, u.TenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// Delete supprime définitivement un utilisateur du tenant donné. Les tables
// qui référencent users(id) (alertes, tokens d'enrollment, ...)
// utilisent ON DELETE SET NULL — l'historique/l'audit trail est préservé,
// seule l'attribution nominative disparaît. user_roles cascade normalement.
func (r *UserRepo) Delete(ctx context.Context, tenantID, userID string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND tenant_id = $2`, userID, tenantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// SetRoles remplace l'ensemble des rôles assignés à un utilisateur, dans une
// transaction. Ne (ré)insère que les role_ids qui appartiennent au tenant
// donné (jointure implicite dans le INSERT ... SELECT) — le nombre de lignes
// effectivement insérées permet à l'appelant de détecter un role_id
// invalide ou appartenant à un autre tenant.
func (r *UserRepo) SetRoles(ctx context.Context, tenantID, userID string, roleIDs []string) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback après commit réussi est un no-op sans danger

	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return 0, err
	}

	assigned := 0
	if len(roleIDs) > 0 {
		tag, err := tx.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1, r.id FROM roles r WHERE r.id = ANY($2) AND r.tenant_id = $3
		`, userID, roleIDs, tenantID)
		if err != nil {
			return 0, err
		}
		assigned = int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return assigned, nil
}

var _ userDomain.Repository = (*UserRepo)(nil)
