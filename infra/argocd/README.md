# ArgoCD GitOps Configuration

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


This directory contains the ArgoCD App-of-Apps configuration for recursive self-deployment of the Enclii infrastructure.

## Architecture

```
root-application.yaml
      │
      ▼
  apps/
  ├── core-services.yaml    → infra/k8s/production/ (API, UI, etc.)
  ├── monitoring.yaml       → infra/k8s/production/monitoring/
  ├── ingress.yaml          → Cloudflare Tunnel
  ├── storage.yaml          → Longhorn CSI (Helm)
  └── arc-runners.yaml      → GitHub Actions Runners (Helm)
```

## Installation

### 1. Install ArgoCD

```bash
# Create namespace
kubectl create namespace argocd

# Install ArgoCD (pin to specific version for reproducibility)
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v3.2.5/manifests/install.yaml

# Wait for pods to be ready
kubectl wait --for=condition=available --timeout=300s deployment/argocd-server -n argocd
```

### 2. Access ArgoCD UI

#### Option A: Via Cloudflare Tunnel (Production)

After deploying the tunnel, ArgoCD is accessible at:
- **URL**: https://argocd.enclii.dev
- **Username**: admin
- **Password**: See below

```bash
# Get admin password
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d && echo
```

**DNS Setup** (in Cloudflare dashboard):
```
Type: CNAME
Name: argocd
Target: <your-tunnel-id>.cfargotunnel.com
Proxied: Yes
```

#### Option B: Via Port Forward (Local Development)

```bash
# Get initial admin password
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d

# Port forward to UI
kubectl port-forward svc/argocd-server -n argocd 8080:443

# Access at https://localhost:8080
# Username: admin
# Password: (from above)
```

### 3. Deploy Root Application

```bash
# Apply the root application
kubectl apply -f infra/argocd/root-application.yaml

# Watch sync status
kubectl get applications -n argocd -w
```

## GitOps Flow

After installation, the cluster becomes self-managing:

1. **Push to Git** → Changes to `infra/` directory
2. **ArgoCD detects** → Polls repo every 3 minutes (or webhook)
3. **ArgoCD syncs** → Applies changes to cluster
4. **Self-healing** → Drift automatically corrected

## Applications

### core-services
- **Path**: `infra/k8s/production/`
- **Resources**: switchyard-api, switchyard-ui, roundhouse, waybill
- **Sync Policy**: Automated with prune and self-heal

### monitoring
- **Path**: `infra/k8s/production/monitoring/`
- **Resources**: Prometheus, Grafana, Alertmanager, Jaeger
- **Sync Policy**: Automated with self-heal

### ingress
- **Path**: `infra/k8s/production/cloudflared*.yaml`
- **Resources**: Cloudflare Tunnel deployment
- **Namespace**: cloudflare

### storage
- **Source**: Helm chart `longhorn/longhorn`
- **Namespace**: longhorn-system
- **Purpose**: Replicated storage for multi-node HA

### arc-runners
- **Source**: OCI Helm chart from GitHub
- **Namespace**: arc-systems, arc-runners
- **Purpose**: Self-hosted GitHub Actions runners

## Retiring a project

Legacy projects get their `<name>-services` Application from
`apps/project-appset.yaml`, one per `projects/<name>/config.json`. **Do not
retire one by deleting its `config.json`.** The ApplicationSet would delete
the Application, and the Application carries
`resources-finalizer.argocd.argoproj.io`, so Argo would delete every workload
it deployed with it.

Instead, add `"lifecycle": "retired"` to the `config.json`. The
ApplicationSet's `templatePatch` then renders that one Application:

- without `syncPolicy.automated`: Argo stops applying the project's repo and
  stops self-healing, so hand changes such as Deployments scaled to 0 stay;
- without the resources finalizer: deleting the Application later removes
  only the Application, never the workloads.

Every other project renders exactly as before.
`tests/scripts/test_project_appset_lifecycle.py` checks both. It was also
proven once against Argo CD v3.2.5's own ApplicationSet code over every
`config.json` (the PR that retired sim4d).

Coordinator steps after the change merges:

1. Confirm the Application changed: `enclii ops apps status <name>-services -n argocd --json`
   reports no automated sync policy and no `resources-finalizer`. The
   ApplicationSet controller reconciles within its requeue interval (3
   minutes by default).
2. Scale the project's Deployments to 0. Enclii's scale path covers only
   services bound to an explicit workload target, so for a legacy project
   this is break-glass until that adapter exists
   ([adapter gap](../../docs/ADAPTER_GAPS.md)):

   ```bash
   kubectl -n <namespace> get deploy,statefulset,hpa,scaledobject
   kubectl -n <namespace> scale deploy --all --replicas=0
   ```

   An HPA does not scale a Deployment back up from 0. A KEDA `ScaledObject`
   does, so pause or delete it first if the first command lists one.
   StatefulSets and PVCs are left alone: they hold data and are a separate
   decision.
3. Later, once nothing should come back, remove the `config.json` in a PR.
   Only the Application goes; the scaled-down workloads stay until the
   namespace is deleted on purpose.

Do not use `enclii ops apps retire` for this. While the `config.json` exists
the ApplicationSet recreates the Application within minutes. Also, that
command deletes with Kubernetes orphan propagation, but Argo CD decides
cascade by the `resources-finalizer` alone (v3.2.5
`Application.CascadedDeletion`): on an Application that still carries the
finalizer it deletes the workloads anyway.

### Retired projects

| Project | Retired | Why | Application |
| --- | --- | --- | --- |
| sim4d | 2026-11-04 | GitHub repo archived; product retired 2026-08-30 for Yantra4D | `sim4d-services`, manual sync |

## Webhooks (Optional)

For instant sync instead of polling:

```bash
# Get webhook URL
ARGOCD_WEBHOOK_URL="https://argocd.enclii.dev/api/webhook"

# Configure in GitHub repo settings:
# Settings → Webhooks → Add webhook
# Payload URL: https://argocd.enclii.dev/api/webhook
# Content type: application/json
# Secret: (generate and store)
# Events: Push events only
```

## Troubleshooting

### Application Out of Sync

```bash
# Check sync status
argocd app get core-services

# Force sync
argocd app sync core-services

# View diff
argocd app diff core-services
```

### Self-Heal Not Working

```bash
# Check application sync policy
kubectl get application core-services -n argocd -o yaml | grep -A5 syncPolicy

# Ensure selfHeal: true is set
```

### Resource Stuck in Progressing

```bash
# Check for resource issues
argocd app resources core-services

# View specific resource
kubectl describe deployment switchyard-api -n enclii
```

## Repository Credentials

For private repos or OCI registries (ghcr.io), configure credentials:

### Quick Setup (Recommended)

```bash
# Set your GitHub PAT (needs read:packages scope)
export GITHUB_TOKEN="ghp_your_token_here"

# Run the setup script
chmod +x infra/argocd/setup-credentials.sh
./infra/argocd/setup-credentials.sh
```

### Manual Setup

```bash
# Create OCI registry credentials for ghcr.io
kubectl create secret generic ghcr-oci-creds \
    --namespace=argocd \
    --from-literal=url=ghcr.io \
    --from-literal=type=helm \
    --from-literal=enableOCI=true \
    --from-literal=username=madfam-org \
    --from-literal=password="${GITHUB_TOKEN}"

kubectl label secret ghcr-oci-creds -n argocd \
    argocd.argoproj.io/secret-type=repository
```

### Verify Credentials

```bash
# List configured credentials
kubectl get secrets -n argocd -l argocd.argoproj.io/secret-type=repository

# Test by syncing an OCI-based application
kubectl patch application arc-runners -n argocd --type merge -p '{"operation":{"sync":{}}}'
```

See `repo-credentials.yaml` for a declarative template (requires secret management).

## Security

- **RBAC**: ArgoCD uses its own RBAC for UI/API access
- **Git Access**: Uses deploy keys or GitHub App for repo access
- **Secrets**: Sensitive values stored in Kubernetes Secrets (not in Git)
- **Network**: ArgoCD server can be restricted to internal access only

## Backup & Recovery

ArgoCD stores state in Kubernetes (ConfigMaps/Secrets). To backup:

```bash
# Export all applications
kubectl get applications -n argocd -o yaml > argocd-apps-backup.yaml

# Export ArgoCD config
kubectl get configmaps,secrets -n argocd -o yaml > argocd-config-backup.yaml
```

To restore:
```bash
# Reinstall ArgoCD (use pinned version matching production)
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v3.2.5/manifests/install.yaml

# Restore applications
kubectl apply -f argocd-apps-backup.yaml
```

## GitOps Maturity

With this setup, Enclii achieves **GitOps Level 4/5**:

| Level | Capability | Status |
|-------|-----------|--------|
| 1 | Version Control | ✅ |
| 2 | Declarative Config | ✅ |
| 3 | Automated Deployment | ✅ |
| 4 | Pull-based Sync | ✅ |
| 5 | Continuous Reconciliation | ✅ |

Missing for Level 5:
- External Secrets Operator (secrets from Vault)
- Image Updater (automatic image tag updates)
- Policy enforcement (OPA/Kyverno)
