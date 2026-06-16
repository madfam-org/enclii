# Secret Intake (chat-safe credential handoff)

**Last Updated:** 2026-06-16

Operators supply production credentials through Enclii without pasting values into
agent chat or git. Switchyard merges keys into Vault once; agents poll `intake_id`
status only.

**Post-rebuild (2026-06-16):** Vault writer live. Dhanam Phase 0 **MITIGATED** —
see [recovery session](https://github.com/madfam-org/internal-devops/blob/main/runbooks/2026-06-16-dhanam-secrets-recovery-session.md).
Use `export KUBECONFIG=~/.kube/config-hetzner`.
Public API: `https://api.enclii.dev`. Private record: [vault rebuild complete](https://github.com/madfam-org/internal-devops/blob/main/runbooks/2026-06-16-vault-rebootstrap-complete.md).

Policy (private): [internal-devops secret intake decision](https://github.com/madfam-org/internal-devops/blob/main/decisions/2026-06-15-secret-intake-protocol.md)

Custody split (Dhanam vs Resend): [platform comms decision](https://github.com/madfam-org/internal-devops/blob/main/decisions/2026-06-16-platform-comms-and-dhanam-secret-custody.md)

Full provider matrix: [ecosystem provider custody model](https://github.com/madfam-org/internal-devops/blob/main/decisions/2026-06-16-ecosystem-provider-custody-model.md)

## Prerequisites

1. Switchyard Vault writer enabled on `switchyard-api`:
   - `ENCLII_SECRET_ROTATION_ENABLED=true`
   - `ENCLII_VAULT_TOKEN` from `vault-credentials` in `enclii` namespace
2. Operator Janua admin session (`enclii login`) or `ENCLII_TOKEN`

Provision `vault-credentials` (never commit token values):

```bash
VAULT_TOKEN_FILE=/path/to/vault-admin.token \
  ./scripts/provision-switchyard-vault-writer.sh
```

Template: `infra/k8s/production/vault-credentials.secret.template.yaml`

## Registry targets

Canonical routing: `apps/switchyard-api/internal/secretsintake/registry.yaml`

| Target ID | Vault path | Keys |
|-----------|------------|------|
| `ceq/vast-api-key` | `secret/ceq` | `VAST_API_KEY` |
| `ceq/janua-client-secret` | `secret/ceq` | `JANUA_CLIENT_SECRET` |
| `dhanam/stripe-mx-live` | `secret/dhanam` | `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET` |
| `dhanam/oidc-janua` | `secret/dhanam` | `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_ISSUER` |
| `dhanam/session-auth` | `secret/dhanam` | `SESSION_SECRET`, `NEXTAUTH_SECRET` |
| `dhanam/app-infra` | `secret/dhanam` | `R2_*`, `CLOUDFLARE_API_TOKEN`, PostHog, Sentry |
| `platform/comms-resend-api-key` | `secret/comms` | `resend-api-key` → `resend_api_key` in Vault |

After `platform/comms-resend-api-key` intake, fan-out to all consumers:

```bash
./scripts/force-sync-comms-fanout.sh
```

ESO sources: `enclii-secrets`, `janua-secrets`, `madfam-site-secrets`, `phynd-crm-secrets` (and staging) read `secret/comms.resend_api_key`.
| `enclii/internal-api-key` | `secret/enclii` | `INTERNAL_API_KEY` |

Add targets via PR to the registry — do not hardcode paths in runbooks.

## Operator flow

**One-shot (recommended):**

```bash
VAULT_TOKEN_FILE=/path/to/vault-admin.token \
VAST_API_KEY_FILE=/path/to/vast.api.key \
  ./scripts/finish-line-secret-intake.sh
```

Or step-by-step:

```bash
export ENCLII_API_ENDPOINT=https://api.enclii.dev
enclii login   # admin@madfam.io — single Janua SSO session
enclii secrets provision oidc --platform dhanam --reason "post-rebuild oidc"
# Optional: auto-generates session-auth + intakes OIDC trio to Vault
enclii secrets intake status int_<id>
```

Manual per-key intake (when a provider secret is not Janua-derived):

```bash
enclii secrets intake targets
enclii secrets intake submit ceq/vast-api-key --reason "orchestrator bootstrap"
# masked prompt, or --value-file / --stdin (KEY=VALUE lines)
enclii secrets intake status int_<id>
```

Tell agents only the `intake_id` — never the secret value.

## API (admin role)

| Method | Path | Notes |
|--------|------|-------|
| `GET` | `/v1/secrets/intake/targets` | Public-safe target metadata |
| `POST` | `/v1/secrets/intake` | Write-only; body has values once |
| `GET` | `/v1/secrets/intake/:id` | Status/metadata only |

Errors: `503 vault_writer_disabled` (no `vault-credentials`), `404 unknown_target`,
`400 invalid_values`.

## Agent flow

1. Request operator run intake for a registry target.
2. Poll `enclii secrets intake status <id>` or `GET /v1/secrets/intake/:id`.
3. Run downstream ops (ESO sync, bootstrap scripts) — never request the value.

## Related

- [Secrets Management](../infrastructure/SECRETS_MANAGEMENT.md)
- [Vault Operations](./VAULT_OPERATIONS.md)
- [CLI: secrets intake](../cli/commands/secrets.md#enclii-secrets-intake)
- Private bridge gaps: `internal-devops/runbooks/2026-06-15-vault-bridge-gaps.md`
