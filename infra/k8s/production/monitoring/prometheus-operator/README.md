# prometheus-operator (Tier 0.3b)

Availability remediation program, Tier 0.3b
(`internal-devops/roadmaps/2026-08-26-availability-remediation-9999.md`).

## Why this exists

An ecosystem sweep found roughly 90 `PrometheusRule` alert rules across 12
locations — 48 in 7 product repos (janua, dhanam, avala, tulana, ceq,
blueprint-harvester, pravara-mes; mostly kustomization-applied and inert)
plus enclii's own `../arc/monitoring.yaml`,
`../../base/verdaccio/npm-token-alert.yaml`, and
`../postgres-ha/podmonitor.yaml` — every single one dead on arrival, because
**no prometheus-operator has ever run in this cluster** to reconcile a
`PrometheusRule` or `PodMonitor` CRD into a running Prometheus.

The CRD *type* has been installed for 82+ days
(`../prometheus-operator-crds/prometheusrules.yaml`), which is exactly what
makes this failure mode so easy to miss: `kubectl apply` on a `PrometheusRule`
succeeds, the object sits in `kubectl get prometheusrule` looking perfectly
healthy, and nothing about that command tells you whether anything is
actually reading it. It wasn't.

This change deploys one operator. That one deployment resurrects all ~90
rules and makes the per-repo "ship a PrometheusRule" pattern correct forever,
instead of every product team independently rediscovering that their alerts
never fired.

## What this deploys (Phase A)

1. **`prometheus-operator` v0.93.1** — the official operator Deployment,
   full RBAC, wired to `prometheus-config-reloader` v0.93.1 for its injected
   sidecars. `operator.yaml`.
2. **8 additional CRDs** — `Alertmanager`, `AlertmanagerConfig`, `Probe`,
   `PrometheusAgent`, `Prometheus`, `ScrapeConfig`, `ServiceMonitor`,
   `ThanosRuler` — version-matched to v0.93.1, added alongside the 2 that
   already existed (`PrometheusRule`, `PodMonitor`, both at v0.75.2). See
   `../prometheus-operator-crds/kustomization.yaml` for the version-skew
   note on those two pre-existing files — **left un-upgraded, deliberately,
   out of this change's scope**.
3. **A dedicated `Prometheus` CR named `rules-eval`** — 1 replica, 24h
   retention, `ruleSelector: {}` + `ruleNamespaceSelector: {}` (match every
   `PrometheusRule` in every namespace — the entire point),
   `podMonitorSelector: {}` + `podMonitorNamespaceSelector: {}` (same, for
   `PodMonitor`), its own ServiceAccount/RBAC, and `alerting.alertmanagers`
   pointed at the **existing** Alertmanager Service. `rules-eval-*.yaml`.

## What this deliberately does NOT do yet

- **Does not touch the existing hand-rolled Prometheus** (`../prometheus.yaml`)
  or **either Alertmanager** (`../alertmanager.yaml`,
  `../alertmanager-statefulset.yaml`). Both remain the primary stack,
  unmodified, unchanged in behavior. `rules-eval` is a second, small,
  operator-managed Prometheus that runs alongside them.
- **Does not consolidate scrape configs.** `rules-eval` needs the same
  metrics the primary Prometheus already scrapes so the resurrected rules
  have series to evaluate against (`up{job="switchyard-api"}`, `pg_up`,
  `redis_up`, `kube_pod_container_status_restarts_total`, etc.). Rather than
  invent a new mechanism for sharing scrape config, or federate (rejected —
  federation rewrites the `job` label and would silently break every rule
  matching on the original job name), this copies the primary Prometheus's
  `scrape_configs:` list **verbatim** into an `additionalScrapeConfigs`
  Secret (`rules-eval-scrape-configs.secret.yaml`). This is temporary,
  duplicated state, and it is documented as such in that file's header —
  read it before changing either copy.
- **Does not upgrade the 2 pre-existing CRDs** (`PrometheusRule`,
  `PodMonitor`) from v0.75.2 to v0.93.1, even though a real (if currently
  inert) schema gap exists between them — see
  `../prometheus-operator-crds/kustomization.yaml`.
- **Does not give `rules-eval` full 7-day retention.** At 24h, the
  `PodChronicRestart7d` rule's `increase(...[7d])` window will only ever see
  a partial (≤24h-deep) slice of data on this evaluator until Phase B. It
  will still correctly evaluate `PodChronicRestart24h` and everything with a
  `for:`/window ≤24h — which is the overwhelming majority of the ~90 rules.
- **Does not fold the new Alertmanager-ingress NetworkPolicy rule into the
  primary one.** See `rules-eval-network-policy.yaml`'s header for why a
  second, additive `NetworkPolicy` object was used instead of editing
  `../network-policies.yaml` (owned by #440 in this rollout sequence).

## Phase B sketch (not built, not scheduled — write down before it's needed)

Once `rules-eval` has been running clean for a while and its resurrected
rules have been observed firing correctly against real conditions:

1. Retarget scrape config so there is one source of truth — either point the
   primary Prometheus's own config at the same `kubernetes_sd_configs`
   pattern via `ServiceMonitor`/`PodMonitor` objects the operator discovers,
   or migrate the primary Prometheus itself under operator management as a
   second `Prometheus` CR with `retention: 7d` and delete `rules-eval`.
2. Delete `rules-eval-scrape-configs.secret.yaml` and the duplication it
   documents.
3. Fold `alertmanager-ingress-rules-eval`'s one rule into
   `../network-policies.yaml`'s `alertmanager-ingress`, then delete the
   additive object.
4. Decide, deliberately, whether to bump `prometheusrules.yaml` and
   `podmonitors.yaml` in `../prometheus-operator-crds/` to v0.93.1 (or
   whatever the then-current release is) to close the schema-skew gap noted
   above.
5. Revisit `rules-eval`'s retention (24h -> 7d, if it becomes the sole
   evaluator) and storage size accordingly.

## Post-deploy acceptance test

Run in this order; each step should be true before moving to the next.

```bash
# 1. The operator is up and the Prometheus CR exists.
kubectl -n monitoring get deployment prometheus-operator
kubectl -n monitoring get prometheus rules-eval

# 2. The operator actually reconciled it into a running StatefulSet + pod.
kubectl -n monitoring get statefulset prometheus-rules-eval
kubectl -n monitoring get pods -l app.kubernetes.io/name=prometheus,prometheus=rules-eval
# expect: prometheus-rules-eval-0 Running, 2/2 (prometheus + config-reloader)

# 3. Every resurrected rule group loaded — this is the actual resurrection,
#    not just "the pod is Running". Port-forward or hit through the existing
#    ingress path, then:
kubectl -n monitoring port-forward svc/prometheus-operated 9090:9090 &
curl -s http://localhost:9090/api/v1/rules | jq -r '.data.groups[].name' | sort
# expect to see groups from: arc-alerts, npm-token-expiry-rules,
# postgres-ha-rules, and (once the 7 product repos' own operator-adjacent
# rollouts land) their groups too — this Prometheus watches ALL namespaces,
# so anything applied anywhere shows up here without a redeploy of this
# directory.

# 4. PodMonitor discovery — postgres-ha specifically, since it's named in
#    the "why" above.
curl -s http://localhost:9090/api/v1/targets | jq -r '.data.activeTargets[] | select(.labels.job=="data/postgres-ha") | .health'
# expect: "up" for each cnpg instance pod

# 5. A test alert actually reaches the EXISTING Alertmanager — this is the
#    step that proves the NetworkPolicy fix in
#    rules-eval-network-policy.yaml is doing its job, not just that the rule
#    evaluates locally.
curl -s http://localhost:9090/api/v1/alertmanagers | jq
# expect: the existing `alertmanager` Service listed under activeAlertmanagers,
# not just droppedAlertmanagers.
#
# To force an actual firing alert end-to-end: temporarily scale a workload
# a resurrected rule watches (e.g. `kubectl -n arc-system scale deployment
# arc-controller --replicas=0` triggers ARCControllerDown after 5m — remember
# to scale back up), then confirm it appears in:
kubectl -n monitoring port-forward svc/alertmanager 9093:9093 &
curl -s http://localhost:9093/api/v2/alerts | jq -r '.[].labels.alertname'
```

## Dedup audit

With the operator live, the following previously-dead rule sets go live too:
`../arc/monitoring.yaml`, `../../base/verdaccio/npm-token-alert.yaml`,
`../postgres-ha/podmonitor.yaml`, plus the 7 product repos. Below is every
alert whose name or PromQL condition duplicates one already live today in
the flat-file `prometheus-rules` ConfigMap
(`../prometheus.yaml`, read on branch `fix/monitoring-resurrect-dead-rules`
— PR #440's soon-to-merge baseline). **These are flagged, not fixed** — no
edits were made to any of the files below; that's a decision for whoever
owns each rule set once they can see it actually firing.

### enclii's own newly-live rule sets — no duplicates found

`arc-alerts` (`../arc/monitoring.yaml`), `npm-token-expiry-rules`
(`../../base/verdaccio/npm-token-alert.yaml`), and `postgres-ha-rules`
(`../postgres-ha/podmonitor.yaml`) were each checked individually against the
#440 baseline. None of their alert names or metrics (`arc_runner_scale_set_*`,
`npm_token_*`, `cnpg_collector_*`) overlap anything already live — genuinely
new coverage, all three.

### 7 product repos — duplicates found

| Repo | Duplicate alert | Duplicates (baseline) | Why |
|---|---|---|---|
| dhanam | `DatabaseDown` | `PostgresDown` | Identical expr `pg_up==0` |
| dhanam | `RedisDown` | `RedisDown` | Same name, identical expr `redis_up==0` |
| dhanam | `DiskSpaceLow` | `NodeDiskSpaceCritical` | Same metric/direction (free ratio <0.1 ≡ used >90%) |
| dhanam | `PodRestarts` | `PodCrashLooping` / `PodChronicRestart24h` | Same metric (`kube_pod_container_status_restarts_total`), same intent |
| avala | `AvalaApiHighErrorRate` | `SwitchyardAPIHighErrorRate` / `ClientServiceErrorRate` | Identical threshold (>0.02) and 5xx-ratio pattern |
| avala | `AvalaApiHighLatencyP95` | `SwitchyardAPIHighLatency` / `ClientServiceLatencyP95` | Same p95-histogram_quantile pattern |
| avala | `AvalaApiDown` | `SwitchyardAPIDown` / `ClientDeploymentUnavailable` | Same up==0-style pattern |
| tulana | `TulanaApiReplicaFloorBreached` / `TulanaWebReplicaFloorBreached` | `DeploymentReplicasMismatch` / `ClientDeploymentUnavailable` | Same metric, same low-replica-count intent |
| tulana | `TulanaScheduledJobFailed` | `CronJobFailed` | Same metric (`kube_job_status_failed`), different job-name scope |
| tulana | `TulanaScheduledJobStale` | `PostgresBackupMissing` / `GitHubBackupMissing` (pattern) | Same staleness pattern, different jobs |
| blueprint-harvester | `BlueprintApiNoReadyReplicas` / `BelowDesiredReplicas` | `DeploymentReplicasMismatch` / `ClientDeploymentUnavailable` | Same metric family, zero/below-desired-replica intent |
| pravara-mes | `PravaraAPIDown` | `SwitchyardAPIDown`; also live via `ClientDeploymentUnavailable` since `pravara-mes` is explicitly in the client-slo namespace regex | Same up==0 pattern; in-scope for the generic client-slo group already |
| pravara-mes | `PravaraAPIHighErrorRate` | `SwitchyardAPIHighErrorRate` / `ClientServiceErrorRate` (in-scope) | Same 5xx-ratio pattern, threshold 0.05 vs 0.02 |
| pravara-mes | `PravaraAPIHighLatency` | `SwitchyardAPIHighLatency` / `ClientServiceLatencyP95` (in-scope) | Same p95 pattern, threshold 2s vs implicit |
| pravara-mes | `PravaraAPIHighDBConnectionUsage` | `PostgresHighConnections` | Same >0.8 threshold and connection-saturation intent; different metric source (app-pool vs `pg_stat_activity_count`) |

Roughly one-third of the ~43 product-repo rules found overlap a baseline
rule — concentrated in generic "API down / high error rate / high latency /
replica floor / cronjob staleness" patterns that every repo independently
reinvented before any operator existed to make a shared baseline visible.

**Not duplicates — genuinely new coverage:** janua (8 rules, secret-rotation
compliance), avala (2 rules, RLS drift probes), ceq (5 rules, job-queue/
webhook/migration health), pravara-mes (9 rules, MQTT telemetry + Centrifugo),
dhanam (6 rules, app-level auth/queue metrics under dhanam's own metric
names).

`dhanam-monitoring` (dhanam's actual rule namespace) does not literally match
the client-slo regex's `dhanam` token, so dhanam's overlaps above are
pattern-level duplication, not a live double-fire — except `pg_up`/`redis_up`,
which are exact global-metric collisions regardless of namespace.
