package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/netpolicy"
)

// fakeNetworkPolicyCluster records applies; it never reaches a cluster.
type fakeNetworkPolicyCluster struct {
	live      []networkingv1.NetworkPolicy
	listErr   error
	workloads []netpolicy.Workload
	notes     []string
	applied   int
}

func (f *fakeNetworkPolicyCluster) ListNetworkPolicies(context.Context, string) ([]networkingv1.NetworkPolicy, error) {
	return f.live, f.listErr
}

func (f *fakeNetworkPolicyCluster) ListNetworkPolicyWorkloads(context.Context, string) ([]netpolicy.Workload, []string) {
	return f.workloads, f.notes
}

func (f *fakeNetworkPolicyCluster) ApplyNetworkPolicies(_ context.Context, _ string, yamlBytes []byte) (int, error) {
	policies, err := netpolicy.ParsePolicies(yamlBytes)
	f.applied += len(policies)
	return len(policies), err
}

// The live crea-map egress before 2026-10-21: every pod, PgBouncer allowed.
func creaMapLiveEgress() networkingv1.NetworkPolicy {
	return networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "crea-map-egress"},
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "data"}},
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pgbouncer"}},
				}},
			}},
		},
	}
}

// enclii.yaml as it was: postgres, not pgbouncer.
var creaMapDeclared = netpolicy.NetworkSpec{Services: []netpolicy.ServiceSpec{{
	Name: "crea-map", Port: 3000, Ingress: []string{"cloudflare-tunnel"}, Egress: []string{"dns", "https", "postgres"},
}}}

func generatedCreaMap(t *testing.T) []byte {
	t.Helper()
	out, err := netpolicy.GeneratePolicies("crea-map", "crea-map", creaMapDeclared)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestApplyNetworkPoliciesGuardedRefusesNarrowing(t *testing.T) {
	cluster := &fakeNetworkPolicyCluster{
		live:      []networkingv1.NetworkPolicy{creaMapLiveEgress()},
		workloads: []netpolicy.Workload{{Kind: "Job", Name: "crea-map-migrate", Labels: map[string]string{}}},
	}
	view, err := applyNetworkPoliciesGuarded(context.Background(), cluster, "crea-map", generatedCreaMap(t), creaMapDeclared, false)
	if err == nil {
		t.Fatal("narrowing plan was applied without the opt-in")
	}
	if cluster.applied != 0 {
		t.Fatalf("applied %d policies, want 0", cluster.applied)
	}
	if view == nil || !view.Blocked || view.Applied {
		t.Fatalf("view = %+v, want blocked and not applied", view)
	}
	for _, want := range []string{"crea-map-egress: podSelector {} → app=crea-map", "Job/crea-map-migrate loses egress", "allow_netpol_narrowing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}

func TestApplyNetworkPoliciesGuardedAppliesWithOptIn(t *testing.T) {
	cluster := &fakeNetworkPolicyCluster{live: []networkingv1.NetworkPolicy{creaMapLiveEgress()}}
	view, err := applyNetworkPoliciesGuarded(context.Background(), cluster, "crea-map", generatedCreaMap(t), creaMapDeclared, true)
	if err != nil {
		t.Fatalf("opt-in apply failed: %v", err)
	}
	if cluster.applied != 4 || !view.Applied || view.Blocked || !view.NarrowingAllowed || !view.Narrows() {
		t.Fatalf("applied=%d view=%+v, want 4 applied with the findings kept", cluster.applied, view)
	}
}

// Without the live policies the guard cannot tell a no-op from an outage.
func TestApplyNetworkPoliciesGuardedFailsClosedWhenLiveUnreadable(t *testing.T) {
	cluster := &fakeNetworkPolicyCluster{listErr: errors.New("forbidden")}
	_, err := applyNetworkPoliciesGuarded(context.Background(), cluster, "crea-map", generatedCreaMap(t), creaMapDeclared, true)
	if err == nil || !strings.Contains(err.Error(), "nothing was applied") || cluster.applied != 0 {
		t.Fatalf("err=%v applied=%d, want a refusal and no apply even with the opt-in", err, cluster.applied)
	}
}

// The step result and the response carry the refusal and the diff.
func TestNetworkPolicyRefusalReachesStepAndResponse(t *testing.T) {
	cluster := &fakeNetworkPolicyCluster{
		live:      []networkingv1.NetworkPolicy{creaMapLiveEgress()},
		notes:     []string{"could not list CronJobs in crea-map (forbidden): the workload-level check is incomplete"},
		workloads: []netpolicy.Workload{{Kind: "Deployment", Name: "crea-map", Labels: map[string]string{"app": "crea-map"}}},
	}
	h := &Handler{logger: newNopLogger()}
	var steps []stepResult
	view, err := applyNetworkPoliciesGuarded(context.Background(), cluster, "crea-map", generatedCreaMap(t), creaMapDeclared, false)
	h.recordStep(context.Background(), &steps, networkPolicyStep, false, err)

	if steps[0].Status != "failed" || !strings.Contains(steps[0].Detail, "refused") {
		t.Fatalf("step = %+v, want failed with the refusal", steps[0])
	}
	if computeOnboardStatus(steps) == "completed" {
		t.Fatal("a refused network_policies step left the onboarding 'completed'")
	}
	raw, mErr := json.Marshal(view)
	if mErr != nil {
		t.Fatal(mErr)
	}
	body := string(raw)
	for _, want := range []string{`"blocked":true`, `"diff":[`, `"refusals":[`, `"kind":"access_lost"`, `"notes":["could not list CronJobs`, "pgbouncer"} {
		if !strings.Contains(body, want) {
			t.Errorf("network_policy_plan JSON missing %s:\n%s", want, body)
		}
	}
}
