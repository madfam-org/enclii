package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func initialPortalPlan() IsolatedInitialJobPlan {
	id := uuid.New()
	return IsolatedInitialJobPlan{ProjectID: id, AddonID: uuid.New(), ServiceID: uuid.New(), Namespace: "project-" + id.String()[:8], Cluster: "pg-fixture-owned", Profile: "portal-sim-initial", Image: "ghcr.io/madfam-org/crea-sim/gateway@sha256:" + strings.Repeat("a", 64), Reason: "synthetic initial proof", IdempotencyKey: "synthetic-operation", Identity: initialFixtureIdentity(), Data: map[string][]byte{"CREA_SIM_MIGRATOR_DATABASE_URL": []byte("synthetic-owner-uri-never-returned"), "CREA_SIM_PORTAL_DATABASE_URL": []byte("synthetic-runtime-uri-never-returned"), "CREA_SIM_DATABASE_NAMESPACE": []byte("synthetic-namespace"), "ca.crt": []byte("synthetic-public-ca")}}
}

func initialFixtureIdentity() InitialBootstrapIdentity {
	return InitialBootstrapIdentity{ApplicationNamespaceUID: "owned-app-namespace", AddonNamespaceUID: "owned-namespace", DeploymentUID: "owned-deployment", DeploymentResourceVersion: "7", WorkloadServiceUID: "owned-service", ClusterUID: "owned-cluster", BindingID: uuid.New(), OnboardingID: uuid.New(), Database: "crea_sim_portal_sim", OwnerRole: "enclii_migrator", RuntimeRole: "crea_sim_portal", PublicCASHA256: initialHash([]byte("synthetic-public-ca")), Producer: "madfam-org/crea-sim", SourceRevision: strings.Repeat("b", 40), BuildRunID: 9, Checks: 2, SignatureVerified: true, PinPayloadSHA256: strings.Repeat("c", 64)}
}

func initialJobClient(t *testing.T, p IsolatedInitialJobPlan) (*Client, *kubefake.Clientset, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	port := intstr.FromInt32(53)
	client := kubefake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ghcr-credentials", Namespace: "enclii", UID: "central-registry"}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"ghcr.io":{"auth":"c3ludGhldGljOm9ubHk="}}}`)}}, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: p.Namespace, UID: "owned-namespace", Labels: map[string]string{LabelProjectNamespace: p.ProjectID.String(), "enclii.dev/verify-signatures": "true"}}}, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "default-deny", Namespace: p.Namespace}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{{Port: &port}}}}}})
	client.PrependReactor("create", "secrets", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		secret := action.GetObject().(*corev1.Secret)
		if len(action.GetCreateOptions().DryRun) > 0 {
			require.Equal(t, []string{metav1.DryRunAll}, action.GetCreateOptions().DryRun)
			return true, secret, nil
		}
		secret.UID = "owned-secret"
		return false, nil, nil
	})
	client.PrependReactor("create", "jobs", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		job := action.GetObject().(*batchv1.Job)
		if len(action.GetCreateOptions().DryRun) > 0 {
			require.Equal(t, []string{metav1.DryRunAll}, action.GetCreateOptions().DryRun)
			return true, job, nil
		}
		job.UID = "owned-job"
		job.ResourceVersion = "1"
		return false, nil, nil
	})

	client.PrependReactor("create", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		cm := action.GetObject().(*corev1.ConfigMap)
		if len(action.GetCreateOptions().DryRun) > 0 {
			return true, cm, nil
		}
		cm.UID = types.UID(cm.Name + "-uid")
		cm.ResourceVersion = "1"
		return false, nil, nil
	})
	client.PrependReactor("create", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.CreateActionImpl)
		require.Equal(t, []string{metav1.DryRunAll}, action.GetCreateOptions().DryRun)
		return true, action.GetObject(), nil
	})
	policy := &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "kyverno.io/v1", "kind": "ClusterPolicy", "metadata": map[string]interface{}{"name": "verify-image-signatures"}, "spec": map[string]interface{}{"validationFailureAction": "Enforce", "rules": []interface{}{map[string]interface{}{"name": "check-signature", "match": map[string]interface{}{"any": []interface{}{map[string]interface{}{"resources": map[string]interface{}{"kinds": []interface{}{"Pod"}, "namespaceSelector": map[string]interface{}{"matchLabels": map[string]interface{}{"enclii.dev/verify-signatures": "true"}}}}}}, "verifyImages": []interface{}{map[string]interface{}{"required": true, "verifyDigest": true, "imageReferences": []interface{}{"ghcr.io/madfam-org/*"}}}}}}}}
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), policy)
	return &Client{KubeClient: client, DynamicClient: dynamic}, client, dynamic
}

func TestInitialJobDryRunAndRefusalsNeverCreateRuntime(t *testing.T) {
	for _, name := range []string{"dry run", "foreign namespace", "broad egress", "admission denied", "unknown profile", "broadened environment"} {
		t.Run(name, func(t *testing.T) {
			p := initialPortalPlan()
			kube, client, _ := initialJobClient(t, p)
			if name == "foreign namespace" {
				ns, _ := client.CoreV1().Namespaces().Get(t.Context(), p.Namespace, metav1.GetOptions{})
				ns.Labels[LabelProjectNamespace] = "foreign"
				_, err := client.CoreV1().Namespaces().Update(t.Context(), ns, metav1.UpdateOptions{})
				require.NoError(t, err)
				client.ClearActions()
			}
			if name == "broad egress" {
				_, err := client.NetworkingV1().NetworkPolicies(p.Namespace).Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "foreign-wide", Namespace: p.Namespace}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{}}}}, metav1.CreateOptions{})
				require.NoError(t, err)
				client.ClearActions()
			}
			if name == "admission denied" {
				client.PrependReactor("create", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("synthetic admission refusal")
				})
			}
			if name == "unknown profile" {
				p.Profile = "reset"
			}
			if name == "broadened environment" {
				p.Data["JANUA_INTERNAL_API_KEY"] = []byte("synthetic-forbidden")
			}
			receipt, err := kube.RunIsolatedInitialJob(t.Context(), p, true)
			if name == "dry run" {
				require.NoError(t, err)
				require.True(t, receipt.DryRun)
				require.True(t, receipt.Suspended)
			} else {
				require.Error(t, err)
				require.Nil(t, receipt)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "create" {
					require.Equal(t, []string{metav1.DryRunAll}, action.(ktesting.CreateActionImpl).GetCreateOptions().DryRun)
				} else {
					require.Contains(t, []string{"get", "list"}, action.GetVerb(), "dry run/refusal must precede all persisted Jobs/Secret/policy writes")
				}
			}
		})
	}
}

func TestInitialJobStopsSuspendedOnCustodyOrAuthorityFailure(t *testing.T) {
	for _, name := range []string{"custody failure", "authority changed", "activate once"} {
		t.Run(name, func(t *testing.T) {
			p := initialPortalPlan()
			kube, client, _ := initialJobClient(t, p)
			p.Revalidate = func(ctx context.Context) error {
				if name == "authority changed" {
					return errors.New("synthetic stale authority")
				}
				return nil
			}
			if name == "custody failure" {
				client.PrependReactor("create", "secrets", func(a ktesting.Action) (bool, runtime.Object, error) {
					if a.(ktesting.CreateAction).GetObject().(*corev1.Secret).Name == "ghcr-credentials" {
						return false, nil, nil
					}
					return true, nil, errors.New("synthetic custody failure")
				})
			}
			receipt, err := kube.RunIsolatedInitialJob(t.Context(), p, false)
			job, getErr := client.BatchV1().Jobs(p.Namespace).Get(t.Context(), p.jobName(), metav1.GetOptions{})
			require.NoError(t, getErr)
			if name == "activate once" {
				require.NoError(t, err)
				require.False(t, receipt.Suspended)
				require.False(t, *job.Spec.Suspend)
			} else {
				require.Error(t, err)
				require.Nil(t, receipt)
				require.True(t, *job.Spec.Suspend)
			}
			encoded, _ := json.Marshal(receipt)
			require.NotContains(t, string(encoded), "synthetic-owner-uri-never-returned")
			if name == "activate once" {
				secret, getErr := client.CoreV1().Secrets(p.Namespace).Get(t.Context(), job.Name+"-credentials", metav1.GetOptions{})
				require.NoError(t, getErr)
				require.Equal(t, job.UID, secret.OwnerReferences[0].UID)
				policy, getErr := client.NetworkingV1().NetworkPolicies(p.Namespace).Get(t.Context(), job.Name+"-database", metav1.GetOptions{})
				require.NoError(t, getErr)
				require.Len(t, policy.Spec.Egress, 1)
				require.Nil(t, policy.Spec.Egress[0].To[0].NamespaceSelector)
				require.Equal(t, p.Cluster, policy.Spec.Egress[0].To[0].PodSelector.MatchLabels["cnpg.io/cluster"])
				require.EqualValues(t, 5432, policy.Spec.Egress[0].Ports[0].Port.IntVal)
				client.ClearActions()
				again, replayErr := kube.RunIsolatedInitialJob(t.Context(), p, false)
				require.NoError(t, replayErr)
				require.False(t, again.Suspended)
				for _, action := range client.Actions() {
					require.Contains(t, []string{"get", "list"}, action.GetVerb(), "same operation key must not rerun/replace an activated Job")
				}
			}
		})
	}
}

func TestInitialMAPJobUsesCanonicalEntrypointsAndFreshGuard(t *testing.T) {
	p := initialPortalPlan()
	p.Profile = "map-sim-initial"
	p.Image = "ghcr.io/madfam-org/crea-map/crea-map-web@sha256:" + strings.Repeat("a", 64)
	p.Data = map[string][]byte{"ca.crt": []byte("synthetic-public-ca")}
	for _, key := range []string{"DATABASE_URL", "MAP_SIMULATION_RUNTIME_DATABASE_URL", "MAP_SIMULATION_SEED_ENABLED", "MAP_SIMULATION_ENABLED", "MAP_SIMULATION_KERNEL_ID", "APP_ORIGIN", "JANUA_AUDIENCE", "JANUA_ISSUER_URL", "JANUA_JWKS_URL", "RLS_POR_CASO", "MAP_SIMULATION_CONTEXT_KEY", "MAP_SIMULATION_CONTROL_KEY", "MAP_SIMULATION_MAIL_KEY", "MAP_SIMULATION_MCP_CLIENT_ID", "MAP_MCP_PSEUDONYM_KEY"} {
		p.Data[key] = []byte("synthetic-value")
	}
	job, err := BuildIsolatedInitialJob(p)
	require.NoError(t, err)
	spec := job.Spec.Template.Spec
	require.EqualValues(t, 1001, *spec.SecurityContext.RunAsUser)
	require.EqualValues(t, 1001, *spec.SecurityContext.RunAsGroup)
	require.EqualValues(t, 1001, *spec.SecurityContext.FSGroup)
	require.Equal(t, "tmp", spec.Volumes[1].Name)
	require.NotNil(t, spec.Volumes[1].EmptyDir)
	require.False(t, *spec.AutomountServiceAccountToken)
	require.Len(t, spec.InitContainers, 2)
	require.Contains(t, spec.InitContainers[0].Command[2], "a[0].tablas!==0n")
	require.Equal(t, []string{"node", "/app/prisma-cli/node_modules/prisma/build/index.js", "migrate", "deploy", "--schema=/app/prisma/schema.prisma"}, spec.InitContainers[1].Command)
	require.Equal(t, []string{"node", "/app/dist/semilla-simulacion.cjs", "--aplicar"}, spec.Containers[0].Command)
	for _, container := range append(spec.InitContainers, spec.Containers...) {
		require.Equal(t, p.Image, container.Image)
		require.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
		// The cluster's disallow-privileged-containers policy requires the field
		// to be present and false on every container, init containers included.
		require.NotNil(t, container.SecurityContext.Privileged)
		require.False(t, *container.SecurityContext.Privileged)
		for _, env := range container.Env {
			require.NotEqual(t, "synthetic-value", env.Value, "secret values live only in the owned transient Secret")
		}
	}
	encoded, _ := json.Marshal(p)
	require.NotContains(t, string(encoded), "synthetic-value")
}
