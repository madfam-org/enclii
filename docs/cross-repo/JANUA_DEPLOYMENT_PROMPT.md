# Janua Deployment Hardening — Agent Prompt

Use this prompt in the `janua` repository to bring Janua's CI/CD and K8s manifests in line with Enclii's Kyverno policies and credential standards.

---

## Context

Enclii's production cluster enforces:
- **Kyverno `require-run-as-nonroot`** policy — pods must set `runAsNonRoot: true`
- **Kyverno `disallow-capabilities`** policy — containers must drop ALL capabilities
- **Standardized GHCR pull secret** named `ghcr-credentials` (type `docker-registry`) in every namespace
- **madfam-bot** as the service account for CI pushes (fine-grained PAT, `read:packages` + `write:packages`, scoped to `madfam-org`)

## Tasks

### 1. CI/CD: Push images using madfam-bot

In `.github/workflows/build.yml` (or equivalent):

```yaml
- name: Login to GHCR
  uses: docker/login-action@v3
  with:
    registry: ghcr.io
    username: madfam-bot
    password: ${{ secrets.MADFAM_BOT_PAT }}
```

Ensure the image tag follows: `ghcr.io/madfam-org/janua-api:main-<short-sha>`

Add `MADFAM_BOT_PAT` as a repository secret (or org-level secret).

### 2. Dockerfile: Non-root user

Ensure the Dockerfile creates and switches to a non-root user:

```dockerfile
RUN addgroup -g 1000 appgroup && adduser -u 1000 -G appgroup -D appuser
USER appuser
```

This is required for Kyverno's `runAsNonRoot` enforcement.

### 3. K8s Deployment: securityContext

Add to ALL deployment manifests:

```yaml
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 1000
        runAsGroup: 1000
        fsGroup: 1000
      containers:
        - name: janua-api
          securityContext:
            privileged: false
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
```

### 4. K8s Deployment: imagePullSecrets

Ensure all deployments reference the standardized secret name:

```yaml
spec:
  template:
    spec:
      imagePullSecrets:
        - name: ghcr-credentials
```

### 5. FIELD_ENCRYPTION_KEY

Verify that `FIELD_ENCRYPTION_KEY` is:
1. Present in the `janua-secrets` K8s secret
2. Referenced in the deployment's env section

### 6. ArgoCD Ecosystem Services (automated deployment)

Janua is managed by the `ecosystem-services` ArgoCD Application (`infra/argocd/apps/ecosystem-services.yaml` in enclii repo). Once Janua's CI pushes a Kyverno-compliant image to `ghcr.io/madfam-org/janua-api:latest`, ArgoCD Image Updater will automatically detect the new digest and deploy it — no manual intervention required.

**Requirements for automatic deployment:**
1. Image must pass Kyverno policies (non-root, capabilities dropped)
2. Image pushed to GHCR via `madfam-bot` PAT
3. K8s manifests at a path ArgoCD can reach (already configured)

### 7. GitHub webhook for Enclii-managed builds (future)

Add a webhook to the janua repo:
- URL: `https://api.enclii.dev/v1/webhooks/github`
- Content type: `application/json`
- Secret: (get from `kubectl get secret enclii-github-webhook -n enclii -o jsonpath='{.data.secret}' | base64 -d`)
- Events: Push events only

This enables Enclii's Roundhouse to build and deploy Janua automatically.
