# Phynd app Enclii activation blockers

Date: 2026-05-14

## Scope

This runbook captures the Enclii-side blockers preventing `https://phynd.app` from being activated through the required Enclii-first workflow.

## Verified blockers

- `phynd-crm` is now registered in Enclii project inventory.
- `phynd-crm-web` is registered as service `55d2ba51-d6b3-481c-ae56-e5410c3b5a6d`.
- `phynd-crm-worker` is registered as service `5e1a20e4-2302-4aa0-a37e-fa7dc9fa87ea`.
- `phynd.app` is registered through Porkbun and delegated to Cloudflare
  nameservers:
  - `chin.ns.cloudflare.com`
  - `woz.ns.cloudflare.com`
- Historical: Enclii Porkbun provider returned `adapter_unconfigured` at the
  time of this blocker capture. Active credentials were restored as a
  break-glass provider secret on 2026-05-17.
- Active Cloudflare tunnel config must route `crm.madfam.io` and
  `crm.phynd.app` to the current `phynd-crm` service target.
- Active Cloudflare tunnel config now includes `phynd.app` and `www.phynd.app` pointing to `http://phynd-crm-web.phynd-crm.svc.cluster.local:80`.
- `phynd-crm-production` has now been retired through Enclii with orphan propagation.
- `phynd-crm-services` now syncs cleanly, but remains `Degraded` because its runtime secrets are not materialized.
- `ExternalSecret/phynd-crm-secrets` now exists in namespace `phynd-crm`, but Enclii reports it as `Ready=False` with `SecretSyncedError` and message `could not get secret data from provider`.
- Phynd web and worker pods are blocked pending container readiness while `phynd-crm-secrets` is not materialized.
- Live `api.enclii.dev` now advertises `ops.apps.retire`, and the adapter is wired far enough to submit the Kubernetes delete request through Enclii.
- The first production retire apply failed because `system:serviceaccount:enclii:switchyard-api` lacked `delete` on `applications.argoproj.io` in namespace `argocd`.
- The Enclii RBAC release now grants `delete` on `argoproj.io/applications` and `patch` on `external-secrets.io/externalsecrets` to the `switchyard-api` service account.
- `ops.secrets.refresh` successfully patches `ExternalSecret/phynd-crm-secrets`, but the provider still lacks `secret/phynd-crm` data.
- `core-services` remains `OutOfSync` for unrelated unsigned Enclii Deployment images blocked by Kyverno image-signature policy; the RBAC resources themselves are synced.
- The Cloudflare tunnel provider command is currently useful for inventory/planning but does not yet provide a complete conflict-resolution path for replacing the existing `crm.madfam.io` legacy route.

## Remediation shipped in code

- API tier middleware now bypasses project/service/deploy plan caps for trusted `admin` and `superadmin` roles.
- Unit coverage was added for the trusted-operator tier bypass helper.
- Phynd onboarding and service registration completed through Enclii after the Phynd manifest digest pins were committed and pushed.
- Enclii junctions were created for `phynd.app`, `www.phynd.app`, and `crm.madfam.io`.
- Phynd production now declares a Vault-backed ExternalSecret for `phynd-crm-secrets` at `secret/phynd-crm`.
- `phynd-crm-services` synced commit `e5c51bab9ace0ee0194677e26a84f51d4337faef`, including the ExternalSecret manifest, but remains Degraded/OutOfSync because of legacy shared ownership and missing provider data.
- Roundhouse now normalizes Dockerfile paths for Kaniko Git subdirectory contexts, so service specs that use `context: apps/<service>` with `dockerfile: apps/<service>/Dockerfile` are passed to Kaniko as `--dockerfile=Dockerfile` inside the selected context instead of failing as an invalid path.
- Enclii service records with empty build configs were corrected through the Enclii API:
  - `dispatch` -> `apps/admin-console/Dockerfile`, context `.`
  - `docs-site` -> `apps/docs-site/Dockerfile`, context `.`
  - `landing-page` -> `apps/landing/Dockerfile`, context `.`
  - `waybill` -> `apps/waybill/Dockerfile`, context `.`
- Fresh Enclii builds were triggered for `dispatch`, `docs-site`, `landing-page`, `waybill`, `roundhouse`, and `switchyard-api` at commit `3afbe8604c6d8b862011df5305443e9030ffab1b` after those service records were corrected.

## Required rollout

1. Restore production secret material through Enclii/Vault or an approved Selva secret workflow.
   - Vault key: `secret/phynd-crm`
   - ExternalSecret target: `phynd-crm-secrets`
2. Confirm Enclii can operate the delegated Cloudflare zone for `phynd.app`.
3. Reconcile `crm.madfam.io` by removing/replacing the legacy `phynd-crm` tunnel route through Enclii.
4. Configure Janua OIDC client redirects:
   - `https://phynd.app/api/auth/callback/janua`
   - `https://crm.madfam.io/api/auth/callback/janua`
5. Run production smoke checks from Enclii:
   - `https://phynd.app/api/health`
   - `https://phynd.app/login`
   - `admin@madfam.io` Janua login redirects to `/overview`.

## Operational boundary

Do not fix this by using raw `kubectl`, direct Cloudflare dashboard changes, or direct Porkbun dashboard changes unless Enclii is unavailable during a production incident. Any break-glass change must be reconciled back into Enclii immediately afterward.

## Continuation status — 2026-05-14

Completed through Enclii:

- Added the Switchyard RBAC needed for application retirement and ExternalSecret refresh.
- Retired `phynd-crm-production` through `enclii ops apps retire`.
- Recreated the `crm.madfam.io` junction through Enclii with id `15118c4b-aaf1-4c7a-bba7-27e58c688e96`.
- Verified the active tunnel route now maps `crm.madfam.io`, `phynd.app`, and `www.phynd.app` to `http://phynd-crm-web.phynd-crm.svc.cluster.local:80`.

Still blocked:

- `phynd-crm-services` is `Synced` but `Degraded`.
- `ExternalSecret/phynd-crm-secrets` is `Ready=False`, reason `SecretSyncedError`, message `could not get secret data from provider`.
- Enclii/Selva secret namespace lookup for `phynd-crm` returns `404`, so no secret key set is currently available through that API path.
- `https://crm.madfam.io` reaches Cloudflare but returns `502` because the upstream Phynd pods cannot start without `phynd-crm-secrets`.
- `phynd.app` and `www.phynd.app` still resolve to Porkbun/Pixie IPs, so they do not reach the Enclii tunnel despite the tunnel route existing.

Required remediation:

- Populate Vault key `secret/phynd-crm` or the active ExternalSecret provider path with real values for the Phynd secret contract.
- Refresh `ExternalSecret/phynd-crm-secrets` through `enclii ops secrets refresh`.
- Keep `phynd.app` delegated to the Enclii-managed Cloudflare zone and apply
  DNS changes through Enclii where possible.
- Keep raw `kubectl` secret writes as break-glass only; the standard path is Selva RFC 0005 for secret material and Enclii for deploy/provisioning/health.

## Continuation status — 2026-05-15

Completed through Enclii and GitOps:

- `https://phynd.app` is live through Cloudflare and serves the Phynd landing.
- `https://crm.madfam.io/` redirects unauthenticated visitors to `/login` and exposes the MADFAM Janua SSO login copy.
- The Phynd web image containing the corrected host policy and repository link was promoted to production through the Phynd production workflow.
- Enclii junction `1bf7e7d5-86f0-40df-a4b8-a2d68c0eae16` was created for `crm.phynd.app`.
- Enclii's active Cloudflare tunnel inventory now includes `crm.phynd.app -> http://phynd-crm-web.phynd-crm.svc.cluster.local:80`.

Still blocked:

- `crm.phynd.app` now has public DNS and routes through the production
  Cloudflare tunnel to `phynd-crm-web`.
- Enclii Porkbun provider credentials were restored as a break-glass provider
  secret on 2026-05-17 for registrar inventory and fallback operations.

Continuation on 2026-05-17:

- RDAP confirms `phynd.app` is registered through Porkbun and delegated to
  Cloudflare nameservers.
- `vault-store` is `Ready=False` / `InvalidProviderConfig`, so the
  Vault-backed `enclii-porkbun-credentials` ExternalSecret cannot sync yet.

Required remediation:

- Verify Enclii Cloudflare authority for `phynd.app`; use Porkbun only for
  registrar inventory or fallback recovery.
- Repair Vault Kubernetes auth with `VAULT_TOKEN="$TOKEN" ./scripts/repair-vault-eso-auth.sh` before expecting the Porkbun adapter secret to sync.
- After DNS authority exists, create the `crm.phynd.app` CNAME/record to the Enclii tunnel target through Enclii.
- Keep `PhyndCRM App` red on `status.madfam.io` until external DNS and HTTPS are live.
