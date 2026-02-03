# Disaster Recovery Runbook

**Cluster:** 2-node k3s (foundry-core + foundry-builder-01)
**RPO:** 24 hours (daily PostgreSQL backup to R2)
**RTO:** 2 hours (manual rebuild)
**Last Tested:** _Update after each drill_

---

## 1. PostgreSQL Restore from R2

**When:** Database corruption, accidental data deletion, failed migration.

### Prerequisites
- `kubectl` access to cluster
- R2 credentials in `r2-backup-credentials` secret

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
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
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

# 4. Nuclear option: delete pod (PVC data persists)
kubectl delete pod -n enclii -l app=postgres
```

---

## 5. RPO/RTO Summary

| Scenario | RPO | RTO | Notes |
|----------|-----|-----|-------|
| Database corruption | 24h | 30min | Restore from R2 daily backup |
| Single service failure | 0 | 5min | Pod restart, no data loss |
| Single node failure | 0 | 15min | Pods reschedule to other node |
| Full cluster loss | 24h | 2h | Terraform + ArgoCD + DB restore |
| Cloudflare tunnel loss | 0 | 5min | Pod restart, auto-reconnect |

---

## 6. Contacts & Escalation

| Role | Contact | When |
|------|---------|------|
| Platform Lead | Check internal directory | All P0 incidents |
| Hetzner Support | https://robot.hetzner.com | Hardware failures |
| Cloudflare Status | https://cloudflarestatus.com | Tunnel/CDN issues |

---

## 7. Post-Incident

After every incident:
1. Update this runbook with lessons learned
2. Run `./scripts/backup-database.sh backup` to create fresh restore point
3. Verify all monitoring alerts are firing: `kubectl get prometheusrules -A`
4. Document timeline in `docs/production/incidents/`
