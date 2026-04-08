# Capacity Roadmap — Production Cluster

> **Created**: 2026-03-13 | **Audit Baseline**: ~150 pods, 22 namespaces, 46 endpoints (37 operational)
> **Cluster**: 3-node k3s v1.33.7+k3s3 (foundry-cp [EX44, control-plane] + foundry-worker-01 [AX41, worker] + foundry-builder-01 [builder])
> **Last Updated**: 2026-04-08 — Control-plane migrated to foundry-cp (EX44, i5-13500, 128GB), foundry-core renamed to foundry-worker-01

## Current Utilization Summary

### Compute

| Resource | Allocatable | Requested | Actual | Utilization (req) | Utilization (actual) |
|----------|-------------|-----------|--------|--------------------|-----------------------|
| **CPU (foundry-worker-01)** | 12,000m | 10,460m | 1,340m | **87%** | 11% |
| **CPU (builder-01)** | 2,000m | ~0m | 31m | ~0% | 1% |
| **Memory (foundry-worker-01)** | 64Gi | 24.9Gi | 21.5Gi | 39% | 33% |
| **Memory (builder-01)** | 3.8Gi | ~0m | 1.1Gi | ~0% | 28% |

**Key Insight**: CPU is over-committed at request level (87%) but actual usage is only 11%. The bottleneck is Kubernetes scheduling — pods won't schedule if requests exceed allocatable. The biggest offender is Longhorn `instance-manager` at 1440m requested vs 84m actual.

### Storage

| Volume | Capacity | Used | Usage % | Namespace |
|--------|----------|------|---------|-----------|
| data/postgres | 10Gi | 232Mi | 3% | data |
| prometheus | 20Gi | 4.3Gi | 22% | monitoring |
| tezca-es | 50Gi | 1.0Gi | 2% | tezca |
| pravara-mes/postgres | 20Gi | 46Mi | 0% | pravara-mes |
| Longhorn replicas total | ~150Gi | 58Gi | ~39% | longhorn-system |

**Disk (root filesystem, foundry-worker-01)**: 77G/98G (83%) — **P1 alert**. 399 container images + 58G Longhorn + 5.5G logs.

### Growth Trends

| Metric | Feb 6, 2026 | Mar 13, 2026 | Delta | Rate |
|--------|-------------|--------------|-------|------|
| Pods | 82 | 150 | +68 (+83%) | +1.9/day |
| Disk | 67% (62G) | 83% (77G) | +16% (+15G) | +0.43G/day |
| ArgoCD apps | 19 | 28 | +9 (+47%) | — |
| Longhorn volumes | 5 | 17 | +12 | — |
| Namespaces | 14 | 22 | +8 | — |
| Endpoints | 12 | 46 | +34 | — |

**At current disk growth rate (0.43G/day), 95% disk usage reached in ~28 days (April 10, 2026).**

## Immediate Actions (This Week)

### 1. Prune Container Images — Saves ~10-20G

```bash
# On foundry-worker-01 (or foundry-cp):
sudo k3s crictl rmi --prune
# Verify:
sudo k3s crictl images | wc -l
df -h /
```

399 images on disk. Pruning unused images should recover 10-20G, buying ~3-6 weeks.

### 2. Apply Longhorn CPU Fix — Saves 1,080m CPU

Session 79 reduced `guaranteedEngineManagerCPU/ReplicaManagerCPU` from 12→3 in Helm values, but the settings were never applied to the cluster. The instance-manager still requests 1440m (12% of total cluster CPU) while using only 84m.

```bash
# Via Longhorn UI or API:
kubectl -n longhorn-system edit settings.longhorn.io guaranteed-instance-manager-cpu
# Set value to: 3
# Note: Setting name may vary by Longhorn version. Check:
kubectl get settings.longhorn.io -n longhorn-system
```

After applying: CPU allocation drops from 87% → ~78%.

### 3. Delete Detached Longhorn Volumes — Saves ~44G

**Status:** PostHog namespace deleted (S110). Volumes may have been auto-deleted with namespace.
Verify remaining detached volumes and delete if still present:

```bash
# Check for any remaining detached volumes:
kubectl get volumes.longhorn.io -n longhorn-system --no-headers | grep -i detach
# Delete detached volumes via Longhorn UI or CLI
```

### 4. Log Rotation — Saves ~3-4G

```bash
sudo journalctl --vacuum-size=500M
sudo find /var/log -name "*.gz" -mtime +7 -delete
```

## Capacity Thresholds

| Threshold | Trigger | Action |
|-----------|---------|--------|
| Disk > 80% | **NOW (83%)** | Prune images, clean volumes, rotate logs |
| Disk > 90% | ~April 10 at current rate | Emergency: add storage node |
| CPU req > 85% | **NOW (87%)** | Apply Longhorn fix, review HPA mins |
| CPU req > 95% | +2-3 more services | Must add compute node |
| Memory req > 70% | ~40Gi | Review and compact |
| Pods > 200 | +50 pods | Check max-pods setting |

## Hardware Recommendation

> **Detailed pricing, server specs, and cost projections**: see `madfam-org/internal-devops` → `infrastructure/cost-analysis.md` and `hardware/hetzner-evaluation.md`

The next node should be ordered from Hetzner (same DC as foundry-cp for latency). Key considerations:
- Match or exceed current control plane CPU/RAM specs
- NVMe storage preferred for Longhorn replication
- Same k3s version (v1.33.7+k3s3)

## k3s Node Addition Checklist

### Pre-Deploy

```bash
# 1. Order server (see internal-devops/hardware/ for recommendations)
# 2. Initial server setup
hostnamectl set-hostname foundry-node-02
apt update && apt upgrade -y
```

### Join Cluster

```bash
# Get join token from control plane:
ssh foundry-cp 'sudo cat /var/lib/rancher/k3s/server/node-token'

# Install k3s agent (MUST match version v1.33.7+k3s3):
curl -sfL https://get.k3s.io | \
  K3S_URL=https://37.27.235.104:6443 \
  K3S_TOKEN=<token> \
  INSTALL_K3S_VERSION="v1.33.7+k3s3" sh -

# Label (no taint — general workloads):
kubectl label node foundry-node-02 node-role.kubernetes.io/worker=true
```

### Post-Join

1. Verify: `kubectl get nodes -o wide` — 3 nodes Ready
2. Install Longhorn on new node (auto via DaemonSet)
3. Update Cloudflare tunnel to include new node (if needed)
4. Rebalance workloads: `kubectl rollout restart deploy -n <ns>` for large deployments

## Longhorn Multi-Replica Migration

When the 3rd node (2nd storage node) joins:

### Step 1: Update Default Replica Count

```bash
# In infra/helm/longhorn/values.yaml:
# Change: defaultReplicaCount: 1 → 2
helm upgrade longhorn longhorn/longhorn -n longhorn-system -f values.yaml
```

### Step 2: Patch Existing Volumes

```bash
for vol in $(kubectl get volumes.longhorn.io -n longhorn-system -o name); do
  kubectl patch $vol -n longhorn-system --type merge \
    -p '{"spec":{"numberOfReplicas":2}}'
done
```

### Step 3: Monitor Replication

```bash
# Watch replication progress (~30-60 min for 58GB):
kubectl get volumes.longhorn.io -n longhorn-system -w
# All volumes should show ROBUSTNESS=healthy with 2 replicas
```

### Also Staged for Multi-Node

- **Redis Sentinel**: `infra/k8s/production/redis-sentinel.yaml` — activate for HA
- **Anti-affinity**: Existing `preferredDuringScheduling` rules will auto-spread replicas

## Scaling Decision Matrix

| Clients | Pods (est.) | Nodes | Storage |
|---------|-------------|-------|---------|
| 1-100 (current — alpha) | 150-400 | **3 (current)** | 200GB-1TB Longhorn |
| 100-500 (beta) | 400-800 | 3 | 1TB Longhorn |
| 500-2,000 (early revenue) | 800-1,500 | 3-4 | 1-2TB |
| 2,000-10,000 (growth) | 1,500-3,000 | 4-6 | 2-5TB |
| 10,000+ (scale) | 3,000+ | 6+ | 5TB+ |

> Cost projections: see `internal-devops/infrastructure/cost-analysis.md`

## Non-Running Pod Remediation

| Pod | Issue | Root Cause | Fix |
|-----|-------|-----------|-----|
| roundhouse (3) | CrashLoopBackOff | Redis DNS `i/o timeout` | Check NetworkPolicy egress to CoreDNS |
| karafiel-worker (1) | CrashLoopBackOff | Can't resolve `redis.data.svc.cluster.local` | External repo — DNS/NetworkPolicy issue |
| tezca-beat (1) | CrashLoopBackOff | Celery beat crash | External repo — app-level issue |
| yantra4d-admin/studio (2) | CrashLoopBackOff | nginx `host not found in upstream "backend"` | Stale deployments from rollout — delete old ReplicaSets |
| ~~posthog (5)~~ | ~~Init:0/N stuck~~ | ~~ClickHouse not ready~~ | **Removed S110** — namespace + ArgoCD app deleted |
| sentinel (1) | Error | CronJob failure | Investigate sentinel namespace config |

## ArgoCD OutOfSync Apps

| App | Status | Likely Cause |
|-----|--------|-------------|
| enclii-infrastructure | OutOfSync/Healthy | Git-side changes not synced (new resources) |
| network-policies | OutOfSync/Healthy | Manual kubectl changes diverged from git |
| platform-infra-services | OutOfSync/Healthy | Infrastructure changes pending sync |
| status-enclii/madfam | OutOfSync/Healthy | Recent commits not auto-synced |
| vault | OutOfSync/Healthy | Vault deployed manually, ArgoCD app defined but not synced |
| ~~posthog~~ | ~~OutOfSync/Degraded~~ | **Removed S110** — ArgoCD app deleted |
