//go:build workload_target_postgres

package workloadtargets

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/stagingintents"
	"github.com/stretchr/testify/require"
)

// Actual PostgreSQL journal/trigger proof, synthetic retained native records.
// The required native PG18/TLS lane includes this TestNativeTarget filter.
// It never contacts Kubernetes or establishes live staging authority.
func TestNativeTargetJanuaDatabaseJournalRequiresOrderedExactNativeUIDs(t *testing.T) {
	owner := nativeConnection(t, "ENCLII_TARGET_OWNER_PROOF_URL", "binding_owner")
	ctx := context.Background()
	schema := "stage_db_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := owner.ExecContext(ctx, "CREATE SCHEMA "+schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = owner.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	u, err := url.Parse(os.Getenv("ENCLII_TARGET_OWNER_PROOF_URL"))
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE projects(id uuid PRIMARY KEY,slug text);
CREATE TABLE environments(id uuid PRIMARY KEY,project_id uuid,name text,kube_namespace text);
CREATE TABLE onboarding_registrations(id uuid PRIMARY KEY,project_id uuid,repo_full_name text,onboard_status text);
CREATE TABLE users(id uuid PRIMARY KEY,is_platform_admin boolean,active boolean);
CREATE TABLE services(id uuid PRIMARY KEY);
CREATE TABLE managed_db_plans(code text PRIMARY KEY,available boolean,engine text,cpu_request text,memory_request text,storage_gb integer,replica_count integer,max_connections integer,price_cents_month bigint);
CREATE TABLE database_addons(id uuid PRIMARY KEY,environment_id uuid,k8s_namespace text,deleted_at timestamptz);
INSERT INTO managed_db_plans VALUES('standard-0',true,'postgres','100m','256Mi',1,1,10,0);`)
	require.NoError(t, err)
	for _, file := range []string{"045_janua_staging_preregistration.up.sql", "046_janua_staging_inert_materialization.up.sql", "048_janua_staging_database_operations.up.sql"} {
		body, e := os.ReadFile("../db/migrations/" + file)
		require.NoError(t, e)
		_, e = db.ExecContext(ctx, string(body))
		require.NoError(t, e)
	}
	project, environment, registration, actor, intent := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO projects VALUES($1,'janua')`, []any{project}},
		{`INSERT INTO environments VALUES($1,$2,'staging','janua-staging')`, []any{environment, project}},
		{`INSERT INTO onboarding_registrations VALUES($1,$2,'madfam-org/janua','completed')`, []any{registration, project}},
		{`INSERT INTO users VALUES($1,true,true)`, []any{actor}},
	} {
		_, err = db.ExecContext(ctx, row.sql, row.args...)
		require.NoError(t, err)
	}
	store := stagingintents.Store{DB: db, ProjectID: project}
	held, err := store.Plan(ctx, actor, intent)
	require.NoError(t, err)
	_, err = store.Record(ctx, actor, intent, held.PlanSHA256, "synthetic staging intent")
	require.NoError(t, err)
	a := januastage.Authority{ID: uuid.New(), ProjectID: project, EnvironmentID: environment, RepositoryRegistrationID: registration, HeldIntentID: intent, HeldServiceID: januastage.HeldServiceID(project, intent), Profile: januastage.Profile, NamespaceUID: "synthetic-app-namespace", DeploymentUID: "synthetic-api", APIServiceUID: "synthetic-api-service", RedisDeploymentUID: "synthetic-redis", RedisServiceUID: "synthetic-redis-service", ApplicationUID: "synthetic-argo-app", AppProjectUID: "synthetic-argo-project", SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), PlanSHA256: strings.Repeat("c", 64), ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	require.NoError(t, januastage.ValidateAuthorityShape(a))
	payload, err := json.Marshal(a)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO janua_staging_inert_operations(id,held_intent_id,project_id,environment_id,repository_registration_id,actor_id,held_service_id,profile,plan_sha256,request_sha256,reason,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,'synthetic native receipts',$10)`, a.ID, intent, project, environment, registration, actor, a.HeldServiceID, a.Profile, a.PlanSHA256, payload)
	require.NoError(t, err)
	objects := map[string]string{"AppProject:argocd/janua-staging": a.AppProjectUID, "Application:argocd/janua-staging": a.ApplicationUID, "Deployment:janua-staging/janua-api-staging": a.DeploymentUID, "Deployment:janua-staging/janua-staging-redis": a.RedisDeploymentUID, "Service:janua-staging/janua-api-staging": a.APIServiceUID, "Service:janua-staging/janua-staging-redis": a.RedisServiceUID}
	for key, uid := range objects {
		_, err = db.ExecContext(ctx, `INSERT INTO janua_staging_inert_objects(operation_id,object_key,uid,object_sha256,payload) VALUES($1,$2,$3,$4,'{}'::jsonb)`, a.ID, key, uid, strings.Repeat("d", 64))
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO janua_staging_inert_authorities(id,held_service_id,project_id,environment_id,repository_registration_id,payload) VALUES($1,$2,$3,$4,$5,$6)`, a.ID, a.HeldServiceID, project, environment, registration, payload)
	require.NoError(t, err)
	p, err := januastagedb.PlanFor(a, januastagedb.Allocation{CatalogPlan: "standard-0", CPU: "100m", Memory: "256Mi", StorageGi: 1, Replicas: 1, MaxConnections: 10}, januastagedb.NativeBinding{AddonNamespaceUID: "synthetic-addon-namespace", StorageClassUID: "synthetic-storage", StorageClassVersion: "1", NetworkSHA256: strings.Repeat("e", 64), CapacityBudgetSHA256: strings.Repeat("f", 64)}, uuid.New(), actor, "synthetic database operation")
	require.NoError(t, err)
	journal := januastagedb.SQLJournal{DB: db}
	require.NoError(t, journal.Check(ctx, p))
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- journal.Claim(ctx, p) }()
	}
	close(start)
	wg.Wait()
	close(results)
	successes, refused := 0, 0
	for outcome := range results {
		if outcome == nil {
			successes++
		} else {
			require.ErrorIs(t, outcome, januastagedb.ErrRefused)
			refused++
		}
	}

	require.Equal(t, 1, successes)
	require.Equal(t, 1, refused)
	_, err = journal.Receipt(ctx, p)
	require.ErrorIs(t, err, januastagedb.ErrRefused)
	require.ErrorIs(t, journal.Record(ctx, p, "CLUSTER_CREATED", "synthetic-cluster"), januastagedb.ErrRefused)
	require.NoError(t, journal.Record(ctx, p, "RUNTIME_SECRET_CREATED", "synthetic-runtime-secret"))
	require.ErrorIs(t, journal.Record(ctx, p, "CLUSTER_CREATED", "synthetic-runtime-secret"), januastagedb.ErrRefused)
	require.NoError(t, journal.Record(ctx, p, "CLUSTER_CREATED", "synthetic-cluster"))
	require.ErrorIs(t, journal.Record(ctx, p, "CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED", "recreated-cluster"), januastagedb.ErrRefused)
	require.NoError(t, journal.Record(ctx, p, "CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED", "synthetic-cluster"))
	receipt, err := journal.Receipt(ctx, p)
	require.NoError(t, err)
	require.EqualValues(t, "synthetic-cluster", receipt.ClusterUID)
	require.EqualValues(t, "synthetic-runtime-secret", receipt.RuntimeSecretUID)

	_, err = db.ExecContext(ctx, `UPDATE managed_db_plans SET price_cents_month=1 WHERE code='standard-0'`)
	require.NoError(t, err)
	require.ErrorIs(t, journal.Check(ctx, p), januastagedb.ErrRefused)
	_, err = db.ExecContext(ctx, `UPDATE managed_db_plans SET price_cents_month=0 WHERE code='standard-0'`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO services(id) VALUES($1)`, a.HeldServiceID)
	require.NoError(t, err)
	require.ErrorIs(t, journal.Check(ctx, p), januastagedb.ErrRefused)
	_, err = db.ExecContext(ctx, `DELETE FROM services WHERE id=$1`, a.HeldServiceID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO database_addons VALUES($1,$2,'synthetic-other-namespace',NULL)`, uuid.New(), environment)
	require.NoError(t, err)
	require.ErrorIs(t, journal.Check(ctx, p), januastagedb.ErrRefused)
	_, err = db.ExecContext(ctx, `DELETE FROM database_addons`)
	require.NoError(t, err)
	require.NoError(t, journal.Check(ctx, p))
	_, err = db.ExecContext(ctx, `UPDATE users SET is_platform_admin=false WHERE id=$1`, actor)
	require.NoError(t, err)
	require.ErrorIs(t, journal.Check(ctx, p), januastagedb.ErrRefused)
	_, err = journal.Receipt(ctx, p)
	require.ErrorIs(t, err, januastagedb.ErrRefused)
	for _, statement := range []string{`UPDATE janua_staging_database_operations SET reason='rewrite'`, `DELETE FROM janua_staging_database_objects`, `TRUNCATE janua_staging_database_operations CASCADE`} {
		_, err = db.ExecContext(ctx, statement)
		nativeState(t, err, "P0001")
	}
	down, err := os.ReadFile("../db/migrations/048_janua_staging_database_operations.down.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(down))
	nativeState(t, err, "P0001")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM janua_staging_database_operations`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM services`).Scan(&count))
	require.Zero(t, count)
}
