package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

// AdminActingSession represents a master-admin "acting as <tenant>" session.
// While a row exists with ended_at IS NULL and expires_at in the future, every
// authed request from that admin is filtered to the tenant_team_id team. See
// claudedocs/master-admin-tenant-switching.md.
type AdminActingSession struct {
	ID           uuid.UUID
	AdminUserID  uuid.UUID
	TenantTeamID uuid.UUID
	StartedAt    time.Time
	ExpiresAt    time.Time
	EndedAt      *time.Time
	Reason       *string
	ClientIP     *string
	UserAgent    *string
}

// AdminActingSessionRepository owns CRUD for the admin_acting_sessions table.
type AdminActingSessionRepository struct {
	db DBTX
}

func NewAdminActingSessionRepository(db DBTX) *AdminActingSessionRepository {
	return &AdminActingSessionRepository{db: db}
}

// ErrNoActiveActingSession is returned by GetActive when the admin has no
// open session. Callers should treat this as "not currently acting as
// anyone", not as a hard error.
var ErrNoActiveActingSession = errors.New("no active acting session")

// Start creates a new acting session and ends any prior open session for the
// same admin so we never have two rows with ended_at IS NULL for one admin.
func (r *AdminActingSessionRepository) Start(
	ctx context.Context,
	adminUserID, tenantTeamID uuid.UUID,
	expiresAt time.Time,
	reason, clientIP, userAgent string,
) (*AdminActingSession, error) {
	// Close any prior open session — keeps the partial index single-row.
	if _, err := r.db.ExecContext(ctx, `
		UPDATE admin_acting_sessions
		   SET ended_at = now()
		 WHERE admin_user_id = $1 AND ended_at IS NULL
	`, adminUserID); err != nil {
		return nil, err
	}

	id := uuid.New()
	now := time.Now()
	row := &AdminActingSession{
		ID:           id,
		AdminUserID:  adminUserID,
		TenantTeamID: tenantTeamID,
		StartedAt:    now,
		ExpiresAt:    expiresAt,
	}
	if reason != "" {
		row.Reason = &reason
	}
	if clientIP != "" {
		row.ClientIP = &clientIP
	}
	if userAgent != "" {
		row.UserAgent = &userAgent
	}

	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO admin_acting_sessions
		    (id, admin_user_id, tenant_team_id, started_at, expires_at, reason, client_ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		row.ID, row.AdminUserID, row.TenantTeamID, row.StartedAt, row.ExpiresAt,
		row.Reason, normalizeInet(deref(row.ClientIP)), row.UserAgent,
	); err != nil {
		return nil, err
	}
	return row, nil
}

// GetActive returns the admin's open session if one exists and hasn't expired.
// Stale rows (expires_at < now()) are auto-closed and ErrNoActiveActingSession
// is returned so callers can clear the cookie next response.
func (r *AdminActingSessionRepository) GetActive(
	ctx context.Context,
	adminUserID uuid.UUID,
) (*AdminActingSession, error) {
	row := &AdminActingSession{}
	var ipText sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, admin_user_id, tenant_team_id, started_at, expires_at, ended_at,
		       reason, client_ip::text, user_agent
		  FROM admin_acting_sessions
		 WHERE admin_user_id = $1
		   AND ended_at IS NULL
		 ORDER BY started_at DESC
		 LIMIT 1
	`, adminUserID).Scan(
		&row.ID, &row.AdminUserID, &row.TenantTeamID, &row.StartedAt, &row.ExpiresAt,
		&row.EndedAt, &row.Reason, &ipText, &row.UserAgent,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoActiveActingSession
	}
	if err != nil {
		return nil, err
	}
	if ipText.Valid {
		v := ipText.String
		row.ClientIP = &v
	}

	if time.Now().After(row.ExpiresAt) {
		// Expired — close it lazily so the partial index stays cheap, then
		// surface "no active session" to the caller.
		_, _ = r.db.ExecContext(ctx, `
			UPDATE admin_acting_sessions SET ended_at = now() WHERE id = $1
		`, row.ID)
		return nil, ErrNoActiveActingSession
	}
	return row, nil
}

// EndAll closes every open session for the admin. Used by POST .../exit and
// by a forced logout. Returns the number of rows closed (0 is fine).
func (r *AdminActingSessionRepository) EndAll(ctx context.Context, adminUserID uuid.UUID) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE admin_acting_sessions
		   SET ended_at = now()
		 WHERE admin_user_id = $1 AND ended_at IS NULL
	`, adminUserID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
