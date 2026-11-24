package resourceusage

import "testing"

func owner(ns, nameLabel, name, kind, ownerName string) Series {
	return Series{Labels: map[string]string{
		"namespace": ns, nameLabel: name, "owner_kind": kind, "owner_name": ownerName,
	}}
}

func TestOwners_ResolveToTheTopLevelWorkload(t *testing.T) {
	o := NewOwners(
		[]Series{
			owner("shop", "pod", "api-7d9-a", "ReplicaSet", "api-7d9"),
			owner("shop", "pod", "nightly-1-x", "Job", "nightly-1"),
			owner("shop", "pod", "migrate-x", "Job", "migrate"),
			owner("shop", "pod", "db-0", "StatefulSet", "db"),
			owner("data", "pod", "pg-1", "Cluster", "pg"),
			owner("shop", "pod", "debug", "<none>", "<none>"),
			owner("shop", "pod", "orphan-rs-x", "ReplicaSet", "orphan-rs"),
			owner("ci", "pod", "runner-abc", "EphemeralRunner", "runner-abc"),
		},
		[]Series{
			owner("shop", "replicaset", "api-7d9", "Deployment", "api"),
			owner("shop", "replicaset", "orphan-rs", "<none>", "<none>"),
		},
		[]Series{
			owner("shop", "job_name", "nightly-1", "CronJob", "nightly"),
			owner("shop", "job_name", "migrate", "<none>", "<none>"),
		},
		[]string{"arc-runners"},
	)
	for _, tc := range []struct{ ns, pod, kind, name string }{
		{"shop", "api-7d9-a", "Deployment", "api"},
		{"shop", "nightly-1-x", "CronJob", "nightly"},
		{"shop", "migrate-x", "Job", "migrate"},
		{"shop", "db-0", "StatefulSet", "db"},
		{"data", "pg-1", "Cluster", "pg"},
		{"shop", "debug", KindPod, "debug"},
		{"shop", "never-seen", KindPod, "never-seen"},
		{"shop", "orphan-rs-x", "ReplicaSet", "orphan-rs"},
		{"ci", "runner-abc", KindNamespace, "ci"},
		{"arc-runners", "anything", KindNamespace, "arc-runners"},
	} {
		kind, name := o.Resolve(tc.ns, tc.pod)
		if kind != tc.kind || name != tc.name {
			t.Errorf("Resolve(%s/%s) = %s/%s, want %s/%s", tc.ns, tc.pod, kind, name, tc.kind, tc.name)
		}
	}
}

// A real owner beats "<none>", and between two real owners the choice does
// not depend on the order Prometheus returned them.
func TestOwners_ChoiceIsDeterministic(t *testing.T) {
	a := owner("shop", "pod", "p", "StatefulSet", "zeta")
	b := owner("shop", "pod", "p", "DaemonSet", "alpha")
	none := owner("shop", "pod", "p", "<none>", "<none>")
	for _, order := range [][]Series{{a, b, none}, {none, b, a}, {b, none, a}} {
		kind, name := NewOwners(order, nil, nil, nil).Resolve("shop", "p")
		if kind != "DaemonSet" || name != "alpha" {
			t.Errorf("order %v: got %s/%s", order, kind, name)
		}
	}
}
