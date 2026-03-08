package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// UserRepository handles user CRUD operations
type UserRepository struct {
	db DBTX
}

func NewUserRepository(db DBTX) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *types.User) error {
	user.ID = uuid.New()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	query := `
		INSERT INTO users (id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		user.ID, user.Email, user.PasswordHash, user.Name, user.Role,
		user.OIDCSubject, user.OIDCIssuer, user.Active, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*types.User, error) {
	user := &types.User{}
	query := `
		SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at
		FROM users WHERE email = $1
	`

	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role,
		&user.OIDCSubject, &user.OIDCIssuer, &user.Active, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetByOIDCIdentity retrieves a user by their OIDC issuer and subject
func (r *UserRepository) GetByOIDCIdentity(ctx context.Context, issuer string, subject string) (*types.User, error) {
	user := &types.User{}
	query := `
		SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at
		FROM users WHERE oidc_issuer = $1 AND oidc_subject = $2
	`

	err := r.db.QueryRowContext(ctx, query, issuer, subject).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role,
		&user.OIDCSubject, &user.OIDCIssuer, &user.Active, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.User, error) {
	user := &types.User{}
	query := `
		SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at
		FROM users WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role,
		&user.OIDCSubject, &user.OIDCIssuer, &user.Active, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
	)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (r *UserRepository) Update(ctx context.Context, user *types.User) error {
	user.UpdatedAt = time.Now()

	query := `
		UPDATE users
		SET email = $1, password_hash = $2, name = $3, role = $4, oidc_subject = $5, oidc_issuer = $6, active = $7, updated_at = $8, last_login_at = $9
		WHERE id = $10
	`
	_, err := r.db.ExecContext(ctx, query,
		user.Email, user.PasswordHash, user.Name, user.Role,
		user.OIDCSubject, user.OIDCIssuer, user.Active, user.UpdatedAt, user.LastLoginAt, user.ID,
	)
	return err
}

func (r *UserRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE users SET last_login_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *UserRepository) List(ctx context.Context) ([]*types.User, error) {
	query := `
		SELECT id, email, password_hash, name, role, oidc_subject, oidc_issuer, active, created_at, updated_at, last_login_at
		FROM users ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []*types.User
	for rows.Next() {
		user := &types.User{}
		err := rows.Scan(
			&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role,
			&user.OIDCSubject, &user.OIDCIssuer, &user.Active, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}
