package argocd

import (
	"context"
	"strings"
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
	assertNotificationSubscriptions(t, app)

	syncOptions, found, err := unstructured.NestedStringSlice(app.Object, "spec", "syncPolicy", "syncOptions")
	if err != nil || !found {
		t.Fatalf("syncOptions not found: found=%v err=%v", found, err)
	}
	for _, want := range []string{"CreateNamespace=true", "RespectIgnoreDifferences=true", "ServerSideApply=true"} {
		if !hasString(syncOptions, want) {
			t.Fatalf("syncOptions = %#v, missing %q", syncOptions, want)
		}
	}

	// The diff must stay CLIENT-SIDE. ServerSideDiff runs a dry-run apply through
	// admission, which Kyverno's verify-image-signatures denies whenever a CI
	// digest bump changes the kyverno.io/verify-images annotation on a
	// signature-verified Deployment -- wedging the whole app sync on a
	// ComparisonError (confirmed live on nauta-services 2026-09-08). It must not
	// be reintroduced, in the compare-options annotation or anywhere else.
	compareOptions := app.GetAnnotations()["argocd.argoproj.io/compare-options"]
	if !hasCompareOption(compareOptions, "IgnoreExtraneous=true") {
		t.Fatalf("compare-options = %q, missing %q", compareOptions, "IgnoreExtraneous=true")
	}
	if hasCompareOption(compareOptions, "ServerSideDiff=true") {
		t.Fatalf("compare-options = %q, must NOT carry ServerSideDiff (its admission dry-run is denied by Kyverno on signature-verified Deployments; ESO CRD defaults are spelled out in git instead)", compareOptions)
	}
	if hasString(syncOptions, "ServerSideDiff=true") {
		t.Fatalf("syncOptions = %#v, must not carry ServerSideDiff (ArgoCD ignores it there)", syncOptions)
	}

	ignore, found, err := unstructured.NestedSlice(app.Object, "spec", "ignoreDifferences")
	if err != nil || !found {
		t.Fatalf("ignoreDifferences not found: found=%v err=%v", found, err)
	}
	if len(ignore) < 7 {
		t.Fatalf("ignoreDifferences length = %d, want at least 7 entries", len(ignore))
	}
	if !hasIgnoreDifference(ignore, "batch", "CronJob", "/metadata/annotations") {
		t.Fatalf("ignoreDifferences missing CronJob Kyverno verify-images rule: %#v", ignore)
	}

	// Regression guard: an ignoreDifferences rule on a CRD whose jqPathExpressions
	// reach inside a list makes RespectIgnoreDifferences=true replay the whole live
	// list over the desired one (RFC 7386 merge patch), so new spec.data entries
	// never reach the cluster. ExternalSecret must therefore have no rule at all.
	for _, rule := range ignore {
		entry, ok := rule.(map[string]any)
		if !ok {
			continue
		}
		if entry["group"] == "external-secrets.io" && entry["kind"] == "ExternalSecret" {
			t.Fatalf("ExternalSecret ignoreDifferences rule must not be reintroduced: %#v", entry)
		}
	}

	// The rules that remain must only target built-in kinds, which resolve in the
	// Kubernetes scheme and so take ArgoCD's safe strategic-merge path.
	builtInGroups := map[string]bool{"": true, "apps": true, "batch": true, "policy": true}
	for _, rule := range ignore {
		entry, ok := rule.(map[string]any)
		if !ok {
			continue
		}
		group, _ := entry["group"].(string)
		if _, hasJQ := entry["jqPathExpressions"]; hasJQ && !builtInGroups[group] {
			t.Fatalf("jqPathExpressions on non-built-in group %q is unsafe with RespectIgnoreDifferences: %#v", group, entry)
		}
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
	// A pre-fix runtime Application has no release subscriptions. Preserve
	// unrelated annotations while adding both channels on an explicit ensure.
	existing.SetAnnotations(map[string]string{"example.test/custom": "keep"})
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
	assertNotificationSubscriptions(t, app)
	if app.GetAnnotations()["example.test/custom"] != "keep" {
		t.Fatal("custom annotation was not preserved")
	}
	branch, _, _ := unstructured.NestedString(app.Object, "spec", "source", "targetRevision")
	if branch != "main" {
		t.Fatalf("targetRevision = %q, want main", branch)
	}
	result, err = reconciler.ReconcileApplication(context.Background(), testDesiredApplication())
	if err != nil || result.Action != "unchanged" {
		t.Fatalf("repeat ensure must be a no-op: result=%v err=%v", result, err)
	}
}

func assertNotificationSubscriptions(t *testing.T, app *unstructured.Unstructured) {
	t.Helper()
	for _, trigger := range []string{"on-sync-succeeded", "on-sync-failed"} {
		key := "notifications.argoproj.io/subscribe." + trigger + ".enclii"
		if value, exists := app.GetAnnotations()[key]; !exists || value != "" {
			t.Fatalf("notification subscription %s missing or invalid: %q", key, value)
		}
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
		"":           RegistrationModeRuntime,
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

// hasCompareOption mirrors how ArgoCD reads the compare-options annotation
// (gitops-engine resource.GetAnnotationCSVs): comma separated, values trimmed.
func hasCompareOption(annotation string, want string) bool {
	for _, item := range strings.Split(annotation, ",") {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasIgnoreDifference(ignore []any, group string, kind string, pointer string) bool {
	for _, entry := range ignore {
		rule, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if rule["group"] != group || rule["kind"] != kind {
			continue
		}
		pointers, ok := rule["jsonPointers"].([]any)
		if !ok {
			continue
		}
		for _, candidate := range pointers {
			if candidate == pointer {
				return true
			}
		}
	}
	return false
}
