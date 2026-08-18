package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fakeFinalizer records which addons the retention sweep asked it to tear down,
// and can be made to fail to exercise the retry path.
type fakeFinalizer struct {
	seen []uuid.UUID
	err  error
}

func (f *fakeFinalizer) FinalizeExpiredDeletion(_ context.Context, addon *types.DatabaseAddon) error {
	f.seen = append(f.seen, addon.ID)
	return f.err
}

var addonDueColumns = []string{
	"id", "project_id", "environment_id", "type", "name", "plan", "status", "status_message",
	"config", "k8s_namespace", "k8s_resource_name", "connection_secret",
	"host", "port", "database_name", "username",
	"storage_used_bytes", "connections_active", "last_backup_at",
	"created_by", "created_by_email", "created_at", "updated_at", "provisioned_at",
	"deletion_scheduled_at", "deleted_at",
}

func dueAddonRow(id, projID uuid.UUID, name string, scheduledAt time.Time) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows(addonDueColumns).AddRow(
		id, projID, nil,
		types.DatabaseAddonTypePostgres, name, "standard-0", types.DatabaseAddonStatusPendingDeletion, "",
		[]byte("{}"), "project-abc", "pg-"+name, nil,
		nil, nil, nil, nil,
		int64(0), 0, nil,
		nil, "", now, now, nil,
		scheduledAt, nil,
	)
}

// newSweepReconciler builds an AddonReconciler backed by sqlmock repos, wired to
// the given finalizer. White-box construction (same package) — the reconciler's
// K8s/dynamic clients are unused by the retention sweep, so they stay nil.
func newSweepReconciler(t *testing.T, f RetentionFinalizer) (*AddonReconciler, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetOutput(logrusDiscard{})

	r := &AddonReconciler{
		repos:  db.NewRepositories(sqlDB),
		logger: logger,
		stopCh: make(chan struct{}),
	}
	r.SetRetentionFinalizer(f)
	return r, mock, func() { _ = sqlDB.Close() }
}

// logrusDiscard silences reconciler log output during tests.
type logrusDiscard struct{}

func (logrusDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestSweepExpiredRetentionHolds_FinalizesDueAddons(t *testing.T) {
	fin := &fakeFinalizer{}
	r, mock, cleanup := newSweepReconciler(t, fin)
	defer cleanup()

	id := uuid.New()
	projID := uuid.New()
	past := time.Now().Add(-time.Hour)

	mock.ExpectQuery(`(?s)WHERE status = 'pending_deletion'`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(dueAddonRow(id, projID, "old-db", past))

	r.sweepExpiredRetentionHolds(context.Background())

	require.Len(t, fin.seen, 1)
	assert.Equal(t, id, fin.seen[0])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepExpiredRetentionHolds_NoFinalizerIsNoop(t *testing.T) {
	// No ExpectQuery registered: if the sweep queried the DB, sqlmock would
	// flag an unexpected call. A nil finalizer must short-circuit before that.
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	r.finalizer = nil // explicit: SetRetentionFinalizer(nil) already leaves it nil

	r.sweepExpiredRetentionHolds(context.Background())

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepExpiredRetentionHolds_FinalizeErrorDoesNotAbortBatch(t *testing.T) {
	fin := &fakeFinalizer{err: errors.New("deprovision boom")}
	r, mock, cleanup := newSweepReconciler(t, fin)
	defer cleanup()

	id1 := uuid.New()
	id2 := uuid.New()
	projID := uuid.New()
	past := time.Now().Add(-time.Hour)

	rows := dueAddonRow(id1, projID, "db-one", past)
	rows.AddRow(
		id2, projID, nil,
		types.DatabaseAddonTypePostgres, "db-two", "standard-0", types.DatabaseAddonStatusPendingDeletion, "",
		[]byte("{}"), "project-abc", "pg-db-two", nil,
		nil, nil, nil, nil,
		int64(0), 0, nil,
		nil, "", time.Now(), time.Now(), nil,
		past, nil,
	)

	mock.ExpectQuery(`(?s)WHERE status = 'pending_deletion'`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(rows)

	r.sweepExpiredRetentionHolds(context.Background())

	// Both addons must be attempted even though the first errored — a stuck
	// teardown must not block the rest of the batch.
	require.Len(t, fin.seen, 2)
	assert.Equal(t, id1, fin.seen[0])
	assert.Equal(t, id2, fin.seen[1])
	assert.NoError(t, mock.ExpectationsWereMet())
}
