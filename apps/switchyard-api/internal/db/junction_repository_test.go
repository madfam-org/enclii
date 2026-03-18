package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

func newJunctionMockDB(t *testing.T) (*JunctionRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	repo := NewJunctionRepository(db)
	return repo, mock, func() { db.Close() }
}

var junctionColumns = []string{
	"id", "project_id", "service_id", "domain", "path", "protocol",
	"tls_enabled", "tls_issuer", "tls_cert_secret", "tls_min_version", "tls_force_redirect",
	"created_at", "updated_at",
}

func sampleJunction(projectID, serviceID uuid.UUID) *types.Junction {
	now := time.Now().Truncate(time.Microsecond)
	return &types.Junction{
		ID:        uuid.New(),
		ProjectID: projectID,
		ServiceID: serviceID,
		Domain:    "api.example.com",
		Path:      "/v1",
		Protocol:  "https",
		TLS: &types.TLSConfig{
			Enabled:       true,
			Issuer:        "letsencrypt-prod",
			MinVersion:    "1.2",
			ForceRedirect: true,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func junctionRow(j *types.Junction) *sqlmock.Rows {
	var tlsCertSecret sql.NullString
	var tlsMinVersion sql.NullString
	tlsEnabled := true
	tlsIssuer := "letsencrypt-prod"
	tlsForceRedirect := true

	if j.TLS != nil {
		tlsEnabled = j.TLS.Enabled
		tlsIssuer = j.TLS.Issuer
		tlsForceRedirect = j.TLS.ForceRedirect
		if j.TLS.CertSecret != "" {
			tlsCertSecret = sql.NullString{String: j.TLS.CertSecret, Valid: true}
		}
		if j.TLS.MinVersion != "" {
			tlsMinVersion = sql.NullString{String: j.TLS.MinVersion, Valid: true}
		}
	}

	return sqlmock.NewRows(junctionColumns).
		AddRow(
			j.ID, j.ProjectID, j.ServiceID, j.Domain, j.Path, j.Protocol,
			tlsEnabled, tlsIssuer, tlsCertSecret, tlsMinVersion, tlsForceRedirect,
			j.CreatedAt, j.UpdatedAt,
		)
}

// --- Create ---

func TestJunctionRepository_Create(t *testing.T) {
	t.Run("success with TLS config", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		junction := &types.Junction{
			ProjectID: projectID,
			ServiceID: serviceID,
			Domain:    "api.example.com",
			Path:      "/v1",
			Protocol:  "https",
			TLS: &types.TLSConfig{
				Enabled:       true,
				Issuer:        "letsencrypt-prod",
				MinVersion:    "1.2",
				ForceRedirect: true,
			},
		}

		mock.ExpectExec(`INSERT INTO junctions`).
			WithArgs(
				sqlmock.AnyArg(), projectID, serviceID, "api.example.com", "/v1", "https",
				true, "letsencrypt-prod", sql.NullString{Valid: false}, "1.2", true,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), junction)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, junction.ID)
		assert.False(t, junction.CreatedAt.IsZero())
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("success with defaults", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		junction := &types.Junction{
			ProjectID: projectID,
			ServiceID: serviceID,
			Domain:    "app.example.com",
		}

		// Path defaults to "/", Protocol defaults to "https",
		// TLS defaults are applied when TLS is nil
		mock.ExpectExec(`INSERT INTO junctions`).
			WithArgs(
				sqlmock.AnyArg(), projectID, serviceID, "app.example.com", "/", "https",
				true, "letsencrypt-prod", sql.NullString{Valid: false}, "1.2", true,
				sqlmock.AnyArg(), sqlmock.AnyArg(),
			).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Create(context.Background(), junction)
		assert.NoError(t, err)
		assert.Equal(t, "/", junction.Path)
		assert.Equal(t, "https", junction.Protocol)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		junction := &types.Junction{
			ProjectID: uuid.New(),
			ServiceID: uuid.New(),
			Domain:    "fail.example.com",
		}

		mock.ExpectExec(`INSERT INTO junctions`).
			WillReturnError(sql.ErrConnDone)

		err := repo.Create(context.Background(), junction)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- GetByID ---

func TestJunctionRepository_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		expected := sampleJunction(projectID, serviceID)

		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(expected.ID).
			WillReturnRows(junctionRow(expected))

		result, err := repo.GetByID(context.Background(), expected.ID)
		assert.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, expected.ID, result.ID)
		assert.Equal(t, expected.ProjectID, result.ProjectID)
		assert.Equal(t, expected.ServiceID, result.ServiceID)
		assert.Equal(t, expected.Domain, result.Domain)
		assert.Equal(t, expected.Path, result.Path)
		assert.Equal(t, expected.Protocol, result.Protocol)
		require.NotNil(t, result.TLS)
		assert.True(t, result.TLS.Enabled)
		assert.Equal(t, "letsencrypt-prod", result.TLS.Issuer)
		assert.Equal(t, "1.2", result.TLS.MinVersion)
		assert.True(t, result.TLS.ForceRedirect)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(id).
			WillReturnError(sql.ErrNoRows)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(id).
			WillReturnError(sql.ErrConnDone)

		result, err := repo.GetByID(context.Background(), id)
		assert.Nil(t, result)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByProject ---

func TestJunctionRepository_ListByProject(t *testing.T) {
	t.Run("multiple results", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		serviceID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(junctionColumns).
			AddRow(uuid.New(), projectID, serviceID, "api.example.com", "/v1", "https", true, "letsencrypt-prod", sql.NullString{Valid: false}, sql.NullString{String: "1.2", Valid: true}, true, now, now).
			AddRow(uuid.New(), projectID, serviceID, "app.example.com", "/", "https", true, "letsencrypt-staging", sql.NullString{Valid: false}, sql.NullString{String: "1.3", Valid: true}, true, now, now)

		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(projectID).
			WillReturnRows(rows)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Len(t, results, 2)
		assert.Equal(t, "api.example.com", results[0].Domain)
		assert.Equal(t, "app.example.com", results[1].Domain)
		assert.Equal(t, "1.3", results[1].TLS.MinVersion)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(projectID).
			WillReturnRows(sqlmock.NewRows(junctionColumns))

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.NoError(t, err)
		assert.Empty(t, results)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectID := uuid.New()
		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(projectID).
			WillReturnError(sql.ErrConnDone)

		results, err := repo.ListByProject(context.Background(), projectID)
		assert.Nil(t, results)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ExistsByDomainPath ---

func TestJunctionRepository_ExistsByDomainPath_True(t *testing.T) {
	repo, mock, cleanup := newJunctionMockDB(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs("api.example.com", "/v1").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	exists, err := repo.ExistsByDomainPath(context.Background(), "api.example.com", "/v1")
	assert.NoError(t, err)
	assert.True(t, exists)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJunctionRepository_ExistsByDomainPath_False(t *testing.T) {
	repo, mock, cleanup := newJunctionMockDB(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs("new.example.com", "/").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	exists, err := repo.ExistsByDomainPath(context.Background(), "new.example.com", "/")
	assert.NoError(t, err)
	assert.False(t, exists)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJunctionRepository_ExistsByDomainPath_Error(t *testing.T) {
	repo, mock, cleanup := newJunctionMockDB(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT EXISTS`).
		WithArgs("api.example.com", "/").
		WillReturnError(sql.ErrConnDone)

	exists, err := repo.ExistsByDomainPath(context.Background(), "api.example.com", "/")
	assert.Error(t, err)
	assert.False(t, exists)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Delete ---

func TestJunctionRepository_Delete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM junctions WHERE id`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := repo.Delete(context.Background(), id)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found returns ErrNoRows", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM junctions WHERE id`).
			WithArgs(id).
			WillReturnResult(sqlmock.NewResult(0, 0))

		err := repo.Delete(context.Background(), id)
		assert.ErrorIs(t, err, sql.ErrNoRows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("db error", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		id := uuid.New()
		mock.ExpectExec(`DELETE FROM junctions WHERE id`).
			WithArgs(id).
			WillReturnError(sql.ErrConnDone)

		err := repo.Delete(context.Background(), id)
		assert.Error(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// --- ListByService ---

func TestJunctionRepository_ListByService(t *testing.T) {
	t.Run("returns junctions for service", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		serviceID := uuid.New()
		projectID := uuid.New()
		now := time.Now().Truncate(time.Microsecond)

		rows := sqlmock.NewRows(junctionColumns).
			AddRow(uuid.New(), projectID, serviceID, "svc.example.com", "/", "https", true, "letsencrypt-prod", sql.NullString{Valid: false}, sql.NullString{String: "1.2", Valid: true}, true, now, now)

		mock.ExpectQuery(`SELECT id, project_id, service_id, domain, path, protocol`).
			WithArgs(serviceID).
			WillReturnRows(rows)

		results, err := repo.ListByService(context.Background(), serviceID)
		assert.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "svc.example.com", results[0].Domain)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
