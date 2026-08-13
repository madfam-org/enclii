package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// APITokenRepository handles API token CRUD operations
type APITokenRepository struct {
	db DBTX
}

// NewAPITokenRepository creates a new API token repository
func NewAPITokenRepository(db DBTX) *APITokenRepository {
	return &APITokenRepository{db: db}
}

// NewAPITokenRepositoryWithTx creates a repository using a transaction
func NewAPITokenRepositoryWithTx(tx DBTX) *APITokenRepository {
	return &APITokenRepository{db: tx}
}

// generateAPIToken creates a cryptographically secure random API token
// Returns: raw token (for user), prefix (for display), hash (for storage)
func generateAPIToken() (rawToken, prefix, hash string, err error) {
	// Generate 32 bytes of random data
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random token: %w", err)
	}

	// Create the full token with prefix
	rawToken = "enclii_" + hex.EncodeToString(tokenBytes)

	// Extract prefix for display (first 16 chars including "enclii_")
	prefix = rawToken[:16]

	// Hash the full token for storage
	hashBytes := sha256.Sum256([]byte(rawToken))
	hash = hex.EncodeToString(hashBytes[:])

	return rawToken, prefix, hash, nil
}

// hashAPIToken computes SHA-256 hash of a token for lookup
func hashAPIToken(token string) string {
	hashBytes := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hashBytes[:])
}

// Create generates a new API token for a user
// Returns the raw token (only shown once!) and the token metadata
func (r *APITokenRepository) Create(ctx context.Context, userID uuid.UUID, name string, scopes []string, expiresAt *time.Time) (*types.APITokenCreateResponse, error) {
	rawToken, prefix, hash, err := generateAPIToken()
	if err != nil {
		return nil, err
	}

	id := uuid.New()
	now := time.Now()

	query := `
		INSERT INTO api_tokens (id, user_id, name, prefix, token_hash, scopes, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING id
	`

	err = r.db.QueryRowContext(ctx, query,
		id, userID, name, prefix, hash, pq.Array(scopes), expiresAt, now,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("failed to create API token: %w", err)
	}

	response := &types.APITokenCreateResponse{
		Token:  rawToken,
		ID:     id,
		Name:   name,
		Prefix: prefix,
	}

	if expiresAt != nil {
		expStr := expiresAt.Format(time.RFC3339)
		response.ExpireAt = &expStr
	}

	return response, nil
}

// GetByID retrieves a token by its ID (does not include the hash)
func (r *APITokenRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.APIToken, error) {
	token := &types.APIToken{}
	query := `
		SELECT id, user_id, name, prefix, scopes, expires_at, last_used_at, last_used_ip,
		       revoked, revoked_at, created_at, updated_at
		FROM api_tokens
		WHERE id = $1
	`

	var scopes pq.StringArray
	var lastUsedIP sql.NullString
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&token.ID, &token.UserID, &token.Name, &token.Prefix, &scopes,
		&token.ExpiresAt, &token.LastUsedAt, &lastUsedIP,
		&token.Revoked, &token.RevokedAt, &token.CreatedAt, &token.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	token.Scopes = []string(scopes)
	if lastUsedIP.Valid {
		token.LastUsedIP = lastUsedIP.String
	}
	return token, nil
}

// GetByHash retrieves a token by its hash (used for authentication)
func (r *APITokenRepository) GetByHash(ctx context.Context, tokenHash string) (*types.APIToken, error) {
	token := &types.APIToken{}
	query := `
		SELECT id, user_id, name, prefix, token_hash, scopes, expires_at, last_used_at, last_used_ip,
		       revoked, revoked_at, created_at, updated_at
		FROM api_tokens
		WHERE token_hash = $1 AND revoked = false
	`

	var scopes pq.StringArray
	var lastUsedIP sql.NullString
	err := r.db.QueryRowContext(ctx, query, tokenHash).Scan(
		&token.ID, &token.UserID, &token.Name, &token.Prefix, &token.TokenHash, &scopes,
		&token.ExpiresAt, &token.LastUsedAt, &lastUsedIP,
		&token.Revoked, &token.RevokedAt, &token.CreatedAt, &token.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	token.Scopes = []string(scopes)
	if lastUsedIP.Valid {
		token.LastUsedIP = lastUsedIP.String
	}
	return token, nil
}

// ValidateToken checks if a raw token is valid and returns the associated token record
func (r *APITokenRepository) ValidateToken(ctx context.Context, rawToken string) (*types.APIToken, error) {
	hash := hashAPIToken(rawToken)
	token, err := r.GetByHash(ctx, hash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid token")
		}
		return nil, err
	}

	// Check expiration
	if token.ExpiresAt != nil && token.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("token expired")
	}

	return token, nil
}

// ValidateTokenForAuth checks if a raw token is valid and returns minimal info for auth
// This is used by the auth package to avoid type dependencies
func (r *APITokenRepository) ValidateTokenForAuth(ctx context.Context, rawToken string) (*APITokenInfo, error) {
	token, err := r.ValidateToken(ctx, rawToken)
	if err != nil {
		return nil, err
	}

	return &APITokenInfo{
		ID:     token.ID,
		UserID: token.UserID,
		Name:   token.Name,
		Scopes: token.Scopes,
	}, nil
}

// ListByUser retrieves all tokens for a user (active and revoked)
func (r *APITokenRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*types.APIToken, error) {
	query := `
		SELECT id, user_id, name, prefix, scopes, expires_at, last_used_at, last_used_ip,
		       revoked, revoked_at, created_at, updated_at
		FROM api_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tokens []*types.APIToken
	for rows.Next() {
		token := &types.APIToken{}
		var scopes pq.StringArray
		var lastUsedIP sql.NullString
		err := rows.Scan(
			&token.ID, &token.UserID, &token.Name, &token.Prefix, &scopes,
			&token.ExpiresAt, &token.LastUsedAt, &lastUsedIP,
			&token.Revoked, &token.RevokedAt, &token.CreatedAt, &token.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		token.Scopes = []string(scopes)
		if lastUsedIP.Valid {
			token.LastUsedIP = lastUsedIP.String
		}
		tokens = append(tokens, token)
	}

	return tokens, rows.Err()
}

// ListActiveByUser retrieves only active (non-revoked, non-expired) tokens
func (r *APITokenRepository) ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]*types.APIToken, error) {
	query := `
		SELECT id, user_id, name, prefix, scopes, expires_at, last_used_at, last_used_ip,
		       revoked, revoked_at, created_at, updated_at
		FROM api_tokens
		WHERE user_id = $1
		  AND revoked = false
		  AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tokens []*types.APIToken
	for rows.Next() {
		token := &types.APIToken{}
		var scopes pq.StringArray
		var lastUsedIP sql.NullString
		err := rows.Scan(
			&token.ID, &token.UserID, &token.Name, &token.Prefix, &scopes,
			&token.ExpiresAt, &token.LastUsedAt, &lastUsedIP,
			&token.Revoked, &token.RevokedAt, &token.CreatedAt, &token.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		token.Scopes = []string(scopes)
		if lastUsedIP.Valid {
			token.LastUsedIP = lastUsedIP.String
		}
		tokens = append(tokens, token)
	}

	return tokens, rows.Err()
}

// Revoke marks a token as revoked
func (r *APITokenRepository) Revoke(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	now := time.Now()
	query := `
		UPDATE api_tokens
		SET revoked = true, revoked_at = $1, updated_at = $1
		WHERE id = $2 AND user_id = $3 AND revoked = false
	`

	result, err := r.db.ExecContext(ctx, query, now, id, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("token not found or already revoked")
	}

	return nil
}

// UpdateLastUsed updates the last used timestamp and IP for a token
func (r *APITokenRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID, ip string) error {
	now := time.Now()
	query := `
		UPDATE api_tokens
		SET last_used_at = $1, last_used_ip = $2, updated_at = $1
		WHERE id = $3
	`

	_, err := r.db.ExecContext(ctx, query, now, ip, id)
	return err
}

// Delete permanently removes a token (use Revoke for soft delete)
func (r *APITokenRepository) Delete(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM api_tokens WHERE id = $1 AND user_id = $2`
	result, err := r.db.ExecContext(ctx, query, id, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("token not found")
	}

	return nil
}

// CountByUser returns the number of tokens for a user
// CountByUser counts a user's tokens that are still USABLE — not revoked and
// not expired.
//
// It used to count `revoked = false` alone, which made the 10-token cap
// permanent rather than a limit on live credentials: an expired token cannot
// authenticate anything, but it still occupied a slot forever. A user who
// created ten 90-day tokens was locked out of token creation for good once they
// aged out, and the error told them to "revoke unused tokens" while the CLI's
// own `tokens list` was broken (it decoded the array response as an object), so
// there was no supported way to discover what to revoke.
//
// This matters more, not less, as expiries get used: bounded lifetimes are the
// safe practice, and under the old query every expiry was a slot leaked on a
// timer.
//
// Expiry is evaluated in the database rather than in Go so that the cap check
// and any concurrent authentication agree on "now" — comparing against an
// application clock can admit a token the auth path has already rejected.
func (r *APITokenRepository) CountByUser(ctx context.Context, userID uuid.UUID) (int, error) {
	var count int
	query := `
		SELECT COUNT(*) FROM api_tokens
		WHERE user_id = $1
		  AND revoked = false
		  AND (expires_at IS NULL OR expires_at > NOW())`
	err := r.db.QueryRowContext(ctx, query, userID).Scan(&count)
	return count, err
}
