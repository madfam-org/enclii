package argocd

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func testDesiredApplication() DesiredApplication {
	return DesiredApplication{
		AppName:              "My_App-services",
		ProjectName:          "My_App",
		RepoURL:              "https://github.com/madfam-org/my-app.git",
		Branch:               "main",
		ManifestPath:         "infra/k8s/production",
		DestinationNamespace: "my-app",
	}
}

func TestBuildApplicationMirrorsApplicationSetSemantics(t *testing.T) {
	app, err := BuildApplication(testDesiredApplication(), "")
	if err != nil {
		t.Fatalf("BuildApplication() error = %v", err)
	}

	if app.GetName() != "my-app-services" {
		t.Fatalf("name = %q, want my-app-services", app.GetName())
	}
	if app.GetNamespace() != DefaultNamespace {
		t.Fatalf("namespace = %q, want %q", app.GetNamespace(), DefaultNamespace)
	}
	if got := app.GetLabels()["app.kubernetes.io/managed-by"]; got != "enclii-platform" {
		t.Fatalf("managed-by label = %q, want enclii-platform", got)
	}

	syncOptions, found, err := unstructured.NestedStringSlice(app.Object, "spec", "syncPolicy", "syncOptions")
	if err != nil || !found {
		t.Fatalf("syncOptions not found: found=%v err=%v", found, err)
	}
	for _, want := range []string{"CreateNamespace=true", "RespectIgnoreDifferences=true", "ServerSideApply=true"} {
		if !hasString(syncOptions, want) {
			t.Fatalf("syncOptions = %#v, missing %q", syncOptions, want)
		}
	}

	ignore, found, err := unstructured.NestedSlice(app.Object, "spec", "ignoreDifferences")
	if err != nil || !found {
		t.Fatalf("ignoreDifferences not found: found=%v err=%v", found, err)
	}
	if len(ignore) < 7 {
		t.Fatalf("ignoreDifferences length = %d, want at least 7 entries", len(ignore))
	}
}

func TestReconcileApplicationCreatesMissingApplication(t *testing.T) {
	client := fake.NewSimpleDynamicClient(runtime.NewScheme())
	reconciler := NewApplicationReconciler(client, "")

	result, err := reconciler.ReconcileApplication(context.Background(), testDesiredApplication())
	if err != nil {
		t.Fatalf("ReconcileApplication() error = %v", err)
	}
	if result.Action != "created" {
		t.Fatalf("action = %q, want created", result.Action)
	}

	app, err := client.Resource(applicationGVR).Namespace(DefaultNamespace).Get(context.Background(), "my-app-services", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("created application not found: %v", err)
	}
	repoURL, _, _ := unstructured.NestedString(app.Object, "spec", "source", "repoURL")
	if repoURL != "https://github.com/madfam-org/my-app.git" {
		t.Fatalf("repoURL = %q", repoURL)
	}
}

func TestReconcileApplicationUpdatesExistingApplicationAndPreservesMetadata(t *testing.T) {
	existing, err := BuildApplication(testDesiredApplication(), "")
	if err != nil {
		t.Fatalf("BuildApplication() error = %v", err)
	}
	existing.SetLabels(map[string]string{"custom": "keep"})
	existing.SetAnnotations(map[string]string{"notifications.argoproj.io/subscribe.on-sync-succeeded.enclii": ""})
	_ = unstructured.SetNestedField(existing.Object, "old-branch", "spec", "source", "targetRevision")

	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), existing)
	reconciler := NewApplicationReconciler(client, "")

	result, err := reconciler.ReconcileApplication(context.Background(), testDesiredApplication())
	if err != nil {
		t.Fatalf("ReconcileApplication() error = %v", err)
	}
	if result.Action != "updated" {
		t.Fatalf("action = %q, want updated", result.Action)
	}

	app, err := client.Resource(applicationGVR).Namespace(DefaultNamespace).Get(context.Background(), "my-app-services", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("updated application not found: %v", err)
	}
	if got := app.GetLabels()["custom"]; got != "keep" {
		t.Fatalf("custom label = %q, want keep", got)
	}
	if _, ok := app.GetAnnotations()["notifications.argoproj.io/subscribe.on-sync-succeeded.enclii"]; !ok {
		t.Fatalf("expected notification annotation to be preserved")
	}
	branch, _, _ := unstructured.NestedString(app.Object, "spec", "source", "targetRevision")
	if branch != "main" {
		t.Fatalf("targetRevision = %q, want main", branch)
	}
}

func TestReconcileApplicationNilClient(t *testing.T) {
	reconciler := NewApplicationReconciler(nil, "")

	_, err := reconciler.ReconcileApplication(context.Background(), testDesiredApplication())
	if err == nil {
		t.Fatal("expected error for nil dynamic client")
	}
}

func TestNormalizeRegistrationMode(t *testing.T) {
	tests := map[string]string{
		"":           RegistrationModeGitOps,
		"gitops":     RegistrationModeGitOps,
		"legacy-git": RegistrationModeGitOps,
		"runtime":    RegistrationModeRuntime,
		"kubernetes": RegistrationModeRuntime,
		"weird":      "weird",
	}
	for input, want := range tests {
		if got := NormalizeRegistrationMode(input); got != want {
			t.Fatalf("NormalizeRegistrationMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
