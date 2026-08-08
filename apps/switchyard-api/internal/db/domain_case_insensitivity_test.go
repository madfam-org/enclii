package db

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BROKEN-2: custom_domains.domain and junctions.domain are varchar(255) with
// plain btree indexes, and every ownership gate was `WHERE domain = $1`.
// Cloudflare matches hostnames case-insensitively (strings.EqualFold in
// FindCustomHostname), so a case variant was a DIFFERENT hostname to Postgres
// and the SAME hostname at the edge — enough to hold a junction for
// `App.Victim.com` on your own project, pass every ownership check, and have
// the victim's `app.victim.com` deleted from the edge.
//
// These assert the emitted SQL, because that is where the defect lived: the
// query text, not the Go around it.

func TestJunctionRepository_DomainLookupsAreCaseInsensitive(t *testing.T) {
	t.Run("ExistsByDomainPath", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM junctions WHERE lower\(domain\) = lower\(\$1\) AND path = \$2\)`).
			WithArgs("App.Victim.com", "/").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		exists, err := repo.ExistsByDomainPath(context.Background(), "App.Victim.com", "/")
		require.NoError(t, err)
		assert.True(t, exists, "a case variant must collide with the stored hostname")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("CountOtherByDomain", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		excludeID := uuid.New()
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM junctions WHERE lower\(domain\) = lower\(\$1\) AND id <> \$2`).
			WithArgs("App.Victim.com", excludeID).
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

		count, err := repo.CountOtherByDomain(context.Background(), "App.Victim.com", excludeID)
		require.NoError(t, err)
		assert.Equal(t, 1, count, "the victim's junction must be counted, not missed")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ProjectIDsByDomain is the owner junction-provisioned hostnames never had.
// Nothing else in the schema records that a junction's project is entitled to
// be served on its hostname.
func TestJunctionRepository_ProjectIDsByDomain(t *testing.T) {
	t.Run("returns every project serving the hostname", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		projectA, projectB := uuid.New(), uuid.New()
		mock.ExpectQuery(`SELECT DISTINCT project_id FROM junctions WHERE lower\(domain\) = lower\(\$1\)`).
			WithArgs("App.Client.com").
			WillReturnRows(sqlmock.NewRows([]string{"project_id"}).AddRow(projectA).AddRow(projectB))

		ids, err := repo.ProjectIDsByDomain(context.Background(), "App.Client.com")
		require.NoError(t, err)
		assert.ElementsMatch(t, []uuid.UUID{projectA, projectB}, ids)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no junction is an empty result, not an error", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT DISTINCT project_id FROM junctions`).
			WithArgs("free.example.com").
			WillReturnRows(sqlmock.NewRows([]string{"project_id"}))

		ids, err := repo.ProjectIDsByDomain(context.Background(), "free.example.com")
		require.NoError(t, err)
		assert.Empty(t, ids)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// "Could not tell" must never render as "nobody owns it": callers read an
	// empty result as unowned and an error as a refusal.
	t.Run("a failed query is an error, never an empty result", func(t *testing.T) {
		repo, mock, cleanup := newJunctionMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT DISTINCT project_id FROM junctions`).
			WillReturnError(assertError{})

		ids, err := repo.ProjectIDsByDomain(context.Background(), "app.client.com")
		assert.Error(t, err)
		assert.Nil(t, ids)
	})
}

func TestCustomDomainRepository_DomainLookupsAreCaseInsensitive(t *testing.T) {
	t.Run("GetByDomain", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		now := time.Now().Truncate(time.Microsecond)
		mock.ExpectQuery(`FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)`).
			WithArgs("App.Victim.com").
			WillReturnRows(sqlmock.NewRows(customDomainColumns).AddRow(
				append([]driver.Value{
					uuid.New(), uuid.New(), uuid.New(), "app.victim.com", true, true,
					"letsencrypt-prod", now, now, &now, nil, false, false, nil,
					"cloudflare-for-saas", "active", nil,
				}, "ch-victim", "active", "active", nil, "", now)...,
			))

		record, err := repo.GetByDomain(context.Background(), "App.Victim.com")
		require.NoError(t, err)
		require.NotNil(t, record, "the victim's row must be found through a case variant")
		assert.Equal(t, "app.victim.com", record.Domain)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Exists", func(t *testing.T) {
		repo, mock, cleanup := newCustomDomainMockDB(t)
		defer cleanup()

		mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)\)`).
			WithArgs("APP.VICTIM.COM").
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		exists, err := repo.Exists(context.Background(), "APP.VICTIM.COM")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// assertError is a sentinel for "the query failed".
type assertError struct{}

func (assertError) Error() string { return "connection reset by peer" }
