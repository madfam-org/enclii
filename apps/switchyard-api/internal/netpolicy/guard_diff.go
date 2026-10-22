package netpolicy

import (
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
)

// pgbouncerPort is PgBouncer's port in the data namespace (see the
// "pgbouncer" egress block in generator.go).
const pgbouncerPort = "TCP/6432"

func isPgbouncerAccess(a access) bool {
	return a.port == pgbouncerPort || strings.Contains(a.peer, "app=pgbouncer")
}

// pgbouncerHint explains the most likely cause when the plan drops PgBouncer
// access. It is derived from the live policies only: the guard never reads a
// DATABASE_URL or any other Secret value to learn where the service connects.
func pgbouncerHint(lostPgbouncer bool, spec NetworkSpec) string {
	if !lostPgbouncer {
		return ""
	}
	for _, svc := range spec.Services {
		if svc.hasEgress("pgbouncer") {
			return ""
		}
	}
	return "live egress allows PgBouncer (TCP 6432) but no network.services[] entry in enclii.yaml declares `pgbouncer` egress; if the service's DATABASE_URL points at PgBouncer, add `pgbouncer` to its egress list instead of opting in"
}

func createChange(g networkingv1.NetworkPolicy) PolicyChange {
	diff := []string{
		"podSelector: " + selectorString(&g.Spec.PodSelector),
		"policyTypes: " + typesString(effectiveTypes(g)),
	}
	for _, dir := range []networkingv1.PolicyType{networkingv1.PolicyTypeEgress, networkingv1.PolicyTypeIngress} {
		if !hasType(g, dir) {
			continue
		}
		rules := policyAccess(g, dir)
		if len(rules) == 0 {
			diff = append(diff, fmt.Sprintf("+ %s: none (denies all %s for the selected pods)", lower(dir), lower(dir)))
		}
		for _, a := range rules {
			diff = append(diff, fmt.Sprintf("+ %s %s", lower(dir), a))
		}
	}
	return PolicyChange{Name: g.Name, Action: ActionCreate, Diff: diff}
}

func diffPolicy(l, g networkingv1.NetworkPolicy) PolicyChange {
	var diff []string
	if before, after := selectorString(&l.Spec.PodSelector), selectorString(&g.Spec.PodSelector); before != after {
		diff = append(diff, fmt.Sprintf("podSelector: %s → %s", before, after))
	}
	if before, after := typesString(effectiveTypes(l)), typesString(effectiveTypes(g)); before != after {
		diff = append(diff, fmt.Sprintf("policyTypes: %s → %s", before, after))
	}
	for _, dir := range []networkingv1.PolicyType{networkingv1.PolicyTypeEgress, networkingv1.PolicyTypeIngress} {
		before, after := policyAccess(l, dir), policyAccess(g, dir)
		for _, a := range before {
			if !containsAccess(after, a) {
				diff = append(diff, fmt.Sprintf("- %s %s", lower(dir), a))
			}
		}
		for _, a := range after {
			if !containsAccess(before, a) {
				diff = append(diff, fmt.Sprintf("+ %s %s", lower(dir), a))
			}
		}
	}
	if len(diff) == 0 {
		return PolicyChange{Name: g.Name, Action: ActionUnchanged}
	}
	return PolicyChange{Name: g.Name, Action: ActionUpdate, Diff: diff}
}

func containsAccess(set []access, a access) bool {
	for _, b := range set {
		if b == a {
			return true
		}
	}
	return false
}

func typesString(types []networkingv1.PolicyType) string {
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = string(t)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func lower(t networkingv1.PolicyType) string { return strings.ToLower(string(t)) }
