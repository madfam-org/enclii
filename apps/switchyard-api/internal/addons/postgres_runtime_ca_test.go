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
	object["status"] = map[string]interface{}{"certificates": map[string]interface{}{"serverCASecret": resource + "-ca"}}
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
