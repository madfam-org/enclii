# Enclii adapter gaps registry

Track operations that still require break-glass (`kubectl`, provider CLIs, manual secrets) until an Enclii web/API/CLI adapter exists.

| Gap | Current workaround | Target adapter | Priority |
|-----|-------------------|----------------|----------|
| Production build secrets | Manual `kubectl apply` per `infra/k8s/production/kustomization.yaml` comments | `enclii secrets` + ExternalSecrets sync | P1 |
| Cloudflare optional secrets | Manual kubectl when not in git | `enclii providers cloudflare` | P2 |
| Policy-only kubectl comment | `infra/k8s/policies/enclii-default-deny.yaml` header | ArgoCD app docs only | P3 |
| Makefile `deploy-prod` | Raw `kubectl apply -k` | `enclii deploy` / GitOps-only path | P2 |
| `POST /v1/admin/projects/:slug/reconcile-services` | Admin API curl to register GitOps Deployments as Enclii services | `enclii projects reconcile-services` | P1 |
| Cloudflare tunnel route mutation | `enclii junctions add` (updates wrong targets via `AddRoute`) | `enclii providers cloudflare tunnels-apply` or junction reconcile job | P1 |
| Commercial GA staging secrets | Manual `workflow_dispatch` + repo secrets | GitHub Environment `commercial-ga-staging` documented in STAGING_SECRETS_SETUP | P1 |
| Prod DB migration verify (030) | Assume API startup `db.Migrate`; no read-back command | `enclii db schema` or migration status endpoint | P2 |
| Detached Longhorn volume delete | `kubectl delete volumes.longhorn.io` (2026-05-23 orphan sweep) | `enclii ops storage longhorn --apply` prune detached | P2 |
| Longhorn helm CPU upgrade | `helm upgrade` via SSH/kubectl per REMAINING_ITEMS | `enclii providers` or `enclii ops storage` apply path | P1 |

When closing a gap, remove the row and link the PR that added the adapter.

## Progress log

### 2026-05-25 — Secrets adapter surface

`enclii secrets sync EXTERNAL_SECRET --namespace <ns>` now routes routine ExternalSecret reconciliation refresh through the audited Enclii ops contract (`ops.secrets.sync`, backed by the existing refresh adapter). This replaces ad-hoc `kubectl annotate externalsecret ... force-sync=...` for the common sync case.

`enclii secrets rotate TARGET` now provides a plan-first audited operation contract (`ops.secrets.rotate`). Apply remains intentionally blocked until the Vault writer, dual-consumer cutover, verification, and old-value revocation flow are server-side safe. Keep the P1 production-build-secret gap open until rotation apply is implemented end-to-end.

### 2026-05-25 — Cloudflare provider CLI hardening

`enclii providers cloudflare dns-apply` now exposes the concrete DNS mutation flags used by the existing server-side Cloudflare adapter: `--type`, `--content`, and `--proxied`. `enclii providers cloudflare credentials` is also exposed as a contract-read surface for provider credential readiness.

Keep the Cloudflare optional-secrets gap open until the credentials read endpoint reports concrete provider environment state instead of only the generic operation contract.

## 2026-05-25 Cloudflare credential-readiness adapter

Implemented the local API handler for `providers.cloudflare.credentials` after production preflight confirmed the deployed API returns `404 unsupported operation cloudflare.credentials`.

- Registered `cloudflare.credentials` as a read-only provider action.
- Added metadata-only readiness output for required config keys: `ENCLII_CLOUDFLARE_API_TOKEN`, `ENCLII_CLOUDFLARE_ACCOUNT_ID`, `ENCLII_CLOUDFLARE_ZONE_ID`, and `ENCLII_CLOUDFLARE_TUNNEL_ID`.
- The handler returns presence booleans and service wiring state only; it does not return secret values.
- Deployment remains required before production preflight can pass this capability.
