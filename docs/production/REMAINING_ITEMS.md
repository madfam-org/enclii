# Remaining Items — Post Session 106

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> **Last updated:** 2026-05-22 (GA program)
> **Platform status:** Release Candidate — see [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) for GA gates (replaces “95% ready” narrative)
> **Cluster:** 3-node k3s (foundry-cp [CP] + foundry-worker-01 [worker] + foundry-builder-01 [builder]), ~150 pods
> **Full Remediation Plan:** [REMEDIATION_PLAN.md](./REMEDIATION_PLAN.md) (8 phases, 45+ items)
> **GA program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) — Phase 0 cluster queue maps to Section 1 here

This document is the single source of truth for every remaining actionable item
across the enclii platform and ecosystem. Items are organized by execution context
(cluster vs. code), priority, and dependency order.

---

## Quick Reference — Execution Order

```
P0  [Cluster] PostHog cleanup — scale to 0, delete PVCs + detached volumes     ~15 min
P0  [Cluster] Longhorn CPU fix — `enclii ops storage settings-apply`              ~10 min
P0  [Cluster] Disk prune — crictl rmi + journalctl vacuum + old logs            ~10 min
P1  [Cluster] ArgoCD sync sweep — force-sync OutOfSync apps (post git push)     ~10 min
P1  [Cluster] Backup credentials (GitHub PAT + Cloudflare API token)            ~10 min
P1  [Cluster] Restore drill (validate backup pipeline)                          ~15 min
P1  [Cluster] Vault init → unseal → configure → migrate secrets                 ~60 min
P1  [Cluster] Cosign enforce activation (per-namespace, phased)                 ~20 min
P2  [Cluster] Legacy CronJob cleanup (2 orphaned jobs)                           ~2 min
P2  [Cluster] PostHog namespace deletion (after Wave 0 cleanup)                  ~5 min
                                                                        Total: ~2.5 hrs

Deferred (not blocking v0.1.0):
  - ESO CRD migration (0.9.11 → 0.16.2)  — needs maintenance window
  - PagerDuty/Opsgenie integration        — email alerting works
  - PostgreSQL HA / Redis Sentinel         — when SLA > 99.9% required
  - Multi-node Longhorn replication        — when 3rd node added
  - GPU node setup                         — when GPU hardware available
  - Multi-region                           — out of scope for v1
```

---

## Session 106 Resolved Items (Software-Only Remediation)

| Wave | Item | Status | Details |
|------|------|--------|---------|
| W2 | Vault health probes | **Committed** | `uninitcode=200&sealedcode=200` added to readiness+liveness. ArgoCD auto-syncs on push. |
| W3 | Prometheus retention | **Committed** | 15d/15GB → 7d/8GB. Saves ~7GB after compaction. |
| W4 | Roundhouse Redis auth | **Committed** | Config.Load() now resolves `REDIS_URL` from `REDIS_HOST`+`REDIS_PASSWORD` components. Hardcoded unauthenticated URL removed from K8s manifest. Needs image rebuild+deploy. |
| W6 | PostHog config cleanup | **Committed** | ArgoCD app deleted, pgbouncer-proxy deleted, Helm values + Redpanda archived to `infra/archive/posthog/`. Cloudflare Worker proxy retained. |
| W7a | Timetable reconciler tests | **Committed** | 30 tests: sanitizeK8sName, cronJobK8sName, oneOffJobK8sName, mapConcurrencyPolicy, stringSliceEqual, buildCronJob, buildOneOffJob, cronJobNeedsUpdate. All passing. |
| W7b | Cron job run repo tests | **Committed** | 14 tests: Create, ListByCronJob (ordered, empty, limit, cap, error), UpdateStatus (success, not found, error). All passing. |
| W7c | CI load-test dedup | **Committed** | Removed duplicate smoke test from ci.yml (already in load-test.yml). |
| W8 | Resource right-sizing | **Committed** | switchyard-api CPU limit 1000m→800m, cloudflared 500m→300m, switchyard-ui 500m→300m. Requests unchanged. |
| W9 | Documentation | **Committed** | REMAINING_ITEMS.md + CLAUDE.md updated. |

---

## Section 1: Cluster Operations (Require SSH + Secrets)

All cluster commands use:
```bash
ssh -o ProxyCommand="cloudflared access ssh --hostname %h" solarpunk@ssh.madfam.io "sudo kubectl ..."
```

### 1A. PostHog Cleanup — P0, ~15 min (Wave 0)

PostHog self-host abandoned. 5 stuck pods, 3 orphaned Longhorn volumes (~44 GB).

```bash
# Scale everything to 0 first
sudo kubectl scale deploy --all -n posthog --replicas=0
sudo kubectl scale statefulset --all -n posthog --replicas=0
sudo kubectl delete pods -n posthog --all --grace-period=0 --force
sudo kubectl delete pvc -n posthog --all

# Delete detached Longhorn volumes (Enclii-first)
enclii ops storage prune-detached
enclii ops storage prune-detached --apply --reason "Commercial GA O-4 orphan cleanup"

# Or wave0 orchestration:
# ./scripts/wave0-ga-ops.sh --apply --reason "Commercial GA Wave 0"

# Delete the namespace (after ArgoCD app is already removed in W6)
sudo kubectl delete namespace posthog --timeout=120s
```

**Verify:** `df -h /` drops from 83% to ~33%. `kubectl get pods -n posthog` returns empty.

### 0.2.2 Longhorn helm upgrade (committed CPU values) — Enclii-first

```bash
# Plan (dry-run)
enclii ops storage settings-apply

# Apply (Commercial GA O-5)
enclii ops storage settings-apply --apply --reason "Commercial GA O-5 Longhorn CPU"
```

Break-glass fallback: `helm upgrade longhorn ... -f infra/helm/longhorn/values.yaml`

**Verify:** `kubectl top pods -n longhorn-system | grep instance-manager` shows &lt;200m each.

### 1C. Capacity Cleanup — P0, ~10 min

Enclii-first (triggers existing `node-maintenance` CronJob in `enclii` namespace):

```bash
enclii ops jobs list node-maintenance -n enclii
enclii ops jobs trigger node-maintenance -n enclii --apply --reason "Commercial GA O-6 disk prune"
```

Or `./scripts/wave0-ga-ops.sh --apply --disk-prune --reason "Commercial GA O-6"`.

Break-glass (node SSH): `k3s crictl rmi --prune`, `journalctl --vacuum-size=500M`, old log rotation per `infra/k8s/production/node-maintenance-cronjob.yaml`.

### 1D. ArgoCD Sync Sweep — P1, ~10 min (Wave 2)

After pushing the Session 106 commit, ArgoCD auto-syncs changed apps. Force-sync any remaining OutOfSync:

```bash
enclii ops apps sync-sweep -n argocd
enclii ops apps sync-sweep -n argocd --apply --reason "Commercial GA O-8 Argo sweep"
# Or wave1 orchestration:
# ./scripts/wave1-ga-ops.sh --apply --reason "Commercial GA Wave 1"
```

**Expected permanently OutOfSync:** `network-policies` (prune:false by design).

### 1E. Backup Credential Provisioning — P1, ~10 min (Wave 5)

Two backup CronJobs fail because secrets don't exist yet.

**GitHub Backup Secret:**
```bash
sudo kubectl create secret generic github-backup-credentials -n data \
  --from-literal=github-pat="<MADFAM_BOT_PAT_WITH_REPO_READ>" \
  --dry-run=client -o yaml | sudo kubectl apply -f -
```

**Cloudflare API Secret:**
```bash
sudo kubectl create secret generic cloudflare-api-credentials -n data \
  --from-literal=api-token="<CF_API_TOKEN_DNS_READ_TUNNEL_READ>" \
  --from-literal=zone-id-enclii="<ENCLII_DEV_ZONE_ID>" \
  --from-literal=zone-id-madfam="<MADFAM_IO_ZONE_ID>" \
  --from-literal=account-id="<CF_ACCOUNT_ID>" \
  --dry-run=client -o yaml | sudo kubectl apply -f -
```

**Test + restore drill:**
```bash
sudo kubectl create job github-backup-test --from=cronjob/github-repos-backup -n data
sudo kubectl create job cf-backup-test --from=cronjob/cloudflare-config-backup -n data
sudo kubectl create job restore-drill-manual --from=cronjob/postgres-restore-drill -n data
sudo kubectl get jobs -n data --watch  # all should Complete
```

### 1F. Vault Deployment — P1, ~60 min

Full runbook: `docs/runbooks/CLUSTER_REMEDIATION_OPS.md` §3

Health probes now accept uninitialized/sealed state (Session 106 fix). Pod should be Running.

Steps: init → unseal (3/5 keys) → enable KV engine → configure K8s auth → create ESO policy → migrate secrets → deploy ClusterSecretStore + ExternalSecrets → verify.

### 1G. Cosign Enforce Activation — P1, ~20 min

```bash
enclii ops policy cosign-enable
enclii ops policy cosign-enable --apply --reason "Commercial GA O-11 Cosign enforce"
```

Phased namespaces: `enclii`, `status`, `monitoring`. Verify images are signed before enabling on additional namespaces.

### 1H. Legacy CronJob Cleanup — P2, ~2 min

```bash
sudo kubectl delete cronjob image-cleanup -n kube-system --ignore-not-found
sudo kubectl delete cronjob disk-cleanup -n enclii --ignore-not-found
```

---

## Section 2: Roundhouse Image Rebuild (Post Wave 4)

The Redis auth fix requires a Docker image rebuild for the Roundhouse service.

```bash
# Option A: Trigger via CI (push to main triggers docker-build)
# The commit from Session 106 changes apps/roundhouse/ code — CI auto-builds.

# Option B: Manual rebuild
cd apps/roundhouse
docker build -t ghcr.io/madfam-org/enclii/roundhouse:latest -f Dockerfile .
docker push ghcr.io/madfam-org/enclii/roundhouse:latest

# Verify pods restart with auth:
sudo kubectl rollout restart deploy/roundhouse -n enclii
sudo kubectl rollout restart deploy/roundhouse-api -n enclii
sudo kubectl logs -n enclii -l app=roundhouse --tail=5
# Should show: "Connected to Redis queue (standalone mode)"
```

---

## Section 3: External Repo Follow-ups (Out of Scope)

| Repo | Issue | Fix Needed |
|------|-------|-----------|
| autoswarm-office | agents-api 404 | Add `/health` endpoint |
| tezca | api.tezca.mx 404 | Add `/health` endpoint |
| karafiel | kf-api 404, worker crash | Add `/health` endpoint, fix Redis connection |
| forgesight | cms.madfam.io 404 | Add `/health` endpoint |
| yantra4d | 4d-api 502, admin/studio crash | Fix nginx upstream config |
| pravara-mes | mes-api 502 | Fix API startup |

---

## Section 4: Deferred Items (Not Blocking v0.1.0)

| Item | Notes |
|------|-------|
| ESO CRD migration (0.9.11 → 0.16.2) | Needs maintenance window, v1beta1 to v1 CRD migration |
| PagerDuty/Opsgenie integration | Email alerting works; configure AlertManager receiver |
| PostgreSQL HA / Redis Sentinel | When SLA > 99.9% required |
| Multi-node Longhorn replication | When 3rd storage node added |
| GPU node setup | Manifests ready at `infra/k8s/base/gpu/`, awaiting hardware |
| Multi-region | Out of scope for v1 |
| Handler legacy pattern migration | Incremental, no dedicated sprint |
| Image digest pinning (ecosystem) | Each repo pins own images |
| KEDA runtime (serverless) | ArgoCD app staged, deploy when scale-to-zero needed |

---

## Section 5: Expected Post-Remediation Metrics

| Metric | Before | After (projected) | Change |
|--------|--------|-------------------|--------|
| Disk | 83% | ~33% | -50pp |
| CPU allocated | 87% | ~69% | -18pp |
| Non-running pods | 13 | 3-5 (external repos) | -8 to -10 |
| ArgoCD Synced/Healthy | 17/28 | 24/27 | +7 |
| Backup pipelines | 3/5 | 5/5 | +2 |
| Test files missing | 2 | 0 | complete |
| Disk fill date | ~April 10 | months out | safe |

---

## Section 6: Test Coverage Summary

| Module | Tests | Session |
|--------|-------|---------|
| switchyard-api (db, api, reconciler, services) | ~330+ | S97-106 |
| CLI (cmd, client, config) | 82 | S99 |
| SDK (client, types) | 30 | S97 |
| Dispatch (auth, API, components) | 123 | S98 |
| Status page (lib, config, health) | 129 | S68-69 |
| Switchyard UI (components, hooks) | 159 | Various |
| Provenance + Signing | 50 | S97 |
| Timetable + Junction | 81 | S103 |
| Timetable reconciler | 30 | S106 |
| Cron job run repo | 14 | S106 |
| **Total** | **~1,000+** | |

CI threshold: 50% minimum. No `t.Skip("TODO...")` stubs remaining. CI load-test deduplicated (S106).

---

## Section 7: File Reference

| Purpose | Path |
|---------|------|
| Main runbook | `docs/runbooks/CLUSTER_REMEDIATION_OPS.md` |
| Vault operations | `docs/runbooks/VAULT_OPERATIONS.md` |
| Backup coverage | `docs/runbooks/BACKUP_COVERAGE.md` |
| Longhorn recovery | `docs/runbooks/LONGHORN_VOLUME_RECOVERY.md` |
| Capacity roadmap | `docs/infrastructure/CAPACITY_ROADMAP.md` |
| External Secrets | `docs/infrastructure/EXTERNAL_SECRETS.md` |
| Production checklist | `docs/production/PRODUCTION_CHECKLIST.md` |
| Secret rotation log | `docs/security/SECRET_ROTATION_LOG.md` |
| Vault migration script | `scripts/vault-secret-migration.sh` |
| Backup secret templates | `infra/k8s/production/backup/*-secrets.yaml.template` |
| Kyverno policies | `infra/k8s/base/kyverno/policies/` |
| GPU manifests | `infra/k8s/base/gpu/` |
| Longhorn Helm values | `infra/helm/longhorn/values.yaml` |
| Vault Helm values | `infra/helm/vault/values.yaml` |
| PostHog archived values | `infra/archive/posthog/` |
| Cloudflare PostHog proxy | `infra/cloudflare/posthog-proxy/` (still active) |
