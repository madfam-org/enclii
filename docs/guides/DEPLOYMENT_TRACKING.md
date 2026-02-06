# Deployment Lifecycle Tracking

Enclii tracks every step in the deployment pipeline — from git push to healthy production pods — as a chain of **lifecycle events**. This gives you a single timeline view of what happened, when, and why.

## How It Works

```
Git push → push_received → build_started → build_succeeded → image_pushed
         → digest_committed → deploy_started → deploy_synced → deploy_healthy
```

Each event is stored in the `deployment_lifecycle_events` table and linked to existing records (deployments, releases, CI runs) via optional foreign keys.

### Event Types

| Event | Description | Source |
|-------|-------------|--------|
| `push_received` | Git push webhook processed | `github_webhook` |
| `build_started` | Container build kicked off | `ci_callback` |
| `build_succeeded` | Image built and pushed | `ci_callback` |
| `build_failed` | Build failed | `ci_callback` |
| `image_pushed` | Image available in GHCR | `ci_callback` |
| `digest_committed` | Digest written to kustomization.yaml | `ci_callback` |
| `deploy_started` | ArgoCD sync initiated | `argocd_callback` |
| `deploy_synced` | ArgoCD reports Synced | `argocd_callback` |
| `deploy_healthy` | Pods running and healthy | `argocd_callback` |
| `deploy_degraded` | Deployment degraded | `argocd_callback` |
| `deploy_failed` | Deployment failed | `argocd_callback` |
| `preview_created` | Preview environment created | `ci_callback` |
| `preview_destroyed` | Preview environment destroyed | `ci_callback` |

### Sources

| Source | Description |
|--------|-------------|
| `github_webhook` | GitHub push/PR events processed by Switchyard |
| `ci_callback` | CI workflows calling the lifecycle callback API |
| `argocd_callback` | ArgoCD sync status updates |
| `manual` | Manual events via admin API |

### Branch-to-Environment Mapping

Events are automatically tagged with a `target_env` derived from the branch name:

| Branch Pattern | Environment |
|----------------|-------------|
| `main` | `production` |
| `staging`, `staging/*` | `staging` |
| `feature/*`, `fix/*`, `feat/*` | `preview` |
| `dev`, `dev/*`, `develop` | `dev` |

## Callback API

External CI workflows (dhanam, janua, any repo) report events via:

```
POST https://api.enclii.dev/v1/callbacks/lifecycle-event
Authorization: Bearer <ENCLII_CALLBACK_TOKEN>
Content-Type: application/json
```

### Request Body

```json
{
  "repo_full_name": "madfam-org/dhanam",
  "commit_sha": "abc1234567890",
  "branch": "main",
  "ref": "refs/heads/main",
  "event_type": "image_pushed",
  "source": "ci_callback",
  "message": "dhanam-api build image_pushed",
  "metadata": {
    "image": "ghcr.io/madfam-org/dhanam/api",
    "digest": "sha256:abc123...",
    "workflow": "deploy-k8s",
    "service": "dhanam-api"
  }
}
```

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `repo_full_name` | string | GitHub `owner/repo` (e.g. `madfam-org/dhanam`) |
| `commit_sha` | string | Full git commit SHA |
| `branch` | string | Branch name (e.g. `main`, `feature/foo`) |
| `ref` | string | Full git ref (e.g. `refs/heads/main`) |
| `event_type` | string | One of the event types above |
| `source` | string | One of the sources above |

### Optional Fields

| Field | Type | Description |
|-------|------|-------------|
| `message` | string | Human-readable description |
| `metadata` | object | Arbitrary JSON (image, digest, workflow, etc.) |
| `target_env` | string | Override auto-derived environment |

### CI Workflow Example (GitHub Actions)

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
          "image": "ghcr.io/madfam-org/my-service",
          "digest": "${{ steps.build.outputs.digest }}",
          "workflow": "${{ github.workflow }}"
        }
      }'
```

## Query API

All query endpoints require OIDC JWT authentication.

### Timeline by Repository

```
GET /v1/lifecycle/timeline/{owner}/{repo}
```

Query parameters:
- `branch` — filter to specific branch (e.g. `main`)
- `env` — filter by target environment (e.g. `production`)
- `event_type` — filter by event type (e.g. `deploy_healthy`)
- `since` — ISO 8601 timestamp (e.g. `2026-02-01T00:00:00Z`)
- `limit` — max results (default 50, max 200)

Example:
```bash
curl -H "Authorization: Bearer $TOKEN" \
  "https://api.enclii.dev/v1/lifecycle/timeline/madfam-org/dhanam?branch=main&limit=20"
```

### Events by Branch

```
GET /v1/lifecycle/branch/{owner}/{repo}/{branch}
```

Returns all events for a specific branch, most recent first.

Example:
```bash
curl -H "Authorization: Bearer $TOKEN" \
  "https://api.enclii.dev/v1/lifecycle/branch/madfam-org/dhanam/feature/new-api"
```

### Events by Commit

```
GET /v1/lifecycle/commit/{sha}
```

Returns all events for a specific commit across all repos and branches.

### Recent Events (Global)

```
GET /v1/lifecycle/events
```

Query parameters: `env`, `event_type`, `since`, `limit`

## Database Schema

```sql
CREATE TABLE deployment_lifecycle_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id   UUID REFERENCES deployments(id),
    release_id      UUID REFERENCES releases(id),
    ci_run_id       UUID REFERENCES ci_runs(id),
    project_id      UUID REFERENCES projects(id),
    service_id      UUID REFERENCES services(id),
    repo_full_name  TEXT NOT NULL,
    commit_sha      TEXT NOT NULL,
    branch          TEXT NOT NULL,
    ref             TEXT NOT NULL,
    target_env      TEXT,
    event_type      TEXT NOT NULL,
    source          TEXT NOT NULL,
    message         TEXT,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Migration: `005_deployment_lifecycle.up.sql`

## Secret Configuration

The `ENCLII_CALLBACK_TOKEN` secret must be configured in each repo that reports lifecycle events:

```bash
# The token value is the ArgoCD webhook secret from the enclii-argocd-webhook K8s secret.
# Get the value:
ssh foundry-core "sudo kubectl get secret enclii-argocd-webhook -n enclii -o jsonpath='{.data.secret}' | base64 -d"

# Set in each repo:
gh secret set ENCLII_CALLBACK_TOKEN --repo madfam-org/dhanam --body "<token>"
gh secret set ENCLII_CALLBACK_TOKEN --repo madfam-org/janua --body "<token>"
gh secret set ENCLII_CALLBACK_TOKEN --repo madfam-org/enclii --body "<token>"
```

The ArgoCD Notifications webhook uses the same token value (stored in `argocd-notifications-secret` in the `argocd` namespace as `argocd-webhook-secret`).

## Key Files

| Purpose | Path |
|---------|------|
| Types & constants | `packages/sdk-go/pkg/types/deployment_lifecycle.go` |
| Event repository | `apps/switchyard-api/internal/db/lifecycle_event_repository.go` |
| Callback + query handlers | `apps/switchyard-api/internal/api/lifecycle_event_handlers.go` |
| DB migration | `apps/switchyard-api/internal/db/migrations/005_deployment_lifecycle.up.sql` |
| Push webhook (emits events) | `apps/switchyard-api/internal/api/webhook_push.go` |
| ArgoCD callback (emits events) | `apps/switchyard-api/internal/api/argocd_callbacks.go` |
| Build callback (emits events) | `apps/switchyard-api/internal/api/build_callbacks.go` |
