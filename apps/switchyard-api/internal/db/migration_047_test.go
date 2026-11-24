package db

import (
	"embed"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Migration 047 creates the daily resource ledger that waybill-aggregator
// writes from Prometheus (RFC 0049 Phase 2). The column list the aggregator
// writes is checked against this file from the waybill module
// (apps/waybill/internal/resourceusage/store_test.go).

//go:embed migrations/047_resource_daily.up.sql
//go:embed migrations/047_resource_daily.down.sql
var migration047FS embed.FS

func readMigration047(t *testing.T, name string) string {
	t.Helper()
	data, err := migration047FS.ReadFile("migrations/" + name)
	require.NoError(t, err)
	return stripSQLLineComments(string(data))
}

func TestMigration047_IsAdditiveAndIdempotent(t *testing.T) {
	up := readMigration047(t, "047_resource_daily.up.sql")
	for _, table := range []string{"resource_daily", "node_daily", "resource_daily_captures"} {
		assert.Contains(t, up, "CREATE TABLE IF NOT EXISTS "+table+" (")
	}
	assert.NotRegexp(t, regexp.MustCompile(`(?i)\b(DROP|DELETE|UPDATE|ALTER)\b`), up,
		"the up migration only creates")
}

// The grain is what makes the aggregator's upsert idempotent; a grain without
// node or container would merge rows the allocation and rightsizing need apart.
func TestMigration047_GrainIncludesNodeAndContainer(t *testing.T) {
	up := readMigration047(t, "047_resource_daily.up.sql")
	assert.Contains(t, up,
		"CONSTRAINT resource_daily_grain UNIQUE (date, cluster, node, namespace, workload_kind, workload_name, container)")
	assert.Contains(t, up, "CONSTRAINT node_daily_grain UNIQUE (date, cluster, node)")
	assert.Contains(t, up, "PRIMARY KEY (date, cluster)")
}

// Usage and requests only: money belongs to cost_line and the allocation.
func TestMigration047_CarriesNoMoney(t *testing.T) {
	up := readMigration047(t, "047_resource_daily.up.sql")
	ddl := regexp.MustCompile(`(?is)COMMENT ON .*?;`).ReplaceAllString(up, "")
	assert.NotRegexp(t, regexp.MustCompile(`(?i)\b\w*(cost|price|usd|mxn|cents|amount)\w*\b`), ddl)
}

// Measured values are nullable (NULL = not measured), never DEFAULT 0.
func TestMigration047_MeasurementsAreNullableWithoutZeroDefault(t *testing.T) {
	up := readMigration047(t, "047_resource_daily.up.sql")
	for _, col := range []string{"cpu_core_hours_used", "cpu_core_hours_requested", "mem_gib_hours_used", "mem_working_set_max_bytes"} {
		line := regexp.MustCompile(`(?m)^\s*` + col + `\s+[^\n]*$`).FindString(up)
		require.NotEmpty(t, line, col)
		assert.NotContains(t, line, "NOT NULL", col)
		assert.NotContains(t, line, "DEFAULT", col)
	}
}

func TestMigration047_DownRefusesToEraseCapturedDays(t *testing.T) {
	down := readMigration047(t, "047_resource_daily.down.sql")
	assert.Contains(t, down, "EXISTS (SELECT 1 FROM resource_daily_captures)")
	assert.Contains(t, down, "EXISTS (SELECT 1 FROM resource_daily)")
	assert.Contains(t, down, "RAISE EXCEPTION")
	assert.Contains(t, down, "DROP TABLE IF EXISTS resource_daily;")
}
