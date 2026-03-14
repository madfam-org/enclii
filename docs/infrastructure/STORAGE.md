# Cluster Storage with Longhorn

**Last Updated:** January 2026
**Status:** Operational (Single-Node)

---

## Overview

Enclii uses Longhorn as the Container Storage Interface (CSI) driver for persistent storage.

> **Current State:** Running on a single Hetzner dedicated server. Longhorn is deployed and configured for multi-node replication, but currently operates in single-replica mode. When additional nodes are added, replication will automatically activate.

## Architecture

**Current (Single-Node):**
```
┌─────────────────────────────────────────────────────────┐
│                  Longhorn Manager                        │
│                  (longhorn-system)                       │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
                    ┌─────────┐
                    │ Node 1  │  ◄── Hetzner dedicated server
                    │ (Single)│
                    └─────────┘
                          │
                    ┌─────────┐
                    │   Pod   │
                    │ (Mount) │
                    └─────────┘
```

**Future (Multi-Node - when nodes added):**
```
┌─────────────────────────────────────────────────────────┐
│                  Longhorn Manager                        │
│                  (longhorn-system)                       │
└─────────────────────────────────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        ▼                 ▼                 ▼
   ┌─────────┐       ┌─────────┐       ┌─────────┐
   │ Node 1  │       │ Node 2  │       │ Node 3  │
   │ Replica │◄─────►│ Replica │◄─────►│ Replica │
   └─────────┘       └─────────┘       └─────────┘
```

## Storage Classes

### longhorn (Default)

Default storage class for all workloads.

> **Note:** `numberOfReplicas: "1"` because we're on a single node. When nodes are added, update to `"2"` or `"3"` for replication.

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: longhorn
provisioner: driver.longhorn.io
parameters:
  numberOfReplicas: "1"  # Single node - no replication target
  staleReplicaTimeout: "2880"
  fromBackup: ""
  fsType: "ext4"
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true
```

| Parameter | Value | Description |
|-----------|-------|-------------|
| `numberOfReplicas` | 1 | Single replica (single-node cluster) |
| `staleReplicaTimeout` | 2880 | 48 hours before replica rebuilt |
| `reclaimPolicy` | Delete | PV deleted when PVC deleted |
| `allowVolumeExpansion` | true | Can resize volumes online |

### Future: longhorn-replicated

When additional nodes are added, create this StorageClass for HA workloads:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: longhorn-fast
provisioner: driver.longhorn.io
parameters:
  numberOfReplicas: "1"
  staleReplicaTimeout: "2880"
  fsType: "ext4"
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true
```

## Usage

### Creating a PersistentVolumeClaim

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-data
  namespace: enclii
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: longhorn
  resources:
    requests:
      storage: 10Gi
```

### Using in a Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: postgres
  namespace: enclii
spec:
  containers:
  - name: postgres
    image: postgres:15
    volumeMounts:
    - name: data
      mountPath: /var/lib/postgresql/data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: postgres-data
```

## Operations

### Accessing Longhorn UI

```bash
# Port forward to Longhorn frontend
kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80

# Access UI at http://localhost:8081
```

### Checking Volume Status

```bash
# List all Longhorn volumes
kubectl get volumes.longhorn.io -n longhorn-system

# Describe a specific volume
kubectl describe volume.longhorn.io <volume-name> -n longhorn-system

# Check PVC status
kubectl get pvc -A
```

### Checking Replica Health

```bash
# List all replicas
kubectl get replicas.longhorn.io -n longhorn-system

# Check replica distribution
kubectl get replicas.longhorn.io -n longhorn-system -o wide
```

### Volume Snapshots

```bash
# Create snapshot via Longhorn UI or API
kubectl create -f - <<EOF
apiVersion: longhorn.io/v1beta2
kind: Snapshot
metadata:
  name: postgres-data-snapshot-$(date +%Y%m%d)
  namespace: longhorn-system
spec:
  volume: <volume-name>
EOF

# List snapshots
kubectl get snapshots.longhorn.io -n longhorn-system
```

### Volume Expansion

```bash
# Edit PVC to increase size
kubectl patch pvc postgres-data -n enclii \
  --type merge -p '{"spec":{"resources":{"requests":{"storage":"20Gi"}}}}'

# Verify expansion
kubectl get pvc postgres-data -n enclii
```

## Troubleshooting

### Volume Not Attaching

```bash
# Check volume attachment status
kubectl get volumeattachments

# Check Longhorn manager logs
kubectl logs -n longhorn-system -l app=longhorn-manager -f

# Check node availability
kubectl get nodes -o wide
kubectl get pods -n longhorn-system -o wide
```

### Replica Failures

```bash
# Check replica status
kubectl get replicas.longhorn.io -n longhorn-system \
  -o custom-columns=NAME:.metadata.name,STATE:.status.currentState

# Check for disk space issues
kubectl exec -n longhorn-system <longhorn-manager-pod> -- df -h

# Force replica rebuild
kubectl patch volume <volume-name> -n longhorn-system \
  --type merge -p '{"spec":{"numberOfReplicas":2}}'
```

### Pod Stuck in "ContainerCreating"

```bash
# Check pod events
kubectl describe pod <pod-name> -n <namespace>

# Check volume status
kubectl get volumes.longhorn.io -n longhorn-system

# Check CSI driver pods
kubectl get pods -n longhorn-system -l app=longhorn-csi-plugin
```

## Backup and Recovery

### Configure Backup Target

```yaml
# Set backup target (Cloudflare R2 or S3-compatible)
kubectl patch settings backup-target -n longhorn-system \
  --type merge -p '{"value":"s3://enclii-backups@us-east-1/"}'

kubectl create secret generic longhorn-backup-secret \
  -n longhorn-system \
  --from-literal=AWS_ACCESS_KEY_ID=<key> \
  --from-literal=AWS_SECRET_ACCESS_KEY=<secret>
```

### Create Backup

```bash
# Via kubectl
kubectl create -f - <<EOF
apiVersion: longhorn.io/v1beta2
kind: Backup
metadata:
  name: postgres-backup-$(date +%Y%m%d)
  namespace: longhorn-system
spec:
  snapshotName: <snapshot-name>
EOF
```

### Restore from Backup

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-data-restored
  namespace: enclii
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: longhorn-replicated
  dataSource:
    name: <backup-name>
    kind: Backup
    apiGroup: longhorn.io
  resources:
    requests:
      storage: 10Gi
```

## Best Practices

1. **Use replicated storage for databases** - Always use `longhorn-replicated` for stateful workloads
2. **Monitor disk space** - Longhorn requires 25%+ free space per node
3. **Regular snapshots** - Schedule snapshots for critical volumes
4. **Off-site backups** - Configure backup target to R2/S3
5. **Anti-affinity** - Ensure replicas spread across nodes

## Configuration

### Installation Values

Located at `infra/helm/longhorn/values.yaml`:

```yaml
persistence:
  defaultClass: true
  defaultClassReplicaCount: 2

defaultSettings:
  backupTarget: ""
  backupTargetCredentialSecret: ""
  createDefaultDiskLabeledNodes: true
  defaultDataPath: /var/lib/longhorn
  storageMinimalAvailablePercentage: 25
  upgradeChecker: false
```

## Redis Storage

**Status:** Single Instance (Sentinel Ready for Multi-Node)

### Current Configuration

Redis runs as a single Deployment from `infra/k8s/base/redis.yaml`:

```yaml
Type: Deployment
Replicas: 1
Storage: 5Gi PVC
Persistence: AOF + RDB snapshots
```

### Redis Sentinel (Ready for Multi-Node)

Configuration exists at `infra/k8s/production/redis-sentinel.yaml` but is **disabled** on single-node:

```yaml
Type: StatefulSet + Sentinel
Replicas: 3 Redis + 3 Sentinel (co-located)
HA: Automatic failover with quorum
Requirement: 3 different nodes (podAntiAffinity)
```

> **Why Disabled:** The Sentinel config uses `requiredDuringSchedulingIgnoredDuringExecution` podAntiAffinity, requiring 3 different nodes. On single-node, only redis-0 would schedule; redis-1 and redis-2 would be Pending forever.

### When to Enable Sentinel

Enable when multi-node cluster is deployed:

```bash
# In infra/k8s/production/kustomization.yaml, uncomment:
# - redis-sentinel.yaml
```

### Infrastructure Audit Decision (Jan 2026)

| Component | Decision | Rationale |
|-----------|----------|-----------|
| Redis Sentinel | Skip for now | Single-node cluster; Sentinel requires 3 nodes |
| Ubicloud PostgreSQL | Not needed | Current setup meets 99.5% SLA / 24-hour RPO |

Redis Sentinel configuration is **staged and ready** for multi-node deployment.

## Related Documentation

- [GitOps with ArgoCD](./GITOPS.md)
- [Cloudflare Integration](./CLOUDFLARE.md)
- Deployment Guide: `infra/DEPLOYMENT.md`
- [Production Checklist](../production/PRODUCTION_CHECKLIST.md)

## Verification

```bash
# Verify Longhorn is healthy
kubectl get pods -n longhorn-system

# Expected: All pods Running
NAME                                        READY   STATUS    RESTARTS
longhorn-manager-xxxxx                      1/1     Running   0
longhorn-driver-deployer-xxxxx              1/1     Running   0
longhorn-csi-plugin-xxxxx                   2/2     Running   0
...

# Check storage class
kubectl get sc

# Expected:
NAME                   PROVISIONER          RECLAIMPOLICY   VOLUMEBINDINGMODE
longhorn-replicated    driver.longhorn.io   Delete          Immediate
longhorn-fast          driver.longhorn.io   Delete          Immediate
```
