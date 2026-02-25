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
| `deploy_synced` | ArgoCD sync in progress (health unknown) | `argocd_callback` |
| `deploy_healthy` | ArgoCD reports Synced (primary deploy success signal) | `argocd_callback` |
| `deploy_degraded` | ArgoCD reports Synced but health Degraded | `argocd_callback` |
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

## Pipeline Visibility (UI)

The **Deployments** page at `app.enclii.dev/deployments` shows two views:

1. **Pipeline Activity** — A real-time feed of lifecycle events from all repos, fetched from `GET /v1/lifecycle/events?limit=20`. Each event is color-coded by type (green for success, red for failure, blue for in-progress). This provides immediate feedback after a `git push` — events appear within seconds, long before ArgoCD syncs.

2. **Deployment History** — The traditional deployment records, now enriched with `deploying` and `failed` states from CI events:

### Deployment Status Flow

```
CI reports image_pushed  → Deployment created as "deploying"
ArgoCD syncs             → Deployment updated to "running" (healthy)
                         → Stale "deploying" records cleaned up as "cancelled"

CI reports build_failed  → Deployment created as "failed"
```

Valid deployment statuses: `pending`, `deploying`, `running`, `failed`, `cancelled`.

The `LifecycleEventCallback` handler creates Deployment records automatically for `image_pushed` and `build_failed` events. When ArgoCD later syncs, the `ArgocdSyncCallback` finds the existing `deploying` record and updates it to `running` instead of creating a duplicate.

**Race guard (CI → ArgoCD):** The lifecycle handler runs async (goroutine). If ArgoCD syncs before the goroutine completes, ArgoCD creates a `running` record directly. The goroutine then checks whether a `running` deployment already exists for the service within the last 5 minutes — if so, it skips creating the `deploying` record entirely. This prevents orphaned "deploying" records from the race condition.

**SHA mismatch fallback:** The CI `commit-digests` job creates a new commit (B) after the original push (A). CI lifecycle events use SHA=A, but ArgoCD syncs with SHA=B. If `FindDeployingByServiceAndSHA` doesn't find a match, the callback falls back to `FindRecentDeployingByService` — finding any `deploying` deployment for the service within a 30-minute window, regardless of SHA. This prevents "stuck deploying" records.

**Stale deploying cleanup (server-side):** After each ArgoCD sync processes a service, `CleanupStaleDeploying` marks any `deploying` records older than 30 minutes as `cancelled`. This catches orphaned records from race conditions where the CI goroutine ran after ArgoCD already synced. The `cancelled` status is a terminal state — these records appear in Deployment History (not Active Deployments).

**Deduplication:** The ArgoCD callback also checks whether the service's latest deployment already points to the same Release (same git SHA + image). If so, it skips creating a new record. This prevents deployment record explosion when ArgoCD syncs an Application containing multiple images but only some actually changed.

**Release enrichment:** The ArgoCD callback enriches Releases with git metadata (commit message, author, branch, repo URL) from lifecycle events — both when creating new Releases AND when finding existing ones with empty metadata fields. This handles the race condition where CI creates the release first (without commit metadata) and ArgoCD later finds it. The `"actor"` metadata key is also accepted as a fallback for `"author"` since some CI workflows use GitHub's `actor` field. **Event source preference:** `enrichReleaseFields` prefers CI/webhook events (`ci_callback`, `github_webhook`) over ArgoCD events when populating metadata. ArgoCD status messages like "ArgoCD sync Synced: ..." are never stored as `commit_message` — only genuine CI commit messages are used.

**Stale deploying filter (UI):** The Active Deployments card is always visible on the deployments page. It filters out `deploying`/`pending` records older than 30 minutes — these are shown in Deployment History instead, rendered as "Timed Out" badges. Records marked `cancelled` by server-side cleanup also appear in history as "Cancelled". When no active deployments exist, the card shows a green "No active deployments" idle state.

### Service Name Resolution

When a lifecycle event or ArgoCD callback arrives with an image URI like `ghcr.io/madfam-org/tezca/api`, Enclii needs to match it to a registered service. The `extractServiceCandidates` function generates candidate names:

| Image URI | Candidates (tried in order) |
|-----------|-----------------------------|
| `ghcr.io/madfam-org/tezca/api` | `tezca-api`, `api` |
| `ghcr.io/madfam-org/dhanam/admin` | `dhanam-admin`, `admin` |
| `ghcr.io/madfam-org/enclii/switchyard-api` | `enclii-switchyard-api`, `switchyard-api` |

For nested GHCR paths (3+ segments after the registry), the prefixed form (`{project}-{service}`) is tried first. This ensures services registered as `tezca-api` in the DB are correctly matched.

**Git repo URL fallback:** If no candidate name matches a registered service, both callbacks fall back to `ListByGitRepo()` — deriving the GitHub repo URL from the image URI (e.g. `ghcr.io/madfam-org/yantra4d/backend` → `https://github.com/madfam-org/yantra4d`) or from the lifecycle event's `repo_full_name` field. This handles mono-service repos like Yantra4D where the DB service is named `"yantra4d"` but image-derived candidates are `["yantra4d-backend", "backend"]`.

The `metadata.service` field in CI callbacks provides an explicit service name. In the lifecycle handler, this explicit name is always preserved as the first candidate — image-derived candidates are appended (deduped) rather than overwriting the explicit name. This ensures CI can control service matching by setting `metadata.service` to the exact DB name. ArgoCD lifecycle events also include the resolved `service` name in their metadata. When neither `metadata.service` is set, the Pipeline Activity UI extracts the service name from `metadata.image` (last path segment without tag/digest) — e.g. `ghcr.io/madfam-org/enclii/switchyard-api:sha-abc` → "switchyard-api". The full fallback chain is: `metadata.service` → image-derived candidates → git repo URL → repo name.

## Key Files

| Purpose | Path |
|---------|------|
| Types & constants | `packages/sdk-go/pkg/types/deployment_lifecycle.go` |
| Deployment types (status constants) | `packages/sdk-go/pkg/types/types.go` |
| Event repository | `apps/switchyard-api/internal/db/lifecycle_event_repository.go` |
| Deployment repository | `apps/switchyard-api/internal/db/deployment_repository.go` |
| Callback + query handlers | `apps/switchyard-api/internal/api/lifecycle_event_handlers.go` |
| ArgoCD callback (emits events + updates deployments) | `apps/switchyard-api/internal/api/argocd_callbacks.go` |
| Service name extraction tests | `apps/switchyard-api/internal/api/argocd_callbacks_test.go` |
| DB migration | `apps/switchyard-api/internal/db/migrations/005_deployment_lifecycle.up.sql` |
| Push webhook (emits events) | `apps/switchyard-api/internal/api/webhook_push.go` |
| Build callback (emits events) | `apps/switchyard-api/internal/api/build_callbacks.go` |
| Deployments UI page | `apps/switchyard-ui/app/(protected)/deployments/page.tsx` |
