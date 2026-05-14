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

func newReleaseMockDB(t *testing.T) (*ReleaseRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewReleaseRepository(db)
	return repo, mock, func() { db.Close() }
}

// releaseGetColumns matches the columns returned by GetByID and ListByService
var releaseGetColumns = []string{
	"id", "service_id", "version", "image_uri", "git_sha", "status",
	"sbom", "sbom_format", "image_signature", "signature_verified_at",
	"error_message", "framework_slug", "created_at", "updated_at",
}

func newTestRelease() *types.Release {
	return &types.Release{
		ServiceID:         uuid.New(),
		Version:           "v1.0.0",
		ImageURI:          "ghcr.io/org/svc:v1.0.0",
		GitSHA:            "abc123",
		GitBranch:         "main",
		CommitMessage:     "feat: initial release",
		CommitAuthorName:  "Dev",
		CommitAuthorEmail: "dev@example.com",
		Status:            types.ReleaseStatusBuilding,
	}
}

// --- Create ---

func TestReleaseRepository_Create(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		rel := newTestRelease()

		mock.ExpectExec(`INSERT INTO releases`).
			WithArgs(
				sqlmock.AnyArg(), rel.ServiceID, "v1.0.0", "ghcr.io/org/svc:v1.0.0",
				"abc123", "main", "feat: initial release", "Dev", "dev@example.com",
				(*int)(nil), "", "", "",
				types.ReleaseStatusBuilding,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(rel)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, rel.ID)
		assert.False(t, rel.CreatedAt.IsZero())
		assert.False(t, rel.UpdatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with PR metadata", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		prNum := 42
		rel := &types.Release{
			ServiceID:         uuid.New(),
			Version:           "v1.1.0",
			ImageURI:          "ghcr.io/org/svc:v1.1.0",
			GitSHA:            "def456",
			GitBranch:         "feat/new",
			CommitMessage:     "feat: add feature",
			CommitAuthorName:  "Dev",
			CommitAuthorEmail: "dev@example.com",
			PRNumber:          &prNum,
			PRTitle:           "Add feature",
			PRURL:             "https://github.com/org/repo/pull/42",
			RepoURL:           "https://github.com/org/repo",
			Status:            types.ReleaseStatusBuilding,
		}

		mock.ExpectExec(`INSERT INTO releases`).
			WithArgs(
				sqlmock.AnyArg(), rel.ServiceID, "v1.1.0", "ghcr.io/org/svc:v1.1.0",
				"def456", "feat/new", "feat: add feature", "Dev", "dev@example.com",
				&prNum, "Add feature", "https://github.com/org/repo/pull/42",
				"https://github.com/org/repo",
				types.ReleaseStatusBuilding,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(rel)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		rel := newTestRelease()
		mock.ExpectExec(`INSERT INTO releases`).
			WithArgs(
				sqlmock.AnyArg(), rel.ServiceID, "v1.0.0", "ghcr.io/org/svc:v1.0.0",
				"abc123", "main", "feat: initial release", "Dev", "dev@example.com",
				(*int)(nil), "", "", "",
				types.ReleaseStatusBuilding,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnError(fmt.Errorf("foreign key constraint on service_id"))

		err := repo.Create(rel)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestReleaseRepository_GetByID(t *testing.T) {
	t.Run("found with all fields", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		sigTime := now.Add(-time.Minute)

		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(releaseGetColumns).
				AddRow(id, svcID, "v1.0.0", "ghcr.io/org/svc:v1.0.0", "abc123",
					types.ReleaseStatusReady,
					"cyclonedx-sbom-content", "cyclonedx-json",
					"cosign-sig-abc", sigTime,
					nil,
					nil, // framework_slug
					now, now))

		result, err := repo.GetByID(id)
		assert.NoError(t, err)
		assert.Equal(t, id, result.ID)
		assert.Equal(t, svcID, result.ServiceID)
		assert.Equal(t, "v1.0.0", result.Version)
		assert.Equal(t, "ghcr.io/org/svc:v1.0.0", result.ImageURI)
		assert.Equal(t, types.ReleaseStatusReady, result.Status)
		assert.Equal(t, "cyclonedx-sbom-content", result.SBOM)
		assert.Equal(t, "cyclonedx-json", result.SBOMFormat)
		assert.Equal(t, "cosign-sig-abc", result.ImageSignature)
		assert.NotNil(t, result.SignatureVerifiedAt)
		assert.Nil(t, result.ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("found with nullable fields nil", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(releaseGetColumns).
				AddRow(id, uuid.New(), "v0.1.0", "img", "sha1",
					types.ReleaseStatusBuilding,
					nil, nil, nil, nil, nil,
					nil, // framework_slug
					now, now))

		result, err := repo.GetByID(id)
		assert.NoError(t, err)
		assert.Empty(t, result.SBOM)
		assert.Empty(t, result.SBOMFormat)
		assert.Empty(t, result.ImageSignature)
		assert.Nil(t, result.SignatureVerifiedAt)
		assert.Nil(t, result.ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("found with error message", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE id = \$1`).
			WithArgs(id).
			WillReturnRows(sqlmock.NewRows(releaseGetColumns).
				AddRow(id, uuid.New(), "v0.2.0", "img", "sha2",
					types.ReleaseStatusFailed,
					nil, nil, nil, nil,
					"build failed: exit code 1",
					nil, // framework_slug
					now, now))

		result, err := repo.GetByID(id)
		assert.NoError(t, err)
		assert.Equal(t, types.ReleaseStatusFailed, result.Status)
		assert.NotNil(t, result.ErrorMessage)
		assert.Equal(t, "build failed: exit code 1", *result.ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE id = \$1`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatus ---

func TestReleaseRepository_UpdateStatus(t *testing.T) {
	t.Run("building to ready", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET status = \$1, error_message = NULL, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.ReleaseStatusReady, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(id, types.ReleaseStatusReady)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("building to failed", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET status = \$1, error_message = NULL, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.ReleaseStatusFailed, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatus(id, types.ReleaseStatusFailed)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET status = \$1, error_message = NULL, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(types.ReleaseStatusReady, id).
			WillReturnError(fmt.Errorf("connection lost"))

		err := repo.UpdateStatus(id, types.ReleaseStatusReady)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateStatusWithError ---

func TestReleaseRepository_UpdateStatusWithError(t *testing.T) {
	t.Run("with error message", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		errMsg := "nixpacks build failed"
		mock.ExpectExec(`UPDATE releases SET status = \$1, error_message = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs(types.ReleaseStatusFailed, &errMsg, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatusWithError(id, types.ReleaseStatusFailed, &errMsg)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("with nil error clears message", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET status = \$1, error_message = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs(types.ReleaseStatusReady, (*string)(nil), id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateStatusWithError(id, types.ReleaseStatusReady, nil)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestReleaseRepository_CleanupAllStaleBuilding(t *testing.T) {
	t.Run("returns affected count", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE releases SET status = 'failed'`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 7))

		count, err := repo.CleanupAllStaleBuilding(context.Background(), 30*time.Minute)
		assert.NoError(t, err)
		assert.Equal(t, int64(7), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("zero affected", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE releases SET status = 'failed'`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 0))

		count, err := repo.CleanupAllStaleBuilding(context.Background(), 30*time.Minute)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), count)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		mock.ExpectExec(`UPDATE releases SET status = 'failed'`).
			WithArgs(sqlmock.AnyArg()).
			WillReturnError(fmt.Errorf("lock timeout"))

		count, err := repo.CleanupAllStaleBuilding(context.Background(), 30*time.Minute)
		assert.Equal(t, int64(0), count)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateImageURI ---

func TestReleaseRepository_UpdateImageURI(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		newURI := "ghcr.io/org/svc@sha256:abc123"
		mock.ExpectExec(`UPDATE releases SET image_uri = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(newURI, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateImageURI(id, newURI)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET image_uri = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs("bad-uri", id).
			WillReturnError(fmt.Errorf("constraint"))

		err := repo.UpdateImageURI(id, "bad-uri")
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateSBOM ---

func TestReleaseRepository_UpdateSBOM(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET sbom = \$1, sbom_format = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs(`{"components":[]}`, "cyclonedx-json", id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateSBOM(context.Background(), id, `{"components":[]}`, "cyclonedx-json")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET sbom = \$1, sbom_format = \$2, updated_at = NOW\(\) WHERE id = \$3`).
			WithArgs("sbom-data", "spdx-json", id).
			WillReturnError(fmt.Errorf("too large"))

		err := repo.UpdateSBOM(context.Background(), id, "sbom-data", "spdx-json")
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateSignature ---

func TestReleaseRepository_UpdateSignature(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		sig := "cosign-signature-base64"
		mock.ExpectExec(`UPDATE releases SET image_signature = \$1, signature_verified_at = NOW\(\), updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs(sig, id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateSignature(context.Background(), id, sig)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET image_signature = \$1, signature_verified_at = NOW\(\), updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs("sig", id).
			WillReturnError(fmt.Errorf("write conflict"))

		err := repo.UpdateSignature(context.Background(), id, "sig")
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateMetadata ---

func TestReleaseRepository_UpdateMetadata(t *testing.T) {
	t.Run("all fields provided", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases`).
			WithArgs(id, "feat: new feature", "Author Name", "author@example.com", "main", "https://github.com/org/repo").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateMetadata(context.Background(), id,
			"feat: new feature", "Author Name", "author@example.com", "main", "https://github.com/org/repo")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("partial fields (empty strings skip update via CASE)", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases`).
			WithArgs(id, "commit msg", "", "", "develop", "").
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateMetadata(context.Background(), id,
			"commit msg", "", "", "develop", "")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases`).
			WithArgs(id, "msg", "name", "email", "branch", "repo").
			WillReturnError(fmt.Errorf("deadlock"))

		err := repo.UpdateMetadata(context.Background(), id,
			"msg", "name", "email", "branch", "repo")
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByService ---

func TestReleaseRepository_ListByService(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)
		sigTime := now.Add(-time.Hour)

		rows := sqlmock.NewRows(releaseGetColumns).
			AddRow(uuid.New(), svcID, "v2.0.0", "ghcr.io/org/svc:v2.0.0", "sha2",
				types.ReleaseStatusReady,
				"sbom2", "cyclonedx-json", "sig2", sigTime,
				nil,
				"nextjs", // framework_slug
				now, now).
			AddRow(uuid.New(), svcID, "v1.0.0", "ghcr.io/org/svc:v1.0.0", "sha1",
				types.ReleaseStatusReady,
				nil, nil, nil, nil,
				nil,
				nil, // framework_slug (legacy row, before P3.5)
				now.Add(-24*time.Hour), now.Add(-24*time.Hour))

		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE service_id = \$1 ORDER BY created_at DESC`).
			WithArgs(svcID).
			WillReturnRows(rows)

		results, err := repo.ListByService(svcID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "v2.0.0", results[0].Version)
		assert.Equal(t, "sbom2", results[0].SBOM)
		assert.Equal(t, "sig2", results[0].ImageSignature)
		assert.NotNil(t, results[0].SignatureVerifiedAt)
		assert.Equal(t, "v1.0.0", results[1].Version)
		assert.Empty(t, results[1].SBOM)
		assert.Nil(t, results[1].SignatureVerifiedAt)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE service_id = \$1 ORDER BY created_at DESC`).
			WithArgs(svcID).
			WillReturnRows(sqlmock.NewRows(releaseGetColumns))

		results, err := repo.ListByService(svcID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status`).
			WithArgs(svcID).
			WillReturnError(fmt.Errorf("connection refused"))

		results, err := repo.ListByService(svcID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("releases with mixed error messages", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		svcID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(releaseGetColumns).
			AddRow(uuid.New(), svcID, "v3.0.0", "img3", "sha3",
				types.ReleaseStatusReady,
				nil, nil, nil, nil, nil,
				nil, // framework_slug
				now, now).
			AddRow(uuid.New(), svcID, "v2.0.0", "img2", "sha2",
				types.ReleaseStatusFailed,
				nil, nil, nil, nil, "OOM killed",
				nil, // framework_slug
				now.Add(-time.Hour), now.Add(-time.Hour))

		mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status`).
			WithArgs(svcID).
			WillReturnRows(rows)

		results, err := repo.ListByService(svcID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Nil(t, results[0].ErrorMessage)
		assert.NotNil(t, results[1].ErrorMessage)
		assert.Equal(t, "OOM killed", *results[1].ErrorMessage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- UpdateFrameworkSlug ---

func TestReleaseRepository_UpdateFrameworkSlug(t *testing.T) {
	t.Run("writes slug when non-empty", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`UPDATE releases SET framework_slug = \$1, updated_at = NOW\(\) WHERE id = \$2`).
			WithArgs("nextjs", id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.UpdateFrameworkSlug(context.Background(), id, "nextjs")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no-op for empty slug", func(t *testing.T) {
		repo, mock, cleanup := newReleaseMockDB(t)
		defer cleanup()

		// No ExpectExec — an UPDATE would cause sqlmock to fail.
		err := repo.UpdateFrameworkSlug(context.Background(), uuid.New(), "")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID populates FrameworkSlug ---

func TestReleaseRepository_GetByID_FrameworkSlug(t *testing.T) {
	repo, mock, cleanup := newReleaseMockDB(t)
	defer cleanup()

	id := uuid.New()
	svcID := uuid.New()
	now := time.Now().Truncate(time.Microsecond)

	mock.ExpectQuery(`SELECT id, service_id, version, image_uri, git_sha, status, sbom, sbom_format, image_signature, signature_verified_at, error_message, framework_slug, created_at, updated_at FROM releases WHERE id = \$1`).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows(releaseGetColumns).
			AddRow(id, svcID, "v1.0.0", "img", "sha",
				types.ReleaseStatusReady,
				nil, nil, nil, nil,
				nil,
				"go-fiber", // framework_slug
				now, now))

	result, err := repo.GetByID(id)
	assert.NoError(t, err)
	assert.Equal(t, "go-fiber", result.FrameworkSlug)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Silence unused import warning when sql isn't referenced above.
var _ = sql.NullString{}
