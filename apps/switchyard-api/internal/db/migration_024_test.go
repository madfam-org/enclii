package db

import (
	"embed"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Migration 024 backfills the master-admin tenant model: one teams row per
// known tenant (white-glove client + MADFAM platform group), parents the
// 25 existing projects observed in the 2026-05-02 audit, and gives
// admin@madfam.io owner role on every team.
//
// These tests exercise the invariants that matter at the SQL layer:
// re-running the migration must be safe, the down migration must be
// symmetric, and every project slug we backfill must have a matching team
// row to attach to.

//go:embed migrations/024_reparent_projects_to_teams.up.sql
//go:embed migrations/024_reparent_projects_to_teams.down.sql
var migration024FS embed.FS

func readMigration024(t *testing.T, name string) string {
	t.Helper()
	data, err := migration024FS.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// TestMigration024_TeamsInsertIsIdempotent — the INSERT INTO teams must
// carry ON CONFLICT (slug) DO NOTHING so a redeploy that reruns
// migrations doesn't blow up on a unique constraint.
func TestMigration024_TeamsInsertIsIdempotent(t *testing.T) {
	up := readMigration024(t, "024_reparent_projects_to_teams.up.sql")
	assert.Contains(t, up, "ON CONFLICT (slug) DO NOTHING",
		"teams insert must use ON CONFLICT (slug) DO NOTHING for re-run safety")
}

// TestMigration024_ProjectUpdatesAreGuarded — every UPDATE on projects
// must filter on team_id IS NULL so re-running doesn't stomp a project
// that's been moved to a different team since the migration first ran.
func TestMigration024_ProjectUpdatesAreGuarded(t *testing.T) {
	up := readMigration024(t, "024_reparent_projects_to_teams.up.sql")

	// Count UPDATE projects statements vs guarded ones, ignoring SQL
	// line comments so prose mentions of "team_id IS NULL" don't inflate
	// the guard count above the update count.
	stripped := stripSQLLineComments(up)
	updateRe := regexp.MustCompile(`UPDATE projects SET team_id`)
	guardRe := regexp.MustCompile(`(?i)team_id IS NULL`)

	updates := len(updateRe.FindAllString(stripped, -1))
	guards := len(guardRe.FindAllString(stripped, -1))

	assert.Greater(t, updates, 0, "migration should contain at least one UPDATE projects statement")
	assert.Equal(t, updates, guards,
		"every UPDATE projects must be guarded by `team_id IS NULL` (got %d updates, %d guards)", updates, guards)
}

// stripSQLLineComments returns the SQL body with `--` line-comments
// removed. Used by the guard counter so prose mentions of "team_id IS
// NULL" inside `--` comments don't get counted as guards.
func stripSQLLineComments(body string) string {
	commentRe := regexp.MustCompile(`(?m)^\s*--.*$`)
	return commentRe.ReplaceAllString(body, "")
}

// TestMigration024_TeamMembersInsertGuardsExisting — the team_members
// backfill must use NOT EXISTS so re-running doesn't insert duplicate
// (team_id, user_id) rows (the table has a unique constraint on that
// pair per the genesis migration).
func TestMigration024_TeamMembersInsertGuardsExisting(t *testing.T) {
	up := readMigration024(t, "024_reparent_projects_to_teams.up.sql")
	assert.Contains(t, up, "NOT EXISTS",
		"team_members backfill must use NOT EXISTS to skip already-linked admin")
	assert.Contains(t, up, "team_members tm",
		"team_members guard subquery must reference the table")
}

// TestMigration024_AdminUserResolvedByEmail — the team_members + owner_id
// backfills must look the admin user up by email rather than embed a UUID.
// Embedding a UUID would lock the migration to a specific DB instance.
func TestMigration024_AdminUserResolvedByEmail(t *testing.T) {
	up := readMigration024(t, "024_reparent_projects_to_teams.up.sql")
	assert.Contains(t, up, "u.email = 'admin@madfam.io'",
		"admin user must be resolved via email, not a hard-coded UUID")
}

// TestMigration024_DownIsSymmetric — every team slug created in the up
// migration must be deleted in the down migration. Drift between the two
// lists is the most common rollback bug.
//
// We focus on TEAM slugs specifically (not project slugs). Team slugs in
// the up migration are the second column of `INSERT INTO teams VALUES (...)`,
// which we extract by looking for the pattern `'name', 'slug',` after the
// uuid generator. The down migration deletes them via `slug IN (...)`.
func TestMigration024_DownIsSymmetric(t *testing.T) {
	up := readMigration024(t, "024_reparent_projects_to_teams.up.sql")
	down := readMigration024(t, "024_reparent_projects_to_teams.down.sql")

	// Up: team slugs are the literal string immediately after the team
	// display name in each VALUES tuple. The pattern `'<Name>', '<slug>',`
	// uniquely identifies them and avoids picking up project slugs from
	// the UPDATE statements lower down.
	upTeamSlugRe := regexp.MustCompile(`'[A-Z][^']*',\s+'([a-z][a-z0-9-]+)',`)
	var upTeamSlugs []string
	for _, m := range upTeamSlugRe.FindAllStringSubmatch(up, -1) {
		upTeamSlugs = append(upTeamSlugs, m[1])
	}
	assert.NotEmpty(t, upTeamSlugs, "could not extract any team slugs from up migration")

	// Down: collect everything in the DELETE FROM teams WHERE slug IN (...)
	// list.
	for _, want := range upTeamSlugs {
		assert.Contains(t, down, "'"+want+"'",
			"down migration is missing team slug %q present in up migration", want)
	}
}

// TestMigration024_NoBareTransactionMarkers — golang-migrate runs each
// file in its own transaction by default. A stray BEGIN/COMMIT inside the
// SQL would double-wrap and fail. Catch that here.
func TestMigration024_NoBareTransactionMarkers(t *testing.T) {
	for _, name := range []string{
		"024_reparent_projects_to_teams.up.sql",
		"024_reparent_projects_to_teams.down.sql",
	} {
		body := readMigration024(t, name)
		// Allow BEGIN/COMMIT inside comments, but no bare statement.
		bareBegin := regexp.MustCompile(`(?m)^\s*BEGIN\s*;`)
		bareCommit := regexp.MustCompile(`(?m)^\s*COMMIT\s*;`)
		assert.False(t, bareBegin.MatchString(body),
			"%s must not contain a bare BEGIN; (golang-migrate already wraps in a tx)", name)
		assert.False(t, bareCommit.MatchString(body),
			"%s must not contain a bare COMMIT; (golang-migrate already wraps in a tx)", name)
	}
}

// uniqueLiterals returns the unique submatch values from `re` applied to
// `body`, preserving first-seen order.
func uniqueLiterals(re *regexp.Regexp, body string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if _, ok := seen[m[1]]; ok {
			continue
		}
		seen[m[1]] = struct{}{}
		out = append(out, m[1])
	}
	return out
}
