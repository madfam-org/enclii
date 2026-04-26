# Config Reload Runbook (Stakater Reloader)

_Last updated: 2026-04-26 — Tier 1 rollout (status-madfam, status-enclii, cloudflared)_

## Why this exists

On 2026-04-24, `status-madfam` was running with 4-day-stale environment because:

1. PR #128 (2026-04-20) added Routecraft monitoring entries to the status ConfigMap.
2. ArgoCD reconciled the ConfigMap on 2026-04-24.
3. The `status-madfam` pod had started 2026-04-20 and was never restarted.
4. Pods only re-read ConfigMaps on startup, so the new entries were not visible.

Stakater Reloader closes this loop: when a ConfigMap or Secret a Deployment
references changes, Reloader patches the Deployment's pod template (adds a
checksum annotation) which triggers a normal RollingUpdate.

## How Reloader works

- Runs as a deployment in the `reloader` namespace (2 replicas, leader-election).
- Watches ConfigMaps and Secrets cluster-wide.
- Computes a SHA1 of each watched ConfigMap/Secret data and stores it on
  itself; on change it detects the diff.
- For each Deployment annotated with `reloader.stakater.com/match: "true"`,
  finds which ConfigMaps/Secrets it references via `envFrom`, `valueFrom`,
  and volume mounts, and patches the pod template's
  `last-reloaded-from` annotation to force a rollout.
- **Opt-in only** — `matchLabels: {}` plus `watchGlobally: true` plus our
  config means Reloader watches every namespace but only acts on
  Deployments carrying the explicit annotation. Tier 1 contains blast
  radius. Tier 2 expansion is per-ecosystem repo.

The behavior is documented upstream:
[github.com/stakater/Reloader](https://github.com/stakater/Reloader).

## Tier 1 deployments (this PR)

| Deployment | Namespace | Manifest | Triggers reload on |
|------------|-----------|----------|--------------------|
| `status-madfam` | `enclii` | `apps/status/k8s/madfam/kustomization.yaml` | `status-config-madfam` ConfigMap |
| `status-enclii` | `enclii` | `apps/status/k8s/enclii/kustomization.yaml` | `status-config-enclii` ConfigMap |
| `cloudflared` | `cloudflare-tunnel` | `infra/k8s/production/cloudflared-unified.yaml` | `cloudflared-config` ConfigMap, `cloudflared-enclii-token` Secret |

## Opt-in a new Deployment

### Plain manifest (e.g. cloudflared-unified.yaml)

```yaml
spec:
  template:
    metadata:
      annotations:
        reloader.stakater.com/match: "true"
```

### Kustomize overlay (e.g. status-madfam)

Add a JSON-Patch entry to `patches:` in the kustomization. Use the
JSON-Pointer escape `~1` for the `/` in the annotation key:

```yaml
- patch: |-
    - op: add
      path: /spec/template/metadata/annotations/reloader.stakater.com~1match
      value: "true"
  target:
    kind: Deployment
    name: <deployment-name>
```

### Notes

- `match: "true"` is the right annotation when the Helm value
  `reloader.matchLabels: true` is set (our config). `auto: "true"` is the
  alternative for `autoReloadAll: true` (we deliberately did not enable
  this — opt-in posture).
- Annotations go on the **pod template** (`spec.template.metadata.annotations`),
  not on the Deployment metadata. Reloader patches the pod template
  checksum annotation, so anything other than the pod template is ignored.
- A Deployment can carry both annotations (`match` and `auto`); they're
  not mutually exclusive but `match` is the documented production posture.
- Per-resource annotations also exist if you want to limit which
  ConfigMaps trigger a reload:
  - `configmap.reloader.stakater.com/reload: "<name1>,<name2>"`
  - `secret.reloader.stakater.com/reload: "<name1>,<name2>"`

## Test procedure (canonical)

After ArgoCD has synced this PR, verify Reloader is working end-to-end:

```bash
# 1. Confirm Reloader is running
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n reloader get deploy,po'

# 2. Confirm Tier 1 deployments are annotated
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n enclii get deploy status-madfam \
  -o jsonpath="{.spec.template.metadata.annotations}" | jq'
# Expect: reloader.stakater.com/match = "true"

# 3. Capture pod start time before the test
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n enclii get po -l app=status-madfam \
  -o jsonpath="{.items[*].status.startTime}"'

# 4. Patch the ConfigMap with a no-op label bump (do NOT change anything
#    that would actually break the service)
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n enclii annotate cm status-config-madfam \
  enclii.dev/reload-test="$(date +%s)" --overwrite'

# 5. Within ~5 seconds, watch Reloader log the trigger
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n reloader logs -l app=reloader-reloader \
  --tail=20 -f' &
# Expect: "Changes detected in 'status-config-madfam'... Updated 'status-madfam'"

# 6. Confirm the pod restarted (new start time)
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n enclii rollout status deploy/status-madfam'
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n enclii get po -l app=status-madfam \
  -o jsonpath="{.items[*].status.startTime}"'
# Expect: start time has advanced
```

If step 6 shows no restart within 60s, see Troubleshooting below.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| ConfigMap changed but pod did not restart | Annotation missing or on wrong path | `kubectl get deploy ... -o yaml` and confirm the annotation is on `spec.template.metadata.annotations`, not `metadata.annotations` |
| Annotation present, pod still not restarting | Reloader pods not running | `kubectl -n reloader get po` — both replicas should be Ready |
| Reloader pods running but no log entries on change | Reloader leader is unhealthy | `kubectl -n reloader logs -l app=reloader-reloader --tail=200` — look for leader-election errors. Restart: `kubectl -n reloader rollout restart deploy/reloader-reloader` |
| All deployments restart on any ConfigMap change | `autoReloadAll: true` accidentally set | Verify `infra/argocd/apps/reloader.yaml` Helm values — `autoReloadAll` should be **false** and `matchLabels` set |
| Reloader restarting in a loop | Permission issue (RBAC) | `kubectl -n reloader logs -l app=reloader-reloader` — look for `forbidden` errors. Verify `serviceAccount.create: true` in the Application |
| Old pods remain after change | New ReplicaSet is unhealthy | `kubectl -n <ns> describe deploy <name>` — look for image pull or probe failures. The Deployment is mid-RollingUpdate; fix the underlying issue or roll back |
| Two restart loops competing | `enclii.dev/configmap-version` patch and Reloader fighting | They don't conflict (different annotations on the same template). Reloader's checksum annotation is added by the controller; the manual version annotation is added by Kustomize. Both can coexist |

## Rollback

### Remove a single deployment from Reloader (no platform impact)

Drop the `reloader.stakater.com/match: "true"` annotation:

- Plain manifest: delete the annotation from `spec.template.metadata.annotations`.
- Kustomize: delete the patch entry that adds the annotation.

Commit, push, ArgoCD syncs. The Deployment will no longer auto-restart.

### Uninstall Reloader entirely (cluster-wide)

```bash
# 1. Remove the ArgoCD Application
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl -n argocd delete application reloader'

# 2. Confirm the namespace and resources are pruned
ssh ssh.madfam.io 'sudo /usr/local/bin/k3s kubectl get ns reloader'
# Expect: NotFound (resources-finalizer.argocd.argoproj.io will GC the namespace)

# 3. Revert the git change
git revert <sha-of-reloader-add>
```

The `reloader.stakater.com/match: "true"` annotations on Tier 1 deployments
are harmless without the controller — they become inert metadata.

## Related

- ArgoCD Application: `infra/argocd/apps/reloader.yaml`
- Helm chart: [reloader 2.2.11 (appVersion v1.4.16)](https://github.com/stakater/Reloader)
- Tier 2 candidates surfaced in the audit (separate PRs per repo): prometheus,
  grafana, alertmanager, roundhouse, dispatch, pgbouncer, verdaccio, plus
  every ecosystem repo whose deployment references a ConfigMap/Secret.
