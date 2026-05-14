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
- `phynd-crm-services` is Degraded/OutOfSync because legacy ArgoCD app `phyne-crm-production` still owns shared resources in the same namespace.
- `ExternalSecret/phynd-crm-secrets` now exists in namespace `phynd-crm`, but Enclii reports it as `Ready=False` with `SecretSyncedError` and message `could not get secret data from provider`.
- Phynd web and worker pods are blocked at `CreateContainerConfigError`.
- The Cloudflare tunnel provider command is currently useful for inventory/planning but does not yet provide a complete conflict-resolution path for replacing the existing `crm.madfam.io` legacy route.
- `ops.apps.retire` has been added to the Enclii contract so stale Argo Applications can be retired through Enclii instead of raw `kubectl`/Argo access.
- Live `api.enclii.dev` does not yet advertise `ops.apps.retire`; the production Switchyard API still reports app actions `status`, `sync`, `diff`, and `rollback`.
- Switchyard API release history shows the retire-operation commit queued/building, while older releases are timing out with `Build timed out (no callback received within 30 minutes)`.
- Roundhouse pods are running and some build callbacks succeed, but logs also show repeated failed build jobs with invalid Dockerfile paths for other services. The build queue must be stabilized before the retire operation can be exercised safely in production.

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

1. Stabilize Enclii release/build processing so the Switchyard API image containing `ops.apps.retire` reaches production.
   - Release the Roundhouse Dockerfile-path normalization fix.
   - Confirm corrected Enclii service records stop invalid Dockerfile-path failures for `dispatch`, `docs-site`, `landing-page`, and `waybill`.
   - Confirm `enclii ops capabilities --json` advertises app action `retire`.
2. Retire the legacy `phyne-crm-production` ArgoCD application through Enclii:
   - `enclii ops apps retire phyne-crm-production --apply --reason "retire legacy Phyne CRM app after Phynd CRM successor onboarding"`
   - Default propagation is orphan, so this removes the stale Argo Application without deleting the Phynd namespace/resources.
3. Restore production secret material through Enclii/Vault or an approved Selva secret workflow.
   - Vault key: `secret/phynd-crm`
   - ExternalSecret target: `phynd-crm-secrets`
4. Configure DNS ownership for `phynd.app` using one Enclii-owned path:
   - Preferred: delegate `phynd.app` to the MADFAM Cloudflare account managed by Enclii.
   - Alternate: configure the Enclii Porkbun adapter and keep Porkbun authoritative.
5. Reconcile `crm.madfam.io` by removing/replacing the legacy `phyne-crm` tunnel route through Enclii.
6. Configure Janua OIDC client redirects:
   - `https://phynd.app/api/auth/callback/janua`
   - `https://crm.madfam.io/api/auth/callback/janua`
7. Run production smoke checks from Enclii:
   - `https://phynd.app/api/health`
   - `https://phynd.app/login`
   - `admin@madfam.io` Janua login redirects to `/overview`.

## Operational boundary

Do not fix this by using raw `kubectl`, direct Cloudflare dashboard changes, or direct Porkbun dashboard changes unless Enclii is unavailable during a production incident. Any break-glass change must be reconciled back into Enclii immediately afterward.
