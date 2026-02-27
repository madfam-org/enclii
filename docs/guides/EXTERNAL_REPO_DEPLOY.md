# External Repository Deployment Guide

How to set up auto-deploy and deployment tracking for repositories outside the Enclii monorepo (e.g. client repos, external services).

## Architecture

```
External Repo                    Enclii Platform                  Kubernetes
┌──────────┐                     ┌──────────────┐                ┌──────────┐
│ git push │──webhook──────────→ │ push_received│                │          │
│ CI build │──callback─────────→ │ image_pushed │                │          │
│ digest   │──git commit───────→ │ kustomize    │──ArgoCD sync─→ │ deploy   │
│ commit   │                     │ digest_committed│              │ healthy  │
└──────────┘                     └──────────────┘                └──────────┘
```

The external repo's CI pipeline does the heavy lifting (build + push + digest commit), while Enclii provides event tracking and ArgoCD handles the actual deployment.

## Required Setup

### 1. enclii.yaml

Create an `enclii.yaml` in your repository root. See [service-spec.md](../reference/service-spec.md) for the full schema.

```yaml
version: "2"
project: my-project
services:
  - name: my-api
    type: web
    dockerfile: ./Dockerfile
    port: 8080
    domains:
      - api.myproject.com
```

### 2. GitHub Secrets

Configure these secrets in your GitHub repository settings:

| Secret | Description | Required |
|--------|-------------|----------|
| `MADFAM_BOT_PAT` | GHCR push token (long-lived PAT) | Yes |
| `ENCLII_CALLBACK_TOKEN` | Bearer token for lifecycle callbacks | Yes |
| `KUBECONFIG_PRODUCTION` | Base64-encoded kubeconfig (optional) | For direct deploy |

### 3. K8s Manifests with Kustomize

Your repo needs kustomize-managed K8s manifests:

```
my-repo/
  k8s/production/
    kustomization.yaml
    deployment.yaml
    service.yaml
```

**kustomization.yaml:**
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - deployment.yaml
  - service.yaml
images:
  - name: my-service
    newName: ghcr.io/madfam-org/my-project/my-service
```

**deployment.yaml** uses short image names:
```yaml
spec:
  template:
    spec:
      containers:
        - name: my-service
          image: my-service  # Kustomize transforms this to full GHCR path + digest
```

### 4. CI Workflow Pattern

Here's the complete CI workflow pattern:

```yaml
name: Deploy to K8s (GHCR)

on:
  push:
    branches: [main]
    paths:
      - 'apps/my-service/**'
      - 'packages/**'
      - '!k8s/production/kustomization.yaml'
  workflow_dispatch: {}

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: madfam-org/my-project/my-service
  SERVICE_SHORT_NAME: my-service

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      packages: write
    outputs:
      image_digest: ${{ steps.build.outputs.digest }}

    steps:
      - uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITHUB_TOKEN }}

      - uses: docker/setup-buildx-action@v3

      - uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: madfam-bot
          password: ${{ secrets.MADFAM_BOT_PAT }}

      - id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=sha,prefix=
            type=raw,value=main,enable={{is_default_branch}}

      - id: build
        uses: docker/build-push-action@v6
        with:
          context: .
          file: ./apps/my-service/Dockerfile
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
          provenance: false
          sbom: false

      - name: Commit digest to kustomization.yaml
        if: github.ref == 'refs/heads/main'
        run: |
          DIGEST="${{ steps.build.outputs.digest }}"
          echo "Updating kustomization.yaml with digest: $DIGEST"

          curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash
          sudo mv kustomize /usr/local/bin/

          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"

          # Retry loop: handles concurrent pushes from parallel deploy workflows
          for ATTEMPT in 1 2 3; do
            echo "Push attempt $ATTEMPT/3"
            git fetch origin main
            git reset --hard origin/main

            cd k8s/production
            kustomize edit set image ${{ env.SERVICE_SHORT_NAME }}=${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}@${DIGEST}
            cd ${{ github.workspace }}

            git add k8s/production/kustomization.yaml
            git diff --staged --quiet && { echo "No changes to commit"; exit 0; }
            git commit -m "chore(deploy): update image digest to ${DIGEST:0:19}"

            if git push origin main; then
              echo "Push succeeded on attempt $ATTEMPT"
              exit 0
            fi
            echo "Push failed (likely concurrent update), retrying..."
            sleep $((ATTEMPT * 2))
          done
          echo "ERROR: Failed to push after 3 attempts"
          exit 1

      - name: Report lifecycle event
        if: always()
        continue-on-error: true
        run: |
          EVENT_TYPE="image_pushed"
          if [ "${{ steps.build.outcome }}" != "success" ]; then
            EVENT_TYPE="build_failed"
          fi

          curl -sf -X POST "https://api.enclii.dev/v1/callbacks/lifecycle-event" \
            -H "Authorization: Bearer ${{ secrets.ENCLII_CALLBACK_TOKEN }}" \
            -H "Content-Type: application/json" \
            -d '{
              "repo_full_name": "${{ github.repository }}",
              "commit_sha": "${{ github.sha }}",
              "branch": "${{ github.ref_name }}",
              "ref": "${{ github.ref }}",
              "event_type": "'"$EVENT_TYPE"'",
              "source": "ci_callback",
              "message": "Build '"$EVENT_TYPE"'",
              "metadata": {
                "image": "${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}",
                "digest": "${{ steps.build.outputs.digest }}",
                "workflow": "${{ github.workflow }}",
                "service": "${{ env.SERVICE_SHORT_NAME }}"
              }
            }'
```

### 5. ArgoCD Application

Create an ArgoCD Application in the enclii repo at `infra/argocd/apps/my-project.yaml`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-project
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io
spec:
  project: default
  source:
    repoURL: https://github.com/madfam-org/my-project.git
    targetRevision: main
    path: k8s/production
  destination:
    server: https://kubernetes.default.svc
    namespace: my-project
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

## How Auto-Deploy Works

1. **Push to main** triggers CI workflow
2. **CI builds** Docker image, pushes to GHCR with SHA tag + `:main` tag
3. **CI commits** image digest to `kustomization.yaml` via `kustomize edit set image`
4. **CI reports** `image_pushed` lifecycle event to Enclii API
5. **ArgoCD detects** the kustomization.yaml change (git poll every 3 minutes)
6. **ArgoCD syncs** — applies the new image digest to the K8s deployment
7. **K8s rolls** out the new pods
8. **ArgoCD reports** sync status via callback → Enclii records `deploy_healthy` (skips services whose digest didn't change; enriches Releases with commit metadata from lifecycle events; transitions `deploying` → `running` even when the ArgoCD sync SHA differs from the original CI push SHA)

## Preventing CI Loops

The digest commit to `kustomization.yaml` would re-trigger CI. Use `!` negation in `paths` to exclude it (GitHub Actions does not allow `paths` and `paths-ignore` together):

```yaml
paths:
  - 'apps/my-service/**'
  - '!k8s/production/kustomization.yaml'   # negation pattern
```

Alternatively, if using only `paths-ignore` (no `paths` filter), you can exclude broadly:
```yaml
paths-ignore:
  - 'k8s/**'                                # broad exclusion pattern
```

## GHCR Image Naming

Use nested GHCR naming that auto-links to the GitHub repo:

```
ghcr.io/madfam-org/{repo}/{service}
```

Examples:
- `ghcr.io/myorg/myapp/api`
- `ghcr.io/myorg/myapp/admin`
- `ghcr.io/myorg/myapp/web`
- `ghcr.io/myorg/myapp-api` (flat naming)

### Service Name Resolution

Enclii resolves GHCR image paths to registered service names using a candidate strategy. For nested paths like `ghcr.io/myorg/myapp/api`, it tries `myapp-api` first, then `api`. This means your DB service names should use the `{project}-{service}` pattern (e.g. `myapp-api`, `myapp-admin`) for reliable matching.

**Git repo URL fallback:** If no candidate name matches, both the lifecycle and ArgoCD callbacks fall back to looking up services by git repo URL — derived from the image URI (e.g. `ghcr.io/myorg/myapp/backend` → `https://github.com/myorg/myapp`). This handles mono-service repos where the DB service name (e.g. `"myapp"`) doesn't match any image-derived candidate.

The `metadata.service` field in lifecycle event callbacks provides an explicit override — set it to the exact service name registered in Enclii (e.g. `"service": "myapp-api"`).

## Disabling Provenance Attestations

Always set `provenance: false` and `sbom: false` in `docker/build-push-action`:

```yaml
- uses: docker/build-push-action@v6
  with:
    provenance: false
    sbom: false
```

Without this, GHCR creates attestation manifests alongside images. ArgoCD Image Updater can pick up attestation SHAs instead of image SHAs, causing 403 errors on pull.

## Example Repo Layout

### Multi-Service Repo

- **Repo**: `myorg/myapp`
- **Services**: myapp-api, myapp-admin, myapp-web
- **Manifests**: `infra/k8s/production/` or `k8s/production/`
- **Workflows**: One per service (`deploy-api.yml`, `deploy-admin.yml`, `deploy-web.yml`) or consolidated with change detection
- **ArgoCD app**: `infra/argocd/apps/myapp.yaml` (in the enclii repo)

### Single-Service Repo

- **Repo**: `myorg/my-service`
- **Services**: my-service
- **Manifests**: `k8s/production/`
- **Workflow**: `deploy-k8s.yml`
- **ArgoCD app**: `infra/argocd/apps/my-service.yaml` (in the enclii repo)

## Troubleshooting

| Issue | Cause | Fix |
|-------|-------|-----|
| CI loop on kustomization commit | Missing path negation | Add `!kustomization.yaml` to `paths` filter |
| ArgoCD not syncing | Git poll interval (3min) | Wait or force-sync via kubectl |
| GHCR 403 on digest pull | Attestation manifest SHA | Set `provenance: false` in build-push-action |
| Build succeeds but no deploy | Missing digest commit step | Add kustomize edit + git push step |
| Lifecycle events not appearing | Missing/wrong callback token | Check `ENCLII_CALLBACK_TOKEN` secret |
| Concurrent digest commit fails | Multiple workflows push same file | Use fetch/reset/re-apply retry loop (see CI workflow pattern above) |
| New image not pulled by K8s | `imagePullPolicy: IfNotPresent` with tag | Set `imagePullPolicy: Always` or use digest refs |
| Lifecycle events exist but no deployment record | Service name in DB doesn't match image path | Ensure DB service name follows `{project}-{service}` pattern (e.g. `myapp-api`), or set explicit `metadata.service` in CI callback |
| Duplicate deployment records on ArgoCD sync | ArgoCD syncs all images in the Application, not just changed ones | Fixed: callback now skips services whose latest deployment already has the same Release |
| External repo deployments lack git metadata | ArgoCD callback creates bare Releases (`argocd-{sha}`) | Fixed: callback enriches both new AND existing Releases with metadata from lifecycle events (commit message, author/actor, branch) |
| Active deployments stuck "Deploying" forever | CI commit SHA (A) differs from ArgoCD sync SHA (B) due to digest-commit creating a new commit; also race condition where CI goroutine runs after ArgoCD sync | Fixed: (1) callback falls back to time-window lookup (30 min) when SHA match fails, (2) lifecycle handler skips deploying record if ArgoCD already created a running deployment within 5 min, (3) ArgoCD callback cleans up stale deploying records (>30 min → `cancelled`), (4) UI shows stale records as "Timed Out" in history |
| Pipeline Activity shows repo name instead of service | ArgoCD lifecycle events lacked `service` key in metadata | Fixed: ArgoCD events now include `"service": serviceName` in metadata. UI also extracts service name from `metadata.image` (last path segment) as fallback |
| Commit message shows ArgoCD status text | `enrichReleaseFields` used the most recent event regardless of source | Fixed: CI/webhook events are preferred over ArgoCD events. ArgoCD status messages are never stored as `commit_message` |
