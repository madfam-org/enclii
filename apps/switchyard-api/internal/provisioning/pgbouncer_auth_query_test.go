package provisioning

import (
	"context"
	"crypto/md5" // #nosec G501 -- builds a PostgreSQL md5 verifier fixture
	"database/sql"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	k8scorev1 "k8s.io/api/core/v1"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const testAuthQueryINI = `[pgbouncer]
listen_port = 6432
; auth_type = plain   (a comment, ignored)
auth_type = md5
auth_file = /etc/pgbouncer/userlist.txt
auth_user = pgbouncer_auth
auth_dbname = postgres
auth_query = SELECT usename, passwd FROM pgbouncer.get_auth($1)

[databases]
auth_user = not-this-one
* = host=postgres.data.svc.cluster.local port=5432
`

func TestParsePgbouncerAuthConfig(t *testing.T) {
	got := parsePgbouncerAuthConfig(testAuthQueryINI)
	want := pgbouncerAuthConfig{
		AuthType:   "md5",
		AuthQuery:  "SELECT usename, passwd FROM pgbouncer.get_auth($1)",
		AuthUser:   "pgbouncer_auth",
		AuthDBName: "postgres",
	}
	if got != want {
		t.Fatalf("parsePgbouncerAuthConfig = %+v, want %+v", got, want)
	}
	if !got.Enabled() {
		t.Error("auth_query + auth_user should be Enabled")
	}

	static := parsePgbouncerAuthConfig("[pgbouncer]\nauth_type = plain\nauth_file = /etc/pgbouncer/userlist.txt\n")
	if static.Enabled() || static.AuthType != "plain" {
		t.Errorf("static userlist config parsed as %+v", static)
	}
	if (pgbouncerAuthConfig{AuthQuery: "SELECT 1"}).Enabled() {
		t.Error("auth_query without auth_user is not usable and must not count as Enabled")
	}
}

func TestParseDirectConnectRoles(t *testing.T) {
	got := parseDirectConnectRoles("voxa_app\n# migrators\njanua_migrator  # direct DSN\n\n  phyne \nvoxa_app\n")
	want := []string{"janua_migrator", "phyne", "voxa_app"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDirectConnectRoles = %v, want %v", got, want)
	}
	if parseDirectConnectRoles("") != nil {
		t.Error("empty declaration should yield nil")
	}
}

// w1pRoles is the 2026-10-18 situation (internal-devops W1-P, W1P-5): nine
// login roles without a userlist line, of which only ceq tries the pooler.
func w1pRoles() []LoginRole {
	names := []string{"app_admin", "ceq", "janua_migrator", "phyne", "posthog", "selva_migrator", "telesia", "tlacuilo", "voxa_app"}
	roles := []LoginRole{{Name: "janua", Resolvable: true}, {Name: "postgres", Superuser: true, Resolvable: true}, {Name: "pgbouncer_admin", Resolvable: true}, {Name: "pgbouncer_auth", Resolvable: true}}
	for _, n := range names {
		roles = append(roles, LoginRole{Name: n, Resolvable: true})
	}
	return roles
}

var w1pDirect = []string{"app_admin", "janua_migrator", "phyne", "posthog", "selva_migrator", "telesia", "tlacuilo", "voxa_app"}

var authQueryOn = pgbouncerAuthConfig{
	AuthType:   "md5",
	AuthQuery:  "SELECT usename, passwd FROM pgbouncer.get_auth($1)",
	AuthUser:   "pgbouncer_auth",
	AuthDBName: "postgres",
}

func TestEvaluatePooledAuth(t *testing.T) {
	tests := []struct {
		name        string
		in          pooledAuthInputs
		wantMissing []string
		wantStale   []string
		wantDirect  []string
	}{
		{
			name:        "static userlist, nothing declared: the nine of W1-P",
			in:          pooledAuthInputs{Roles: w1pRoles(), UserlistUsers: []string{"janua"}},
			wantMissing: []string{"app_admin", "ceq", "janua_migrator", "phyne", "posthog", "selva_migrator", "telesia", "tlacuilo", "voxa_app"},
		},
		{
			name:        "static userlist, direct-connect declared: only ceq is real",
			in:          pooledAuthInputs{Roles: w1pRoles(), UserlistUsers: []string{"janua"}, DirectConnect: w1pDirect},
			wantMissing: []string{"ceq"},
			wantDirect:  w1pDirect,
		},
		{
			name:       "auth_query working: ceq resolves, nothing missing",
			in:         pooledAuthInputs{Roles: w1pRoles(), UserlistUsers: []string{"janua"}, DirectConnect: w1pDirect, AuthQuery: authQueryOn},
			wantDirect: w1pDirect,
		},
		{
			name: "auth_query working: a superuser and a role with no usable password still need a line",
			in: pooledAuthInputs{
				Roles: []LoginRole{
					{Name: "ops_super", Superuser: true, Resolvable: true},
					{Name: "expired_svc", Resolvable: false},
					{Name: "fine_svc", Resolvable: true},
					{Name: "listed_super", Superuser: true, Resolvable: true},
				},
				UserlistUsers: []string{"listed_super"},
				AuthQuery:     authQueryOn,
			},
			wantMissing: []string{"expired_svc", "ops_super"},
		},
		{
			name: "auth_query configured but broken: every unlisted role and the auth user",
			in: pooledAuthInputs{
				Roles:            w1pRoles(),
				UserlistUsers:    []string{"janua"},
				DirectConnect:    w1pDirect,
				AuthQuery:        authQueryOn,
				AuthQueryProblem: "role pgbouncer_auth does not exist",
			},
			wantMissing: []string{"ceq", "pgbouncer_auth"},
			wantDirect:  w1pDirect,
		},
		{
			name: "auth_query broken but every pooled role has a line: still non-zero",
			in: pooledAuthInputs{
				Roles:            []LoginRole{{Name: "janua", Resolvable: true}},
				UserlistUsers:    []string{"janua"},
				AuthQuery:        authQueryOn,
				AuthQueryProblem: "Secret data/pgbouncer-auth has no pgbouncer_auth_password",
			},
			wantMissing: []string{"pgbouncer_auth"},
		},
		{
			name: "stale lines are still reported; the auth user and pooler roles never are",
			in: pooledAuthInputs{
				Roles:         []LoginRole{{Name: "janua", Resolvable: true}},
				UserlistUsers: []string{"janua", "retired_svc", "pgbouncer_auth", "pgbouncer_admin"},
				AuthQuery:     authQueryOn,
			},
			wantStale: []string{"retired_svc"},
		},
		{
			name: "a declared role that no longer exists is simply not listed",
			in: pooledAuthInputs{
				Roles:         []LoginRole{{Name: "janua", Resolvable: true}},
				UserlistUsers: []string{"janua"},
				DirectConnect: []string{"gone_role"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluatePooledAuth(tt.in)
			if !reflect.DeepEqual(got.MissingFromUserlist, tt.wantMissing) {
				t.Errorf("MissingFromUserlist = %v, want %v", got.MissingFromUserlist, tt.wantMissing)
			}
			if !reflect.DeepEqual(got.StaleInUserlist, tt.wantStale) {
				t.Errorf("StaleInUserlist = %v, want %v", got.StaleInUserlist, tt.wantStale)
			}
			if !reflect.DeepEqual(got.DirectConnectSkipped, tt.wantDirect) {
				t.Errorf("DirectConnectSkipped = %v, want %v", got.DirectConnectSkipped, tt.wantDirect)
			}
			if got.AuthQuery != tt.in.AuthQuery.Enabled() || got.AuthQueryProblem != tt.in.AuthQueryProblem {
				t.Errorf("auth fields = (%v, %q), want (%v, %q)", got.AuthQuery, got.AuthQueryProblem, tt.in.AuthQuery.Enabled(), tt.in.AuthQueryProblem)
			}
		})
	}
}

func TestPasswordMatchesVerifier(t *testing.T) {
	salt := []byte("0123456789abcdef")
	scram, err := ScramSHA256Verifier("pencil-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", salt, 4096)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum([]byte("pencil" + "legacy_role")) // #nosec G401 -- fixture
	md5Stored := "md5" + hex.EncodeToString(sum[:])

	cases := []struct {
		name, role, password, stored string
		want                         bool
		wantErr                      bool
	}{
		{"scram, right password", "pgbouncer_auth", "pencil-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", scram, true, false},
		{"scram, wrong password", "pgbouncer_auth", "pencil-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", scram, false, false},
		{"md5, right password", "legacy_role", "pencil", md5Stored, true, false},
		{"md5, right password, other role (md5 salts with the name)", "other_role", "pencil", md5Stored, false, false},
		{"scram, malformed", "r", "x", "SCRAM-SHA-256$4096", false, true},
		{"scram, bad iterations", "r", "x", "SCRAM-SHA-256$zero:MDEy$a:b", false, true},
		{"plaintext stored", "r", "x", "x", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := passwordMatchesVerifier(c.role, c.password, c.stored)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("match = %v, want %v", got, c.want)
			}
		})
	}
}

func TestWithDatabase(t *testing.T) {
	got, err := withDatabase("postgres://admin:pw@postgres.data.svc.cluster.local:5432/enclii?sslmode=disable", "postgres")
	if err != nil || got != "postgres://admin:pw@postgres.data.svc.cluster.local:5432/postgres?sslmode=disable" {
		t.Errorf("url form = %q, %v", got, err)
	}
	got, _ = withDatabase("host=h user=u dbname=enclii", "postgres")
	if got != "host=h user=u dbname=enclii dbname=postgres" {
		t.Errorf("key=value form = %q", got)
	}
}

// fakeAuthPathDB answers checkAuthQueryPath's two questions from fields.
type fakeAuthPathDB struct {
	stored   string
	canLogin bool
	found    bool
	usable   bool
	err      error
}

func (f fakeAuthPathDB) authRolePassword(context.Context, string) (string, bool, bool, error) {
	return f.stored, f.canLogin, f.found, f.err
}

func (f fakeAuthPathDB) authFunctionUsable(context.Context, string, string) (bool, error) {
	return f.usable, f.err
}

func TestCheckAuthQueryPath(t *testing.T) {
	const pw = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	stored, err := ScramSHA256Verifier(pw, []byte("0123456789abcdef"), 4096)
	if err != nil {
		t.Fatal(err)
	}
	healthy := fakeAuthPathDB{stored: stored, canLogin: true, found: true, usable: true}
	ctx := context.Background()

	cases := []struct {
		name        string
		db          fakeAuthPathDB
		cfg         pgbouncerAuthConfig
		secret      string
		secretFound bool
		wantProblem string // substring; "" means no problem
		wantErr     bool
	}{
		{"auth_query off: nothing to check", healthy, pgbouncerAuthConfig{AuthType: "plain"}, "", false, "", false},
		{"healthy", healthy, authQueryOn, pw, true, "", false},
		{"no auth_dbname", healthy, pgbouncerAuthConfig{AuthQuery: authQueryOn.AuthQuery, AuthUser: "pgbouncer_auth"}, pw, true, "without auth_dbname", false},
		{"auth_query calls something else", healthy, pgbouncerAuthConfig{AuthQuery: "SELECT usename, passwd FROM pg_shadow WHERE usename=$1", AuthUser: "pgbouncer_auth", AuthDBName: "postgres"}, pw, true, "does not call pgbouncer.get_auth(text)", false},
		{"secret missing", healthy, authQueryOn, "", false, "has no pgbouncer_auth_password", false},
		{"role missing (migration not run)", fakeAuthPathDB{}, authQueryOn, pw, true, "does not exist or cannot log in", false},
		{"role cannot log in", fakeAuthPathDB{stored: stored, found: true}, authQueryOn, pw, true, "does not exist or cannot log in", false},
		{"role has no password", fakeAuthPathDB{found: true, canLogin: true}, authQueryOn, pw, true, "has no password", false},
		{"rotation half landed", healthy, authQueryOn, "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB", true, "does not match Secret data/pgbouncer-auth", false},
		{"function missing or no grant", fakeAuthPathDB{stored: stored, canLogin: true, found: true}, authQueryOn, pw, true, "is missing in database postgres", false},
		{"database unreachable is an error, not drift", fakeAuthPathDB{err: errors.New("connection refused")}, authQueryOn, pw, true, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			problem, err := checkAuthQueryPath(ctx, c.db, c.cfg, c.secret, c.secretFound)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantProblem == "" && problem != "" {
				t.Errorf("problem = %q, want none", problem)
			}
			if c.wantProblem != "" && !strings.Contains(problem, c.wantProblem) {
				t.Errorf("problem = %q, want it to contain %q", problem, c.wantProblem)
			}
			if strings.Contains(problem, c.secret) && c.secret != "" {
				t.Errorf("problem text carries the password: %q", problem)
			}
		})
	}
}

// reconcileFixture builds an updater over a fake clientset and a queue of
// sqlmock connections, one per openDB call, in the order ReconcileUserlist
// opens them: login roles, then (auth_query on) the auth role, then the auth
// database.
func reconcileFixture(t *testing.T, ini, direct, userlist string, authSecret map[string][]byte, dbs ...func(sqlmock.Sqlmock)) (*PgBouncerUpdater, []sqlmock.Sqlmock) {
	t.Helper()
	objs := []runtime.Object{
		&k8scorev1.ConfigMap{
			ObjectMeta: k8smetav1.ObjectMeta{Name: pgbouncerConfigMap, Namespace: pgbouncerNamespace},
			Data:       map[string]string{pgbouncerConfigKey: ini, pgbouncerDirectConnectKey: direct},
		},
		&k8scorev1.Secret{
			ObjectMeta: k8smetav1.ObjectMeta{Name: pgbouncerUserlistName, Namespace: pgbouncerNamespace},
			Data:       map[string][]byte{pgbouncerUserKey: []byte(userlist)},
		},
	}
	if authSecret != nil {
		objs = append(objs, &k8scorev1.Secret{
			ObjectMeta: k8smetav1.ObjectMeta{Name: pgbouncerAuthSecretName, Namespace: pgbouncerNamespace},
			Data:       authSecret,
		})
	}
	u := NewPgBouncerUpdater(fake.NewSimpleClientset(objs...), newTestLogger(t))
	var mocks []sqlmock.Sqlmock
	var conns []*sql.DB
	for _, setup := range dbs {
		db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
		if err != nil {
			t.Fatal(err)
		}
		setup(mock)
		mock.ExpectClose()
		mocks = append(mocks, mock)
		conns = append(conns, db)
	}
	next := 0
	u.openDB = func(string) (*sql.DB, error) {
		if next >= len(conns) {
			return nil, errors.New("unexpected extra database connection")
		}
		db := conns[next]
		next++
		return db, nil
	}
	return u, mocks
}

func loginRolesRows(mock sqlmock.Sqlmock, roles ...LoginRole) {
	mock.ExpectPing()
	rows := sqlmock.NewRows([]string{"rolname", "rolsuper", "resolvable"})
	for _, r := range roles {
		rows.AddRow(r.Name, r.Superuser, r.Resolvable)
	}
	mock.ExpectQuery("SELECT rolname, rolsuper").WillReturnRows(rows)
}

func TestReconcileUserlistStaticModeHonoursDirectConnect(t *testing.T) {
	u, mocks := reconcileFixture(t,
		"[pgbouncer]\nauth_type = plain\nauth_file = /etc/pgbouncer/userlist.txt\n",
		"voxa_app\n",
		"\"janua\" \"x\"\n",
		nil,
		func(m sqlmock.Sqlmock) {
			loginRolesRows(m, LoginRole{Name: "janua", Resolvable: true}, LoginRole{Name: "ceq", Resolvable: true}, LoginRole{Name: "voxa_app", Resolvable: true})
		})
	drift, err := u.ReconcileUserlist(context.Background(), "postgres://admin@db/enclii")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(drift.MissingFromUserlist, []string{"ceq"}) || !reflect.DeepEqual(drift.DirectConnectSkipped, []string{"voxa_app"}) || drift.AuthQuery {
		t.Errorf("drift = %+v", drift)
	}
	for _, m := range mocks {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	}
}

func TestReconcileUserlistAuthQueryHealthy(t *testing.T) {
	const pw = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	stored, _ := ScramSHA256Verifier(pw, []byte("0123456789abcdef"), 4096)
	u, mocks := reconcileFixture(t, testAuthQueryINI, "", "\"janua\" \"x\"\n",
		map[string][]byte{pgbouncerAuthSecretKey: []byte(pw + "\n")},
		func(m sqlmock.Sqlmock) {
			loginRolesRows(m, LoginRole{Name: "janua", Resolvable: true}, LoginRole{Name: "ceq", Resolvable: true}, LoginRole{Name: "pgbouncer_auth", Resolvable: true})
		},
		func(m sqlmock.Sqlmock) {
			m.ExpectQuery("SELECT rolpassword, rolcanlogin FROM pg_authid").WithArgs("pgbouncer_auth").
				WillReturnRows(sqlmock.NewRows([]string{"rolpassword", "rolcanlogin"}).AddRow(stored, true))
		},
		func(m sqlmock.Sqlmock) {
			m.ExpectQuery("to_regprocedure").WithArgs(PgbouncerAuthFunction, "pgbouncer_auth").
				WillReturnRows(sqlmock.NewRows([]string{"usable"}).AddRow(true))
		})
	drift, err := u.ReconcileUserlist(context.Background(), "postgres://admin@db/enclii")
	if err != nil {
		t.Fatal(err)
	}
	if drift.HasMissing() || !drift.AuthQuery || drift.AuthQueryProblem != "" || drift.AuthUser != "pgbouncer_auth" {
		t.Errorf("drift = %+v, want auth_query on, healthy, nothing missing", drift)
	}
	for _, m := range mocks {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
	}
}

func TestReconcileUserlistAuthQueryWithoutSecret(t *testing.T) {
	// The config PR landed but intake has not run: no pgbouncer-auth Secret.
	// No database call is made for the auth path, and the result is never zero.
	u, _ := reconcileFixture(t, testAuthQueryINI, "", "\"janua\" \"x\"\n", nil,
		func(m sqlmock.Sqlmock) {
			loginRolesRows(m, LoginRole{Name: "janua", Resolvable: true}, LoginRole{Name: "ceq", Resolvable: true})
		})
	drift, err := u.ReconcileUserlist(context.Background(), "postgres://admin@db/enclii")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(drift.MissingFromUserlist, []string{"ceq", "pgbouncer_auth"}) || !strings.Contains(drift.AuthQueryProblem, "pgbouncer-auth") {
		t.Errorf("drift = %+v", drift)
	}
}

func TestReconcileUserlistNeedsTheConfigMap(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	loginRolesRows(mock, LoginRole{Name: "janua", Resolvable: true})
	mock.ExpectClose()
	u := NewPgBouncerUpdater(fake.NewSimpleClientset(), newTestLogger(t))
	u.openDB = func(string) (*sql.DB, error) { return db, nil }
	if _, err := u.ReconcileUserlist(context.Background(), "postgres://admin@db/enclii"); err == nil {
		t.Fatal("a missing pgbouncer-config ConfigMap must be an error (a failed check), not a quiet result")
	}
}
