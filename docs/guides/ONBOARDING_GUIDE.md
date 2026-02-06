# Repository Onboarding Guide

How to add a new repository to the Enclii platform for auto-deploy, deployment tracking, and domain provisioning.

## Prerequisites

- GitHub repository under `madfam-org` (or with webhook access)
- `ENCLII_CALLBACK_TOKEN` secret configured in the repo's GitHub Actions
- `MADFAM_BOT_PAT` secret for GHCR image push

## Step 1: Create `enclii.yaml`

Add an `enclii.yaml` to your repository root:

```yaml
version: "2"
project: my-project
services:
  - name: my-api
    type: web
    dockerfile: ./apps/api/Dockerfile
    port: 8080
    health_check: /health
    domains:
      - api.example.com
  - name: my-web
    type: web
    dockerfile: ./apps/web/Dockerfile
    port: 3000
    health_check: /
    domains:
      - app.example.com
environments:
  production:
    branch: main
    auto_deploy: true
```

## Step 2: Call the Onboarding API

```bash
curl -X POST "https://api.enclii.dev/v1/admin/onboard" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repo_full_name": "madfam-org/my-project",
    "branch": "main"
  }'
```

The onboarding endpoint:
1. Fetches and validates `enclii.yaml` from the repo
2. Creates project and service records in the Enclii DB
3. Sets up a GitHub webhook for push events
4. Generates an ArgoCD Application YAML
5. Provisions custom domains (Cloudflare tunnel routes + DNS CNAMEs)

### Response

```json
{
  "registration": {
    "id": "uuid",
    "project_id": "uuid",
    "repo_full_name": "madfam-org/my-project",
    "webhook_id": 12345,
    "argocd_app_name": "my-project",
    "onboard_status": "completed"
  },
  "argocd_yaml": "apiVersion: argoproj.io/v1alpha1\nkind: Application\n...",
  "next_steps": [
    "Commit the ArgoCD Application YAML to infra/argocd/apps/my-project.yaml",
    "Add ENCLII_CALLBACK_TOKEN secret to your GitHub repo",
    "Add lifecycle event callback to your CI workflows"
  ]
}
```

## Step 3: Commit the ArgoCD Application

Take the `argocd_yaml` from the response and commit it:

```bash
# In the enclii repo
cat > infra/argocd/apps/my-project.yaml << 'EOF'
# Paste the argocd_yaml content here
EOF

git add infra/argocd/apps/my-project.yaml
git commit -m "feat(argocd): add my-project application"
git push
```

ArgoCD will pick up the new application on its next sync cycle.

## Step 4: Set Up CI Auto-Deploy

Your CI workflow needs two additions:

### A. Commit image digest to kustomization.yaml

After building and pushing the Docker image, commit the digest:

```yaml
- name: Commit digest to kustomization.yaml
  if: github.ref == 'refs/heads/main'
  run: |
    DIGEST="${{ steps.build.outputs.digest }}"
    curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash
    sudo mv kustomize /usr/local/bin/

    cd path/to/k8s/production
    kustomize edit set image my-service=ghcr.io/madfam-org/my-project/my-service@${DIGEST}

    cd ${{ github.workspace }}
    git config user.name "github-actions[bot]"
    git config user.email "github-actions[bot]@users.noreply.github.com"
    git add path/to/k8s/production/kustomization.yaml
    git diff --staged --quiet || git commit -m "chore(deploy): update image digest to ${DIGEST:0:19}"
    git push
```

### B. Report lifecycle events

```yaml
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
          "image": "ghcr.io/madfam-org/my-project/my-service",
          "digest": "${{ steps.build.outputs.digest }}",
          "workflow": "${{ github.workflow }}"
        }
      }'
```

### C. Add paths-ignore to prevent CI loops

The digest commit will modify `kustomization.yaml`. Prevent it from re-triggering CI:

```yaml
on:
  push:
    branches: [main]
    paths:
      - 'apps/my-service/**'
    paths-ignore:
      - 'path/to/k8s/production/kustomization.yaml'
```

## Step 5: Create K8s Manifests

Set up kustomize-based manifests:

```
my-repo/
  k8s/
    production/
      kustomization.yaml      # Image transformer
      my-service-deployment.yaml
      my-service-service.yaml
```

**kustomization.yaml:**
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - my-service-deployment.yaml
  - my-service-service.yaml
images:
  - name: my-service          # Short name used in deployment
    newName: ghcr.io/madfam-org/my-project/my-service
```

**Deployment manifests** should use short image names (not full GHCR paths):
```yaml
containers:
  - name: my-service
    image: my-service    # Kustomize transforms this
```

## Step 6: Verify

1. Push a code change to `main`
2. Watch CI build and push the image
3. Check the lifecycle timeline:
   ```bash
   curl -H "Authorization: Bearer $TOKEN" \
     "https://api.enclii.dev/v1/lifecycle/timeline/madfam-org/my-project?branch=main"
   ```
4. Verify ArgoCD synced the new digest
5. Confirm the service is healthy at its public domain

## Checking Onboarding Status

```bash
# List all onboarded repos
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "https://api.enclii.dev/v1/admin/onboard"

# Check specific repo
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "https://api.enclii.dev/v1/admin/onboard/madfam-org/my-project"
```

## Key Files

| Purpose | Path |
|---------|------|
| Onboarding handlers | `apps/switchyard-api/internal/api/onboarding_handlers.go` |
| ArgoCD template generator | `apps/switchyard-api/internal/api/argocd_template.go` |
| Onboarding repository | `apps/switchyard-api/internal/db/onboarding_repository.go` |
| enclii.yaml parser | `apps/switchyard-api/internal/api/enclii_yaml.go` |
| Domain provisioner | `apps/switchyard-api/internal/api/domain_provisioner.go` |
