package k8s

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const fixtureDockerConfig = `{"auths":{"ghcr.io":{"auth":"Zml4dHVyZTpmaXh0dXJl"}}}`

func registrySource() *corev1.Secret {
	return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: registryCredentialName, Namespace: registryCredentialSource, ResourceVersion: "10", Labels: map[string]string{"private-source-label": "do-not-copy"}}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(fixtureDockerConfig)}}
}
func registryTarget(manager string) *corev1.Secret {
	s := registrySource()
	s.Namespace = "project-a"
	s.ResourceVersion = "20"
	s.Labels = map[string]string{registryManagerLabel: manager, registrySourceLabel: registryCredentialSource, "keep": "label"}
	s.Annotations = map[string]string{"keep": "annotation"}
	s.Finalizers = []string{"fixture.example/retain"}
	s.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"ghcr.io":{"auth":"b2xkOm9sZA=="}}}`)
	return s
}
func registryWrites(client *fake.Clientset) []k8stesting.Action {
	var writes []k8stesting.Action
	for _, a := range client.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			writes = append(writes, a)
		}
	}
	return writes
}

func TestRegistryCredentialsCreateAndConverge(t *testing.T) {
	for _, manager := range []string{"onboarding-api", "switchyard-reconciler"} {
		t.Run(manager, func(t *testing.T) {
			source := registrySource()
			client := fake.NewSimpleClientset(source)
			c := &Client{KubeClient: client}
			require.NoError(t, c.EnsureRegistryCredentials(context.Background(), "project-a", manager))
			got, err := client.CoreV1().Secrets("project-a").Get(context.Background(), registryCredentialName, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, source.Data, got.Data)
			require.Equal(t, source.Type, got.Type)
			require.Equal(t, map[string]string{registryManagerLabel: manager, registrySourceLabel: registryCredentialSource}, got.Labels)
			require.Empty(t, got.OwnerReferences)
			require.Empty(t, got.ResourceVersion)
			client.ClearActions()
			require.NoError(t, c.EnsureRegistryCredentials(context.Background(), "project-a", manager))
			require.Empty(t, registryWrites(client), "identical copy must not write")
		})
	}
	for _, owner := range []string{"onboarding-api", "switchyard-reconciler"} {
		t.Run("update "+owner, func(t *testing.T) {
			source, target := registrySource(), registryTarget(owner)
			target.Data["keep"] = []byte("namespace-data")
			client := fake.NewSimpleClientset(source, target)
			c := &Client{KubeClient: client}
			require.NoError(t, c.EnsureRegistryCredentials(context.Background(), target.Namespace, "switchyard-reconciler"))
			writes := registryWrites(client)
			require.Len(t, writes, 1)
			require.Equal(t, "update", writes[0].GetVerb())
			update := writes[0].(k8stesting.UpdateAction).GetObject().(*corev1.Secret)
			require.Equal(t, "20", update.ResourceVersion, "write must carry inspected version")
			require.Equal(t, target.ObjectMeta, update.ObjectMeta)
			require.Equal(t, target.Data["keep"], update.Data["keep"])
			require.Equal(t, source.Data[corev1.DockerConfigJsonKey], update.Data[corev1.DockerConfigJsonKey])
			unchanged, err := client.CoreV1().Secrets(registryCredentialSource).Get(context.Background(), registryCredentialName, metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, source, unchanged)
		})
	}
}

func TestRegistryCredentialsRefuseUnownedTargets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{"unknown manager", func(s *corev1.Secret) { s.Labels[registryManagerLabel] = "other" }},
		{"missing source label", func(s *corev1.Secret) { delete(s.Labels, registrySourceLabel) }},
		{"other source", func(s *corev1.Secret) { s.Labels[registrySourceLabel] = "other" }},
		{"ESO owner", func(s *corev1.Secret) {
			s.OwnerReferences = []metav1.OwnerReference{{APIVersion: "external-secrets.io/v1beta1", Kind: "ExternalSecret", Name: "fixture"}}
		}},
		{"other owner", func(s *corev1.Secret) {
			s.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "ConfigMap", Name: "fixture"}}
		}},
		{"ESO merge label", func(s *corev1.Secret) { s.Labels["reconcile.external-secrets.io/managed"] = "true" }},
		{"ESO merge annotation", func(s *corev1.Secret) { s.Annotations["reconcile.external-secrets.io/data-hash"] = "fixture" }},
		{"ESO managed by", func(s *corev1.Secret) { s.Labels["app.kubernetes.io/managed-by"] = "external-secrets" }},
		{"immutable", func(s *corev1.Secret) { v := true; s.Immutable = &v }},
		{"missing resource version", func(s *corev1.Secret) { s.ResourceVersion = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := registryTarget("onboarding-api")
			tc.mutate(target)
			client := fake.NewSimpleClientset(registrySource(), target)
			err := (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), target.Namespace, "switchyard-reconciler")
			require.Error(t, err)
			require.Empty(t, registryWrites(client))
			got, e := client.CoreV1().Secrets(target.Namespace).Get(context.Background(), registryCredentialName, metav1.GetOptions{})
			require.NoError(t, e)
			require.Equal(t, target, got)
		})
	}
}

func TestRegistryCredentialsValidateCanonicalSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Secret)
	}{
		{"wrong type", func(s *corev1.Secret) { s.Type = corev1.SecretTypeOpaque }},
		{"missing data", func(s *corev1.Secret) { s.Data = nil }},
		{"extra payload", func(s *corev1.Secret) { s.Data["other"] = []byte("unrelated-value") }},
		{"invalid JSON", func(s *corev1.Secret) { s.Data[corev1.DockerConfigJsonKey] = []byte("private-sentinel") }},
		{"empty auths", func(s *corev1.Secret) { s.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{}}`) }},
		{"other registry", func(s *corev1.Secret) {
			s.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"other.example":{"auth":"Zml4dHVyZTpmaXh0dXJl"}}}`)
		}},
		{"invalid auth", func(s *corev1.Secret) {
			s.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"ghcr.io":{"auth":"private-sentinel"}}}`)
		}},
		{"empty credentials", func(s *corev1.Secret) { s.Data[corev1.DockerConfigJsonKey] = []byte(`{"auths":{"ghcr.io":{}}}`) }},
	} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%v", tc.name, exists), func(t *testing.T) {
				source := registrySource()
				tc.mutate(source)
				objects := []runtime.Object{source}
				if exists {
					objects = append(objects, registryTarget("onboarding-api"))
				}
				client := fake.NewSimpleClientset(objects...)
				err := (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), "project-a", "onboarding-api")
				require.EqualError(t, err, "canonical registry credentials are malformed")
				require.Empty(t, registryWrites(client))
				require.NotContains(t, err.Error(), "private-sentinel")
			})
		}
	}
	for _, data := range []string{`{"auths":{"ghcr.io":{"username":"fixture","password":"fixture"}}}`, `{"auths":{"ghcr.io":{"identitytoken":"fixture"}}}`} {
		source := registrySource()
		source.Data[corev1.DockerConfigJsonKey] = []byte(data)
		require.True(t, validRegistryCredentials(source))
	}
}

func TestRegistryCredentialsNoSourceOrClient(t *testing.T) {
	for _, client := range []*Client{nil, {}} {
		require.Error(t, client.EnsureRegistryCredentials(context.Background(), "project-a", "onboarding-api"))
	}
	for _, ns := range []string{"", registryCredentialSource} {
		client := fake.NewSimpleClientset(registrySource())
		require.Error(t, (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), ns, "onboarding-api"))
		require.Empty(t, client.Actions())
	}
	client := fake.NewSimpleClientset()
	require.Error(t, (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), "project-a", "onboarding-api"))
	require.Empty(t, registryWrites(client))
}

func TestRegistryCredentialsConflictDoesNotOverwrite(t *testing.T) {
	client := fake.NewSimpleClientset(registrySource(), registryTarget("onboarding-api"))
	client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		update := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret)
		require.Equal(t, "20", update.ResourceVersion)
		concurrent := registryTarget("onboarding-api")
		concurrent.ResourceVersion = "21"
		concurrent.Data[corev1.DockerConfigJsonKey] = []byte("concurrent-value")
		require.NoError(t, client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), concurrent, concurrent.Namespace))
		return true, nil, k8serrors.NewConflict(schema.GroupResource{Resource: "secrets"}, registryCredentialName, fmt.Errorf("private-sentinel"))
	})
	err := (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), "project-a", "onboarding-api")
	require.EqualError(t, err, "registry credentials changed during reconciliation; retry required")
	require.Len(t, registryWrites(client), 1)
	got, e := client.CoreV1().Secrets("project-a").Get(context.Background(), registryCredentialName, metav1.GetOptions{})
	require.NoError(t, e)
	require.Equal(t, []byte("concurrent-value"), got.Data[corev1.DockerConfigJsonKey])
}

func TestRegistryCredentialsErrorsAreSanitized(t *testing.T) {
	for _, verb := range []string{"get", "create", "update"} {
		t.Run(verb, func(t *testing.T) {
			objects := []runtime.Object{registrySource()}
			if verb == "update" {
				objects = append(objects, registryTarget("onboarding-api"))
			}
			client := fake.NewSimpleClientset(objects...)
			client.PrependReactor(verb, "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("private-sentinel internal-server-address")
			})
			err := (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), "project-a", "onboarding-api")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-sentinel")
			require.NotContains(t, err.Error(), "internal-server-address")
		})
	}
	client := fake.NewSimpleClientset(registrySource())
	client.PrependReactor("create", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, k8serrors.NewAlreadyExists(schema.GroupResource{Resource: "secrets"}, registryCredentialName)
	})
	err := (&Client{KubeClient: client}).EnsureRegistryCredentials(context.Background(), "project-a", "onboarding-api")
	require.EqualError(t, err, "registry credential target appeared during reconciliation; retry required")
	require.Len(t, registryWrites(client), 1)
}
