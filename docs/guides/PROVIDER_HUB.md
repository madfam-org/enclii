# Provider Hub operator runbook

Enclii-first management of Madfam ecosystem providers (Resend, Cloudflare, GitHub, Porkbun, Vault/ESO) through **switchyard-api** operator contracts, **Dispatch** (`admin.enclii.dev`), and **Switchyard UI** (`app.enclii.dev`) read-only surfaces.

## Surfaces

| Surface | URL | Mutations |
|---------|-----|-----------|
| Dispatch Provider Hub | `https://admin.enclii.dev/providers` | Yes — dry-run → apply with reason |
| Switchyard Integrations | `https://app.enclii.dev/integrations` | Read-only; admin sees catalog readiness |
| CLI | `enclii providers …` | Yes — same contract as API |

## Resend domain onboarding (enclii.dev example)

1. **Credentials** — `enclii providers resend credentials` or Dispatch **Providers → Overview**.
2. **Add domain** — Dispatch **Providers → Resend → Add domain**, or:
   ```bash
   enclii providers resend domain-add-apply --target enclii.dev
   enclii providers resend domain-add-apply --target enclii.dev --apply --reason "GA sender domain"
   ```
3. **Apply DNS** — orchestrates Resend TXT/MX via Cloudflare:
   ```bash
   enclii providers resend domain-dns-apply --target enclii.dev --apply --reason "Resend DNS for enclii.dev"
   ```
4. **Verify** — `enclii providers resend domain-verify-apply --target enclii.dev --apply --reason "post-DNS verify"`
5. **Send test** — `enclii providers resend send-test-apply --target enclii.dev --to ops@madfam.io --apply --reason "smoke"`

Vault backfill (retire Janua bridge):

```bash
enclii secrets vault-backfill enclii-secrets \
  --namespace enclii \
  --vault-path secret/enclii \
  --external-secret enclii-resend-api-key \
  --apply \
  --reason "retire Janua bridge after Resend key staged in source Secret"
```

## Cloudflare (Dispatch consolidation)

Dispatch **Domain Matrix** and DNS drawer call Switchyard `providers.cloudflare.*` — no `CLOUDFLARE_API_TOKEN` on Dispatch pods.

- List zones: `providers.cloudflare.zones` (read)
- Commission: `providers.cloudflare.zone-add-apply` (mutate)
- DNS: `providers.cloudflare.dns` / `dns-apply`

## Audit

All applies return `operation_id` / `audit_id`. Dispatch **OperationPlanDialog** shows these after apply.

## Ecosystem tenants

Shared registry: `packages/ecosystem-tenants/` (TS) and `apps/switchyard-api/internal/ecosystem/` (Go). Tenant is inferred from domain suffix for Resend region and default sender.
