package api

import (
	"context"
	"testing"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestOnboardingConvergesExistingRegistryCopy(t *testing.T) {
	source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ghcr-credentials", Namespace: "enclii"}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{"ghcr.io":{"auth":"Zml4dHVyZTpmaXh0dXJl"}}}`)}}
	target := source.DeepCopy()
	target.Namespace = "project-a"
	target.ResourceVersion = "7"
	target.Labels = map[string]string{"enclii.dev/managed-by": "switchyard-reconciler", "enclii.dev/copied-from": "enclii"}
	target.Data[corev1.DockerConfigJsonKey] = []byte("stale")
	client := fake.NewSimpleClientset(source, target)
	logger, err := logging.NewStructuredLogger(&logging.LogConfig{Level: "panic", Format: "text", Output: "stderr"})
	require.NoError(t, err)
	handler := &Handler{k8sClient: &k8s.Client{KubeClient: client}, logger: logger}
	handler.copyRegistryCredentials(context.Background(), target.Namespace)
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
