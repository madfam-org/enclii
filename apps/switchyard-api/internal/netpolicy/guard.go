package netpolicy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	sigsyaml "sigs.k8s.io/yaml"
)

// The narrowing guard (2026-10-21 crea-map incident).
//
// `enclii onboard ensure` regenerated crea-map's policies from enclii.yaml in
// repair mode, with every step "ok": crea-map-egress went from podSelector {}
// to app=crea-map with postgres only on TCP 5432, crea-map-ingress was
// narrowed to cloudflare-tunnel, and two default-deny policies were added. The
// runtime used PgBouncer on TCP 6432, which enclii.yaml never declared, and
// the MAP returned 502 for 5h42m. CronJobs, the unlabeled PreSync migrate Job
// and the alarm probe lost their connectivity too.
//
// ComputePlan compares the live policies with the generated ones before
// anything is applied and reports, in Refusals, every change that takes
// access away from something that runs in the namespace today. The caller
// refuses to apply a plan with refusals unless the operator opts in.

// Finding kinds. Each one blocks an apply without the explicit opt-in.
const (
	// FindingSelectorNarrowed: an existing policy would select fewer pods.
	FindingSelectorNarrowed = "selector_narrowed"
	// FindingAccessLost: a workload would lose an allowed peer or port.
	FindingAccessLost = "access_lost"
	// FindingDefaultDenyAdded: a namespace-wide deny where none existed.
	FindingDefaultDenyAdded = "default_deny_added"
)

// Change actions in a plan.
const (
	ActionCreate    = "create"
	ActionUpdate    = "update"
	ActionUnchanged = "unchanged"
	ActionKept      = "kept"
)

const anyValue = "*"

// Workload is one set of pod labels that exists in the namespace: a
// controller's pod template or a pod without a listed controller. Only labels
// are read; never env values or Secrets.
type Workload struct {
	Kind   string            `json:"kind"`
	Name   string            `json:"name"`
	Labels map[string]string `json:"-"`
}

func (w Workload) ref() string { return w.Kind + "/" + w.Name }

// Finding is one change the guard refuses without the opt-in.
type Finding struct {
	Kind     string `json:"kind"`
	Policy   string `json:"policy,omitempty"`
	Workload string `json:"workload,omitempty"`
	Message  string `json:"message"`
}

// PolicyChange is the diff of one NetworkPolicy.
type PolicyChange struct {
	Name   string   `json:"name"`
	Action string   `json:"action"`
	Diff   []string `json:"diff,omitempty"`
}

// Plan is what applying the generated policies would change.
type Plan struct {
	Namespace        string         `json:"namespace"`
	Changes          []PolicyChange `json:"changes"`
	Refusals         []Finding      `json:"refusals,omitempty"`
	Warnings         []string       `json:"warnings,omitempty"`
	Notes            []string       `json:"notes,omitempty"`
	WorkloadsChecked int            `json:"workloads_checked"`
}

// Narrows reports whether the plan takes access away from live workloads.
func (p *Plan) Narrows() bool { return p != nil && len(p.Refusals) > 0 }

// RefusalError is the error a caller returns when it refuses the plan.
func (p *Plan) RefusalError() error {
	if !p.Narrows() {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "refused: the policies generated from enclii.yaml would narrow live network access in namespace %q (%d finding(s)); nothing was applied:", p.Namespace, len(p.Refusals))
	for _, f := range p.Refusals {
		fmt.Fprintf(&b, "\n  - %s", f.Message)
	}
	b.WriteString("\nDeclare the access in enclii.yaml (network.services[].egress/ingress or network.custom), or, after reviewing the diff, re-run with --allow-netpol-narrowing (request field allow_netpol_narrowing)")
	return fmt.Errorf("%s", b.String())
}

// Lines renders the plan as human-readable diff lines.
func (p *Plan) Lines() []string {
	if p == nil {
		return nil
	}
	var out []string
	for _, c := range p.Changes {
		out = append(out, fmt.Sprintf("%s %s (%s)", actionMarker(c.Action), c.Name, c.Action))
		for _, d := range c.Diff {
			out = append(out, "    "+d)
		}
	}
	for _, f := range p.Refusals {
		out = append(out, "REFUSE "+f.Message)
	}
	for _, w := range p.Warnings {
		out = append(out, "WARN   "+w)
	}
	for _, n := range p.Notes {
		out = append(out, "NOTE   "+n)
	}
	return out
}

func actionMarker(action string) string {
	switch action {
	case ActionCreate:
		return "+"
	case ActionUpdate:
		return "~"
	default:
		return "="
	}
}

// ParsePolicies decodes the multi-document YAML that GeneratePolicies emits.
func ParsePolicies(data []byte) ([]networkingv1.NetworkPolicy, error) {
	var out []networkingv1.NetworkPolicy
	var current []string
	flush := func() error {
		doc := strings.TrimSpace(strings.Join(current, "\n"))
		current = nil
		if doc == "" {
			return nil
		}
		raw, err := sigsyaml.YAMLToJSON([]byte(doc))
		if err != nil {
			return fmt.Errorf("decode generated NetworkPolicy: %w", err)
		}
		if s := strings.TrimSpace(string(raw)); s == "" || s == "null" || s == "{}" {
			return nil
		}
		var np networkingv1.NetworkPolicy
		if err := json.Unmarshal(raw, &np); err != nil {
			return fmt.Errorf("decode generated NetworkPolicy: %w", err)
		}
		if np.Kind == "NetworkPolicy" && np.Name != "" {
			out = append(out, np)
		}
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "---" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		current = append(current, line)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return out, nil
}

// ComputePlan diffs the live policies against the generated ones. The apply
// only creates and updates (it never deletes), so live policies whose names
// are not generated are kept and still count toward what workloads may reach.
func ComputePlan(namespace string, live, generated []networkingv1.NetworkPolicy, workloads []Workload, spec NetworkSpec) *Plan {
	plan := &Plan{Namespace: namespace, WorkloadsChecked: len(workloads)}
	liveByName := map[string]networkingv1.NetworkPolicy{}
	for _, p := range live {
		liveByName[p.Name] = p
	}
	generatedNames := map[string]bool{}
	after := make([]networkingv1.NetworkPolicy, 0, len(live)+len(generated))
	for _, g := range generated {
		generatedNames[g.Name] = true
		after = append(after, g)
		if l, ok := liveByName[g.Name]; ok {
			plan.Changes = append(plan.Changes, diffPolicy(l, g))
			plan.Refusals = append(plan.Refusals, selectorNarrowing(l, g, workloads)...)
		} else {
			plan.Changes = append(plan.Changes, createChange(g))
		}
	}
	for _, l := range live {
		if !generatedNames[l.Name] {
			after = append(after, l)
			plan.Changes = append(plan.Changes, PolicyChange{Name: l.Name, Action: ActionKept})
		}
	}
	sort.SliceStable(plan.Changes, func(i, j int) bool { return plan.Changes[i].Name < plan.Changes[j].Name })

	plan.Refusals = append(plan.Refusals, defaultDenyAdditions(live, generated, liveByName)...)
	lost, lostPgbouncer := accessLosses(live, after, workloads)
	plan.Refusals = append(plan.Refusals, lost...)
	if hint := pgbouncerHint(lostPgbouncer, spec); hint != "" {
		plan.Warnings = append(plan.Warnings, hint)
	}
	return plan
}

// --- selector and default-deny checks -------------------------------------

func selectorNarrowing(l, g networkingv1.NetworkPolicy, workloads []Workload) []Finding {
	before, after := selectorString(&l.Spec.PodSelector), selectorString(&g.Spec.PodSelector)
	if before == after {
		return nil
	}
	if before == "{}" {
		return []Finding{{Kind: FindingSelectorNarrowed, Policy: g.Name, Message: fmt.Sprintf(
			"%s: podSelector {} → %s; it stops applying to every pod without those labels", g.Name, after)}}
	}
	var dropped []string
	for _, w := range workloads {
		if selects(&l.Spec.PodSelector, w.Labels) && !selects(&g.Spec.PodSelector, w.Labels) {
			dropped = append(dropped, w.ref())
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	return []Finding{{Kind: FindingSelectorNarrowed, Policy: g.Name, Message: fmt.Sprintf(
		"%s: podSelector %s → %s drops %s", g.Name, before, after, strings.Join(dropped, ", "))}}
}

func defaultDenyAdditions(live, generated []networkingv1.NetworkPolicy, liveByName map[string]networkingv1.NetworkPolicy) []Finding {
	wide := map[networkingv1.PolicyType]bool{}
	for _, p := range live {
		if selectorString(&p.Spec.PodSelector) != "{}" {
			continue
		}
		for _, t := range effectiveTypes(p) {
			wide[t] = true
		}
	}
	var out []Finding
	for _, g := range generated {
		if _, exists := liveByName[g.Name]; exists || selectorString(&g.Spec.PodSelector) != "{}" {
			continue
		}
		for _, t := range effectiveTypes(g) {
			if wide[t] || len(policyAccess(g, t)) > 0 {
				continue
			}
			out = append(out, Finding{Kind: FindingDefaultDenyAdded, Policy: g.Name, Message: fmt.Sprintf(
				"%s: adds a namespace-wide %s deny; no live policy restricted %s for every pod before", g.Name, lower(t), lower(t))})
		}
	}
	return out
}

// --- effective access per workload ------------------------------------------

// access is one allowed (peer, port) pair; anyValue is a wildcard.
type access struct{ peer, port string }

func (a access) String() string {
	port := a.port
	if port == anyValue {
		port = "all ports"
	}
	peer := a.peer
	if peer == anyValue {
		peer = "anywhere"
	}
	return port + " ↔ " + peer
}

// accessLosses also reports whether any lost access was PgBouncer's.
func accessLosses(before, after []networkingv1.NetworkPolicy, workloads []Workload) ([]Finding, bool) {
	var out []Finding
	pgbouncer := false
	for _, w := range workloads {
		for _, dir := range []networkingv1.PolicyType{networkingv1.PolicyTypeEgress, networkingv1.PolicyTypeIngress} {
			wasIsolated, had := effectiveAccess(before, w.Labels, dir)
			_, has := effectiveAccess(after, w.Labels, dir)
			var lost []string
			for _, a := range had {
				if !coveredByAny(a, has) {
					lost = append(lost, a.String())
					pgbouncer = pgbouncer || (dir == networkingv1.PolicyTypeEgress && isPgbouncerAccess(a))
				}
			}
			if len(lost) == 0 {
				continue
			}
			msg := fmt.Sprintf("%s loses %s: %s", w.ref(), lower(dir), strings.Join(lost, "; "))
			if !wasIsolated {
				msg += fmt.Sprintf(" (no %s policy selected it before)", lower(dir))
			}
			out = append(out, Finding{Kind: FindingAccessLost, Workload: w.ref(), Message: msg})
		}
	}
	return out, pgbouncer
}

// effectiveAccess is what a pod with these labels may reach (egress) or be
// reached by (ingress). A pod no policy of that type selects is unrestricted.
func effectiveAccess(policies []networkingv1.NetworkPolicy, podLabels map[string]string, dir networkingv1.PolicyType) (bool, []access) {
	isolated := false
	var out []access
	seen := map[access]bool{}
	for _, p := range policies {
		if !hasType(p, dir) || !selects(&p.Spec.PodSelector, podLabels) {
			continue
		}
		isolated = true
		for _, a := range policyAccess(p, dir) {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	if !isolated {
		return false, []access{{peer: anyValue, port: anyValue}}
	}
	return true, out
}

func policyAccess(p networkingv1.NetworkPolicy, dir networkingv1.PolicyType) []access {
	if !hasType(p, dir) {
		return nil
	}
	var out []access
	if dir == networkingv1.PolicyTypeEgress {
		for _, r := range p.Spec.Egress {
			out = append(out, ruleAccess(r.To, r.Ports)...)
		}
		return out
	}
	for _, r := range p.Spec.Ingress {
		out = append(out, ruleAccess(r.From, r.Ports)...)
	}
	return out
}

func ruleAccess(peers []networkingv1.NetworkPolicyPeer, ports []networkingv1.NetworkPolicyPort) []access {
	peerKeys := []string{anyValue}
	if len(peers) > 0 {
		peerKeys = peerKeys[:0]
		for _, p := range peers {
			peerKeys = append(peerKeys, peerString(p))
		}
	}
	portKeys := []string{anyValue}
	if len(ports) > 0 {
		portKeys = portKeys[:0]
		for _, p := range ports {
			portKeys = append(portKeys, portString(p))
		}
	}
	var out []access
	for _, peer := range peerKeys {
		for _, port := range portKeys {
			out = append(out, access{peer: peer, port: port})
		}
	}
	return out
}

func coveredByAny(a access, set []access) bool {
	for _, b := range set {
		if (b.peer == anyValue || b.peer == a.peer) && portCovers(b.port, a.port) {
			return true
		}
	}
	return false
}

func portCovers(b, a string) bool {
	if b == anyValue || b == a {
		return true
	}
	if a == anyValue {
		return false
	}
	bProto, bPort, _ := strings.Cut(b, "/")
	aProto, _, _ := strings.Cut(a, "/")
	return bPort == anyValue && bProto == aProto
}

// --- formatting helpers ------------------------------------------------------

func peerString(p networkingv1.NetworkPolicyPeer) string {
	var parts []string
	if p.IPBlock != nil {
		s := "cidr " + p.IPBlock.CIDR
		if len(p.IPBlock.Except) > 0 {
			s += " except " + strings.Join(p.IPBlock.Except, ",")
		}
		parts = append(parts, s)
	}
	if p.NamespaceSelector != nil {
		parts = append(parts, "namespaces"+bracedSelector(p.NamespaceSelector))
	}
	if p.PodSelector != nil {
		parts = append(parts, "pods"+bracedSelector(p.PodSelector))
	}
	if len(parts) == 0 {
		return anyValue
	}
	return strings.Join(parts, " ")
}

func portString(p networkingv1.NetworkPolicyPort) string {
	proto := "TCP"
	if p.Protocol != nil {
		proto = string(*p.Protocol)
	}
	if p.Port == nil {
		return proto + "/" + anyValue
	}
	s := proto + "/" + p.Port.String()
	if p.EndPort != nil {
		s += fmt.Sprintf("-%d", *p.EndPort)
	}
	return s
}

// bracedSelector renders a peer selector; pods{} is every pod.
func bracedSelector(sel *metav1.LabelSelector) string {
	if s := selectorString(sel); s != "{}" {
		return "{" + s + "}"
	}
	return "{}"
}

func selectorString(sel *metav1.LabelSelector) string {
	if sel == nil || (len(sel.MatchLabels) == 0 && len(sel.MatchExpressions) == 0) {
		return "{}"
	}
	s, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return "<invalid selector>"
	}
	return s.String()
}

func selects(sel *metav1.LabelSelector, podLabels map[string]string) bool {
	s, err := metav1.LabelSelectorAsSelector(sel)
	if err != nil {
		return false
	}
	return s.Matches(labels.Set(podLabels))
}

// effectiveTypes follows the API defaulting: Ingress always, Egress when the
// policy has egress rules, unless policyTypes says otherwise.
func effectiveTypes(p networkingv1.NetworkPolicy) []networkingv1.PolicyType {
	if len(p.Spec.PolicyTypes) > 0 {
		return p.Spec.PolicyTypes
	}
	types := []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	if len(p.Spec.Egress) > 0 {
		types = append(types, networkingv1.PolicyTypeEgress)
	}
	return types
}

func hasType(p networkingv1.NetworkPolicy, t networkingv1.PolicyType) bool {
	for _, have := range effectiveTypes(p) {
		if have == t {
			return true
		}
	}
	return false
}
