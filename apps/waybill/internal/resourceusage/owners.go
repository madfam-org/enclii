package resourceusage

import "sort"

// Workload kinds the resolver emits besides the owners' own kinds.
const (
	// KindPod is a pod with no owner reference (or none kube-state-metrics
	// reported): the pod is its own workload.
	KindPod = "Pod"
	// KindNamespace aggregates every pod of a namespace on purpose (CI
	// runners): one row per node and container, so totals reconcile without
	// a row per ephemeral runner.
	KindNamespace = "Namespace"
)

// ephemeralRunnerKind is ARC's per-job runner object. Its pods are CI
// runners wherever they run.
const ephemeralRunnerKind = "EphemeralRunner"

type nsName struct{ namespace, name string }

type ownerRef struct{ kind, name string }

// Owners resolves a pod to its top-level workload from kube-state-metrics'
// owner series: Pod -> ReplicaSet -> Deployment and Pod -> Job -> CronJob.
// Other owners (StatefulSet, DaemonSet, CRDs such as a CNPG Cluster) are the
// workload themselves.
type Owners struct {
	pods        map[nsName]ownerRef
	replicaSets map[nsName]ownerRef
	jobs        map[nsName]ownerRef
	runnerNS    map[string]bool
}

// NewOwners builds a resolver from the three owner queries' results.
func NewOwners(pods, replicaSets, jobs []Series, runnerNamespaces []string) *Owners {
	o := &Owners{
		pods:        ownerIndex(pods, "pod"),
		replicaSets: ownerIndex(replicaSets, "replicaset"),
		jobs:        ownerIndex(jobs, "job_name"),
		runnerNS:    map[string]bool{},
	}
	for _, ns := range runnerNamespaces {
		o.runnerNS[ns] = true
	}
	return o
}

// ownerIndex keeps one owner per object. kube-state-metrics reports
// "<none>" for an object without owners; a real owner wins over it, and among
// several real owners the lexically first, so the choice is deterministic.
func ownerIndex(series []Series, nameLabel string) map[nsName]ownerRef {
	type cand struct {
		key nsName
		ref ownerRef
	}
	cands := make([]cand, 0, len(series))
	for _, s := range series {
		key := nsName{s.Labels["namespace"], s.Labels[nameLabel]}
		if key.name == "" {
			continue
		}
		cands = append(cands, cand{key, ownerRef{s.Labels["owner_kind"], s.Labels["owner_name"]}})
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i].ref, cands[j].ref
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.name < b.name
	})
	idx := make(map[nsName]ownerRef, len(cands))
	for _, c := range cands {
		prev, seen := idx[c.key]
		if !seen || (isNone(prev) && !isNone(c.ref)) {
			idx[c.key] = c.ref
		}
	}
	return idx
}

func isNone(r ownerRef) bool {
	return r.kind == "" || r.kind == "<none>" || r.name == "" || r.name == "<none>"
}

// Resolve returns the workload kind and name for a pod.
func (o *Owners) Resolve(namespace, pod string) (kind, name string) {
	if o.runnerNS[namespace] {
		return KindNamespace, namespace
	}
	ref, ok := o.pods[nsName{namespace, pod}]
	if !ok || isNone(ref) {
		return KindPod, pod
	}
	switch ref.kind {
	case ephemeralRunnerKind:
		return KindNamespace, namespace
	case "ReplicaSet":
		if up, ok := o.replicaSets[nsName{namespace, ref.name}]; ok && !isNone(up) {
			return up.kind, up.name
		}
	case "Job":
		if up, ok := o.jobs[nsName{namespace, ref.name}]; ok && !isNone(up) {
			return up.kind, up.name
		}
	}
	return ref.kind, ref.name
}
