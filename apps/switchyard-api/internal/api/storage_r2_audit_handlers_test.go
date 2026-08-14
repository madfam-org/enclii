package api

import (
	"context"
	"strings"
	"testing"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/provisioning"
)

func newAuditHandler(t *testing.T, objects ...k8sruntime.Object) *Handler {
	t.Helper()
	clientset := fake.NewSimpleClientset(objects...)
	return &Handler{
		logger:    r2TestLogger(t),
		k8sClient: &k8s.Client{KubeClient: clientset},
	}
}

func TestR2Audit_IsAnAllowlistedReadOnlyAction(t *testing.T) {
	if !isReadOnlyOperatorAction("ops", "storage", "r2-audit") {
		t.Fatal("ops.storage.r2-audit must be in the read-only operator allowlist, or the endpoint 404s")
	}

	found := false
	for _, cap := range opsCapabilities {
		if cap.Name != "storage" {
			continue
		}
		for _, action := range cap.Actions {
			if action == "r2-audit" {
				found = true
			}
		}
	}
	if !found {
		t.Error("r2-audit must be declared in opsCapabilities or the operator contract rejects it")
	}
}

// TestHandleOpsStorageR2Audit_CriticalFindingFailsTheOperation is what makes
// this a gate rather than a report: `admin ga-verify` keys off the status.
func TestHandleOpsStorageR2Audit_CriticalFindingFailsTheOperation(t *testing.T) {
	h := newAuditHandler(t,
		r2Secret("tezca", "tezca-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket:          "tezca-documents",
			provisioning.SecretKeyR2AccessKeyID:     "shared-key",
			provisioning.SecretKeyR2SecretAccessKey: "shared-secret",
			provisioning.SecretKeyStorageBackend:    provisioning.StorageBackendR2,
		}, map[string]string{
			provisioning.AnnotationR2Bucket:  "tezca-documents",
			provisioning.AnnotationR2Project: "tezca",
		}),
		r2Secret("karafiel", "r2-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket:          "tezca-documents",
			provisioning.SecretKeyR2AccessKeyID:     "shared-key",
			provisioning.SecretKeyR2SecretAccessKey: "shared-secret",
			provisioning.SecretKeyStorageBackend:    provisioning.StorageBackendR2,
		}, nil),
	)

	resp := h.handleOpsStorageR2Audit(context.Background(), "ops.storage.r2-audit", "storage", "r2-audit",
		operatorOperationRequest{Operation: "ops.storage.r2-audit", DryRun: true})

	if resp.Status != "failed" {
		t.Errorf("status = %q, want failed when a critical finding exists", resp.Status)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data = %T, want map", resp.Data)
	}
	if critical, _ := data["critical_count"].(int); critical == 0 {
		t.Error("critical_count must be non-zero for shared credentials")
	}
	if len(resp.Warnings) == 0 {
		t.Error("findings must be surfaced as warnings for CLI rendering")
	}
	if len(resp.Next) == 0 {
		t.Error("a critical result must carry remediation next-steps")
	}
	for _, warn := range resp.Warnings {
		if strings.Contains(warn, "shared-secret") {
			t.Errorf("credential material leaked into a warning: %s", warn)
		}
	}
}

func TestHandleOpsStorageR2Audit_CleanFleetSucceeds(t *testing.T) {
	h := newAuditHandler(t,
		r2Secret("tezca", "tezca-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket:          "tezca-documents",
			provisioning.SecretKeyR2AccessKeyID:     "key-tezca",
			provisioning.SecretKeyR2SecretAccessKey: "secret-tezca",
			provisioning.SecretKeyStorageBackend:    provisioning.StorageBackendR2,
		}, map[string]string{
			provisioning.AnnotationR2Bucket:  "tezca-documents",
			provisioning.AnnotationR2Project: "tezca",
		}),
	)

	resp := h.handleOpsStorageR2Audit(context.Background(), "ops.storage.r2-audit", "storage", "r2-audit",
		operatorOperationRequest{Operation: "ops.storage.r2-audit", DryRun: true})

	if resp.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded for a clean fleet", resp.Status)
	}
	data := resp.Data.(map[string]any)
	if critical, _ := data["critical_count"].(int); critical != 0 {
		t.Errorf("critical_count = %d, want 0", critical)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("a clean fleet must be quiet, got warnings: %v", resp.Warnings)
	}
}

func TestHandleOpsStorageR2Audit_NamespaceScope(t *testing.T) {
	h := newAuditHandler(t,
		r2Secret("tezca", "tezca-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket: "tezca-documents",
		}, nil),
		r2Secret("karafiel", "karafiel-credentials", map[string]string{
			provisioning.SecretKeyR2Bucket: "karafiel-documents",
		}, nil),
	)

	data, err := h.readR2CredentialDrift(context.Background(), operatorOperationRequest{
		Operation: "ops.storage.r2-audit",
		Scope:     map[string]string{"namespace": "karafiel"},
	})
	if err != nil {
		t.Fatalf("readR2CredentialDrift: %v", err)
	}
	if got, _ := data["binding_count"].(int); got != 1 {
		t.Errorf("binding_count = %d, want 1 with a namespace scope", got)
	}
	if got, _ := data["scope"].(string); got != "karafiel" {
		t.Errorf("scope = %q", got)
	}
}

func TestHandleOpsStorageR2Audit_RequiresKubeClient(t *testing.T) {
	h := &Handler{logger: r2TestLogger(t)}
	resp := h.handleOpsStorageR2Audit(context.Background(), "ops.storage.r2-audit", "storage", "r2-audit",
		operatorOperationRequest{Operation: "ops.storage.r2-audit"})
	if resp.Status != "failed" {
		t.Errorf("status = %q, want failed without a cluster client", resp.Status)
	}
}
