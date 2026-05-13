---
title: k3s Cluster Upgrade
description: Runbook for upgrading k3s on the 3-node enclii production cluster with zero-downtime rolling procedure
sidebar_position: 8
tags: [operations, runbook, k3s, upgrade, cluster]
---

# k3s Cluster Upgrade

**Purpose:** Step-by-step procedure for upgrading k3s on the enclii production cluster (3-node: foundry-cp server + foundry-worker-01 agent + foundry-builder-01 agent). The server node is upgraded first, then agent nodes one at a time, ensuring all nodes end at the same k3s version.

**Last Updated:** April 2026

**Current Version:** v1.33.7+k3s3

**Cluster Topology:**

| Node | Role | Notes |
|------|------|-------|
| foundry-cp | Server (control plane) | EX44, i5-13500, 128GB. K3s API: <CONTROL_PLANE_IP>:6443 |
| foundry-worker-01 | Agent (worker) | AX41, Ryzen 5 3600, 64GB. Runs platform workloads + Longhorn storage |
| foundry-builder-01 | Agent (builder) | VPS, 2 vCPU, 4GB. Taint `builder=true:NoSchedule` -- runs only ARC GitHub Actions runners |

**SSH Access:** `ssh ssh.madfam.io` (the ONLY authorized method -- never use direct IP)

**SCP Access:** `scp -o ProxyCommand="cloudflared access ssh --hostname ssh.madfam.io" ...`

---

## Prerequisites

Complete every item in this checklist before starting the upgrade. Do not proceed if any check fails.

### 1. Identify Target Version

Review the k3s release notes for the target version. Pay attention to:
- Kubernetes API deprecations and removals
- containerd / runc version changes
- Embedded component updates (CoreDNS, Traefik, metrics-server)
- Known issues with the release

```bash
# Check current version on both nodes
ssh ssh.madfam.io
k3s --version
# Expected: k3s version v1.33.7+k3s3

# On the worker node (from foundry-cp)
ssh foundry-builder-01 k3s --version
# Expected: same version as server
```

### 2. Verify Component Compatibility

Before upgrading, confirm that these cluster components support the target Kubernetes version:

| Component | Current Version | Compatibility Doc |
|-----------|----------------|-------------------|
| Longhorn CSI | v1.7.2 | [longhorn.io/docs](https://longhorn.io/docs/) |
| Kyverno | Check installed | [kyverno.io/docs](https://kyverno.io/docs/) |
| ESO | v0.9.11 | [external-secrets.io](https://external-secrets.io/) |
| ArgoCD | Check installed | [argo-cd.readthedocs.io](https://argo-cd.readthedocs.io/) |

```bash
# Check installed versions
kubectl get deploy -n longhorn-system -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
kubectl get deploy -n kyverno -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
kubectl get deploy -n external-secrets -o jsonpath='{.items[0].spec.template.spec.containers[0].image}'
kubectl get deploy -n argocd argocd-server -o jsonpath='{.spec.template.spec.containers[0].image}'
```

### 3. Backup k3s Datastore

**CRITICAL:** Always snapshot before upgrading. This is your rollback safety net.

```bash
ssh ssh.madfam.io

# Create a named etcd snapshot
sudo k3s etcd-snapshot save --name pre-upgrade-$(date +%Y%m%d)

# Verify the snapshot was created
sudo k3s etcd-snapshot ls
# Expected: pre-upgrade-YYYYMMDD listed with recent timestamp
```

Record the snapshot path from the output. Default location: `/var/lib/rancher/k3s/server/db/snapshots/`.

### 4. Check ArgoCD Sync Status

All applications must be Synced and Healthy before starting. Do not upgrade with OutOfSync or Degraded apps.

```bash
kubectl get applications -n argocd
# Expected: all apps show Synced + Healthy (or Progressing for known-OK cases)

# If any app is OutOfSync, investigate and resolve first
kubectl describe application <app-name> -n argocd
```

### 5. Verify Longhorn Volume Health

All volumes must be Healthy. Degraded volumes risk data loss during node drain.

```bash
kubectl get volumes.longhorn.io -n longhorn-system -o custom-columns=NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness
# Expected: all volumes show "attached" or "detached" with robustness "healthy"
```

### 6. Verify Disk Space

Both nodes need at least 5 GB free for the upgrade binary and temporary files.

```bash
ssh ssh.madfam.io

# Check server node
df -h /var/lib/rancher
# Expected: >5 GB available

# Check worker node
ssh foundry-builder-01 df -h /var/lib/rancher
# Expected: >5 GB available
```

### 7. Record Current Pod State

Capture a baseline so you can compare after the upgrade.

```bash
kubectl get nodes -o wide > /tmp/pre-upgrade-nodes.txt
kubectl get pods -A -o wide > /tmp/pre-upgrade-pods.txt
kubectl top nodes > /tmp/pre-upgrade-resources.txt 2>/dev/null || true

echo "--- Node count ---"
kubectl get nodes --no-headers | wc -l
echo "--- Pod count ---"
kubectl get pods -A --no-headers | wc -l
echo "--- Not-Running pods ---"
kubectl get pods -A --no-headers | grep -v Running | grep -v Completed || echo "None"
```

---

## Upgrade Sequence (Zero-Downtime)

Upgrade order: **server node first, then agent node**. Never upgrade both simultaneously.

Set the target version as a variable for use throughout:

```bash
export K3S_TARGET="v1.XX.Y+k3s1"  # Replace with actual target version
```

### Step 1 -- Upgrade Server Node (foundry-cp)

#### 1A. Cordon the Server Node

Prevent new pods from being scheduled on the node being upgraded.

```bash
kubectl cordon foundry-cp
kubectl get nodes
# Expected: foundry-cp shows SchedulingDisabled
```

#### 1B. Drain the Server Node

Evict all non-DaemonSet pods. The 120-second grace period allows in-flight requests to complete.

```bash
kubectl drain foundry-cp \
  --ignore-daemonsets \
  --delete-emptydir-data \
  --grace-period=120 \
  --timeout=300s
```

**Note:** When draining foundry-cp, pods can reschedule to foundry-worker-01 (the general-purpose worker). foundry-builder-01 has the `builder=true:NoSchedule` taint and only accepts ARC runner pods. If draining foundry-worker-01, pods may go Pending until it is uncordoned since foundry-cp is the server node and foundry-builder-01 only accepts builder workloads.

If drain times out or a PodDisruptionBudget blocks eviction:

```bash
# Identify stuck pods
kubectl get pods -A --field-selector spec.nodeName=foundry-cp | grep -v Running

# If safe, force delete the stuck pod (use with caution)
kubectl delete pod <pod-name> -n <namespace> --grace-period=0 --force
```

#### 1C. Upgrade k3s on the Server

```bash
ssh ssh.madfam.io

# Install the target version
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="${K3S_TARGET}" sh -s - server

# The installer restarts the k3s-server systemd service automatically
```

#### 1D. Verify Server Upgrade

```bash
# Still on foundry-cp via SSH
k3s --version
# Expected: k3s version <K3S_TARGET>

# Check that the node is Ready (may take 30-60 seconds)
kubectl get nodes
# Expected: foundry-cp shows Ready,SchedulingDisabled with the new version

# Check k3s service status
sudo systemctl status k3s
# Expected: active (running)
```

#### 1E. Uncordon the Server Node

```bash
kubectl uncordon foundry-cp
kubectl get nodes
# Expected: foundry-cp shows Ready (no SchedulingDisabled)
```

#### 1F. Wait for Pod Recovery

Pods that were pending during the drain will now reschedule. Wait for the cluster to stabilize.

```bash
# Watch pod status until stable (Ctrl+C to exit)
kubectl get pods -A --watch

# Or check for non-running pods periodically
kubectl get pods -A --no-headers | grep -v Running | grep -v Completed
# Expected: empty (all pods running or completed)
```

Allow up to 5 minutes for all pods to return to Running. Longhorn and ArgoCD pods may take longest.

---

### Step 2 -- Upgrade Worker Node (foundry-worker-01)

#### 2A. Cordon the Worker Node

```bash
kubectl cordon foundry-worker-01
kubectl get nodes
# Expected: foundry-worker-01 shows SchedulingDisabled
```

#### 2B. Drain the Worker Node

foundry-worker-01 runs platform workloads and Longhorn storage. Drained pods will reschedule to foundry-cp (server node). Longhorn volumes will be served from the remaining replica.

```bash
kubectl drain foundry-worker-01 \
  --ignore-daemonsets \
  --delete-emptydir-data \
  --grace-period=120 \
  --timeout=300s
```

#### 2C. Upgrade k3s Agent on foundry-worker-01

```bash
ssh ssh.madfam.io

# From foundry-cp, SSH to the worker
ssh foundry-worker-01

# Install the target version as an agent
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="${K3S_TARGET}" sh -s - agent

# The installer restarts the k3s-agent systemd service automatically
```

#### 2D. Verify Worker Upgrade

```bash
# On foundry-worker-01
k3s --version
# Expected: k3s version <K3S_TARGET>

# Back on foundry-cp (or any machine with kubectl)
kubectl get nodes
# Expected: foundry-worker-01 shows Ready,SchedulingDisabled with the new version

# Check agent service status (on foundry-worker-01)
sudo systemctl status k3s-agent
# Expected: active (running)
```

#### 2E. Uncordon the Worker Node

```bash
kubectl uncordon foundry-worker-01
kubectl get nodes
# Expected: foundry-worker-01 shows Ready (no SchedulingDisabled)
```

#### 2F. Wait for Pod Recovery

Wait for pods to stabilize before proceeding to the builder node.

```bash
kubectl get pods -A --no-headers | grep -v Running | grep -v Completed
# Expected: empty (all pods running or completed)
```

---

### Step 3 -- Upgrade Agent Node (foundry-builder-01)

#### 3A. Cordon the Builder Node

```bash
kubectl cordon foundry-builder-01
kubectl get nodes
# Expected: foundry-builder-01 shows SchedulingDisabled
```

#### 3B. Drain the Builder Node

Because of the `builder=true:NoSchedule` taint, only ARC runner pods and DaemonSets run here. The drain is typically fast.

```bash
kubectl drain foundry-builder-01 \
  --ignore-daemonsets \
  --delete-emptydir-data \
  --grace-period=120 \
  --timeout=300s
```

#### 3C. Upgrade k3s Agent on foundry-builder-01

```bash
ssh ssh.madfam.io

# From foundry-cp, SSH to the builder
ssh foundry-builder-01

# Install the target version as an agent
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="${K3S_TARGET}" sh -s - agent

# The installer restarts the k3s-agent systemd service automatically
```

#### 3D. Verify Builder Upgrade

```bash
# On foundry-builder-01
k3s --version
# Expected: k3s version <K3S_TARGET>

# Back on foundry-cp (or any machine with kubectl)
kubectl get nodes
# Expected: foundry-builder-01 shows Ready,SchedulingDisabled with the new version

# Check agent service status (on foundry-builder-01)
sudo systemctl status k3s-agent
# Expected: active (running)
```

#### 3E. Uncordon the Builder Node

```bash
kubectl uncordon foundry-builder-01
kubectl get nodes
# Expected: all 3 nodes Ready, all showing the same new version
```

---

## Post-Upgrade Verification

Run every check in this section. The upgrade is not complete until all pass.

### Node Version Match

```bash
kubectl get nodes -o wide
# CRITICAL: All 3 nodes must show identical k3s version in the VERSION column
```

### Cluster Health

```bash
# All pods should be Running or Completed
kubectl get pods -A --no-headers | grep -v Running | grep -v Completed
# Expected: empty output

# Node resource usage looks normal
kubectl top nodes
```

### ArgoCD Applications

```bash
kubectl get applications -n argocd
# Expected: all applications Synced + Healthy

# If any app shows OutOfSync after upgrade, it may need a manual sync
kubectl patch application <app-name> -n argocd --type merge -p '{"operation":{"sync":{}}}'
```

### Longhorn Storage

```bash
# All volumes healthy
kubectl get volumes.longhorn.io -n longhorn-system -o custom-columns=NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness
# Expected: all healthy

# Longhorn manager pods running
kubectl get pods -n longhorn-system -l app=longhorn-manager
# Expected: one pod per node, both Running
```

### Platform Services Health

```bash
# Control plane API
curl -s https://api.enclii.dev/health
# Expected: 200 OK

# Web UI
curl -s -o /dev/null -w "%{http_code}" https://app.enclii.dev
# Expected: 200

# Status page
curl -s -o /dev/null -w "%{http_code}" https://status.enclii.dev
# Expected: 200

# Admin console
curl -s -o /dev/null -w "%{http_code}" https://admin.enclii.dev
# Expected: 200 (or 302 redirect to auth)

# Auth provider
curl -s https://auth.madfam.io/.well-known/openid-configuration | head -c 100
# Expected: valid JSON
```

### Kyverno and ESO

```bash
# Kyverno controller running
kubectl get pods -n kyverno
# Expected: all pods Running

# Policies still enforced
kubectl get clusterpolicies
# Expected: policies listed and Ready

# External Secrets syncing
kubectl get externalsecrets -A
# Expected: all show SecretSynced=True (or no ESO resources if Vault not yet deployed)
```

### Compare to Baseline

```bash
echo "--- Post-upgrade node count ---"
kubectl get nodes --no-headers | wc -l
echo "--- Post-upgrade pod count ---"
kubectl get pods -A --no-headers | wc -l

# Compare with pre-upgrade snapshot
# Node count should be identical (3)
# Pod count should be approximately the same (within +/- 5 of pre-upgrade)
```

---

## CRD Migration Notes

Kubernetes minor version upgrades can deprecate or remove CRD API versions. Check the following after each upgrade.

### Kyverno

```bash
kubectl get crd | grep kyverno
# Verify CRDs still exist and are served

kubectl get clusterpolicies
# If this fails with a version error, Kyverno CRDs need updating
# Follow: https://kyverno.io/docs/installation/upgrading/
```

### Longhorn

```bash
kubectl get crd | grep longhorn
# Verify all Longhorn CRDs are present

# Test volume operations still work
kubectl get storageclass longhorn
# Expected: longhorn StorageClass still present and default
```

### External Secrets Operator

```bash
kubectl get crd | grep external-secrets
# ESO v0.9.11 uses v1beta1 CRDs
# If the target k3s version removes v1beta1, ESO must be upgraded first
# Check: https://external-secrets.io/latest/introduction/stability-support/
```

### ArgoCD

```bash
kubectl get crd | grep argoproj
# Verify Application and AppProject CRDs are intact

kubectl get applications -n argocd
# If this fails, ArgoCD CRDs may need migration
# Check: https://argo-cd.readthedocs.io/en/stable/operator-manual/upgrading/overview/
```

---

## Rollback Procedure

Use this procedure if the upgrade fails and the cluster is not recoverable through normal means.

**Risk:** High. Cluster reset restores the datastore to its pre-upgrade state. Any changes made after the snapshot (new deployments, secret updates, etc.) will be lost.

### Rollback Server Node

```bash
ssh ssh.madfam.io

# 1. Stop k3s
sudo systemctl stop k3s

# 2. Restore from the pre-upgrade etcd snapshot
sudo k3s server \
  --cluster-reset \
  --cluster-reset-restore-path=/var/lib/rancher/k3s/server/db/snapshots/pre-upgrade-YYYYMMDD

# 3. Wait for the reset to complete (watch for "Managed etcd cluster membership has been reset" message)

# 4. Install the previous k3s version
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="v1.33.7+k3s3" sh -s - server

# 5. Verify
k3s --version
kubectl get nodes
kubectl get pods -A
```

### Rollback Agent Node

If only the agent upgrade failed and the server is healthy:

```bash
ssh ssh.madfam.io
ssh foundry-builder-01

# 1. Stop k3s-agent
sudo systemctl stop k3s-agent

# 2. Install the previous k3s version as agent
curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="v1.33.7+k3s3" sh -s - agent

# 3. Verify
k3s --version
kubectl get nodes
# Expected: foundry-builder-01 shows previous version and Ready
```

### Post-Rollback Verification

After rolling back, run the full Post-Upgrade Verification section above to confirm the cluster is healthy at the previous version.

---

## Timing and Communication

### Schedule

- **Window:** UTC 02:00 - 06:00 (low-traffic period)
- **Expected Duration:** 45-60 minutes total (15-20 min per node including verification)
- **Buffer:** Schedule a 2-hour window to allow for complications

### Communication Checklist

| When | Action |
|------|--------|
| T-7 days | Announce upgrade in team channel with target version and date |
| T-24 hours | Reminder with exact time window and expected impact |
| T-1 hour | Post maintenance notice on status.enclii.dev |
| T-0 | Begin upgrade, update status page to "Under Maintenance" |
| Completion | Update status page to "Operational", confirm in team channel |
| T+1 day | Remove maintenance notice, archive this run's notes |

### Status Page Updates

```bash
# Before upgrade -- create maintenance incident
curl -X POST https://status.enclii.dev/api/incidents \
  -H "Authorization: Bearer ${ADMIN_SECRET}" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Scheduled Maintenance: k3s Cluster Upgrade",
    "description": "Upgrading k3s from v1.33.7+k3s3 to '"${K3S_TARGET}"'. Brief service interruptions possible during node drains.",
    "severity": "maintenance",
    "status": "investigating"
  }'

# After upgrade -- resolve incident
curl -X POST https://status.enclii.dev/api/incidents \
  -H "Authorization: Bearer ${ADMIN_SECRET}" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Scheduled Maintenance Complete",
    "description": "k3s upgrade to '"${K3S_TARGET}"' completed successfully. All services operational.",
    "severity": "maintenance",
    "status": "resolved"
  }'
```

---

## Upgrade Log Template

Copy this template for each upgrade and fill it in as you go. Store completed logs in `docs/runbooks/upgrade-logs/`.

```
## k3s Upgrade Log -- YYYY-MM-DD

**Operator:** <name>
**Previous Version:** v1.33.7+k3s3
**Target Version:** <version>
**Start Time (UTC):** HH:MM
**End Time (UTC):** HH:MM

### Pre-Upgrade
- [ ] Release notes reviewed
- [ ] Component compatibility verified
- [ ] etcd snapshot created: <snapshot name>
- [ ] ArgoCD apps all Synced
- [ ] Longhorn volumes all Healthy
- [ ] Disk space verified (server: __GB, worker: __GB)
- [ ] Baseline pod count: __

### Server Upgrade (foundry-cp)
- [ ] Cordoned
- [ ] Drained (duration: __)
- [ ] k3s upgraded
- [ ] Version verified
- [ ] Uncordoned
- [ ] Pods recovered

### Worker Upgrade (foundry-worker-01)
- [ ] Cordoned
- [ ] Drained (duration: __)
- [ ] k3s upgraded
- [ ] Version verified
- [ ] Uncordoned
- [ ] Pods recovered

### Builder Upgrade (foundry-builder-01)
- [ ] Cordoned
- [ ] Drained (duration: __)
- [ ] k3s upgraded
- [ ] Version verified
- [ ] Uncordoned

### Post-Upgrade Verification
- [ ] All 3 nodes same version
- [ ] All pods Running
- [ ] ArgoCD apps Synced
- [ ] Longhorn volumes Healthy
- [ ] api.enclii.dev health OK
- [ ] app.enclii.dev reachable
- [ ] status.enclii.dev reachable
- [ ] auth.madfam.io OIDC OK
- [ ] Kyverno policies enforced
- [ ] CRDs intact
- [ ] Post-upgrade pod count: __ (baseline: __)

### Issues Encountered
<none or describe>

### Rollback Required?
<no / yes -- describe>
```
