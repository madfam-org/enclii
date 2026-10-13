package api

import (
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestInitialWorkloadValueRequiresExactExternalCustody(t *testing.T) {
	p, err := isolatedBootstrapProfileFor("map-sim-initial")
	require.NoError(t, err)
	const key = "MAP_SIMULATION_CONTEXT_KEY"
	for _, name := range []string{"owned", "foreign Secret", "optional", "wrong ESO UID", "foreign vault path", "wrong store", "duplicate key", "missing value"} {
		t.Run(name, func(t *testing.T) {
			es := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "external-secrets.io/v1", "kind": "ExternalSecret", "metadata": map[string]any{"name": p.Service, "namespace": p.Namespace, "uid": "owned-eso"}, "spec": map[string]any{"target": map[string]any{"name": p.Service}, "secretStoreRef": map[string]any{"name": "vault-store", "kind": "ClusterSecretStore"}, "data": []any{map[string]any{"secretKey": key, "remoteRef": map[string]any{"key": "secret/" + p.Service}}}}}}
			s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: p.Service, Namespace: p.Namespace, UID: "owned-secret", OwnerReferences: []metav1.OwnerReference{{APIVersion: "external-secrets.io/v1", Kind: "ExternalSecret", Name: p.Service, UID: "owned-eso"}}}, Data: map[string][]byte{key: []byte("synthetic-private-value")}}
			env := corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: p.Service}, Key: key}}}
			switch name {
			case "foreign Secret":
				env.ValueFrom.SecretKeyRef.Name = "production-custody"
			case "optional":
				env.ValueFrom.SecretKeyRef.Optional = ptr.To(true)
			case "wrong ESO UID":
				s.OwnerReferences[0].UID = "foreign-eso"
			case "foreign vault path":
				require.NoError(t, unstructured.SetNestedSlice(es.Object, []any{map[string]any{"secretKey": key, "remoteRef": map[string]any{"key": "secret/foreign"}}}, "spec", "data"))
			case "wrong store":
				require.NoError(t, unstructured.SetNestedField(es.Object, "other-store", "spec", "secretStoreRef", "name"))
			case "missing value":
				delete(s.Data, key)
			}
			d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{env}}}}}}}
			if name == "duplicate key" {
				d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, env)
			}
			client := kubefake.NewSimpleClientset(s)
			h := &Handler{k8sClient: &k8s.Client{KubeClient: client, DynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), es)}}
			value, err := h.initialWorkloadValue(t.Context(), p, d, key)
			if name == "owned" {
				require.NoError(t, err)
				require.Equal(t, "synthetic-private-value", string(value))
			} else {
				require.Error(t, err)
				require.Empty(t, value)
				require.NotContains(t, err.Error(), "synthetic-private-value")
			}
			for _, action := range client.Actions() {
				require.Equal(t, "get", action.GetVerb(), "private custody resolution never mutates source credentials")
			}
		})
	}
}
