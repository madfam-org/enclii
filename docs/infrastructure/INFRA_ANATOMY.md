# Infrastructure Anatomy - Production State

> **Generated**: 2026-01-17 | **Last Updated**: 2026-04-08 | **Host**: foundry-cp [control-plane] + foundry-worker-01 [worker] + foundry-builder-01 [builder] | **Audit Type**: Full Cluster Audit (Capacity + Auth + Roadmap)
>
> **Live Status Check** (2026-04-07):
> - Nodes: 3/3 Ready (foundry-cp [EX44] + foundry-worker-01 [AX41] + foundry-builder-01 [VPS]), k3s v1.33.7+k3s3
> - Pods: ~150 total (est. 137 Running/Completed, 13 non-Running)
> - Disk usage: **83% (77G/98G) — P1, image pruning needed** (+16% in 35 days)
> - ArgoCD: 28 apps (11 infra + 2 ARC + 15 project-appset) — see ArgoCD table below
> - Endpoints: **37/46 operational**, 4 degraded (404), 3 backend-502, 2 not deployed (see Endpoint Health table)
> - CPU allocated: 10,460m/12,000m (87%) — Longhorn instance-manager 1440m fix NOT applied
> - Memory allocated: 24.9Gi/64Gi (39%) — adequate headroom
> - Longhorn: 14/17 volumes healthy, 3 detached/unknown (PostHog)
> - NetworkPolicies: 197+ across 16 namespaces
> - Kyverno: 16 cluster policies (7 Enforce, 9 Audit), 8 exceptions
> - Auth (Janua SSO): OIDC/PKCE verified on app.enclii.dev + admin.enclii.dev + app.dhan.am
> - Databases: All healthy (data/postgres, data/redis, enclii/redis)
> - No OOM kills, 8 HPAs within targets
>
> **Crash Summary (13 non-Running pods):**
> - roundhouse (3): Redis DNS timeout — `lookup redis: i/o timeout`
> - karafiel-worker (1): Redis DNS failure to `redis.data.svc.cluster.local`
> - tezca-beat (1): Celery beat crash after start
> - yantra4d-admin/studio (2): nginx `host not found in upstream "backend"`
> - posthog (5): Init containers stuck (ClickHouse dependency not ready)
> - sentinel infra-audit (1): CronJob Error
>
> **Service 502s (3 endpoints at time of audit):** 4d-api (yantra4d-backend unreachable), mes-api (pravara-api regression), mes-admin (resolved Session 100 — tunnel route fixed, NetworkPolicies created).
>
> **Degraded 404s (4 endpoints):** agents-api, api.tezca.mx, kf-api, cms.madfam.io — services running but no health/root endpoint.
>
> **Urgent Actions:**
> 1. Prune container images (`k3s crictl rmi --prune`) — 399 images consuming disk
> 2. Apply Longhorn CPU fix (1440m→360m) — saves 1080m CPU, drops allocation to 78%
> 3. Delete 3 detached Longhorn volumes (PostHog) — free ~44G
> 4. Order EX44 before April 1 price increase

## Executive Summary

| Category | Status | Severity |
|----------|--------|----------|
| **Overall Health** | 91% pods healthy, 80% endpoints operational | ⚠️ DEGRADED |
| **Endpoints** | 37/46 operational, 4 degraded (404), 3 backend-502, 2 not deployed | ⚠️ DEGRADED |
| **Pods** | ~150 total, ~137 Running/Completed, ~13 non-Running | ⚠️ DEGRADED |
| **Nodes** | 3/3 Ready, version matched (k3s v1.33.7+k3s3) | ✅ HEALTHY |
| **CPU (actual)** | core: 11% (1340m), builder: 1% (31m) | ✅ HEALTHY |
| **CPU (allocated)** | 10,460m/12,000m = 87% | ⚠️ TIGHT |
| **Memory (actual)** | core: 33% (21.5GB/64GB), builder: 28% (1.1GB/3.8GB) | ✅ HEALTHY |
| **Memory (allocated)** | 24.9Gi/64Gi = 39% | ✅ HEALTHY |
| **Disk** | core: 83% (77G/98G) — grew +16% in 35 days | 🔴 CRITICAL |
| **ArgoCD** | 28 apps (11 infra + 2 ARC + 15 project-appset) | ⚠️ DEGRADED |
| **Storage** | 18/18 PVCs bound, 14/17 Longhorn volumes healthy | ⚠️ DEGRADED |
| **Longhorn** | 17 volumes (~150GB allocated), 3 detached/unknown | ⚠️ DEGRADED |
| **Auth** | Janua OIDC/PKCE verified on 3 platforms | ✅ HEALTHY |
| **Databases** | All healthy (postgres, redis in data + enclii) | ✅ HEALTHY |
| **NetworkPolicies** | 197+ across 16 namespaces (per-app ownership, no overlaps) | ✅ HEALTHY |
| **Kyverno** | 16 policies, 8 exceptions | ✅ HEALTHY |
| **Cost** | see internal-devops | ✅ ON TARGET |

### Wave 15 Changes (Wave 14 → Wave 15)

**Session 1 (hardening):**

| Item | Before | After | Status |
|------|--------|-------|--------|
| Cloudflared | 2025.11.1 | 2026.1.2 (SHA-pinned) | **UPDATED** |
| docs-site image | `:latest` (mutable) | SHA-pinned digest | **PINNED** |
| landing-page image | `:latest` (mutable) | SHA-pinned digest | **PINNED** |
| switchyard-api image | `switchyard-api:latest` (no registry) | Full GHCR path + SHA digest | **PINNED** |
| switchyard-ui image | `switchyard-ui:latest` (no registry) | Full GHCR path + SHA digest | **PINNED** |
| Secrets strategy | Doppler references in config | Vault chosen, Doppler refs removed | **CLARIFIED** |
| External Secrets README | Doppler-focused docs | Vault-focused with trigger criteria | **UPDATED** |
| Redis secret TODO | "Migrate to Doppler" | "Migrate when Vault deployed" | **UPDATED** |

**Session 2 (ArgoCD expansion + dhanam stabilization):**

| Item | Before | After | Status |
|------|--------|-------|--------|
| ArgoCD apps | 14 | 16 (+janua-services, +dhanam-services) | **EXPANDED** |
| janua ArgoCD | Manual deploy only | Synced/Healthy, auto-sync with prune+selfHeal | **ONBOARDED** |
| dhanam ArgoCD | Manual deploy only | OutOfSync/Healthy, visibility-only (auto-sync disabled) | **ONBOARDED** |
| Dhanam ResourceQuota | CPU 4, memory 6Gi | CPU 6, memory 8Gi (rolling update headroom) | **INCREASED** |
| Dhanam imagePullPolicy | Always (default) | IfNotPresent (expired GHCR tags use node cache) | **PATCHED** |
| Dhanam container names | Manifest `web` vs live `dhanam-web` (mismatch) | Aligned to `dhanam-web` in manifest + 3 CI workflows | **FIXED** |
| Dhanam API container | CI uses `dhanam-api`, manifest uses `api` | CI fixed to use `api` | **FIXED** |
| ARC runner probes | Missing base values file in blue runner set | Base values file chain added | **FIXED** |
| Golden configs | 5/20 entries in update script, CI failing | 20/20 synced and passing | **FIXED** |
| DR runbook | Not documented | `docs/runbooks/DISASTER_RECOVERY.md` created | **CREATED** |
| auth.madfam.io | ✅ Healthy | **Brief 502 outage** (ArgoCD prune incident) → restored | **INCIDENT** |

**Session 3 (dhanam CI + image pinning + auto-sync):**

| Item | Before | After | Status |
|------|--------|-------|--------|
| enclii/registry-secret | `github-token` (personal) | `madfam-bot` (standardized) | **FIXED** |
| Registry secrets audit | 5 using personal creds | 0 personal (100% madfam-bot) | **STANDARDIZED** |
| core-services ignoreDifferences | Missing Secret data | Secret `/data` + `/stringData` ignored (prevents selfHeal overwrite) | **FIXED** |
| Dhanam GHCR images | Expired :89438293 tags, push failing | Fresh :main tags via nested naming (`dhanam/api`, `dhanam/web`) | **FIXED** |
| Dhanam CI auth | `MADFAM_BOT_PAT` (failing) | `GITHUB_TOKEN` + `provenance: false` | **FIXED** |
| Dhanam web targetPort | Service: 3000, app: 4200 (mismatch → 502) | All aligned to 4200 (deployment, service, probes) | **FIXED** |
| Dhanam ArgoCD auto-sync | Disabled (blocked on images) | Enabled: selfHeal=true + ServerSideApply | **ENABLED** |
| Janua image pinning | `:main` mutable tags, no kustomization.yaml | SHA-pinned via kustomization.yaml + Image Updater auto-update | **PINNED** |
| Janua GHCR credentials | Expired/different PAT | Working madfam-bot PAT (matched enclii) | **FIXED** |
| PostHogProvider.tsx | TypeScript error blocking CI | Fixed `match[1]` → `match?.[1]` optional chaining | **FIXED** |
| Orphaned GHCR packages | `dhanam-api` unlinked (repo: null) | Bypassed via rename to `dhanam/api` (auto-linked) | **WORKAROUND** |

### Wave 14 Changes (Wave 13 → Wave 14)

| Item | Before | After | Status |
|------|--------|-------|--------|
| Prometheus | ❌ CrashLoopBackOff (11+ restarts, stuck rollout) | ✅ Running (Recreate strategy) | **FIXED** |
| Monitoring ArgoCD | 🔴 Degraded | ✅ Healthy | **FIXED** |
| api.dhan.am latency | ⚠️ 2.5s | ✅ 0.56s | **RESOLVED** |
| Pod count | 98 | 84 | **CLEANED** (orphan pods removed) |
| Redis (data namespace) | ❌ Missing | ✅ Deployed with auth | **DEPLOYED** |
| Monitoring PolicyException | ❌ Missing | ✅ Docker Hub images allowed | **CREATED** |
| kube-system pods | ⚠️ 59 days old | ✅ Refreshed | **DONE** |

### Wave 13 Changes (Wave 12 → Wave 13)

| Item | Before | After | Status |
|------|--------|-------|--------|
| PostgreSQL | ❌ CrashLoopBackOff (35+ restarts) | ✅ Running (0 restarts) | **FIXED** |
| core-services | 🔴 Degraded | ✅ Synced/Progressing | **FIXED** |
| Stale ReplicaSets | 115 orphaned | 0 orphaned | **CLEANED** |
| ArgoCD Sync | 8/13 Synced | 10/14 Synced | **IMPROVED** |
| Kyverno PolicyException | ❌ Missing | ✅ postgres-security-exception | **ADDED** |

### Historical Issues (All Resolved)

| Category | Status | Resolution Date |
|----------|--------|-----------------|
| **Architecture Conflict** | K8s-only (systemd disabled) | ✅ Jan 17 |
| **Disk Pressure** | 87% usage | ✅ Jan 25 |
| **Database Exposure** | 127.0.0.1 binding | ✅ Jan 17 |
| **OIDC Endpoints** | auth.madfam.io operational | ✅ Jan 17 |
| **Switchyard API** | api.enclii.dev operational | ✅ Jan 17 |
| **Redis URL Drift** | K8s internal DNS | ✅ Jan 17 |
| **Port Mismatch** | Standardized on 4200 | ✅ Jan 25 |
| **ImagePullBackOff** | 0 pods | ✅ Jan 25 |
| **Pod Evictions** | 0 pods | ✅ Jan 25 |
| **VPS Builder Node** | CNI fixed (k3s version match) | ✅ Jan 25 |
| **Dual Cloudflared** | Consolidated | ✅ Jan 26 |
| **Kyverno CronJobs** | bitnami/kubectl:latest | ✅ Jan 26 |
| **Grafana CrashLoop** | PVC and ConfigMap fixed | ✅ Jan 25 |
| **dhanam-api CrashLoop** | TCP probes | ✅ Jan 25 |
| **PostgreSQL CrashLoop** | Kyverno PolicyException | ✅ Feb 3 |

---

## Host Details

| Node | IP | Role | Hardware | k3s | CPU | RAM | Status | Uptime |
|------|----|------|----------|-----|-----|-----|--------|--------|
| **foundry-cp** | <CONTROL_PLANE_IP> | control-plane, master | Hetzner EX44 (i5-13500, 128GB, 2x512GB NVMe Gen4) | v1.33.7+k3s3 | — | — | ✅ Ready | new |
| **foundry-worker-01** | <WORKER_NODE_IP> | worker | Hetzner AX41-NVMe (Ryzen 5 3600, 64GB) | v1.33.7+k3s3 | 12% | 27% (17.7GB/64GB) | ✅ Ready | 62 days |
| **foundry-builder-01** | <WORKER_NODE_IP> | worker (role=builder) | VPS ("The Forge") | v1.33.7+k3s3 | 1% | 33% (1.2GB/4GB) | ✅ Ready | 18 days |

- **OS**: Ubuntu 24.04.3 LTS (Noble Numbat)
- **Kernel**: 6.8.0-88-generic
- **Node Count**: 3 (since Apr 2026; was 2 from Jan-Apr 2026)
- **Builder Node**: Tainted `builder=true:NoSchedule` — only ARC runner pods schedule here
- **Resource Headroom**: 94-99% CPU available, 69-81% Memory available

---

## Architecture Overview

### Unified K8s-Only Architecture

All services run exclusively in K8s. Docker containers (Verdaccio, registry) run on the host for non-K8s workloads. systemd tunnels disabled.

```
┌─────────────────────────────────────────────────────────────────┐
│              CLOUDFLARE TUNNEL (single unified)                  │
│  K8s: cloudflared pods (2 replicas, v2026.1.2)                  │
│  Config: infra/k8s/production/cloudflared-unified.yaml          │
│  Routes: ~54 production domains (managed via Cloudflare API)      │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                K8s CLUSTER (K3s, 3 nodes)                       │
│                                                                 │
│  foundry-cp (control-plane, EX44):                              │
│    janua-api.janua.svc (80)       switchyard-api.enclii.svc (80)│
│    janua-dashboard.janua.svc      dispatch.enclii.svc (80)      │
│    postgres.enclii.svc (5432)     redis.data.svc (6379)         │
│    grafana.monitoring.svc (3000)  prometheus.monitoring.svc      │
│    argocd-server.argocd.svc       dhanam-api.dhanam.svc (80)    │
│                                                                 │
│  foundry-builder-01 (worker, builder taint):                    │
│    arc-runner-blue pods (GitHub Actions CI)                     │
│                                                                 │
│  Host-level Docker:                                             │
│    verdaccio (4873) — npm registry                              │
│    foundry-registry (5000) — container registry                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Namespaces (22 active as of Mar 13, 2026)

| Namespace | Purpose | Pod Count (est.) | Status |
|-----------|---------|-------------------|--------|
| `longhorn-system` | Block Storage CSI (v1.7.2) | 20 | ✅ Healthy |
| `enclii` | Platform Control Plane | 18 | ✅ Healthy |
| `argocd` | GitOps Engine | 8 | ✅ Healthy |
| `autoswarm` | Agent Orchestration (6 services) | 8 | ✅ Healthy |
| `kyverno` | Policy Engine (v1.11.4) | 6 | ✅ Healthy |
| `janua` | Identity Provider | 6 | ✅ Healthy |
| `dhanam` | Finance Services | 6 | ✅ Healthy |
| `tezca` | E-commerce Platform | 6 | ⚠️ Degraded (beat crash) |
| `external-secrets` | Secret Management (ESO v0.9.11) | 5 | ✅ Healthy |
| `yantra4d` | 3D Printing Platform | 5 | ⚠️ Degraded (admin/studio) |
| `karafiel` | CMS Platform | 4 | ⚠️ Degraded (worker crash) |
| `pravara-mes` | Manufacturing Execution System | 6 | ⚠️ Degraded (API 502) |
| `monitoring` | Observability (Prometheus, Grafana) | 4 | ✅ Healthy |
| `forgesight` | Analytics Platform | 2 | ✅ Healthy |
| `kube-system` | K8s system components | 3 | ✅ Healthy |
| `cloudflare-tunnel` | Ingress (2 replicas) | 2 | ✅ Healthy |
| `data` | Shared Databases | 2 | ✅ Healthy |
| `arc-system` | ARC Controller | 2 | ✅ Healthy |
| `madfam-site` | MADFAM Marketing Site | 1 | ✅ Healthy |
| `npm-registry` | Verdaccio Private NPM | 1 | ✅ Healthy |
| `arc-runners` | GitHub Actions Runner Sets | 1 | ✅ Healthy |
| `cnpg-system` | CloudNative PG Operator | 1 | ✅ Healthy |
| `sentinel` | Infra Audit CronJob | 1 | ✅ Healthy |
| `enclii-builds` | CI/CD Build Jobs | - | ✅ Healthy |
| `default` | Default namespace | - | ✅ Empty |
| `kube-node-lease` | Node heartbeats | - | ✅ System |
| `kube-public` | Public info | - | ✅ System |

---

## Live Endpoint Health (Mar 13, 2026)

### Enclii Platform (8 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| api.enclii.dev | ✅ | 404/200 | 0.53s | Root 404, **/health returns 200** — operational |
| app.enclii.dev | ✅ | 200 | 0.62s | Dashboard responsive |
| admin.enclii.dev | ✅ | 307 | 0.70s | Auth redirect (expected) |
| enclii.dev | ✅ | 200 | 0.70s | Landing page |
| docs.enclii.dev | ✅ | 200 | 0.60s | Documentation operational |
| status.enclii.dev | ✅ | 200 | 0.93s | Status page operational |
| status.madfam.io | ✅ | 200 | 1.13s | MADFAM status operational |
| vault.madfam.io | ⏳ | 307 | 0.60s | Vault staged, not deployed (redirect, not 502) |

### Janua SSO (6 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| auth.madfam.io | ✅ | 200 | 1.36s | OIDC/JWKS available |
| api.janua.dev | ✅ | 200 | 0.61s | Janua API |
| app.janua.dev | ✅ | 307 | 0.61s | Auth redirect |
| admin.janua.dev | ✅ | 307 | 0.62s | Auth redirect |
| docs.janua.dev | ✅ | 200 | 0.62s | Janua docs |
| janua.dev | ✅ | 200 | 0.68s | Janua website |

### Dhanam (4 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| api.dhan.am | ✅ | 404/200 | 0.51s | Root 404, **/health returns 200** — operational |
| app.dhan.am | ✅ | 307 | 0.56s | Auth redirect |
| admin.dhan.am | ✅ | 307 | 0.56s | Auth redirect |
| dhan.am | ✅ | 307 | 0.55s | Redirect to app |

### AutoSwarm (5 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| agents.madfam.io | ✅ | 307 | 0.72s | Auth redirect (office-ui) |
| agents-admin.madfam.io | ✅ | 307 | 0.61s | Auth redirect |
| agents-ws.madfam.io | ✅ | 200 | 0.65s | /health returns 200 (colyseus) |
| agents-api.madfam.io | ⚠️ | 404 | 0.71s | No health endpoint found — service running but no root/health route |
| agents-gw.madfam.io | ⏳ | 502 | 0.60s | Expected — background worker, no HTTP listener |

### Tezca — tezca.mx (3 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| tezca.mx | ✅ | 200 | 0.55s | Web platform |
| admin.tezca.mx | ✅ | 307 | 0.56s | Auth redirect |
| api.tezca.mx | ⚠️ | 404 | 0.60s | No health endpoint (needs external repo change) |

### Forgesight — forgesight.quest (4 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| forgesight.quest | ✅ | 200 | 0.75s | Web platform |
| app.forgesight.quest | ✅ | 307 | 0.77s | Auth redirect |
| api.forgesight.quest | ✅ | 200 | 0.65s | API operational |
| admin.forgesight.quest | ✅ | 200 | 0.61s | Admin operational |

### Karafiel — karafiel.mx (4 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| karafiel.mx | ✅ | 307 | 0.60s | Redirect to app |
| app.karafiel.mx | ✅ | 307 | 0.63s | Auth redirect |
| admin.karafiel.mx | ✅ | 200 | 0.61s | Admin operational |
| api.karafiel.mx | ⚠️ | 404 | 0.70s | No health endpoint (needs external repo change) |

### Yantra4D — yantra4d.com (4 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| yantra4d.com | ✅ | 200 | 0.62s | Landing page |
| app.yantra4d.com | ✅ | 200 | 0.60s | Studio operational |
| admin.yantra4d.com | ✅ | 200 | 0.59s | Admin operational |
| api.yantra4d.com | 🔴 | 502 | 0.32s | Backend unreachable |

### Pravara MES — mes.madfam.io (3 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| mes.madfam.io | ✅ | 307 | 0.55s | MES web redirect |
| mes-api.madfam.io | 🔴 | 502 | 0.59s | **Regression** — was fixed session 78 |
| mes-admin.madfam.io | 🔴 | 502 | 0.56s | Known — pravara-gateway service never created |

### MADFAM Site — madfam.io (2+1 endpoints)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| madfam.io | ✅ | 307 | 0.42s | Redirect |
| cms.madfam.io | ⚠️ | 404 | 0.60s | CMS — no root endpoint |

### Summary

| Category | Count | Percentage |
|----------|-------|------------|
| **Operational** | 37 | 80% |
| **Not deployed / expected** | 2 | 4% |
| **Degraded (404, no health endpoint)** | 4 | 9% |
| **Backend 502** | 3 | 7% |
| **Total endpoints** | **46** | — |

**Backend 502 root causes:**
- `api.yantra4d.com` — yantra4d-backend unreachable (NetworkPolicy or pod crash)
- `mes-api.madfam.io` — pravara-api regression (was fixed session 78 — CrashLoopBackOff or NetworkPolicy relapse)
- `mes-admin.madfam.io` — pravara-gateway K8s service never created (known since session 75)

---

## Services by Namespace

### janua

| Service | Type | Port | targetPort | Status |
|---------|------|------|------------|--------|
| janua-api | ClusterIP | 80 | 8080 | ✅ |
| janua-dashboard | ClusterIP | 80 | 80 | ✅ |
| janua-admin | ClusterIP | 80 | 80 | ✅ |

> **Note:** `janua-docs` and `janua-website` services were deleted in Session 9 (orphaned — these services route via Cloudflare tunnel directly to the janua namespace deployments).

### enclii

| Service | Type | Port | targetPort | Status |
|---------|------|------|------------|--------|
| switchyard-api | ClusterIP | 80 | 4200 | ✅ |
| switchyard-ui | ClusterIP | 80 | 80 | ✅ |
| dispatch | ClusterIP | 80 | 80 | ✅ |
| roundhouse | ClusterIP | 80, 8080 | - | ✅ |
| waybill | ClusterIP | 80 | - | ✅ |
| docs-site | ClusterIP | 80 | - | ✅ |
| landing-page | ClusterIP | 80 | - | ✅ |
| status-enclii | ClusterIP | 80 | - | ✅ |
| status-madfam | ClusterIP | 80 | - | ✅ |
| postgres | ClusterIP | 5432 | 5432 | ✅ |
| redis | ClusterIP | 6379 | 6379 | ✅ |

### data

| Service | Type | Port | Status |
|---------|------|------|--------|
| postgres | ClusterIP (headless) | 5432 | ✅ |
| redis | ClusterIP | 6379 | ✅ |

### monitoring

| Service | Type | Port | Status |
|---------|------|------|--------|
| prometheus | ClusterIP | 9090 | ✅ |
| grafana | ClusterIP | 3000 | ✅ |
| alertmanager | ClusterIP | 9093 | ✅ |
| node-exporter | ClusterIP (DaemonSet) | 9100 | ✅ |

**Maintenance Observability (Mar 2026):**

The `node-maintenance` CronJob (daily 2:30 AM UTC) now exports Prometheus metrics via the node-exporter textfile collector:

| Metric | Type | Description |
|--------|------|-------------|
| `node_maintenance_last_run_timestamp_seconds` | gauge | Unix timestamp of last run |
| `node_maintenance_images_removed_total` | gauge | Container images pruned |
| `node_maintenance_containers_cleaned_total` | gauge | Exited containers cleaned |
| `node_maintenance_logs_freed_bytes` | gauge | Log space reclaimed (bytes) |
| `node_maintenance_disk_usage_percent` | gauge | Root disk % after cleanup |

**Alert rules** (in `prometheus-rules` ConfigMap):
- `MaintenanceCronJobFailed` — job failure detection (10m)
- `NodeMaintenanceStale` — no metrics for >48h
- `NodeDiskWillFillIn7Days` — `predict_linear` on root FS
- `LonghornOrphanedVolume` — detached volume >24h
- `LonghornVolumeDegraded` — degraded volume >30m

**Grafana dashboard**: `node-maintenance` (uid) — disk health, maintenance history, Longhorn storage stats

---

## Storage Status

### PVC Status (10/10 Bound)

| PVC | Namespace | StorageClass | Size | Status |
|-----|-----------|--------------|------|--------|
| postgres-pvc | enclii | longhorn | 10Gi | ✅ Bound |
| redis-pvc | enclii | longhorn | 5Gi | ✅ Bound |
| prometheus-data | monitoring | longhorn | 20Gi | ✅ Bound |
| alertmanager-data | monitoring | longhorn | 2Gi | ✅ Bound |
| grafana-data | monitoring | longhorn | 5Gi | ✅ Bound |
| postgres-data | data | local-path | 20Gi | ✅ Bound |
| redis-data | data | local-path | 5Gi | ✅ Bound |
| arc-docker-cache-blue | arc-runners | local-path | 50Gi | ✅ Bound |
| arc-go-cache | arc-runners | local-path | 20Gi | ✅ Bound |
| arc-npm-cache | arc-runners | local-path | 20Gi | ✅ Bound |

> **Note:** `arc-docker-cache-green` was stuck Pending and deleted in Session 9.

### Longhorn Volumes (100% Healthy)

| Volume | State | Robustness | Size |
|--------|-------|------------|------|
| prometheus-data | attached | ✅ healthy | 20Gi |
| alertmanager-data | attached | ✅ healthy | 2Gi |
| grafana-data | attached | ✅ healthy | 5Gi |
| postgres-pvc | attached | ✅ healthy | 10Gi |
| redis-pvc | attached | ✅ healthy | 5Gi |

---

## ArgoCD GitOps Status

### Infrastructure Apps (13)

| Application | Sync | Health | Notes |
|-------------|------|--------|-------|
| core-services | ✅ Synced | Healthy | |
| ecosystem-services | ✅ Synced | Healthy | Image Updater disabled (multi-source incompatible) |
| enclii-infrastructure | OutOfSync | Healthy | Git-side changes not synced |
| external-secrets | ✅ Synced | Healthy | |
| external-secrets-config | ✅ Synced | Healthy | |
| ingress | ✅ Synced | Healthy | |
| kyverno | ✅ Synced | Healthy | |
| kyverno-policies | ✅ Synced | Healthy | |
| longhorn | ✅ Synced | Healthy | |
| monitoring | ✅ Synced | Healthy | |
| network-policies | ✅ Synced | Healthy | Removed monitoring/dhanam duplicates (owned by their own apps) |
| arc-runners | ⚠️ Unknown | Healthy | ArgoCD v3.2.5 multi-source OCI revision resolution bug (pods functional) |
| arc-runners-blue | ⚠️ Unknown | Healthy | ArgoCD v3.2.5 multi-source OCI revision resolution bug (pods functional) |

### Project Apps (15 — via ApplicationSet `project-appset.yaml`)

| Application | Namespace | Sync | Health | Notes |
|-------------|-----------|------|--------|-------|
| autoswarm-services | autoswarm | ✅ Synced | Healthy | 6 services (session 81) |
| dhanam-services | dhanam | ✅ Synced | Healthy | 3-way merge |
| dispatch-services | enclii | ✅ Synced | Healthy | |
| enclii-services | enclii | OutOfSync | Healthy | Recent git changes |
| forgesight-services | forgesight | ✅ Synced | Healthy | |
| janua-services | janua | ✅ Synced | Healthy | auto-sync enabled |
| karafiel-services | karafiel | ✅ Synced | Degraded | worker CrashLoopBackOff |
| madfam-site-services | madfam-site | ✅ Synced | Healthy | |
| npm-registry-services | npm-registry | ✅ Synced | Healthy | Verdaccio |
| platform-infra-services | data | ✅ Synced | Healthy | Shared postgres + redis |
| pravara-mes-services | pravara-mes | ✅ Synced | Degraded | API crash |
| status-enclii-services | enclii | OutOfSync | Healthy | Recent commits |
| status-madfam-services | enclii | OutOfSync | Healthy | Recent commits |
| tezca-services | tezca | ✅ Synced | Degraded | beat crash |
| yantra4d-services | yantra4d | ✅ Synced | Degraded | admin/studio crash |

**Summary:** 28 apps total. 13 infra + 15 project (via ApplicationSet). ~17 Synced/Healthy, 2 Unknown/Healthy (ARC), 5 OutOfSync, 4 Degraded.

---

## Cloudflare Tunnel Routes

Single unified tunnel. Routes managed remotely via Cloudflare Tunnel Configuration API (not in local YAML). ~54 ingress rules.

> **Note:** `cloudflared-unified.yaml` contains only the local config (metrics, loglevel). Ingress rules are managed via the Cloudflare API by the domain provisioner and manual API calls. The table below documents the current remote config state.

### Enclii Platform Routes

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| api.enclii.dev | switchyard-api.enclii.svc:80 | 200 | /health returns 200 |
| app.enclii.dev | switchyard-ui.enclii.svc:80 | 200 | |
| admin.enclii.dev | dispatch.enclii.svc:80 | 307 | Auth redirect |
| enclii.dev | landing-page.enclii.svc:80 | 200 | |
| www.enclii.dev | landing-page.enclii.svc:80 | 200 | |
| docs.enclii.dev | docs-site.enclii.svc:80 | 200 | |
| status.enclii.dev | status-enclii.enclii.svc:80 | 200 | |
| status.madfam.io | status-madfam.enclii.svc:80 | 200 | |
| vault.madfam.io | vault.vault.svc.cluster.local:8200 | 307 | Backend not deployed |
| analytics.madfam.io | posthog-web.posthog.svc.cluster.local:8000 | 502 | Backend not deployed |

### Monitoring & Infrastructure Routes

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| argocd.enclii.dev | argocd-server.argocd.svc:443 | 404 | noTLSVerify, self-signed |
| grafana.enclii.dev | grafana.monitoring.svc:3000 | 302 | Login redirect |
| prometheus.enclii.dev | prometheus.monitoring.svc:9090 | 302 | |
| alertmanager.enclii.dev | alertmanager.monitoring.svc:9093 | 200 | |
| ssh.madfam.io | ssh://<CONTROL_PLANE_IP>:22 | 302 | Cloudflare Access gate |

### Janua SSO Routes

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| api.janua.dev | janua-api.janua.svc:80 | 200 | Primary auth domain |
| auth.madfam.io | janua-api.janua.svc:80 | 200 | MADFAM alias |
| app.janua.dev | janua-dashboard.janua.svc:80 | 307 | |
| admin.janua.dev | janua-admin.janua.svc:80 | 307 | |
| docs.janua.dev | janua-docs.janua.svc:80 | 200 | |
| janua.dev | janua-website.janua.svc:80 | 200 | |
| www.janua.dev | janua-website.janua.svc:80 | 200 | |

### Dhanam Routes

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| api.dhan.am | dhanam-api.dhanam.svc:80 | 200 | /health returns 200 |
| admin.dhan.am | dhanam-admin.dhanam.svc:80 | 307 | |
| app.dhan.am | dhanam-web.dhanam.svc:80 | 307 | |
| dhan.am | dhanam-web.dhanam.svc:80 | 307 | |
| www.dhan.am | dhanam-web.dhanam.svc:80 | 200 | |

### AutoSwarm Routes (added session 81)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| agents-api.madfam.io | nexus-api.autoswarm.svc:80 | 404 | Service running, no root route |
| agents.madfam.io | office-ui.autoswarm.svc:80 | 307 | Auth redirect |
| agents-admin.madfam.io | admin.autoswarm.svc:80 | 307 | Auth redirect |
| agents-ws.madfam.io | colyseus.autoswarm.svc:80 | 200 | /health returns 200 |
| agents-gw.madfam.io | gateway.autoswarm.svc:80 | 502 | Background worker, no HTTP |

### Tezca Routes (tezca.mx zone)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| tezca.mx | tezca-web.tezca.svc:80 | 200 | |
| api.tezca.mx | tezca-api.tezca.svc:80 | 404 | No health endpoint |
| admin.tezca.mx | tezca-admin.tezca.svc:80 | 307 | |

### Forgesight Routes (forgesight.quest zone)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| forgesight.quest | forgesight-www.forgesight.svc:80 | 200 | |
| www.forgesight.quest | forgesight-www.forgesight.svc:80 | 200 | Alias |
| app.forgesight.quest | forgesight-app.forgesight.svc:80 | 307 | |
| api.forgesight.quest | forgesight-api.forgesight.svc:80 | 200 | |
| admin.forgesight.quest | forgesight-admin.forgesight.svc:80 | 200 | |

### Karafiel Routes (karafiel.mx)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| karafiel.mx | karafiel-web.karafiel.svc:80 | 307 | |
| app.karafiel.mx | karafiel-web.karafiel.svc:80 | 307 | Alias |
| admin.karafiel.mx | karafiel-admin.karafiel.svc:80 | 200 | |
| api.karafiel.mx | karafiel-api.karafiel.svc:80 | 404 | No health endpoint |

### Yantra4D Routes (yantra4d.com)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| yantra4d.com | yantra4d-landing.yantra4d.svc:80 | 200 | |
| api.yantra4d.com | yantra4d-backend.yantra4d.svc:80 | 502 | Backend unreachable |
| app.yantra4d.com | yantra4d-studio.yantra4d.svc:80 | 200 | Studio |
| admin.yantra4d.com | yantra4d-admin.yantra4d.svc:80 | 200 | |

### Pravara MES Routes (mes.madfam.io)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| mes.madfam.io | pravara-ui.pravara-mes.svc:80 | 307 | |
| mes-api.madfam.io | pravara-api.pravara-mes.svc:80 | 502 | Regression |
| mes-admin.madfam.io | pravara-gateway.pravara-mes.svc:80 | 502 | Service never created |

### MADFAM Site Routes (madfam.io)

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| madfam.io | madfam-web.madfam-site.svc:80 | 307 | |
| www.madfam.io | madfam-web.madfam-site.svc:80 | 307 | Alias |
| cms.madfam.io | madfam-cms.madfam-site.svc:80 | 404 | No root endpoint |

### Other Routes

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| npm.madfam.io | <CONTROL_PLANE_IP>:4873 (host Docker) | 200 | Verdaccio |
| *.fn.enclii.dev | keda interceptor.keda.svc:8080 | - | KEDA scale-to-zero |
| (catch-all) | http_status:404 | 404 | Required default |

---

## Docker Containers (Host Level)

| Container | Ports | Status |
|-----------|-------|--------|
| janua-api | 0.0.0.0:4100, 0.0.0.0:8000 | Up |
| postgres-shared | **127.0.0.1:5432** | ✅ Secured |
| redis-shared | **127.0.0.1:6379** | ✅ Secured |
| verdaccio | 0.0.0.0:4873 | Up |
| foundry-registry | 0.0.0.0:5000 | Up |

> **Note:** `janua-proxy` was deleted in Session 10 (0/0 replicas, no longer needed).

---

## Items Requiring Attention

### ✅ RESOLVED: api.dhan.am Latency (Was 2.5s → Now 0.3s)

**Status:** ✅ **FIXED** - Latency now consistently <1s (289-600ms)
**Resolution Date:** 2026-02-03
**Root Cause:** Transient - possibly cold start or connection pool issue

### ✅ RESOLVED: Prometheus CrashLoopBackOff

**Status:** ✅ **FIXED** - Prometheus now stable with 0 restarts
**Resolution Date:** 2026-02-03
**Root Cause:** Stuck rollout causing duplicate pods fighting over PVC lock
**Fix Applied:** `kubectl rollout undo deployment/prometheus -n monitoring --to-revision=12`

### ✅ RESOLVED: Dhanam Redis Authentication

**Status:** ✅ **INFRA FIXED** - Redis now has requirepass enabled
**Resolution Date:** 2026-02-03
**Fix Applied:** Created `infra/k8s/production/data/redis.yaml` with authentication
**Remaining:** App-level investigation needed for ioredis reconnection behavior

### ✅ RESOLVED: kube-system Pods Older Than 30 Days

**Status:** ✅ **REFRESHED**
**Resolution Date:** 2026-02-03
**Fix Applied:** `kubectl rollout restart deployment -n kube-system`
**Result:** All kube-system pods now fresh (<1h old)

### ✅ RESOLVED: Kyverno Policy for Monitoring Images

**Status:** ✅ **FIXED** - PolicyException created for monitoring namespace
**Resolution Date:** 2026-02-03
**Fix Applied:** Created `monitoring-policy-exception.yaml`
**Result:** Prometheus, Grafana, Alertmanager images no longer blocked

### ~~P2: Container Images Using :latest Tag~~ ✅ RESOLVED

**Wave 15 Progress:** Production images pinned where stable:
- 4 enclii deployments: SHA-pinned in base manifests (docs-site, landing-page, switchyard-api, switchyard-ui)
- 5 janua deployments: `:main` mutable tags via kustomization.yaml (Image Updater **disabled** — was causing thrashing, see Session 6)
- 3 dhanam deployments: SHA-pinned via CI `kubectl set image` (dhanam/admin, dhanam/api, dhanam/web)

**Note:** Orphaned GHCR packages `dhanam-api` and `dhanam-web` still exist with restrictive permissions. New images use nested naming (`dhanam/api`, `dhanam/web`) which auto-link to the repo. The orphaned packages should be deleted via GitHub UI when convenient (requires `delete:packages` scope).

---

## Production Services: 95% Complete

### Enclii Core Services

| Service | URL | Status | Replicas | Auto-Deploy |
|---------|-----|--------|----------|-------------|
| switchyard-api | api.enclii.dev | ✅ Running | 5 | ✅ |
| switchyard-ui | app.enclii.dev | ✅ Running | 2 | ✅ |
| dispatch | admin.enclii.dev | ✅ Running | 2 | ✅ |
| docs-site | docs.enclii.dev | ✅ Running | 1 | ✅ |
| landing-page | enclii.dev | ✅ Running | 2 | ✅ |
| status-enclii | status.enclii.dev | ✅ Running | 1 | ✅ |
| status-madfam | status.madfam.io | ✅ Running | 1 | ✅ |
| janua-api | auth.madfam.io | ✅ Running | 2 | ✅ |
| roundhouse | (internal) | ⚠️ CrashLoop | 3 | ✅ |
| waybill | (internal) | ✅ Running | 1 | ✅ |

### AutoSwarm Services (added session 81)

| Service | URL | Status | Replicas | Auto-Deploy |
|---------|-----|--------|----------|-------------|
| nexus-api | agents-api.madfam.io | ✅ Running | 1 | ✅ |
| office-ui | agents.madfam.io | ✅ Running | 1 | ✅ |
| colyseus | agents-ws.madfam.io | ✅ Running | 1 | ✅ |
| gateway | (background) | ✅ Running | 1 | ✅ |
| admin | agents-admin.madfam.io | ✅ Running | 1 | ✅ |
| workers | (background) | ✅ Running | 1 | ✅ |

### Ecosystem Services (managed via ArgoCD project-appset)

| Project | Namespace | Services | Status |
|---------|-----------|----------|--------|
| dhanam | dhanam | api, web, admin | ✅ Healthy |
| tezca | tezca | api, web, admin, es, beat, worker | ⚠️ Degraded (beat crash) |
| karafiel | karafiel | api, web, admin, worker | ⚠️ Degraded (worker crash) |
| yantra4d | yantra4d | backend, landing, studio, admin, redis | ⚠️ Degraded (backend 502) |
| forgesight | forgesight | api, app, www, admin | ✅ Healthy |
| pravara-mes | pravara-mes | api, web, postgres, redis, emqx, centrifugo | ⚠️ Degraded (API 502) |
| madfam-site | madfam-site | site | ✅ Healthy |
| npm-registry | npm-registry | verdaccio | ✅ Healthy |

**Status:** All 15 projects managed via ArgoCD. Core services + AutoSwarm healthy. Ecosystem projects have DNS + service-level issues (see Endpoint Health section).

---

## Long-Term Stability Recommendations

### Immediate (This Week)

| Task | Priority | Effort | Impact |
|------|----------|--------|--------|
| ~~Investigate api.dhan.am latency~~ | ~~P1~~ | ~~2h~~ | ✅ **RESOLVED** |
| ~~Review Prometheus restart cause~~ | ~~P2~~ | ~~1h~~ | ✅ **FIXED** |
| ~~Fix Dhanam Redis auth mismatch~~ | ~~P2~~ | ~~1h~~ | ✅ **INFRA FIXED** |
| ~~Refresh kube-system pods~~ | ~~P3~~ | ~~30min~~ | ✅ **DONE** |
| ~~Fix Kyverno monitoring policy~~ | ~~P2~~ | ~~30min~~ | ✅ **DONE** |
| ~~Configure webhooks for janua/dhanam~~ | ~~P2~~ | ~~2h~~ | ✅ **DONE** (ArgoCD apps created) |

### Short-Term (Next 2 Weeks)

| Task | Priority | Effort | Impact |
|------|----------|--------|--------|
| ~~Pin enclii images to digests (4 deployments)~~ | ~~P2~~ | ~~1h~~ | ✅ **DONE** (Wave 15) |
| ~~Pin remaining images (dhanam + janua)~~ | ~~P2~~ | ~~1h~~ | ✅ **DONE** (all pinned to SHA) |
| ~~Standardize 5 registry secrets to madfam-bot~~ | ~~P2~~ | ~~1h~~ | ✅ **DONE** (0 personal creds) |
| ~~Configure argocd-image-updater for auto-pinning~~ | ~~P2~~ | ~~2h~~ | ✅ **DONE** (janua active) |
| ~~Disable Image Updater for janua (thrashing)~~ | ~~P0~~ | ~~30min~~ | ✅ **DONE** (Session 6) |
| ~~Clean containerd image cache on foundry-core (72% disk)~~ | ~~P1~~ | ~~30min~~ | ✅ **DONE** (Session 9, 72% → 67%) |
| ~~Investigate core-services Progressing status~~ | ~~P1~~ | ~~1h~~ | ✅ **DONE** (now Synced/Healthy) |
| ~~Clean orphaned janua ReplicaSets (59 → ~5-10)~~ | ~~P2~~ | ~~15min~~ | ✅ **DONE** (28 cleaned, Session 9) |
| ~~Decide on `janua-proxy` (scaled to 0/0)~~ | ~~P2~~ | ~~30min~~ | ✅ **DONE** (deleted, Session 10) |
| ~~Disk monitoring alert at 80% in Prometheus~~ | ~~P1~~ | ~~1h~~ | ✅ **DONE** (predictive 7-day + maintenance CronJob failure + Longhorn orphan alerts) |
| Pod Disruption Budgets for switchyard-api, janua-api, cloudflared | P1 | 2h | Zero-downtime maintenance |
| Investigate `status` vs `status-enclii` redundancy | P2 | 1h | Lean operations |
| Investigate `roundhouse` vs `roundhouse-api` split | P2 | 1h | Documentation |
| Fix dhanam-api ioredis config (connects to localhost, not REDIS_URL) | P2 | 2h | Reliability |
| ~~Increase dhanam ResourceQuota~~ | ~~P2~~ | ~~30min~~ | ✅ **DONE** (CPU 4→6, memory 6→8Gi) |
| Add health probes to arc-runners | P2 | 1h | Reliability |
| ~~Configure ArgoCD apps for janua/dhanam~~ | ~~P2~~ | ~~2h~~ | ✅ **DONE** (both auto-sync enabled) |
| ~~Document disaster recovery runbook~~ | ~~P2~~ | ~~4h~~ | ✅ **DONE** (`docs/runbooks/DISASTER_RECOVERY.md`) |
| ~~Run dhanam CI to push fresh `:main` images to GHCR~~ | ~~P2~~ | ~~1h~~ | ✅ **DONE** (nested naming: `dhanam/api`, `dhanam/web`) |
| ~~Create dhanam-admin CI workflow (no workflow exists)~~ | ~~P3~~ | ~~2h~~ | ✅ **DONE** (2026-02-05, `deploy-admin-k8s.yml`) |
| Migrate dhanam CI from `kubectl set image` to GitOps | P3 | 4h | Full GitOps |
| Delete orphaned GHCR packages (`dhanam-api`, `dhanam-web`) | P3 | 15min | Cleanup (requires GitHub UI) |
| Migrate PostgreSQL to Bitnami image | P3 | 4h | Security |

### For Client Onboarding (Next Month)

| Feature | Status | Effort | Blocks |
|---------|--------|--------|--------|
| Tenant provisioning API | ❌ | 2-3 weeks | Signup |
| Registration UI flow | ❌ | 2-3 weeks | Self-service |
| Per-project RBAC | ❌ | 2-3 weeks | Security |
| Audit logging (SOC2) | ❌ | 1-2 weeks | Compliance |
| Billing/quota (Waybill) | ⚠️ Partial | 2-3 weeks | Revenue |
| Custom domain automation | ⚠️ Manual | 1-2 weeks | UX |

### Scaling Readiness Assessment (Session 6)

| Clients | Ready? | Blockers |
|---------|--------|----------|
| 1-5 | ✅ Yes (after P0 fix) | None — infrastructure solid |
| 5-10 | ⚠️ Mostly | Need PDBs, alerting, backup verification |
| 10-25 | ❌ Not yet | Need 3rd node, PostgreSQL HA, Redis Sentinel, tenant isolation |
| 25+ | ❌ Not yet | Multi-region, managed DB evaluation |

### Scaling Thresholds

| Clients | Infrastructure |
|---------|----------------|
| 1-10 | Current 3-node |
| 10-25 | HA + storage expansion |
| 25-50 | Managed DB evaluation |
| 50+ | Multi-region |

> Cost projections: see `internal-devops/infrastructure/cost-analysis.md`

### Infrastructure Already Staged

| Component | Location | Trigger |
|-----------|----------|---------|
| Redis Sentinel HA | `redis-sentinel.yaml` | 3rd node |
| Longhorn multi-replica | Helm values | 3rd node |
| PgBouncer pooling | Staged | High DB load |
| GPU nodes | `gpu/nvidia-device-plugin.yaml` | GPU hardware |

---

## Security Findings

### ✅ RESOLVED: Database Exposure (Fixed 2026-01-17)

```bash
# PostgreSQL now bound to localhost only
LISTEN 127.0.0.1:5432 (docker-proxy)

# Redis now bound to localhost only
LISTEN 127.0.0.1:6379 (docker-proxy)
```

### Environment Variables

| Service | Variable | Value | Status |
|---------|----------|-------|--------|
| janua-api | DATABASE_URL | K8s internal | ✅ |
| janua-api | REDIS_URL | K8s internal | ✅ |
| janua-api | JWT_ALGORITHM | RS256 | ✅ |
| switchyard-api | ENCLII_REDIS_URL | `redis://redis.data.svc.cluster.local:6379` | ✅ |
| dispatch | NEXT_PUBLIC_JANUA_URL | https://auth.madfam.io | ✅ |

---

## Verification Commands

```bash
export KUBECONFIG=~/.kube/config-hetzner

# Quick health check
kubectl get nodes
kubectl get pods -A | grep -v Running | grep -v Completed

# PostgreSQL status
kubectl get pods -n enclii -l app=postgres
kubectl exec -n enclii deploy/postgres -- pg_isready -U postgres

# ArgoCD sync status
kubectl get applications -n argocd

# Endpoint sweep
for d in api.enclii.dev app.enclii.dev docs.enclii.dev status.enclii.dev; do
  echo -n "$d: "; curl -s -o /dev/null -w "%{http_code} %{time_total}s" "https://$d"; echo
done

# Full pod health
kubectl get pods -A --field-selector 'status.phase!=Running,status.phase!=Succeeded'
# Expected: No results (zero failing pods)

# Node status
kubectl get nodes -o wide
# Expected: 3 nodes, all Ready, all v1.33.7+k3s3
```

---

## Stabilization Log

### Wave 15 (2026-02-04) — Full Health + Hardening + ArgoCD Expansion

**Trigger:** Full health audit, infrastructure hardening, ArgoCD expansion to external repos, client onboarding readiness.

**Scope:** 3-node cluster, 15 active namespaces, 9 key endpoints, 14→16 ArgoCD applications.

**Session 1 — Hardening (07:05 UTC):**

1. **Cloudflared Updated** (2025.11.1 → 2026.1.2)
   - Image pinned to SHA digest for immutability
   - File: `infra/k8s/production/cloudflared-unified.yaml`

2. **4 Enclii Images Pinned to SHA Digests**
   - docs-site, landing-page, switchyard-api, switchyard-ui
   - switchyard-api/ui corrected to full GHCR paths (were missing registry prefix)
   - `imagePullPolicy` changed from `Always` to `IfNotPresent` (SHA guarantees immutability)

3. **Secrets Strategy Clarified**
   - Self-hosted HashiCorp Vault chosen as future provider
   - Removed all Doppler references from config comments (redis.yaml, external-secrets README)
   - Documented explicit trigger criteria for Vault deployment

4. **15 Orphaned ReplicaSets Cleaned**

5. **ArgoCD Drift Reviewed** (3 OutOfSync — all cosmetic)

**Session 2 — ArgoCD Expansion + Dhanam Stabilization:**

6. **ArgoCD Apps Created for External Repos** (14→16 apps)
   - `janua-services`: Synced/Healthy, auto-sync with prune+selfHeal
   - `dhanam-services`: OutOfSync/Healthy, visibility-only (auto-sync disabled → enabled in Session 3)
   - Files: `infra/argocd/apps/janua.yaml`, `infra/argocd/apps/dhanam.yaml`

7. **Dhanam ResourceQuota Increased** (rolling update headroom)
   - CPU: 4→6, memory: 6→8Gi
   - File: `dhanam/enclii.yaml` (quota section; previously `infra/k8s/policies/dhanam-quota.yaml`)

8. **Dhanam Container Name Mismatch Fixed**
   - Manifest `web` → `dhanam-web` to match live deployment (strategic merge patch key)
   - Updated 3 CI workflow files in dhanam repo
   - API container ref in CI fixed: `dhanam-api` → `api`

9. **Dhanam Image Pull Stabilized**
   - Expired GHCR tags (e.g. `:89438293`) cause 403 on pull
   - Patched `imagePullPolicy: IfNotPresent` on live deployments (node-cached images)

10. **ARC Runner Probe Fix**
    - Blue runner set missing base values file in Helm chain
    - File: `infra/argocd/apps/arc-runners.yaml`

11. **Golden Config Infrastructure Fixed**
    - `scripts/update-golden.sh` synced from 5→20 entries (matching check script)
    - Added janua.yaml and dhanam.yaml golden configs
    - CI now passing (20/20)

12. **Disaster Recovery Runbook Created**
    - 7 failure scenarios: PostgreSQL, Redis, Longhorn, worker node, control plane, Cloudflare Tunnel, full cluster
    - File: `docs/runbooks/DISASTER_RECOVERY.md`

**Incident: auth.madfam.io 502 (P1, ~2 min)**

- **Cause:** ArgoCD `directory.exclude: '{janua-api.yaml}'` + `prune: true` classified janua-api Deployment as orphan and deleted it
- **Impact:** SSO authentication unavailable (~2 minutes)
- **Resolution:** Immediate `kubectl apply -f janua-api.yaml`, then removed directory.exclude
- **Lesson:** Never combine `directory.exclude` with `prune: true` unless deletion of excluded resources is intended
- **Prevention:** janua.yaml now includes ALL files in source path (no exclude)

**Session 3 — Dhanam CI + Image Pinning + Auto-Sync:**

13. **Registry Secrets Standardized to madfam-bot** (5→0 personal)
    - `enclii/registry-secret`: patched from `github-token` to `madfam-bot`
    - `dhanam/ghcr-credentials`: patched with working madfam-bot PAT
    - `dhanam/enclii-registry-credentials`: recreated as madfam-bot
    - `dhanam/registry-credentials`: recreated as madfam-bot
    - `janua/ghcr-credentials`: patched with working madfam-bot PAT

14. **Dhanam CI Fixed — GHCR Push + Deploy Working**
    - Root cause 1: GHCR `provenance: true` (default) generates attestation manifests → 403 Forbidden on push
    - Root cause 2: Orphaned GHCR packages (`dhanam-api`, `dhanam-web`) with `repo: null` block pushes
    - Fix: `provenance: false` + `sbom: false`, switched `MADFAM_BOT_PAT` → `GITHUB_TOKEN`
    - Renamed images to nested naming (`dhanam/api`, `dhanam/web`) for auto-linking to repo
    - Files: `.github/workflows/deploy-k8s.yml`, `.github/workflows/deploy-web-k8s.yml` (dhanam repo)

15. **Dhanam Web Port Alignment** (3000 → 4200)
    - Service targetPort, containerPort, PORT env, readiness/liveness probes all aligned to 4200
    - Port 4200 matches Enclii port allocation (4200-4299 block)
    - File: `infra/k8s/production/web-deployment.yaml` (dhanam repo)

16. **Dhanam ArgoCD Auto-Sync Enabled**
    - Prerequisites met: fresh GHCR images, corrected manifests
    - `selfHeal: true` + `ServerSideApply=true` sync option
    - `ignoreDifferences` for image (CI manages) and replicas (HPA manages)
    - Resolved SSA merge conflict via `kubectl apply --server-side --force-conflicts`
    - Files: `infra/argocd/apps/dhanam.yaml` (enclii repo)

17. **Janua Images Pinned via Image Updater**
    - Created `kustomization.yaml` with all 5 service images
    - ArgoCD Image Updater auto-committed SHA digests (`0d6805c..1945cbb`)
    - All 5 janua services now tracked with immutable digests
    - File: `infra/k8s/base/janua/kustomization.yaml` (janua repo)

18. **Dhanam PostHogProvider TypeScript Fix**
    - `match[1]` → `match?.[1]` (optional chaining) — unblocked CI build
    - File: `apps/web/src/providers/PostHogProvider.tsx` (dhanam repo)

**Incident: ArgoCD SSA Merge Conflict (dhanam-web, P0 → RESOLVED)**

- **Cause:** Live deployment has `containerPort: 3000`, git manifest has `containerPort: 4200`. Server-Side Apply tries to merge both port entries, creating duplicate "http" named ports.
- **Error:** `spec.template.spec.containers[0].ports[1].name: Duplicate value: "http"` (retried 5x, Failed)
- **Impact:** ArgoCD updated the Service (targetPort→4200) but could NOT update the Deployment (still containerPort 3000). App listens on 3000, Service sends to 4200 → **502 Bad Gateway on `dhan.am` and `app.dhan.am`**.
- **Resolution (Session 4):** Removed `ServerSideApply=true` and `RespectIgnoreDifferences=true` from `infra/argocd/apps/dhanam.yaml`. This switches ArgoCD to traditional 3-way merge, which computes a JSON patch (replacement) instead of SSA field merge. 3-way merge cleanly replaces the port spec without ownership conflicts. Same pattern as `janua.yaml` which syncs without issues.
- **Lesson:** When CI uses `kubectl set image` (creating a competing field manager), SSA merge conflicts are inevitable for any field that diverges between git and live state. Traditional 3-way merge avoids this by not tracking field ownership.

**Remaining Items:**
- ~~**🔴 P0: Fix dhan.am / app.dhan.am 502**~~ — **FIXED** (Session 4)
- ~~**🔴 P0: Janua Image Updater thrashing**~~ — **FIXED** (Session 6): Removed all Image Updater annotations from `janua.yaml`, reverted kustomization to `:main` tags
- ~~Run dhanam CI to push fresh `:main` images~~ — **DONE** (Session 3)
- ~~Standardize 5 registry secrets to madfam-bot~~ — **DONE** (Session 3)
- ~~Pin remaining 6 images (dhanam + janua)~~ — **DONE** (Session 3)
- ~~dhanam-admin CI workflow (no workflow exists)~~ — **DONE** (Session 5)
- ~~Clean containerd image cache on foundry-core (72% disk → target <70%)~~ — ✅ **DONE** (Session 9, 72% → 67%)
- ~~Investigate core-services Progressing status~~ — ✅ **DONE** (now Synced/Healthy)
- ~~Clean orphaned janua ReplicaSets (59 → ~5-10)~~ — ✅ **DONE** (28 cleaned, Session 9)
- Delete orphaned GHCR packages (`dhanam-api`, `dhanam-web`) via GitHub UI — P3
- Migrate dhanam CI from `kubectl set image` to full GitOps (commit image refs) — P3
- Centralized logging (Loki) — Phase 3
- PostgreSQL Bitnami migration — Phase 3

**Post-Session 3 Metrics:**
- Endpoints: 7/9 healthy — dhan.am and app.dhan.am were returning 502 (SSA conflict)
- api.dhan.am: ✅ 200 OK | auth.madfam.io: ✅ 200 | All enclii endpoints: ✅
- Non-running pods: 0 (all pods Running 1/1, issue was port mismatch not pod health)
- ArgoCD: 16 apps (11 Synced/Healthy, 1 Failed, 4 cosmetic drift)
- Registry secrets: 100% madfam-bot (0 personal credentials)
- Image pinning: 11/11 services pinned (4 enclii SHA, 5 janua kustomize, 2 dhanam CI SHA)
- All changes committed and pushed (9 commits across 3 repos + 3 enclii commits)
- Golden configs: 20/20 passing

**Post-Session 4 Fix:**
- SSA sync options removed from `dhanam.yaml` → 3-way merge enabled
- ArgoCD will resync dhanam-services cleanly, resolving the 502 on dhan.am/app.dhan.am
- Expected: 9/9 endpoints healthy after resync

**Session 6 — Production Health Audit (2026-02-05, 22:08 UTC):**

| Item | Before | After | Status |
|------|--------|-------|--------|
| Janua Image Updater | Thrashing (59 orphaned RS, 403s, 10+ versions in 20 min) | Annotations removed, kustomization reverted to `:main` | **FIXED** |
| Janua kustomization | SHA digests (written by Image Updater) | `:main` mutable tags (stable pull) | **REVERTED** |
| Disk usage | 72% (67G/98G) — approaching 80% threshold | Needs containerd image cache prune | **MONITOR** |
| core-services ArgoCD | Synced/Progressing (not Healthy) | Investigation needed | **PENDING** |
| Endpoint count | 9 documented | 17 verified (all <1s) | **UPDATED** |

**Session 5 — Stability Session (2026-02-05):**

19. **PR #36 Merged** (Dependabot: `github/codeql-action` v3 → v4)
    - All 13 CI checks passed, squash-merged

20. **dhanam-admin CI Workflow Created**
    - New file: `.github/workflows/deploy-admin-k8s.yml` (dhanam repo)
    - Follows same kubeconfig+kubectl pattern as `deploy-web-k8s.yml`
    - Graceful preflight, `dhanam/admin` nested GHCR naming
    - Container name `admin` matches deployment spec

21. **dhanam-admin Image Name Fixed**
    - `ghcr.io/madfam-org/dhanam-admin:main` → `ghcr.io/madfam-org/dhanam/admin:main`
    - Aligns with api/web nested GHCR package convention
    - File: `infra/k8s/production/admin-deployment.yaml` (dhanam repo)

22. **ArgoCD dhanam.yaml Updated**
    - Added `dhanam/admin` to GHCR package list in header comments
    - `ignoreDifferences` already covers all Deployments (no `name` filter)
    - Golden config synced

23. **Dhanam Workflows Aligned** (done by dhanam agent earlier in session)
    - All 3 deploy workflows rewritten: SSH → kubeconfig+kubectl
    - Graceful preflight with `::notice::` skip (not hard-fail)
    - 9 unused SSH secrets eliminated

**Client Onboarding Assessment:** Infrastructure ready for 1-5 clients immediately (after Session 6 P0 fix). 5-10 clients need PDBs, alerting, backup verification. Software has 5 blockers (tenant provisioning, registration UI, per-project RBAC, audit logging, billing).

**Session 9 — Production Infrastructure Audit + Remediation (2026-02-06):**

| Item | Before | After | Status |
|------|--------|-------|--------|
| Orphaned ReplicaSets | 28 stale across namespaces | 0 orphaned | **CLEANED** |
| Containerd image cache | 72% disk (67G/98G) | 67% disk (62G/98G) | **PRUNED** |
| janua-admin service (enclii ns) | Stale service in wrong namespace | Deleted | **CLEANED** |
| arc-docker-cache-green PVC | Stuck Pending | Deleted | **CLEANED** |
| StorageClass | Dual default (local-path + longhorn) | Longhorn sole default | **FIXED** |
| argocd-image-updater | OutOfSync | Force-synced → Synced/Healthy | **FIXED** |
| ecosystem-services | Image Updater enabled (multi-source incompatible) | Image Updater disabled | **FIXED** |
| ARC runners | auto-sync enabled (OCI bug causes drift) | auto-sync disabled | **FIXED** |
| janua-docs + janua-website (enclii ns) | Orphaned services | Deleted | **CLEANED** |

**Session 10 — Dhanam API Health Endpoint Unblock (2026-02-06):**

| Item | Before | After | Status |
|------|--------|-------|--------|
| Dhanam deploy workflows | Build gated behind KUBECONFIG_PRODUCTION | Build runs independently, deploy gates separately | **FIXED** |
| api.dhan.am/health | 404 (no /health endpoint image) | 200 (full health JSON) | **FIXED** |
| Dhanam API probes | tcpSocket | httpGet /health | **UPGRADED** |
| janua-proxy deployment | 0/0 replicas (stale) | Deleted + ConfigMap removed | **CLEANED** |
| image-updater ConfigMap | OutOfSync | Confirmed cosmetic (dual-ownership by Helm chart + git app) | **ACCEPTED** |

**Post-Session 10 Metrics (Final — 100% HEALTHY):**
- Pods: 82 running, 0 errors, 6 Completed jobs
- ArgoCD: 14/16 Synced/Healthy, 2 Unknown/Healthy (ARC OCI bug), 1 OutOfSync/Healthy (cosmetic)
- Endpoints: 12/12 public endpoints healthy, all <1s
- Disk: 67% (62G/98G)
- Orphaned resources: 0
- CI: enclii green, dhanam green (lint/test), janua Docker Build green

**Audit Conclusion (through Session 10):** Infrastructure at 100% production health. All P0/P1 issues resolved. 82 pods running, 0 errors. 12/12 public endpoints healthy. Disk cleaned from 72% to 67%. All orphaned resources removed. Dhanam API probes upgraded to httpGet /health.

### Wave 14 (2026-02-03 @ 21:30 UTC)

**Trigger:** Scheduled infrastructure audit with issue resolution.

**Scope:** 3-node cluster, 19 namespaces, 9 key endpoints, 14 ArgoCD applications.

**Issues Identified & Resolved:**

1. **Prometheus CrashLoopBackOff** (11+ restarts)
   - **Root Cause:** Stuck rollout with duplicate pods fighting over PVC lock
   - **Fix:** `kubectl rollout undo deployment/prometheus -n monitoring --to-revision=12`
   - **Result:** Single healthy pod, ArgoCD monitoring status → Healthy

2. **api.dhan.am Latency** (was 2.5s)
   - **Status:** Self-resolved, now 289-600ms
   - **Likely Cause:** Transient cold start / connection pool warmup

3. **Dhanam Redis Connection Errors**
   - **Root Cause:** REDIS_URL secret has password, but Redis has no requirepass
   - **Status:** Non-blocking (app still responds)
   - **Recommendation:** Align Redis auth configuration

**Post-Audit Metrics:**
- Pods: 85 (83 Running, 2 Completed)
- PVCs: 10/11 Bound (1 pending by design)
- Endpoints: 9/9 responding <1s
- ArgoCD: 10/14 Synced, monitoring now Healthy

**Audit Conclusion:** Infrastructure is 99% healthy. All critical issues resolved.

### Wave 13 (2026-02-03 @ 14:09 UTC)

**Trigger:** Comprehensive health check and long-term stability assessment post-Wave 12 fixes.

**Scope:** 3-node cluster, 19 namespaces, 14 production endpoints, 14 ArgoCD applications.

**Key Findings:**
- ✅ PostgreSQL CrashLoopBackOff resolved (Kyverno PolicyException added)
- ✅ 115 stale ReplicaSets cleaned up
- ✅ ArgoCD applications improved (10/14 Synced)
- ⚠️ api.dhan.am latency requires investigation (2.5s) → **RESOLVED in Wave 14**
- ⚠️ Prometheus restarts require monitoring (7x in 15h) → **FIXED in Wave 14**

**Audit Conclusion:** Infrastructure is 98% healthy with excellent endpoint availability (14/14). All critical issues from Wave 12 have been resolved.

### Wave 12 (2026-01-25 / 2026-01-26) — Full Ecosystem Audit

**Trigger:** Post-credential rotation end-to-end verification.

**Critical Fixes Applied:**

1. **dhanam-api CrashLoop** - Switched to TCP probes (commit: `9354dcb`)
2. **Grafana CrashLoopBackOff** - Fixed PVC and ConfigMap (commit: `9354dcb`)
3. **Dispatch Wrong Image Path** - Corrected to `ghcr.io/madfam-org/enclii/admin-console` (commit: `9354dcb`)
4. **VPS Builder Node CNI** - Downgraded k3s to v1.33.7+k3s3 to match control plane
5. **Cloudflared Consolidation** - Single unified config (commit: `4c17f1f`)
6. **Kyverno CronJob Deadlock** - Set cleanup image to `latest` (commits: `39b3a72`, `7e4cbd4`, `9934b94`, `33b71ca`)
7. **Cloudflared Kyverno Compliance** - Added explicit `privileged: false` (commit: `1391e1a`)

### Wave 1 (2026-01-17) — Initial Stabilization

**Critical Fixes Applied:**

1. **Tunnel Consolidation** - Verified systemd tunnels disabled; K8s cloudflared pods handling all traffic
2. **Switchyard API Recovery** - Reset database migration version from 23 to 22
3. **Redis URL Correction** - Changed from external IP to K8s internal DNS
4. **Database Port Security** - Bound to 127.0.0.1 (localhost only)

---

## Appendix: Port Mapping Reference

| Service | Expected Port | Actual Port | Deviation |
|---------|--------------|-------------|-----------|
| janua-api | 4100 | 8080 (K8s) / 4100 (Docker) | Yes |
| switchyard-api | 4200 | 4200 | No |
| switchyard-ui | 3000 | 80 | Normalized |
| dispatch | 4203 | 80 | Normalized |
| postgres | 5432 | 5432 | No |
| redis | 6379 | 6379 | No |
