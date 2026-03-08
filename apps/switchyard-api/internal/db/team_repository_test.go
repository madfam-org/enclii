package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newTeamMockDB(t *testing.T) (*TeamRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewTeamRepository(db)
	return repo, mock, func() { db.Close() }
}

func newTeamMemberMockDB(t *testing.T) (*TeamMemberRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewTeamMemberRepository(db)
	return repo, mock, func() { db.Close() }
}

func newTeamInvitationMockDB(t *testing.T) (*TeamInvitationRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewTeamInvitationRepository(db)
	return repo, mock, func() { db.Close() }
}

var teamColumns = []string{
	"id", "name", "slug", "description", "avatar_url", "billing_email",
	"owner_id", "settings", "created_at", "updated_at",
}

// --- Team Create ---

func TestTeamRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		team := &Team{Name: "my-team", Slug: "my-team"}

		mock.ExpectExec(`INSERT INTO teams`).
			WithArgs(
				sqlmock.AnyArg(), "my-team", "my-team", sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), team)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, team.ID)
		assert.False(t, team.CreatedAt.IsZero())
		assert.Equal(t, json.RawMessage("{}"), team.Settings)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		team := &Team{Name: "dup", Slug: "dup"}
		mock.ExpectExec(`INSERT INTO teams`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("duplicate key"))

		err := repo.Create(context.Background(), team)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Team GetByID ---

func TestTeamRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(teamColumns).
				AddRow(id, "my-team", "my-team", nil, nil, nil, nil, json.RawMessage("{}"), now, now))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "my-team", result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, slug`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Team GetBySlug ---

func TestTeamRepository_GetBySlug(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		mock.ExpectQuery(`SELECT id, name, slug, description, avatar_url, billing_email, owner_id, settings, created_at, updated_at`).
			WithArgs("my-team").
			WillReturnRows(sqlmock.NewRows(teamColumns).
				AddRow(uuid.New(), "my-team", "my-team", nil, nil, nil, nil, json.RawMessage("{}"), now, now))

		result, err := repo.GetBySlug(context.Background(), "my-team")
		assert.NoError(t, err)
		assert.Equal(t, "my-team", result.Slug)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Team Update ---

func TestTeamRepository_Update(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		team := &Team{ID: uuid.New(), Name: "updated", Slug: "updated", Settings: json.RawMessage("{}")}

		mock.ExpectExec(`UPDATE teams`).
			WithArgs(
				"updated", "updated", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), team.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Update(context.Background(), team)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		team := &Team{ID: uuid.New(), Name: "ghost", Slug: "ghost", Settings: json.RawMessage("{}")}

		mock.ExpectExec(`UPDATE teams`).
			WithArgs(
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), team.ID,
			).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Update(context.Background(), team)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Team Delete ---

func TestTeamRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM teams`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newTeamMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM teams`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamMember Add ---

func TestTeamMemberRepository_Add(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamMemberMockDB(t)
		defer cleanup()

		member := &TeamMember{
			TeamID: uuid.New(),
			UserID: uuid.New(),
			Role:   "member",
		}

		mock.ExpectExec(`INSERT INTO team_members`).
			WithArgs(
				sqlmock.AnyArg(), member.TeamID, member.UserID, "member",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Add(context.Background(), member)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, member.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamMember Remove ---

func TestTeamMemberRepository_Remove(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamMemberMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		userID := uuid.New()

		mock.ExpectExec(`DELETE FROM team_members`).
			WithArgs(teamID, userID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Remove(context.Background(), teamID, userID)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newTeamMemberMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		userID := uuid.New()

		mock.ExpectExec(`DELETE FROM team_members`).
			WithArgs(teamID, userID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Remove(context.Background(), teamID, userID)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamMember GetUserRole ---

func TestTeamMemberRepository_GetUserRole(t *testing.T) {
	t.Run("has role", func(t *testing.T) {
		repo, mock, cleanup := newTeamMemberMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		userID := uuid.New()

		mock.ExpectQuery(`SELECT role FROM team_members`).
			WithArgs(teamID, userID).
			WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("admin"))

		role, err := repo.GetUserRole(context.Background(), teamID, userID)
		assert.NoError(t, err)
		assert.Equal(t, "admin", role)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not a member returns empty", func(t *testing.T) {
		repo, mock, cleanup := newTeamMemberMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		userID := uuid.New()

		mock.ExpectQuery(`SELECT role FROM team_members`).
			WithArgs(teamID, userID).
			WillReturnError(sql.ErrNoRows)

		role, err := repo.GetUserRole(context.Background(), teamID, userID)
		assert.NoError(t, err)
		assert.Empty(t, role)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamMember CountByTeam ---

func TestTeamMemberRepository_CountByTeam(t *testing.T) {
	t.Run("returns count", func(t *testing.T) {
		repo, mock, cleanup := newTeamMemberMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(5))

		count, err := repo.CountByTeam(context.Background(), teamID)
		assert.NoError(t, err)
		assert.Equal(t, 5, count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamInvitation Create ---

func TestTeamInvitationRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		invitation := &TeamInvitation{
			TeamID:    uuid.New(),
			Email:     "user@example.com",
			Role:      "member",
			InvitedBy: uuid.New(),
		}

		mock.ExpectExec(`INSERT INTO team_invitations`).
			WithArgs(
				sqlmock.AnyArg(), invitation.TeamID, "user@example.com", "member",
				invitation.InvitedBy, sqlmock.AnyArg(), "pending",
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), invitation)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, invitation.ID)
		assert.NotEmpty(t, invitation.Token)
		assert.Equal(t, "pending", invitation.Status)
		assert.False(t, invitation.ExpiresAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamInvitation Accept ---

func TestTeamInvitationRepository_Accept(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE team_invitations SET status = 'accepted'`).
			WithArgs(sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Accept(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found or not pending", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE team_invitations SET status = 'accepted'`).
			WithArgs(sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Accept(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamInvitation Decline ---

func TestTeamInvitationRepository_Decline(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE team_invitations SET status = 'declined'`).
			WithArgs(sqlmock.AnyArg(), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Decline(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamInvitation HasPendingInvitation ---

func TestTeamInvitationRepository_HasPendingInvitation(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(teamID, "user@example.com").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		exists, err := repo.HasPendingInvitation(context.Background(), teamID, "user@example.com")
		assert.NoError(t, err)
		assert.True(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("does not exist", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT`).
			WithArgs(teamID, "other@example.com").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

		exists, err := repo.HasPendingInvitation(context.Background(), teamID, "other@example.com")
		assert.NoError(t, err)
		assert.False(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- TeamInvitation CleanupExpired ---

func TestTeamInvitationRepository_CleanupExpired(t *testing.T) {
	t.Run("returns affected count", func(t *testing.T) {
		repo, mock, cleanup := newTeamInvitationMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE team_invitations SET status = 'expired'`).
			WillReturnResult(sqlmock.NewResult(0, 3))

		count, err := repo.CleanupExpired(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
