package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Team represents a team/organization in the system
type Team struct {
	ID           uuid.UUID       `json:"id"`
	Name         string          `json:"name"`
	Slug         string          `json:"slug"`
	Description  *string         `json:"description,omitempty"`
	AvatarURL    *string         `json:"avatar_url,omitempty"`
	BillingEmail *string         `json:"billing_email,omitempty"`
	OwnerID      *uuid.UUID      `json:"owner_id,omitempty"`
	Settings     json.RawMessage `json:"settings,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// TeamMember represents a user's membership in a team
type TeamMember struct {
	ID         uuid.UUID  `json:"id"`
	TeamID     uuid.UUID  `json:"team_id"`
	UserID     uuid.UUID  `json:"user_id"`
	Role       string     `json:"role"` // 'owner', 'admin', 'member', 'viewer'
	InvitedBy  *uuid.UUID `json:"invited_by,omitempty"`
	JoinedAt   time.Time  `json:"joined_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
}

// TeamMemberWithUser includes user details in the member record
type TeamMemberWithUser struct {
	TeamMember
	UserEmail string  `json:"user_email"`
	UserName  *string `json:"user_name,omitempty"`
}

// TeamInvitation represents a pending invitation to join a team
type TeamInvitation struct {
	ID         uuid.UUID  `json:"id"`
	TeamID     uuid.UUID  `json:"team_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"` // 'admin', 'member', 'viewer'
	InvitedBy  uuid.UUID  `json:"invited_by"`
	Token      string     `json:"token"`
	Status     string     `json:"status"` // 'pending', 'accepted', 'declined', 'expired'
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	DeclinedAt *time.Time `json:"declined_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// TeamInvitationWithDetails includes team and inviter info
type TeamInvitationWithDetails struct {
	TeamInvitation
	TeamName    string  `json:"team_name"`
	TeamSlug    string  `json:"team_slug"`
	InviterName *string `json:"inviter_name,omitempty"`
}

// TeamRepository handles team CRUD operations
type TeamRepository struct {
	db DBTX
}

func NewTeamRepository(db DBTX) *TeamRepository {
	return &TeamRepository{db: db}
}

// NewTeamRepositoryWithTx creates a repository using a transaction
func NewTeamRepositoryWithTx(tx DBTX) *TeamRepository {
	return &TeamRepository{db: tx}
}

// Create creates a new team
func (r *TeamRepository) Create(ctx context.Context, team *Team) error {
	team.ID = uuid.New()
	team.CreatedAt = time.Now()
	team.UpdatedAt = time.Now()

	if team.Settings == nil {
		team.Settings = json.RawMessage("{}")
	}

	query := `
		INSERT INTO teams (id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		team.ID, team.Name, team.Slug, team.Description, team.AvatarURL,
		team.BillingEmail, team.OwnerID, team.Settings, team.CreatedAt, team.UpdatedAt,
	)
	return err
}

// GetByID retrieves a team by ID
func (r *TeamRepository) GetByID(ctx context.Context, id uuid.UUID) (*Team, error) {
	team := &Team{}
	query := `
		SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at
		FROM teams WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&team.ID, &team.Name, &team.Slug, &team.Description, &team.AvatarURL,
		&team.BillingEmail, &team.OwnerID, &team.Settings, &team.CreatedAt, &team.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return team, nil
}

// GetBySlug retrieves a team by slug
func (r *TeamRepository) GetBySlug(ctx context.Context, slug string) (*Team, error) {
	team := &Team{}
	query := `
		SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at
		FROM teams WHERE slug = $1
	`

	err := r.db.QueryRowContext(ctx, query, slug).Scan(
		&team.ID, &team.Name, &team.Slug, &team.Description, &team.AvatarURL,
		&team.BillingEmail, &team.OwnerID, &team.Settings, &team.CreatedAt, &team.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return team, nil
}

// Update updates an existing team
func (r *TeamRepository) Update(ctx context.Context, team *Team) error {
	team.UpdatedAt = time.Now()

	query := `
		UPDATE teams
		SET name = $1, slug = $2, description = $3, avatar_url = $4, billing_email = $5,
		    owner_id = $6, settings = $7, updated_at = $8
		WHERE id = $9
	`
	result, err := r.db.ExecContext(ctx, query,
		team.Name, team.Slug, team.Description, team.AvatarURL, team.BillingEmail,
		team.OwnerID, team.Settings, team.UpdatedAt, team.ID,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete removes a team by ID
func (r *TeamRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM teams WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// ListByUser returns all teams a user is a member of
func (r *TeamRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]*Team, error) {
	query := `
		SELECT t.id, t.name, t.slug, t.description, t.avatar_url, t.billing_email, t.owner_id, t.settings, t.created_at, t.updated_at
		FROM teams t
		JOIN team_members tm ON t.id = tm.team_id
		WHERE tm.user_id = $1
		ORDER BY t.name ASC
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var teams []*Team
	for rows.Next() {
		team := &Team{}
		err := rows.Scan(
			&team.ID, &team.Name, &team.Slug, &team.Description, &team.AvatarURL,
			&team.BillingEmail, &team.OwnerID, &team.Settings, &team.CreatedAt, &team.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}

	return teams, nil
}

// TeamMemberRepository handles team membership operations
type TeamMemberRepository struct {
	db DBTX
}

func NewTeamMemberRepository(db DBTX) *TeamMemberRepository {
	return &TeamMemberRepository{db: db}
}

// NewTeamMemberRepositoryWithTx creates a repository using a transaction
func NewTeamMemberRepositoryWithTx(tx DBTX) *TeamMemberRepository {
	return &TeamMemberRepository{db: tx}
}

// Add adds a user to a team
func (r *TeamMemberRepository) Add(ctx context.Context, member *TeamMember) error {
	member.ID = uuid.New()
	member.JoinedAt = time.Now()

	query := `
		INSERT INTO team_members (id, team_id, user_id, role, invited_by, joined_at, accepted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (team_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, invited_by = EXCLUDED.invited_by
	`
	_, err := r.db.ExecContext(ctx, query,
		member.ID, member.TeamID, member.UserID, member.Role, member.InvitedBy, member.JoinedAt, member.AcceptedAt,
	)
	return err
}

// Remove removes a user from a team
func (r *TeamMemberRepository) Remove(ctx context.Context, teamID, userID uuid.UUID) error {
	query := `DELETE FROM team_members WHERE team_id = $1 AND user_id = $2`
	result, err := r.db.ExecContext(ctx, query, teamID, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// UpdateRole updates a member's role in a team
func (r *TeamMemberRepository) UpdateRole(ctx context.Context, teamID, userID uuid.UUID, role string) error {
	query := `UPDATE team_members SET role = $1 WHERE team_id = $2 AND user_id = $3`
	result, err := r.db.ExecContext(ctx, query, role, teamID, userID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// GetMember retrieves a specific team member
func (r *TeamMemberRepository) GetMember(ctx context.Context, teamID, userID uuid.UUID) (*TeamMember, error) {
	member := &TeamMember{}
	query := `
		SELECT id, team_id, user_id, role, invited_by, joined_at, accepted_at
		FROM team_members WHERE team_id = $1 AND user_id = $2
	`

	err := r.db.QueryRowContext(ctx, query, teamID, userID).Scan(
		&member.ID, &member.TeamID, &member.UserID, &member.Role, &member.InvitedBy, &member.JoinedAt, &member.AcceptedAt,
	)
	if err != nil {
		return nil, err
	}

	return member, nil
}

// ListByTeam returns all members of a team with user details
func (r *TeamMemberRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]*TeamMemberWithUser, error) {
	query := `
		SELECT tm.id, tm.team_id, tm.user_id, tm.role, tm.invited_by, tm.joined_at, tm.accepted_at,
		       u.email, u.name
		FROM team_members tm
		JOIN users u ON tm.user_id = u.id
		WHERE tm.team_id = $1
		ORDER BY tm.joined_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var members []*TeamMemberWithUser
	for rows.Next() {
		member := &TeamMemberWithUser{}
		err := rows.Scan(
			&member.ID, &member.TeamID, &member.UserID, &member.Role, &member.InvitedBy,
			&member.JoinedAt, &member.AcceptedAt, &member.UserEmail, &member.UserName,
		)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}

	return members, nil
}

// GetUserRole returns the role of a user in a team (empty string if not a member)
func (r *TeamMemberRepository) GetUserRole(ctx context.Context, teamID, userID uuid.UUID) (string, error) {
	var role string
	query := `SELECT role FROM team_members WHERE team_id = $1 AND user_id = $2`
	err := r.db.QueryRowContext(ctx, query, teamID, userID).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return role, nil
}

// CountByTeam returns the number of members in a team
func (r *TeamMemberRepository) CountByTeam(ctx context.Context, teamID uuid.UUID) (int, error) {
	var count int
	query := `SELECT COUNT(*) FROM team_members WHERE team_id = $1`
	err := r.db.QueryRowContext(ctx, query, teamID).Scan(&count)
	return count, err
}

// TeamInvitationRepository handles team invitation operations
type TeamInvitationRepository struct {
	db DBTX
}

func NewTeamInvitationRepository(db DBTX) *TeamInvitationRepository {
	return &TeamInvitationRepository{db: db}
}

// NewTeamInvitationRepositoryWithTx creates a repository using a transaction
func NewTeamInvitationRepositoryWithTx(tx DBTX) *TeamInvitationRepository {
	return &TeamInvitationRepository{db: tx}
}

// generateToken creates a secure random token for invitation links
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Create creates a new invitation
func (r *TeamInvitationRepository) Create(ctx context.Context, invitation *TeamInvitation) error {
	invitation.ID = uuid.New()
	invitation.Status = "pending"
	invitation.CreatedAt = time.Now()
	invitation.UpdatedAt = time.Now()

	// Generate secure token if not provided
	if invitation.Token == "" {
		token, err := generateToken()
		if err != nil {
			return fmt.Errorf("failed to generate token: %w", err)
		}
		invitation.Token = token
	}

	// Set default expiration (7 days) if not provided
	if invitation.ExpiresAt.IsZero() {
		invitation.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	}

	query := `
		INSERT INTO team_invitations (id, team_id, email, role, invited_by, token, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		invitation.ID, invitation.TeamID, invitation.Email, invitation.Role, invitation.InvitedBy,
		invitation.Token, invitation.Status, invitation.ExpiresAt, invitation.CreatedAt, invitation.UpdatedAt,
	)
	return err
}

// GetByID retrieves an invitation by ID
func (r *TeamInvitationRepository) GetByID(ctx context.Context, id uuid.UUID) (*TeamInvitation, error) {
	invitation := &TeamInvitation{}
	query := `
		SELECT id, team_id, email, role, invited_by, token, status, expires_at, accepted_at, declined_at, created_at, updated_at
		FROM team_invitations WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&invitation.ID, &invitation.TeamID, &invitation.Email, &invitation.Role, &invitation.InvitedBy,
		&invitation.Token, &invitation.Status, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.DeclinedAt,
		&invitation.CreatedAt, &invitation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return invitation, nil
}

// GetByToken retrieves an invitation by its token
func (r *TeamInvitationRepository) GetByToken(ctx context.Context, token string) (*TeamInvitation, error) {
	invitation := &TeamInvitation{}
	query := `
		SELECT id, team_id, email, role, invited_by, token, status, expires_at, accepted_at, declined_at, created_at, updated_at
		FROM team_invitations WHERE token = $1
	`

	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&invitation.ID, &invitation.TeamID, &invitation.Email, &invitation.Role, &invitation.InvitedBy,
		&invitation.Token, &invitation.Status, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.DeclinedAt,
		&invitation.CreatedAt, &invitation.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return invitation, nil
}

// GetByTokenWithDetails retrieves an invitation with team and inviter details
func (r *TeamInvitationRepository) GetByTokenWithDetails(ctx context.Context, token string) (*TeamInvitationWithDetails, error) {
	invitation := &TeamInvitationWithDetails{}
	query := `
		SELECT ti.id, ti.team_id, ti.email, ti.role, ti.invited_by, ti.token, ti.status,
		       ti.expires_at, ti.accepted_at, ti.declined_at, ti.created_at, ti.updated_at,
		       t.name, t.slug, u.name
		FROM team_invitations ti
		JOIN teams t ON ti.team_id = t.id
		JOIN users u ON ti.invited_by = u.id
		WHERE ti.token = $1
	`

	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&invitation.ID, &invitation.TeamID, &invitation.Email, &invitation.Role, &invitation.InvitedBy,
		&invitation.Token, &invitation.Status, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.DeclinedAt,
		&invitation.CreatedAt, &invitation.UpdatedAt, &invitation.TeamName, &invitation.TeamSlug, &invitation.InviterName,
	)
	if err != nil {
		return nil, err
	}

	return invitation, nil
}

// ListPendingByTeam returns all pending invitations for a team
func (r *TeamInvitationRepository) ListPendingByTeam(ctx context.Context, teamID uuid.UUID) ([]*TeamInvitation, error) {
	query := `
		SELECT id, team_id, email, role, invited_by, token, status, expires_at, accepted_at, declined_at, created_at, updated_at
		FROM team_invitations
		WHERE team_id = $1 AND status = 'pending' AND expires_at > NOW()
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, teamID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var invitations []*TeamInvitation
	for rows.Next() {
		invitation := &TeamInvitation{}
		err := rows.Scan(
			&invitation.ID, &invitation.TeamID, &invitation.Email, &invitation.Role, &invitation.InvitedBy,
			&invitation.Token, &invitation.Status, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.DeclinedAt,
			&invitation.CreatedAt, &invitation.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}

	return invitations, nil
}

// ListPendingByEmail returns all pending invitations for an email address
func (r *TeamInvitationRepository) ListPendingByEmail(ctx context.Context, email string) ([]*TeamInvitationWithDetails, error) {
	query := `
		SELECT ti.id, ti.team_id, ti.email, ti.role, ti.invited_by, ti.token, ti.status,
		       ti.expires_at, ti.accepted_at, ti.declined_at, ti.created_at, ti.updated_at,
		       t.name, t.slug, u.name
		FROM team_invitations ti
		JOIN teams t ON ti.team_id = t.id
		JOIN users u ON ti.invited_by = u.id
		WHERE ti.email = $1 AND ti.status = 'pending' AND ti.expires_at > NOW()
		ORDER BY ti.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, email)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var invitations []*TeamInvitationWithDetails
	for rows.Next() {
		invitation := &TeamInvitationWithDetails{}
		err := rows.Scan(
			&invitation.ID, &invitation.TeamID, &invitation.Email, &invitation.Role, &invitation.InvitedBy,
			&invitation.Token, &invitation.Status, &invitation.ExpiresAt, &invitation.AcceptedAt, &invitation.DeclinedAt,
			&invitation.CreatedAt, &invitation.UpdatedAt, &invitation.TeamName, &invitation.TeamSlug, &invitation.InviterName,
		)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}

	return invitations, nil
}

// Accept marks an invitation as accepted
func (r *TeamInvitationRepository) Accept(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `UPDATE team_invitations SET status = 'accepted', accepted_at = $1, updated_at = $1 WHERE id = $2 AND status = 'pending'`
	result, err := r.db.ExecContext(ctx, query, now, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Decline marks an invitation as declined
func (r *TeamInvitationRepository) Decline(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	query := `UPDATE team_invitations SET status = 'declined', declined_at = $1, updated_at = $1 WHERE id = $2 AND status = 'pending'`
	result, err := r.db.ExecContext(ctx, query, now, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// Delete removes an invitation
func (r *TeamInvitationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM team_invitations WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// CleanupExpired marks expired invitations
func (r *TeamInvitationRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `UPDATE team_invitations SET status = 'expired', updated_at = NOW() WHERE status = 'pending' AND expires_at < NOW()`
	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// HasPendingInvitation checks if a pending invitation exists for an email in a team
func (r *TeamInvitationRepository) HasPendingInvitation(ctx context.Context, teamID uuid.UUID, email string) (bool, error) {
	var count int
	query := `SELECT COUNT(*) FROM team_invitations WHERE team_id = $1 AND email = $2 AND status = 'pending' AND expires_at > NOW()`
	err := r.db.QueryRowContext(ctx, query, teamID, email).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
