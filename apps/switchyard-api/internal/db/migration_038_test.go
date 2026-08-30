package db

import (
	"embed"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Migration 038 creates the Crea Tu Mundo (CTM) tenant team and parents its
// two apps (crea-map, crea-frontend) to it. Both projects postdate the
// 2026-05-02 audit that 024 backfilled, so they were team_id IS NULL. The
// same SQL-layer invariants as 024 apply: re-running must be safe, the down
// migration must be symmetric, and the admin user is resolved by email.

//go:embed migrations/038_ctm_team_crea.up.sql
//go:embed migrations/038_ctm_team_crea.down.sql
var migration038FS embed.FS

func readMigration038(t *testing.T, name string) string {
	t.Helper()
	data, err := migration038FS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// The teams insert must be idempotent (ON CONFLICT (slug) DO NOTHING) so a
// redeploy that reruns migrations does not fail on the unique slug.
func TestMigration038_TeamsInsertIsIdempotent(t *testing.T) {
	up := readMigration038(t, "038_ctm_team_crea.up.sql")
	assert.Contains(t, up, "ON CONFLICT (slug) DO NOTHING",
		"teams insert must use ON CONFLICT (slug) DO NOTHING for re-run safety")
}

// It creates exactly the crea team.
func TestMigration038_CreatesCreaTeam(t *testing.T) {
	up := readMigration038(t, "038_ctm_team_crea.up.sql")
	assert.Regexp(t, regexp.MustCompile(`'Crea Tu Mundo',\s*'crea',`), up,
		"up migration must insert the 'crea' team")
	assert.Contains(t, up, `'{"tier":"client"}'::jsonb`,
		"the CTM team must be tier=client like the white-glove clients")
}

// Every UPDATE projects must be guarded by team_id IS NULL so a re-run never
// stomps a project moved to a different team.
func TestMigration038_ProjectUpdatesAreGuarded(t *testing.T) {
	up := readMigration038(t, "038_ctm_team_crea.up.sql")
	stripped := stripSQLLineComments(up)
	updates := len(regexp.MustCompile(`UPDATE projects SET team_id`).FindAllString(stripped, -1))
	guards := len(regexp.MustCompile(`(?i)team_id IS NULL`).FindAllString(stripped, -1))
	assert.Greater(t, updates, 0, "migration should contain at least one UPDATE projects statement")
	assert.Equal(t, updates, guards,
		"every UPDATE projects must be guarded by `team_id IS NULL` (got %d updates, %d guards)", updates, guards)
}

// The two CTM projects are the ones re-parented.
func TestMigration038_ReparentsBothCreaProjects(t *testing.T) {
	up := readMigration038(t, "038_ctm_team_crea.up.sql")
	assert.Contains(t, up, "'crea-map'", "must re-parent crea-map")
	assert.Contains(t, up, "'crea-frontend'", "must re-parent crea-frontend")
}

// The team_members backfill must use NOT EXISTS to skip an already-linked
// admin (the (team_id, user_id) pair is unique).
func TestMigration038_TeamMembersInsertGuardsExisting(t *testing.T) {
	up := readMigration038(t, "038_ctm_team_crea.up.sql")
	assert.Contains(t, up, "NOT EXISTS",
		"team_members backfill must use NOT EXISTS to skip an already-linked admin")
	assert.Contains(t, up, "team_members tm",
		"team_members guard subquery must reference the table")
}

// The admin user is resolved by email, not a hard-coded UUID (which would lock
// the migration to one DB instance).
func TestMigration038_AdminUserResolvedByEmail(t *testing.T) {
	up := readMigration038(t, "038_ctm_team_crea.up.sql")
	assert.Contains(t, up, "u.email = 'admin@madfam.io'",
		"admin user must be resolved via email, not a hard-coded UUID")
}

// The down migration deletes the crea team the up migration created.
func TestMigration038_DownIsSymmetric(t *testing.T) {
	down := readMigration038(t, "038_ctm_team_crea.down.sql")
	assert.Contains(t, down, "DELETE FROM teams WHERE slug = 'crea'",
		"down migration must delete the crea team")
	assert.Contains(t, down, "'crea-map'", "down must un-parent crea-map")
	assert.Contains(t, down, "'crea-frontend'", "down must un-parent crea-frontend")
}

// golang-migrate wraps each file in its own transaction; a bare BEGIN/COMMIT
// would double-wrap and fail.
func TestMigration038_NoBareTransactionMarkers(t *testing.T) {
	for _, name := range []string{
		"038_ctm_team_crea.up.sql",
		"038_ctm_team_crea.down.sql",
	} {
		body := readMigration038(t, name)
		bareBegin := regexp.MustCompile(`(?m)^\s*BEGIN\s*;`)
		bareCommit := regexp.MustCompile(`(?m)^\s*COMMIT\s*;`)
		assert.False(t, bareBegin.MatchString(body),
			"%s must not contain a bare BEGIN; (golang-migrate already wraps in a tx)", name)
		assert.False(t, bareCommit.MatchString(body),
			"%s must not contain a bare COMMIT; (golang-migrate already wraps in a tx)", name)
	}
}
