# Repository Onboarding Guide

How to add a new repository to the Enclii platform for auto-deploy, deployment tracking, and domain provisioning.

> **Zero-Touch Policy**: Onboarding a new app must NOT require modifying enclii, janua, or dhanam repos. All deployment configs live in the provisioned repo itself. See [ZERO_TOUCH_CONTRACT.md](./ZERO_TOUCH_CONTRACT.md) for the full contract.

## Prerequisites

- GitHub repository under `madfam-org` (or with webhook access)
- `ENCLII_CALLBACK_TOKEN` secret configured in the repo's GitHub Actions
- `MADFAM_BOT_PAT` secret for GHCR image push
- **Automation team access** — the `automation` team (which includes `madfam-bot`) must have `write` (push) access to the repo for CI digest commits. The org default is `read`, so new repos do NOT inherit write access automatically. Grant it with:
  ```bash
  gh api -X PUT "orgs/madfam-org/teams/automation/repos/madfam-org/<repo-name>" \
    -f permission=push
  ```

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

## Step 2: Onboard via CLI or API

### Option A: CLI (Recommended)

The `enclii onboard` command handles the complete provisioning pipeline:

```bash
# Basic onboarding
enclii onboard --repo madfam-org/my-project --project my-project

# Full provisioning with database, secrets, and R2 storage
enclii onboard --repo madfam-org/my-project \
  --project my-project \
  --manifest-path k8s/production \
  --db-name my_project \
  --db-password "$(openssl rand -base64 32)" \
  --secrets-file ./my-project.env \
  --r2-bucket my-project-uploads

# Preview what would be provisioned
enclii onboard --repo madfam-org/my-project --db-name my_project --dry-run
```

See [`docs/cli/commands/onboard.md`](../cli/commands/onboard.md) for all flags and examples.

### Option B: API

```bash
curl -X POST "https://api.enclii.dev/v1/admin/onboard" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "repo_full_name": "madfam-org/my-project",
    "project_name": "my-project",
    "manifest_path": "k8s/production",
    "branch": "main",
    "provision_postgres": {
      "database_name": "my_project",
      "role_password": "secure-password",
      "extensions": ["pgcrypto"]
    },
    "provision_secrets": [
      {"key": "JANUA_CLIENT_ID", "value": "jnc_abc123"},
      {"key": "DATABASE_URL", "value": "postgresql://my_project:pass@pgbouncer.data.svc.cluster.local:6432/my_project"}
    ],
    "provision_r2": {
      "bucket_name": "my-project-uploads"
    }
  }'
```

### What Happens

The onboarding pipeline executes 11 steps:
1. Fetches and validates `enclii.yaml` from the repo
2. Creates project and service records in the Enclii DB
3. Creates service records from `enclii.yaml` metadata
4. Generates ArgoCD `config.json` for the ApplicationSet
5. **Auto-commits** `config.json` to `infra/argocd/projects/<name>/` in the enclii repo (no manual step)
6. Creates K8s namespace with labels + copies GHCR credentials
7. Provisions custom domains (Cloudflare tunnel routes + DNS CNAMEs)
8. Registers onboarding in DB
9. Creates Postgres database + role, updates PgBouncer *(if requested)*
10. Creates K8s Secret from `.env` entries *(if requested)*
11. Creates R2 bucket + appends R2 credentials to K8s Secret *(if requested)*

Steps 9-11 are optional and independent — failure in one does not block others.

### Standalone Provisioning (Ad-Hoc)

For already-onboarded projects, use the standalone provision endpoints:

```bash
POST /v1/admin/provision/postgres   # Create DB + role + PgBouncer update
POST /v1/admin/provision/secrets    # Create K8s secret in namespace
POST /v1/admin/provision/r2         # Create R2 bucket
```

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

### C. Exclude kustomization.yaml to prevent CI loops

The digest commit will modify `kustomization.yaml`. Use `!` negation in `paths` to exclude it (GitHub Actions does not allow `paths` and `paths-ignore` together):

```yaml
on:
  push:
    branches: [main]
    paths:
      - 'apps/my-service/**'
      - '!path/to/k8s/production/kustomization.yaml'
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
| CLI onboard command | `packages/cli/internal/cmd/onboard.go` |
| Onboarding handlers | `apps/switchyard-api/internal/api/onboarding_handlers.go` |
| Provisioning handlers | `apps/switchyard-api/internal/api/provisioning_handlers.go` |
| Postgres provisioner | `apps/switchyard-api/internal/provisioning/postgres.go` |
| PgBouncer updater | `apps/switchyard-api/internal/provisioning/pgbouncer.go` |
| Secrets provisioner | `apps/switchyard-api/internal/provisioning/secrets.go` |
| R2 provisioner | `apps/switchyard-api/internal/provisioning/r2.go` |
| Input validation | `apps/switchyard-api/internal/provisioning/validate.go` |
| RBAC manifest | `infra/k8s/base/switchyard-rbac.yaml` |
| ArgoCD template generator | `apps/switchyard-api/internal/api/argocd_template.go` |
| Onboarding repository | `apps/switchyard-api/internal/db/onboarding_repository.go` |
| enclii.yaml parser | `apps/switchyard-api/internal/api/enclii_yaml.go` |
| Domain provisioner | `apps/switchyard-api/internal/api/domain_provisioner.go` |
