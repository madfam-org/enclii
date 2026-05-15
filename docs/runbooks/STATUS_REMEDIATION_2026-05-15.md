# MADFAM status remediation runbook

Date: 2026-05-15

## Current verified baseline

Live `status.madfam.io` currently reports `1 of 60` affected services:

- `Routecraft API` at `https://api.routecraft.app` returns HTTP 502.

The corrected source configuration targets the expanded 68-service inventory, but
that projection has not been promoted to the public page yet. Once promoted, the
known non-green entries to re-check are:

- `Forgesight App` at `https://app.forgesight.quest` returns HTTP 502.
- `PhyneCRM App` at `https://app.phyne.app` has no public DNS answer.
- `Tulana` at `https://tulana.madfam.io` fetch fails.
- `Tulana App` at `https://tulana-app.madfam.io` fetch fails.
- `Tulana API` at `https://tulana-api.madfam.io/api/v1/health/` fetch fails.

Status regenerate safety note:

- `enclii admin status regenerate --force` was run on May 15, 2026 and returned an unsafe projection: `total_count=16`, with only `11` MADFAM services.
- The generated commits were immediately reverted on `main`:
  - `e4db5cdd` reverts the 11-service MADFAM configmap.
  - `bee8f2da` reverts the 5-service Enclii configmap.
- The Switchyard regenerate handler now has a pre-commit count guard so it refuses projections below the safety floors or below the checked-in configmap count.

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

Deployment blocker cleared in local Enclii tooling:

- Enclii manual deploys failed before image build with `Roundhouse enqueue failed` because the persisted service record was missing `GitRepo`.
- `services-sync --reconcile-existing` now supports opt-in repair of existing service metadata from checked-in specs, including `git_repo`, `app_path`, auto-deploy fields, and `build_config`.

2026-05-15 live execution:

- `enclii services-sync --dir apps/app --project forgesight --dry-run --reconcile-existing` showed only `git_repo` and `build_config` drift.
- `enclii services-sync --dir apps/app --project forgesight --reconcile-existing` updated the existing `forgesight-app` service record successfully.
- `enclii deploy -f apps/app/.enclii.yml -e prod -w` progressed past the previous `GitRepo required` failure and created release `v20260515-050358-7b23968`.
- The release remained `building` beyond the CLI's 10-minute wait window.
- The active app pods still fail because `forgesight-secrets` is missing.
- `ExternalSecret/forgesight-secrets` is `Ready=False` with `SecretSyncedError` and `could not get secret data from provider`.

Next step:

- Populate the backing provider path `secret/forgesight` through Selva RFC 0005 or an Enclii-owned Vault writer, refresh `forgesight-secrets` through Enclii, then retry the `forgesight-app` deployment and market-data jobs.

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
