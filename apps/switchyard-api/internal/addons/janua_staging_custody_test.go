package addons

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestJanuaCustodyRequiresOwnedReadyBirthAndProjectsOnlyRuntimeAndPublicCA(t *testing.T) {
	f := newJanuaProvisionFixture(t)
	birth, err := f.run(false)
	require.NoError(t, err)
	f.addon.K8sNamespace = "project-" + f.addon.ProjectID.String()[:8]
	f.addon.K8sResourceName = JanuaStagingClusterName
	f.addon.ConnectionSecret = JanuaStagingRuntimeSecretName
	cluster, err := f.dynamic.Resource(cnpgGVR).Namespace(f.addon.K8sNamespace).Get(context.Background(), JanuaStagingClusterName, metav1.GetOptions{})
	require.NoError(t, err)
	cluster.Object["status"] = map[string]interface{}{"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1), "certificates": map[string]interface{}{"serverCASecret": JanuaStagingClusterName + "-ca"}, "managedRolesStatus": map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{JanuaStagingRuntime}}}}
	_, err = f.dynamic.Resource(cnpgGVR).Namespace(f.addon.K8sNamespace).Update(context.Background(), cluster, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = f.kube.CoreV1().Secrets(f.addon.K8sNamespace).Create(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: JanuaStagingClusterName + "-ca", Namespace: f.addon.K8sNamespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: JanuaStagingClusterName, UID: birth.ClusterUID}}}, Data: map[string][]byte{"ca.crt": syntheticCA(t, true, time.Now().Add(time.Hour)), "ca.key": []byte("private-key-never-returned")}}, metav1.CreateOptions{})
	require.NoError(t, err)
	// The public generic parser/credential profile stays synthetic-only.
	_, err = ParsePostgresClusterStatus(cluster, f.addon)
	require.Error(t, err)
	custody, err := f.service.ReadJanuaStagingCustody(context.Background(), f.addon, f.operation, f.journalUID, 100, januaFixturePeers(), januaFixtureOperator(), func(context.Context) error { return nil })
	require.NoError(t, err)
	require.Len(t, custody.Data, 2)
	require.NotContains(t, custody.Data["database_ca"], "private-key-never-returned")
	uri, err := url.Parse(custody.Data["database_url"].(string))
	require.NoError(t, err)
	require.Equal(t, JanuaStagingRuntime, uri.User.Username())
	require.Equal(t, "/"+JanuaStagingDatabase, uri.Path)
	require.Equal(t, "verify-full", uri.Query().Get("sslmode"))
	require.Equal(t, JanuaStagingCAFile, uri.Query().Get("sslrootcert"))
	// Replacing the same-name runtime Secret cannot be adopted by custody.
	secret, err := f.kube.CoreV1().Secrets(f.addon.K8sNamespace).Get(context.Background(), JanuaStagingRuntimeSecretName, metav1.GetOptions{})
	require.NoError(t, err)
	secret.UID = "replacement-runtime"
	_, err = f.kube.CoreV1().Secrets(f.addon.K8sNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = f.service.ReadJanuaStagingCustody(context.Background(), f.addon, f.operation, f.journalUID, 100, januaFixturePeers(), januaFixtureOperator(), func(context.Context) error { return nil })
	require.Error(t, err)
}

func januaFixturePeers() []KarafielControlPeer {
	return []KarafielControlPeer{{HostRoute: "192.0.2.1/32", Port: 443}}
}
func januaFixtureOperator() metav1.LabelSelector {
	return metav1.LabelSelector{MatchLabels: map[string]string{"app": "fixture-cnpg"}}
}
