# Secret Intake (chat-safe credential handoff)

**Last Updated:** 2026-06-15

Operators supply production credentials through Enclii without pasting values into
agent chat or git. Switchyard merges keys into Vault once; agents poll `intake_id`
status only.

Policy (private): [internal-devops secret intake decision](https://github.com/madfam-org/internal-devops/blob/main/decisions/2026-06-15-secret-intake-protocol.md)

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
