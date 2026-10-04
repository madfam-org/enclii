//go:build integration

package provisioning

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// TestGeneratedRolesAgainstPostgres runs against a disposable Postgres given
// by TEST_POSTGRES_ADMIN_URL (a superuser URL). It proves that a role created
// from a SCRAM verifier accepts the plaintext login, that the app role has the
// runtime posture, and that a re-run without a password leaves it untouched.
func TestGeneratedRolesAgainstPostgres(t *testing.T) {
	admin := os.Getenv("TEST_POSTGRES_ADMIN_URL")
	if admin == "" {
		t.Skip("TEST_POSTGRES_ADMIN_URL not set")
	}
	ctx := context.Background()
	p := NewPostgresProvisioner(admin, newTestLogger(t))

	ownerPw, _ := GeneratePassword()
	if err := p.ProvisionGeneratedOwner(ctx, &types.PostgresProvisionSpec{DatabaseName: "itest_db", GeneratePassword: true, ConnectionLimit: 4}, ownerPw); err != nil {
		t.Fatalf("owner: %v", err)
	}
	appPw, _ := GeneratePassword()
	action, err := p.EnsureAppRole(ctx, AppRole{RoleName: "itest_db_app", DatabaseName: "itest_db", ConnectionLimit: 3}, appPw)
	if err != nil || action != AppRoleCreated {
		t.Fatalf("app role: %q %v", action, err)
	}
	login := func(role, pw string) error {
		u, _ := url.Parse(admin)
		u.User = url.UserPassword(role, pw)
		u.Path = "/itest_db"
		db, err := sql.Open("postgres", u.String())
		if err != nil {
			return err
		}
		defer db.Close()
		return db.PingContext(ctx)
	}
	if err := login("itest_db", ownerPw); err != nil {
		t.Fatalf("owner login with generated password: %v", err)
	}
	if err := login("itest_db_app", appPw); err != nil {
		t.Fatalf("app login with generated password: %v", err)
	}
	if login("itest_db_app", "wrong-password") == nil {
		t.Fatalf("wrong password accepted")
	}

	db, _ := sql.Open("postgres", admin)
	defer db.Close()
	var super, bypass, inherit, createdb, createrole, repl bool
	var limit int
	err = db.QueryRowContext(ctx, `SELECT rolsuper, rolbypassrls, rolinherit, rolcreatedb, rolcreaterole, rolreplication, rolconnlimit
		FROM pg_roles WHERE rolname='itest_db_app'`).Scan(&super, &bypass, &inherit, &createdb, &createrole, &repl, &limit)
	if err != nil || super || bypass || inherit || createdb || createrole || repl || limit != 3 {
		t.Fatalf("posture wrong: err=%v super=%v bypass=%v inherit=%v limit=%d", err, super, bypass, inherit, limit)
	}
	var ownerLimit int
	_ = db.QueryRowContext(ctx, `SELECT rolconnlimit FROM pg_roles WHERE rolname='itest_db'`).Scan(&ownerLimit)
	if ownerLimit != 4 {
		t.Fatalf("owner connection limit %d", ownerLimit)
	}
	var canConnect bool
	_ = db.QueryRowContext(ctx, `SELECT has_database_privilege('itest_db_app','itest_db','CONNECT')`).Scan(&canConnect)
	if !canConnect {
		t.Fatalf("CONNECT not granted")
	}

	// Re-run without a password: kept, and the old password still works.
	action, err = p.EnsureAppRole(ctx, AppRole{RoleName: "itest_db_app", DatabaseName: "itest_db", ConnectionLimit: 3}, "")
	if err != nil || action != AppRoleKept || login("itest_db_app", appPw) != nil {
		t.Fatalf("re-run did not keep the role: %q %v", action, err)
	}
	// Rotation: new password works, old one does not.
	newPw, _ := GeneratePassword()
	if action, err = p.EnsureAppRole(ctx, AppRole{RoleName: "itest_db_app", DatabaseName: "itest_db", ConnectionLimit: 3}, newPw); err != nil || action != AppRoleRotated {
		t.Fatalf("rotate: %q %v", action, err)
	}
	if login("itest_db_app", newPw) != nil || login("itest_db_app", appPw) == nil {
		t.Fatalf("rotation did not take effect")
	}
	// The owner may not be used as a runtime role.
	if _, err := p.EnsureAppRole(ctx, AppRole{RoleName: "itest_db_owner", DatabaseName: "itest_db", ConnectionLimit: 3}, newPw); err != nil {
		t.Fatalf("second app role: %v", err)
	}
}
