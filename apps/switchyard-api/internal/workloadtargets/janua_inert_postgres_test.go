//go:build workload_target_postgres

package workloadtargets

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/stagingintents"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// Real SQL, synthetic source/native objects only. No Kubernetes API is called.
// This is included by the required native PG18/TLS proof's TestNativeTarget filter.
func nativeJanuaInertPlanFixture(t *testing.T) (*sql.DB, januastage.Plan, uuid.UUID) {
	t.Helper()
	owner := nativeConnection(t, "ENCLII_TARGET_OWNER_PROOF_URL", "binding_owner")
	ctx := context.Background()
	schemaName := "inert_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := owner.ExecContext(ctx, "CREATE SCHEMA "+schemaName)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = owner.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") })
	u, err := url.Parse(os.Getenv("ENCLII_TARGET_OWNER_PROOF_URL"))
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	database, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.ExecContext(ctx, `CREATE TABLE projects(id uuid PRIMARY KEY,slug text);
CREATE TABLE environments(id uuid PRIMARY KEY,project_id uuid,name text,kube_namespace text);
CREATE TABLE onboarding_registrations(id uuid PRIMARY KEY,project_id uuid,repo_full_name text,onboard_status text);
CREATE TABLE users(id uuid PRIMARY KEY,is_platform_admin boolean,active boolean);
CREATE TABLE services(id uuid PRIMARY KEY,name text,k8s_namespace text);`)
	require.NoError(t, err)
	for _, file := range []string{"045_janua_staging_preregistration.up.sql", "046_janua_staging_inert_materialization.up.sql"} {
		body, err := os.ReadFile("../db/migrations/" + file)
		require.NoError(t, err)
		_, err = database.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	project, environment, registration, actor, intentID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, row := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO projects VALUES($1,'janua')`, []any{project}},
		{`INSERT INTO environments VALUES($1,$2,'staging','janua-staging')`, []any{environment, project}},
		{`INSERT INTO onboarding_registrations VALUES($1,$2,'madfam-org/janua','completed')`, []any{registration, project}},
		{`INSERT INTO users VALUES($1,true,true)`, []any{actor}},
	} {
		_, err = database.ExecContext(ctx, row.sql, row.args...)
		require.NoError(t, err)
	}
	intentStore := stagingintents.Store{DB: database, ProjectID: project}
	intent, err := intentStore.Plan(ctx, actor, intentID)
	require.NoError(t, err)
	intent, err = intentStore.Record(ctx, actor, intentID, intent.PlanSHA256, "synthetic held intent")
	require.NoError(t, err)
	var files []checks.ManifestFile
	for _, name := range []string{"api.yaml", "service.yaml", "secrets.yaml", "redis.yaml", "kustomization.yaml"} {
		body, err := os.ReadFile("../januastage/testdata/v1/" + name)
		require.NoError(t, err)
		files = append(files, checks.ManifestFile{Path: name, Content: string(body)})
	}
	plan, err := januastage.BuildPlan(uuid.New(), intent, strings.Repeat("a", 40), "namespace-fixture", "baseline-fixture", files)
	require.NoError(t, err)
	store := januastage.Store{DB: database}
	require.NoError(t, store.Prepare(ctx, actor, "synthetic inert objects", plan.SHA256, plan))
	require.NoError(t, store.Prepare(ctx, actor, "synthetic inert objects", plan.SHA256, plan))
	return database, plan, actor
}

type existingInertNative struct {
	object  *unstructured.Unstructured
	creates int
}

func (n *existingInertNative) Guard(context.Context, januastage.Plan) error { return nil }
func (n *existingInertNative) Get(context.Context, januastage.Object) (*unstructured.Unstructured, error) {
	return n.object.DeepCopy(), nil
}
func (n *existingInertNative) Create(context.Context, januastage.Object) (*unstructured.Unstructured, error) {
	n.creates++
	return nil, errors.New("must not create after unconfirmed native incarnation")
}

func TestNativeTargetJanuaInertPartialRollbackCannotAdoptRecreatedUID(t *testing.T) {
	database, plan, actor := nativeJanuaInertPlanFixture(t)
	ctx := context.Background()
	store := januastage.Store{DB: database}
	first := plan.Objects[0]
	prior := first.Body.DeepCopy()
	prior.SetUID("original-synthetic-native-uid")
	session, err := store.Begin(ctx, actor, "synthetic inert objects", plan.ID)
	require.NoError(t, err)
	require.NoError(t, session.RecordObject(ctx, first, prior))
	session.Close() // The partial SQL transaction rolled back; only intent persists.
	replacement := prior.DeepCopy()
	replacement.SetUID("replacement-synthetic-native-uid") // Same spec/op labels, different incarnation.
	native := &existingInertNative{object: replacement}
	session, err = store.Begin(ctx, actor, "synthetic inert objects", plan.ID)
	require.NoError(t, err)
	defer session.Close()
	_, err = januastage.Materialize(ctx, plan, native, session)
	require.ErrorIs(t, err, januastage.ErrUnknown)
	require.Zero(t, native.creates)
	uid, _, err := session.RetainedObject(ctx, first)
	require.NoError(t, err)
	require.Empty(t, uid, "an unconfirmed object must never become retained UID authority")
}

func TestNativeTargetJanuaInertRefusesOwnershipReassignment(t *testing.T) {
	for _, table := range []string{"environments", "onboarding_registrations"} {
		t.Run(table, func(t *testing.T) {
			database, plan, actor := nativeJanuaInertPlanFixture(t)
			ctx := context.Background()
			_, err := database.ExecContext(ctx, "UPDATE "+table+" SET project_id=$1", uuid.New())
			require.NoError(t, err)
			store := januastage.Store{DB: database}
			require.ErrorIs(t, store.Prepare(ctx, actor, "synthetic inert objects", plan.SHA256, plan), januastage.ErrRefused)
			_, err = store.Begin(ctx, actor, "synthetic inert objects", plan.ID)
			require.ErrorIs(t, err, januastage.ErrRefused)
		})
	}
}

func nativeJanuaInertAuthorityFixture(t *testing.T) (*sql.DB, januastage.Authority, uuid.UUID) {
	t.Helper()
	database, plan, actor := nativeJanuaInertPlanFixture(t)
	ctx := context.Background()
	store := januastage.Store{DB: database}
	session, err := store.Begin(ctx, actor, "synthetic inert objects", plan.ID)
	require.NoError(t, err)
	defer session.Close()
	for _, object := range plan.Objects {
		live := object.Body.DeepCopy()
		live.SetUID(types.UID(uuid.NewString()))
		require.NoError(t, session.RecordObject(ctx, object, live))
	}
	// A prepared or partial operation alone never becomes a completed authority.
	session.Close()
	_, _, err = store.Read(ctx, plan.ID)
	require.Error(t, err)
	session, err = store.Begin(ctx, actor, "synthetic inert objects", plan.ID)
	require.NoError(t, err)
	defer session.Close()
	uids := map[string]string{}
	for _, object := range plan.Objects {
		live := object.Body.DeepCopy()
		uid := uuid.NewString()
		live.SetUID(types.UID(uid))
		uids[object.Kind+"/"+object.Name] = uid
		require.NoError(t, session.RecordObject(ctx, object, live))
	}
	authority := januastage.Authority{ID: plan.ID, ProjectID: plan.ProjectID, EnvironmentID: plan.EnvironmentID,
		RepositoryRegistrationID: plan.RepositoryRegistrationID, HeldIntentID: plan.HeldIntentID, HeldServiceID: plan.HeldServiceID,
		Profile: januastage.Profile, NamespaceUID: plan.NamespaceUID,
		DeploymentUID: uids["Deployment/janua-api-staging"], APIServiceUID: uids["Service/janua-api-staging"],
		RedisDeploymentUID: uids["Deployment/janua-staging-redis"], RedisServiceUID: uids["Service/janua-staging-redis"],
		ApplicationUID: uids["Application/janua-staging"], AppProjectUID: uids["AppProject/janua-staging"],
		SourceRevision: plan.SourceRevision, SourceSHA256: plan.SourceSHA256, PlanSHA256: plan.SHA256,
		ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	require.NoError(t, session.Complete(ctx, authority))
	_, retained, err := store.Read(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, authority, retained)
	return database, authority, actor
}

func TestNativeTargetJanuaInertJournalRetainsHistoryOutsideServices(t *testing.T) {
	database, authority, _ := nativeJanuaInertAuthorityFixture(t)
	ctx := context.Background()
	var active int
	require.NoError(t, database.QueryRowContext(ctx, `SELECT count(*) FROM services`).Scan(&active))
	require.Zero(t, active)
	_, err := database.ExecContext(ctx, `UPDATE janua_staging_inert_operations SET reason='rewrite' WHERE id=$1`, authority.ID)
	nativeState(t, err, "P0001")
	_, err = database.ExecContext(ctx, `TRUNCATE janua_staging_inert_operations CASCADE`)
	nativeState(t, err, "P0001")
	down, err := os.ReadFile("../db/migrations/046_janua_staging_inert_materialization.down.sql")
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, string(down))
	nativeState(t, err, "P0001")
	require.NoError(t, database.QueryRowContext(ctx, `SELECT count(*) FROM janua_staging_inert_operations`).Scan(&active))
	require.Equal(t, 1, active)
}
