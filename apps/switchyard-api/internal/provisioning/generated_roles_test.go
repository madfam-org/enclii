package provisioning

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	k8scorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestGenerateSecretValue(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		v, err := GenerateSecretValue(32)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 43 || strings.ContainsAny(v, "+/=") {
			t.Fatalf("not unpadded base64url of 32 bytes (len %d)", len(v))
		}
		if ValidateSecretValue("k", v) != nil {
			t.Fatalf("generated value tripped the placeholder blocklist")
		}
		if seen[v] {
			t.Fatalf("duplicate generated value")
		}
		seen[v] = true
	}
	for _, n := range []int{15, 129} {
		if _, err := GenerateSecretValue(n); err == nil {
			t.Errorf("size %d accepted", n)
		}
	}
}

// Shape and determinism. Acceptance by a real server is proven by the
// integration test (generated_roles_integration_test.go), which logs in with
// the plaintext against a role created from this verifier.
func TestScramSHA256VerifierShape(t *testing.T) {
	salt := []byte("0123456789abcdef")
	a, err := ScramSHA256Verifier("pencil", salt, 4096)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := ScramSHA256Verifier("pencil", salt, 4096)
	c, _ := ScramSHA256Verifier("pencil2", salt, 4096)
	if a != b || a == c {
		t.Fatalf("verifier not deterministic per input")
	}
	if !regexp.MustCompile(`^SCRAM-SHA-256\$4096:MDEyMzQ1Njc4OWFiY2RlZg==\$[A-Za-z0-9+/]{43}=:[A-Za-z0-9+/]{43}=$`).MatchString(a) {
		t.Fatalf("unexpected verifier shape %s", a)
	}
	if strings.Contains(a, "pencil") {
		t.Fatalf("verifier carries the plaintext")
	}
}

func TestPooledConnectionURLAndPasswordRoundTrip(t *testing.T) {
	u := PooledConnectionURL("pravara_app", "abc_DEF-123", "pravara")
	if u != "postgresql://pravara_app:abc_DEF-123@pgbouncer.data.svc.cluster.local:6432/pravara" {
		t.Fatalf("unexpected URL shape: %s", u)
	}
	pw, err := PasswordFromConnectionURL(u, "pravara_app")
	if err != nil || pw != "abc_DEF-123" {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, err := PasswordFromConnectionURL(u, "other_app"); err == nil {
		t.Fatalf("a URL for another role was accepted")
	}
}

func TestValidateAppRoleName(t *testing.T) {
	ok := [][3]string{{"pravara_app", "pravara", "pravara"}, {"fabrication_prep_app", "fabrication_prep", ""}}
	for _, c := range ok {
		if err := ValidateAppRoleName(c[0], c[1], c[2]); err != nil {
			t.Errorf("%v: %v", c, err)
		}
	}
	bad := [][3]string{{"janua", "pravara", ""}, {"pravara_", "pravara", ""}, {"Pravara_app", "pravara", ""},
		{"pravara_app", "pravara", "pravara_app"}, {"postgres", "postgres", ""}, {"pg_app", "pg", ""}, {"pravara_app;drop", "pravara", ""}}
	for _, c := range bad {
		if err := ValidateAppRoleName(c[0], c[1], c[2]); err == nil {
			t.Errorf("%v accepted", c)
		}
	}
	if ValidateConnectionLimit(0, "l") == nil || ValidateConnectionLimit(21, "l") == nil || ValidateConnectionLimit(8, "l") != nil {
		t.Errorf("connection limit bounds wrong")
	}
}

func TestEnsureUserUpsert(t *testing.T) {
	cs := fake.NewSimpleClientset(&k8scorev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: pgbouncerUserlistName, Namespace: pgbouncerNamespace},
		Data:       map[string][]byte{pgbouncerUserKey: []byte("\"a\" \"pa\"\n\"b_app\" \"old\"\n\"c\" \"pc\"\n")},
	})
	u := NewPgBouncerUpdater(cs, newTestLogger(t))
	ctx := context.Background()
	read := func() string {
		s, _ := cs.CoreV1().Secrets(pgbouncerNamespace).Get(ctx, pgbouncerUserlistName, metav1.GetOptions{})
		return string(s.Data[pgbouncerUserKey])
	}
	if changed, _ := u.EnsureUser(ctx, "b_app", "new", false); changed || !strings.Contains(read(), "\"b_app\" \"old\"") {
		t.Fatalf("replace=false changed an existing line")
	}
	if changed, _ := u.EnsureUser(ctx, "b_app", "new", true); !changed || read() != "\"a\" \"pa\"\n\"b_app\" \"new\"\n\"c\" \"pc\"\n" {
		t.Fatalf("replace=true did not rewrite in place: %q", read())
	}
	if changed, _ := u.EnsureUser(ctx, "d_app", "pd", false); !changed || !strings.HasSuffix(read(), "\"d_app\" \"pd\"\n") {
		t.Fatalf("missing line not appended")
	}
	if has, _ := u.HasUser(ctx, "d_app"); !has {
		t.Fatalf("HasUser false after append")
	}
	if _, err := u.EnsureUser(ctx, "e_app", "bad\"pw", true); err == nil {
		t.Fatalf("a quote in the password was accepted")
	}
}

func newMockProvisioner(t *testing.T) (*PostgresProvisioner, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatal(err)
	}
	p := NewPostgresProvisioner("postgres://unused", newTestLogger(t))
	p.openDB = func(string) (*sql.DB, error) { return db, nil }
	mock.ExpectPing()
	return p, mock
}

func TestEnsureAppRoleCreatesWithPostureAndVerifier(t *testing.T) {
	p, mock := newMockProvisioner(t)
	mock.ExpectQuery("SELECT pg_get_userbyid").WithArgs("pravara").WillReturnRows(sqlmock.NewRows([]string{"o"}).AddRow("pravara"))
	mock.ExpectQuery("SELECT rolsuper, rolbypassrls").WithArgs("pravara_app").WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("CREATE ROLE pravara_app LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 6 PASSWORD 'SCRAM-SHA-256$4096:")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("GRANT CONNECT ON DATABASE pravara TO pravara_app")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectClose()

	action, err := p.EnsureAppRole(context.Background(), AppRole{RoleName: "pravara_app", DatabaseName: "pravara", ConnectionLimit: 6}, "generated-plaintext-value")
	if err != nil || action != AppRoleCreated {
		t.Fatalf("action %q err %v", action, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureAppRoleKeepsExistingWithoutAlter(t *testing.T) {
	p, mock := newMockProvisioner(t)
	mock.ExpectQuery("SELECT pg_get_userbyid").WithArgs("pravara").WillReturnRows(sqlmock.NewRows([]string{"o"}).AddRow("pravara"))
	mock.ExpectQuery("SELECT rolsuper, rolbypassrls").WithArgs("pravara_app").WillReturnRows(sqlmock.NewRows([]string{"s", "b"}).AddRow(false, false))
	mock.ExpectExec(regexp.QuoteMeta("GRANT CONNECT ON DATABASE pravara TO pravara_app")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectClose()
	action, err := p.EnsureAppRole(context.Background(), AppRole{RoleName: "pravara_app", DatabaseName: "pravara", ConnectionLimit: 6}, "")
	if err != nil || action != AppRoleKept {
		t.Fatalf("action %q err %v", action, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureAppRoleRefusesBypassingRoleAndOwner(t *testing.T) {
	p, mock := newMockProvisioner(t)
	mock.ExpectQuery("SELECT pg_get_userbyid").WithArgs("pravara").WillReturnRows(sqlmock.NewRows([]string{"o"}).AddRow("pravara"))
	mock.ExpectQuery("SELECT rolsuper, rolbypassrls").WithArgs("pravara_app").WillReturnRows(sqlmock.NewRows([]string{"s", "b"}).AddRow(false, true))
	mock.ExpectClose()
	if _, err := p.EnsureAppRole(context.Background(), AppRole{RoleName: "pravara_app", DatabaseName: "pravara", ConnectionLimit: 6}, "x-generated"); err == nil ||
		!strings.Contains(err.Error(), "BYPASSRLS") {
		t.Fatalf("want BYPASSRLS refusal, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	p2, mock2 := newMockProvisioner(t)
	mock2.ExpectQuery("SELECT pg_get_userbyid").WithArgs("pravara").WillReturnRows(sqlmock.NewRows([]string{"o"}).AddRow("pravara_app"))
	mock2.ExpectClose()
	if _, err := p2.EnsureAppRole(context.Background(), AppRole{RoleName: "pravara_app", DatabaseName: "pravara", ConnectionLimit: 6}, "x-generated"); err == nil ||
		!strings.Contains(err.Error(), "owns database") {
		t.Fatalf("want owner refusal, got %v", err)
	}
}

func TestGeneratedOwnerSendsVerifierNotPlaintext(t *testing.T) {
	p, mock := newMockProvisioner(t)
	mock.ExpectQuery("SELECT EXISTS").WithArgs("fabrication_prep").WillReturnRows(sqlmock.NewRows([]string{"e"}).AddRow(false))
	mock.ExpectExec("CREATE ROLE fabrication_prep WITH LOGIN").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`ALTER ROLE fabrication_prep WITH PASSWORD 'SCRAM-SHA-256\$4096:`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT EXISTS").WithArgs("fabrication_prep").WillReturnRows(sqlmock.NewRows([]string{"e"}).AddRow(true))
	mock.ExpectExec("GRANT ALL PRIVILEGES").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectClose()
	spec := &types.PostgresProvisionSpec{DatabaseName: "fabrication_prep", GeneratePassword: true}
	if err := p.ProvisionGeneratedOwner(context.Background(), spec, "plaintext-should-not-be-sent"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newTestLogger(t *testing.T) logging.Logger {
	t.Helper()
	l, err := logging.NewStructuredLogger(&logging.LogConfig{Level: "panic", Format: "text", Output: "stderr"})
	if err != nil {
		t.Fatal(err)
	}
	return l
}
