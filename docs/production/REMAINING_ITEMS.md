# Remaining Items — Post Session 103

> **Last updated:** 2026-03-18 (Session 103)
> **Platform status:** Production Release Candidate v0.1.0 (95% ready)
> **Cluster:** 2-node k3s (foundry-core + foundry-builder-01), ~150 pods, 0 crashing

This document is the single source of truth for every remaining actionable item
across the enclii platform and ecosystem. Items are organized by execution context
(cluster vs. code), priority, and dependency order.

---

## Quick Reference — Execution Order

```
P0  [Cluster] Capacity cleanup (disk prune + detached volumes + log rotation)  ~15 min
P1  [Cluster] Backup credentials (GitHub PAT + Cloudflare API token)           ~10 min
P1  [Cluster] Vault init → unseal → configure → migrate secrets               ~60 min
P1  [Cluster] Restore drill (validate backup pipeline)                         ~15 min
P1  [Cluster] Cosign enforce activation (per-namespace, phased)                ~20 min
P2  [Cluster] Legacy CronJob cleanup (2 orphaned jobs)                          ~2 min
P2  [Cluster] ArgoCD sync sweep (post-Vault)                                   ~10 min
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

## Section 1: Cluster Operations (Require SSH + Secrets)

All cluster commands use:
```bash
ssh -o ProxyCommand="cloudflared access ssh --hostname %h" solarpunk@ssh.madfam.io "sudo kubectl ..."
```

### 1A. Capacity Cleanup — P0, ~15 min

Disk is at 83%, CPU requests at 87%. These must be addressed first.

| Task | Command | Recovery | Reference |
|------|---------|----------|-----------|
| **Prune container images** | `sudo k3s crictl rmi --prune` | ~10-20 GB | CAPACITY_ROADMAP.md:46 |
| **Delete PostHog detached volumes** | `kubectl delete pvc --all -n posthog --ignore-not-found` then `kubectl delete volumes.longhorn.io <name> -n longhorn-system` for any remaining detached | ~44 GB | CLUSTER_REMEDIATION_OPS.md §5 |
| **Apply Longhorn CPU fix** | Reduce `guaranteedEngineManagerCPU` from 12 → 3 via Longhorn UI or API | 1,080m CPU | CAPACITY_ROADMAP.md:58, Helm values updated Session 79 but not applied |
| **Log rotation** | `sudo journalctl --vacuum-size=500M` + `sudo find /var/log -name "*.gz" -mtime +7 -delete` | ~3-4 GB | CAPACITY_ROADMAP.md:85 |

**Forecast without cleanup:** 95% disk reached ~April 10, 2026 (0.43 GB/day growth).

### 1B. Backup Credential Provisioning — P1, ~10 min

Two backup CronJobs fail on every scheduled run because their secrets don't exist yet.

**GitHub Backup Secret** (`github-backup-credentials` in `data` namespace):
```bash
sudo kubectl create secret generic github-backup-credentials -n data \
  --from-literal=github-pat="<MADFAM_BOT_PAT_WITH_REPO_READ>" \
  --dry-run=client -o yaml | sudo kubectl apply -f -
```
- Template: `infra/k8s/production/backup/github-backup-secrets.yaml.template`
- Consumer: `github-repos-backup` CronJob (daily 1:00 AM UTC)

**Cloudflare API Secret** (`cloudflare-api-credentials` in `data` namespace):
```bash
sudo kubectl create secret generic cloudflare-api-credentials -n data \
  --from-literal=api-token="<CF_API_TOKEN_DNS_READ_TUNNEL_READ>" \
  --from-literal=zone-id-enclii="<ENCLII_DEV_ZONE_ID>" \
  --from-literal=zone-id-madfam="<MADFAM_IO_ZONE_ID>" \
  --from-literal=account-id="<CF_ACCOUNT_ID>" \
  --dry-run=client -o yaml | sudo kubectl apply -f -
```
- Template: `infra/k8s/production/backup/cloudflare-api-secrets.yaml.template`
- Consumer: `cloudflare-config-backup` CronJob (daily 1:15 AM UTC)

**Verify:**
```bash
sudo kubectl create job github-backup-test --from=cronjob/github-repos-backup -n data
sudo kubectl create job cf-backup-test --from=cronjob/cloudflare-config-backup -n data
sudo kubectl get jobs -n data --watch  # both should Complete
```

### 1C. Vault Deployment — P1, ~60 min (8 sequential steps)

Vault pod is Running 1/1 but uninitialized. All manifests are committed.
Full runbook: `docs/runbooks/CLUSTER_REMEDIATION_OPS.md` §3

**Step 1 — Initialize:**
```bash
sudo kubectl exec -n vault vault-0 -- vault operator init \
  -key-shares=5 -key-threshold=3 -format=json > vault-init.json
# CRITICAL: Store vault-init.json off-cluster immediately (unseal keys are irreplaceable)
```

**Step 2 — Unseal (3 of 5 keys):**
```bash
sudo kubectl exec -n vault vault-0 -- vault operator unseal <KEY_1>
sudo kubectl exec -n vault vault-0 -- vault operator unseal <KEY_2>
sudo kubectl exec -n vault vault-0 -- vault operator unseal <KEY_3>
```

**Step 3 — Enable KV engine:**
```bash
sudo kubectl exec -n vault vault-0 -- vault secrets enable -path=secret kv-v2
```

**Step 4 — Configure K8s auth:**
```bash
sudo kubectl exec -n vault vault-0 -- vault auth enable kubernetes
sudo kubectl exec -n vault vault-0 -- sh -c 'vault write auth/kubernetes/config \
  kubernetes_host="https://kubernetes.default.svc:443" \
  kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
  token_reviewer_jwt=@/var/run/secrets/kubernetes.io/serviceaccount/token'
```

**Step 5 — Create ESO reader policy + role:**
```bash
sudo kubectl exec -n vault vault-0 -- sh -c 'vault policy write eso-reader - <<POLICY
path "secret/data/*" { capabilities = ["read", "list"] }
POLICY'

sudo kubectl exec -n vault vault-0 -- vault write auth/kubernetes/role/eso-reader \
  bound_service_account_names=external-secrets \
  bound_service_account_namespaces=external-secrets \
  policies=eso-reader ttl=1h
```

**Step 6 — Migrate secrets (19 namespaces, ~160 keys):**
```bash
# Dry run first
./scripts/vault-secret-migration.sh --all --dry-run --verbose

# Actual migration
./scripts/vault-secret-migration.sh --all --verbose
```

**Step 7 — Deploy ClusterSecretStore + ExternalSecrets:**
```bash
sudo kubectl apply -f infra/k8s/base/external-secrets/vault-cluster-secret-store.yaml
sudo kubectl apply -f infra/k8s/base/external-secrets/vault-secrets/
```

**Step 8 — Verify:**
```bash
sudo kubectl exec -n vault vault-0 -- vault status  # Sealed=false
sudo kubectl get externalsecrets -A                  # all SecretSynced
```

**Risk:** Medium. Unseal keys are irreplaceable. Back up `vault-init.json` to multiple secure locations before proceeding.

### 1D. Restore Drill — P1, ~15 min

Run after backup credentials exist (1B). Non-destructive.

```bash
sudo kubectl create job restore-drill-manual --from=cronjob/postgres-restore-drill -n data
sudo kubectl logs -n data job/restore-drill-manual -f
```

Expected output: `=== RESTORE DRILL PASSED ===` with table count > 0.

### 1E. Cosign Enforce Activation — P1, ~20 min

Policy is committed in Enforce mode (`infra/k8s/base/kyverno/policies/image-policies.yaml`).
Activation is per-namespace via label.

**Phase 1 (safe — all first-party images):**
```bash
sudo kubectl label namespace enclii enclii.dev/verify-signatures=true
```

**Phase 2:**
```bash
sudo kubectl label namespace status enclii.dev/verify-signatures=true
sudo kubectl label namespace monitoring enclii.dev/verify-signatures=true
```

**Phase 3:**
```bash
sudo kubectl label namespace enclii-builds enclii.dev/verify-signatures=true
```

**Verify before each phase:**
```bash
# Check all images in a namespace are signed
for pod in $(sudo kubectl get pods -n enclii -o jsonpath='{.items[*].spec.containers[*].image}'); do
  cosign verify --certificate-oidc-issuer="https://token.actions.githubusercontent.com" "$pod" 2>/dev/null && echo "OK: $pod" || echo "UNSIGNED: $pod"
done
```

Ecosystem namespaces have PolicyExceptions and should NOT be labeled until their images are signed.

### 1F. Legacy CronJob Cleanup — P2, ~2 min

```bash
sudo kubectl delete cronjob image-cleanup -n kube-system --ignore-not-found
sudo kubectl delete cronjob disk-cleanup -n enclii --ignore-not-found
```

### 1G. ArgoCD Sync Sweep — P2, ~10 min

Run after Vault deployment (1C) to verify all apps are healthy.

```bash
# Force sync vault
sudo kubectl patch application vault -n argocd --type merge \
  -p '{"operation":{"sync":{"prune":true}}}'

# Full status
sudo kubectl get application -n argocd \
  -o custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status
```

**Expected permanently OutOfSync:** `network-policies` (prune:false by design), `posthog` (sync paused).

---

## Section 2: Deferred Items (Not Blocking v0.1.0)

### 2A. ESO CRD Migration (0.9.11 → 0.16.2) — Needs Maintenance Window

| Aspect | Detail |
|--------|--------|
| **Current version** | v0.9.11 (v1beta1 CRDs), stable |
| **Target version** | v0.16.2 (v1 CRDs) |
| **Risk** | High — affects all ExternalSecrets cluster-wide |
| **Duration** | ~45 min with rollback plan |
| **Prerequisite** | Vault deployed and all ExternalSecrets syncing |
| **Full plan** | `docs/runbooks/CLUSTER_REMEDIATION_OPS.md` §7 |

Migration steps: backup CRDs → scale down ESO → update ArgoCD targetRevision → verify CRD migration → scale up → verify all ExternalSecrets reconcile.

### 2B. PagerDuty/Opsgenie Integration

AlertManager is deployed with alert rules, but notifications go nowhere.
Need to configure a receiver in `infra/k8s/production/monitoring/prometheus.yaml` and add the integration secret.

### 2C. PostgreSQL HA (Patroni/CloudNativePG)

Required when SLA target exceeds 99.9%. Redis Sentinel manifests staged at `infra/k8s/production/redis-sentinel.yaml`.

### 2D. Multi-Node Longhorn Replication

When 3rd storage node is added: increase `defaultReplicaCount` from 1 → 2 in `infra/helm/longhorn/values.yaml`, enable `replicaAutoBalance: best-effort`.

### 2E. GPU Node Setup

Manifests ready at `infra/k8s/base/gpu/` (NVIDIA device plugin DaemonSet + README). Awaiting GPU hardware.

### 2F. Multi-Region

Explicitly out of scope for v1 per `docs/architecture/SOFTWARE_SPEC.md`.

### 2G. Handler Legacy Pattern Migration

Incremental: migrate handlers from direct repo access to service layer as they're touched for other work. No dedicated sprint needed.

### 2H. Image Digest Pinning for Ecosystem Repos

Out of scope for enclii repo. Each ecosystem repo (forgesight, karafiel, pravara-mes, etc.) should pin their own images.

### 2I. KEDA Runtime for Serverless Functions

KEDA operator + HTTP add-on ArgoCD app is staged but not synced. Functions API + UI are complete. Deploy when scale-to-zero is needed.

---

## Section 3: Known Issues (No Fix Available)

| Issue | Impact | Workaround | Reference |
|-------|--------|------------|-----------|
| **ArgoCD OCI multi-source bug** (v3.2.5) | 2 ARC apps show Unknown/Healthy | Cosmetic — pods functional | PRODUCTION_CHECKLIST.md:296 |
| **PostHog Helm chart v30.46 broken** | Cannot self-host PostHog | Cloudflare Worker proxy to PostHog Cloud (`analytics.madfam.io`) | PRODUCTION_CHECKLIST.md:304 |
| **Longhorn EXT4 corruption pattern** | ~1 incident/month (5 total) | Manual PVC recreation per `docs/runbooks/LONGHORN_VOLUME_RECOVERY.md` | PRODUCTION_CHECKLIST.md:303 |
| **Janua DB backup workflow failing** | Janua-specific backups not running | Platform postgres-backup covers the DB; Janua-specific workflow needs investigation | PRODUCTION_CHECKLIST.md:298 |

---

## Section 4: Ecosystem Repos Status

All 7 ecosystem repos are clean on `main`, no uncommitted changes.

| Repo | Last Commit | CI | Notes |
|------|-------------|-----|-------|
| **madfam-site** | `b697882` — add network section | GREEN | No issues |
| **karafiel** | `c484cfd` — pgbouncer egress fix | GREEN | 2 stale branches |
| **forgesight** | `19081d5` — PostHog localStorage fix (S103) | GREEN | 3 stale branches |
| **yantra4d** | `d435a6d` — admin OIDC redirect | GREEN | 10 stale branches |
| **dhanam** | `a12b2a8` — admin image digest | GREEN | PR #37 open (fix/dhanam-admin-argocd-config), 7 stale branches |
| **pravara-mes** | `53c413a` — Next.js 16 build artifacts | GREEN | 1 stale branch |
| **autoswarm-office** | `726bf97` — GHCR_TOKEN PAT fix (S102) | GREEN | 18 stale branches |

**Stale branch cleanup:** Optional. ~41 stale remote branches across all repos. Safe to delete with `git push origin --delete <branch>`.

**Open PR:** `dhanam` PR #37 (fix/dhanam-admin-argocd-config) — review and merge or close.

---

## Section 5: Capacity Forecast

| Metric | Current | Threshold | Date at Risk | Growth Rate |
|--------|---------|-----------|--------------|-------------|
| **Disk** | 83% | 95% | ~April 10 | 0.43 GB/day |
| **CPU requested** | 87% | 95% | ~April 25 | Slow |
| **Pods** | ~150 | ~200 (etcd limit) | ~May | +1.9/day |
| **Longhorn volumes** | 17 | ~30 | ~June | +0.3/week |

After cleanup (Section 1A), disk drops to ~60% and CPU to ~78%, buying ~6-8 weeks.

---

## Section 6: Test Coverage Summary

| Module | Tests | Session |
|--------|-------|---------|
| switchyard-api (db, api, reconciler, services) | ~300+ | S97-103 |
| CLI (cmd, client, config) | 82 | S99 |
| SDK (client, types) | 30 | S97 |
| Dispatch (auth, API, components) | 123 | S98 |
| Status page (lib, config, health) | 129 | S68-69 |
| Switchyard UI (components, hooks) | 159 | Various |
| Provenance + Signing | 50 | S97 |
| Timetable + Junction | 81 | S103 |
| **Total** | **~950+** | |

CI threshold: 50% minimum (raised from 40% in Session 97). No `t.Skip("TODO...")` stubs remaining.

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
