# MADFAM Projects Remediation - 2026-06-02

Scope: `https://app.enclii.dev/projects`, MADFAM project slice.

Latest refreshed snapshot: `2026-06-03T02:01:43Z`.

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

Actions in progress:

- Enclii card matcher now prefers non-staging Argo evidence for non-staging projects before severity tie-breaking.
- Enclii reconcile response now includes `failed` and `errors` so silent reconcile failures are visible.
- Selva staging overlay now sets `nexus-api` replicas to `1`, matching the staging HPA.

Remaining to green:

- Push/deploy Enclii main so the project card matcher takes effect.
- Push Selva main so Argo sees the staging replica fix.
- Re-run/inspect Selva service reconcile after the Enclii reconcile diagnostics deploy.

## Verification Run

- Enclii: `go test ./internal/api -run 'TestMatchProjectCardArgoEvidence|TestReconcileServicesResponseRecordsFailures' -count=1`
- Tulana: `python -m pytest apps/api/tests/integrations/test_health.py -q`
- Selva: `kubectl kustomize infra/k8s/overlays/staging`
- Live cards: `/private/tmp/enclii-project-cards-after-tulana-fix.json`

## Selva Cutover Closeout - 2026-06-03

Live Selva state is now remediated.

Actions completed:

- Renamed Selva source, GitOps applications, namespaces, image references, Cloudflare tunnel routes, local inventories, and local state filenames to the Selva naming set.
- Pushed Selva Office fixes: `e4f8126`, `77d5456`, and `6321fda`.
- Pushed Enclii project-source fixes: `84af093b`, `f4d508dc`, and status image build fix `94356008`.
- Cleared the production Selva PVC deletion hang by pausing the worker scaler, draining `workers`, letting the memory PVC recreate, then restoring the worker replica.

Verification:

- `selva-services`: Synced/Healthy.
- `selva-office-staging`: Synced/Healthy.
- Production Selva deployments: all ready.
- `https://api.selva.town/api/v1/health/health`: healthy.
- Workspace hidden and visible naming scan: no legacy Selva naming references or filenames found outside ignored dependency/build directories.
- Enclii workflows for `94356008`: Public hygiene, Production Readiness Ratchet, Integration Tests, and CI Pipeline all succeeded.

Remaining follow-up:

- Move the temporary admin auth Kubernetes Secret dependency into the intended writable Vault-backed path once the write workflow is available.
