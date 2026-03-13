# Disaster Recovery Runbook

**Last Updated:** 2026-02-04 (Wave 15 audit)
**Owner:** Platform Team
**RPO (Recovery Point Objective):** 24 hours (daily PostgreSQL backups to R2)
**RTO (Recovery Target):** 2-4 hours (full cluster rebuild + restore)

---

## Quick Reference

| Scenario | Severity | RPO | Estimated RTO | Runbook Section |
|----------|----------|-----|---------------|-----------------|
| Single pod crash | Low | 0 | Automatic (<2 min) | Not covered (K8s self-heals) |
| PostgreSQL failure | High | 24h | 30 min | [Section 1](#1-postgresql-recovery) |
| Redis failure | Medium | 0 (cache) | 5 min | [Section 2](#2-redis-recovery) |
| Longhorn volume failure | High | Varies | 30-60 min | [Section 3](#3-longhorn-volume-recovery) |
| Worker node failure | Medium | 0 | 15 min | [Section 4](#4-node-failure-recovery) |
| Control plane failure | Critical | 24h | 2-4 hours | [Section 5](#5-control-plane-failure) |
| Cloudflare Tunnel down | High | 0 | 5-15 min | [Section 6](#6-cloudflare-tunnel-recovery) |
| Full cluster rebuild | Critical | 24h | 2-4 hours | [Section 7](#7-full-cluster-rebuild) |

---

## Prerequisites

```bash
# All commands assume this KUBECONFIG
export KUBECONFIG=~/.kube/config-hetzner

# Verify cluster access
kubectl get nodes
```

Required access:
- SSH to foundry-core (95.217.198.239)
- SSH to foundry-builder-01 (77.42.89.211)
- Cloudflare R2 credentials (for backup retrieval)
- Hetzner Cloud API token (for node provisioning)
- GitHub madfam-bot PAT (for image pulls)

---

## 1. PostgreSQL Recovery

See [DATABASE_RECOVERY.md](./DATABASE_RECOVERY.md) for detailed procedures.

### Quick Recovery

```bash
# Check PostgreSQL status
kubectl get pods -n enclii -l app=postgres
kubectl exec -n enclii deploy/postgres -- pg_isready -U postgres

# If pod is CrashLooping, check logs
kubectl logs -n enclii -l app=postgres --tail=50

# Common fix: Delete pod (K8s recreates it)
kubectl delete pod -n enclii -l app=postgres

# If PVC is corrupt, restore from backup
./scripts/backup-database.sh restore
```

### Data Namespace PostgreSQL

```bash
# Check data namespace PostgreSQL
kubectl get pods -n data -l app=postgres
kubectl exec -n data deploy/postgres -- pg_isready -U postgres

# Dhanam uses this instance
# Backup: manual (see dhanam repo docs)
```

---

## 2. Redis Recovery

Redis is used for caching and session storage. Data loss is acceptable (cache rebuilds on next request).

### Enclii Redis (namespace: enclii)

```bash
# Check status
kubectl get pods -n enclii -l app=redis
kubectl exec -n enclii deploy/redis -- redis-cli -a "$REDIS_PASSWORD" PING

# If unhealthy, delete pod (fresh start)
kubectl delete pod -n enclii -l app=redis

# Verify reconnection
kubectl logs -n enclii -l app=switchyard-api --tail=20 | grep -i redis
```

### Data Namespace Redis (namespace: data)

```bash
# Check status
kubectl get pods -n data -l app=redis
kubectl exec -n data deploy/redis -- redis-cli -a "$REDIS_PASSWORD" PING

# Password is in secret
kubectl get secret redis-auth -n data -o jsonpath='{.data.redis-password}' | base64 -d

# If unhealthy, delete pod
kubectl delete pod -n data -l app=redis

# Redis persists to PVC (AOF + snapshots)
# On restart, it replays from /data/appendonly.aof
```

### Redis Data Recovery (if PVC corrupt)

```bash
# Delete PVC and let Redis start fresh (cache data is ephemeral)
kubectl delete pvc redis-data -n data
kubectl delete pod -n data -l app=redis
# K8s will recreate PVC from StorageClass and start fresh Redis

# For enclii namespace
kubectl delete pvc redis-pvc -n enclii
kubectl delete pod -n enclii -l app=redis
```

---

## 3. Longhorn Volume Recovery

Longhorn manages 5 volumes (42GB total): prometheus-data, alertmanager-data, grafana-data, postgres-pvc, redis-pvc.

### Check Volume Health

```bash
# List all Longhorn volumes
kubectl get volumes.longhorn.io -n longhorn-system

# Check volume details
kubectl get volumes.longhorn.io -n longhorn-system -o custom-columns="NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness,SIZE:.spec.size"

# Access Longhorn UI
kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80
# Open http://localhost:8081
```

### Degraded Volume (replica failure)

```bash
# Check replica status
kubectl get replicas.longhorn.io -n longhorn-system

# Longhorn auto-rebuilds replicas on single-node setup
# If stuck, delete the failed replica
kubectl delete replica.longhorn.io <replica-name> -n longhorn-system

# Force volume rebuild
kubectl get volumes.longhorn.io <volume-name> -n longhorn-system -o json | \
  python3 -c "import json,sys; v=json.load(sys.stdin); print(v['status']['state'])"
```

### Volume Stuck Attaching

```bash
# If a volume is stuck in "Attaching" state after node restart
# Detach and reattach
kubectl patch volumes.longhorn.io <volume-name> -n longhorn-system \
  --type merge -p '{"spec":{"nodeID":""}}'

# Wait for detach, then it will auto-attach to the requesting pod
kubectl get volumes.longhorn.io <volume-name> -n longhorn-system -w
```

### Complete Volume Loss

```bash
# If Longhorn volume is unrecoverable:
# 1. Delete the PVC
kubectl delete pvc <pvc-name> -n <namespace>

# 2. Delete the Longhorn volume
kubectl delete volumes.longhorn.io <volume-name> -n longhorn-system

# 3. Recreate the workload (PVC will be recreated from manifest)
kubectl delete pod -l app=<app-name> -n <namespace>

# 4. For PostgreSQL: restore from backup
./scripts/backup-database.sh restore

# 5. For Prometheus/Grafana: data will rebuild from scrape targets
# Historical metrics will be lost but new data starts immediately
```

---

## 4. Node Failure Recovery

### Worker Node (foundry-builder-01) Failure

Impact: CI/CD builds stop. No production service impact.

```bash
# Check node status
kubectl get nodes
kubectl describe node foundry-builder-01

# If NotReady, SSH and check
ssh root@77.42.89.211
systemctl status k3s-agent
journalctl -u k3s-agent --since "1 hour ago"

# Restart k3s agent
systemctl restart k3s-agent

# If node is unrecoverable, drain and remove
kubectl drain foundry-builder-01 --ignore-daemonsets --delete-emptydir-data
kubectl delete node foundry-builder-01

# Provision replacement (see Hetzner VPS setup)
# Rejoin cluster:
curl -sfL https://get.k3s.io | K3S_URL=https://95.217.198.239:6443 \
  K3S_TOKEN=<node-token> INSTALL_K3S_VERSION=v1.33.7+k3s3 sh -

# Re-apply builder taint
kubectl taint nodes <new-node> builder=true:NoSchedule
kubectl label nodes <new-node> role=builder
```

### Control Plane (foundry-core) Failure

Impact: All services down. Critical recovery scenario.

```bash
# If SSH still works:
ssh root@95.217.198.239
systemctl status k3s
journalctl -u k3s --since "1 hour ago"

# Restart k3s
systemctl restart k3s

# If k3s won't start, check disk
df -h
# If disk full, clean up
crictl rmi --prune
journalctl --vacuum-size=500M

# If hardware failure, see Section 7 (Full Cluster Rebuild)
```

---

## 5. Control Plane Failure

If foundry-core is unrecoverable (hardware failure, data center issue):

1. **Contact Hetzner support** for hardware replacement
2. **Provision replacement server** via Hetzner Robot
3. **Follow Section 7** (Full Cluster Rebuild)
4. **Restore PostgreSQL** from R2 backups
5. **Rejoin builder node** to new control plane

---

## 6. Cloudflare Tunnel Recovery

All external traffic routes through Cloudflare Tunnel. If tunnel is down, all public endpoints are unreachable.

```bash
# Check cloudflared pods
kubectl get pods -n cloudflare-tunnel
kubectl logs -n cloudflare-tunnel -l app=cloudflared --tail=50

# Restart cloudflared
kubectl rollout restart deployment/cloudflared -n cloudflare-tunnel
kubectl rollout status deployment/cloudflared -n cloudflare-tunnel

# If tunnel token is expired/invalid
# 1. Get new token from Cloudflare Zero Trust dashboard
#    → Networks → Tunnels → Configure → Token
# 2. Update secret
kubectl delete secret cloudflared-enclii-token -n cloudflare-tunnel
kubectl create secret generic cloudflared-enclii-token \
  -n cloudflare-tunnel --from-literal=token=<new-token>

# 3. Restart pods
kubectl rollout restart deployment/cloudflared -n cloudflare-tunnel

# Verify
for d in api.enclii.dev app.enclii.dev auth.madfam.io; do
  echo -n "$d: "; curl -s -o /dev/null -w "%{http_code} %{time_total}s" "https://$d"; echo
done
```

---

## 7. Full Cluster Rebuild

Worst case: control plane is gone, need to rebuild from scratch.

### Step 1: Provision New Server

```bash
# Via Hetzner Robot or API
# Minimum: AX41-NVME (Ryzen 5 3600, 64GB RAM, 2x512GB NVMe)
# OS: Ubuntu 24.04 LTS
```

### Step 2: Install k3s

```bash
ssh root@<new-ip>

# Install k3s (match existing version)
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION=v1.33.7+k3s3 \
  INSTALL_K3S_EXEC="server --disable traefik --disable servicelb" sh -

# Get kubeconfig
cat /etc/rancher/k3s/k3s.yaml
# Update server address to new IP and save locally
```

### Step 3: Deploy Core Infrastructure

```bash
# Clone enclii repo
git clone https://github.com/madfam-org/enclii.git
cd enclii

# Install Longhorn
helm repo add longhorn https://charts.longhorn.io
helm install longhorn longhorn/longhorn -n longhorn-system --create-namespace \
  -f infra/helm/longhorn/values.yaml

# Install Kyverno
helm repo add kyverno https://kyverno.github.io/kyverno/
helm install kyverno kyverno/kyverno -n kyverno --create-namespace

# Install ArgoCD
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v2.14.3/manifests/install.yaml

# Wait for ArgoCD
kubectl wait --for=condition=available deploy/argocd-server -n argocd --timeout=300s
```

### Step 4: Configure Secrets

```bash
# Create registry credentials
kubectl create namespace enclii
kubectl create secret docker-registry ghcr-credentials -n enclii \
  --docker-server=ghcr.io \
  --docker-username=madfam-bot \
  --docker-password=<madfam-bot-PAT>

# Repeat for other namespaces: janua, dhanam, argocd, enclii-builds

# Create Cloudflare Tunnel secret
kubectl create namespace cloudflare-tunnel
kubectl create secret generic cloudflared-enclii-token \
  -n cloudflare-tunnel --from-literal=token=<tunnel-token>

# Create PostgreSQL credentials, Redis auth, JWT secrets
# (These are in the platform team's password manager)
```

### Step 5: Deploy via ArgoCD

```bash
# Apply root application (app-of-apps)
kubectl apply -f infra/argocd/root-application.yaml

# ArgoCD will sync all 14+ applications automatically
# Monitor progress
kubectl get applications -n argocd -w
```

### Step 6: Restore Data

```bash
# Restore PostgreSQL from R2
./scripts/backup-database.sh restore

# Redis will start fresh (cache only)
# Prometheus will start scraping (historical data lost)
# Grafana dashboards are in git (ArgoCD deploys them)
```

### Step 7: Rejoin Builder Node

```bash
# Get node token from new control plane
cat /var/lib/rancher/k3s/server/node-token

# On builder node
curl -sfL https://get.k3s.io | K3S_URL=https://<new-ip>:6443 \
  K3S_TOKEN=<token> INSTALL_K3S_VERSION=v1.33.7+k3s3 sh -

# Taint and label
kubectl taint nodes foundry-builder-01 builder=true:NoSchedule
kubectl label nodes foundry-builder-01 role=builder
```

### Step 8: Verify Recovery

```bash
# Run full health check
kubectl get nodes
kubectl get pods -A | grep -v Running | grep -v Completed
kubectl get applications -n argocd

# Endpoint sweep
for d in api.enclii.dev app.enclii.dev docs.enclii.dev status.enclii.dev \
         auth.madfam.io admin.enclii.dev enclii.dev api.dhan.am status.madfam.io; do
  echo -n "$d: "; curl -s -o /dev/null -w "%{http_code} %{time_total}s" "https://$d"; echo
done
```

---

## Post-Recovery Checklist

After any recovery scenario:

- [ ] All nodes Ready (`kubectl get nodes`)
- [ ] Zero non-Running pods (`kubectl get pods -A | grep -v Running | grep -v Completed`)
- [ ] All endpoints responding (`9/9 < 1s`)
- [ ] ArgoCD applications synced (`kubectl get applications -n argocd`)
- [ ] PostgreSQL accessible (`kubectl exec -n enclii deploy/postgres -- pg_isready`)
- [ ] Redis responding (`kubectl exec -n data deploy/redis -- redis-cli PING`)
- [ ] Longhorn volumes healthy (`kubectl get volumes.longhorn.io -n longhorn-system`)
- [ ] Authentication working (`curl https://auth.madfam.io/.well-known/jwks.json`)
- [ ] Backup CronJob scheduled (`kubectl get cronjobs -A`)
- [ ] Monitoring collecting data (`curl https://prometheus.enclii.dev/api/v1/status/runtimeinfo`)

---

## Preventive Measures

### Daily (Automated)
- PostgreSQL backup to R2 (CronJob at 2AM UTC)
- ArgoCD drift detection and self-heal

### Weekly (Manual)
- Review backup job logs: `kubectl logs job/postgres-backup -n enclii`
- Check disk usage: `ssh root@95.217.198.239 df -h`
- Review Longhorn volume health

### Monthly
- Test restore procedure from R2 backup
- Verify builder node is healthy
- Review and update this runbook

### Quarterly
- Full disaster recovery drill (restore to test environment)
- Rotate madfam-bot PAT
- Review Cloudflare Tunnel configuration
- Capacity planning review

---

## Related Documentation

- [Database Recovery](./DATABASE_RECOVERY.md) - Detailed PostgreSQL procedures
- [Infrastructure Anatomy](../infrastructure/INFRA_ANATOMY.md) - Current cluster state
- [Secrets Management](../infrastructure/SECRETS_MANAGEMENT.md) - Secret locations and strategy
- [GitOps with ArgoCD](../infrastructure/GITOPS.md) - ArgoCD configuration
- [Storage (Longhorn)](../infrastructure/STORAGE.md) - Volume management
