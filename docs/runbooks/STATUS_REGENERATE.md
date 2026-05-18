# Status Configmap Regeneration Runbook

_Last updated: 2026-05-18 — onboarding now stores status entries in the DB snapshot; runtime ConfigMap projection is the default zero-touch path._

## Why this exists

The two status pages (`status.enclii.dev`, `status.madfam.io`) are driven by
Kubernetes ConfigMaps:

- `apps/status/k8s/enclii/configmap.yaml` (small platform-only set)
- `apps/status/k8s/madfam/configmap.yaml` (~60 services across the ecosystem)

Historically, every product onboarding required a human to hand-edit the
madfam configmap. That violates the zero-touch onboarding policy in
[CLAUDE.md § NetworkPolicy Architecture](../../CLAUDE.md) (the same principle
applies to status entries). The `POST /v1/admin/status/regenerate` endpoint
makes the configmaps a pure projection of:

1. A small **platform core** set baked into the API (`coreEncliiServicesFor*Site()` in
   `apps/switchyard-api/internal/api/status_handlers.go`).
2. **Per-project `enclii.yaml`** `status.entries[]` blocks captured into each
   onboarding record's `config_snapshot.status_entries` field.

After a successful regenerate, each ConfigMap is byte-identical to its
function-of-source equivalent. Projection mode is controlled by
`ENCLII_STATUS_PROJECTION_MODE`:

- `runtime` (default): update `status-config-enclii` and `status-config-madfam`
  directly in Kubernetes from DB/core state; Stakater Reloader restarts the
  status pods without requiring an Enclii repo commit. ArgoCD ignores only the
  runtime-owned `services-config` key for these two ConfigMaps so self-heal does
  not revert the projected catalog.
- `gitops` (legacy break-glass): commit regenerated files to this repo; ArgoCD
  applies the commit and Stakater Reloader restarts the status pods. This mode
  requires `ENCLII_ALLOW_LEGACY_GITOPS_STATUS_PROJECTION=true`.

## When to run

Run regenerate after:

- A new project is onboarded with `status.entries[]` in its `enclii.yaml`.
- An existing project edits its `status.entries[]` (renames, adds, removes).
- A platform-core entry is added/removed in `coreEncliiServicesForMadfamSite()`.
- Suspicion that the live configmap drifted from source.

A weekly cron (recommended deferred work, not yet wired) can call this
endpoint as a self-healing safety net. Until that lands, regenerate is an
operator action.

## Pre-conditions

| Requirement | Where to check |
|---|---|
| `ENCLII_STATUS_PROJECTION_MODE` set | `runtime` by default; `gitops` is legacy break-glass only |
| `ENCLII_ALLOW_LEGACY_GITOPS_STATUS_PROJECTION` | required only when deliberately using legacy `gitops` mode |
| `GITHUB_TOKEN` set in switchyard-api env | required only in `gitops` mode |
| `EncliiRepoOwner` configured | required only in `gitops` mode; typically `madfam-org` |
| `EncliiRepoName` configured | required only in `gitops` mode; typically `enclii` |
| Runtime ConfigMap RBAC present | required only in `runtime` mode; see `switchyard-status-config-manager` |
| Caller has `admin` JWT role | issued by Janua; verify with `enclii auth verify` |
| ArgoCD self-heal enabled for the `status-*` apps | required for `gitops`; `kubectl get application status-madfam -n argocd -o jsonpath='{.spec.syncPolicy.automated.selfHeal}'` |

## Run the regenerate

Prefer the Enclii operator CLI:

```bash
enclii admin status regenerate --force
```

The command calls `POST /v1/admin/status/regenerate` with the active Enclii
operator credentials and prints the API response as JSON.

If the CLI is unavailable, use the API directly:

```bash
JWT=$(enclii auth token)            # or pull from ~/.enclii/auth.json

curl -sS -X POST \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  https://api.enclii.dev/v1/admin/status/regenerate | jq .
```

Successful no-op response (configmaps already match source):

```json
{
  "status": "no_changes",
  "projection_mode": "runtime",
  "targets": {
    "enclii": { "service_count": 5,  "changed": false, "action": "unchanged", "configmap": "enclii/status-config-enclii" },
    "madfam": { "service_count": 62, "changed": false, "action": "unchanged", "configmap": "enclii/status-config-madfam" }
  },
  "total_count": 67
}
```

Successful with a real diff:

```json
{
  "status": "regenerated",
  "projection_mode": "gitops",
  "targets": {
    "enclii": { "service_count": 5,  "changed": false, "action": "unchanged", "configmap": "enclii/status-config-enclii" },
    "madfam": {
      "service_count": 63,
      "changed": true,
      "action": "committed",
      "configmap": "enclii/status-config-madfam",
      "commit_sha": "abc123def456..."
    }
  },
  "total_count": 68
}
```

In `gitops` mode the commit lands directly on `main` (the kustomization's
tracked branch). In `runtime` mode no Enclii repo commit is made; the API
returns `action: "created"` or `action: "updated"` for changed targets.

## Guardrails

The API refuses unsafe shrink projections before committing any generated
configmap. Current floors:

- `status.enclii.dev`: at least 5 services.
- `status.madfam.io`: at least 60 services.

The API also refuses to project if the generated service count is lower than
the existing configmap count. If a genuine catalog reduction is needed, land an
explicit source-of-truth migration first and document why the public inventory
is being reduced.

Before accepting a regeneration, compare the returned service counts with the
current public status surface:

```bash
curl -sS https://status.madfam.io/api/status | jq '.services | length'
curl -sS https://status.enclii.dev/api/status | jq '.services | length'
```

If the regenerated count is materially lower than live coverage, revert the
generated commit and fix the aggregation source before rerunning. This happened
on 2026-05-14 when the endpoint projected only onboarded `enclii.yaml` entries
and would have reduced MADFAM coverage from the broader curated status surface.
Status truthfulness prefers stale-but-broad coverage over silently dropping
checks.

## Verify

1. **Projection landed.** In `gitops` mode, open the `commit_sha` from the
   response and confirm it changes only `apps/status/k8s/{enclii,madfam}/configmap.yaml`.
   In `runtime` mode, inspect the returned ConfigMap:
   ```bash
   kubectl -n enclii get configmap status-config-madfam -o jsonpath='{.data.services-config}' | jq length
   ```
2. **ArgoCD syncs.** Required after `gitops` mode, optional confirmation after
   runtime mode because the ConfigMap is already live.
   ```bash
   kubectl -n argocd get application status-madfam status-enclii \
     -o custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status
   ```
   Expect both `Synced / Healthy` within ~60 seconds.
3. **Reloader restarts the pod.** Watch for a new pod:
   ```bash
   kubectl -n enclii get pods -l app=status-madfam --sort-by=.metadata.creationTimestamp -w
   ```
4. **Status page reflects new entries.**
   ```bash
   curl -sS https://status.madfam.io/api/status | jq '.services | length'
   curl -sS https://status.madfam.io/api/status | jq '.services[] | select(.service == "<your new service>")'
   ```

## Rollback

The regenerate output is a pure function of source — to undo a regeneration
that committed an unwanted entry:

1. **Fix the source first.** Either remove/correct `status.entries[]` in the
   ecosystem repo's `enclii.yaml`, or remove the entry from
   `coreEncliiServicesForMadfamSite()` in this repo. Failing to fix source
   means the next regenerate will re-commit the unwanted change.
2. **Revert the auto-PR / commit.**
   ```bash
   gh pr revert <num>            # if regenerate started using a PR-based path
   # or
   git revert <commit_sha> && git push origin main
   ```
3. Run regenerate again to confirm bytes match the new source-of-truth.

## Troubleshooting

**`503: GitHub token or enclii repo not configured`**
The switchyard-api deployment is missing `ENCLII_GITHUB_TOKEN`,
`ENCLII_ENCLII_REPO_OWNER`, or `ENCLII_ENCLII_REPO_NAME`. Patch the
`enclii-secrets` Secret and the deployment env, or switch
`ENCLII_STATUS_PROJECTION_MODE=runtime`.

**`503: Kubernetes client not configured for runtime status projection`**
The API is in `runtime` projection mode but switchyard-api does not have a
usable in-cluster Kubernetes client. Confirm the pod is running in-cluster and
that `switchyard-api` has the `switchyard-status-config-manager` RoleBinding.

**`500: failed to read existing <site> configmap`**
The configmap file does not exist on `main` yet. This happens only on a
fresh repo. Check the path with the GitHub UI; if intentional, drop a
minimal skeleton via PR (the handler will then take over).

**`500: failed to commit <site> configmap`**
The token lacks write permission to the enclii repo, or branch protection
is blocking direct pushes to `main`. Check the GitHub App / PAT scopes.

**Regenerate keeps committing every call (no idempotency).**
Indicates `generateStatusConfigmap` round-trip changed format. Run
`go test ./apps/switchyard-api/internal/api/ -run TestStatusHandler_GenerateIsIdempotent`
to reproduce and pin the regression. If the test still passes locally but
the live endpoint still commits repeatedly, inspect whether the runtime source
inventory differs from local fixtures.

**Provider DNS apply returns `adapter_required`.**
Use `enclii providers cloudflare dns <hostname> --json` to prove the current
Cloudflare state. If a production hostname is down and `dns-apply --apply`
returns `501 adapter_required`, this is an Enclii adapter gap. Use direct
provider access only as break-glass, keep the mutation narrow, and record the
reason in the provider comment plus the relevant remediation issue/commit.
prod commits on every call, diff the live configmap byte-for-byte against
the regenerator output — a manual edit is most likely the culprit.

**Status page is missing my new entry after regenerate.**
1. Confirm the entry is on `main`: `git show main:apps/status/k8s/madfam/configmap.yaml | grep <name>`.
2. Confirm ArgoCD is `Synced` (it sometimes lags 30 - 60s).
3. Confirm the status pod restarted after the configmap change. If
   `kubectl describe deploy status-madfam -n enclii | grep reloader`
   doesn't show `reloader.stakater.com/last-reloaded-from`, Reloader didn't
   fire — see `CONFIG_RELOAD_RUNBOOK.md`.

## Related

- Source of truth: `apps/switchyard-api/internal/api/status_handlers.go`
- Tests: `apps/switchyard-api/internal/api/status_handlers_test.go`
- Reloader pipeline: `docs/runbooks/CONFIG_RELOAD_RUNBOOK.md`
- Onboarding: `docs/guides/ONBOARDING_GUIDE.md`
