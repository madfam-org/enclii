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

## Requirements

Projects onboarded to Enclii **MUST** meet the following requirements:

1. **Health endpoint**: Every service MUST expose a health endpoint (e.g., `/health` returning HTTP 200) for status monitoring. Services without health endpoints will show as "degraded" on the status page.

2. **No NetworkPolicy resources**: Project K8s manifests MUST NOT include `kind: NetworkPolicy` resources. NetworkPolicies are centrally managed by enclii via the `network` section in `enclii.yaml`. Including them causes ArgoCD resource ownership conflicts. The preflight check (`POST /v1/admin/onboard/preflight`) will warn about this.

3. **Network and status declarations**: Projects SHOULD declare `network` and `status` sections in `enclii.yaml` for auto-provisioned NetworkPolicies and status page registration.

## Step 1: Create `enclii.yaml`

Add an `enclii.yaml` to your repository root:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: my-project
  project: my-project
spec:
  runtime:
    port: 8080
  domains:
    - name: api.example.com
      environment: production
    - name: app.example.com
      environment: production
      port: 3000
  network:
    services:
      - name: my-api
        label: app
        port: 8080
        ingress: [cloudflare-tunnel]
        egress: [dns, https, postgres, redis]
      - name: my-web
        label: app
        port: 3000
        ingress: [cloudflare-tunnel]
        egress: [dns, https]
  status:
    entries:
      - name: api.example.com
        url: https://api.example.com/health
        group: My Project
      - name: app.example.com
        url: https://app.example.com
        group: My Project
```

### Network Section Reference

The `network.services` section auto-generates Kubernetes NetworkPolicies during onboarding:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `name` | Yes | - | Pod app label value |
| `label` | No | `app` | Label key for pod selector |
| `port` | Yes | - | Container port for ingress |
| `ingress` | No | `[]` | Ingress sources: `cloudflare-tunnel` |
| `egress` | No | `[]` | Egress types: `dns`, `https`, `http`, `postgres`, `redis`, `janua`, `pgbouncer` |

For intra-namespace communication (e.g., nginx proxy → backend), use `network.custom`:

```yaml
  network:
    custom:
      - name: landing-to-backend
        from: { app.kubernetes.io/name: my-landing }
        to: { app.kubernetes.io/name: my-backend }
        port: 5000
        direction: both
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
  --secret-name my-project-secrets \
  --db-name my_project \
  --db-password "$(openssl rand -base64 32)" \
  --secrets-file ./my-project.env \
  --r2-bucket my-project-uploads

# Preflight validation (checks manifests against cluster admission policies)
enclii onboard --repo madfam-org/my-project --project my-project --preflight

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
    "secret_name": "my-project-secrets",
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

The onboarding pipeline executes a multi-step provisioning workflow:
1. Fetches and validates `enclii.yaml` from the repo
2. Creates project and service records in the Enclii DB
3. Creates service records from `enclii.yaml` metadata
4. **Validates manifest path** — checks the directory exists in the repo and contains YAML files
5. Registers ArgoCD desired state. The current production implementation still
   uses a legacy Enclii repo `config.json` write; the target zero-touch path is
   runtime ArgoCD reconciliation from the client repo declaration. Operators can
   opt into the runtime path with `ENCLII_ARGOCD_REGISTRATION_MODE=runtime` once
   ArgoCD RBAC is confirmed.
6. Refuses new app-specific Enclii catalog entries outside the legacy allowlist
   enforced by `scripts/check-zero-touch-boundaries.sh`
7. Creates K8s namespace with labels (`enclii.dev/data-access=true`, `enclii.dev/type=application`), **default-deny NetworkPolicy** (DNS-only egress), and copies GHCR credentials. These labels auto-grant access to shared data services (PostgreSQL, Redis, PgBouncer) and Janua SSO — no manual NetworkPolicy edits needed.
8. Provisions custom domains (Cloudflare tunnel routes + DNS CNAMEs)
9. Registers onboarding in DB, including `status.entries[]` for later status
   ConfigMap projection without editing the Enclii repo
10. Creates Postgres database + role, updates PgBouncer *(if requested)* — PgBouncer userlist is bootstrapped automatically if absent
11. Creates K8s Secret (name configurable via `secret_name`, default: `<project>-credentials`) from `.env` entries *(if requested)*
12. Creates R2 bucket + appends R2 credentials to K8s Secret *(if requested)*

**Status reporting**: The response includes `step_results` and an overall `status`:
- `completed` — all steps succeeded
- `partial` — non-critical steps failed (domain provisioning, secrets, R2, postgres)
- `failed` — a critical step failed (namespace creation or ArgoCD config commit)

### Preflight Validation

Before onboarding, you can validate manifests against the cluster's admission policies (Kyverno):

```bash
# Via CLI
enclii onboard --repo madfam-org/my-project --project my-project --preflight

# Via API
curl -X POST "https://api.enclii.dev/v1/admin/onboard/preflight" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo_full_name": "madfam-org/my-project", "project_name": "my-project"}'
```

This fetches YAML manifests from the repo, runs server-side dry-run against the cluster, and reports any violations (unqualified images, missing security context, etc.).

### Preventive Image Hygiene Gates (auto-run on every onboard)

Every call to `POST /v1/admin/onboard` runs two additional gates BEFORE ArgoCD Application creation. Either one returning a blocker aborts onboarding with HTTP 400 — there is no bypass flag. Both gates are also available side-effect-free via `GET /v1/admin/preflight?repo=owner/name`.

**The kustomize `images:` transformer is honoured before either gate runs.** Both gates judge the *effective* image — the value `kustomize build` would render — not the raw string in the Deployment. That is required by the house convention documented in Step 4A below: deployment YAML carries a bare image name and CI writes the digest into `kustomization.yaml` with `kustomize edit set image`, so the digest stays a reviewable one-line diff. See "Kustomize image resolution" below for exactly what is resolved.

**1. Image digest pinning** (`gate: image-digest-pinned`)

Rejects onboarding if any workload manifest in `manifest_path` would deploy a container image that is not pinned by `@sha256:` digest. Blocks `:latest`, mutable tags like `:v1.2.3` / `:main`, images with no tag at all, and the all-zero placeholder digest (`@sha256:0000…`, the house convention for "CI has not pinned this yet"). This mirrors the cluster-side Kyverno `require-image-digest` policy, but catches the problem at onboarding instead of at first admission. Response shape:

```json
{
  "error": "image must be digest-pinned (@sha256:...)",
  "gate": "image-digest-pinned",
  "result": { "digest_issues": [{"file": "web-deployment.yaml", "kind": "Deployment",
    "name": "web", "container": "web", "image": "ghcr.io/madfam-org/foo/web:v1.2.3",
    "manifest_image": "web", "source": "kustomization",
    "kustomize_entry": "web", "kustomization_file": "kustomization.yaml",
    "message": "image ... uses a mutable tag ... (resolved from manifest image \"web\" via kustomization.yaml images[] entry \"web\")",
    "severity": "blocker"}],
    "resolution": {"ran": true, "manifests_scanned": 4, "kustomization_found": true,
      "kustomization_file": "kustomization.yaml", "kustomize_entries": 2,
      "workload_images": 3, "resolved_by_kustomize": 3,
      "summary": "scanned 4 manifest file(s); kustomization.yaml with 2 images[] entries; 3 workload image(s), 3 resolved through a kustomization override"} }
}
```

`image` is always the value that was judged; `manifest_image` + `source` + `kustomize_entry` tell you where it came from, so you never have to diff the Deployment against the kustomization by hand.

Fix: pin the digest. The supported way is the CI pin step in Step 4A — `kustomize edit set image name=image@sha256:…` writing into `kustomization.yaml`. Hardcoding a digest into the Deployment also passes, but do not do it: the CI pin step then has nothing to update and the deployed digest silently stops tracking `main`. If this is a greenfield repo, run your CI build once so it commits the first real digest.

**2. GHCR image existence** (`gate: image-exists`)

Rejects onboarding if any `ghcr.io/<org>/<package>` image referenced by the manifests has not been pushed to GHCR yet (no package versions exist). Non-GHCR images (`docker.io/...`, `registry.k8s.io/...`, `nvcr.io/...`) are ignored. This catches the exact failure mode that produced "six services silently 502 for 4+ days" — status-page targets registered before CI ever built a first image. Response shape:

```json
{
  "error": "no image has been pushed to GHCR yet; run CI to build and push first image before enclii onboarding",
  "gate": "image-exists",
  "result": { "missing_packages": [{"image": "ghcr.io/...", "org": "madfam-org",
    "package": "avala/avala-web", "message": "no images pushed yet for ..."}] }
}
```

Fix: trigger CI on the repo's `main` branch to build and push the image to GHCR, confirm the package appears in https://github.com/orgs/madfam-org/packages, then re-run onboarding.

**3. Kustomize images transformer** (`gate: kustomize-images`)

Not a policy gate — a fail-closed guard. If the directory's kustomization cannot be interpreted, the gates cannot know what would deploy, so onboarding is rejected rather than passed. Triggers: unparseable YAML; an `images[]` entry with no `name`; an entry setting both `digest` and `newTag` (kustomize treats them as mutually exclusive); a `digest` carrying its own `@`; a `newName` that embeds a tag or digest; two entries with the same `name`; two kustomization files in one directory. The message names the file and the offending `images[<i>]` index.

#### Kustomize image resolution

The API has no checkout and does not shell out to `kustomize`. It implements the `images:` transformer — and only that transformer — directly:

| Field | Effect |
| --- | --- |
| `name` | Matches a workload image whose reference is `name`, `name:<tag>` or `name@<digest>`. Prefix match, so `web` does **not** match `web-api` or `ghcr.io/org/web`. |
| `newName` | Replaces the name; any existing tag or digest is preserved. |
| `newTag` | Replaces the tag. Still fails gate 1 — a tag is not a digest. |
| `digest` | Replaces the tag with `@<digest>`. This is what the CI pin step writes. |

Recognised file names: `kustomization.yaml`, `kustomization.yml`, `Kustomization`. Out of scope: everything else in a kustomization (`resources`, `patches`, generators, name prefixes) and **parent overlays** — the gate only sees the single directory named by `manifest_path`, so a digest supplied by a `resources: [../base]` parent is not resolved and its image is reported as unpinned. Point `manifest_path` at the overlay that carries the `images:` block (the one your CI pin step edits).

**Read-proof**: every gate run reports a `resolution` block — on pass as well as failure — and logs the same counts server-side (`Image gates resolved manifest images`). `kustomization_found: false` means no kustomization was present; `kustomization_found: true` with `kustomize_entries: 0` means one was present but declared no image overrides. Those two states used to be indistinguishable from a clean pass. A preflight that reports `manifests_scanned: 0` checked nothing — treat it as a failure of the check, not a pass of the repo.

**Transient failures**: If the gate cannot reach GitHub or GHCR, onboarding returns HTTP 503 with `"detail"` populated — we do not silently pass because "we couldn't check". Retry when upstream is healthy.

**No bypass**: These gates have no `--skip` flag and accept no request field to turn them off. If a legitimate exception exists (e.g. a tenant using a non-GHCR registry), the gate should be extended to ignore that registry explicitly — file an issue rather than adding a bypass.

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
    digest: sha256:…          # written by the CI pin step (Step 4A)
```

**Deployment manifests** should use short image names (not full GHCR paths):
```yaml
containers:
  - name: my-service
    image: my-service    # Kustomize transforms this
```

The onboarding digest gate resolves this transformer, so the bare `image: my-service` above is fine — it judges `ghcr.io/madfam-org/my-project/my-service@sha256:…`. Until CI has run once there is no `digest:` line and the gate correctly rejects onboarding: an untagged, unpinned image is a service that cannot start. Run the build first.

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
| Preflight validation | `apps/switchyard-api/internal/api/preflight.go` |
| GitHub directory listing | `apps/switchyard-api/internal/api/github_file_writer.go` |
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
