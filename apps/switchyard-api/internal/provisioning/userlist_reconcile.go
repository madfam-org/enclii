package provisioning

import (
	"context"
	"database/sql"
	"fmt"
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
//
// Since 2026-10-21 the pooler can also resolve a role with auth_query (see
// pgbouncer_auth_query.go). MissingFromUserlist then means "pgbouncer cannot
// authenticate this role by either path", not literally "no userlist line";
// the field keeps its name because the metric, the alert and the admin API
// already carry it.
type UserlistDrift struct {
	MissingFromUserlist []string // login roles pgbouncer cannot authenticate (no line, and auth_query cannot resolve them)
	StaleInUserlist     []string // userlist entries with no matching login role

	AuthQuery            bool     // pgbouncer.ini configures auth_query
	AuthUser             string   // the auth_user auth_query runs as
	AuthQueryProblem     string   // why auth_query cannot work right now; empty when it can or is off
	DirectConnectSkipped []string // declared direct-connect login roles, not counted
}

// HasMissing reports whether any login role is unroutable through the pooler —
// the fail-closed condition worth paging on.
func (d UserlistDrift) HasMissing() bool { return len(d.MissingFromUserlist) > 0 }

// Empty reports whether the userlist and Postgres login roles agree exactly.
func (d UserlistDrift) Empty() bool {
	return len(d.MissingFromUserlist) == 0 && len(d.StaleInUserlist) == 0
}

// ignoredRoles are Postgres login roles that legitimately never appear in the
// application userlist: the pooler's own admin/stats identity, its auth_query
// identity (its line is written by the pooler's init container from the
// pgbouncer-auth Secret, not kept in pgbouncer-userlist) and the cluster
// superuser. Reconcile must not flag these as "missing"; when auth_query is
// on, checkAuthQueryPath checks the auth role on its own terms.
var ignoredRoles = map[string]bool{
	"postgres":        true,
	"pgbouncer_admin": true,
	"pgbouncer_auth":  true,
}

// DiffUserlist compares the set of Postgres login-role names against the set of
// usernames parsed from a PgBouncer userlist. It is a pure function so the
// drift logic is unit-testable without a cluster or a database — the two
// callers below supply the real inputs.
//
// `pgLoginRoles` is the rolname of every role with rolcanlogin=true.
// `userlistUsers` is the first quoted field of every userlist line.
func DiffUserlist(pgLoginRoles, userlistUsers []string) UserlistDrift {
	roles := make([]LoginRole, 0, len(pgLoginRoles))
	for _, r := range pgLoginRoles {
		roles = append(roles, LoginRole{Name: r})
	}
	return evaluatePooledAuth(pooledAuthInputs{Roles: roles, UserlistUsers: userlistUsers})
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

// ReconcileUserlist reads the login roles from Postgres, the pooler's config
// (pgbouncer.ini and the direct-connect declaration in the pgbouncer-config
// ConfigMap), the userlist Secret and, when auth_query is on, the auth path
// (the pgbouncer-auth Secret, the auth role and the lookup function), and
// returns the roles pgbouncer cannot authenticate. It performs NO mutation:
// restoring a dropped credential requires the role's password, which lives in
// the consuming service's own secret, not in pg_authid — so repair stays a
// deliberate, per-service operator action (scripts/restore-pgbouncer-users.py
// in internal-devops), or, with auth_query on, a fix of the auth path itself.
// The periodic checker (internal/reconciler/pgbouncer_drift_checker.go) pages
// when HasMissing() is true.
func (u *PgBouncerUpdater) ReconcileUserlist(ctx context.Context, adminURL string) (UserlistDrift, error) {
	roles, err := u.queryLoginRoles(ctx, adminURL)
	if err != nil {
		return UserlistDrift{}, fmt.Errorf("query login roles: %w", err)
	}

	core := u.clientset.CoreV1()
	// The ConfigMap is what the pooler runs from; reading it, not a flag,
	// keeps the detector on the same truth. A missing ConfigMap is an error:
	// without it the detector cannot tell which auth mode the pooler is in.
	cm, err := core.ConfigMaps(pgbouncerNamespace).Get(ctx, pgbouncerConfigMap, k8smetav1.GetOptions{})
	if err != nil {
		return UserlistDrift{}, fmt.Errorf("get configmap %s/%s: %w", pgbouncerNamespace, pgbouncerConfigMap, err)
	}
	authCfg := parsePgbouncerAuthConfig(cm.Data[pgbouncerConfigKey])
	direct := parseDirectConnectRoles(cm.Data[pgbouncerDirectConnectKey])

	var users []string
	secret, err := core.Secrets(pgbouncerNamespace).Get(ctx, pgbouncerUserlistName, k8smetav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		// No userlist at all: report what that means rather than erroring.
	case err != nil:
		return UserlistDrift{}, fmt.Errorf("get secret %s/%s: %w", pgbouncerNamespace, pgbouncerUserlistName, err)
	default:
		users = parseUserlistUsernames(string(secret.Data[pgbouncerUserKey]))
	}

	problem := ""
	if authCfg.Enabled() {
		password, found, err := u.authSecretPassword(ctx)
		if err != nil {
			return UserlistDrift{}, err
		}
		problem, err = checkAuthQueryPath(ctx, sqlAuthPathDB{adminURL: adminURL, openDB: u.dbOpener()}, authCfg, password, found)
		if err != nil {
			return UserlistDrift{}, fmt.Errorf("check auth_query path: %w", err)
		}
	}

	drift := evaluatePooledAuth(pooledAuthInputs{
		Roles:            roles,
		UserlistUsers:    users,
		DirectConnect:    direct,
		AuthQuery:        authCfg,
		AuthQueryProblem: problem,
	})

	if drift.AuthQueryProblem != "" {
		u.logger.Error(ctx, "PgBouncer auth_query is configured but cannot work — roles without a userlist line fail pooled auth",
			logging.String("problem", drift.AuthQueryProblem))
	}
	if drift.HasMissing() {
		u.logger.Error(ctx, "PgBouncer cannot authenticate these login roles — pooled auth will fail for them",
			logging.String("missing", strings.Join(drift.MissingFromUserlist, ",")),
			logging.Bool("auth_query", drift.AuthQuery))
	}
	if len(drift.StaleInUserlist) > 0 {
		u.logger.Info(ctx, "PgBouncer userlist has stale entries (no matching login role)",
			logging.String("stale", strings.Join(drift.StaleInUserlist, ",")))
	}
	return drift, nil
}

// authSecretPassword reads pgbouncer_auth's password from the pgbouncer-auth
// Secret. found is false when the Secret or the key is absent.
func (u *PgBouncerUpdater) authSecretPassword(ctx context.Context) (string, bool, error) {
	secret, err := u.clientset.CoreV1().Secrets(pgbouncerNamespace).Get(ctx, pgbouncerAuthSecretName, k8smetav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get secret %s/%s: %w", pgbouncerNamespace, pgbouncerAuthSecretName, err)
	}
	value, ok := secret.Data[pgbouncerAuthSecretKey]
	if !ok {
		return "", false, nil
	}
	return strings.TrimSpace(string(value)), true, nil
}

// dbOpener returns the admin-connection opener; tests replace it with sqlmock.
func (u *PgBouncerUpdater) dbOpener() func(string) (*sql.DB, error) {
	if u.openDB != nil {
		return u.openDB
	}
	return func(dsn string) (*sql.DB, error) { return sql.Open("postgres", dsn) }
}

// queryLoginRoles returns every login-capable role in Postgres, with whether
// it is a superuser and whether auth_query could return a usable password for
// it. Uses the same admin connection string and database/sql path as
// PostgresProvisioner so there is one Postgres-access idiom in this package.
func (u *PgBouncerUpdater) queryLoginRoles(ctx context.Context, adminURL string) ([]LoginRole, error) {
	db, err := u.dbOpener()(adminURL)
	if err != nil {
		return nil, fmt.Errorf("connect to admin postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping admin postgres: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT rolname, rolsuper,
	       rolpassword IS NOT NULL AND (rolvaliduntil IS NULL OR rolvaliduntil > now())
	  FROM pg_authid WHERE rolcanlogin = true`)
	if err != nil {
		return nil, fmt.Errorf("select login roles: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var roles []LoginRole
	for rows.Next() {
		var r LoginRole
		if err := rows.Scan(&r.Name, &r.Superuser, &r.Resolvable); err != nil {
			return nil, fmt.Errorf("scan login role: %w", err)
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate login roles: %w", err)
	}
	return roles, nil
}
