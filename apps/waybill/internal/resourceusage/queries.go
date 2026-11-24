package resourceusage

import (
	"fmt"
	"time"
)

// Measure names one per-slot quantity the capture reads.
type Measure int

// Per-container measures (labels namespace, pod, container, node).
const (
	CPUUsed Measure = iota // cores, 5-minute rate of container_cpu_usage_seconds_total
	MemUsed                // bytes, container_memory_working_set_bytes
	CPURequest
	CPULimit
	MemRequest
	MemLimit
	numContainerMeasures
)

// Per-node measures (label node).
const (
	NodeCPUUsed Measure = iota // cores, the node's root cgroup
	NodeMemUsed                // bytes, the node's root cgroup working set
	NodeCPUAllocatable
	NodeMemAllocatable
	numNodeMeasures
)

// Queries builds the PromQL the capture runs. Every container query returns
// labels namespace, pod, container and node; every node query returns node.
type Queries struct {
	CadvisorJob string
	KSMJob      string
}

// cadvisor container series: the pod-level cgroup (container="") and the
// pause container are excluded, or every pod would be counted twice.
func (q Queries) cadvisor(metric string) string {
	return fmt.Sprintf(`%s{job=%q,container!="",container!="POD",pod!=""}`, metric, q.CadvisorJob)
}

// The cadvisor job sets instance to the node name (scraped through the API
// server's node proxy); copy it into node so both exporters share one label.
func nodeFromInstance(expr string) string {
	return fmt.Sprintf(`label_replace(%s, "node", "$1", "instance", "(.+)")`, expr)
}

// Requests and limits reserve capacity only while the pod is Pending (bound)
// or Running. kube-state-metrics keeps reporting them for finished Job pods
// until they are deleted, which would bill a CronJob's whole history as
// requested; keep only live pods, and only pods bound to a node.
func (q Queries) liveResource(metric, resource, unit string) string {
	return fmt.Sprintf(
		`max by (namespace, pod, container, node) (%s{job=%q,resource=%q,unit=%q,node!=""}) `+
			`and on (namespace, pod) (max by (namespace, pod) (kube_pod_status_phase{job=%q,phase=~"Pending|Running"}) == 1)`,
		metric, q.KSMJob, resource, unit, q.KSMJob)
}

// Container returns the range query for a per-container measure.
func (q Queries) Container(m Measure) string {
	switch m {
	case CPUUsed:
		return fmt.Sprintf(`sum by (namespace, pod, container, node) (%s)`,
			nodeFromInstance(fmt.Sprintf(`rate(%s[5m])`, q.cadvisor("container_cpu_usage_seconds_total"))))
	case MemUsed:
		// max, not sum: across a container restart the old and new series
		// briefly coexist, and two working sets of one container are not
		// additive.
		return fmt.Sprintf(`max by (namespace, pod, container, node) (%s)`,
			nodeFromInstance(q.cadvisor("container_memory_working_set_bytes")))
	case CPURequest:
		return q.liveResource("kube_pod_container_resource_requests", "cpu", "core")
	case CPULimit:
		return q.liveResource("kube_pod_container_resource_limits", "cpu", "core")
	case MemRequest:
		return q.liveResource("kube_pod_container_resource_requests", "memory", "byte")
	case MemLimit:
		return q.liveResource("kube_pod_container_resource_limits", "memory", "byte")
	}
	panic(fmt.Sprintf("unknown container measure %d", m))
}

// Node returns the range query for a per-node measure.
func (q Queries) Node(m Measure) string {
	root := func(metric string) string {
		return fmt.Sprintf(`%s{job=%q,id="/"}`, metric, q.CadvisorJob)
	}
	switch m {
	case NodeCPUUsed:
		return fmt.Sprintf(`sum by (node) (%s)`,
			nodeFromInstance(fmt.Sprintf(`rate(%s[5m])`, root("container_cpu_usage_seconds_total"))))
	case NodeMemUsed:
		return fmt.Sprintf(`max by (node) (%s)`, nodeFromInstance(root("container_memory_working_set_bytes")))
	case NodeCPUAllocatable:
		return fmt.Sprintf(`max by (node) (kube_node_status_allocatable{job=%q,resource="cpu",unit="core"})`, q.KSMJob)
	case NodeMemAllocatable:
		return fmt.Sprintf(`max by (node) (kube_node_status_allocatable{job=%q,resource="memory",unit="byte"})`, q.KSMJob)
	}
	panic(fmt.Sprintf("unknown node measure %d", m))
}

func rangeSeconds(d Day) string {
	return fmt.Sprintf("%ds", int64(d.End.Sub(d.Start)/time.Second))
}

// Probe is an instant query at day end: how many node root cgroups had any
// sample in the day. No result means Prometheus holds nothing for the day.
func (q Queries) Probe(d Day) string {
	return fmt.Sprintf(`count(max_over_time(container_memory_working_set_bytes{job=%q,id="/"}[%s]))`,
		q.CadvisorJob, rangeSeconds(d))
}

// PodOwners is an instant query at day end: every pod seen during the day,
// with its owner reference.
func (q Queries) PodOwners(d Day) string {
	return fmt.Sprintf(`max by (namespace, pod, owner_kind, owner_name) (max_over_time(kube_pod_owner{job=%q}[%s]))`,
		q.KSMJob, rangeSeconds(d))
}

// ReplicaSetOwners resolves ReplicaSet -> Deployment.
func (q Queries) ReplicaSetOwners(d Day) string {
	return fmt.Sprintf(`max by (namespace, replicaset, owner_kind, owner_name) (max_over_time(kube_replicaset_owner{job=%q}[%s]))`,
		q.KSMJob, rangeSeconds(d))
}

// JobOwners resolves Job -> CronJob.
func (q Queries) JobOwners(d Day) string {
	return fmt.Sprintf(`max by (namespace, job_name, owner_kind, owner_name) (max_over_time(kube_job_owner{job=%q}[%s]))`,
		q.KSMJob, rangeSeconds(d))
}
