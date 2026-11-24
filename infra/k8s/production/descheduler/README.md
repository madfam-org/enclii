# Descheduler

The scheduler places a pod once and never moves it. This CronJob
([kubernetes-sigs/descheduler](https://github.com/kubernetes-sigs/descheduler)
v0.33.0, matching the cluster's Kubernetes minor) evicts a few pods per run
so they get placed again. The Deployment controller recreates each evicted
pod at once. Owner-approved 2026-11-04, with the request right-sizing of the
same day: apps now reserve much less than before, but their pods still sit
where the old requests put them, many on the control-plane node.

| File | What it is |
| --- | --- |
| `policy.yaml` | The policy the descheduler reads, mounted from the `descheduler-policy` ConfigMap. Its header lists every guarantee. |
| `cronjob.yaml` | Runs every 30 minutes (`7,37 * * * *` UTC); one process per run, at most 5 evictions. |
| `rbac.yaml` | Read nodes, namespaces, pods, PDBs and priority classes; create `pods/eviction`. No `pods` delete. |
| `namespace.yaml` | `descheduler`, labelled `enclii.dev/type: infrastructure`. |

Synced by the `descheduler` Application (`infra/argocd/apps/descheduler.yaml`).
Tested by `tests/scripts/test_descheduler_policy.py`.

## What a run does

1. **RemovePodsViolatingNodeTaints.** Evicts a pod from a node tainted
   `node-role.madfam.io/control-plane:PreferNoSchedule` when the pod does not
   tolerate it. The taint is the only one this plugin looks at.
2. **RemoveDuplicates.** Spreads replicas of one owner that share a node.
3. **LowNodeUtilization.** Works by requests, not by measured use. While some
   node is under 30% on cpu, memory and pods, it moves pods off nodes over 60%
   cpu, 60% memory or 80% pods.

Every run is capped at 5 evictions in total and 1 per namespace. It never
touches builder nodes (`role=builder`) and never evicts:

- pods in the excluded namespaces: kube-system, arc-system, arc-runners,
  longhorn-system, data, monitoring, logging, argocd, cnpg-system,
  external-secrets, descheduler, kyverno and vault;
- pods with local storage (emptyDir or hostPath) or with a PVC;
- DaemonSet pods, system-critical pods, or pods with no owner;
- pods younger than 1 hour;
- pods that fit no other non-builder node, such as a pod pinned to the
  control-plane node.

Every eviction goes through the Eviction API, so a PodDisruptionBudget can
refuse it. A refused pod stays where it is and is tried again next run.

## Coordinator steps

1. **Merge and let Argo sync.** The root Application adopts
   `infra/argocd/apps/descheduler.yaml`. Then check:

   ```bash
   enclii ops apps status descheduler -n argocd --json
   enclii ops jobs list -n descheduler --json
   ```

2. **Read the first run before tainting.** Until the taint exists, only
   RemoveDuplicates and LowNodeUtilization act. Take a Job pod name from
   `ops jobs list` and read its log:

   ```bash
   enclii ops pods logs <descheduler-job-pod> -n descheduler --tail 500 --json
   ```

   Lines to look for:
   - `"Node has been classified" category=... usagePercentage=...`: each
     node's band, by request;
   - `"Evicted pod" pod=... strategy=...`: an eviction;
   - `"Pod fails the following checks"` and `"pod does not fit on any other
     node"`: a pod the policy protected, with the reason.

   If the bands put nodes in the wrong class, change `policy.yaml` in a PR.
   The next run reads it; no restart is needed.

3. **Taint the control-plane node.** This is node configuration and lives
   outside this repo. Use the role, not a name:

   ```bash
   kubectl taint nodes <CONTROL_PLANE_NODE> node-role.madfam.io/control-plane=true:PreferNoSchedule
   ```

   Make it durable in the k3s server config (`node-taint`) kept in
   `internal-devops`. `PreferNoSchedule` is soft: the scheduler still uses
   the node when nothing else fits, and nodeFit keeps the descheduler from
   evicting a pod that has nowhere else to go.

4. **Watch the drift.** Pods leave the control-plane node at most 5 per run,
   1 per namespace. KEDA's Helm values *prefer* the control-plane node. The
   default scheduler weighs the taint above that preference, so its pods move
   once and stay on a worker. Anything that must stay on the control-plane
   node needs a toleration for the taint, or a required node affinity.

## Pause, exempt, remove

- **Pause:** set `suspend: true` in `cronjob.yaml`, in Git. Argo reverts a
  hand patch.
- **Exempt one workload:** give it a PodDisruptionBudget, or a toleration for
  the taint (exempts it from that plugin only). To exempt a whole namespace,
  add it to all three `exclude` lists in `policy.yaml`; the test keeps them
  equal.
- **Never** annotate a pod `descheduler.alpha.kubernetes.io/evict`: upstream
  treats it as "evict regardless" and it bypasses every filter above.
- **Remove:** delete the Application file. Its resources finalizer prunes
  the namespace, RBAC and CronJob. Evictions already made stay made.
