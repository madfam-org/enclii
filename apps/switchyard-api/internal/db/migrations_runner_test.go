package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
)

type migrationStartupProbe struct {
	upErr      error
	version    uint
	dirty      bool
	versionErr error
	calls      []string
}

func (p *migrationStartupProbe) Up() error {
	p.calls = append(p.calls, "up")
	return p.upErr
}

func (p *migrationStartupProbe) Version() (uint, bool, error) {
	p.calls = append(p.calls, "version")
	return p.version, p.dirty, p.versionErr
}

// These are intentionally present to catch reintroducing automatic recovery.
// A dirty 042 must not run 041.down and discard junction environment bindings;
// a dirty first migration must never drop the database.
func (p *migrationStartupProbe) Force(int) error {
	panic("startup must not force migration versions")
}
func (p *migrationStartupProbe) Steps(int) error {
	panic("startup must not run down migrations")
}
func (p *migrationStartupProbe) Drop() error {
	panic("startup must not drop data")
}

func TestMigrationStartupRefusesDirtyStateWithoutRecovery(t *testing.T) {
	for _, version := range []uint{1, 41, 42, 43} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			cause := migrate.ErrDirty{Version: int(version)}
			probe := &migrationStartupProbe{upErr: cause, version: version, dirty: true}
			err := runPendingMigrations(probe)
			require.ErrorIs(t, err, cause)
			require.Contains(t, err.Error(), fmt.Sprintf("migration v%d is dirty", version))
			require.Contains(t, err.Error(), "reviewed repair")
			require.Contains(t, err.Error(), "preserve existing data and operational holds")
			require.Equal(t, []string{"up", "version"}, probe.calls)
		})
	}
}

func TestMigrationStartupCleanAndUnchanged(t *testing.T) {
	for _, upErr := range []error{nil, migrate.ErrNoChange} {
		probe := &migrationStartupProbe{upErr: upErr}
		require.NoError(t, runPendingMigrations(probe))
		require.Equal(t, []string{"up"}, probe.calls)
	}
}

func TestMigrationStartupDoesNotRetryFailedMigration(t *testing.T) {
	cause := errors.New("migration execution failed")
	for _, versionErr := range []error{nil, errors.New("version unavailable")} {
		probe := &migrationStartupProbe{upErr: cause, version: 42, versionErr: versionErr}
		err := runPendingMigrations(probe)
		require.ErrorIs(t, err, cause)
		if versionErr != nil {
			require.ErrorIs(t, err, versionErr)
			require.Contains(t, err.Error(), "operator inspection required")
		}
		require.Equal(t, []string{"up", "version"}, probe.calls)
	}
}
