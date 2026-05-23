package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newProjectMockDB(t *testing.T) (*ProjectRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewProjectRepository(db)
	return repo, mock, func() { db.Close() }
}

// projectColumns returns the standard column set for project queries.
var projectColumns = []string{"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at"}

func projectRow(p *types.Project) *sqlmock.Rows {
	mode := p.CIRunnerMode
	if mode == "" {
		mode = types.CIRunnerModeGitHub
	}
	return sqlmock.NewRows(projectColumns).
		AddRow(p.ID, p.Name, p.Slug, mode, p.CreatedAt, p.UpdatedAt)
}

// --- Create ---

func TestProjectRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		project := &types.Project{Name: "my-project", Slug: "my-project"}

		mock.ExpectExec(`INSERT INTO projects`).
			WithArgs(sqlmock.AnyArg(), "my-project", "my-project", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(project)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, project.ID, "ID should be assigned")
		assert.False(t, project.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.False(t, project.UpdatedAt.IsZero(), "UpdatedAt should be set")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("duplicate slug error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		project := &types.Project{Name: "dup", Slug: "dup"}

		mock.ExpectExec(`INSERT INTO projects`).
			WithArgs(sqlmock.AnyArg(), "dup", "dup", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("pq: duplicate key value violates unique constraint \"projects_slug_key\""))

		err := repo.Create(project)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		project := &types.Project{Name: "fail", Slug: "fail"}

		mock.ExpectExec(`INSERT INTO projects`).
			WithArgs(sqlmock.AnyArg(), "fail", "fail", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("connection refused"))

		err := repo.Create(project)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestProjectRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		expected := &types.Project{ID: id, Name: "proj", Slug: "proj", CreatedAt: now, UpdatedAt: now}

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(projectRow(expected))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.Name, result.Name)
		assert.Equal(t, expected.Slug, result.Slug)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetBySlug ---

func TestProjectRepository_GetBySlug(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		expected := &types.Project{ID: uuid.New(), Name: "found", Slug: "found-slug", CreatedAt: now, UpdatedAt: now}

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug = \$1`).
			WithArgs("found-slug").
			WillReturnRows(projectRow(expected))

		result, err := repo.GetBySlug("found-slug")
		assert.NoError(t, err)
		assert.Equal(t, expected.Slug, result.Slug)
		assert.Equal(t, expected.Name, result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE slug = \$1`).
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetBySlug("nonexistent")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- List ---

func TestProjectRepository_List(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).
			WillReturnRows(sqlmock.NewRows(projectColumns))

		results, err := repo.List()
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		rows := sqlmock.NewRows(projectColumns).
			AddRow(uuid.New(), "alpha", "alpha", "github", now, now).
			AddRow(uuid.New(), "beta", "beta", "github", now.Add(-time.Hour), now.Add(-time.Hour))

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).
			WillReturnRows(rows)

		results, err := repo.List()
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "alpha", results[0].Name)
		assert.Equal(t, "beta", results[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.List()
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("scan error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		// Return a row with wrong number of columns to trigger scan error
		rows := sqlmock.NewRows([]string{"id", "name"}).
			AddRow(uuid.New(), "partial")

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects ORDER BY created_at DESC`).
			WillReturnRows(rows)

		results, err := repo.List()
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateCIRunnerMode ---

func TestProjectRepository_UpdateCIRunnerMode(t *testing.T) {
	t.Run("success - self-hosted", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE projects SET ci_runner_mode = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.CIRunnerModeSelfHosted, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateCIRunnerMode(context.Background(), id, types.CIRunnerModeSelfHosted)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success - github", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE projects SET ci_runner_mode = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.CIRunnerModeGitHub, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateCIRunnerMode(context.Background(), id, types.CIRunnerModeGitHub)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE projects SET ci_runner_mode = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.CIRunnerModeSelfHosted, id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.UpdateCIRunnerMode(context.Background(), id, types.CIRunnerModeSelfHosted)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE projects SET ci_runner_mode = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.CIRunnerModeSelfHosted, id).
			WillReturnError(fmt.Errorf("constraint violation"))

		err := repo.UpdateCIRunnerMode(context.Background(), id, types.CIRunnerModeSelfHosted)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "constraint")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- Delete ---

func TestProjectRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM projects WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM projects WHERE id = \$1`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM projects WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("foreign key violation"))

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "foreign key")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByTeam (XC-2 enforcement) ---

func TestProjectRepository_ListByTeam(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		p := &types.Project{
			ID: uuid.New(), Name: "Dhanam", Slug: "dhanam",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}

		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at\s+FROM projects\s+WHERE team_id = \$1`).
			WithArgs(teamID).
			WillReturnRows(projectRow(p))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, p.Slug, out[0].Slug)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty result", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at\s+FROM projects\s+WHERE team_id = \$1`).
			WithArgs(teamID).
			WillReturnRows(sqlmock.NewRows(projectColumns))

		out, err := repo.ListByTeam(context.Background(), teamID)
		require.NoError(t, err)
		assert.Empty(t, out)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		teamID := uuid.New()
		mock.ExpectQuery(`SELECT id, name, slug`).
			WithArgs(teamID).
			WillReturnError(fmt.Errorf("connection refused"))

		_, err := repo.ListByTeam(context.Background(), teamID)
		require.Error(t, err)
	})
}

func TestProjectRepository_ListByIDs(t *testing.T) {
	t.Run("empty ids", func(t *testing.T) {
		repo, _, cleanup := newProjectMockDB(t)
		defer cleanup()
		out, err := repo.ListByIDs(context.Background(), nil)
		require.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		id := uuid.New()
		p := &types.Project{
			ID: id, Name: "Demo", Slug: "demo",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		mock.ExpectQuery(`SELECT id, name, slug, ci_runner_mode, created_at, updated_at FROM projects WHERE id = ANY\(\$1\)`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnRows(projectRow(p))

		out, err := repo.ListByIDs(context.Background(), []uuid.UUID{id})
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "demo", out[0].Slug)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetTeamID (XC-2 Round 5 enforcement helper) ---

func TestProjectRepository_GetTeamID(t *testing.T) {
	t.Run("returns team id when set", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		projID := uuid.New()
		teamID := uuid.New()

		mock.ExpectQuery(`SELECT team_id FROM projects WHERE id = \$1`).
			WithArgs(projID).
			WillReturnRows(sqlmock.NewRows([]string{"team_id"}).AddRow(teamID))

		out, err := repo.GetTeamID(context.Background(), projID)
		require.NoError(t, err)
		assert.Equal(t, teamID, out)
	})

	t.Run("returns uuid.Nil when team_id is null (personal)", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		projID := uuid.New()
		mock.ExpectQuery(`SELECT team_id FROM projects WHERE id = \$1`).
			WithArgs(projID).
			WillReturnRows(sqlmock.NewRows([]string{"team_id"}).AddRow(nil))

		out, err := repo.GetTeamID(context.Background(), projID)
		require.NoError(t, err)
		assert.Equal(t, uuid.Nil, out)
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		projID := uuid.New()
		mock.ExpectQuery(`SELECT team_id FROM projects WHERE id = \$1`).
			WithArgs(projID).
			WillReturnError(sql.ErrNoRows)

		_, err := repo.GetTeamID(context.Background(), projID)
		assert.ErrorIs(t, err, sql.ErrNoRows)
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newProjectMockDB(t)
		defer cleanup()

		projID := uuid.New()
		mock.ExpectQuery(`SELECT team_id FROM projects WHERE id = \$1`).
			WithArgs(projID).
			WillReturnError(fmt.Errorf("connection refused"))

		_, err := repo.GetTeamID(context.Background(), projID)
		require.Error(t, err)
	})
}
