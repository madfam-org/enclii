# Post-Incident Reviews

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


This document captures significant production incidents and the lessons learned during the January 2026 stabilization period.

---

## PIR-001: Triple Tunnel Conflict

**Date:** January 12-14, 2026
**Severity:** P1 - Production Outage
**Duration:** ~6 hours (intermittent)
**Status:** Resolved

### Summary
Three separate cloudflared tunnel instances were discovered running simultaneously, causing routing conflicts and intermittent 502/504 errors across all public endpoints.

### Root Cause
During iterative debugging of connectivity issues, multiple tunnel deployments were created:
1. Original `cloudflared.yaml` in `cloudflare-tunnel` namespace
2. Duplicate deployment in `enclii` namespace
3. Third instance from kustomization overlay

### Impact
- 502/504 errors on api.enclii.dev, app.enclii.dev, admin.enclii.dev
- Intermittent authentication failures
- Build webhook delivery failures

### Resolution
1. Identified all running cloudflared pods: `kubectl get pods -A | grep cloudflared`
2. Removed duplicate deployments (kept only `cloudflare-tunnel` namespace)
3. Added comment to `infra/k8s/production/kustomization.yaml` documenting single-source-of-truth
4. Verified unified routing via `infra/k8s/production/cloudflared-unified.yaml`

### Lessons Learned
- Single namespace for tunnel infrastructure
- Document removal decisions in kustomization.yaml comments
- Add monitoring for duplicate deployments

### Follow-up Actions
- [x] Remove duplicate cloudflared references
- [x] Update documentation with tunnel architecture
- [ ] Add alerting for multiple tunnel pods

---

## PIR-002: k3s Version Mismatch

**Date:** January 15, 2026
**Severity:** P2 - Degraded Performance
**Duration:** ~2 hours
**Status:** Resolved

### Summary
The foundry-builder-01 worker node was running a different k3s version than foundry-core control plane, causing intermittent scheduling failures and pod evictions.

### Root Cause
Manual node addition without version pinning. Control plane was upgraded but worker node retained older version.

### Impact
- Build jobs failed to schedule on worker node
- ARC runner pods entered CrashLoopBackOff
- Increased load on control plane node

### Resolution
1. Identified version mismatch: `kubectl get nodes -o wide`
2. Upgraded worker node to match control plane: k3s v1.33.6+k3s1
3. Cordoned, drained, and rejoined worker node

### Lessons Learned
- Pin k3s version in deployment scripts
- Add version check to cluster health monitoring
- Document upgrade procedure requiring both nodes

### Follow-up Actions
- [x] Match k3s versions across all nodes
- [x] Document version requirements in CLAUDE.md
- [ ] Add version mismatch alerting

---

## PIR-003: Bitnami Image Tag Deprecation

**Date:** January 18, 2026
**Severity:** P3 - Warning
**Duration:** Ongoing (mitigated)
**Status:** Mitigated

### Summary
Several Bitnami images using `latest` or deprecated tags began failing ImagePull with registry authentication errors and missing manifest warnings.

### Root Cause
Bitnami deprecated certain image tags without prior warning. The `latest` tag policy changed, breaking immutable deployments.

### Impact
- Redis pods failed to start after node restart
- PostgreSQL backup job failures
- Extended pod restart times

### Resolution
1. Identified affected images: `kubectl get pods -A -o jsonpath='{.items[*].spec.containers[*].image}' | tr ' ' '\n' | grep bitnami`
2. Pinned all Bitnami images to specific version tags
3. Updated registry pull policy to `IfNotPresent` for version-tagged images

### Lessons Learned
- Never use `latest` tag in production
- Pin to specific semantic versions
- Monitor upstream deprecation announcements

### Follow-up Actions
- [x] Pin all image tags to specific versions
- [x] Update deployment manifests
- [ ] Implement image tag validation in CI

---

## PIR-004: Disk Pressure Cleanup

**Date:** January 20, 2026
**Severity:** P2 - Degraded Performance
**Duration:** ~1 hour
**Status:** Resolved

### Summary
Foundry-core node entered DiskPressure condition, triggering pod evictions and build failures.

### Root Cause
Accumulated container images, build cache, and logs consumed >85% of root partition. Longhorn volumes also contained orphaned data.

### Impact
- Pod evictions for non-critical workloads
- Build pipeline failures
- Degraded API response times

### Resolution
1. Identified disk usage: `df -h` and `crictl images | wc -l`
2. Pruned unused container images: `crictl rmi --prune`
3. Cleaned old build jobs: `kubectl delete jobs -n enclii-builds --field-selector status.successful=1`
4. Removed orphaned Longhorn volumes
5. Implemented log rotation for system journals

### Lessons Learned
- Implement proactive disk monitoring with alerts at 70%
- Schedule regular cleanup jobs
- Set TTL on completed Jobs

### Follow-up Actions
- [x] Clean up disk usage
- [x] Add disk pressure monitoring
- [ ] Implement CronJob for image pruning
- [ ] Configure Job TTL in base manifests

---

## Incident Response Checklist

### Immediate Actions
1. Assess impact scope: `kubectl get pods -A | grep -v Running`
2. Check recent changes: `kubectl get events -A --sort-by='.lastTimestamp' | tail -50`
3. Verify node health: `kubectl get nodes` and `kubectl describe node <name>`
4. Check resource pressure: `kubectl top nodes` and `kubectl top pods -A`

### Communication
1. Update status.enclii.dev if customer-facing
2. Notify team via Slack #enclii-incidents
3. Create PIR document within 48 hours

### Post-Incident
1. Document timeline and resolution
2. Identify root cause and contributing factors
3. Define follow-up actions with owners
4. Schedule blameless retrospective if warranted

---

## Metrics Dashboard Links

- ArgoCD: `kubectl port-forward svc/argocd-server -n argocd 8080:443`
- Longhorn: `kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80`
- Grafana: (pending deployment)

---

*Last Updated: February 2, 2026*
