package reconciler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// fakeConverger records which addons the network sweep converged and fails
// the ones listed in failing.
type fakeConverger struct {
	seen    []uuid.UUID
	failing map[uuid.UUID]bool
}

func (f *fakeConverger) ConvergeCNPGNetworkPolicies(_ context.Context, addon *types.DatabaseAddon) error {
	f.seen = append(f.seen, addon.ID)
	if f.failing[addon.ID] {
		return errors.New("apiserver unavailable")
	}
	return nil
}

func liveAddonRows(ids ...uuid.UUID) *sqlmock.Rows {
	now, project := time.Now(), uuid.New()
	rows := sqlmock.NewRows(addonDueColumns)
	for i, id := range ids {
		name := []string{"kernel01g", "kernel01h", "portal-e"}[i%3]
		rows.AddRow(id, project, nil, types.DatabaseAddonTypePostgres, name, "standard-0", types.DatabaseAddonStatusReady, "",
			[]byte("{}"), "project-abc", "pg-"+name, nil, nil, nil, nil, nil, int64(0), 0, nil, nil, "", now, now, nil, nil, nil)
	}
	return rows
}

func TestSweepCNPGNetworkPoliciesConvergesEveryLiveClusterAndSurvivesAFailure(t *testing.T) {
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	conv := &fakeConverger{failing: map[uuid.UUID]bool{a: true}}
	r.SetNetworkConverger(conv)
	mock.ExpectQuery(`(?s)WHERE type = 'postgres' AND status IN \('ready', 'provisioning'\).*deleted_at IS NULL`).
		WillReturnRows(liveAddonRows(a, b, c))

	r.sweepCNPGNetworkPolicies(context.Background())

	require.Equal(t, []uuid.UUID{a, b, c}, conv.seen, "one failure never stops the others")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepCNPGNetworkPoliciesWithoutAConvergerIsNoop(t *testing.T) {
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	r.sweepCNPGNetworkPolicies(context.Background())
	require.NoError(t, mock.ExpectationsWereMet(), "no converger: no query")
}

func TestSweepCNPGNetworkPoliciesStopsOnAListError(t *testing.T) {
	r, mock, cleanup := newSweepReconciler(t, nil)
	defer cleanup()
	conv := &fakeConverger{}
	r.SetNetworkConverger(conv)
	mock.ExpectQuery(`FROM database_addons`).WillReturnError(errors.New("db down"))
	r.sweepCNPGNetworkPolicies(context.Background())
	require.Empty(t, conv.seen)
	require.NoError(t, mock.ExpectationsWereMet())
}
