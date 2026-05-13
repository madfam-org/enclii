---
title: Longhorn Volume Recovery
description: Recovery procedure for Longhorn CSI volume filesystem corruption (EXT4)
sidebar_position: 5
tags: [runbook, longhorn, storage, recovery, incident-response]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Longhorn Volume Recovery

**Purpose:** Step-by-step recovery for Longhorn EXT4 filesystem corruption incidents.
**Last Updated:** March 2026
**Incidents Covered:** 5 occurrences across Redis, Prometheus, Verdaccio, and PostHog volumes.

---

## Symptoms

Any combination of the following indicates potential Longhorn volume corruption:

| Symptom | Where to Check | Example |
|---------|---------------|---------|
| I/O errors in pod logs | `kubectl logs -n <ns> <pod>` | `MISCONF Redis is configured to save RDB snapshots, but it's currently unable to persist to disk` |
| `stop-writes-on-bgsave-error` | Redis CLI via `kubectl exec` | Redis rejecting writes, returning MISCONF on every command |
| Pod CrashLoopBackOff | `kubectl get pods -n <ns>` | Readiness probe fails because app can't write/read |
| EXT4 errors in dmesg | SSH to node, run `dmesg` | `EXT4-fs error (device sdX): ext4_journal_check_start` |
| WAL write failures | Prometheus logs | `err="write to WAL" msg="can't write to WAL"` |

---

## Diagnosis

### Step 1: Identify the Affected Volume

```bash
# Check pod status
kubectl get pods -n <namespace> -o wide

# Check PVC
kubectl get pvc -n <namespace>

# Check Longhorn volume status
kubectl get volumes.longhorn.io -n longhorn-system
```

### Step 2: Confirm Filesystem Corruption (Not Hardware)

```bash
# SSH to the node
ssh -o ProxyCommand="cloudflared access ssh --hostname %h" solarpunk@ssh.madfam.io

# Check RAID health (should be [UU] = healthy)
cat /proc/mdstat

# Check for EXT4 errors
dmesg | grep -i ext4 | tail -20

# Check disk I/O errors
dmesg | grep -iE "error|fault|fail" | grep -i disk | tail -20
```

If RAID arrays show `[U_]` or `[_U]`, that's a hardware issue — escalate differently.

### Step 3: Check Longhorn Volume Health

```bash
# Access Longhorn UI
kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80

# Or check via CLI
kubectl get volumes.longhorn.io -n longhorn-system -o custom-columns='NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness,SIZE:.spec.size'
```

---

## Recovery Procedure

### Step 1: Delete the Corrupted PVC

```bash
# Note the PVC name and volume name for logging
kubectl get pvc -n <namespace> <pvc-name> -o yaml > pvc-backup-$(date +%Y%m%d).yaml

# Delete the PVC
kubectl delete pvc -n <namespace> <pvc-name>
```

### Step 2: Wait for ArgoCD to Recreate

ArgoCD self-heal will detect the missing PVC and recreate it from the git manifest. This typically takes 30-90 seconds.

```bash
# Watch for PVC recreation
kubectl get pvc -n <namespace> -w

# Verify new PVC is Bound
kubectl get pvc -n <namespace>
# STATUS should be "Bound"
```

If ArgoCD doesn't recreate within 2 minutes:
```bash
# Force sync the application
kubectl patch application <app-name> -n argocd --type merge -p '{"operation":{"sync":{}}}'
```

### Step 3: Verify Application Reconnects

```bash
# Restart the dependent pods if they don't auto-recover
kubectl rollout restart deployment/<deployment-name> -n <namespace>

# Watch pod status
kubectl get pods -n <namespace> -w

# Check logs for successful startup
kubectl logs -n <namespace> -l app=<app-label> --tail=50
```

### Step 4: Post-Recovery Data Verification

Depending on the application type:

**Redis:**
```bash
kubectl exec -n <namespace> <redis-pod> -- redis-cli DBSIZE
kubectl exec -n <namespace> <redis-pod> -- redis-cli INFO persistence
# Verify: rdb_last_bgsave_status:ok
```

**PostgreSQL:**
```bash
kubectl exec -n <namespace> <pg-pod> -- psql -U postgres -c "\dt"
kubectl exec -n <namespace> <pg-pod> -- psql -U postgres -c "SELECT count(*) FROM pg_stat_user_tables;"
```

**Prometheus:**
```bash
kubectl exec -n monitoring <prometheus-pod> -- wget -qO- http://localhost:9090/api/v1/status/tsdb | head -20
# Verify: headStats shows recent samples
```

---

## Prevention

1. **Monitoring alerts** — `LonghornVolumeIOSaturation`, `PodWriteFailureCrashLoop`, and `LonghornVolumeNearFull` alerts in Prometheus catch early warning signs
2. **Orphaned volume cleanup** — Run monthly: `kubectl get volumes.longhorn.io -n longhorn-system` and delete detached volumes with no corresponding PVC
3. **Volume capacity headroom** — Keep volumes below 90% utilization to avoid write pressure

---

## Known Root Causes

| Cause | Frequency | Mitigation |
|-------|-----------|------------|
| Longhorn replica rebuild under I/O pressure | Most common | Monitor I/O saturation alerts |
| Node memory pressure causing OOM kills during writes | Occasional | Ensure adequate memory headroom |
| Abrupt pod termination during fsync | Rare | Graceful shutdown periods, preStop hooks |

**Important:** Longhorn volume health (healthy/degraded/faulted) is NOT the same as filesystem health. A volume can be "healthy" at the Longhorn level while the filesystem inside is corrupted.

---

## Incident Log Template

Record each incident for pattern tracking:

| Field | Value |
|-------|-------|
| **Date** | YYYY-MM-DD |
| **Namespace** | |
| **PVC Name** | |
| **Volume Name** | |
| **Application** | |
| **Symptom** | |
| **Root Cause** | |
| **Old PVC ID** | pvc-XXXXXXXX |
| **New PVC ID** | pvc-XXXXXXXX |
| **Data Loss** | Yes/No — describe |
| **Recovery Duration** | |
| **Operator** | |

### Historical Incidents

| # | Date | Namespace | Application | PVC | Notes |
|---|------|-----------|-------------|-----|-------|
| 1 | 2026-03 | data | Redis | pvc-fc0c013e → fresh | Roundhouse MISCONF errors |
| 2 | 2026-03 | monitoring | Prometheus | pvc-2259ecdc → fresh | WAL write failures |
| 3 | 2026-03 | data | Redis (yantra4d) | pvc-5c623a32 → pvc-b36cb15c | stop-writes-on-bgsave-error → flask-limiter crash |
| 4 | 2026-03 | verdaccio | Verdaccio (npm.madfam.io) | pvc-fb3f0c06 → pvc-4190cec8 | Registry 500s, had to republish packages |
| 5 | 2026-03 | posthog | ClickHouse/PostHog | Multiple | PostHog deployment paused (unrelated chart issues) |
