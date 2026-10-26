package reconciler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fakeBackupConverger answers each addon with the outcome listed for it.
type fakeBackupConverger struct {
	seen     []uuid.UUID
	outcomes map[uuid.UUID]error
	wired    map[uuid.UUID]bool
}

func (f *fakeBackupConverger) ConvergeAddonBackups(_ context.Context, addon *types.DatabaseAddon) (addons.BackupConvergence, error) {
	f.seen = append(f.seen, addon.ID)
	if err := f.outcomes[addon.ID]; err != nil {
		return addons.BackupConvergence{}, err
	}
	return addons.BackupConvergence{StanzaPatched: f.wired[addon.ID], ScheduledBackupCreated: f.wired[addon.ID]}, nil
}

const liveQuery = `(?s)WHERE type = 'postgres' AND status IN \('ready', 'provisioning'\).*deleted_at IS NULL`

func TestSweepAddonBackupsTalliesEveryOutcomeAndNeverStopsEarly(t *testing.T) {
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	wired, steady, hibernated, drifted, broken := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	conv := &fakeBackupConverger{
		wired: map[uuid.UUID]bool{wired: true},
		outcomes: map[uuid.UUID]error{
			hibernated: fmt.Errorf("%w: hibernated", addons.ErrBackupConvergenceNotApplicable),
			drifted:    fmt.Errorf("%w: c", addons.ErrBackupStanzaDrift),
			broken:     errors.New("apiserver unavailable"),
		},
	}
	r.SetBackupConverger(conv)
	mock.ExpectQuery(liveQuery).WillReturnRows(liveAddonRows(broken, wired, steady, hibernated, drifted))

	res := r.sweepAddonBackups(context.Background())

	require.Equal(t, []uuid.UUID{broken, wired, steady, hibernated, drifted}, conv.seen, "a failure never stops the others")
	require.Equal(t, backupSweepResult{Live: 5, Wired: 1, Skipped: 1, Drifted: 1, Failed: 1}, res)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepAddonBackupsCountsUnprotectedWhenStoreOrCredentialIsMissing(t *testing.T) {
	for name, cause := range map[string]error{
		"store unconfigured": addons.ErrBackupStoreNotConfigured,
		"credential missing": addons.ErrBackupCredentialsMissing,
	} {
		t.Run(name, func(t *testing.T) {
			r, mock, cleanup := newSweepReconciler(t, nil)
			defer cleanup()
			a, b := uuid.New(), uuid.New()
			r.SetBackupConverger(&fakeBackupConverger{outcomes: map[uuid.UUID]error{a: cause, b: cause}})
			mock.ExpectQuery(liveQuery).WillReturnRows(liveAddonRows(a, b))

			res := r.sweepAddonBackups(context.Background())

			require.Equal(t, 2, res.Unprotected)
			require.Equal(t, errors.Is(cause, addons.ErrBackupStoreNotConfigured), res.StoreUnconfigured)
			require.Zero(t, res.Failed, "a missing store is reported as unprotected, not as an apiserver failure")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSweepAddonBackupsWithoutAConvergerIsNoop(t *testing.T) {
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	require.Equal(t, backupSweepResult{}, r.sweepAddonBackups(context.Background()))
	require.NoError(t, mock.ExpectationsWereMet(), "no converger: no query")
}

func TestSweepAddonBackupsStopsOnAListError(t *testing.T) {
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	conv := &fakeBackupConverger{}
	r.SetBackupConverger(conv)
	mock.ExpectQuery(`FROM database_addons`).WillReturnError(errors.New("db down"))
	r.sweepAddonBackups(context.Background())
	require.Empty(t, conv.seen)
	require.NoError(t, mock.ExpectationsWereMet())
}
