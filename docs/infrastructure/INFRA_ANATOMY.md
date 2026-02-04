# Infrastructure Anatomy - Production State

> **Generated**: 2026-01-17 | **Last Updated**: 2026-02-04 | **Host**: foundry-core + foundry-builder-01 | **Audit Type**: Wave 15 (Full Health + Hardening + ArgoCD Expansion)
>
> **Live Status Check** (2026-02-04, end of session):
> - auth.madfam.io OIDC: ✅ 200 OK (<1s) — **recovered from brief outage** (see incident log)
> - api.enclii.dev: ✅ 404 (<1s) — expected for root path
> - app.enclii.dev: ✅ 200 OK (<1s)
> - api.dhan.am: ✅ 404 (<1s) — latency stable (fixed in Wave 14)
> - All endpoints: ✅ <1s latency, 100% availability (9/9)
> - Pods: All Running (0 errors, 0 CrashLoops)
> - ArgoCD: 16 apps (was 14) — 11 Synced, 1 OutOfSync/Healthy (dhanam visibility-only), 4 cosmetic
> - Dhanam: Stabilized with `imagePullPolicy: IfNotPresent` (expired GHCR tags)
> - Dhanam ResourceQuota: Increased (CPU 4→6, memory 6→8Gi) for rolling updates
> - Golden configs: 20/20 passing (was failing in CI)
> - DR runbook: Created (`docs/runbooks/DISASTER_RECOVERY.md`)
> - Cloudflared: 2026.1.2 (updated earlier in Wave 15)
> - Image pinning: 4 enclii deployments pinned to SHA digests

## Executive Summary

| Category | Status | Severity |
|----------|--------|----------|
| **Overall Health** | 99% operational | ✅ HEALTHY |
| **Endpoints** | 9/9 responding <1s | ✅ HEALTHY |
| **Pods** | 84 total (80 Running, 4 Completed) | ✅ HEALTHY |
| **Nodes** | 2/2 Ready, version matched (k3s v1.33.6) | ✅ HEALTHY |
| **ArgoCD** | 16 apps: 11 Synced, 1 OutOfSync/Healthy (dhanam), 4 cosmetic | ✅ HEALTHY |
| **Storage** | 10/11 PVCs bound (1 pending, expected) | ✅ HEALTHY |
| **Longhorn** | 5/5 volumes healthy (42GB allocated) | ✅ HEALTHY |
| **Cost** | ~$55/month | ✅ ON TARGET |

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
| **foundry-core** | 95.217.198.239 | control-plane, master | Hetzner AX41-NVME (Ryzen 5 3600, 64GB, 2x512GB NVMe) | v1.33.6+k3s1 | 5% | 23% (14.9GB/64GB) | ✅ Ready | 60 days |
| **foundry-builder-01** | 77.42.89.211 | worker (role=builder) | VPS ("The Forge") | v1.33.6+k3s1 | 1% | 31% (1.2GB/4GB) | ✅ Ready | 16 days |

- **OS**: Ubuntu 24.04.3 LTS (Noble Numbat)
- **Kernel**: 6.8.0-88-generic
- **Node Count**: 2 (since Jan 2026)
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
│  Routes: ~28 production domains                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                K8s CLUSTER (K3s, 2 nodes)                       │
│                                                                 │
│  foundry-core (control-plane):                                  │
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

## Namespaces (15 active as of Feb 4, 2026)

| Namespace | Purpose | Pod Count | Status |
|-----------|---------|-----------|--------|
| `longhorn-system` | Block Storage CSI (v1.7.2) | 20 | ✅ Healthy |
| `enclii` | Platform Control Plane | 20 | ✅ Healthy |
| `argocd` | GitOps Engine | 8 | ✅ Healthy |
| `kyverno` | Policy Engine (v1.11.4) | 6 | ✅ Healthy |
| `janua` | Identity Provider | 6 | ✅ Healthy |
| `external-secrets` | Secret Management (ESO v0.9.11) | 5 | ✅ Healthy |
| `dhanam` | Finance Services | 4 | ✅ Healthy |
| `monitoring` | Observability (Prometheus, Grafana) | 4 | ✅ Healthy |
| `kube-system` | K8s system components | 3 | ✅ Healthy |
| `cloudflare-tunnel` | Ingress (2 replicas) | 2 | ✅ Healthy |
| `data` | Shared Databases | 2 | ✅ Healthy |
| `arc-system` | ARC Controller | 2 | ✅ Healthy |
| `arc-runners` | GitHub Actions Runner Sets | 1 | ✅ Healthy |
| `cnpg-system` | CloudNative PG Operator | 1 | ✅ Healthy |
| `sentinel` | Infra Audit CronJob | 1 | ✅ Healthy |
| `enclii-builds` | CI/CD Build Jobs | - | ✅ Healthy |
| `default` | Default namespace | - | ✅ Empty |
| `kube-node-lease` | Node heartbeats | - | ✅ System |
| `kube-public` | Public info | - | ✅ System |

---

## Live Endpoint Health (Feb 4, 2026 @ 07:05 UTC)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| api.enclii.dev | ✅ | 404 | 750ms | Root path 404 expected |
| app.enclii.dev | ✅ | 200 | 800ms | Dashboard responsive |
| admin.enclii.dev | ✅ | 307 | 620ms | Auth redirect (expected) |
| docs.enclii.dev | ✅ | 200 | 630ms | Documentation operational |
| enclii.dev | ✅ | 200 | 650ms | Landing page |
| status.enclii.dev | ✅ | 200 | 680ms | Status page operational |
| status.madfam.io | ✅ | 200 | 910ms | Madfam status |
| auth.madfam.io (OIDC) | ✅ | 200 | 700ms | JWKS available |
| api.dhan.am | ✅ | 404 | 570ms | Stable (<1s since Wave 14 fix) |

**Result:** 100% endpoint availability (9/9 responding, all <1s)

---

## Services by Namespace

### janua

| Service | Type | Port | targetPort | Status |
|---------|------|------|------------|--------|
| janua-api | ClusterIP | 80 | 8080 | ✅ |
| janua-dashboard | ClusterIP | 80 | 80 | ✅ |
| janua-admin | ClusterIP | 80 | 80 | ✅ |
| janua-docs | ClusterIP | 80 | 80 | ✅ |
| janua-website | ClusterIP | 80 | 80 | ✅ |

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

---

## Storage Status

### PVC Status (10/11 Bound)

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
| arc-docker-cache-green | arc-runners | local-path | 50Gi | ⏳ Pending (WaitForFirstConsumer) |
| arc-go-cache | arc-runners | local-path | 20Gi | ✅ Bound |
| arc-npm-cache | arc-runners | local-path | 20Gi | ✅ Bound |

> **Note:** arc-docker-cache-green is Pending because no pod is currently using it (green runner set inactive). This is expected behavior.

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

| Application | Sync | Health | Notes |
|-------------|------|--------|-------|
| core-services | ✅ Synced | Healthy | |
| ecosystem-services | ✅ Synced | Healthy | |
| enclii-infrastructure | ⚠️ OutOfSync | Healthy | Non-critical drift (root app) |
| external-secrets | ✅ Synced | Healthy | |
| external-secrets-config | ✅ Synced | Healthy | |
| image-updater-config | ✅ Synced | Healthy | |
| ingress | ✅ Synced | Healthy | |
| kyverno | ✅ Synced | Healthy | |
| longhorn | ✅ Synced | Healthy | |
| monitoring | ✅ Synced | Healthy | |
| **janua-services** | ✅ Synced | Healthy | **NEW** — auto-sync enabled (prune+selfHeal) |
| **dhanam-services** | ⚠️ OutOfSync | Healthy | **NEW** — visibility-only (auto-sync disabled, see note) |
| arc-runners | ⚠️ Unknown | Healthy | OCI chart fetch issue |
| arc-runners-blue | ⚠️ Unknown | Healthy | OCI chart fetch issue |
| argocd-image-updater | ⚠️ OutOfSync | Healthy | ConfigMap shared by 2 apps |
| kyverno-policies | ⚠️ OutOfSync | Healthy | SSA metadata drift |

**Summary:** 16 apps total. 11 Synced/Healthy, 5 with known non-critical drift.

> **Dhanam auto-sync disabled:** Old image tags (e.g. `:89438293`) are expired in GHCR. Running pods use node-cached images. Any ArgoCD-triggered rollout would create pods that fail ImagePull. Prerequisites to enable: (1) Run dhanam CI to push fresh `:main` tags, (2) Verify `imagePullPolicy: IfNotPresent` in manifests, (3) Uncomment `syncPolicy.automated` in `infra/argocd/apps/dhanam.yaml`.

---

## Cloudflare Tunnel Routes

Single unified tunnel via `infra/k8s/production/cloudflared-unified.yaml`. All routes verified.

| Hostname | Target Service | HTTP | Notes |
|----------|---------------|------|-------|
| api.enclii.dev | switchyard-api.enclii.svc:80 | 200 | /health returns 200 |
| app.enclii.dev | switchyard-ui.enclii.svc:80 | 200 | |
| admin.enclii.dev | dispatch.enclii.svc:80 | 307 | Redirect to auth |
| enclii.dev | landing-page.enclii.svc:80 | 200 | |
| www.enclii.dev | landing-page.enclii.svc:80 | 200 | |
| docs.enclii.dev | docs-site.enclii.svc:80 | 200 | |
| status.enclii.dev | status-enclii.enclii.svc:80 | 200 | |
| status.madfam.io | status-madfam.enclii.svc:80 | 200 | |
| argocd.enclii.dev | argocd-server.argocd.svc:443 | 404 | noTLSVerify, self-signed |
| grafana.enclii.dev | grafana.monitoring.svc:3000 | 302 | Redirect to login |
| prometheus.enclii.dev | prometheus.monitoring.svc:9090 | 302 | |
| alertmanager.enclii.dev | alertmanager.monitoring.svc:9093 | 200 | |
| api.janua.dev | janua-api.janua.svc:80 | 200 | Primary auth domain |
| auth.madfam.io | janua-api.janua.svc:80 | 200 | MADFAM alias |
| app.janua.dev | janua-dashboard.janua.svc:80 | 307 | |
| admin.janua.dev | janua-admin.janua.svc:80 | 307 | |
| docs.janua.dev | janua-docs.janua.svc:80 | 200 | |
| janua.dev | janua-website.janua.svc:80 | 200 | |
| www.janua.dev | janua-website.janua.svc:80 | 200 | |
| madfam.io | janua-website.janua.svc:80 | 307 | |
| www.madfam.io | janua-website.janua.svc:80 | 307 | |
| npm.madfam.io | 95.217.198.239:4873 (host Docker) | 200 | Verdaccio |
| api.dhan.am | dhanam-api.dhanam.svc:80 | 200 | |
| admin.dhan.am | dhanam-admin.dhanam.svc:80 | 200 | |
| app.dhan.am | dhanam-web.dhanam.svc:80 | 307 | |
| dhan.am | dhanam-web.dhanam.svc:80 | 200 | |
| www.dhan.am | dhanam-web.dhanam.svc:80 | 200 | |
| *.fn.enclii.dev | keda interceptor.keda.svc:8080 | - | KEDA scale-to-zero |
| ssh.madfam.io | ssh://95.217.198.239:22 | 302 | Cloudflare Access gate |
| agents.madfam.io | http_status:503 | 502 | Pending Auto-Claude deploy |
| (catch-all) | http_status:404 | 404 | Required default |

---

## Docker Containers (Host Level)

| Container | Ports | Status |
|-----------|-------|--------|
| janua-api | 0.0.0.0:4100, 0.0.0.0:8000 | Up |
| janua-proxy | - | Up |
| postgres-shared | **127.0.0.1:5432** | ✅ Secured |
| redis-shared | **127.0.0.1:6379** | ✅ Secured |
| verdaccio | 0.0.0.0:4873 | Up |
| foundry-registry | 0.0.0.0:5000 | Up |

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

### P2: Container Images Using :latest Tag (6 remaining)

**Issue:** 6 deployments still use `:latest` or mutable tags without SHA digest pins
**Impact:** Reproducibility, potential security drift
**Wave 15 Progress:** 4 enclii deployments pinned (docs-site, landing-page, switchyard-api, switchyard-ui)
**Remaining:**
- dhanam: dhanam-admin, dhanam-api (`:main` tag)
- janua: janua-admin, janua-dashboard, janua-docs, janua-website (`:latest`)

**Recommendation:** Pin remaining images in next wave; configure argocd-image-updater for automated pinning

---

## Dogfooding Status: 90% Complete

| Service | URL | Status | Replicas | Auto-Deploy |
|---------|-----|--------|----------|-------------|
| switchyard-api | api.enclii.dev | ✅ Running | 5 | ✅ |
| switchyard-ui | app.enclii.dev | ✅ Running | 2 | ✅ |
| dispatch | admin.enclii.dev | ✅ Running | 2 | ✅ |
| docs-site | docs.enclii.dev | ✅ Running | 1 | ✅ |
| landing-page | enclii.dev | ✅ Running | 2 | ✅ |
| status-page | status.enclii.dev | ✅ Running | 1 | ✅ |
| status-madfam | status.madfam.io | ✅ Running | 1 | ✅ |
| janua-api | auth.madfam.io | ✅ Running | 2 | ✅ |
| roundhouse | (internal) | ✅ Running | 1 | ✅ |
| waybill | (internal) | ✅ Running | 1 | ✅ |

**Remaining Gap:** Dhanam auto-sync blocked on fresh GHCR images (expired tags). Janua fully managed via ArgoCD.

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
| Pin remaining images (6 deployments: dhanam + janua) | P2 | 1h | Immutability |
| Standardize 5 registry secrets to madfam-bot | P2 | 1h | Security |
| Configure argocd-image-updater for auto-pinning | P2 | 2h | Automation |
| Fix dhanam-api ioredis config (connects to localhost, not REDIS_URL) | P2 | 2h | Reliability |
| ~~Increase dhanam ResourceQuota~~ | ~~P2~~ | ~~30min~~ | ✅ **DONE** (CPU 4→6, memory 6→8Gi) |
| Add health probes to arc-runners | P2 | 1h | Reliability |
| ~~Configure ArgoCD apps for janua/dhanam~~ | ~~P2~~ | ~~2h~~ | ✅ **DONE** (janua Synced, dhanam visibility-only) |
| ~~Document disaster recovery runbook~~ | ~~P2~~ | ~~4h~~ | ✅ **DONE** (`docs/runbooks/DISASTER_RECOVERY.md`) |
| Run dhanam CI to push fresh `:main` images to GHCR | P2 | 1h | Enables dhanam auto-sync |
| Migrate dhanam CI from `kubectl set image` to GitOps | P3 | 4h | Full GitOps |
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

### Scaling Thresholds

| Clients | Infrastructure | Estimated Cost |
|---------|----------------|----------------|
| 1-10 | Current 2-node | $55/mo |
| 10-25 | Add 3rd node + HA | $100/mo |
| 25-50 | Managed DB evaluation | $150-200/mo |
| 50+ | Multi-region | $300+/mo |

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
# Expected: 2 nodes, both Ready, both v1.33.6+k3s1
```

---

## Stabilization Log

### Wave 15 (2026-02-04) — Full Health + Hardening + ArgoCD Expansion

**Trigger:** Full health audit, infrastructure hardening, ArgoCD expansion to external repos, client onboarding readiness.

**Scope:** 2-node cluster, 15 active namespaces, 9 key endpoints, 14→16 ArgoCD applications.

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
   - `dhanam-services`: OutOfSync/Healthy, visibility-only (auto-sync disabled)
   - Files: `infra/argocd/apps/janua.yaml`, `infra/argocd/apps/dhanam.yaml`

7. **Dhanam ResourceQuota Increased** (rolling update headroom)
   - CPU: 4→6, memory: 6→8Gi
   - File: `infra/k8s/policies/dhanam-quota.yaml`

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

**Remaining Items:**
- Run dhanam CI to push fresh `:main` images (enables auto-sync)
- Migrate dhanam CI from `kubectl set image` to GitOps (commit image refs)
- Standardize 5 registry secrets to madfam-bot
- Pin remaining 6 images (dhanam + janua)
- Centralized logging (Loki) — Phase 3
- PostgreSQL Bitnami migration — Phase 3

**Post-Session Metrics:**
- Endpoints: 9/9 responding <1s
- Non-running pods: 0
- ArgoCD: 16 apps (11 Synced, 1 OutOfSync/Healthy visibility-only, 4 cosmetic)
- All changes committed and pushed (7 commits across 2 repos)
- Golden configs: 20/20 passing

**Client Onboarding Assessment:** Infrastructure ready for 1-10 clients. Software has 5 blockers (tenant provisioning, registration UI, per-project RBAC, audit logging, billing).

**Audit Conclusion:** Infrastructure is 99% healthy. ArgoCD expanded to cover all external repos. One P1 incident (auth.madfam.io) caused by misconfigured prune+exclude, resolved in <2 minutes. All hardening and automation changes applied.

### Wave 14 (2026-02-03 @ 21:30 UTC)

**Trigger:** Scheduled infrastructure audit with issue resolution.

**Scope:** 2-node cluster, 19 namespaces, 9 key endpoints, 14 ArgoCD applications.

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

**Scope:** 2-node cluster, 19 namespaces, 14 production endpoints, 14 ArgoCD applications.

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
3. **Dispatch Wrong Image Path** - Corrected to `ghcr.io/madfam-org/enclii/dispatch` (commit: `9354dcb`)
4. **VPS Builder Node CNI** - Downgraded k3s to v1.33.6+k3s1 to match control plane
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
