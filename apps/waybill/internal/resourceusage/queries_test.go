package resourceusage

import (
	"strings"
	"testing"
)

var testQueries = Queries{CadvisorJob: "kubernetes-cadvisor", KSMJob: "kube-state-metrics"}

func TestQueries_ContainerUsageExcludesPodCgroupsAndPinsTheJob(t *testing.T) {
	for _, m := range []Measure{CPUUsed, MemUsed} {
		q := testQueries.Container(m)
		for _, want := range []string{
			`job="kubernetes-cadvisor"`, `container!=""`, `container!="POD"`, `pod!=""`,
			`label_replace(`, `"node", "$1", "instance", "(.+)"`,
			`by (namespace, pod, container, node)`,
		} {
			if !strings.Contains(q, want) {
				t.Errorf("measure %d: %s\n  missing %s", m, q, want)
			}
		}
	}
	if q := testQueries.Container(CPUUsed); !strings.HasPrefix(q, "sum by") || !strings.Contains(q, "rate(container_cpu_usage_seconds_total{") || !strings.Contains(q, "}[5m])") {
		t.Errorf("CPU used must be the summed 5-minute rate: %s", q)
	}
	if q := testQueries.Container(MemUsed); !strings.HasPrefix(q, "max by") || !strings.Contains(q, "container_memory_working_set_bytes{") {
		t.Errorf("memory used must be the max working set: %s", q)
	}
}

// Finished Job pods keep their kube-state-metrics request series until they
// are deleted; counting them would bill a CronJob's history as requested.
func TestQueries_RequestsAndLimitsCountOnlyLiveBoundPods(t *testing.T) {
	for m, want := range map[Measure]string{
		CPURequest: `kube_pod_container_resource_requests{job="kube-state-metrics",resource="cpu",unit="core",node!=""}`,
		CPULimit:   `kube_pod_container_resource_limits{job="kube-state-metrics",resource="cpu",unit="core",node!=""}`,
		MemRequest: `kube_pod_container_resource_requests{job="kube-state-metrics",resource="memory",unit="byte",node!=""}`,
		MemLimit:   `kube_pod_container_resource_limits{job="kube-state-metrics",resource="memory",unit="byte",node!=""}`,
	} {
		q := testQueries.Container(m)
		if !strings.Contains(q, want) {
			t.Errorf("measure %d: %s\n  missing %s", m, q, want)
		}
		if !strings.Contains(q, `and on (namespace, pod) (max by (namespace, pod) (kube_pod_status_phase{job="kube-state-metrics",phase=~"Pending|Running"}) == 1)`) {
			t.Errorf("measure %d does not keep only Pending/Running pods: %s", m, q)
		}
	}
}

func TestQueries_NodeUsageReadsTheRootCgroup(t *testing.T) {
	if q := testQueries.Node(NodeCPUUsed); !strings.Contains(q, `container_cpu_usage_seconds_total{job="kubernetes-cadvisor",id="/"}[5m]`) || !strings.Contains(q, "by (node)") {
		t.Errorf("node CPU: %s", q)
	}
	if q := testQueries.Node(NodeMemUsed); !strings.Contains(q, `container_memory_working_set_bytes{job="kubernetes-cadvisor",id="/"}`) {
		t.Errorf("node memory: %s", q)
	}
	if q := testQueries.Node(NodeCPUAllocatable); !strings.Contains(q, `kube_node_status_allocatable{job="kube-state-metrics",resource="cpu",unit="core"}`) {
		t.Errorf("allocatable CPU: %s", q)
	}
	if q := testQueries.Node(NodeMemAllocatable); !strings.Contains(q, `resource="memory",unit="byte"`) {
		t.Errorf("allocatable memory: %s", q)
	}
}

// Instant queries at day end look back exactly the day's length.
func TestQueries_InstantQueriesSpanTheDay(t *testing.T) {
	loc := cdmx(t)
	d, _ := ParseDay("2026-11-04", loc)
	for _, q := range []string{testQueries.Probe(d), testQueries.PodOwners(d), testQueries.ReplicaSetOwners(d), testQueries.JobOwners(d)} {
		if !strings.Contains(q, "[86400s]") {
			t.Errorf("%s does not span 86400s", q)
		}
	}
	long, _ := ParseDay("2022-10-30", loc)
	if !strings.Contains(testQueries.PodOwners(long), "[90000s]") {
		t.Error("a 25-hour day must look back 90000s")
	}
	if q := testQueries.PodOwners(d); !strings.Contains(q, "by (namespace, pod, owner_kind, owner_name)") {
		t.Errorf("pod owners: %s", q)
	}
	if q := testQueries.ReplicaSetOwners(d); !strings.Contains(q, "kube_replicaset_owner{") {
		t.Errorf("replicaset owners: %s", q)
	}
	if q := testQueries.JobOwners(d); !strings.Contains(q, "by (namespace, job_name, owner_kind, owner_name)") {
		t.Errorf("job owners: %s", q)
	}
}
