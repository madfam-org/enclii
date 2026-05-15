# Status Configmap Regeneration Runbook

_Last updated: 2026-05-14 — added Enclii CLI-first regeneration path._

## Why this exists

The two status pages (`status.enclii.dev`, `status.madfam.io`) are driven by
configmaps committed to this repo:

- `apps/status/k8s/enclii/configmap.yaml` (small platform-only set)
- `apps/status/k8s/madfam/configmap.yaml` (~60 services across the ecosystem)

Historically, every product onboarding required a human to hand-edit the
madfam configmap. That violates the zero-touch onboarding policy in
[CLAUDE.md § NetworkPolicy Architecture](../../CLAUDE.md) (the same principle
applies to status entries). The `POST /v1/admin/status/regenerate` endpoint
makes the configmaps a pure projection of:

1. A small **platform core** set baked into the API (`coreEncliiServicesFor*Site()` in
   `apps/switchyard-api/internal/api/status_handlers.go`).
2. **Per-project `enclii.yaml`** `status.entries[]` blocks aggregated from
   every onboarded project.

After a successful regenerate, the configmap files are byte-identical to
their function-of-source equivalents. ArgoCD picks up the commit, Stakater
Reloader restarts the status pods, and the new entries appear on the page.

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
| `GITHUB_TOKEN` set in switchyard-api env | `kubectl get deploy switchyard-api -n enclii -o yaml \| grep -i github` |
| `EncliiRepoOwner` configured | typically `madfam-org`; see `apps/switchyard-api/internal/config/config.go` |
| `EncliiRepoName` configured | typically `enclii` |
| Caller has `admin` JWT role | issued by Janua; verify with `enclii auth verify` |
| ArgoCD self-heal enabled for the `status-*` apps | `kubectl get application status-madfam -n argocd -o jsonpath='{.spec.syncPolicy.automated.selfHeal}'` |

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
  "targets": {
    "enclii": { "service_count": 5,  "changed": false },
    "madfam": { "service_count": 62, "changed": false }
  },
  "total_count": 67
}
```

Successful with a real diff:

```json
{
  "status": "regenerated",
  "targets": {
    "enclii": { "service_count": 5,  "changed": false },
    "madfam": {
      "service_count": 63,
      "changed": true,
      "commit_sha": "abc123def456..."
    }
  },
  "total_count": 68
}
```

The commit lands directly on `main` (the kustomization's tracked branch).

## Guardrails

The API refuses unsafe shrink projections before committing any generated
configmap. Current floors:

- `status.enclii.dev`: at least 5 services.
- `status.madfam.io`: at least 60 services.

The API also refuses to commit if the generated service count is lower than the
checked-in configmap count. If a genuine catalog reduction is needed, land an
explicit source-of-truth migration first and document why the public inventory is
being reduced.

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

1. **GitHub commit appears.** Open the `commit_sha` from the response —
   should show only `apps/status/k8s/{enclii,madfam}/configmap.yaml` modified.
2. **ArgoCD syncs.**
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
   curl -sS https://status.madfam.io/api/services | jq '.services | length'
   curl -sS https://status.madfam.io/api/services | jq '.services[] | select(.name == "<your new service>")'
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
`enclii-secrets` Secret and the deployment env.

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
