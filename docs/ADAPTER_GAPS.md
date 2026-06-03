# Enclii adapter gaps registry

Track operations that still require break-glass (`kubectl`, provider CLIs, manual secrets) until an Enclii web/API/CLI adapter exists.

| Gap | Current workaround | Target adapter | Priority |
|-----|-------------------|----------------|----------|
| Production build secrets | Manual `kubectl apply` per `infra/k8s/production/kustomization.yaml` comments | `enclii secrets` + ExternalSecrets sync | P1 |
| Cloudflare optional secrets | Manual kubectl when not in git | `enclii providers cloudflare` | P2 |
| Dispatch direct Cloudflare API | ~~`CLOUDFLARE_API_TOKEN` on Dispatch~~ | **Closed** — Dispatch uses Switchyard provider APIs (W3) | — |
| Policy-only kubectl comment | `infra/k8s/policies/enclii-default-deny.yaml` header | ArgoCD app docs only | P3 |
| Makefile `deploy-prod` | Break-glass raw `kubectl apply -k` only | `enclii deploy` / GitOps-only path | P2 |
| Commercial GA staging secrets | Manual `workflow_dispatch` + repo secrets | GitHub Environment `commercial-ga-staging` — **8/8 populated** 2026-05-30 | P2 |
| Enclii Vault `internal_api_key` backfill | ~~Manual `VAULT_TOKEN` + `scripts/backfill-vault-path-from-k8s-secret.sh`~~ | **Closed** — `enclii secrets vault-backfill` merges K8s Secret keys into Vault KV v2 and force-syncs merge ESO | — |
| Signup verification email (Resend) | ~~Janua bridge~~ | **Closed** — `enclii.dev` verified; Vault backfill via `enclii secrets vault-backfill`; `providers.resend.*` + Dispatch Provider Hub | — |
| Agent SaaS tool plane (`madfam.ops.*` proxy) | N/A — not built | Coupler `madfam.ops.*` → Enclii `providers.*` / `ops.*` (admin JWT) | P1 — track in [COUPLER_REMEDIATION_PLAN.md](strategy/COUPLER_REMEDIATION_PLAN.md) |

When closing a gap, remove the row and link the PR that added the adapter.

## Progress log

### 2026-05-25 — Secrets adapter surface

`enclii secrets sync EXTERNAL_SECRET --namespace <ns>` now routes routine ExternalSecret reconciliation refresh through the audited Enclii ops contract (`ops.secrets.sync`, backed by the existing refresh adapter). This replaces ad-hoc `kubectl annotate externalsecret ... force-sync=...` for the common sync case.

`enclii secrets vault-backfill SOURCE_SECRET --namespace <ns> --vault-path <path> --external-secret <name>` now replaces the manual Vault backfill script for merge ESO migration. The server reads source Kubernetes Secret values, normalizes keys to lower snake case, merges them into Vault KV v2, omits values from responses, and force-syncs the target ExternalSecret when supplied.

`enclii secrets rotate TARGET` now supports audited ExternalSecret rotation cutover apply (`ops.secrets.rotate`) once the backing provider value/version has already been staged. The adapter patches rotation metadata plus `force-sync` without reading or writing secret values, then points verification back to `ops.secrets.external`.

Keep the P1 production-build-secret gap open until Enclii can generate/write new provider values, verify dual-consumer rollout, and revoke old values end-to-end.

### 2026-05-25 — Cloudflare provider CLI hardening

`enclii providers cloudflare dns-apply` now exposes the concrete DNS mutation flags used by the existing server-side Cloudflare adapter: `--type`, `--content`, and `--proxied`. `enclii providers cloudflare credentials` is also exposed as a contract-read surface for provider credential readiness.

Keep the Cloudflare optional-secrets gap open until the credentials read endpoint reports concrete provider environment state instead of only the generic operation contract.

### 2026-05-30 — Coupler Program (Agent Tool Plane)

Documented separate AGPL repo `madfam-org/coupler` for Composio-class agent tools. Enclii retains operator Provider Hub only; end-user SaaS connectors and MCP live in Coupler. See `docs/strategy/COUPLER_REMEDIATION_PLAN.md`. Open adapter gap: `madfam.ops.*` proxy (closes in Coupler P4).

### 2026-05-29 — GA adapter surfaces (Wave 0 engineering)

- `enclii projects reconcile-services <slug>` — admin POST `/v1/admin/projects/:slug/reconcile-services`
- `enclii db schema` + `GET /v1/admin/db/schema` — migration version, dirty flag, GA column checks (migration 030 `rollout_blocked_reason`)
- `enclii ops storage settings-apply` — Longhorn CPU settings from helm values (O-5)
- `enclii ops storage prune-detached` — delete detached Longhorn orphan volumes (O-4)
- `enclii admin ga-verify` — Wave 0 security + schema + Longhorn plan smoke
- `scripts/wave0-ga-ops.sh` — Enclii-first Wave 0 orchestration (O-4–O-6)

### 2026-05-29 — Wave 1 stability adapters

- `enclii ops apps sync-sweep` — batch sync drifted Argo apps with default exclusion `network-policies` (O-8)
- `enclii admin ga-verify --stability` — Wave 1 read-only checks (Argo drift, Vault, policy)
- `scripts/wave1-ga-ops.sh` — Enclii-first Wave 1 orchestration (O-8–O-11)

### 2026-05-29 — Wave 1.5 storage + cosign adapters

- `enclii ops storage storageclass-apply` — create missing Longhorn StorageClasses from GA manifest
- `enclii ops policy cosign-enable` — label phased namespaces for Kyverno cosign enforce (O-11)

### 2026-05-29 — Cloudflare tunnel route reconcile adapter

- `enclii providers cloudflare tunnels-apply --project <slug>` — diff junction domains against live tunnel ingress and apply corrected K8s service backends via `resolveServiceNamespace`
- Replaces break-glass `enclii junctions add` / manual Cloudflare tunnel edits when routes point at wrong namespaces

### 2026-05-29 — ESO sync-sweep + post-deploy adapter smoke

- `enclii ops secrets sync-sweep` — batch force-sync ExternalSecrets with `Ready!=True` in GA namespaces (`enclii`, `data`, `monitoring`, `cloudflare-tunnel`)
- `scripts/post-deploy-ga-adapters.sh` — verify Wave 0–1.5 adapter routes are live after deploy

### 2026-05-30 — Switchyard-api Longhorn ops RBAC

`infra/k8s/base/rbac.yaml` now grants `switchyard-api` delete on `longhorn.io/volumes`, patch on `longhorn.io/settings`, and list/create on `storageclasses`. `enclii ops storage prune-detached`, `settings-apply`, and `storageclass-apply` verified on prod after `core-services` sync (`85ad80a3`).

### 2026-05-30 — Staging env bootstrap + ops runbook

- GitHub environment `commercial-ga-staging` created on `madfam-org/enclii`
- `scripts/setup-commercial-ga-staging-env.sh` — idempotent env check + missing secret report
- `scripts/ga-ops-runbook.sh` — ROI-ordered public proof → adapter smoke → Wave 0/1
- `scripts/security-release-tenant-smoke.sh` — O-3 step 3 tenant junction/cron IDOR smoke (requires non-admin token)
- `enclii secrets vault-backfill` — O-10 Vault backfill + ESO refresh through Switchyard API

## 2026-05-25 Cloudflare credential-readiness adapter

Implemented the local API handler for `providers.cloudflare.credentials` after production preflight confirmed the deployed API returns `404 unsupported operation cloudflare.credentials`.

- Registered `cloudflare.credentials` as a read-only provider action.
- Added metadata-only readiness output for required config keys: `ENCLII_CLOUDFLARE_API_TOKEN`, `ENCLII_CLOUDFLARE_ACCOUNT_ID`, `ENCLII_CLOUDFLARE_ZONE_ID`, and `ENCLII_CLOUDFLARE_TUNNEL_ID`.
- The handler returns presence booleans and service wiring state only; it does not return secret values.
- Deployment remains required before production preflight can pass this capability.
