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
   enclii providers resend domain-add-apply enclii.dev
   enclii providers resend domain-add-apply enclii.dev --apply --reason "GA sender domain"
   ```
3. **Apply DNS** — orchestrates Resend TXT/MX via Cloudflare:
   ```bash
   enclii providers resend domain-dns-apply enclii.dev --apply --reason "Resend DNS for enclii.dev"
   ```
4. **Verify** — `enclii providers resend domain-verify-apply enclii.dev --apply --reason "post-DNS verify"`
5. **Send test** — Dispatch **Providers → Resend**, the domain row's send-test action, or pass the recipient with `--to` (sent as `args.to`, which the API requires):
   ```bash
   enclii providers resend send-test-apply enclii.dev --to ops@example.com
   enclii providers resend send-test-apply enclii.dev --to ops@example.com --apply --reason "post-verify send test"
   ```
   `--to` first shipped in `v1.0.0-alpha.12`. On `v1.0.0-alpha.11` and older the flag is rejected as unknown, and without it the API answers `invalid_request`: upgrade the CLI, or use Dispatch.

   **Sender.** The test is sent from the default sender address of the tenant that owns the target domain (for example `noreply@creatumundo.mx` for `creatumundo.mx`, from the ecosystem tenant registry), so it exercises that domain's Resend verification. When the target belongs to no tenant with a default sender, it is sent from the server's configured sender (`ENCLII_EMAIL_FROM_ADDRESS`, default `noreply@enclii.dev`). The dry-run's `from` is exactly what `--apply` sends; before PRNUM_LINK the dry-run showed the tenant sender but the send always used the configured one.

   **Unconfigured server.** Without a Resend API key or without the email service, both the dry-run and `--apply` answer `adapter_unconfigured` (HTTP `503` on `--apply`) before any send is attempted.

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
