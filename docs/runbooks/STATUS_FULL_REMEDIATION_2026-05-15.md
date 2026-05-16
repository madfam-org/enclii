# MADFAM Status Full Remediation Plan

Date: 2026-05-15
Scope: `https://status.madfam.io`, Enclii-managed production surfaces, DNS authority, secret readiness, and release automation.

## Current live state

The live status API reports 68 monitored services with 5 affected services:

- Forgesight App: `https://app.forgesight.quest`, HTTP 502.
- PhyneCRM App: `https://app.phyne.app`, DNS/routing not yet truthful.
- Tulana: `https://tulana.madfam.io`, not yet provisioned as a truthful production surface.
- Tulana App: `https://tulana-app.madfam.io`, not yet provisioned as a truthful production surface.
- Tulana API: `https://tulana-api.madfam.io/api/v1/health/`, not yet provisioned as a truthful production surface.

Routecraft API is no longer in the affected set. Enclii pod logs show `Enclii-Status-Monitor/1.0` receiving HTTP 200 from `GET /health`.

## Evidence

- `status-madfam-services` is `Synced` and `Healthy`.
- `status-madfam-services` is deployed from Enclii revision `a70ed269cca0d9e3a93425f9226b075ebf60c929`.
- Current live status API shape uses `service`, not `name`, for each service label.
- `routecraft-services` is `Synced` and `Healthy`; Cloudflare DNS for `api.routecraft.app` is a no-op against the expected tunnel CNAME.
- `forgesight-services` is `Synced` but `Degraded`.
- `forgesight-secrets` is `Ready=False` with `SecretSyncedError`.
- `forgesight-app` pods are blocked by `CreateContainerConfigError` because Kubernetes cannot find `forgesight-secrets`.

## Non-negotiable operating policy

Routine production remediation must go through Enclii web, API, or CLI. Raw `kubectl`, provider CLIs/APIs, direct container access, or manual DNS mutations are break-glass only.

Secret material must not be printed, copied into chat, or fabricated. Secret writes must use the approved Selva/RFC 0005 path or a documented Enclii secret operation.

## Priority remediation

### P0: Restore Forgesight App

Blocker: `secret/forgesight` provider data is missing, so External Secrets cannot materialize `forgesight-secrets`.

Boot-critical required properties:

- `database_url`
- `redis_url`
- `secret_key`
- `r2_access_key_id`
- `r2_secret_access_key`

Optional integration credentials such as OIDC client credentials, scraper
vendor keys, and PostHog keys must not block core app startup. Track them as
separate service-specific ExternalSecrets when those integrations are enabled.

Plan:

1. Resolve the authoritative secret source without printing values.
2. Write the values to Vault path `secret/forgesight` through the approved Selva/RFC 0005 or Enclii secret-write path.
3. Refresh `forgesight-secrets` through `enclii ops secrets refresh forgesight-secrets -n forgesight --apply --reason "..."`
4. Confirm `forgesight-secrets` is `Ready=True`.
5. Confirm `forgesight-app` pods are ready.
6. Confirm `https://app.forgesight.quest` returns the expected app surface.
7. Confirm status API removes Forgesight App from affected services.

Do not create placeholder secrets. If a temporary direct Kubernetes Secret is used, treat it as break-glass and immediately backfill Vault so External Secrets becomes the durable source.

### P1: Restore PhyneCRM generic app host

Blocker: `app.phyne.app` is not under Enclii-owned DNS authority yet. Cloudflare cannot apply the record until the zone is owned/imported or the registrar delegates `phyne.app` to the Enclii-managed Cloudflare account. Enclii now exposes Porkbun DNS create and nameserver apply operations so registrar remediation can stay inside the Enclii audit path.

Plan:

1. Configure Switchyard API with `ENCLII_PORKBUN_API_KEY` and `ENCLII_PORKBUN_SECRET_API_KEY` through Enclii-managed secrets.
2. Run `enclii providers porkbun domains phyne.app --json` and `enclii providers porkbun nameservers phyne.app --json` to verify Enclii can read the registrar source of truth.
3. If `phyne.app` should delegate to Cloudflare, run `enclii providers porkbun nameservers-apply phyne.app --nameservers <cloudflare-ns-1>,<cloudflare-ns-2> --apply --reason "delegate phyne.app to Enclii-managed Cloudflare"`.
4. If delegation is not ready, run `enclii providers porkbun dns-apply app.phyne.app --domain phyne.app --type CNAME --content c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com --apply --reason "restore PhyneCRM app host through Enclii"`.
5. Apply `app.phyne.app` as the generic authenticated PhyneCRM app host.
6. Rerun `enclii providers cloudflare dns-apply app.phyne.app --json` after delegation/import; expected state is no longer `blocked_by_dns_authority`.
7. Assert final URL contains `app.phyne.app/login`.
8. Keep `crm.madfam.io` as the MADFAM tenant slice that immediately asks for Janua SSO login.

### P1: Provision Tulana production surfaces

Blocker: Tulana domains are in the status catalog but the production project/service set is not fully onboarded.

Expected domain pattern:

- `tulana.madfam.io`
- `tulana-app.madfam.io`
- `tulana-api.madfam.io`

Plan:

1. Complete Tulana onboarding through Enclii.
2. Populate required production secrets through the approved secret path.
3. Apply Enclii DNS/tunnel routing for all three Tulana surfaces.
4. Confirm API health at `https://tulana-api.madfam.io/api/v1/health/`.
5. Confirm landing/app surfaces are not placeholder pages unless explicitly marked as spec-phase.
6. Confirm status API removes all three Tulana entries from affected services.

### P2: Prevent status/release regressions

Plan:

1. Keep digest promotion signed, per-service, and fail-closed.
2. Prevent `madfam-bot` from direct high-frequency digest churn.
3. Require every monitored surface to have a semantic assertion, not only HTTP 200.
4. Add status contract checks for final redirected URL, expected content marker, API health shape, and DNS authority where applicable.
5. Treat `Synced/Healthy` GitOps state as necessary but not sufficient; status must keep probing live behavior.

## Completion definition

The ecosystem is 100% healthy and truthfully reflected in status only when:

- Status reports 68 of 68 operational, or every non-operational entry has an explicit incident.
- Every Enclii-owned production domain has a status entry or an approved exemption.
- Every status entry has semantic expectations.
- Every Enclii app backing a public production surface is `Synced` and `Healthy`.
- Every ExternalSecret backing a production app is `Ready=True`.
- DNS authority changes are handled through Enclii, not raw provider tools.
- The green state holds continuously for at least 24 hours.
