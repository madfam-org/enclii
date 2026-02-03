# Infrastructure Anatomy - Production State

> **Generated**: 2026-01-17 | **Last Updated**: 2026-02-03 | **Host**: foundry-core + foundry-builder-01 | **Audit Type**: Wave 13 (Comprehensive Health + Stability)
>
> **Live Status Check** (2026-02-03 @ 14:09 UTC):
> - auth.madfam.io OIDC: ✅ 200 OK (836ms)
> - api.enclii.dev/health: ✅ 200 OK (617ms)
> - app.enclii.dev: ✅ 200 OK (756ms)
> - All 14 production endpoints: ✅ 100% availability
> - Pods: 95 Running, 3 Completed, 0 errors

## Executive Summary

| Category | Status | Severity |
|----------|--------|----------|
| **Overall Health** | 98% operational | ✅ HEALTHY |
| **Endpoints** | 14/14 responding | ✅ HEALTHY |
| **Pods** | 98 total (95 Running, 3 Completed) | ✅ HEALTHY |
| **Nodes** | 2/2 Ready, version matched | ✅ HEALTHY |
| **ArgoCD** | 10/14 Synced | 🟡 ACCEPTABLE |
| **Storage** | 100% PVCs bound, Longhorn healthy | ✅ HEALTHY |
| **Cost** | ~$55/month | ✅ ON TARGET |

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
| **foundry-core** | 95.217.198.239 | control-plane, master | Hetzner AX41-NVME (Ryzen 5 3600, 64GB, 2x512GB NVMe) | v1.33.6+k3s1 | 5% | 19% (12.6GB/64GB) | ✅ Ready | 59 days |
| **foundry-builder-01** | 77.42.89.211 | worker (role=builder) | VPS ("The Forge") | v1.33.6+k3s1 | 1% | 31% (1.2GB/4GB) | ✅ Ready | 15 days |

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
│  K8s: cloudflared pods (2 replicas, v2025.11.1)                 │
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

## Namespaces (19 active as of Feb 3, 2026)

| Namespace | Purpose | Pod Count | Status |
|-----------|---------|-----------|--------|
| `longhorn-system` | Block Storage CSI (v1.7.2) | 19 | ✅ Healthy |
| `enclii` | Platform Control Plane | 18 | ✅ Healthy |
| `argocd` | GitOps Engine | 11 | ✅ Healthy |
| `kyverno` | Policy Engine (v1.11.4) | 8 | ✅ Healthy |
| `janua` | Identity Provider | 6 | ✅ Healthy |
| `external-secrets` | Secret Management (ESO v0.9.11) | 5 | ✅ Healthy |
| `dhanam` | Finance Services | 4 | ✅ Healthy |
| `monitoring` | Observability (Prometheus, Grafana) | 3 | ✅ Healthy |
| `kube-system` | K8s system components | 3 | ✅ Healthy |
| `cloudflare-tunnel` | Ingress (2 replicas) | 2 | ✅ Healthy |
| `data` | Shared Databases | 2 | ✅ Healthy |
| `arc-runners` | GitHub Actions Runner Sets | 2 | ✅ Healthy |
| `arc-system` | ARC Controller | 1 | ✅ Healthy |
| `cnpg-system` | CloudNative PG Operator | 1 | ✅ Healthy |
| `enclii-builds` | CI/CD Build Jobs | - | ✅ Healthy |
| `sentinel` | Future Redis Sentinel HA | - | ⏳ Placeholder |
| `default` | Default namespace | - | ✅ Empty |
| `kube-node-lease` | Node heartbeats | - | ✅ System |
| `kube-public` | Public info | - | ✅ System |

---

## Live Endpoint Health (Feb 3, 2026 @ 14:09 UTC)

| Endpoint | Status | Code | Latency | Notes |
|----------|--------|------|---------|-------|
| api.enclii.dev/health | ✅ | 200 | 617ms | Control plane operational |
| app.enclii.dev | ✅ | 200 | 756ms | Dashboard responsive |
| admin.enclii.dev | ✅ | 307 | 835ms | Auth redirect (expected) |
| docs.enclii.dev | ✅ | 200 | 743ms | Documentation operational |
| enclii.dev | ✅ | 200 | 739ms | Landing page |
| status.enclii.dev | ✅ | 200 | 820ms | Status page operational |
| status.madfam.io | ✅ | 200 | 972ms | Madfam status |
| auth.madfam.io (OIDC) | ✅ | 200 | 836ms | JWKS available |
| api.dhan.am/health | ⚠️ | 200 | **2589ms** | Slow - investigate |
| app.dhan.am | ✅ | 307 | 720ms | Auth redirect |
| admin.dhan.am | ✅ | 200 | 768ms | Admin dashboard |
| app.janua.dev | ✅ | 307 | 742ms | Auth redirect |
| admin.janua.dev | ✅ | 307 | 762ms | Auth redirect |
| docs.janua.dev | ✅ | 200 | 743ms | Docs operational |

**Result:** 100% endpoint availability (14/14 responding)

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

### PVC Status (100% Bound)

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
| core-services | ✅ Synced | Progressing | Waiting for pod stabilization |
| ecosystem-services | ✅ Synced | Healthy | |
| enclii-infrastructure | ✅ Synced | Healthy | |
| external-secrets | ✅ Synced | Healthy | |
| external-secrets-config | ✅ Synced | Healthy | |
| image-updater-config | ✅ Synced | Healthy | |
| ingress | ✅ Synced | Healthy | |
| kyverno | ✅ Synced | Healthy | |
| longhorn | ✅ Synced | Healthy | |
| monitoring | ✅ Synced | Healthy | |
| arc-runners | ⚠️ Unknown | Healthy | OCI chart fetch issue |
| arc-runners-blue | ⚠️ Unknown | Healthy | OCI chart fetch issue |
| argocd-image-updater | ⚠️ OutOfSync | Healthy | ConfigMap shared by 2 apps |
| kyverno-policies | ⚠️ OutOfSync | Healthy | SSA metadata drift |

**Summary:** 10/14 Synced, 4 with known non-critical issues

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

### P1: api.dhan.am Latency (2.5s)

**Issue:** Health endpoint responding in 2.5s vs typical <1s
**Impact:** User experience degradation for Dhanam API consumers
**Investigation:**
```bash
kubectl logs -n dhanam -l app=dhanam-api --tail=50
kubectl top pod -n dhanam
```

### P2: Prometheus Restarts (7x in 15h)

**Issue:** Prometheus pod has restarted 7 times
**Impact:** Potential metrics gaps
**Investigation:** Check disk I/O and memory pressure on foundry-core

### P2: Pods Older Than 30 Days

| Pod | Namespace | Age |
|-----|-----------|-----|
| metrics-server | kube-system | 59d |
| local-path-provisioner | kube-system | 59d |
| coredns | kube-system | 59d |

**Recommendation:** Schedule rolling refresh during maintenance window

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

**Remaining Gap:** GitHub webhooks for janua/dhanam repos (currently manual deploy)

---

## Long-Term Stability Recommendations

### Immediate (This Week)

| Task | Priority | Effort | Impact |
|------|----------|--------|--------|
| Investigate api.dhan.am latency | P1 | 2h | Performance |
| Review Prometheus restart cause | P2 | 1h | Observability |
| Configure webhooks for janua/dhanam | P2 | 2h | Automation |

### Short-Term (Next 2 Weeks)

| Task | Priority | Effort | Impact |
|------|----------|--------|--------|
| Pin all images to digests | P2 | 2h | Immutability |
| Add health probes to arc-runners | P2 | 1h | Reliability |
| Schedule kube-system pod refresh | P3 | 1h | Hygiene |
| Document disaster recovery runbook | P2 | 4h | Resilience |

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

### Wave 13 (2026-02-03)

**Trigger:** Comprehensive health check and long-term stability assessment post-Wave 12 fixes.

**Scope:** 2-node cluster, 19 namespaces, 14 production endpoints, 14 ArgoCD applications.

**Key Findings:**
- ✅ PostgreSQL CrashLoopBackOff resolved (Kyverno PolicyException added)
- ✅ 115 stale ReplicaSets cleaned up
- ✅ ArgoCD applications improved (10/14 Synced)
- ⚠️ api.dhan.am latency requires investigation (2.5s)
- ⚠️ Prometheus restarts require monitoring (7x in 15h)

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
