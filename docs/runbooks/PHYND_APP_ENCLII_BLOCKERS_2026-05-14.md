# Phynd app Enclii activation blockers

Date: 2026-05-14

## Scope

This runbook captures the Enclii-side blockers preventing `https://phynd.app` from being activated through the required Enclii-first workflow.

## Verified blockers

- `phynd-crm` is now registered in Enclii project inventory.
- `phynd-crm-web` is registered as service `55d2ba51-d6b3-481c-ae56-e5410c3b5a6d`.
- `phynd-crm-worker` is registered as service `5e1a20e4-2302-4aa0-a37e-fa7dc9fa87ea`.
- `phynd.app` is delegated to Porkbun nameservers:
  - `salvador.ns.porkbun.com`
  - `maceio.ns.porkbun.com`
  - `fortaleza.ns.porkbun.com`
  - `curitiba.ns.porkbun.com`
- Enclii Cloudflare provider has no Cloudflare zone for `phynd.app`.
- Enclii Porkbun provider currently returns `adapter_unconfigured`.
- Active Cloudflare tunnel config routes `crm.madfam.io` to the legacy `phyne-crm` service target instead of `phynd-crm`.
- Active Cloudflare tunnel config now includes `phynd.app` and `www.phynd.app` pointing to `http://phynd-crm-web.phynd-crm.svc.cluster.local:80`.
- `phyne-crm-production` has now been retired through Enclii with orphan propagation.
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
2. Configure DNS ownership for `phynd.app` using one Enclii-owned path:
   - Preferred: delegate `phynd.app` to the MADFAM Cloudflare account managed by Enclii.
   - Alternate: configure the Enclii Porkbun adapter and keep Porkbun authoritative.
3. Reconcile `crm.madfam.io` by removing/replacing the legacy `phyne-crm` tunnel route through Enclii.
4. Configure Janua OIDC client redirects:
   - `https://phynd.app/api/auth/callback/janua`
   - `https://crm.madfam.io/api/auth/callback/janua`
5. Run production smoke checks from Enclii:
   - `https://phynd.app/api/health`
   - `https://phynd.app/login`
   - `admin@madfam.io` Janua login redirects to `/overview`.

## Operational boundary

Do not fix this by using raw `kubectl`, direct Cloudflare dashboard changes, or direct Porkbun dashboard changes unless Enclii is unavailable during a production incident. Any break-glass change must be reconciled back into Enclii immediately afterward.
