package db

import (
	"context"
	"strings"
	"testing"
)

// Without a database the lock refuses rather than running fn unlocked: a
// write that proceeds without the lock is the lost-update window it exists to
// close.
func TestWithTunnelConfigLockRefusesWithoutADatabase(t *testing.T) {
	ran := false
	fn := func(context.Context) error { ran = true; return nil }

	var nilRepos *Repositories
	if err := nilRepos.WithTunnelConfigLock(context.Background(), "tunnel-a", fn); err == nil {
		t.Fatalf("nil repositories must refuse")
	}
	if err := (&Repositories{}).WithTunnelConfigLock(context.Background(), "tunnel-a", fn); err == nil ||
		!strings.Contains(err.Error(), "database connection not initialized") {
		t.Fatalf("missing database must refuse, got %v", err)
	}
	if ran {
		t.Fatalf("fn ran without the lock")
	}
}

func TestWithTunnelConfigLockRefusesAnEmptyTunnelID(t *testing.T) {
	err := (&Repositories{}).WithTunnelConfigLock(context.Background(), "  ", func(context.Context) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "no tunnel id") {
		t.Fatalf("an empty tunnel id must refuse, got %v", err)
	}
}
