package api

// Whether a failed canary probe says anything about the backend at all.
//
// probeTunnelBackend dials the backend from switchyard's own pod. cloudflared
// does not live there: it dials from its own namespace, and a tenant namespace
// with a default-deny NetworkPolicy admits exactly that namespace. So for a
// NetworkPolicy-protected backend the probe fails however healthy the backend
// is, and the canary used to read that as "nothing answers" and revert. Two
// consequences, both observed in production:
//
//   - a CORRECT rewrite, or a first-time add, into a protected namespace could
//     never stick: every one was reverted or withdrawn seconds after it landed;
//   - every such write became a flip-and-revert pair of whole-config writes,
//     which is two chances per hostname to lose a concurrent write.
//
// Probing from cloudflared's vantage would need a pod there to exec into or a
// public round-trip through the edge, neither of which this code path has. What
// it does have is read access to the facts that decide the question: the
// backend's pods and the NetworkPolicies that select them. So a failed probe is
// treated as INCONCLUSIVE — the rule is kept and a warning logged — only when
// both of these are positively established:
//
//  1. the Service selects at least one Ready pod (a Service with no Ready pods
//     is broken from every vantage, so that still reverts); and
//  2. for every one of those pods, the Ingress NetworkPolicies selecting it
//     provably do not admit switchyard's namespace.
//
// Everything this cannot decide falls back to the previous behaviour, revert:
// no Kubernetes client, a read that fails, a selector-less Service, an unknown
// own namespace, an ipBlock peer, an unparseable selector. Each approximation
// is chosen so that getting it wrong costs a revert (the old behaviour) rather
// than keeping a broken rule. Ports on a rule are ignored for the same reason:
// counting a port-restricted rule as admitting only ever makes a revert MORE
// likely.

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
)

// serviceAccountNamespaceFile is where every in-cluster pod can read its own
// namespace. A variable only so tests can point it elsewhere.
var serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// canaryProberNamespace returns the namespace the canary probe dials from:
// POD_NAMESPACE when set (downward API), otherwise the service account
// namespace file. Empty when neither is available (e.g. running out of
// cluster), which makes every failed probe count as a real failure.
func canaryProberNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); ns != "" {
		return ns
	}
	raw, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// canaryVantageBlocked reports whether a failed probe of spec is explained by
// NetworkPolicy refusing switchyard's namespace while the backend has Ready
// pods. The string is a human-readable account of the decision either way.
//
// A true return means "do not revert": the probe was not a representative
// test of the route. A false return means the failure stands.
func (h *Handler) canaryVantageBlocked(ctx context.Context, spec *services.RouteSpec) (bool, string) {
	if spec == nil {
		return false, "no route spec"
	}
	if h == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil {
		return false, "no Kubernetes client to check the backend's pods and NetworkPolicies"
	}
	prober := canaryProberNamespace()
	if prober == "" {
		return false, "switchyard's own namespace is unknown, so whether NetworkPolicy admits the probe cannot be decided"
	}
	if prober == spec.ServiceNamespace {
		// Same-namespace policies are routinely pod-scoped, and this code does
		// not know its own pod's labels. Nothing to establish positively.
		return false, "the probe runs in the backend's own namespace"
	}

	kube := h.k8sClient.Kube()
	svc, err := kube.CoreV1().Services(spec.ServiceNamespace).Get(ctx, spec.ServiceName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Sprintf("could not read Service %s/%s: %v", spec.ServiceNamespace, spec.ServiceName, err)
	}
	if len(svc.Spec.Selector) == 0 {
		return false, "the Service has no selector, so its backing pods cannot be identified"
	}

	pods, err := kube.CoreV1().Pods(spec.ServiceNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
	})
	if err != nil {
		return false, fmt.Sprintf("could not list the Service's pods: %v", err)
	}
	ready := readyPods(pods.Items)
	if len(ready) == 0 {
		return false, "the Service selects no Ready pod, so nothing can answer from any vantage"
	}

	policies, err := kube.NetworkingV1().NetworkPolicies(spec.ServiceNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Sprintf("could not list NetworkPolicies in %s: %v", spec.ServiceNamespace, err)
	}

	proberLabels := labels.Set{"kubernetes.io/metadata.name": prober}
	if ns, nsErr := kube.CoreV1().Namespaces().Get(ctx, prober, metav1.GetOptions{}); nsErr == nil {
		for key, value := range ns.Labels {
			proberLabels[key] = value
		}
	}

	blockingPolicies := map[string]struct{}{}
	for i := range ready {
		selecting, admitted := ingressAdmitsNamespace(policies.Items, ready[i].Labels, prober, proberLabels)
		if admitted || len(selecting) == 0 {
			return false, fmt.Sprintf("NetworkPolicy does not provably refuse namespace %q to pod %s", prober, ready[i].Name)
		}
		for _, name := range selecting {
			blockingPolicies[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(blockingPolicies))
	for name := range blockingPolicies {
		names = append(names, name)
	}
	sort.Strings(names)
	return true, fmt.Sprintf(
		"%d Ready pod(s) back %s/%s, and the Ingress NetworkPolicies selecting them (%s) do not admit the probe's namespace %q; "+
			"cloudflared dials from its own namespace, so this probe could not have reached the backend whatever its health",
		len(ready), spec.ServiceNamespace, spec.ServiceName, strings.Join(names, ", "), prober)
}

// readyPods returns the pods that are Ready and not terminating.
func readyPods(pods []corev1.Pod) []corev1.Pod {
	out := make([]corev1.Pod, 0, len(pods))
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
				out = append(out, pod)
				break
			}
		}
	}
	return out
}

// ingressAdmitsNamespace evaluates the Ingress NetworkPolicies that select a
// pod against traffic from namespace prober.
//
// selecting lists the policies that isolate the pod for ingress; admitted is
// true when any rule of any of them COULD admit the prober. With no selecting
// policy the pod is not isolated and admits everything, which the caller
// reads from len(selecting) == 0.
func ingressAdmitsNamespace(
	policies []networkingv1.NetworkPolicy,
	podLabels map[string]string,
	prober string,
	proberLabels labels.Set,
) (selecting []string, admitted bool) {
	for i := range policies {
		policy := &policies[i]
		if !policyIsolatesIngress(policy) {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		if err != nil {
			// Cannot tell whether it selects this pod. Treat it as admitting,
			// which can only produce a revert.
			return append(selecting, policy.Name), true
		}
		if !selector.Matches(labels.Set(podLabels)) {
			continue
		}
		selecting = append(selecting, policy.Name)
		for _, rule := range policy.Spec.Ingress {
			if ingressRuleCouldAdmit(rule, policy.Namespace, prober, proberLabels) {
				return selecting, true
			}
		}
	}
	return selecting, false
}

// policyIsolatesIngress applies the API's defaulting: a policy with no
// policyTypes always affects ingress.
func policyIsolatesIngress(policy *networkingv1.NetworkPolicy) bool {
	if len(policy.Spec.PolicyTypes) == 0 {
		return true
	}
	for _, policyType := range policy.Spec.PolicyTypes {
		if policyType == networkingv1.PolicyTypeIngress {
			return true
		}
	}
	return false
}

// ingressRuleCouldAdmit reports whether one ingress rule could admit traffic
// from namespace prober. "Could" is deliberate: anything this cannot evaluate
// counts as admitting, because that direction only ever produces a revert.
func ingressRuleCouldAdmit(
	rule networkingv1.NetworkPolicyIngressRule,
	policyNamespace, prober string,
	proberLabels labels.Set,
) bool {
	if len(rule.From) == 0 {
		return true // every source
	}
	for _, peer := range rule.From {
		switch {
		case peer.IPBlock != nil:
			return true // pod IPs may fall inside it
		case peer.NamespaceSelector != nil:
			selector, err := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
			if err != nil || selector.Matches(proberLabels) {
				// A podSelector alongside would narrow this to some pods of
				// the namespace, which this code cannot evaluate; counted as
				// admitting.
				return true
			}
		case peer.PodSelector != nil:
			// Pods of the policy's own namespace only.
			if policyNamespace == prober {
				return true
			}
		}
	}
	return false
}
