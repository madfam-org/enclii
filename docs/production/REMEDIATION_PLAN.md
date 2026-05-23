# Full Remediation Plan — Session 106 Ecosystem Audit

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> **Created:** 2026-03-19 (Session 106)
> **Updated:** 2026-05-22 — May codebase audit remediation (see `CODEBASE_AUDIT_2026-05.md`)
> **GA program:** [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) — consolidated stability + commercial GA track
> **Status:** In Progress
> **Scope:** Enclii platform + 8 ecosystem repos
> **Audit Score:** 60/100 (pre-remediation)

This document captures every gap, shortcoming, and remediation action identified during
the comprehensive Session 106 ecosystem audit. Items are organized by phase, priority,
and execution order.

---

## Audit Summary

| Domain | Score | Key Gap |
|--------|-------|---------|
| Monitoring & Alerting | 7/10 | Email-only AlertManager, no escalation |
| Logging & Observability | 2/10 | No centralized logging |
| Security & Secrets | 4/10 | Vault uninitialized, secrets unencrypted at rest |
| Backup & DR | 7/10 | Credentials missing, restore drill unvalidated |
| Test Coverage | 8/10 | 16 Go packages without tests, CI passWithNoTests |
| Documentation | 8/10 | 7 CLI commands undocumented |
| Architecture & Code Quality | 9/10 | Minimal debt, clean patterns |
| Scaling & HA | 4/10 | CPU over-committed, 1 HPA total |
| Deployment Safety | 5/10 | Canary designed but not operational |
| Operational Maturity | 6/10 | Missing incident response, logging |

---

## Phase 0: Emergency Fixes (Session 106 — Committed)

### 0A. RBAC Namespace Bug — DONE

**File**: `infra/k8s/base/rbac.yaml` line 80
**Bug**: ClusterRoleBinding referenced `namespace: default` instead of `enclii`
**Fix**: Changed to `namespace: enclii`
**Risk**: Production kustomization masked this, but base was explicitly wrong.

### 0B. Integration Tests Go Version — DONE

**File**: `tests/integration/go.mod`
**Bug**: `go 1.21` while rest of project is `go 1.25.0`
**Fix**: Updated to `go 1.25.0` + `go mod tidy`

### 0C. CI passWithNoTests Removal — DONE

**File**: `.github/workflows/ci.yml`
**Bug**: `--passWithNoTests` flag on switchyard-ui, dispatch, and status test steps allowed empty suites to pass
**Fix**: Removed flag from mandatory test suites (159, 123, 129 tests respectively). Kept for shared-lib and ui-components (smaller packages).

---

## Phase 1: Cluster Operations (Requires SSH + Secrets)

### 1A. Disk Cleanup — P0

```bash
sudo k3s crictl rmi --prune                                    # ~15 GB
kubectl delete pvc --all -n posthog --ignore-not-found         # ~44 GB (PostHog)
kubectl delete volumes.longhorn.io <detached> -n longhorn-system
sudo journalctl --vacuum-size=500M                              # ~4 GB
sudo find /var/log -name "*.gz" -mtime +7 -delete              # ~3 GB
```
Target: 83% -> ~33% disk usage.

### 1B. Longhorn CPU Fix — P0

```bash
helm upgrade longhorn longhorn/longhorn -n longhorn-system -f infra/helm/longhorn/values.yaml
```
Apply committed `guaranteedEngineManagerCPU: 3` / `guaranteedReplicaManagerCPU: 3`.
Target: 87% -> ~78% CPU allocated.

### 1C. Backup Credentials + Restore Drill — P1

```bash
kubectl create secret generic github-backup-credentials -n data \
  --from-literal=github-pat="<PAT>"
kubectl create secret generic cloudflare-api-credentials -n data \
  --from-literal=api-token="<TOKEN>" \
  --from-literal=zone-id-enclii="<ZONE_ID>" \
  --from-literal=zone-id-madfam="<ZONE_ID>" \
  --from-literal=account-id="<ACCOUNT_ID>"
kubectl create job restore-drill-manual --from=cronjob/postgres-restore-drill -n data
```

### 1D. Vault Init/Unseal/Migrate — P1

Full runbook: `docs/runbooks/CLUSTER_REMEDIATION_OPS.md` section 3.
8 sequential steps: init -> unseal (3/5 keys) -> KV engine -> K8s auth -> ESO policy ->
migrate 160 keys -> ClusterSecretStore -> verify ExternalSecrets.

### 1E. Cosign Enforcement — P1

```bash
# Phase 1: kubectl label namespace enclii enclii.dev/verify-signatures=true
# Phase 2: status, monitoring namespaces
# Phase 3: enclii-builds namespace
```

### 1F. ArgoCD Sync Sweep + PostHog Namespace Deletion — P2

```bash
kubectl delete namespace posthog --timeout=120s
for app in $(kubectl get applications -n argocd -o json | \
  jq -r '.items[] | select(.status.sync.status != "Synced") | .metadata.name'); do
  kubectl patch application "$app" -n argocd --type merge \
    -p '{"operation":{"sync":{"revision":"HEAD","prune":true}}}'
done
```

---

## Phase 2: Test Coverage Expansion

### Priority Tiers

**Tier 1 — Easy, High Value (4 packages, ~52 tests)**

| Package | Approach | Difficulty |
|---------|----------|-----------|
| `internal/monitoring/` | Unit — prometheus counters (in-memory) | Easy |
| `internal/compliance/` | Unit — HTTP mocks for webhook delivery | Easy |
| `internal/clients/` | Unit — httptest for roundhouse client | Easy |
| `internal/notifications/` | Unit — HTTP mocks + testutil for webhooks | Easy |

**Tier 2 — Moderate, Injectable Dependencies (4 packages, ~42 tests)**

| Package | Approach | Difficulty |
|---------|----------|-----------|
| `internal/health/` | Unit — mock DB/cache/k8s, parallel checker | Moderate |
| `internal/lockbox/` | Unit — HTTP mocks for Vault API | Moderate |
| `internal/logging/` | Unit — logrus hooks, field extraction | Moderate |
| `internal/storage/` | Unit — mock S3Client interface | Moderate |

**Tier 3 — Hard, Integration Required (4 packages, ~40 tests)**

| Package | Approach | Difficulty |
|---------|----------|-----------|
| `internal/provisioning/` | Integration — testutil.RequireTestDB (expand validate_test) | Hard |
| `internal/topology/` | Integration — testutil + fixture services | Hard |
| `internal/rotation/` | Integration — testutil + k8s mocks | Hard |
| `internal/backup/` | Integration — testutil + mocked exec/S3 | Hard |

**Skip List (Low ROI)**

| Package | Reason |
|---------|--------|
| `internal/sbom/` | Requires external `syft` binary |
| `internal/github/` | Complex crypto + real API mocking |
| `internal/k8s/` | 40+ functions, all require K8s client mock |
| `cmd/*` | Thin main() wrappers |

---

## Phase 3: CLI Documentation

7 new command docs + CLI README update. Template follows `deploy.md` format.

| File | Command | Subcommands |
|------|---------|-------------|
| `docs/cli/commands/secrets.md` | `enclii secrets` | set, list, delete, get |
| `docs/cli/commands/domains.md` | `enclii domains` | list, add, remove, verify, status |
| `docs/cli/commands/functions.md` | `enclii functions` | list, deploy, logs, invoke, delete, info |
| `docs/cli/commands/jobs.md` | `enclii jobs` | list, create, get, delete, runs, run-once |
| `docs/cli/commands/junctions.md` | `enclii junctions` | list, add, get, delete |
| `docs/cli/commands/releases.md` | `enclii releases` | (list releases for a service) |
| `docs/cli/commands/services-delete.md` | `enclii services delete` | (delete with safeguards) |

---

## Phase 4: Infrastructure Hardening

### 4A. AlertManager Enhancement

- Migrate SMTP credentials to Vault ExternalSecret
- Add Slack webhook receiver for critical alerts
- Add PagerDuty/Opsgenie for on-call escalation
- Switch AlertManager storage from emptyDir to PVC

### 4B. Centralized Logging (Loki + Fluent Bit)

- Fluent Bit DaemonSet (collect pod logs)
- Loki StatefulSet (store + query, R2 backend)
- Grafana datasource integration
- 7-30 day retention policy

### 4C. Grafana Dashboards (Version-Controlled)

- Cluster capacity overview
- API latency + error rates
- Build pipeline metrics
- Cost trends (waybill)
- Longhorn volume health
- ArgoCD sync status

### 4D. HPA for Critical Services

- switchyard-api: 2-5 replicas (CPU 70%, memory 80%)
- switchyard-ui: 2-4 replicas (CPU 70%)
- roundhouse-api: 1-3 replicas (CPU 80%)

### 4E. Kyverno Policy Escalation

- `require-resource-limits`: Audit -> Enforce
- `require-probes`: Audit -> Enforce
- Add `disallow-latest-tags` policy

### 4F. Prometheus HA

- Scale to 2 replicas
- Add PDB with minAvailable: 1

---

## Phase 5: Dependency & Security Upgrades

### 5A. k8s.io Client Library Upgrade

Roundhouse: k8s.io v0.29.0 -> v0.33.0 (match cluster v1.33.7)

### 5B. Kyverno Registry Tightening

- Restrict docker.io whitelist to specific images
- Block `:latest` tags
- Pin Kyverno chart version

### 5C. k3s Encryption at Rest

- Create EncryptionConfiguration
- Add `--encryption-provider-config` to k3s
- Verify secrets encrypted in etcd

### 5D. Dev Secrets Cleanup

- Regenerate dev JWT keys
- Remove RSA private key from `secrets.dev.yaml`
- Use `kubectl create secret` instead of git-tracked files

---

## Phase 6: Ecosystem Repo Hygiene

### 6A. Failing CI Fixes

| Repo | CI Issue | Action |
|------|----------|--------|
| forgesight | Test automation failing (main) | Investigate + fix |
| karafiel | CI failing (main) | Investigate + fix |
| pravara-mes | Security scan failing (main) | Fix gosec/trivy findings |
| dhanam | Dependabot PR failing | Triage/merge/close |

### 6B. Stale Branch Cleanup

| Repo | Branches | Open PRs |
|------|----------|----------|
| madfam-site | 67 | 15 |
| dhanam | 27 | 25 |
| tezca | 18 | 0 |
| forgesight | 8 | 0 |

### 6C. agents-api.madfam.io Health

Image built but pod not rolled. Force ArgoCD sync or `kubectl rollout restart`.

---

## Phase 7: Operational Maturity

### 7A. Incident Response Runbook

Create `docs/runbooks/INCIDENT_RESPONSE.md`:
- Severity classification (P1-P4)
- Escalation matrix
- Communication templates
- Postmortem process

### 7B. k3s Upgrade Procedure

Create `docs/runbooks/K3S_UPGRADE.md`:
- Pre-upgrade backup
- Cordon + drain sequence
- Binary upgrade steps
- CRD migration notes
- Rollback procedure

### 7C. Automatic Rollback

Wire up designed-but-not-implemented auto-rollback:
- Monitor error rate post-deployment
- Trigger rollback if >2% errors for 2 min
- Alert on auto-rollback events

### 7D. Broken Docs Links

- Fix 60+ broken links in docusaurus config
- Change `onBrokenLinks: 'warn'` to `'throw'`
- Add link check to CI

### 7E. Feature Flags

- PostHog feature flag integration
- Per-user/per-org flag API
- Kill switches and gradual rollout

---

## Phase 8: Long-Term (Q2 2026)

| Item | Est. | Dependency |
|------|------|-----------|
| Multi-node cluster (3rd node) | 1 week | Hardware |
| Longhorn multi-replica (1->2) | 2 hrs | 3rd node |
| ESO CRD migration (0.9.11->0.16.2) | 45 min | Maintenance window |
| Distributed tracing (OTel + Tempo) | 8 hrs | Loki first |
| PostgreSQL HA (Patroni) | 1 week | SLA >99.9% |
| Redis Sentinel | 4 hrs | SLA >99.9% |
| Node auto-repair | 4 hrs | Custom health agent |
| Bundle size enforcement | 1 hr | Agreement on limits |
| GPU node deployment | 2 hrs | Hardware |

---

## Resolved Items (Session 106)

| Item | Commit | Details |
|------|--------|---------|
| Vault health probes | `69768be` | `uninitcode=200&sealedcode=200` |
| Prometheus retention | `69768be` | 15d/15GB -> 7d/8GB |
| Roundhouse Redis auth | `69768be` | Config resolves URL from components |
| PostHog cleanup | `69768be` | ArgoCD app + manifests removed, archived |
| 44 new tests | `69768be` | 30 reconciler + 14 cron_job_run repo |
| CI load-test dedup | `69768be` | Removed duplicate k6 smoke from ci.yml |
| Resource right-sizing | `69768be` | CPU limits: api 800m, cloudflared 300m, ui 300m |
| CI Go version | `0ea6fd6` | 1.24.13 -> 1.25.0 (workflows + modules + Dockerfiles) |
| nexus-api /health | `90804e2` | Root health endpoint (autoswarm-office) |
| Colyseus Node.js | `90804e2` | 20 -> 22 (TypeScript support) |
| CMS /health | `a6c8f5a` | Root health endpoint (madfam-site) |
| RBAC namespace fix | Pending | `default` -> `enclii` in ClusterRoleBinding |
| Integration go.mod | Pending | 1.21 -> 1.25.0 |
| CI passWithNoTests | Pending | Removed from mandatory test suites |

---

## Verification Checklist (Post All Phases)

```
Core Platform:
  [ ] curl https://api.enclii.dev/health -> 200
  [ ] curl https://app.enclii.dev -> 200
  [ ] kubectl get pods -A | grep -v Running | grep -v Completed -> only external

CI Pipeline:
  [ ] All CI jobs green (Build, Unit Tests, UI Tests, Security, E2E)
  [ ] Docker builds succeed for all 3 Go services
  [ ] Coverage threshold met (50%)

Cluster:
  [ ] df -h / -> <40%
  [ ] kubectl top nodes -> CPU <75%
  [ ] kubectl get applications -n argocd -> all Synced/Healthy (minus network-policies)
  [ ] Vault status: Sealed=false
  [ ] Backup jobs completing successfully
  [ ] Restore drill passed

Ecosystem:
  [ ] All 21 public endpoints healthy (200 or 307)
  [ ] All 8 ecosystem repos: CI green on main
  [ ] Stale branches cleaned
  [ ] Dependabot PRs triaged

Tests:
  [ ] go test ./... passes all modules
  [ ] 130+ new tests from Tier 1-3 packages
  [ ] pnpm test passes all UI apps (no passWithNoTests)

Documentation:
  [ ] 7 CLI command docs created
  [ ] CLI README updated
  [ ] Incident response runbook created
  [ ] k3s upgrade procedure documented
```
