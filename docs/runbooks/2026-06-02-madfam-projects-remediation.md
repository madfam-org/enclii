# MADFAM Projects Remediation - 2026-06-02

Scope: `https://app.enclii.dev/projects`, MADFAM project slice.

Latest refreshed snapshot: `2026-06-03T02:01:43Z` (pre Selva rename/cutover).

## Live Status

| State | Count | Notes |
| --- | ---: | --- |
| healthy | 57 | Improved from 54 healthy at audit start. |
| failing | 2 | `selva`, `ceq`. |

`ceq` is intentionally not remediated in this pass because it is owned by another active agent.

## Remediated

### enclii

Root cause: `switchyard-ui` had a stuck ReplicaSet because `NEXT_PUBLIC_POSTHOG_KEY` referenced missing Secret key `enclii-secrets/posthog-key`.

Actions:

- Live: added empty `posthog-key` to `enclii-secrets` so the existing ReplicaSet could start.
- Durable: made `NEXT_PUBLIC_POSTHOG_KEY` optional in `infra/k8s/base/switchyard-ui.yaml`.
- Verification: `switchyard-ui` is `2/2` available and `core-services` is Synced/Healthy.

### tulana

Root cause: `tulana-adapter-health` and `tulana-pull-catalog` CronJob pods could not reach Dhanam because Dhanam ingress allowed only pods labeled `app=tulana-api`. The job pods use `app=tulana-adapter-health` and `app=tulana-pull-catalog`.

Actions:

- Live: added Tulana egress policy `allow-dhanam-api-egress`.
- Live: widened Dhanam out-of-band policy `dhanam-api-allow-tulana-fx` to allow the two Tulana job labels.
- Durable: added Tulana egress in `tulana/infra/k8s/production/network-policies.yaml`.
- Durable: added the Tulana job labels to Dhanam API ingress in `dhanam/infra/k8s/production/network-policies.yaml`.
- Durable: changed Tulana Dhanam adapter health probing from `/v1/health` to the live `/health` endpoint while retaining catalog fallback.

Verification:

- `tulana-adapter-health-manual-20260602-projects-rerun`: Complete; final probe reported all configured adapters healthy.
- `tulana-pull-catalog-manual-20260602-projects-rerun`: Complete; synced 26 products and 82 SKUs.
- Refreshed Enclii cards no longer list `tulana` as failing.

### forgesight

Initial state was transient Argo Progressing. Live Argo now reports `forgesight-services` Synced/Healthy without code changes.

## Still Open

### selva

Root causes:

- Dashboard truth bug: the project card matched production project `selva` to staging Argo app `selva-office-staging` because both matched the project prefix and the old tie-breaker preferred the worst equal-rank app.
- Staging GitOps drift: `selva-office-staging` declares `nexus-api` replicas as `2` while its HPA is pinned min/max `1`, causing Argo OutOfSync/Degraded churn.
- Service reconcile observability gap: `POST /v1/admin/projects/selva/reconcile-services` returned discovered deployments but no service rows and no failure details.

Actions completed:

- Enclii card matcher now prefers non-staging Argo evidence for non-staging projects before severity tie-breaking.
- Enclii reconcile response now includes `failed` and `errors` so silent reconcile failures are visible.
- Selva staging overlay now sets `nexus-api` replicas to `1`, matching the staging HPA.
- Selva source rename is pushed in `madfam-org/selva-office`:
  - `e4f8126` normalizes repository labels, code, manifests, image names, hostnames, and package names.
  - `77d5456` stabilizes runtime cutover by removing the active admin-auth ExternalSecret dependency until the Vault path exists and adding the required `COLYSEUS_SECRET` mapping.
  - `6321fda` fixes Cloudflare tunnel merge behavior so existing hostname rules update stale service targets instead of only adding missing rules.
- Enclii ApplicationSet source rename is pushed in `madfam-org/enclii` at `84af093b`; generated projects now include `selva-services` and no pre-cutover Selva project app.
- Live runtime:
  - `selva` and `selva-staging` namespaces are active.
  - Production and staging each have all six deployments available: `admin`, `colyseus`, `gateway`, `nexus-api`, `office-ui`, `workers`.
  - Pre-cutover production/staging namespaces are deleted.
  - Public prod/staging API health endpoints return `{"status":"healthy","version":"0.1.0","service":"nexus-api"}`.
- Labspace old-name scan returned zero content and path matches outside ignored generated/dependency directories.

Remaining to green:

- Refresh the Enclii project card snapshot after Enclii CI/deploy completes.
- `selva-services` is Synced and all child workloads are ready, but Argo still reports aggregate health `Progressing`; no child resource reports non-healthy status. Treat this as a remaining Argo health classification issue until the project card confirms Selva is no longer flagged.
- Migrate Selva admin auth to Vault path `secret/selva` when a writable Vault workflow is available. The active GitOps bundle intentionally relies on the already-provisioned Kubernetes auth Secret until that path exists.

## Verification Run

- Enclii: `go test ./internal/api -run 'TestMatchProjectCardArgoEvidence|TestReconcileServicesResponseRecordsFailures' -count=1`
- Tulana: `python -m pytest apps/api/tests/integrations/test_health.py -q`
- Selva: `kubectl kustomize infra/k8s/overlays/staging`
- Selva: `uv run ruff check .`
- Live cards: `/private/tmp/enclii-project-cards-after-tulana-fix.json`
