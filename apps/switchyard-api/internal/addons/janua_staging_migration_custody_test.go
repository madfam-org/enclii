package addons

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestJanuaMigrationOwnerIsClosedAndNeverProjected(t *testing.T) {
	f := newJanuaProvisionFixture(t)
	birth, e := f.run(false)
	require.NoError(t, e)
	f.addon.K8sNamespace = "project-" + f.addon.ProjectID.String()[:8]
	f.addon.K8sResourceName = JanuaStagingClusterName
	f.addon.ConnectionSecret = JanuaStagingRuntimeSecretName
	ctx := context.Background()
	cluster, e := f.dynamic.Resource(cnpgGVR).Namespace(f.addon.K8sNamespace).Get(ctx, JanuaStagingClusterName, metav1.GetOptions{})
	require.NoError(t, e)
	cluster.Object["status"] = map[string]interface{}{"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1), "certificates": map[string]interface{}{"serverCASecret": JanuaStagingClusterName + "-ca"}, "managedRolesStatus": map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{JanuaStagingRuntime}}}}
	_, e = f.dynamic.Resource(cnpgGVR).Namespace(f.addon.K8sNamespace).Update(ctx, cluster, metav1.UpdateOptions{})
	require.NoError(t, e)
	refs := []metav1.OwnerReference{{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: JanuaStagingClusterName, UID: birth.ClusterUID}}
	_, e = f.kube.CoreV1().Secrets(f.addon.K8sNamespace).Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: JanuaStagingClusterName + "-ca", Namespace: f.addon.K8sNamespace, OwnerReferences: refs}, Data: map[string][]byte{"ca.crt": syntheticCA(t, true, time.Now().Add(time.Hour))}}, metav1.CreateOptions{})
	require.NoError(t, e)
	password := strings.Repeat("a", 64)
	owner := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: JanuaStagingClusterName + "-app", Namespace: f.addon.K8sNamespace, UID: "owner-fixture", ResourceVersion: "1", OwnerReferences: refs}, Type: corev1.SecretTypeBasicAuth, Data: map[string][]byte{"username": []byte("enclii_migrator"), "password": []byte(password), "host": []byte(JanuaStagingClusterName + "-rw"), "port": []byte("5432"), "dbname": []byte(JanuaStagingDatabase)}}
	_, e = f.kube.CoreV1().Secrets(f.addon.K8sNamespace).Create(ctx, owner, metav1.CreateOptions{})
	require.NoError(t, e)
	read := func() (*JanuaStagingMigrationCustody, error) {
		return f.service.ReadJanuaStagingMigrationCustody(ctx, f.addon, f.operation, f.journalUID, 100, januaFixturePeers(), januaFixtureOperator(), func(context.Context) error { return nil })
	}
	got, e := read()
	require.NoError(t, e)
	uri, e := url.Parse(got.OwnerURI)
	require.NoError(t, e)
	require.Equal(t, "enclii_migrator", uri.User.Username())
	require.Equal(t, "verify-full", uri.Query().Get("sslmode"))
	require.NotEqual(t, got.OwnerURI, got.RuntimeURI)
	body, e := json.Marshal(got)
	require.NoError(t, e)
	require.NotContains(t, string(body), password)
	require.NotContains(t, string(body), "postgresql")
	require.NotContains(t, string(body), "BEGIN CERTIFICATE")
	owner.Data["username"] = []byte("postgres")
	_, e = f.kube.CoreV1().Secrets(f.addon.K8sNamespace).Update(ctx, owner, metav1.UpdateOptions{})
	require.NoError(t, e)
	_, e = read()
	require.Error(t, e)
	require.Error(t, ValidatePostgresRuntimeConfig(f.addon.Type, f.addon.Config), "generic _sim profile must stay closed")
}
