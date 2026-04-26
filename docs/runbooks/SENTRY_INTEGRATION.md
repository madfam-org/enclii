# Sentry Observability Integration Runbook

**Last Updated:** April 2026
**Audit Reference:** Parity audit gap #9 (April 2026)
**Endpoint:** `GET /v1/observability/sentry?service=<uuid>&hours=24`
**Sentry Org:** [innovaciones-madfam-sas-de-cv.sentry.io](https://innovaciones-madfam-sas-de-cv.sentry.io/)
**Code:**
- Backend: `apps/switchyard-api/internal/integrations/sentry/`, `apps/switchyard-api/internal/api/sentry_handlers.go`
- UI: `apps/switchyard-ui/components/dashboard/sentry-error-badge.tsx`, `apps/switchyard-ui/hooks/use-sentry-stats.ts`
- Migration: `apps/switchyard-api/internal/db/migrations/019_sentry_project_slug.{up,down}.sql`

---

## Overview

Switchyard exposes a thin admin-only proxy to Sentry's REST API so the dashboard can surface error counts per service. The integration is **dormant until the operator provisions a Sentry auth token** — until then:

- Backend: `GET /v1/observability/sentry` returns `503 {"configured": false, "reason": "sentry_unconfigured"}`.
- UI: `<SentryErrorBadge />` renders nothing.

This runbook documents the one-time enable steps and the per-service override path.

---

## Step 1 — Generate a Sentry Auth Token

1. Sign in as a MADFAM-org Sentry admin.
2. Navigate to **Settings → Account → API → Auth Tokens**.
3. Click **Create New Token**.
4. **Name:** `enclii-switchyard-api` (one token per consumer; do not reuse personal tokens).
5. **Required scopes** (minimum, principle of least privilege):
   - `org:read`
   - `project:read`
   - `event:read`
6. Copy the token immediately — Sentry shows it only once.

**Audit note:** the token grants read access to *every* project in the org. There is currently no per-project token in Sentry's auth model. If we later need narrower scoping, the option is to create an integration token via Sentry Internal Integrations.

---

## Step 2 — Patch the `enclii-secrets` Kubernetes Secret

The switchyard-api Deployment reads `SENTRY_AUTH_TOKEN` and `SENTRY_ORG_SLUG` from the existing `enclii-secrets` secret (no separate secret needed).

```bash
kubectl patch secret enclii-secrets -n enclii \
  --type=merge \
  -p "$(cat <<'JSON'
{
  "stringData": {
    "SENTRY_AUTH_TOKEN": "PASTE_TOKEN_HERE",
    "SENTRY_ORG_SLUG": "innovaciones-madfam-sas-de-cv"
  }
}
JSON
)"
```

Replace `PASTE_TOKEN_HERE` with the value from Step 1. Do **not** commit the value to git — `enclii-secrets` is managed via Vault + ExternalSecrets in production; for an interim manual patch, follow up by writing the value into Vault at `secret/enclii/SENTRY_AUTH_TOKEN` so the next ExternalSecret reconcile preserves it.

### Restart the pod

The switchyard-api Deployment carries the Stakater Reloader annotation in production, so a secret patch triggers an automatic rolling restart within ~30s. To force-restart manually:

```bash
kubectl rollout restart deploy/switchyard-api -n enclii
kubectl rollout status deploy/switchyard-api -n enclii --timeout=2m
```

---

## Step 3 — Verify the Endpoint

Once the pod has restarted, `IsConfigured()` flips to true. From your laptop with a valid admin JWT:

```bash
SERVICE_ID=$(curl -s -H "Authorization: Bearer $JWT" \
  https://api.enclii.dev/v1/projects/switchyard/services \
  | jq -r '.[0].id')

curl -s -H "Authorization: Bearer $JWT" \
  "https://api.enclii.dev/v1/observability/sentry?service=$SERVICE_ID&hours=24" \
  | jq
```

**Expected (happy path):**
```json
{
  "configured": true,
  "service_id": "...",
  "sentry_project_slug": "switchyard-api",
  "stats_period": "24h",
  "error_count": 0,
  "issue_count": 0,
  "org_slug": "innovaciones-madfam-sas-de-cv",
  "fetched_at": "2026-04-26T..."
}
```

**Expected (Sentry project doesn't exist for this slug):**
```json
{
  "configured": true,
  "reason": "no_sentry_project",
  "service_id": "...",
  "sentry_project_slug": "<service.name>",
  "error_count": null,
  ...
}
```

---

## Step 4 — Per-Service Slug Override (Optional)

Most Enclii services share their `name` with their Sentry project slug (e.g. `switchyard-api` ↔ `switchyard-api`). When they diverge — typically because of a legacy rename — set the override column:

```bash
# Find the service ID
kubectl exec -n enclii deploy/switchyard-api -- \
  psql "$DATABASE_URL" -c \
  "SELECT id, name, sentry_project_slug FROM services WHERE name = 'my-service';"

# Set the override
kubectl exec -n enclii deploy/switchyard-api -- \
  psql "$DATABASE_URL" -c \
  "UPDATE services SET sentry_project_slug = 'legacy-sentry-name' WHERE name = 'my-service';"
```

The handler picks up the new value on the next request after the 60s in-memory cache expires (or immediately on a different `hours=` query string).

To revert to the default (use `service.name`):

```sql
UPDATE services SET sentry_project_slug = NULL WHERE name = 'my-service';
```

---

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---|---|---|
| Endpoint returns 503 with `reason=sentry_unconfigured` | `SENTRY_AUTH_TOKEN` or `SENTRY_ORG_SLUG` missing in the pod env | Verify `kubectl exec deploy/switchyard-api -- env \| grep SENTRY`. If unset, redo Step 2. |
| Endpoint returns 502 with `reason=sentry_unauthorized` | Token rotated, expired, or scopes too narrow | Generate a new token (Step 1) ensuring all three scopes are checked. Patch the secret. |
| Endpoint returns 200 with `reason=no_sentry_project` | Sentry has no project matching the resolved slug | Either create the Sentry project with that slug, or set `services.sentry_project_slug` to point at the existing project (Step 4). |
| Endpoint returns 502 with `reason=sentry_rate_limited` | Sentry returned 429 | Wait — the 60s in-memory cache will absorb the next minute of polls. If sustained, reduce dashboard concurrency or contact Sentry support. |
| UI badge invisible after enabling | Browser cache + 60s API cache | Hard reload (Cmd-Shift-R). The badge polls every 60s, so first appearance is up to ~60s after token provisioning. |
| `SENTRY_AUTH_TOKEN` echoed in logs | **Should not happen** — the client masks it. If observed, file a security incident immediately. |

---

## Architecture Notes

- The Sentry SDK is **not** initialised inside switchyard-api itself. This integration is purely a read-only proxy for the dashboard. Capturing switchyard-api's own errors into Sentry is a separate concern tracked elsewhere.
- The control plane is single-replica, so the in-memory 60s cache (`apps/switchyard-api/internal/api/sentry_handlers.go`) is sufficient. If/when we go multi-replica, move the cache into Redis under the same TTL.
- Per-call upstream timeout is 5s. The HTTP client itself has a 5s timeout as a backstop. Cumulative worst case is therefore <10s before a clean 502 is returned.
- Token never appears in error responses or logs — `client.go` explicitly masks both. The `sentry: unexpected status N` fallback truncates upstream bodies to 512 bytes to avoid leaking header fragments.

---

## Disable / Rollback

To turn the integration off without redeploying:

```bash
kubectl patch secret enclii-secrets -n enclii \
  --type=json \
  -p='[{"op": "remove", "path": "/data/SENTRY_AUTH_TOKEN"}]'
kubectl rollout restart deploy/switchyard-api -n enclii
```

The endpoint reverts to `503 {"configured": false}` and the UI badge disappears. No code change required.

To roll back the migration (rare — only if the column is causing downstream tooling issues):

```bash
kubectl exec -n enclii deploy/switchyard-api -- \
  psql "$DATABASE_URL" -f \
  /app/migrations/019_sentry_project_slug.down.sql
```
