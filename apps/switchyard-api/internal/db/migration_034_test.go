package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/034_canonical_lowercase_domains.up.sql
var migration034Up string

//go:embed migrations/034_canonical_lowercase_domains.down.sql
var migration034Down string

// Migration 034 brings rows already in the table into the canonical lowercase
// form the code now writes and compares with. Without it, one pre-existing
// mixed-case row keeps the cross-tenant hole open on its own.
func TestMigration034NormalisesBothDomainColumns(t *testing.T) {
	for _, want := range []string{
		"UPDATE custom_domains",
		"UPDATE junctions",
		"SET domain = lower(domain)",
		"WHERE domain <> lower(domain)",
	} {
		if !strings.Contains(migration034Up, want) {
			t.Errorf("migration 034 up missing %q", want)
		}
	}
}

// A collision means two records already claim one hostname, which is the harm
// this migration exists to close. It must refuse and name them, never pick a
// winner and delete the loser.
func TestMigration034RefusesOnCollisionRatherThanDroppingARow(t *testing.T) {
	if !strings.Contains(migration034Up, "RAISE EXCEPTION") {
		t.Error("migration 034 up does not refuse on a case collision")
	}
	upper := strings.ToUpper(migration034Up)
	for _, destructive := range []string{"DROP TABLE", "DROP COLUMN", "DELETE FROM", "TRUNCATE"} {
		if strings.Contains(upper, destructive) {
			t.Errorf("migration 034 up contains destructive statement %q", destructive)
		}
	}
}

// The down migration must not attempt to restore the original casing: it is
// not recoverable, it carried no information, and re-introducing mixed-case
// rows would re-open the hole.
func TestMigration034DownDoesNotRestoreMixedCase(t *testing.T) {
	upper := strings.ToUpper(migration034Down)
	for _, forbidden := range []string{"UPDATE CUSTOM_DOMAINS", "UPDATE JUNCTIONS", "DROP", "DELETE"} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("migration 034 down contains %q; reverting must not touch the rows", forbidden)
		}
	}
}
