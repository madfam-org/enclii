package k8s

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Guards the fix for the eido-db go-live incident: every addon namespace gets a
// default-deny NetworkPolicy, so a Postgres addon needs a companion allow-ingress
// or consuming services hit a silent ConnectionRefused. This verifies the policy
// shape the provisioner emits.
func TestBuildDataAccessIngressPolicy(t *testing.T) {
	pol := buildDataAccessIngressPolicy(
		"project-0be6ce5e",
		"pg-eido-db-65c0e62b-data-access",
		map[string]string{"cnpg.io/cluster": "pg-eido-db-65c0e62b"},
		5432,
	)

	assert.Equal(t, "pg-eido-db-65c0e62b-data-access", pol.Name)
	assert.Equal(t, "project-0be6ce5e", pol.Namespace)

	// Ingress-only: must NOT re-assert egress (that would clobber the
	// default-deny's DNS-egress allowance for these pods).
	assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, pol.Spec.PolicyTypes)

	// Targets exactly this addon's CNPG pods, not all pods in the namespace.
	assert.Equal(t, map[string]string{"cnpg.io/cluster": "pg-eido-db-65c0e62b"}, pol.Spec.PodSelector.MatchLabels)

	if assert.Len(t, pol.Spec.Ingress, 1) {
		rule := pol.Spec.Ingress[0]
		if assert.Len(t, rule.From, 1) {
			ns := rule.From[0].NamespaceSelector
			if assert.NotNil(t, ns, "must select by namespace, not pod") {
				assert.Equal(t, "true", ns.MatchLabels["enclii.dev/data-access"])
			}
			assert.Nil(t, rule.From[0].PodSelector, "cross-namespace allow keys on the namespace label only")
		}
		if assert.Len(t, rule.Ports, 1) {
			assert.Equal(t, int32(5432), rule.Ports[0].Port.IntVal)
		}
	}
}

// The 2026-08-17 tenant-isolation audit: the flat data-access ingress opened
// each addon DB port to EVERY namespace in the data-access class, so only the
// DB password separated a self-checkout tenant from a clinical DB. The
// project-scoped policy is the fix — ingress admitted ONLY from same-project
// namespaces. This pins that shape so it cannot regress to the flat one.
func TestProjectScopedIngressPolicyIsolatesByProject(t *testing.T) {
	const projectID = "0be6ce5e-1111-4111-8111-111111111111"
	pol := buildProjectScopedIngressPolicy(
		"project-0be6ce5e",
		"pg-crea-db-data-access",
		projectID,
		map[string]string{"cnpg.io/cluster": "pg-crea-db"},
		5432,
	)

	if assert.Len(t, pol.Spec.Ingress, 1) && assert.Len(t, pol.Spec.Ingress[0].From, 1) {
		sel := pol.Spec.Ingress[0].From[0].NamespaceSelector
		if assert.NotNil(t, sel, "must scope by namespaceSelector, not admit all") {
			// The critical assertion: keyed on the PROJECT label with THIS
			// project's id — not on the flat data-access class.
			assert.Equal(t, projectID, sel.MatchLabels[LabelProjectNamespace],
				"ingress must admit only same-project namespaces")
			assert.NotContains(t, sel.MatchLabels, "enclii.dev/data-access",
				"must NOT fall back to the flat data-access class")
		}
	}
	// Pod selector and port are unchanged from the working data-access shape.
	assert.Equal(t, "pg-crea-db", pol.Spec.PodSelector.MatchLabels["cnpg.io/cluster"])
	assert.Equal(t, int32(5432), pol.Spec.Ingress[0].Ports[0].Port.IntVal)
}

// The 2026-08-17 audit found addon namespaces (project-*) got no LimitRange or
// ResourceQuota, so a Postgres cluster with empty CPU/Memory became an
// unbounded BestEffort pod. These guards must exist and must set a DB-sized
// memory floor (not the 128Mi service default that would OOM Postgres).
func TestAddonNamespaceResourceGuardsAreAppliedWithDbSizedFloor(t *testing.T) {
	c := &Client{KubeClient: fake.NewSimpleClientset()}
	ctx := context.Background()
	if err := c.EnsureAddonNamespaceResourceGuards(ctx, "project-crea"); err != nil {
		t.Fatalf("apply guards: %v", err)
	}

	lr, err := c.KubeClient.CoreV1().LimitRanges("project-crea").Get(ctx, "enclii-addon-default", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("LimitRange must exist: %v", err)
	}
	memFloor := lr.Spec.Limits[0].DefaultRequest[corev1.ResourceMemory]
	if memFloor.Cmp(resource.MustParse("256Mi")) != 0 {
		t.Fatalf("DefaultRequest memory floor = %s; must be DB-sized (256Mi), not the 128Mi service default", memFloor.String())
	}
	cpuFloor := lr.Spec.Limits[0].DefaultRequest[corev1.ResourceCPU]
	if cpuFloor.IsZero() {
		t.Fatal("a CPU request floor is required so the pod is not BestEffort")
	}

	q, err := c.KubeClient.CoreV1().ResourceQuotas("project-crea").Get(ctx, "enclii-addon-default", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("ResourceQuota must exist: %v", err)
	}
	cpuCeil := q.Spec.Hard[corev1.ResourceRequestsCPU]
	if cpuCeil.IsZero() {
		t.Fatal("namespace CPU ceiling required so one addon cannot starve the node")
	}
}

// Idempotent: re-applying must not error (provisioning re-runs are the refresh).
func TestAddonResourceGuardsIdempotent(t *testing.T) {
	c := &Client{KubeClient: fake.NewSimpleClientset()}
	ctx := context.Background()
	if err := c.EnsureAddonNamespaceResourceGuards(ctx, "project-crea"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := c.EnsureAddonNamespaceResourceGuards(ctx, "project-crea"); err != nil {
		t.Fatalf("second apply must be a no-op, got: %v", err)
	}
}
