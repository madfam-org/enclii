package addons

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
)

// Only an explicitly owned, disposable loopback PostgreSQL fixture. This is
// not an operator path and never accepts a cluster/production database URL.
func TestRuntimeBootstrapOnNativePostgres(t *testing.T) {
	raw := os.Getenv("ENCLII_RUNTIME_POSTGRES_PROOF_URL")
	if raw == "" {
		t.Skip("requires the owned ephemeral PostgreSQL runtime proof")
	}
	u, err := url.Parse(raw)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/postgres", u.Path)
	require.Equal(t, "postgres", u.User.Username())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", raw)
	require.NoError(t, err)
	defer admin.Close()
	_, err = admin.ExecContext(ctx, `CREATE ROLE enclii_migrator LOGIN PASSWORD 'synthetic-proof' NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT`)
	require.NoError(t, err) // Existing global roles mean the fixture is not fresh.
	_, err = admin.ExecContext(ctx, `CREATE DATABASE enclii_runtime_proof_sim OWNER enclii_migrator`)
	require.NoError(t, err)
	u.Path = "/enclii_runtime_proof_sim"
	bootstrap, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer bootstrap.Close()
	config := sdktypes.DatabaseAddonConfig{DatabaseName: "enclii_runtime_proof_sim", RuntimeRole: "training_runtime"}
	for _, statement := range runtimeBootstrapSQL(config) {
		_, err = bootstrap.ExecContext(ctx, statement.(string))
		require.NoError(t, err)
	}
	_, err = bootstrap.ExecContext(ctx, `ALTER ROLE training_runtime LOGIN PASSWORD 'synthetic-proof'`)
	require.NoError(t, err) // Local stand-in for managed.roles applying its Secret.
	u.User = url.UserPassword(runtimeMigrator, "synthetic-proof")
	setup, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer setup.Close()
	_, err = setup.ExecContext(ctx, `CREATE TABLE runtime_clinical_proof(id integer PRIMARY KEY, body text NOT NULL);
	 CREATE TABLE _prisma_migrations(id text PRIMARY KEY);
	 ALTER TABLE runtime_clinical_proof ENABLE ROW LEVEL SECURITY;
	 ALTER TABLE runtime_clinical_proof FORCE ROW LEVEL SECURITY;
	 CREATE POLICY member_proof ON runtime_clinical_proof USING (current_setting('app.member_id',true)='synthetic-member')
	 WITH CHECK (current_setting('app.member_id',true)='synthetic-member');
	 REVOKE INSERT,UPDATE,DELETE ON _prisma_migrations FROM training_runtime`)
	require.NoError(t, err)
	u.User = url.UserPassword(config.RuntimeRole, "synthetic-proof")
	runtime, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer runtime.Close()
	var bad bool
	err = runtime.QueryRowContext(ctx, `SELECT r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication
	 OR pg_has_role(current_user,d.datdba,'MEMBER') OR has_database_privilege(current_user,current_database(),'CREATE')
	 OR has_database_privilege(current_user,current_database(),'TEMP') OR has_schema_privilege(current_user,'public','CREATE')
	 OR EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid)
	 FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user`).Scan(&bad)
	require.NoError(t, err)
	require.False(t, bad)
	for _, statement := range []string{
		`CREATE TABLE denied_ddl(id integer)`, `CREATE TEMP TABLE denied_temp(id integer)`,
		`ALTER TABLE runtime_clinical_proof DISABLE ROW LEVEL SECURITY`, `SET ROLE enclii_migrator`,
		`INSERT INTO _prisma_migrations(id) VALUES('denied')`,
		`INSERT INTO runtime_clinical_proof(id,body) VALUES(1,'denied-without-member')`,
	} {
		_, err = runtime.ExecContext(ctx, statement)
		require.Error(t, err, "runtime may not execute: %s", statement)
	}
	tx, err := runtime.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT set_config('app.member_id','synthetic-member',true)`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO runtime_clinical_proof(id,body) VALUES(1,'synthetic-native-role')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	var rows int
	require.NoError(t, runtime.QueryRowContext(ctx, `SELECT count(*) FROM runtime_clinical_proof`).Scan(&rows))
	require.Equal(t, 0, rows)
}
