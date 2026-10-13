package addons

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestInitialBootstrapCredentialsAreOwnerOnlyAndNeverSerializable(t *testing.T) {
	for _, name := range []string{"owned", "foreign owner UID", "foreign namespace", "wrong owner role", "owner URI spoof", "not ready", "cached Ready actual Failed", "legacy addon"} {
		t.Run(name, func(t *testing.T) {
			r := isolatedReq()
			r.Namespace = "project-" + r.ProjectID.String()[:8]
			resource := "pg-fixture-bootstrap"
			r.Addon.ProjectID, r.Addon.K8sNamespace, r.Addon.K8sResourceName, r.Addon.ConnectionSecret = r.ProjectID, r.Namespace, resource, runtimeSecretName(resource)
			r.Addon.Status = sdktypes.DatabaseAddonStatusReady
			client := kubefake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: "namespace-fixture", Labels: map[string]string{k8s.LabelProjectNamespace: r.ProjectID.String(), "enclii.dev/verify-signatures": "true"}}})
			p := &PostgresProvisioner{k8sClient: &k8s.Client{KubeClient: client}}
			require.NoError(t, p.ensureRuntimeSecret(t.Context(), r, resource))
			installRuntimeCAFixture(t, p, r, resource)
			cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Get(t.Context(), resource, metav1.GetOptions{})
			require.NoError(t, err)
			require.NoError(t, unstructured.SetNestedField(cluster.Object, "Cluster in healthy state", "status", "phase"))
			require.NoError(t, unstructured.SetNestedField(cluster.Object, int64(1), "status", "instances"))
			require.NoError(t, unstructured.SetNestedField(cluster.Object, int64(1), "status", "readyInstances"))
			require.NoError(t, unstructured.SetNestedStringSlice(cluster.Object, []string{r.Addon.Config.RuntimeRole}, "status", "managedRolesStatus", "byStatus", "reconciled"))
			if name == "cached Ready actual Failed" {
				require.NoError(t, unstructured.SetNestedField(cluster.Object, "Failed", "status", "phase"))
			}
			_, err = p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Update(t.Context(), cluster, metav1.UpdateOptions{})
			require.NoError(t, err)
			owner := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: resource + "-app", Namespace: r.Namespace, UID: "owner-fixture", OwnerReferences: []metav1.OwnerReference{{APIVersion: CloudNativePGAPIVersion, Kind: CloudNativePGKind, Name: resource, UID: "owned-synthetic-cluster"}}}, Type: corev1.SecretTypeBasicAuth,
				Data: map[string][]byte{"username": []byte(runtimeMigrator), "password": []byte("synthetic@long/pass:fixture"), "dbname": []byte(r.Addon.Config.DatabaseName), "host": []byte(resource + "-rw." + r.Namespace + ".svc.cluster.local"), "port": []byte("5432"), "uri": []byte("spoofed-stored-uri-must-not-be-used")}}
			if name == "foreign owner UID" {
				owner.OwnerReferences[0].UID = "foreign-cluster"
			}
			if name == "wrong owner role" {
				owner.Data["username"] = []byte("postgres")
			}
			_, err = client.CoreV1().Secrets(r.Namespace).Create(t.Context(), owner, metav1.CreateOptions{})
			require.NoError(t, err)
			if name == "foreign namespace" {
				ns, _ := client.CoreV1().Namespaces().Get(t.Context(), r.Namespace, metav1.GetOptions{})
				ns.Labels[k8s.LabelProjectNamespace] = "foreign"
				_, err = client.CoreV1().Namespaces().Update(t.Context(), ns, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			if name == "not ready" {
				r.Addon.Status = sdktypes.DatabaseAddonStatusProvisioning
			}
			if name == "legacy addon" {
				r.Addon.Config.DatabaseName, r.Addon.Config.RuntimeRole = "", ""
			}
			bundle, err := p.initialBootstrapCredentials(t.Context(), r.Addon)
			if name != "owned" && name != "owner URI spoof" {
				require.Error(t, err)
				require.Nil(t, bundle)
				return
			}
			require.NoError(t, err)
			require.Contains(t, bundle.OwnerURI, "enclii_migrator:synthetic%40long%2Fpass%3Afixture@")
			require.NotContains(t, bundle.RuntimeURI, "enclii_migrator")
			require.NotContains(t, bundle.OwnerURI, "spoofed")
			require.Contains(t, bundle.PublicCA, "BEGIN CERTIFICATE")
			encoded, err := json.Marshal(bundle)
			require.NoError(t, err)
			require.Equal(t, "{}", strings.TrimSpace(string(encoded)), "owner/runtime/CA bundle must not become an API response")
		})
	}
}
