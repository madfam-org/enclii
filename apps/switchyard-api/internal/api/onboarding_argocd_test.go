package api

import (
	"context"
	"strings"
	"testing"

	argocdreg "github.com/madfam-org/enclii/apps/switchyard-api/internal/argocd"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func testArgoRegistrationRequest() argoCDRegistrationRequest {
	return argoCDRegistrationRequest{
		ProjectName:   "my-app",
		RepoFullName:  "madfam-org/my-app",
		RepoURL:       "https://github.com/madfam-org/my-app.git",
		Branch:        "main",
		ManifestPath:  "infra/k8s/production",
		Namespace:     "my-app",
		AppName:       "my-app-services",
		ProjectConfig: "{}\n",
	}
}

func TestRegisterArgoCDApplicationRuntimeMode(t *testing.T) {
	dynamicClient := fake.NewSimpleDynamicClient(runtime.NewScheme())
	h := &Handler{
		config:    &config.Config{ArgocdRegistrationMode: argocdreg.RegistrationModeRuntime, ArgocdNamespace: argocdreg.DefaultNamespace},
		k8sClient: &k8s.Client{DynamicClient: dynamicClient},
	}

	result, err := h.registerArgoCDApplication(context.Background(), testArgoRegistrationRequest())
	if err != nil {
		t.Fatalf("registerArgoCDApplication() error = %v", err)
	}
	if result.Mode != argocdreg.RegistrationModeRuntime {
		t.Fatalf("mode = %q, want runtime", result.Mode)
	}
	if result.Action != "created" {
		t.Fatalf("action = %q, want created", result.Action)
	}
	if result.CommitSHA != "" {
		t.Fatalf("runtime mode should not produce a git commit sha, got %q", result.CommitSHA)
	}
}

func TestRegisterArgoCDApplicationRuntimeModeRequiresReconciler(t *testing.T) {
	h := &Handler{
		config: &config.Config{ArgocdRegistrationMode: argocdreg.RegistrationModeRuntime},
	}

	_, err := h.registerArgoCDApplication(context.Background(), testArgoRegistrationRequest())
	if err == nil || !strings.Contains(err.Error(), "runtime reconciler not configured") {
		t.Fatalf("expected runtime reconciler error, got %v", err)
	}
}

func TestRegisterArgoCDApplicationGitOpsModeRequiresBreakGlassFlag(t *testing.T) {
	h := &Handler{
		config: &config.Config{ArgocdRegistrationMode: argocdreg.RegistrationModeGitOps},
	}

	result, err := h.registerArgoCDApplication(context.Background(), testArgoRegistrationRequest())
	if err == nil {
		t.Fatal("expected gitops mode to require break-glass flag")
	}
	if result.Mode != argocdreg.RegistrationModeGitOps {
		t.Fatalf("mode = %q, want gitops", result.Mode)
	}
	if result.Action != "" {
		t.Fatalf("action = %q, want empty action before legacy writer runs", result.Action)
	}
	if !strings.Contains(err.Error(), "legacy gitops argocd registration is disabled") {
		t.Fatalf("expected disabled legacy gitops error, got %v", err)
	}
}

func TestRegisterArgoCDApplicationGitOpsModeReportsLegacyConfigFailureWhenAllowed(t *testing.T) {
	h := &Handler{
		config: &config.Config{
			ArgocdRegistrationMode:        argocdreg.RegistrationModeGitOps,
			AllowLegacyGitOpsRegistration: true,
		},
	}

	result, err := h.registerArgoCDApplication(context.Background(), testArgoRegistrationRequest())
	if err == nil {
		t.Fatal("expected enabled gitops mode to require Enclii repo GitHub config")
	}
	if result.Mode != argocdreg.RegistrationModeGitOps {
		t.Fatalf("mode = %q, want gitops", result.Mode)
	}
	if result.Action != "legacy_config_failed" {
		t.Fatalf("action = %q, want legacy_config_failed", result.Action)
	}
}

func TestRegisterArgoCDApplicationRejectsUnknownMode(t *testing.T) {
	h := &Handler{
		config: &config.Config{ArgocdRegistrationMode: "surprise"},
	}

	_, err := h.registerArgoCDApplication(context.Background(), testArgoRegistrationRequest())
	if err == nil || !strings.Contains(err.Error(), "unsupported argocd registration mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}
