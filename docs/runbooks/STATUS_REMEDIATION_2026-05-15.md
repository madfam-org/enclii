# MADFAM status remediation runbook

Date: 2026-05-15

## Current verified baseline

`status.madfam.io` reports `5 of 68` affected services:

- `Forgesight App` at `https://app.forgesight.quest` returns HTTP 502.
- `PhyneCRM App` at `https://app.phyne.app` has no public DNS answer.
- `Tulana` at `https://tulana.madfam.io` fetch fails.
- `Tulana App` at `https://tulana-app.madfam.io` fetch fails.
- `Tulana API` at `https://tulana-api.madfam.io/api/v1/health/` fetch fails.

## Remediation decisions

- Use Enclii, GitOps, and service workflows first.
- Do not use direct `kubectl` unless Enclii operator paths cannot recover a production incident.
- Keep status probes red when DNS, routing, or semantic app behavior is missing.

## PhyndCRM / PhyneCRM

Completed:

- `crm.madfam.io` now redirects unauthenticated users to MADFAM Janua SSO.
- `phynd.app` serves the public landing with the canonical repository link.
- Enclii now has an `app.phyne.app` junction and the active Cloudflare tunnel route targets `phynd-crm-web`.

Blocked:

- Enclii has no Cloudflare zone for `phyne.app`.
- The Porkbun adapter is unconfigured.

Next step:

- Bring `phyne.app` under Enclii DNS authority, then apply the `app.phyne.app` record.

## Forgesight App

Observed through Enclii:

- `forgesight-app` has one pod and zero ready pods.
- The app pod is Running with zero restarts.
- `forgesight-www`, `forgesight-api`, and `forgesight-admin` are healthy.

Remediation staged in the ForgeSight repo:

- Add `GET /api/health` to the app.
- Move app startup/readiness/liveness probes from `/` to `/api/health`.
- Add unit coverage for the app-local health route.
- Align `apps/app/.enclii.yml` with the Git source metadata required by Enclii deploys.

Deployment blocker:

- Enclii manual deploys still fail before image build with `Roundhouse enqueue failed` because the persisted service record is missing `GitRepo`.
- The local service spec is corrected, but Enclii `services-sync` currently treats existing services as already registered and does not update the persisted build source.

Next step:

- Update the persisted Enclii service build metadata for `forgesight-app`, or deploy through the repository workflow after committing/pushing the ForgeSight patch.

## Tulana

Observed through Enclii:

- No `tulana` project exists in live Enclii inventory.
- The Tulana repo contains `enclii.yaml` and production manifests for `tulana-api` and `tulana-web`.
- No `.env` file is present locally for onboarding.

Onboarding dry-run:

- `enclii onboard --repo madfam-org/tulana --project tulana --manifest-path infra/k8s/production --skip-postgres --skip-secrets --skip-r2 --dry-run` plans project, Argo, namespace, GHCR, and domain provisioning.

Blocked:

- Real onboarding requires a Tulana database password and `tulana-secrets` material.

Next step:

- Provide or create approved secret material via the Selva/Enclii secret workflow, then run full Enclii onboarding with Postgres and secrets enabled.
