package addons

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func isolatedReq() *ProvisionRequest {
	return &ProvisionRequest{Addon: &sdktypes.DatabaseAddon{
		ID: uuid.New(), Type: sdktypes.DatabaseAddonTypePostgres, Name: "training",
		Config: sdktypes.DatabaseAddonConfig{DatabaseName: "training_sim", RuntimeRole: "training_runtime"},
	}, ProjectID: uuid.New(), Namespace: "project-fixture1"}
}

func TestRuntimeProfileRejectsPartialReservedAndInjectedIdentifiers(t *testing.T) {
	valid := isolatedReq().Addon.Config
	require.NoError(t, ValidatePostgresRuntimeConfig(sdktypes.DatabaseAddonTypePostgres, valid))
	require.NoError(t, ValidatePostgresRuntimeConfig(sdktypes.DatabaseAddonTypeRedis, sdktypes.DatabaseAddonConfig{}))
	require.Error(t, ValidatePostgresRuntimeConfig(sdktypes.DatabaseAddonTypeRedis, valid))
	for _, db := range []string{"", "training", "training_sim;DROP DATABASE other", "../training_sim", strings.Repeat("a", 64) + "_sim"} {
		c := valid
		c.DatabaseName = db
		require.Error(t, ValidatePostgresRuntimeConfig(sdktypes.DatabaseAddonTypePostgres, c))
	}
	for _, role := range []string{"", "postgres", "app", runtimeMigrator, "pg_monitor", "cnpg_pooler", `runtime"; GRANT ALL`, "Runtime"} {
		c := valid
		c.RuntimeRole = role
		require.Error(t, ValidatePostgresRuntimeConfig(sdktypes.DatabaseAddonTypePostgres, c))
	}
}

func TestRuntimeManifestSeparatesMigrationOwnerAndUsesSecretNotInlinePassword(t *testing.T) {
	p := &PostgresProvisioner{}
	r := isolatedReq()
	m := p.buildClusterManifest(r, "pg-training-fixture1")
	require.NotNil(t, m)
	spec := m["spec"].(map[string]interface{})
	bootstrap := spec["bootstrap"].(map[string]interface{})["initdb"].(map[string]interface{})
	require.Equal(t, "training_sim", bootstrap["database"])
	require.Equal(t, runtimeMigrator, bootstrap["owner"])
	role := spec["managed"].(map[string]interface{})["roles"].([]interface{})[0].(map[string]interface{})
	for _, k := range []string{"superuser", "bypassrls", "createdb", "createrole", "inherit", "replication"} {
		require.Equal(t, false, role[k])
	}
	require.Equal(t, true, role["login"])
	require.Equal(t, "pg-training-fixture1-runtime", role["passwordSecret"].(map[string]interface{})["name"])
	serialized, err := json.Marshal(m)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "PASSWORD '")
	require.NotContains(t, string(serialized), "SUPERUSER LOGIN")
	require.NotContains(t, string(serialized), "inRoles")
	bad := isolatedReq()
	bad.Addon.Config.RuntimeRole = "app"
	require.Nil(t, p.buildClusterManifest(bad, "pg-training-fixture1"))
}

func TestLegacyManifestKeepsOriginalDatabaseOwnerAndNoManagedRuntime(t *testing.T) {
	r := isolatedReq()
	r.Addon.Config = sdktypes.DatabaseAddonConfig{}
	spec := (&PostgresProvisioner{}).buildClusterManifest(r, "pg-training-fixture1")["spec"].(map[string]interface{})
	b := spec["bootstrap"].(map[string]interface{})["initdb"].(map[string]interface{})
	require.Equal(t, DefaultDatabase, b["database"])
	require.Equal(t, DefaultUser, b["owner"])
	require.Nil(t, spec["managed"])
	require.Nil(t, b["postInitApplicationSQL"])
}

func TestHealthyPodsDoNotMeanRuntimeRoleReady(t *testing.T) {
	r := isolatedReq()
	r.Addon.K8sNamespace = r.Namespace
	status := map[string]interface{}{"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1), "writeService": "pg-training-fixture1-rw"}
	cluster := &unstructured.Unstructured{Object: map[string]interface{}{"status": status}}
	p := &PostgresProvisioner{}
	s, err := p.parseClusterStatus(cluster, r.Addon)
	require.NoError(t, err)
	require.False(t, s.Ready)
	status["managedRolesStatus"] = map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{"training_runtime"}}}
	s, err = p.parseClusterStatus(cluster, r.Addon)
	require.NoError(t, err)
	require.True(t, s.Ready)
	require.Equal(t, "training_runtime", s.Username)
	require.Equal(t, "training_sim", s.DatabaseName)
	status["managedRolesStatus"].(map[string]interface{})["cannotReconcile"] = map[string]interface{}{"training_runtime": []interface{}{"synthetic failure"}}
	s, err = p.parseClusterStatus(cluster, r.Addon)
	require.NoError(t, err)
	require.False(t, s.Ready)
}

func TestRuntimeSecretRetriesPreserveCredentialAndRejectAnotherOwner(t *testing.T) {
	ctx := context.Background()
	k := fake.NewSimpleClientset()
	p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: k}}
	r := isolatedReq()
	resource := "pg-training-fixture1"
	require.NoError(t, p.ensureRuntimeSecret(ctx, r, resource))
	s, err := k.CoreV1().Secrets(r.Namespace).Get(ctx, runtimeSecretName(resource), metav1.GetOptions{})
	require.NoError(t, err)
	before := string(s.Data["password"])
	require.True(t, runtimePassword.MatchString(before))
	require.NoError(t, p.ensureRuntimeSecret(ctx, r, resource))
	again, err := k.CoreV1().Secrets(r.Namespace).Get(ctx, s.Name, metav1.GetOptions{})
	require.NoError(t, err)
	require.True(t, before == string(again.Data["password"]), "retry must not rotate the runtime credential")
	require.NoError(t, p.ownRuntimeSecret(ctx, r, resource, types.UID("fixture-cluster-uid")))
	require.Error(t, p.ownRuntimeSecret(ctx, r, resource, types.UID("different-cluster-uid")))
	s.Labels[LabelProjectID] = uuid.NewString()
	_, err = k.CoreV1().Secrets(r.Namespace).Update(ctx, s, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Error(t, p.ensureRuntimeSecret(ctx, r, resource))
}

func TestBindingCredentialsCannotSubstituteOwnerOrUnvalidatedURI(t *testing.T) {
	ctx := context.Background()
	k := fake.NewSimpleClientset()
	p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: k}}
	r := isolatedReq()
	resource := "pg-training-fixture1"
	r.Addon.ProjectID, r.Addon.K8sNamespace, r.Addon.K8sResourceName = r.ProjectID, r.Namespace, resource
	r.Addon.ConnectionSecret = runtimeSecretName(resource)
	require.NoError(t, p.ensureRuntimeSecret(ctx, r, resource))
	installRuntimeCAFixture(t, p, r, resource)
	s, err := k.CoreV1().Secrets(r.Namespace).Get(ctx, r.Addon.ConnectionSecret, metav1.GetOptions{})
	require.NoError(t, err)
	s.Data["uri"] = []byte("postgresql://owner:fixture@other.invalid/other")
	_, err = k.CoreV1().Secrets(r.Namespace).Update(ctx, s, metav1.UpdateOptions{})
	require.NoError(t, err)
	u, err := p.GetConnectionURI(ctx, r.Addon)
	require.NoError(t, err)
	parsed, err := url.Parse(u)
	require.NoError(t, err)
	require.Equal(t, "training_runtime", parsed.User.Username())
	require.Equal(t, "/training_sim", parsed.Path)
	require.Equal(t, resource+"-rw."+r.Namespace+".svc.cluster.local", parsed.Hostname())
	r.Addon.ConnectionSecret = resource + "-app"
	_, err = p.GetConnectionURI(ctx, r.Addon)
	require.Error(t, err)
}
