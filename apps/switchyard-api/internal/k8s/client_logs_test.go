package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestListPodsWithFallbackPrefersEncliiMetadataSelector(t *testing.T) {
	ctx := context.Background()
	client := &Client{KubeClient: fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "forgesight-api-new",
				Namespace: "prod-ns",
				Labels: map[string]string{
					"app":                   "forgesight-api",
					"enclii.dev/service":    "forgesight-api",
					"enclii.dev/deployment": "8043c28f-5815-4d75-897c-9c7d34174842",
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "forgesight-api-old",
				Namespace: "prod-ns",
				Labels: map[string]string{
					"app": "forgesight-api",
				},
			},
		},
	)}

	pods, matchedSelector, err := client.ListPodsWithFallback(ctx, "prod-ns", []string{
		"enclii.dev/deployment=8043c28f-5815-4d75-897c-9c7d34174842",
		"enclii.dev/service=forgesight-api",
		"app=forgesight-api",
	})
	if err != nil {
		t.Fatalf("ListPodsWithFallback returned error: %v", err)
	}
	if matchedSelector != "enclii.dev/deployment=8043c28f-5815-4d75-897c-9c7d34174842" {
		t.Fatalf("matched selector = %q, want deployment metadata selector", matchedSelector)
	}
	if len(pods.Items) != 1 || pods.Items[0].Name != "forgesight-api-new" {
		t.Fatalf("matched pods = %#v, want only deployment-metadata pod", pods.Items)
	}
}

func TestListPodsWithFallbackFallsBackToLegacyAppSelector(t *testing.T) {
	ctx := context.Background()
	client := &Client{KubeClient: fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cotiza-api-legacy",
				Namespace: "prod-ns",
				Labels: map[string]string{
					"app": "cotiza-api",
				},
			},
		},
	)}

	pods, matchedSelector, err := client.ListPodsWithFallback(ctx, "prod-ns", []string{
		"enclii.dev/deployment=5cd3a3eb-b788-48ce-aec4-5ff6c34122cb",
		"enclii.dev/service=cotiza-api",
		"app=cotiza-api",
	})
	if err != nil {
		t.Fatalf("ListPodsWithFallback returned error: %v", err)
	}
	if matchedSelector != "app=cotiza-api" {
		t.Fatalf("matched selector = %q, want legacy app selector", matchedSelector)
	}
	if len(pods.Items) != 1 || pods.Items[0].Name != "cotiza-api-legacy" {
		t.Fatalf("matched pods = %#v, want legacy app pod", pods.Items)
	}
}
