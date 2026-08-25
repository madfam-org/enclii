package provisioning

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// UserlistDrift is the result of comparing the PgBouncer userlist against the
// login roles that actually exist in Postgres (pg_authid). It exists because a
// hand-applied userlist Secret silently dropped four users on 2026-08-23 and
// took api.fortuna.tube (plus bloom-scroll, ceq) hard-down for days: pgbouncer
// rejected auth while the same credentials worked against Postgres directly.
// See internal-devops/runbooks/2026-08-24-pgbouncer-userlist-outage-diagnosis.md.
//
// MissingFromUserlist is the dangerous set: a login role Postgres accepts but
// pgbouncer has no line for — every pooled connection as that role fails auth.
// StaleInUserlist is a userlist line for a role that no longer exists as a
// login role in Postgres — harmless to connectivity, but noise worth clearing.
type UserlistDrift struct {
	MissingFromUserlist []string // login roles in pg_authid, absent from the userlist
	StaleInUserlist     []string // userlist entries with no matching login role
}

// HasMissing reports whether any login role is unroutable through the pooler —
// the fail-closed condition worth paging on.
func (d UserlistDrift) HasMissing() bool { return len(d.MissingFromUserlist) > 0 }

// Empty reports whether the userlist and Postgres login roles agree exactly.
func (d UserlistDrift) Empty() bool {
	return len(d.MissingFromUserlist) == 0 && len(d.StaleInUserlist) == 0
}

// ignoredRoles are Postgres login roles that legitimately never appear in the
// application userlist: the pooler's own admin/stats identity and the cluster
// superuser. Reconcile must not flag these as "missing".
var ignoredRoles = map[string]bool{
	"postgres":        true,
	"pgbouncer_admin": true,
}

// DiffUserlist compares the set of Postgres login-role names against the set of
// usernames parsed from a PgBouncer userlist. It is a pure function so the
// drift logic is unit-testable without a cluster or a database — the two
// callers below supply the real inputs.
//
// `pgLoginRoles` is the rolname of every role with rolcanlogin=true.
// `userlistUsers` is the first quoted field of every userlist line.
func DiffUserlist(pgLoginRoles, userlistUsers []string) UserlistDrift {
	inUserlist := make(map[string]bool, len(userlistUsers))
	for _, u := range userlistUsers {
		inUserlist[u] = true
	}
	inPg := make(map[string]bool, len(pgLoginRoles))
	for _, r := range pgLoginRoles {
		inPg[r] = true
	}

	var missing, stale []string
	for _, role := range pgLoginRoles {
		if ignoredRoles[role] {
			continue
		}
		if !inUserlist[role] {
			missing = append(missing, role)
		}
	}
	for _, user := range userlistUsers {
		if ignoredRoles[user] {
			continue
		}
		if !inPg[user] {
			stale = append(stale, user)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	return UserlistDrift{MissingFromUserlist: missing, StaleInUserlist: stale}
}

// parseUserlistUsernames extracts the username (first quoted field) from each
// non-empty line of a PgBouncer userlist. Lines that are not in the
// `"user" "pass"` shape are skipped rather than erroring — a malformed line is
// not a role, and this detector's job is comparison, not validation.
func parseUserlistUsernames(userlist string) []string {
	var users []string
	for _, line := range strings.Split(userlist, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "\"") {
			continue
		}
		// First quoted field: text between the first and second double-quote.
		rest := line[1:]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			continue
		}
		if name := rest[:end]; name != "" {
			users = append(users, name)
		}
	}
	return users
}

// ReconcileUserlist reads the login roles from Postgres and the current
// PgBouncer userlist Secret, and returns their drift. It performs NO mutation:
// restoring a dropped credential requires the role's password, which lives in
// the consuming service's own secret, not in pg_authid — so repair stays a
// deliberate, per-service operator action (scripts/restore-pgbouncer-users.py
// in internal-devops). This method is the detector the outage lacked; wire it
// into a periodic check that pages when HasMissing() is true.
func (u *PgBouncerUpdater) ReconcileUserlist(ctx context.Context, adminURL string) (UserlistDrift, error) {
	roles, err := queryLoginRoles(ctx, adminURL)
	if err != nil {
		return UserlistDrift{}, fmt.Errorf("query login roles: %w", err)
	}

	secretClient := u.clientset.CoreV1().Secrets(pgbouncerNamespace)
	secret, err := secretClient.Get(ctx, pgbouncerUserlistName, k8smetav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		// No userlist at all: every login role is unroutable. Report them all
		// as missing rather than erroring — that is the actionable truth.
		return DiffUserlist(roles, nil), nil
	}
	if err != nil {
		return UserlistDrift{}, fmt.Errorf("get secret %s/%s: %w", pgbouncerNamespace, pgbouncerUserlistName, err)
	}

	users := parseUserlistUsernames(string(secret.Data[pgbouncerUserKey]))
	drift := DiffUserlist(roles, users)

	if drift.HasMissing() {
		u.logger.Error(ctx, "PgBouncer userlist is missing login roles — pooled auth will fail for them",
			logging.String("missing", strings.Join(drift.MissingFromUserlist, ",")))
	}
	if len(drift.StaleInUserlist) > 0 {
		u.logger.Info(ctx, "PgBouncer userlist has stale entries (no matching login role)",
			logging.String("stale", strings.Join(drift.StaleInUserlist, ",")))
	}
	return drift, nil
}

// queryLoginRoles returns the rolname of every login-capable role in Postgres.
// Uses the same admin connection string and database/sql path as
// PostgresProvisioner so there is one Postgres-access idiom in this package.
func queryLoginRoles(ctx context.Context, adminURL string) ([]string, error) {
	db, err := sql.Open("postgres", adminURL)
	if err != nil {
		return nil, fmt.Errorf("connect to admin postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping admin postgres: %w", err)
	}

	rows, err := db.QueryContext(ctx, "SELECT rolname FROM pg_authid WHERE rolcanlogin = true")
	if err != nil {
		return nil, fmt.Errorf("select login roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var roles []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan rolname: %w", err)
		}
		roles = append(roles, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate login roles: %w", err)
	}
	return roles, nil
}
