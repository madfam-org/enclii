# Disaster Recovery Runbook

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Cluster:** 3-node k3s (foundry-cp [control-plane] + foundry-worker-01 [worker] + foundry-builder-01 [builder])
**RPO:** 24 hours (daily PostgreSQL backup to R2)
**RTO:** 2 hours (manual rebuild)
**Last Updated:** Feb 23, 2026 (Production Audit — Session 37)
**Last Tested:** _Update after each drill_

---

## Quick Reference

| Scenario | RPO | RTO | Runbook Section |
|----------|-----|-----|-----------------|
| Database corruption | 24h | 30min | §1 PostgreSQL Restore |
| Single service failure | 0 | 5min | §4 Partial Recovery |
| Single node failure | 0 | 15min | §4 Node Drain |
| Full cluster loss | 24h | 2h | §2 Full Rebuild |
| Cloudflare tunnel loss | 0 | 5min | §3 Tunnel Reconnect |
| Prometheus restarts | 0 | 10min | §5 Monitoring Recovery |
| API latency spike | 0 | 15min | §6 Performance Issues |

---

## 1. PostgreSQL Restore from R2

**When:** Database corruption, accidental data deletion, failed migration.

### Prerequisites
- `kubectl` access to cluster
- R2 credentials in `r2-backup-credentials` secret (data namespace)

### Steps

```bash
# 1. List available backups
./scripts/backup-database.sh list

# 2. Scale down API to prevent writes
kubectl scale deploy switchyard-api -n enclii --replicas=0

# 3. Restore from latest backup (interactive confirmation required)
./scripts/backup-database.sh restore postgres/latest.sql.gz

# Alternatively, restore a specific dated backup:
# ./scripts/backup-database.sh restore postgres/enclii_dev_20260201_040000.sql.gz

# 4. Verify restore
kubectl exec -n enclii deploy/postgres -- psql -U postgres -d enclii_dev \
  -c "SELECT count(*) FROM projects;"

# 5. Scale API back up
kubectl scale deploy switchyard-api -n enclii --replicas=1

# 6. Verify API health
curl -s https://api.enclii.dev/health | jq .
```

### If backup CronJob hasn't run recently
```bash
# Trigger manual backup before any destructive operation
./scripts/backup-database.sh backup
```

---

## 2. Full Cluster Rebuild

**When:** Total node failure, catastrophic infrastructure loss.

### Steps

```bash
# 1. Provision infrastructure
cd infra/terraform
terraform init
terraform plan -out=tfplan
terraform apply tfplan

# 2. Get kubeconfig
./scripts/deploy-production.sh kubeconfig

# 3. Verify node connectivity
kubectl get nodes

# 4. Bootstrap ArgoCD (installs all apps via App-of-Apps)
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v2.14.3/manifests/install.yaml
kubectl apply -f infra/argocd/root-application.yaml

# 5. Wait for ArgoCD to sync all applications
kubectl get applications -n argocd -w

# 6. Restore secrets (from Doppler or manual backup)
# ESO (External Secrets Operator) will sync from Doppler automatically.
# If Doppler is unavailable, restore from encrypted backup:
kubectl apply -f <sealed-secrets-backup>

# 7. Re-establish Cloudflare tunnel
# cloudflared pods auto-connect using tunnel token in secret.
# Verify tunnel status in Cloudflare Zero Trust dashboard.
kubectl get pods -n cloudflare-tunnel

# 8. Restore database from R2
./scripts/backup-database.sh restore

# 9. Verify all services
kubectl get pods -A | grep -v Running | grep -v Completed
curl -s https://api.enclii.dev/health
curl -s https://app.enclii.dev
curl -s https://status.enclii.dev
```

---

## 3. Cloudflare Tunnel Reconnect

**When:** Tunnel pods evicted, node restart, network blip.

```bash
# 1. Check tunnel pod status
kubectl get pods -n cloudflare-tunnel

# 2. If pods are CrashLooping, check logs
kubectl logs -n cloudflare-tunnel -l app=cloudflared --tail=50

# 3. Restart tunnel pods (zero-downtime with 2 replicas)
kubectl rollout restart deploy/cloudflared -n cloudflare-tunnel
kubectl rollout status deploy/cloudflared -n cloudflare-tunnel

# 4. Verify tunnel in Cloudflare dashboard
# Dashboard → Zero Trust → Access → Tunnels → enclii-production

# 5. Test connectivity
curl -s https://api.enclii.dev/health
curl -s https://app.enclii.dev
```

### If tunnel token is lost
```bash
# Re-create from Cloudflare dashboard:
# 1. Go to Zero Trust → Tunnels → enclii-production → Configure
# 2. Copy new tunnel token
# 3. Update secret:
kubectl create secret generic tunnel-credentials -n cloudflare-tunnel \
  --from-literal=token=<NEW_TOKEN> \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart deploy/cloudflared -n cloudflare-tunnel
```

---

## 4. Partial Failure Recovery

### Single Service Restart
```bash
# Restart a specific deployment
kubectl rollout restart deploy/<service-name> -n enclii
kubectl rollout status deploy/<service-name> -n enclii

# Check logs if not starting
kubectl logs -n enclii -l app=<service-name> --tail=50
kubectl describe pod -n enclii -l app=<service-name>
```

### Single Node Drain
```bash
# 1. Cordon node (prevent new scheduling)
kubectl cordon <node-name>

# 2. Drain workloads (respects PDBs)
kubectl drain <node-name> --ignore-daemonsets --delete-emptydir-data

# 3. Perform maintenance on node

# 4. Uncordon when ready
kubectl uncordon <node-name>

# 5. Verify pods redistributed
kubectl get pods -A -o wide
```

### PostgreSQL Pod Restart (crash loop)
```bash
# 1. Check postgres logs
kubectl logs -n enclii deploy/postgres --tail=100

# 2. If WAL replay is stuck, the startupProbe (failureThreshold: 30 × 5s = 150s)
#    gives postgres up to 2.5 minutes to recover before liveness kills it.

# 3. If still failing, check PVC:
kubectl get pvc postgres-pvc -n enclii
kubectl describe pvc postgres-pvc -n enclii

# 4. Check Kyverno PolicyException (added Wave 13)
kubectl get policyexception -n enclii postgres-security-exception

# 5. Nuclear option: delete pod (PVC data persists)
kubectl delete pod -n enclii -l app=postgres
```

---

## 5. Monitoring Recovery

### Prometheus Restarts (Wave 13 Issue)

**Symptoms:** Prometheus pod restarting frequently (7+ times in 15h observed)

**Root Causes:**
1. Memory pressure (fixed: increased to 3Gi limit)
2. Disk I/O contention on Longhorn volume
3. Large scrape configuration causing slow startup

```bash
# 1. Check current restart count
kubectl get pods -n monitoring -l app=prometheus

# 2. Check logs for OOM or other issues
kubectl logs -n monitoring -l app=prometheus --previous --tail=100

# 3. Check resource usage
kubectl top pod -n monitoring

# 4. Verify PVC is healthy
kubectl get pvc prometheus-data -n monitoring
kubectl exec -n longhorn-system deploy/longhorn-manager -- \
  longhorn-manager volume get prometheus-data

# 5. If restarts continue, increase memory limit
kubectl patch deployment prometheus -n monitoring --type='json' \
  -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/resources/limits/memory", "value": "4Gi"}]'

# 6. Rolling restart
kubectl rollout restart deploy/prometheus -n monitoring
kubectl rollout status deploy/prometheus -n monitoring
```

### Grafana Recovery
```bash
# 1. Check Grafana status
kubectl get pods -n monitoring -l app.kubernetes.io/name=grafana

# 2. Check PVC binding
kubectl get pvc grafana-data -n monitoring

# 3. Restart if needed
kubectl rollout restart deploy/grafana -n monitoring
```

---

## 6. Performance Issues

### API Latency Spike (api.dhan.am Issue - Wave 13)

**Symptoms:** Health endpoint responding in 2.5s+ vs typical <1s

**Investigation:**
```bash
# 1. Check pod resource usage
kubectl top pod -n dhanam -l app=dhanam-api

# 2. Check logs for slow queries or errors
kubectl logs -n dhanam -l app=dhanam-api --tail=100

# 3. Check HPA status (may be at max replicas)
kubectl get hpa -n dhanam

# 4. Check database connection pool
kubectl exec -n dhanam deploy/dhanam-api -- env | grep DATABASE

# 5. Verify Redis connectivity
kubectl exec -n dhanam deploy/dhanam-api -- nc -zv redis.data.svc.cluster.local 6379

# 6. Scale up if needed
kubectl scale deploy dhanam-api -n dhanam --replicas=4
```

**Resolution:**
- Increased resource limits (Wave 13: 768Mi memory, 1000m CPU)
- Added startup probe to prevent cold start latency
- Consider implementing /health endpoint for better diagnostics

### Switchyard API Latency
```bash
# 1. Check current performance
curl -w "@curl-format.txt" -o /dev/null -s https://api.enclii.dev/health

# 2. Check pod metrics
kubectl top pod -n enclii -l app=switchyard-api

# 3. Check database query performance
kubectl exec -n enclii deploy/postgres -- psql -U postgres -d enclii_dev \
  -c "SELECT query, calls, total_exec_time, mean_exec_time FROM pg_stat_statements ORDER BY total_exec_time DESC LIMIT 10;"
```

---

## 7. ArgoCD Issues

### Application Out of Sync

**Known Non-Critical Drift (Wave 13):**
- `arc-runners` / `arc-runners-blue`: OCI chart fetch limitation
- `argocd-image-updater`: ConfigMap shared by 2 apps
- `kyverno-policies`: SSA metadata drift

```bash
# 1. Check sync status
kubectl get applications -n argocd

# 2. View specific application
kubectl describe application <app-name> -n argocd

# 3. Force sync (if safe)
kubectl patch application <app-name> -n argocd --type merge \
  -p '{"operation":{"sync":{}}}'

# 4. Hard refresh (re-read from Git)
kubectl patch application <app-name> -n argocd --type merge \
  -p '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}'
```

### ArgoCD Controller Down
```bash
# 1. Check controller status
kubectl get pods -n argocd -l app.kubernetes.io/name=argocd-application-controller

# 2. Restart controller
kubectl rollout restart statefulset/argocd-application-controller -n argocd

# 3. Check Redis (ArgoCD dependency)
kubectl get pods -n argocd -l app.kubernetes.io/name=argocd-redis
```

---

## 8. Kube-System Maintenance

### Monthly Pod Refresh (Wave 13 Recommendation)

Long-running kube-system pods (59+ days) should be refreshed monthly.

```bash
# Manual refresh (or use CronJob at infra/k8s/production/maintenance/)
kubectl rollout restart deployment/coredns -n kube-system
kubectl rollout status deployment/coredns -n kube-system

kubectl rollout restart deployment/metrics-server -n kube-system
kubectl rollout status deployment/metrics-server -n kube-system

kubectl rollout restart deployment/local-path-provisioner -n kube-system
kubectl rollout status deployment/local-path-provisioner -n kube-system
```

### Deploy Automated Refresh CronJob
```bash
kubectl apply -f infra/k8s/production/maintenance/kube-system-refresh-cronjob.yaml
```

---

## 9. RPO/RTO Summary

| Scenario | RPO | RTO | Notes |
|----------|-----|-----|-------|
| Database corruption | 24h | 30min | Restore from R2 daily backup |
| Single service failure | 0 | 5min | Pod restart, no data loss |
| Single node failure | 0 | 15min | Pods reschedule to other node |
| Full cluster loss | 24h | 2h | Terraform + ArgoCD + DB restore |
| Cloudflare tunnel loss | 0 | 5min | Pod restart, auto-reconnect |
| Prometheus failure | 0 | 10min | Rolling restart, metrics gap |
| API latency spike | 0 | 15min | Scale up, investigate root cause |

---

## 10. Health Check Commands

### Quick Cluster Health
```bash
export KUBECONFIG=~/.kube/config-hetzner

# Nodes
kubectl get nodes

# All unhealthy pods
kubectl get pods -A | grep -v Running | grep -v Completed

# ArgoCD sync status
kubectl get applications -n argocd

# Endpoint sweep
for d in api.enclii.dev app.enclii.dev docs.enclii.dev status.enclii.dev auth.madfam.io; do
  echo -n "$d: "; curl -s -o /dev/null -w "%{http_code} %{time_total}s" "https://$d"; echo
done
```

### Full Audit
```bash
# Run comprehensive audit
./scripts/audit-infrastructure.sh

# Or manual checks:
kubectl get nodes -o wide
kubectl get pods -A --field-selector 'status.phase!=Running,status.phase!=Succeeded'
kubectl top nodes
kubectl top pods -A --sort-by=memory | head -20
```

---

## 11. Contacts & Escalation

| Role | Contact | When |
|------|---------|------|
| Platform Lead | Check internal directory | All P0 incidents |
| Hetzner Support | https://robot.hetzner.com | Hardware failures |
| Cloudflare Status | https://cloudflarestatus.com | Tunnel/CDN issues |

---

## 12. Post-Incident

After every incident:
1. Update this runbook with lessons learned
2. Run `./scripts/backup-database.sh backup` to create fresh restore point
3. Verify all monitoring alerts are firing: `kubectl get prometheusrules -A`
4. Document timeline in `docs/production/incidents/`
5. Update INFRA_ANATOMY.md with any configuration changes

---

## Appendix: Wave 13 Fixes Applied

| Issue | Fix | Location |
|-------|-----|----------|
| PostgreSQL CrashLoopBackOff | Kyverno PolicyException | `infra/k8s/base/external-secrets/postgres-security-exception.yaml` |
| Stale ReplicaSets (115) | Cleanup script | Manual deletion |
| Prometheus restarts | Memory 2Gi→3Gi, CPU 500m→1000m | `infra/k8s/production/monitoring/prometheus.yaml` |
| api.dhan.am latency | Startup probe, memory 512Mi→768Mi | `dhanam/infra/k8s/production/api-deployment.yaml` |
| ARC runner reliability | Health probes added | `infra/helm/arc/values-runner-set.yaml` |
| kube-system pod age | Monthly refresh CronJob | `infra/k8s/production/maintenance/kube-system-refresh-cronjob.yaml` |
