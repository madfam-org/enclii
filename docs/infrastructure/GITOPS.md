# GitOps with ArgoCD

**Last Updated:** January 26, 2026
**Status:** Operational (13 applications, 8 Synced/Healthy)

---

## Overview

Enclii uses ArgoCD for GitOps-based deployment management. All infrastructure and application deployments are declaratively defined in Git and automatically synchronized to the cluster.

## Architecture

```
Git Repository (infra/argocd/)
         │
         ▼
    ArgoCD Server
    (argocd namespace)
         │
    ┌────┴────┐
    ▼         ▼
Root App   Self-heal
    │         │
    ▼         ▼
Child Apps   Drift
(per-service) correction
```

### App-of-Apps Pattern

ArgoCD follows the App-of-Apps pattern:

1. **Root Application** (`infra/argocd/root-application.yaml`)
   - Manages all child applications
   - Points to `infra/argocd/apps/` directory

2. **Child Applications** (`infra/argocd/apps/*.yaml`)
   - One Application per service/component
   - Each defines source, destination, sync policy

## Configuration

### Directory Structure

```
infra/argocd/
├── root-application.yaml       # Root app (enclii-infrastructure)
├── apps/
│   ├── arc-runners.yaml        # GitHub Actions Runner Controller
│   ├── core-services.yaml      # Core platform (switchyard-api/ui, dispatch, etc.)
│   ├── external-secrets.yaml   # External Secrets Operator
│   ├── image-updater.yaml      # ArgoCD Image Updater (Helm)
│   ├── image-updater-config.yaml # Image Updater configuration
│   ├── ingress.yaml            # Cloudflared tunnel ingress
│   ├── kyverno.yaml            # Kyverno policy engine (Helm)
│   ├── kyverno-policies.yaml   # Custom Kyverno policies (in kyverno.yaml)
│   ├── longhorn.yaml           # Longhorn storage (Helm)
│   └── monitoring.yaml         # Prometheus, Grafana, AlertManager
└── README.md
```

### Root Application

```yaml
# infra/argocd/root-application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: root
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/madfam-org/enclii
    targetRevision: main
    path: infra/argocd/apps
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### Child Application Template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: switchyard-api
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/madfam-org/enclii
    targetRevision: main
    path: infra/k8s/production
  destination:
    server: https://kubernetes.default.svc
    namespace: enclii
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

## Operations

### Accessing ArgoCD UI

```bash
# Port forward to ArgoCD server
kubectl port-forward svc/argocd-server -n argocd 8080:443

# Get admin password
kubectl -n argocd get secret argocd-initial-admin-secret \
  -o jsonpath="{.data.password}" | base64 -d

# Access UI at https://localhost:8080
# Username: admin
# Password: (from command above)
```

### Checking Sync Status

```bash
# List all applications
kubectl get applications -n argocd

# Describe specific application
kubectl describe application switchyard-api -n argocd

# Check application health
kubectl get applications -n argocd -o wide
```

### Manual Sync Operations

```bash
# Force sync an application
kubectl patch application switchyard-api -n argocd \
  --type merge -p '{"operation":{"sync":{}}}'

# Refresh application (fetch latest manifests)
kubectl patch application switchyard-api -n argocd \
  --type merge -p '{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}'
```

### Rollback

```bash
# View application history
argocd app history switchyard-api

# Rollback to specific revision
argocd app rollback switchyard-api <revision>
```

## Sync Policies

### Automated Sync

All applications are configured with automated sync:

| Setting | Value | Description |
|---------|-------|-------------|
| `prune` | `true` | Delete resources removed from Git |
| `selfHeal` | `true` | Revert manual cluster changes |
| `allowEmpty` | `false` | Prevent sync of empty directories |

### Self-Healing

When `selfHeal: true`, ArgoCD automatically corrects drift:

1. Manual `kubectl edit` changes are reverted
2. Resource deletions are restored
3. Configuration drift is corrected

**Important:** Disable self-heal temporarily during debugging:

```bash
kubectl patch application switchyard-api -n argocd \
  --type merge -p '{"spec":{"syncPolicy":{"automated":{"selfHeal":false}}}}'
```

## Troubleshooting

### Application Stuck in "Progressing"

```bash
# Check application events
kubectl describe application <app-name> -n argocd

# Check pod status in target namespace
kubectl get pods -n enclii

# View ArgoCD controller logs
kubectl logs -n argocd \
  -l app.kubernetes.io/name=argocd-application-controller -f
```

### Sync Failed

```bash
# View sync operation details
kubectl get application <app-name> -n argocd -o yaml | \
  yq '.status.operationState'

# Check for resource conflicts
kubectl get application <app-name> -n argocd -o yaml | \
  yq '.status.sync.comparedTo'
```

### Out of Sync

```bash
# View what's different
kubectl get application <app-name> -n argocd -o yaml | \
  yq '.status.resources[] | select(.status != "Synced")'
```

## Best Practices

1. **Never edit resources directly** - Always modify Git and let ArgoCD sync
2. **Use Kustomize overlays** - Environment-specific configs in `infra/k8s/{env}/`
3. **Review sync status** - Check ArgoCD UI before deployments
4. **Monitor drift** - Self-heal ensures consistency but review causes

## Related Documentation

- Deployment Guide: `infra/DEPLOYMENT.md`
- [Storage with Longhorn](./STORAGE.md)
- [Cloudflare Integration](./CLOUDFLARE.md)
- [Production Checklist](../production/PRODUCTION_CHECKLIST.md)

## Application Inventory (Jan 26, 2026)

| Application | Source | Sync | Health | Notes |
|-------------|--------|------|--------|-------|
| enclii-infrastructure | infra/argocd/apps (Git) | OutOfSync | Healthy | Root app-of-apps, child drift propagates |
| core-services | infra/k8s/production (Git) | Synced | Progressing | Ingress resource cosmetic |
| ingress | infra/k8s/production/cloudflared* (Git) | Synced | Healthy | |
| monitoring | infra/k8s/production/monitoring (Git) | Synced | Healthy | |
| kyverno | kyverno.github.io/kyverno (Helm 3.1.4) | Synced | Healthy | Hooks disabled |
| kyverno-policies | infra/k8s/base/kyverno/policies (Git) | OutOfSync | Healthy | SSA metadata drift |
| longhorn | charts.longhorn.io (Helm 1.7.2) | Synced | Healthy | |
| external-secrets | external-secrets.io (Helm 0.9.11) | Synced | Healthy | |
| external-secrets-config | infra/k8s/base/external-secrets (Git) | Synced | Degraded | Doppler not provisioned |
| argocd-image-updater | argoproj.github.io/argo-helm (Helm) | OutOfSync | Healthy | Shared ConfigMap |
| image-updater-config | infra/argocd-image-updater (Git) | Synced | Healthy | |
| arc-runners | oci://ghcr.io/actions (Helm) | Unknown | Healthy | OCI chart fetch |
| arc-runners-blue | oci://ghcr.io/actions (Helm) | Unknown | Healthy | OCI chart fetch |

### Known Sync Issues

- **OutOfSync/Healthy**: These are cosmetic. SSA (ServerSideApply) adds metadata that doesn't match Git source. All resources function correctly.
- **Unknown sync**: ArgoCD v3.2.5 has a bug in multi-source OCI Helm revision resolution — see [ArgoCD Known Issues](./ARGOCD_KNOWN_ISSUES.md) for full analysis, reproduction steps, and proposed upstream fix. The ARC runners work correctly despite the Unknown status.
- **Degraded health**: external-secrets-config references a Doppler SecretStore that hasn't been provisioned yet.

## Lessons Learned (Jan 2026 Audit)

### Kyverno Helm Chart Values Paths
- `cleanupJobs`, `webhooksCleanup`, `policyReportsCleanup` are **top-level** keys in Kyverno chart 3.1.4
- They are NOT nested under `admissionController`
- Verify with: `helm template kyverno kyverno/kyverno --version 3.1.4 --set cleanupJobs.admissionReports.image.tag=latest`

### Bitnami Docker Hub Images
- Bitnami has removed ALL version-specific tags from Docker Hub
- Only `latest` and SHA-based tags are available
- Use `bitnami/kubectl:latest` or switch to a different kubectl image

### ArgoCD Helm Hook Deadlocks
- Helm pre-upgrade/post-upgrade hooks can deadlock if they reference images that need updating
- The hook uses the old image (which may be deleted), but updating the image requires the sync to complete
- Solution: Disable hooks via Helm values (`webhooksCleanup.enabled: false`, `policyReportsCleanup.enabled: false`)

### ServerSideApply Metadata Drift
- ArgoCD with `ServerSideApply=true` can show OutOfSync due to SSA metadata fields
- Use `RespectIgnoreDifferences=true` sync option to reduce noise
- Not a functional issue — resources work correctly

## Verification

```bash
# Verify ArgoCD is healthy
KUBECONFIG=~/.kube/config-hetzner kubectl get applications -n argocd

# Expected output (Jan 26, 2026):
# NAME                      SYNC        HEALTH
# arc-runners               Unknown     Healthy
# arc-runners-blue          Unknown     Healthy
# argocd-image-updater      OutOfSync   Healthy
# core-services             Synced      Progressing
# enclii-infrastructure     OutOfSync   Healthy
# external-secrets          Synced      Healthy
# external-secrets-config   Synced      Degraded
# image-updater-config      Synced      Healthy
# ingress                   Synced      Healthy
# kyverno                   Synced      Healthy
# kyverno-policies          OutOfSync   Healthy
# longhorn                  Synced      Healthy
# monitoring                Synced      Healthy
```
