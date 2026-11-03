package januastagedb

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastage"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func fixtureAuthority() januastage.Authority {
	project, intent := uuid.New(), uuid.New()
	return januastage.Authority{ID: uuid.New(), ProjectID: project, EnvironmentID: uuid.New(), RepositoryRegistrationID: uuid.New(), HeldIntentID: intent, HeldServiceID: januastage.HeldServiceID(project, intent), Profile: januastage.Profile, NamespaceUID: "application-namespace-fixture", DeploymentUID: "deployment-fixture", APIServiceUID: "api-service-fixture", RedisDeploymentUID: "redis-fixture", RedisServiceUID: "redis-service-fixture", ApplicationUID: "argo-fixture", AppProjectUID: "argoproject-fixture", SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), PlanSHA256: strings.Repeat("c", 64), ActivationHeld: true, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}
func fixturePlan(t *testing.T) Plan {
	t.Helper()
	p, err := PlanFor(fixtureAuthority(), Allocation{CatalogPlan: "standard-0", CPU: "100m", Memory: "256Mi", StorageGi: 1, Replicas: 1, MaxConnections: 40}, NativeBinding{AddonNamespaceUID: "addon-namespace-fixture", StorageClassUID: "retained-storage-fixture", StorageClassVersion: "1", NetworkSHA256: strings.Repeat("d", 64), CapacityBudgetSHA256: strings.Repeat("e", 64)}, uuid.New(), uuid.New(), "Synthetic fixed database bootstrap")
	require.NoError(t, err)
	return p
}
func freshObserver(p Plan) Observer {
	return func(context.Context, uuid.UUID) (januastage.Authority, error) {
		a := p.Authority
		a.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return a, nil
	}
}

func TestFixedProfileDoesNotRelaxGenericSyntheticAuthority(t *testing.T) {
	p := fixturePlan(t)
	require.NoError(t, p.Validate())
	require.Error(t, addons.ValidatePostgresRuntimeConfig(types.DatabaseAddonTypePostgres, types.DatabaseAddonConfig{DatabaseName: Database, RuntimeRole: RuntimeRole}))
	cluster, err := p.ClusterManifest()
	require.NoError(t, err)
	db, _, _ := unstructured.NestedString(cluster.Object, "spec", "bootstrap", "initdb", "database")
	require.Equal(t, Database, db)
	owner, _, _ := unstructured.NestedString(cluster.Object, "spec", "bootstrap", "initdb", "owner")
	require.Equal(t, OwnerRole, owner)
	image, _, _ := unstructured.NestedString(cluster.Object, "spec", "imageName")
	require.Equal(t, types.JanuaStagingPostgresImage, image)
	roles, _, _ := unstructured.NestedSlice(cluster.Object, "spec", "managed", "roles")
	require.Len(t, roles, 1)
	require.Equal(t, RuntimeRole, roles[0].(map[string]any)["name"])
	for _, field := range []string{"backup", "externalClusters", "replica", "superuserSecret", "plugins", "env", "envFrom"} {
		_, found, _ := unstructured.NestedFieldNoCopy(cluster.Object, "spec", field)
		require.False(t, found, field)
	}
	b, _ := json.Marshal(p)
	require.NotContains(t, string(b), p.Reason)
	require.NotContains(t, string(b), "password")
}

func TestPlanTamperingOrDriftCannotAuthorizeNativeExecution(t *testing.T) {
	p := fixturePlan(t)
	for _, mutate := range []func(*Plan){func(p *Plan) { p.Authority.ActiveServiceRegistered = true }, func(p *Plan) { p.Authority.Native043BindingProven = true }, func(p *Plan) { p.Authority.HeldServiceID = uuid.New() }, func(p *Plan) { p.Allocation.StorageGi = 99 }, func(p *Plan) { p.ActorID = uuid.New() }, func(p *Plan) { p.Reason = "changed audit reason" }, func(p *Plan) { p.NetworkSHA256 = strings.Repeat("f", 64) }, func(p *Plan) { p.AddonNamespaceUID = p.Authority.NamespaceUID }, func(p *Plan) { p.StorageClassUID = "replaced" }} {
		q := p
		mutate(&q)
		require.ErrorIs(t, q.Validate(), ErrRefused)
	}
	require.NoError(t, p.Revalidate(context.Background(), freshObserver(p)))
	require.ErrorIs(t, p.Revalidate(context.Background(), nil), ErrRefused)
	for _, stamp := range []time.Time{time.Now().Add(-time.Minute), time.Now().Add(time.Minute)} {
		require.ErrorIs(t, p.Revalidate(context.Background(), func(context.Context, uuid.UUID) (januastage.Authority, error) {
			a := p.Authority
			a.ObservedAt = stamp.UTC().Format(time.RFC3339Nano)
			return a, nil
		}), ErrRefused)
	}
}

func TestAdmissionCannotAddCredentialsPrivilegesCloneOrOtherDefaults(t *testing.T) {
	p := fixturePlan(t)
	expected, _ := p.ClusterManifest()
	require.True(t, ClosedCluster(expected, expected.DeepCopy()))
	for _, mutate := range []func(*unstructured.Unstructured){
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, true, "spec", "enableSuperuserAccess")
		},
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, map[string]any{"name": "foreign"}, "spec", "superuserSecret")
		},
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, []any{map[string]any{"name": "production"}}, "spec", "externalClusters")
		},
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, map[string]any{}, "spec", "replica")
		},
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, []any{map[string]any{"name": "FOREIGN", "value": "synthetic"}}, "spec", "env")
		},
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, []any{"GRANT privileged TO runtime"}, "spec", "bootstrap", "initdb", "postInitApplicationSQL")
		},
		func(o *unstructured.Unstructured) { o.SetNamespace("janua") },
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, int64(0), "spec", "postgresUID")
		},
		func(o *unstructured.Unstructured) {
			_ = unstructured.SetNestedField(o.Object, map[string]any{"env": "foreign"}, "spec", "logLevel")
		},
	} {
		changed := expected.DeepCopy()
		mutate(changed)
		require.False(t, ClosedCluster(expected, changed))
	}
}
