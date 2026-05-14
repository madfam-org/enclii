# Phynd app Enclii activation blockers

Date: 2026-05-14

## Scope

This runbook captures the Enclii-side blockers preventing `https://phynd.app` from being activated through the required Enclii-first workflow.

## Verified blockers

- `phynd-crm` is not registered in Enclii project inventory.
- `enclii projects create --name "Phynd CRM" --slug phynd-crm` returns `402 tier_limit_exceeded`.
- `phynd.app` is delegated to Porkbun nameservers:
  - `salvador.ns.porkbun.com`
  - `maceio.ns.porkbun.com`
  - `fortaleza.ns.porkbun.com`
  - `curitiba.ns.porkbun.com`
- Enclii Cloudflare provider has no Cloudflare zone for `phynd.app`.
- Enclii Porkbun provider currently returns `adapter_unconfigured`.
- Active Cloudflare tunnel config routes `crm.madfam.io` to the legacy `phyne-crm` service target instead of `phynd-crm`.
- The Cloudflare tunnel provider command is currently useful for inventory/planning but does not yet provide a complete route reconcile/apply path for this migration.
- `enclii onboard` validates manifest state from GitHub. Phynd production Deployment manifests must be committed and pushed with digest-pinned images before onboarding will pass the image gate.

## Remediation shipped in code

- API tier middleware now bypasses project/service/deploy plan caps for trusted `admin` and `superadmin` roles.
- Unit coverage was added for the trusted-operator tier bypass helper.

## Required rollout

1. Build and release `switchyard-api` with the tier middleware change.
2. Commit and push Phynd manifest changes so GitHub contains digest-pinned production images.
3. Re-run:
   - `enclii projects create --name "Phynd CRM" --slug phynd-crm`
   - `enclii services-sync --dir enclii/services --project phynd-crm`
4. Configure DNS ownership for `phynd.app` using one Enclii-owned path:
   - Preferred: delegate `phynd.app` to the MADFAM Cloudflare account managed by Enclii.
   - Alternate: configure the Enclii Porkbun adapter and keep Porkbun authoritative.
5. Add or reconcile route targets through Enclii:
   - `phynd.app` -> `http://phynd-crm-web.phynd-crm.svc.cluster.local:80`
   - `www.phynd.app` -> `http://phynd-crm-web.phynd-crm.svc.cluster.local:80`
   - `crm.madfam.io` -> `http://phynd-crm-web.phynd-crm.svc.cluster.local:80`
6. Provision Phynd production secrets through Enclii/Vault.
7. Configure Janua OIDC client redirects:
   - `https://phynd.app/api/auth/callback/janua`
   - `https://crm.madfam.io/api/auth/callback/janua`
8. Run production smoke checks from Enclii:
   - `https://phynd.app/api/health`
   - `https://phynd.app/login`
   - `admin@madfam.io` Janua login redirects to `/overview`.

## Operational boundary

Do not fix this by using raw `kubectl`, direct Cloudflare dashboard changes, or direct Porkbun dashboard changes unless Enclii is unavailable during a production incident. Any break-glass change must be reconciled back into Enclii immediately afterward.
