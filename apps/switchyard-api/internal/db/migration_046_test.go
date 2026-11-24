package db

import (
	"embed"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Migration 046 creates the platform-ci project that Weighbridge files the
// shared runner pool's minutes under (RFC 0049 Phase 1).

//go:embed migrations/046_platform_ci_project.up.sql
//go:embed migrations/046_platform_ci_project.down.sql
var migration046FS embed.FS

const platformCIProjectID = "02799103-7f4d-443a-af62-b4ba6015a92c"

func readMigration046(t *testing.T, name string) string {
	t.Helper()
	data, err := migration046FS.ReadFile("migrations/" + name)
	require.NoError(t, err)
	return stripSQLLineComments(string(data))
}

func TestMigration046_InsertIsIdempotentAndTeamParented(t *testing.T) {
	up := readMigration046(t, "046_platform_ci_project.up.sql")
	assert.Contains(t, up, "'"+platformCIProjectID+"'")
	assert.Contains(t, up, "'platform-ci'")
	assert.Contains(t, up, "(SELECT id FROM teams WHERE slug = 'madfam-platform')")
	assert.Regexp(t, regexp.MustCompile(`ON CONFLICT DO NOTHING;\s*$`), up,
		"a target-less ON CONFLICT covers both the id and the slug")
	assert.NotRegexp(t, regexp.MustCompile(`(?i)\bUPDATE\b|\bDELETE\b`), up, "the up migration only inserts")
}

func TestMigration046_DownRefusesToEraseMeteredMinutes(t *testing.T) {
	down := readMigration046(t, "046_platform_ci_project.down.sql")
	assert.Contains(t, down, "FROM usage_events WHERE project_id = '"+platformCIProjectID+"'")
	assert.Contains(t, down, "RAISE EXCEPTION")
	assert.Contains(t, down, "DELETE FROM projects WHERE id = '"+platformCIProjectID+"' AND slug = 'platform-ci'")
}

// Weighbridge must name exactly the project the migration creates, or every
// runner event fails the usage_events foreign key and is lost.
func TestMigration046_WeighbridgeNamesThisProject(t *testing.T) {
	manifest := filepath.Join("..", "..", "..", "..", "infra", "k8s", "production", "monitoring", "weighbridge.yaml")
	data, err := os.ReadFile(manifest)
	require.NoError(t, err)
	text := string(data)
	idx := strings.Index(text, "- name: WEIGHBRIDGE_DEFAULT_PROJECT_ID")
	require.GreaterOrEqual(t, idx, 0, "weighbridge.yaml must set WEIGHBRIDGE_DEFAULT_PROJECT_ID")
	assert.Contains(t, text[idx:idx+200], `value: "`+platformCIProjectID+`"`)
}
