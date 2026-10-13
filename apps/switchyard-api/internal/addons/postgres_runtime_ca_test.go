package addons

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

func syntheticCA(t *testing.T, ca bool, until time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic runtime proof CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: until, IsCA: ca, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func installRuntimeCAFixture(t *testing.T, p *PostgresProvisioner, r *ProvisionRequest, resource string) {
	t.Helper()
	uid := types.UID("owned-synthetic-cluster")
	object := p.buildClusterManifest(r, resource)
	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &object)) // API-wire JSON numeric types
	cluster := &unstructured.Unstructured{Object: object}
	cluster.SetUID(uid)
	object["status"] = map[string]interface{}{
		"phase": "Cluster in healthy state", "instances": int64(1), "readyInstances": int64(1),
		"certificates":       map[string]interface{}{"serverCASecret": resource + "-ca"},
		"managedRolesStatus": map[string]interface{}{"byStatus": map[string]interface{}{"reconciled": []interface{}{r.Addon.Config.RuntimeRole}}},
	}
	p.k8sClient.DynamicClient = dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), cluster)
	require.NoError(t, p.ownRuntimeSecret(context.Background(), r, resource, uid))
	_, err = p.k8sClient.Kube().CoreV1().Secrets(r.Namespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: resource + "-ca", Namespace: r.Namespace, OwnerReferences: []metav1.OwnerReference{{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: resource, UID: uid}}},
		Data:       map[string][]byte{"ca.crt": syntheticCA(t, true, time.Now().Add(time.Hour)), "ca.key": []byte("private-key-never-returned")},
	}, metav1.CreateOptions{})
	require.NoError(t, err)
}

func TestPublicCABundleRejectsKeysExpiredAndNonCATrust(t *testing.T) {
	valid := syntheticCA(t, true, time.Now().Add(time.Hour))
	_, err := publicCABundle(valid, time.Now())
	require.NoError(t, err)
	for _, raw := range [][]byte{nil, []byte(strings.Repeat("x", 32769)), []byte("private-key-never-returned"),
		append(append([]byte{}, valid...), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("fixture")})...),
		syntheticCA(t, false, time.Now().Add(time.Hour)), syntheticCA(t, true, time.Now().Add(-time.Minute))} {
		_, err = publicCABundle(raw, time.Now())
		require.Error(t, err)
	}
}

func TestRuntimeCredentialsReturnOnlyOwnedPublicCAAndRejectForeignSelector(t *testing.T) {
	ctx := context.Background()
	r := isolatedReq()
	resource := "pg-training-fixture1"
	r.Addon.ProjectID, r.Addon.K8sNamespace, r.Addon.K8sResourceName, r.Addon.ConnectionSecret = r.ProjectID, r.Namespace, resource, runtimeSecretName(resource)
	p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kubefake.NewSimpleClientset()}}
	require.NoError(t, p.ensureRuntimeSecret(ctx, r, resource))
	installRuntimeCAFixture(t, p, r, resource)
	credentials, err := p.GetCredentials(ctx, r.Addon)
	require.NoError(t, err)
	require.Contains(t, credentials.CACertificate, "BEGIN CERTIFICATE")
	require.NotContains(t, credentials.CACertificate, "private-key-never-returned")
	cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Get(ctx, resource, metav1.GetOptions{})
	require.NoError(t, err)
	require.NoError(t, unstructured.SetNestedField(cluster.Object, "other-addon-ca", "status", "certificates", "serverCASecret"))
	_, err = p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Update(ctx, cluster, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = p.GetCredentials(ctx, r.Addon)
	require.Error(t, err)
	require.NoError(t, unstructured.SetNestedField(cluster.Object, resource+"-ca", "status", "certificates", "serverCASecret"))
	_, err = p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Update(ctx, cluster, metav1.UpdateOptions{})
	require.NoError(t, err)
	ca, err := p.k8sClient.Kube().CoreV1().Secrets(r.Namespace).Get(ctx, resource+"-ca", metav1.GetOptions{})
	require.NoError(t, err)
	ca.OwnerReferences[0].UID = "foreign-cluster"
	_, err = p.k8sClient.Kube().CoreV1().Secrets(r.Namespace).Update(ctx, ca, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = p.GetCredentials(ctx, r.Addon)
	require.Error(t, err)
}

func TestCachedReadyCannotIssueUnreconciledRuntimeCredentials(t *testing.T) {
	for _, scenario := range []string{"missing role", "different role", "reconciliation error", "failed cluster", "no ready instance", "reconciled"} {
		t.Run(scenario, func(t *testing.T) {
			r := isolatedReq()
			r.Addon.Status = "ready"
			resource := "pg-training-fixture1"
			r.Addon.ProjectID, r.Addon.K8sNamespace, r.Addon.K8sResourceName, r.Addon.ConnectionSecret = r.ProjectID, r.Namespace, resource, runtimeSecretName(resource)
			kube := kubefake.NewSimpleClientset()
			p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: kube}}
			require.NoError(t, p.ensureRuntimeSecret(t.Context(), r, resource))
			installRuntimeCAFixture(t, p, r, resource)
			cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Get(t.Context(), resource, metav1.GetOptions{})
			require.NoError(t, err)
			require.NoError(t, unstructured.SetNestedField(cluster.Object, "Cluster in healthy state", "status", "phase"))
			require.NoError(t, unstructured.SetNestedField(cluster.Object, int64(1), "status", "instances"))
			require.NoError(t, unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances"))
			if scenario != "missing role" {
				require.NoError(t, unstructured.SetNestedStringSlice(cluster.Object, []string{r.Addon.Config.RuntimeRole}, "status", "managedRolesStatus", "byStatus", "reconciled"))
			} else {
				unstructured.RemoveNestedField(cluster.Object, "status", "managedRolesStatus")
			}
			if scenario == "different role" {
				require.NoError(t, unstructured.SetNestedStringSlice(cluster.Object, []string{"other_runtime"}, "status", "managedRolesStatus", "byStatus", "reconciled"))
			} else if scenario == "reconciliation error" {
				require.NoError(t, unstructured.SetNestedSlice(cluster.Object, []interface{}{"fixture failure"}, "status", "managedRolesStatus", "cannotReconcile", r.Addon.Config.RuntimeRole))
			} else if scenario == "failed cluster" {
				require.NoError(t, unstructured.SetNestedField(cluster.Object, "Failed", "status", "phase"))
			} else if scenario == "no ready instance" {
				require.NoError(t, unstructured.SetNestedField(cluster.Object, int64(0), "status", "readyInstances"))
			}
			_, err = p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Update(t.Context(), cluster, metav1.UpdateOptions{})
			require.NoError(t, err)
			kube.ClearActions()
			credentials, err := p.GetCredentials(t.Context(), r.Addon)
			if scenario == "reconciled" {
				require.NoError(t, err)
				require.Equal(t, r.Addon.Config.DatabaseName, credentials.DatabaseName)
				require.Equal(t, r.Addon.Config.RuntimeRole, credentials.Username)
				require.Contains(t, credentials.CACertificate, "BEGIN CERTIFICATE")
				require.NotContains(t, credentials.ConnectionURI, runtimeMigrator)
				return
			}
			require.Error(t, err)
			require.Nil(t, credentials)
			require.Len(t, kube.Actions(), 1, "readiness refusal stops before CA Secret lookup")
			require.Equal(t, resource+"-runtime", kube.Actions()[0].(interface{ GetName() string }).GetName())
			_, err = p.GetConnectionURI(t.Context(), r.Addon)
			require.Error(t, err, "binding credential URI must share the readiness guard")
		})
	}
}
