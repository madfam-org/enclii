package db

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed migrations/033_custom_domain_cloudflare_for_saas.up.sql
var migration033Up string

//go:embed migrations/033_custom_domain_cloudflare_for_saas.down.sql
var migration033Down string

func TestMigration033AddsCloudflareForSaaSColumns(t *testing.T) {
	required := []string{
		"ALTER TABLE custom_domains",
		"ADD COLUMN IF NOT EXISTS custom_hostname_id TEXT",
		"ADD COLUMN IF NOT EXISTS custom_hostname_status TEXT",
		"ADD COLUMN IF NOT EXISTS custom_hostname_ssl_status TEXT",
		"ADD COLUMN IF NOT EXISTS pending_dns_records JSONB",
		"ADD COLUMN IF NOT EXISTS provisioning_error TEXT",
		"ADD COLUMN IF NOT EXISTS provisioning_checked_at TIMESTAMPTZ",
		"CREATE INDEX IF NOT EXISTS idx_custom_domains_custom_hostname_id",
	}

	for _, want := range required {
		if !strings.Contains(migration033Up, want) {
			t.Fatalf("migration 033 up missing %q", want)
		}
	}
}

func TestMigration033IsAdditiveOnly(t *testing.T) {
	// The up migration must not drop or rewrite existing data: every domain
	// already provisioned via the zone+CNAME path keeps working untouched.
	forbidden := []string{"DROP COLUMN", "DROP TABLE", "DELETE FROM", "TRUNCATE", "UPDATE custom_domains"}

	upper := strings.ToUpper(migration033Up)
	for _, bad := range forbidden {
		if strings.Contains(upper, bad) {
			t.Fatalf("migration 033 up contains destructive statement %q", bad)
		}
	}
}

func TestMigration033DownIsIdempotent(t *testing.T) {
	required := []string{
		"DROP INDEX IF EXISTS idx_custom_domains_custom_hostname_id",
		"DROP COLUMN IF EXISTS custom_hostname_id",
		"DROP COLUMN IF EXISTS custom_hostname_status",
		"DROP COLUMN IF EXISTS custom_hostname_ssl_status",
		"DROP COLUMN IF EXISTS pending_dns_records",
		"DROP COLUMN IF EXISTS provisioning_error",
		"DROP COLUMN IF EXISTS provisioning_checked_at",
	}

	for _, want := range required {
		if !strings.Contains(migration033Down, want) {
			t.Fatalf("migration 033 down missing %q", want)
		}
	}
}
