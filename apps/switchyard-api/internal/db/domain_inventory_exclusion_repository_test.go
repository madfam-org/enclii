package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomainInventoryExclusionRepository_ListActive(t *testing.T) {
	t.Run("returns active exclusions", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		repo := NewDomainInventoryExclusionRepository(db)
		id := uuid.New()
		now := time.Now()

		mock.ExpectQuery(`SELECT id, hostname_pattern, source, route_target, classification, reason`).
			WillReturnRows(sqlmock.NewRows([]string{
				"id", "hostname_pattern", "source", "route_target", "classification",
				"reason", "active", "created_at", "updated_at",
			}).AddRow(
				id, "*", "kubernetes_configmap", "enclii/status-config-madfam",
				"status_page_catalog", "catalog only", true, now, now,
			))

		out, err := repo.ListActive(context.Background())
		require.NoError(t, err)
		require.Len(t, out, 1)
		assert.Equal(t, "*", out[0].HostnamePattern)
		assert.Equal(t, "kubernetes_configmap", out[0].Source)
		assert.Equal(t, "enclii/status-config-madfam", out[0].RouteTarget)
		assert.Equal(t, "status_page_catalog", out[0].Classification)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("wraps query errors", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		repo := NewDomainInventoryExclusionRepository(db)
		mock.ExpectQuery(`SELECT id, hostname_pattern, source, route_target, classification, reason`).
			WillReturnError(fmt.Errorf("missing table"))

		out, err := repo.ListActive(context.Background())
		assert.Nil(t, out)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list domain inventory exclusions")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
