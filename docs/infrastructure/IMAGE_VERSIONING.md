# Container Image Versioning Policy

**Last Updated:** Feb 3, 2026 (Wave 13)
**Purpose:** Define standards for container image tags and versioning

---

## Overview

Container images in the Enclii ecosystem follow a tiered versioning strategy based on their role and update frequency.

---

## Image Categories

### 1. Application Images (ArgoCD Image Updater Managed)

These images use mutable tags (`latest`, `main`) as **triggers** for ArgoCD Image Updater, which automatically updates them to immutable digests.

| Image | Tag Strategy | Update Method |
|-------|--------------|---------------|
| `ghcr.io/madfam-org/enclii/*` | `:latest` | ArgoCD Image Updater |
| `ghcr.io/madfam-org/janua-*` | `:latest` | ArgoCD Image Updater |
| `ghcr.io/madfam-org/dhanam-*` | `:main` | ArgoCD Image Updater |

**How It Works:**
1. CI/CD pushes image with `:latest` tag
2. ArgoCD Image Updater detects new digest
3. Deployment updated with `@sha256:...` digest
4. Rollout proceeds with immutable reference

**Configuration:** See `infra/argocd/apps/image-updater-config.yaml`

### 2. Infrastructure Images (Pinned Versions)

These images should use **specific version tags** and be updated manually during maintenance windows.

| Image | Current Version | Update Cadence |
|-------|-----------------|----------------|
| `prom/prometheus` | `v2.53.3` | Quarterly |
| `grafana/grafana` | `10.2.2` | Quarterly |
| `prom/alertmanager` | `v0.26.0` | Quarterly |
| `bitnami/kubectl` | `1.33` | With k3s upgrades |

**Location:** `infra/k8s/production/monitoring/*.yaml`

### 3. Third-Party Images (Pinned Versions)

External images should use specific version tags for reproducibility.

| Image | Current Version | Notes |
|-------|-----------------|-------|
| `cloudflare/cloudflared` | `2025.11.1` | Tunnel client |
| `actions/actions-runner` | Via Helm | ARC runners |

---

## Image Tag Requirements

### Allowed Tags

```yaml
# Specific version (recommended for infra)
image: prom/prometheus:v2.53.3

# Digest reference (most secure)
image: prom/prometheus@sha256:abc123...

# Mutable tag with Image Updater (application images only)
image: ghcr.io/madfam-org/enclii/admin-console:latest
# (Image Updater will convert to digest)
```

### Forbidden Tags

```yaml
# Never use in production manifests
image: myapp           # No tag = :latest implied
image: myapp:latest    # Without Image Updater management
```

---

## Kyverno Policy Enforcement

The `disallow-latest-tag` policy blocks `:latest` tags **except** for images managed by ArgoCD Image Updater.

```yaml
# infra/k8s/base/kyverno/policies/best-practices.yaml
- name: disallow-latest-tag
  match:
    resources:
      kinds:
        - Pod
  exclude:
    # ArgoCD Image Updater managed namespaces
    namespaces:
      - enclii
      - janua
      - dhanam
  validate:
    message: "Using 'latest' tag is not allowed. Use specific version tags."
    pattern:
      spec:
        containers:
          - image: "!*:latest"
```

---

## Updating Infrastructure Images

### Pre-requisites

1. Review release notes for breaking changes
2. Test in non-production environment
3. Schedule maintenance window

### Process

```bash
# 1. Update image tag in manifest
# e.g., infra/k8s/production/monitoring/prometheus.yaml

# 2. Commit and push
git add -A && git commit -m "chore(monitoring): upgrade prometheus to v2.54.0"
git push

# 3. Wait for ArgoCD sync or force sync
kubectl patch application monitoring -n argocd --type merge \
  -p '{"operation":{"sync":{}}}'

# 4. Verify rollout
kubectl rollout status deploy/prometheus -n monitoring
```

---

## Image Digest Lookup

To find the current digest for an image:

```bash
# For GHCR images
skopeo inspect docker://ghcr.io/madfam-org/enclii/admin-console:latest | jq -r '.Digest'

# For Docker Hub images
skopeo inspect docker://prom/prometheus:v2.53.3 | jq -r '.Digest'

# Using crane (if available)
crane digest prom/prometheus:v2.53.3
```

---

## Current Image Inventory

### Production Manifests

| Location | Image | Version | Strategy |
|----------|-------|---------|----------|
| monitoring/prometheus.yaml | prom/prometheus | v2.53.3 | Pinned |
| monitoring/grafana.yaml | grafana/grafana | 10.2.2 | Pinned |
| monitoring/alertmanager.yaml | prom/alertmanager | v0.26.0 | Pinned |
| ghcr-credential-check.yaml | bitnami/kubectl | 1.33 | Pinned |
| maintenance/*.yaml | bitnami/kubectl | 1.33 | Pinned |

### Application Deployments

| Deployment | Image | Tag | Strategy |
|------------|-------|-----|----------|
| switchyard-api | ghcr.io/madfam-org/switchyard-api | latest | Image Updater |
| switchyard-ui | ghcr.io/madfam-org/enclii/switchyard-ui | latest | Image Updater |
| dispatch | ghcr.io/madfam-org/enclii/admin-console | latest | Image Updater |
| docs-site | ghcr.io/madfam-org/enclii/docs-site | latest | Image Updater |
| landing-page | ghcr.io/madfam-org/enclii/landing-page | latest | Image Updater |
| status | ghcr.io/madfam-org/enclii/enclii-status | latest | Image Updater |

---

## Audit Checklist

- [ ] All infrastructure images use specific version tags
- [ ] Application images are managed by ArgoCD Image Updater
- [ ] No images without tags (implicit `:latest`)
- [ ] Kyverno policy is enforcing tag requirements
- [ ] Image digests are being recorded in deployments

---

## Related Documentation

- [GitOps Configuration](./GITOPS.md)
- [ArgoCD Image Updater](https://argocd-image-updater.readthedocs.io/)
- [Kyverno Policies](../infrastructure/KYVERNO_POLICIES.md)
