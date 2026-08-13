// Package user définit l'entité User et son interface de persistance.
// Cette couche ne connaît aucune dépendance externe (pas de DB, pas de HTTP).
package user

import (
	"context"
	"time"
)

// User représente un compte utilisateur au sein d'un tenant.
type User struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Email       string     `json:"email"`
	FullName    string     `json:"full_name"`
	IsActive    bool       `json:"is_active"`
	MFAEnabled  bool       `json:"mfa_enabled"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Roles       []Role     `json:"roles,omitempty"`
}

// Role est une référence minimale à un rôle assigné à un utilisateur — pas
// l'entité rôle complète (gestion des rôles : voir RoleHandler, lecture
// seule pour l'instant).
type Role struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Repository définit le contrat de persistance pour les utilisateurs.
// Implémenté dans internal/infrastructure/persistence/postgres/user_repo.go
type Repository interface {
	// FindByID retourne un utilisateur (avec ses rôles) appartenant au tenant donné.
	FindByID(ctx context.Context, tenantID, userID string) (*User, error)

	// FindByEmail retourne un utilisateur par email, pour détecter les doublons à la création.
	FindByEmail(ctx context.Context, tenantID, email string) (*User, error)

	// List retourne tous les utilisateurs d'un tenant (avec leurs rôles), triés par date de création décroissante.
	List(ctx context.Context, tenantID string) ([]*User, error)

	// Create insère un nouvel utilisateur. Remplit u.ID/IsActive/MFAEnabled/CreatedAt/UpdatedAt en retour.
	Create(ctx context.Context, u *User, passwordHash string) error

	// Update met à jour full_name/is_active d'un utilisateur existant (jamais l'email ni le mot de passe ici).
	Update(ctx context.Context, u *User) error

	// Delete supprime définitivement un utilisateur du tenant donné.
	Delete(ctx context.Context, tenantID, userID string) error

	// SetRoles remplace l'ensemble des rôles assignés à un utilisateur.
	// Seuls les role_ids appartenant au tenant donné sont pris en compte —
	// retourne le nombre effectivement assigné, pour que l'appelant détecte
	// un role_id invalide/étranger (assigned < len(roleIDs)).
	SetRoles(ctx context.Context, tenantID, userID string, roleIDs []string) (assigned int, err error)
}
