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
- Enclii reports zero ExternalSecrets in namespace `phynd-crm`; production secret material has not been restored.
- Phynd web and worker pods are blocked at `CreateContainerConfigError`.
- The Cloudflare tunnel provider command is currently useful for inventory/planning but does not yet provide a complete conflict-resolution path for replacing the existing `crm.madfam.io` legacy route.

## Remediation shipped in code

- API tier middleware now bypasses project/service/deploy plan caps for trusted `admin` and `superadmin` roles.
- Unit coverage was added for the trusted-operator tier bypass helper.
- Phynd onboarding and service registration completed through Enclii after the Phynd manifest digest pins were committed and pushed.
- Enclii junctions were created for `phynd.app`, `www.phynd.app`, and `crm.madfam.io`.

## Required rollout

1. Retire the legacy `phyne-crm-production` ArgoCD application through Enclii/GitOps so `phynd-crm-services` is the sole owner of namespace `phynd-crm`.
2. Restore production secret material through Enclii/Vault or an approved Selva secret workflow.
3. Configure DNS ownership for `phynd.app` using one Enclii-owned path:
   - Preferred: delegate `phynd.app` to the MADFAM Cloudflare account managed by Enclii.
   - Alternate: configure the Enclii Porkbun adapter and keep Porkbun authoritative.
4. Reconcile `crm.madfam.io` by removing/replacing the legacy `phyne-crm` tunnel route through Enclii.
5. Configure Janua OIDC client redirects:
   - `https://phynd.app/api/auth/callback/janua`
   - `https://crm.madfam.io/api/auth/callback/janua`
6. Run production smoke checks from Enclii:
   - `https://phynd.app/api/health`
   - `https://phynd.app/login`
   - `admin@madfam.io` Janua login redirects to `/overview`.

## Operational boundary

Do not fix this by using raw `kubectl`, direct Cloudflare dashboard changes, or direct Porkbun dashboard changes unless Enclii is unavailable during a production incident. Any break-glass change must be reconciled back into Enclii immediately afterward.
