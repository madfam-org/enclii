package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newTemplateMockDB(t *testing.T) (*TemplateRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewTemplateRepository(db)
	return repo, mock, func() { db.Close() }
}

var templateColumns = []string{
	"id", "slug", "name", "description", "long_description", "category",
	"framework", "language", "tags",
	"source_type", "source_repo", "source_branch", "source_path", "config",
	"icon_url", "preview_url", "screenshot_urls",
	"author", "author_url", "documentation_url",
	"deploy_count", "star_count", "is_official", "is_featured", "is_public",
	"created_at", "updated_at",
}

func templateRow(id uuid.UUID, slug, name string, now time.Time) []driver.Value {
	configJSON, _ := json.Marshal(types.TemplateConfig{})
	return []driver.Value{
		id, slug, name,
		"A test template",                     // description
		"Long description",                    // long_description
		"starter",                             // category
		"next.js",                             // framework
		"typescript",                          // language
		pq.Array([]string{"web", "react"}),    // tags
		"github",                              // source_type
		"org/repo",                            // source_repo
		"main",                                // source_branch
		"./",                                  // source_path
		configJSON,                            // config
		"https://icon.png",                    // icon_url
		"https://preview",                     // preview_url
		pq.Array([]string{"https://ss1.png"}), // screenshot_urls
		"Enclii",                              // author
		"https://enclii.dev",                  // author_url
		"https://docs",                        // documentation_url
		42,                                    // deploy_count
		10,                                    // star_count
		true,                                  // is_official
		true,                                  // is_featured
		true,                                  // is_public
		now,                                   // created_at
		now,                                   // updated_at
	}
}

// --- Create ---

func TestTemplateRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		tmpl := &types.Template{
			Slug:       "nextjs-starter",
			Name:       "Next.js Starter",
			Category:   types.TemplateCategoryStarter,
			SourceType: types.TemplateSourceGitHub,
			IsPublic:   true,
			Tags:       []string{"web"},
			Config:     types.TemplateConfig{},
		}

		mock.ExpectExec(`INSERT INTO templates`).
			WithArgs(
				sqlmock.AnyArg(), "nextjs-starter", "Next.js Starter",
				sqlmock.AnyArg(), sqlmock.AnyArg(), // description, long_description (NullString)
				"starter", sqlmock.AnyArg(), sqlmock.AnyArg(), // category, framework, language
				sqlmock.AnyArg(),                                     // tags
				"github", sqlmock.AnyArg(), "", "", sqlmock.AnyArg(), // source_type, source_repo, source_branch, source_path, config
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), // icon_url, preview_url, screenshot_urls
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), // author, author_url, documentation_url
				false, false, true, // is_official, is_featured, is_public
				sqlmock.AnyArg(), sqlmock.AnyArg(), // created_at, updated_at
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), tmpl)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, tmpl.ID)
		assert.False(t, tmpl.CreatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		tmpl := &types.Template{
			Slug:       "dup-slug",
			Name:       "Duplicate",
			Category:   types.TemplateCategoryAPI,
			SourceType: types.TemplateSourceInternal,
			Config:     types.TemplateConfig{},
		}

		mock.ExpectExec(`INSERT INTO templates`).
			WithArgs(
				sqlmock.AnyArg(), "dup-slug", "Duplicate",
				sqlmock.AnyArg(), sqlmock.AnyArg(),
				"api", sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(),
				"internal", sqlmock.AnyArg(), "", "", sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
				false, false, false,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("duplicate key value"))

		err := repo.Create(context.Background(), tmpl)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestTemplateRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(templateColumns).
				AddRow(templateRow(id, "nextjs-starter", "Next.js Starter", now)...))

		result, err := repo.GetByID(context.Background(), id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, "nextjs-starter", result.Slug)
		assert.Equal(t, "Next.js Starter", result.Name)
		assert.Equal(t, types.TemplateCategory("starter"), result.Category)
		assert.Equal(t, 42, result.DeployCount)
		assert.True(t, result.IsOfficial)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("connection reset"))

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetBySlug ---

func TestTemplateRepository_GetBySlug(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs("nextjs-starter").
			WillReturnRows(sqlmock.NewRows(templateColumns).
				AddRow(templateRow(id, "nextjs-starter", "Next.js Starter", now)...))

		result, err := repo.GetBySlug(context.Background(), "nextjs-starter")
		assert.NoError(t, err)
		assert.Equal(t, "nextjs-starter", result.Slug)
		assert.Equal(t, "Next.js Starter", result.Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs("nonexistent").
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetBySlug(context.Background(), "nonexistent")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- List ---

func TestTemplateRepository_List(t *testing.T) {
	t.Run("no filters", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(templateColumns).
			AddRow(templateRow(uuid.New(), "alpha", "Alpha", now)...).
			AddRow(templateRow(uuid.New(), "beta", "Beta", now)...)

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WillReturnRows(rows)

		results, err := repo.List(context.Background(), nil)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "alpha", results[0].Slug)
		assert.Equal(t, "beta", results[1].Slug)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty results", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WillReturnRows(sqlmock.NewRows(templateColumns))

		results, err := repo.List(context.Background(), nil)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.List(context.Background(), nil)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetCategories ---

func TestTemplateRepository_GetCategories(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		rows := sqlmock.NewRows([]string{"category", "count"}).
			AddRow("starter", 5).
			AddRow("api", 3)

		mock.ExpectQuery(`SELECT category, COUNT`).
			WillReturnRows(rows)

		result, err := repo.GetCategories(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 5, result["starter"])
		assert.Equal(t, 3, result["api"])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT category, COUNT`).
			WillReturnError(fmt.Errorf("db unavailable"))

		result, err := repo.GetCategories(context.Background())
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- IncrementDeployCount ---

func TestTemplateRepository_IncrementDeployCount(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE templates SET deploy_count = deploy_count`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.IncrementDeployCount(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE templates SET deploy_count = deploy_count`).
			WithArgs(id).
			WillReturnError(fmt.Errorf("db unavailable"))

		err := repo.IncrementDeployCount(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetFeatured ---

func TestTemplateRepository_GetFeatured(t *testing.T) {
	t.Run("returns featured templates", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(templateColumns).
			AddRow(templateRow(uuid.New(), "featured-1", "Featured One", now)...)

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs(6).
			WillReturnRows(rows)

		results, err := repo.GetFeatured(context.Background(), 0) // 0 defaults to 6
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "featured-1", results[0].Slug)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newTemplateMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT id, slug, name, description, long_description, category`).
			WithArgs(6).
			WillReturnError(fmt.Errorf("db unavailable"))

		results, err := repo.GetFeatured(context.Background(), 0)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
