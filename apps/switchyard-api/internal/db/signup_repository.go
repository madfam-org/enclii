// Package db — signup repository for P3.2 Sprint 1.
//
// Persists the signup_requests + signup_events tables that back the
// self-serve signup flow. The business logic for state transitions lives
// in internal/signup; this layer only handles CRUD + event append.
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Signup status values must match the CHECK constraint in
// migrations/017_signup_flow.up.sql.
const (
	SignupStatusPendingVerification = "pending_verification"
	SignupStatusVerified            = "verified"
	SignupStatusGithubLinked        = "github_linked"
	SignupStatusProvisioning        = "provisioning"
	SignupStatusReady               = "ready"
	SignupStatusFailed              = "failed"
)

// SignupRequest mirrors a row in public.signup_requests.
type SignupRequest struct {
	ID                         uuid.UUID
	Email                      string
	CompanyName                *string
	JanuaUserSub               *string
	VerificationTokenHash      *string
	VerificationTokenExpiresAt *time.Time
	GithubUsername             *string
	GithubAccessTokenSecretRef *string
	OAuthStateHash             *string
	Status                     string
	ProvisionedProjectID       *uuid.UUID
	ErrorMessage               *string
	EmailVerifiedAt            *time.Time
	OAuthCompletedAt           *time.Time
	ProvisionedAt              *time.Time
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// SignupEvent is an append-only audit row in public.signup_events.
type SignupEvent struct {
	ID              uuid.UUID
	SignupRequestID uuid.UUID
	EventType       string
	Details         map[string]any
	CreatedAt       time.Time
}

// SignupRepository handles CRUD for signup flows.
type SignupRepository struct {
	db DBTX
}

// NewSignupRepository creates a new repository.
func NewSignupRepository(db DBTX) *SignupRepository {
	return &SignupRepository{db: db}
}

// NewSignupRepositoryWithTx creates a repository using a transaction.
func NewSignupRepositoryWithTx(tx DBTX) *SignupRepository {
	return &SignupRepository{db: tx}
}

// Create inserts a new signup_request and emits the 'initiated' event in
// the same transaction-capable call path. Caller typically wraps this in
// WithTransaction to keep the event and the row consistent.
func (r *SignupRepository) Create(ctx context.Context, req *SignupRequest) error {
	if req.ID == uuid.Nil {
		req.ID = uuid.New()
	}
	now := time.Now().UTC()
	req.CreatedAt = now
	req.UpdatedAt = now
	if req.Status == "" {
		req.Status = SignupStatusPendingVerification
	}

	query := `
		INSERT INTO signup_requests (
			id, email, company_name,
			verification_token_hash, verification_token_expires_at,
			status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
	`
	_, err := r.db.ExecContext(ctx, query,
		req.ID,
		req.Email,
		req.CompanyName,
		req.VerificationTokenHash,
		req.VerificationTokenExpiresAt,
		req.Status,
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to create signup_request: %w", err)
	}
	return nil
}

// GetByID fetches a signup request by its UUID. Returns sql.ErrNoRows if
// not found.
func (r *SignupRepository) GetByID(ctx context.Context, id uuid.UUID) (*SignupRequest, error) {
	return r.scanOne(ctx, `
		SELECT id, email, company_name,
		       janua_user_sub, verification_token_hash, verification_token_expires_at,
		       github_username, github_access_token_secret_ref, oauth_state_hash,
		       status, provisioned_project_id, error_message,
		       email_verified_at, oauth_completed_at, provisioned_at,
		       created_at, updated_at
		FROM signup_requests
		WHERE id = $1
	`, id)
}

// GetActiveByEmail returns the most recent signup for `email` that is in a
// non-terminal state (so /signup can short-circuit to "resume where you
// left off" without creating a new row).
func (r *SignupRepository) GetActiveByEmail(ctx context.Context, email string) (*SignupRequest, error) {
	return r.scanOne(ctx, `
		SELECT id, email, company_name,
		       janua_user_sub, verification_token_hash, verification_token_expires_at,
		       github_username, github_access_token_secret_ref, oauth_state_hash,
		       status, provisioned_project_id, error_message,
		       email_verified_at, oauth_completed_at, provisioned_at,
		       created_at, updated_at
		FROM signup_requests
		WHERE email = $1
		  AND status IN ('pending_verification','verified','github_linked','provisioning')
		ORDER BY created_at DESC
		LIMIT 1
	`, email)
}

// GetByVerificationToken looks up a signup by the sha256 hash of its
// verification token. Callers should always pass the HASH, not the raw
// token, to avoid timing leaks through query logs.
func (r *SignupRepository) GetByVerificationToken(ctx context.Context, tokenHash string) (*SignupRequest, error) {
	return r.scanOne(ctx, `
		SELECT id, email, company_name,
		       janua_user_sub, verification_token_hash, verification_token_expires_at,
		       github_username, github_access_token_secret_ref, oauth_state_hash,
		       status, provisioned_project_id, error_message,
		       email_verified_at, oauth_completed_at, provisioned_at,
		       created_at, updated_at
		FROM signup_requests
		WHERE verification_token_hash = $1
	`, tokenHash)
}

// GetByOAuthState looks up a signup by the sha256 hash of its OAuth state
// nonce. Used during the GitHub OAuth callback to tie the request back to
// the signup row that started it.
func (r *SignupRepository) GetByOAuthState(ctx context.Context, stateHash string) (*SignupRequest, error) {
	return r.scanOne(ctx, `
		SELECT id, email, company_name,
		       janua_user_sub, verification_token_hash, verification_token_expires_at,
		       github_username, github_access_token_secret_ref, oauth_state_hash,
		       status, provisioned_project_id, error_message,
		       email_verified_at, oauth_completed_at, provisioned_at,
		       created_at, updated_at
		FROM signup_requests
		WHERE oauth_state_hash = $1
	`, stateHash)
}

// UpdateVerificationToken rotates the verification token for an in-flight
// pending_verification signup. Used by the resume path when a user hits
// POST /signup twice for the same email — we invalidate the first link
// by overwriting it. No-op if the signup has already progressed.
func (r *SignupRepository) UpdateVerificationToken(ctx context.Context, id uuid.UUID, hash string, exp time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET verification_token_hash = $1,
		    verification_token_expires_at = $2,
		    updated_at = $3
		WHERE id = $4 AND status = $5
	`, hash, exp, time.Now().UTC(), id, SignupStatusPendingVerification)
	if err != nil {
		return fmt.Errorf("failed to update verification token: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signup %s not in pending_verification state", id)
	}
	return nil
}

// MarkEmailVerified transitions pending_verification -> verified and
// stamps email_verified_at.
func (r *SignupRepository) MarkEmailVerified(ctx context.Context, id uuid.UUID, januaUserSub string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET status = $1,
		    email_verified_at = $2,
		    janua_user_sub = COALESCE(janua_user_sub, $3),
		    updated_at = $2
		WHERE id = $4 AND status = $5
	`, SignupStatusVerified, now, januaUserSub, id, SignupStatusPendingVerification)
	if err != nil {
		return fmt.Errorf("failed to mark email verified: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signup %s not in pending_verification state", id)
	}
	return nil
}

// SetOAuthState stores the sha256 hash of the state nonce for the GitHub
// OAuth handshake. Called right before we redirect the user to GitHub.
func (r *SignupRepository) SetOAuthState(ctx context.Context, id uuid.UUID, stateHash string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET oauth_state_hash = $1, updated_at = $2
		WHERE id = $3
	`, stateHash, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to set oauth state: %w", err)
	}
	return nil
}

// MarkGithubLinked transitions verified -> github_linked and stores the
// GitHub username + secret ref (never the raw token).
func (r *SignupRepository) MarkGithubLinked(ctx context.Context, id uuid.UUID, githubUsername, secretRef string) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET status = $1,
		    github_username = $2,
		    github_access_token_secret_ref = $3,
		    oauth_completed_at = $4,
		    updated_at = $4
		WHERE id = $5 AND status = $6
	`, SignupStatusGithubLinked, githubUsername, secretRef, now, id, SignupStatusVerified)
	if err != nil {
		return fmt.Errorf("failed to mark github linked: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signup %s not in verified state", id)
	}
	return nil
}

// MarkProvisioning flips github_linked -> provisioning. We split this from
// MarkReady so that parallel callers can't double-provision.
func (r *SignupRepository) MarkProvisioning(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status = $4
	`, SignupStatusProvisioning, time.Now().UTC(), id, SignupStatusGithubLinked)
	if err != nil {
		return fmt.Errorf("failed to mark provisioning: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signup %s not in github_linked state", id)
	}
	return nil
}

// MarkReady completes the flow: provisioning -> ready, linking the
// provisioned project.
func (r *SignupRepository) MarkReady(ctx context.Context, id, projectID uuid.UUID) error {
	now := time.Now().UTC()
	res, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET status = $1,
		    provisioned_project_id = $2,
		    provisioned_at = $3,
		    updated_at = $3
		WHERE id = $4 AND status = $5
	`, SignupStatusReady, projectID, now, id, SignupStatusProvisioning)
	if err != nil {
		return fmt.Errorf("failed to mark ready: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signup %s not in provisioning state", id)
	}
	return nil
}

// MarkFailed drops the signup into a terminal failure state with an
// error message. Callable from any non-terminal state.
func (r *SignupRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE signup_requests
		SET status = $1, error_message = $2, updated_at = $3
		WHERE id = $4 AND status NOT IN ($5, $1)
	`, SignupStatusFailed, errMsg, time.Now().UTC(), id, SignupStatusReady)
	if err != nil {
		return fmt.Errorf("failed to mark failed: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("signup %s already in terminal state", id)
	}
	return nil
}

// AppendEvent inserts an append-only audit row. Never errors out the
// transaction — callers best-effort log and continue, since a dropped
// event is strictly less bad than a dropped signup.
func (r *SignupRepository) AppendEvent(ctx context.Context, signupID uuid.UUID, eventType string, details map[string]any) error {
	var raw []byte
	if details == nil {
		raw = []byte("{}")
	} else {
		var err error
		raw, err = json.Marshal(details)
		if err != nil {
			return fmt.Errorf("failed to marshal event details: %w", err)
		}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO signup_events (id, signup_request_id, event_type, details, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5)
	`, uuid.New(), signupID, eventType, string(raw), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to append signup event: %w", err)
	}
	return nil
}

// ListEvents returns the append-only event timeline for a signup, oldest
// first.
func (r *SignupRepository) ListEvents(ctx context.Context, signupID uuid.UUID) ([]*SignupEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, signup_request_id, event_type, details, created_at
		FROM signup_events
		WHERE signup_request_id = $1
		ORDER BY created_at ASC
	`, signupID)
	if err != nil {
		return nil, fmt.Errorf("failed to list signup events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*SignupEvent
	for rows.Next() {
		e := &SignupEvent{}
		var rawDetails []byte
		if err := rows.Scan(&e.ID, &e.SignupRequestID, &e.EventType, &rawDetails, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan signup event: %w", err)
		}
		if len(rawDetails) > 0 {
			_ = json.Unmarshal(rawDetails, &e.Details)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// scanOne is a shared row-scanner for the three by-lookup-key reads.
func (r *SignupRepository) scanOne(ctx context.Context, query string, args ...any) (*SignupRequest, error) {
	req := &SignupRequest{}

	var (
		companyName                sql.NullString
		januaUserSub               sql.NullString
		verificationTokenHash      sql.NullString
		verificationTokenExpiresAt sql.NullTime
		githubUsername             sql.NullString
		githubAccessTokenSecretRef sql.NullString
		oauthStateHash             sql.NullString
		provisionedProjectID       sql.NullString
		errorMessage               sql.NullString
		emailVerifiedAt            sql.NullTime
		oauthCompletedAt           sql.NullTime
		provisionedAt              sql.NullTime
	)

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&req.ID,
		&req.Email,
		&companyName,
		&januaUserSub,
		&verificationTokenHash,
		&verificationTokenExpiresAt,
		&githubUsername,
		&githubAccessTokenSecretRef,
		&oauthStateHash,
		&req.Status,
		&provisionedProjectID,
		&errorMessage,
		&emailVerifiedAt,
		&oauthCompletedAt,
		&provisionedAt,
		&req.CreatedAt,
		&req.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if companyName.Valid {
		req.CompanyName = &companyName.String
	}
	if januaUserSub.Valid {
		req.JanuaUserSub = &januaUserSub.String
	}
	if verificationTokenHash.Valid {
		req.VerificationTokenHash = &verificationTokenHash.String
	}
	if verificationTokenExpiresAt.Valid {
		req.VerificationTokenExpiresAt = &verificationTokenExpiresAt.Time
	}
	if githubUsername.Valid {
		req.GithubUsername = &githubUsername.String
	}
	if githubAccessTokenSecretRef.Valid {
		req.GithubAccessTokenSecretRef = &githubAccessTokenSecretRef.String
	}
	if oauthStateHash.Valid {
		req.OAuthStateHash = &oauthStateHash.String
	}
	if provisionedProjectID.Valid {
		id, parseErr := uuid.Parse(provisionedProjectID.String)
		if parseErr == nil {
			req.ProvisionedProjectID = &id
		}
	}
	if errorMessage.Valid {
		req.ErrorMessage = &errorMessage.String
	}
	if emailVerifiedAt.Valid {
		req.EmailVerifiedAt = &emailVerifiedAt.Time
	}
	if oauthCompletedAt.Valid {
		req.OAuthCompletedAt = &oauthCompletedAt.Time
	}
	if provisionedAt.Valid {
		req.ProvisionedAt = &provisionedAt.Time
	}

	return req, nil
}
