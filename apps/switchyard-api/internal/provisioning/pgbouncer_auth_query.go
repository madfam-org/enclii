package provisioning

import (
	"context"
	"crypto/md5" // #nosec G501 -- compares against PostgreSQL's legacy md5 verifier format; not used to protect anything
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// pgbouncer auth_query (2026-10-21). The shared pgbouncer looks up the
// password of a role that has no userlist line by running auth_query as
// auth_user in auth_dbname (infra/k8s/platform-infra/pgbouncer.yaml). The
// lookup function and the auth role are created by the owner-run
// scripts/sql/pgbouncer-auth-query.sql; the auth role's password reaches the
// pooler through the pgbouncer-auth Secret (ExternalSecret, Vault
// secret/enclii pgbouncer_auth_password).
//
// With auth_query working, "absent from the userlist" no longer means "fails
// pooled auth". The drift detector therefore asks a narrower question: which
// login roles can the pooler authenticate by NEITHER path? Answering it
// honestly means checking that the auth_query path itself works, which is
// what checkAuthQueryPath does.
const (
	// PgbouncerAuthFunction is the lookup function auth_query calls. The SQL
	// migration creates exactly this signature; a test pins all three copies
	// (this constant, the migration, pgbouncer.ini).
	PgbouncerAuthFunction = "pgbouncer.get_auth(text)"

	pgbouncerAuthSecretName   = "pgbouncer-auth"          // #nosec G101 -- Secret name, not a value // pragma: allowlist secret
	pgbouncerAuthSecretKey    = "pgbouncer_auth_password" // #nosec G101 -- Secret key name, not a value // pragma: allowlist secret
	pgbouncerDirectConnectKey = "direct-connect-roles"
)

// pgbouncerAuthConfig is the part of pgbouncer.ini's [pgbouncer] section that
// decides whether auth_query is on.
type pgbouncerAuthConfig struct {
	AuthType   string
	AuthQuery  string
	AuthUser   string
	AuthDBName string
}

// Enabled reports whether pgbouncer will try auth_query for a role with no
// userlist line: it needs both the query and the user to run it as.
func (c pgbouncerAuthConfig) Enabled() bool {
	return c.AuthQuery != "" && c.AuthUser != ""
}

// parsePgbouncerAuthConfig reads auth_type, auth_query, auth_user and
// auth_dbname from the [pgbouncer] section. Comment lines (`;` or `#`) and
// every other section are ignored. Keys are case-insensitive, as in pgbouncer.
func parsePgbouncerAuthConfig(ini string) pgbouncerAuthConfig {
	var cfg pgbouncerAuthConfig
	section := ""
	for _, raw := range strings.Split(ini, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section != "pgbouncer" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "auth_type":
			cfg.AuthType = value
		case "auth_query":
			cfg.AuthQuery = value
		case "auth_user":
			cfg.AuthUser = value
		case "auth_dbname":
			cfg.AuthDBName = value
		}
	}
	return cfg
}

// parseDirectConnectRoles reads the ConfigMap key that declares login roles
// which connect to Postgres directly and never through the pooler. One role
// per line; `#` starts a comment; blank lines are skipped. Sorted, unique.
func parseDirectConnectRoles(text string) []string {
	seen := map[string]bool{}
	var roles []string
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		roles = append(roles, line)
	}
	sort.Strings(roles)
	return roles
}

// LoginRole is one row of pg_authid with rolcanlogin = true, reduced to what
// the drift decision needs.
type LoginRole struct {
	Name      string
	Superuser bool
	// Resolvable: the role has a stored password that has not expired, so
	// pgbouncer.get_auth would return it. (get_auth also never returns a
	// superuser; Superuser covers that.)
	Resolvable bool
}

// pooledAuthInputs is everything the drift decision reads, gathered by
// ReconcileUserlist from Postgres and the cluster. Keeping the decision a pure
// function of these makes every case unit-testable.
type pooledAuthInputs struct {
	Roles         []LoginRole
	UserlistUsers []string
	DirectConnect []string
	AuthQuery     pgbouncerAuthConfig
	// AuthQueryProblem is why the auth_query path cannot work right now
	// (empty when it can, or when auth_query is not configured).
	AuthQueryProblem string
}

// evaluatePooledAuth returns the login roles pgbouncer cannot authenticate by
// either path.
//
//   - Static userlist only (auth_query not configured): a role is missing when
//     it has no userlist line. This is the pre-2026-10-21 behaviour.
//   - auth_query configured and working: a role is missing only when it has no
//     userlist line AND auth_query cannot return it (a superuser, or no usable
//     password).
//   - auth_query configured but broken: every role without a userlist line is
//     missing, and so is the auth user itself, so the count is never zero
//     while the path is broken. A pooler that depends on auth_query and cannot
//     use it is the outage class this detector exists for.
//
// Declared direct-connect roles and the pooler's own roles are never missing.
// Stale entries are userlist lines with no login role behind them.
func evaluatePooledAuth(in pooledAuthInputs) UserlistDrift {
	inUserlist := make(map[string]bool, len(in.UserlistUsers))
	for _, u := range in.UserlistUsers {
		inUserlist[u] = true
	}
	direct := make(map[string]bool, len(in.DirectConnect))
	for _, r := range in.DirectConnect {
		direct[r] = true
	}
	authUser := in.AuthQuery.AuthUser
	authQueryOn := in.AuthQuery.Enabled()
	authQueryWorks := authQueryOn && in.AuthQueryProblem == ""

	drift := UserlistDrift{
		AuthQuery:        authQueryOn,
		AuthUser:         authUser,
		AuthQueryProblem: in.AuthQueryProblem,
	}
	inPg := make(map[string]bool, len(in.Roles))
	for _, role := range in.Roles {
		inPg[role.Name] = true
		if ignoredRoles[role.Name] || (authQueryOn && role.Name == authUser) {
			continue
		}
		if direct[role.Name] {
			drift.DirectConnectSkipped = append(drift.DirectConnectSkipped, role.Name)
			continue
		}
		if inUserlist[role.Name] {
			continue
		}
		if authQueryWorks && role.Resolvable && !role.Superuser {
			continue
		}
		drift.MissingFromUserlist = append(drift.MissingFromUserlist, role.Name)
	}
	if authQueryOn && !authQueryWorks {
		drift.MissingFromUserlist = append(drift.MissingFromUserlist, authUser)
	}
	for _, user := range in.UserlistUsers {
		if ignoredRoles[user] || (authQueryOn && user == authUser) {
			continue
		}
		if !inPg[user] {
			drift.StaleInUserlist = append(drift.StaleInUserlist, user)
		}
	}
	sort.Strings(drift.MissingFromUserlist)
	sort.Strings(drift.StaleInUserlist)
	sort.Strings(drift.DirectConnectSkipped)
	return drift
}

// passwordMatchesVerifier reports whether password is the one behind a
// PostgreSQL stored password (rolpassword): a SCRAM-SHA-256 verifier, or the
// legacy "md5" + md5(password + role) form. It recomputes the verifier with
// the stored salt and iteration count and compares in constant time.
func passwordMatchesVerifier(role, password, stored string) (bool, error) {
	switch {
	case strings.HasPrefix(stored, "SCRAM-SHA-256$"):
		// SCRAM-SHA-256$<iter>:<salt>$<StoredKey>:<ServerKey>
		rest := strings.TrimPrefix(stored, "SCRAM-SHA-256$")
		params, _, ok := strings.Cut(rest, "$")
		if !ok {
			return false, fmt.Errorf("malformed SCRAM verifier for %s", role)
		}
		iterText, saltText, ok := strings.Cut(params, ":")
		if !ok {
			return false, fmt.Errorf("malformed SCRAM verifier for %s", role)
		}
		iterations, err := strconv.Atoi(iterText)
		if err != nil || iterations <= 0 {
			return false, fmt.Errorf("malformed SCRAM iteration count for %s", role)
		}
		salt, err := base64.StdEncoding.DecodeString(saltText)
		if err != nil {
			return false, fmt.Errorf("malformed SCRAM salt for %s", role)
		}
		want, err := ScramSHA256Verifier(password, salt, iterations)
		if err != nil {
			return false, err
		}
		return subtle.ConstantTimeCompare([]byte(want), []byte(stored)) == 1, nil
	case strings.HasPrefix(stored, "md5") && len(stored) == 35:
		sum := md5.Sum([]byte(password + role)) // #nosec G401 -- PostgreSQL md5 verifier format
		want := "md5" + hex.EncodeToString(sum[:])
		return subtle.ConstantTimeCompare([]byte(want), []byte(stored)) == 1, nil
	default:
		return false, fmt.Errorf("stored password for %s is neither SCRAM-SHA-256 nor md5", role)
	}
}

// withDatabase returns dsn pointed at database db. It accepts the URL form
// (postgres://...) and the key=value form lib/pq also takes.
func withDatabase(dsn, db string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse admin url: %w", err)
		}
		u.Path = "/" + db
		return u.String(), nil
	}
	// key=value: lib/pq takes the last occurrence of a key.
	return strings.TrimSpace(dsn) + " dbname=" + db, nil
}

// authPathDB is the database access checkAuthQueryPath needs: the auth role's
// stored password (cluster-wide, from pg_authid) and whether the lookup
// function exists in auth_dbname with EXECUTE for the auth role.
type authPathDB interface {
	authRolePassword(ctx context.Context, role string) (stored string, canLogin bool, found bool, err error)
	authFunctionUsable(ctx context.Context, dbName, role string) (bool, error)
}

// checkAuthQueryPath answers "can pgbouncer run auth_query right now?" from
// what the pooler itself depends on. It returns a human-readable problem
// (empty when the path works) or an error when it could not look (the latter
// counts as a failed check, not as drift).
//
// secretPassword is the pgbouncer-auth Secret's value; secretFound is false
// when the Secret or its key is absent.
func checkAuthQueryPath(ctx context.Context, db authPathDB, cfg pgbouncerAuthConfig, secretPassword string, secretFound bool) (string, error) {
	if !cfg.Enabled() {
		return "", nil
	}
	if cfg.AuthDBName == "" {
		return "pgbouncer.ini sets auth_query without auth_dbname; the lookup would run in each client's database, where " + PgbouncerAuthFunction + " does not exist", nil
	}
	if !strings.Contains(cfg.AuthQuery, strings.TrimSuffix(PgbouncerAuthFunction, "(text)")+"(") {
		return fmt.Sprintf("pgbouncer.ini auth_query does not call %s", PgbouncerAuthFunction), nil
	}
	if !secretFound || secretPassword == "" {
		return fmt.Sprintf("Secret %s/%s has no %s: the pooler has no password for %s (run intake data/pgbouncer-auth)",
			pgbouncerNamespace, pgbouncerAuthSecretName, pgbouncerAuthSecretKey, cfg.AuthUser), nil
	}
	stored, canLogin, found, err := db.authRolePassword(ctx, cfg.AuthUser)
	if err != nil {
		return "", err
	}
	if !found || !canLogin {
		return fmt.Sprintf("role %s does not exist or cannot log in (run scripts/pgbouncer-auth-query.sh migrate)", cfg.AuthUser), nil
	}
	if stored == "" {
		return fmt.Sprintf("role %s has no password (run scripts/pgbouncer-auth-query.sh migrate)", cfg.AuthUser), nil
	}
	match, err := passwordMatchesVerifier(cfg.AuthUser, secretPassword, stored)
	if err != nil {
		return err.Error(), nil
	}
	if !match {
		return fmt.Sprintf("role %s's password does not match Secret %s/%s (a rotation half landed: re-run scripts/pgbouncer-auth-query.sh migrate, then restart pgbouncer)",
			cfg.AuthUser, pgbouncerNamespace, pgbouncerAuthSecretName), nil
	}
	usable, err := db.authFunctionUsable(ctx, cfg.AuthDBName, cfg.AuthUser)
	if err != nil {
		return "", err
	}
	if !usable {
		return fmt.Sprintf("%s is missing in database %s, or %s cannot execute it (run scripts/pgbouncer-auth-query.sh migrate)",
			PgbouncerAuthFunction, cfg.AuthDBName, cfg.AuthUser), nil
	}
	return "", nil
}

// sqlAuthPathDB implements authPathDB over the admin connection.
type sqlAuthPathDB struct {
	adminURL string
	openDB   func(dsn string) (*sql.DB, error)
}

func (s sqlAuthPathDB) authRolePassword(ctx context.Context, role string) (string, bool, bool, error) {
	db, err := s.openDB(s.adminURL)
	if err != nil {
		return "", false, false, fmt.Errorf("connect to admin postgres: %w", err)
	}
	defer func() { _ = db.Close() }()
	var stored sql.NullString
	var canLogin bool
	err = db.QueryRowContext(ctx,
		"SELECT rolpassword, rolcanlogin FROM pg_authid WHERE rolname = $1", role).Scan(&stored, &canLogin)
	if err == sql.ErrNoRows {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, fmt.Errorf("read auth role: %w", err)
	}
	return stored.String, canLogin, true, nil
}

func (s sqlAuthPathDB) authFunctionUsable(ctx context.Context, dbName, role string) (bool, error) {
	dsn, err := withDatabase(s.adminURL, dbName)
	if err != nil {
		return false, err
	}
	db, err := s.openDB(dsn)
	if err != nil {
		return false, fmt.Errorf("connect to auth database %s: %w", dbName, err)
	}
	defer func() { _ = db.Close() }()
	var usable bool
	err = db.QueryRowContext(ctx,
		`SELECT CASE WHEN to_regprocedure($1) IS NULL THEN false
		             ELSE has_function_privilege($2, to_regprocedure($1), 'EXECUTE') END`,
		PgbouncerAuthFunction, role).Scan(&usable)
	if err != nil {
		return false, fmt.Errorf("check %s in %s: %w", PgbouncerAuthFunction, dbName, err)
	}
	return usable, nil
}
