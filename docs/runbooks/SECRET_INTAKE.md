# Secret Intake (chat-safe credential handoff)

**Last Updated:** 2026-09-05

> **Boundary checkpoint (2026-09-05, platform on-call):** Public-safe runbook —
> target ids, Vault paths and key NAMES are routing contracts, never values. No
> secret value appears here, and none is retrievable through the intake API by
> design. Private operational detail (the 2026-09-03 break-glass fallback that
> motivated the earlier targets, the messaging-migration decision that motivated
> the 2026-09-05 Courier targets, and the per-app secret custody notes) lives in
> `internal-devops` — the decision is
> `decisions/2026-09-05-third-party-messaging-via-angelia-courier.md` there. No
> recipient id, chat id or channel id appears here or anywhere in this repo.
> Policy: `docs/PUBLIC_REPO_BOUNDARY.md` (repo-boundary contract).

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
| `coupler/janua-service-token` | `secret/coupler` | `JANUA_SERVICE_TOKEN` |
| `janua/internal-api-key` | `secret/janua` | `internal_api_key` |
| `crea-map/internal-api-key` | `secret/crea-map` | `internal_api_key` |
| `crea-map/kalya-feeds` | `secret/crea-map` | `kalya_occupancy_feed_url`, `kalya_capacity_feed_url` |
| `symbiosis-hcm/map-absence-feed` | `secret/symbiosis-hcm` | `map_absence_feed_url`, `map_absence_feed_key` |
| `nauta/kalya-feed-tokens` | `secret/nauta` | `kalya_feed_tokens` |
| `nauta/symbiosis-hcm-token` | `secret/nauta` | `symbiosis_hcm_token` |
| `angelia/courier-producer-keys` | `secret/angelia` | `courier_producer_key_alarms`, `courier_producer_key_enclii_ops`, `courier_producer_key_tulana`, `courier_producer_key_madfam_site` |
| `angelia/courier-channel-tokens` | `secret/angelia` | `courier_telegram_bot_token`, `courier_slack_bot_token` |
| `angelia/courier-alertmanager` | `secret/angelia` | `courier_alertmanager_secret` |

**Angelia OWNS all three Courier targets** (verifier-owns): Angelia verifies every
one of these credentials, so `secret/angelia` is their single writable home, and
producers read their own `courier_producer_key_<producer>` cross-path through
their own ExternalSecret rather than holding a second copy that drifts on
rotation. Same shape as the `symbiosis-hcm` note below. `courier_alertmanager_secret` is
read cross-path by this repo's Alertmanager in the `monitoring` namespace
(`infra/k8s/production/monitoring/alertmanager-courier-secret.externalsecret.yaml`).
Until the targets are populated every Courier route answers `503
not_provisioned` and Alertmanager's `email_configs` carry alerts on their own,
which is what happens today.

`symbiosis-hcm` is the **producer** of the absence feed; `crea-map` cross-reads
`map_absence_feed_key` and consumes it as `HCM_FEED_API_KEY`. One copy at the
producer's path, read by both — not two copies that drift on rotation.

Add targets via PR to the registry — do not hardcode paths in runbooks. A new
`vault_path` also needs its block in `scripts/provision-switchyard-vault-writer.sh`;
`scripts/check-intake-policy-parity.sh` fails CI if you forget, because the
failure otherwise surfaces days later as an opaque `500: failed to write to Vault`.

Merging the policy block is only half of it: the running Vault changes when an
operator re-applies the policy with
`POLICY_ONLY=1 scripts/provision-switchyard-vault-writer.sh` (Vault admin token,
in-cluster against `vault-0`). A path in git but not re-applied 403s exactly like
a path that was never added. See
[Vault Operations](./VAULT_OPERATIONS.md#switchyard-vault-writer-secret-intake--vault-backfill).

Not every policy path has a target. `secret/kalya` is **policy-only**: nothing
intakes it, but `enclii secrets provision kalya-feed` reads
`secret/kalya:internal_api_key` to authorize minting the feed token before
writing `secret/crea-map` and `secret/nauta`. The parity check scans
switchyard-api Go sources too, so a provisioner's Vault literal cannot drift out
of the policy either.

## Server-side generation

For a shared internal key that **no human needs to read** — a smoke-gate key, a
service-to-service token — do not generate it yourself and paste it. Ask
Switchyard to mint it:

```bash
enclii secrets intake submit crea-map/internal-api-key \
  --generate internal_api_key \
  --reason "MAP smoke gate bootstrap"
```

The value is drawn from `crypto/rand` inside Switchyard (32 bytes, unpadded
base64url), merged into Vault on the same path as a supplied value, and **never
returned by any endpoint** — not by submit, not by status, not in logs. The
intake record names the key in `keys_generated` and the ESO annotation records
`enclii.dev/secret-intake-source: generated`; the value exists only in Vault.

Because nothing outside Vault ever holds a copy, rotation is a re-run of the same
command, and there is no scrollback, clipboard, or password manager to clean up.

Mix generated and supplied keys on one target when only some values are secrets
nobody should see:

```bash
enclii secrets intake submit symbiosis-hcm/map-absence-feed \
  --generate map_absence_feed_key \
  --reason "HCM absence feed bootstrap"
# prompts (masked) for map_absence_feed_url only
```

A key cannot be both generated and supplied — that is a `400`, not a silent
preference for one of them. `--generate` rejects any key the target does not
declare. Entropy defaults to 32 bytes and can be raised per target with a
`generate: {bytes: N}` block in the registry (16–128).

### After intake: ESO → reloader

Vault is written; the running pods are not. The rest of the chain is:

1. **Switchyard** annotates the target's ExternalSecret with `force-sync`, so
   External Secrets Operator re-reads Vault. Check `external_secret_refreshed`
   in the intake status — `false` means the annotation was skipped (no
   `external_secret` in the registry, or the name does not resolve) and ESO will
   only pick the value up on its own refresh interval.
2. **ESO** projects the Vault properties into the Kubernetes Secret. Remember it
   is all-or-nothing per ExternalSecret: one `property:` it cannot find syncs
   **zero** keys.
3. **Reloader** restarts the workloads that mount the Secret, so the new value
   reaches the process environment.

Until step 3 lands, the pods still hold the previous value.

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
| `POST` | `/v1/secrets/intake` | Write-only; body has values once, or `generate: ["key"]` for server-side minting |
| `GET` | `/v1/secrets/intake/:id` | Status/metadata only |

Errors: `503 vault_writer_disabled` (no `vault-credentials`), `404 unknown_target`,
`400 invalid_values`, `400 invalid_generate` (key not declared by the target, or
listed both in `values` and `generate`).

## Agent flow

1. Request operator run intake for a registry target.
2. Poll `enclii secrets intake status <id>` or `GET /v1/secrets/intake/:id`.
3. Run downstream ops (ESO sync, bootstrap scripts) — never request the value.

## Related

- [Secrets Management](../infrastructure/SECRETS_MANAGEMENT.md)
- [Vault Operations](./VAULT_OPERATIONS.md)
- [CLI: secrets intake](../cli/commands/secrets.md#enclii-secrets-intake)
- Private bridge gaps: `internal-devops/runbooks/2026-06-15-vault-bridge-gaps.md`
