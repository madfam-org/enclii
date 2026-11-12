package netpolicy

import (
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
)

// The live crea-map namespace before 2026-10-21 11:04Z, reduced to what the
// incident needs: the onboarding default-deny, a namespace-wide egress policy
// that allowed PgBouncer, and an ingress policy that also admitted pods of
// the namespace (the alarm probe).
const creaMapLiveBefore = `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
  egress:
    - ports:
        - {protocol: UDP, port: 53}
        - {protocol: TCP, port: 53}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: crea-map-egress
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to: []
      ports:
        - {protocol: TCP, port: 443}
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: data
          podSelector:
            matchLabels:
              app: pgbouncer
      ports:
        - {protocol: TCP, port: 6432}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: crea-map-ingress
spec:
  podSelector:
    matchLabels:
      app: crea-map
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: cloudflare-tunnel
        - podSelector: {}
      ports:
        - {protocol: TCP, port: 3000}
`

var creaMapWorkloads = []Workload{
	{Kind: "Deployment", Name: "crea-map", Labels: map[string]string{"app": "crea-map"}},
	{Kind: "CronJob", Name: "crea-map-recordatorios", Labels: map[string]string{"app": "crea-map-recordatorios"}},
	{Kind: "Job", Name: "crea-map-migrate", Labels: map[string]string{}},
}

// What enclii.yaml declared: postgres, never pgbouncer.
var creaMapSpec = NetworkSpec{Services: []ServiceSpec{{
	Name: "crea-map", Label: "app", Port: 3000,
	Ingress: []string{"cloudflare-tunnel"},
	Egress:  []string{"dns", "https", "postgres"},
}}}

func mustParse(t *testing.T, yaml string) []networkingv1.NetworkPolicy {
	t.Helper()
	policies, err := ParsePolicies([]byte(yaml))
	if err != nil {
		t.Fatalf("ParsePolicies: %v", err)
	}
	return policies
}

func mustGenerate(t *testing.T, namespace string, spec NetworkSpec) []networkingv1.NetworkPolicy {
	t.Helper()
	out, err := GeneratePolicies(namespace, namespace, spec)
	if err != nil {
		t.Fatalf("GeneratePolicies: %v", err)
	}
	return mustParse(t, string(out))
}

func findingsOfKind(plan *Plan, kind string) []Finding {
	var out []Finding
	for _, f := range plan.Refusals {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

func TestParsePoliciesReadsEveryGeneratedPolicy(t *testing.T) {
	policies := mustGenerate(t, "crea-map", creaMapSpec)
	var names []string
	for _, p := range policies {
		names = append(names, p.Name)
	}
	want := "default-deny-ingress,default-deny-egress,crea-map-ingress,crea-map-egress"
	if strings.Join(names, ",") != want {
		t.Fatalf("parsed %v, want %s", names, want)
	}
}

// The 2026-10-21 regeneration must be refused, naming what would be lost.
func TestComputePlanRefusesTheCreaMapRegeneration(t *testing.T) {
	plan := ComputePlan("crea-map", mustParse(t, creaMapLiveBefore), mustGenerate(t, "crea-map", creaMapSpec), creaMapWorkloads, creaMapSpec)

	if !plan.Narrows() {
		t.Fatalf("plan does not narrow; lines:\n%s", strings.Join(plan.Lines(), "\n"))
	}
	narrowed := findingsOfKind(plan, FindingSelectorNarrowed)
	if len(narrowed) != 1 || narrowed[0].Policy != "crea-map-egress" || !strings.Contains(narrowed[0].Message, "podSelector {} → app=crea-map") {
		t.Errorf("selector findings = %+v, want crea-map-egress {} → app=crea-map", narrowed)
	}

	lost := map[string]string{}
	for _, f := range findingsOfKind(plan, FindingAccessLost) {
		lost[f.Workload+" "+strings.SplitN(strings.SplitN(f.Message, " loses ", 2)[1], ":", 2)[0]] = f.Message
	}
	for _, want := range []struct{ key, contains string }{
		{"Deployment/crea-map egress", "TCP/6432 ↔ namespaces{kubernetes.io/metadata.name=data} pods{app=pgbouncer}"},
		{"CronJob/crea-map-recordatorios egress", "TCP/443 ↔ anywhere"},
		{"Job/crea-map-migrate egress", "TCP/6432"},
		{"Deployment/crea-map ingress", "TCP/3000 ↔ pods{}"},
	} {
		msg, ok := lost[want.key]
		if !ok {
			t.Errorf("no access_lost finding for %s; findings: %v", want.key, lost)
			continue
		}
		if !strings.Contains(msg, want.contains) {
			t.Errorf("%s finding = %q, want it to contain %q", want.key, msg, want.contains)
		}
	}

	// default-deny already restricted both directions namespace-wide, so the
	// two generated deny policies take nothing away by themselves.
	if dd := findingsOfKind(plan, FindingDefaultDenyAdded); len(dd) != 0 {
		t.Errorf("default_deny_added = %+v, want none while default-deny exists", dd)
	}

	if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], "`pgbouncer`") {
		t.Errorf("warnings = %v, want the PgBouncer hint", plan.Warnings)
	}

	err := plan.RefusalError()
	if err == nil || !strings.Contains(err.Error(), "--allow-netpol-narrowing") || !strings.Contains(err.Error(), "nothing was applied") {
		t.Fatalf("RefusalError() = %v, want the opt-in and 'nothing was applied'", err)
	}
}

// With pgbouncer declared and the live state equal to the generated one, a
// re-run is a no-op and must pass.
func TestComputePlanAllowsAnIdempotentRerun(t *testing.T) {
	spec := NetworkSpec{Services: []ServiceSpec{{
		Name: "crea-map", Port: 3000, Ingress: []string{"cloudflare-tunnel"},
		Egress: []string{"dns", "https", "pgbouncer"},
	}}}
	generated := mustGenerate(t, "crea-map", spec)
	plan := ComputePlan("crea-map", generated, mustGenerate(t, "crea-map", spec), creaMapWorkloads, spec)

	if plan.Narrows() {
		t.Fatalf("idempotent re-run refused: %v", plan.RefusalError())
	}
	for _, c := range plan.Changes {
		if c.Action != ActionUnchanged {
			t.Errorf("%s action = %s %v, want unchanged", c.Name, c.Action, c.Diff)
		}
	}
}

// A brand-new namespace (only the onboarding default-deny, nothing running
// yet) is created without an opt-in.
func TestComputePlanAllowsFirstOnboarding(t *testing.T) {
	live := mustParse(t, strings.SplitN(creaMapLiveBefore, "---", 2)[0])
	plan := ComputePlan("crea-map", live, mustGenerate(t, "crea-map", creaMapSpec), nil, creaMapSpec)
	if plan.Narrows() {
		t.Fatalf("first onboarding refused: %v", plan.RefusalError())
	}
	creates := 0
	for _, c := range plan.Changes {
		if c.Action == ActionCreate {
			creates++
		}
	}
	if creates != 4 {
		t.Errorf("creates = %d, want 4; changes %+v", creates, plan.Changes)
	}
}

func TestComputePlanRefusesNewNamespaceWideDeny(t *testing.T) {
	live := mustParse(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: web-egress
spec:
  podSelector:
    matchLabels:
      app: web
  policyTypes: [Egress]
  egress:
    - to: []
`)
	workloads := []Workload{{Kind: "Deployment", Name: "worker", Labels: map[string]string{"app": "worker"}}}
	plan := ComputePlan("demo", live, mustGenerate(t, "demo", NetworkSpec{}), workloads, NetworkSpec{})

	dd := findingsOfKind(plan, FindingDefaultDenyAdded)
	if len(dd) != 2 {
		t.Fatalf("default_deny_added = %+v, want ingress and egress", dd)
	}
	lost := findingsOfKind(plan, FindingAccessLost)
	if len(lost) != 2 || !strings.Contains(lost[0].Message, "no egress policy selected it before") {
		t.Errorf("access_lost = %+v, want worker losing unrestricted egress and ingress", lost)
	}
}

// Replacing a narrow allow with a broader one is not narrowing.
func TestComputePlanAcceptsBroaderAccess(t *testing.T) {
	live := mustParse(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: api-egress
spec:
  podSelector:
    matchLabels: {app: api}
  policyTypes: [Egress]
  egress:
    - to:
        - podSelector:
            matchLabels: {app: pgbouncer}
      ports:
        - {protocol: TCP, port: 6432}
`)
	generated := mustParse(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: api-egress
spec:
  podSelector:
    matchLabels: {app: api}
  policyTypes: [Egress]
  egress:
    - ports:
        - {protocol: TCP}
`)
	workloads := []Workload{{Kind: "Deployment", Name: "api", Labels: map[string]string{"app": "api"}}}
	plan := ComputePlan("demo", live, generated, workloads, NetworkSpec{})
	if plan.Narrows() {
		t.Fatalf("broader access refused: %v", plan.RefusalError())
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Action != ActionUpdate {
		t.Fatalf("changes = %+v, want one update", plan.Changes)
	}
	diff := strings.Join(plan.Changes[0].Diff, "\n")
	if !strings.Contains(diff, "- egress TCP/6432") || !strings.Contains(diff, "+ egress TCP/* ↔ anywhere") {
		t.Errorf("diff = %q, want the removed and added rule", diff)
	}
}

// A selector change that drops a running workload is narrowing even when
// neither selector is {}.
func TestComputePlanRefusesSelectorThatDropsAWorkload(t *testing.T) {
	live := mustParse(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: crea-map-egress
spec:
  podSelector:
    matchLabels: {app.kubernetes.io/part-of: crea-map}
  policyTypes: [Egress]
  egress:
    - to: []
`)
	generated := mustParse(t, `
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: crea-map-egress
spec:
  podSelector:
    matchLabels: {app: crea-map}
  policyTypes: [Egress]
  egress:
    - to: []
`)
	workloads := []Workload{
		{Kind: "Deployment", Name: "crea-map", Labels: map[string]string{"app": "crea-map", "app.kubernetes.io/part-of": "crea-map"}},
		{Kind: "CronJob", Name: "alarms", Labels: map[string]string{"app": "alarms", "app.kubernetes.io/part-of": "crea-map"}},
	}
	plan := ComputePlan("crea-map", live, generated, workloads, NetworkSpec{})
	narrowed := findingsOfKind(plan, FindingSelectorNarrowed)
	if len(narrowed) != 1 || !strings.Contains(narrowed[0].Message, "drops CronJob/alarms") || strings.Contains(narrowed[0].Message, "Deployment/crea-map") {
		t.Fatalf("selector findings = %+v, want only CronJob/alarms dropped", narrowed)
	}
}

func TestPlanLinesShowEveryPolicy(t *testing.T) {
	plan := ComputePlan("crea-map", mustParse(t, creaMapLiveBefore), mustGenerate(t, "crea-map", creaMapSpec), creaMapWorkloads, creaMapSpec)
	text := strings.Join(plan.Lines(), "\n")
	for _, want := range []string{
		"= default-deny (kept)",
		"+ default-deny-egress (create)",
		"~ crea-map-egress (update)",
		"podSelector: {} → app=crea-map",
		"- egress TCP/6432 ↔ namespaces{kubernetes.io/metadata.name=data} pods{app=pgbouncer}",
		"+ egress TCP/5432 ↔ namespaces{kubernetes.io/metadata.name=data} pods{app=postgres}",
		"REFUSE ",
		"WARN   ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("plan lines missing %q:\n%s", want, text)
		}
	}
}
