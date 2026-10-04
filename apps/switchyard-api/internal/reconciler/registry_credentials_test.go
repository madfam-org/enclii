package reconciler

import (
	"context"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestReconcilerConvergesExistingRegistryCopy(t *testing.T) {
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ghcr-credentials", Namespace: "enclii"}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"ghcr.io":{"auth":"Zml4dHVyZTpmaXh0dXJl"}}}`)}}
	target := source.DeepCopy()
	target.Namespace = "project-a"
	target.ResourceVersion = "7"
	target.Labels = map[string]string{"enclii.dev/managed-by": "onboarding-api", "enclii.dev/copied-from": "enclii"}
	target.Data[corev1.DockerConfigJsonKey] = []byte("stale")
	client := fake.NewSimpleClientset(source, target)
	reconciler := &ServiceReconciler{k8sClient: &k8s.Client{KubeClient: client}}
	require.NoError(t, reconciler.ensureRegistryCredentials(context.Background(), target.Namespace))
	var updated *corev1.Secret
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" {
			updated = action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret)
		}
	}
	require.NotNil(t, updated)
	require.Equal(t, source.Data, updated.Data)
	require.Equal(t, "7", updated.ResourceVersion)
}
