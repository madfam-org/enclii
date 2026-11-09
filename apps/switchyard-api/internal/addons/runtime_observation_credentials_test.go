package addons

import (
	"encoding/json"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestRuntimeObservationRequiresNativeReadyCustodyAndNeverReadsOwnerSecret(t *testing.T) {
	for _, scenario := range []string{"qualified without owner", "cached Ready actual Failed", "foreign namespace", "legacy addon", "unreconciled role"} {
		t.Run(scenario, func(t *testing.T) {
			r := isolatedReq()
			r.Namespace = "project-" + r.ProjectID.String()[:8]
			resource := "pg-fixture-observation"
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
			if scenario == "cached Ready actual Failed" {
				require.NoError(t, unstructured.SetNestedField(cluster.Object, "Failed", "status", "phase"))
			}
			if scenario == "unreconciled role" {
				require.NoError(t, unstructured.SetNestedStringSlice(cluster.Object, []string{}, "status", "managedRolesStatus", "byStatus", "reconciled"))
			}
			_, err = p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(r.Namespace).Update(t.Context(), cluster, metav1.UpdateOptions{})
			require.NoError(t, err)
			if scenario == "foreign namespace" {
				ns, _ := client.CoreV1().Namespaces().Get(t.Context(), r.Namespace, metav1.GetOptions{})
				ns.Labels[k8s.LabelProjectNamespace] = "foreign"
				_, err = client.CoreV1().Namespaces().Update(t.Context(), ns, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			if scenario == "legacy addon" {
				r.Addon.Config.DatabaseName, r.Addon.Config.RuntimeRole = "", ""
			}
			client.ClearActions()
			b, err := p.runtimeObservationCredentials(t.Context(), r.Addon)
			for _, a := range client.Actions() {
				require.NotEqual(t, "create", a.GetVerb())
				require.NotEqual(t, "update", a.GetVerb())
				if a.GetVerb() == "get" && a.GetResource().Resource == "secrets" {
					require.NotEqual(t, resource+"-app", a.(ktesting.GetAction).GetName())
				}
			}
			if scenario != "qualified without owner" {
				require.Error(t, err)
				require.Nil(t, b)
				return
			}
			require.NoError(t, err)
			require.NotContains(t, b.RuntimeURI, "enclii_migrator")
			require.Contains(t, b.PublicCA, "BEGIN CERTIFICATE")
			encoded, err := json.Marshal(b)
			require.NoError(t, err)
			require.Equal(t, "{}", string(encoded))
		})
	}
}
