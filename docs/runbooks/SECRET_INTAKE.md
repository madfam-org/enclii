# Secret Intake (chat-safe credential handoff)

**Last Updated:** 2026-10-03

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
>
> **Boundary checkpoint (2026-09-24, platform on-call):** The creator-census
> section adds target ids, key NAMES, a public redirect URI and a Janua client
> NAME only. No client id, secret or session value appears here; the client id
> is read from the operator's own `provision oidc` output.
>
> **Boundary checkpoint (2026-10-01, platform on-call):** The family-history
> section adds target ids, key NAMES, a public redirect URI and a Janua client
> NAME only. No client id, secret, session value or allowlist entry appears
> here; the allowlist holds personal data and lives only in Vault.
>
> **Boundary checkpoint (2026-10-03, platform on-call):** The digital-twins
> machine-edges section adds target ids, key NAMES, Vault paths, planned
> ExternalSecret names, Janua client NAMES, audiences, scopes and one
> organization slug only. No client id or secret appears here; the provisioner
> files both straight into Vault and prints only non-secret fields.

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
| `angelia/courier-database-url` | `secret/angelia` | `courier_database_url` |
| `angelia/courier-webhook-signing-keys` | `secret/angelia` | `courier_webhook_signing_key_alarms`, `courier_webhook_signing_key_enclii_ops`, `courier_webhook_signing_key_tulana`, `courier_webhook_signing_key_madfam_site` |
| `creator-census/web-oidc` | `secret/creator-census` | `janua_client_secret` |
| `creator-census/web-session` | `secret/creator-census` | `session_secret` |
| `family-history/api-access` | `secret/family-history` | `fh_early_access_allowlist` |
| `family-history/web-oidc` | `secret/family-history` | `auth_janua_client_id`, `auth_janua_client_secret` |
| `family-history/web-session` | `secret/family-history` | `fh_session_secret` |
| `pravara-mes/yantra4d-step-reader` | `secret/pravara-mes` | `yantra4d_step_reader_client_id`, `yantra4d_step_reader_client_secret` |
| `pravara-mes/asset-shells-publisher-madfam-ecosystem` | `secret/pravara-mes` | `asset_shells_publisher_madfam_ecosystem_client_id`, `asset_shells_publisher_madfam_ecosystem_client_secret` |
| `pravara-mes/fabrication-prep-client` | `secret/pravara-mes` | `fabrication_prep_client_id`, `fabrication_prep_client_secret` |
| `yantra4d/asset-shells-publisher` | `secret/yantra4d` | `asset_shells_publisher_client_id`, `asset_shells_publisher_client_secret` |
| `fashion-cabinet/asset-shells-publisher` | `secret/fashion-cabinet` | `asset_shells_publisher_client_id`, `asset_shells_publisher_client_secret` |
| `forj/pravara-intake` | `secret/forj` | `pravara_intake_client_id`, `pravara_intake_client_secret` |
| `digifab-quoting/pravara-intake` | `secret/digifab-quoting` | `pravara_intake_client_id`, `pravara_intake_client_secret` |
| `zavlo/cfdi-emitter` | `secret/zavlo` | `zavlo_cfdi_emitter_client_id`, `zavlo_cfdi_emitter_client_secret` |
| `routecraft/billing-relay` | `secret/routecraft` | `billing_relay_client_id`, `billing_relay_client_secret` |
| `voxa/selva-client` | `secret/voxa` | `selva_client_id`, `selva_client_secret` |

**Angelia OWNS all five Courier targets** (verifier-owns): Angelia verifies every
one of these credentials, so `secret/angelia` is their single writable home, and
producers read their own `courier_producer_key_<producer>` cross-path through
their own ExternalSecret rather than holding a second copy that drifts on
rotation. Same shape as the `symbiosis-hcm` note below. `courier_alertmanager_secret` is
read cross-path by this repo's Alertmanager in the `monitoring` namespace
(`infra/k8s/production/monitoring/alertmanager-courier-secret.externalsecret.yaml`).
Until the targets are populated every Courier route answers `503
not_provisioned` and Alertmanager's `email_configs` carry alerts on their own,
which is what happens today.

`angelia/courier-webhook-signing-keys` (R23, ruled 2026-09-05) is the arming step
for Courier's `webhook` channel — customer-supplied HTTPS targets, signed
HMAC-SHA256 per producer. The property **suffix is the pairing mechanism**:
`courier_webhook_signing_key_<suffix>` pairs with the same producer's
`courier_producer_key_<suffix>`, and angelia assembles the set by prefix into
`COURIER_WEBHOOK_SIGNING_KEYS`. A producer with no signing key still
authenticates and is refused `503 not_provisioned` on that channel alone. **Run
this target only after O21** (one real page delivered through Courier) — R23 is
sequenced behind it, and running it earlier arms a channel nothing has verified.

`angelia/courier-database-url` (2026-09-23) is Courier Part A, the durable
ledger. It carries a connection string with the angelia role's password, so
submit it with `--value-file` and never with `--generate`. The value is inert
until angelia-api runs with `COURIER_LEDGER=postgres`. With that env set and the
value absent, the api refuses to start. Apply angelia's ledger migrations
0001–0004 before setting the env (angelia `docs/runbooks/courier-provisioning.md`,
Step 0).

### creator-census web (2026-09-24)

The census web (`cc-app.madfam.io`) signs in through Janua with a confidential
client. Its Deployment mounts Secret `creator-census-web` (`optional: true`) for
`JANUA_CLIENT_SECRET` and `SESSION_SECRET` and refuses sign-in until both exist.
`JANUA_CLIENT_ID` is plain Deployment config, not a secret.

The two secrets are **two targets on one path and one ExternalSecret**. The CLI
prompts for every key of a target that is not in `--generate`, so one target
holding both keys would make `--generate session_secret` ask for the Janua
client secret, which no human holds. Each property has exactly one write route:

| Property in `secret/creator-census` | Written by | Census env var |
|---|---|---|
| `janua_client_secret` | `enclii secrets provision oidc --platform creator-census-web` | `JANUA_CLIENT_SECRET` |
| `session_secret` | `enclii secrets intake submit creator-census/web-session --generate session_secret` | `SESSION_SECRET` |

The intake API lowercases every key before the Vault merge, so the census
ExternalSecret must reference the **lowercase** `property:` names above. An
upper-case `property:` syncs zero keys (ESO is all-or-nothing per
ExternalSecret).

Owner sequence, after the registry change is deployed and the Vault policy
re-applied (`ASSERT_PATH=creator-census bash
scripts/apply-switchyard-vault-policy-remote.sh`):

```bash
export ENCLII_API_ENDPOINT=https://api.enclii.dev
enclii login   # admin@madfam.io
# Until a CLI release embeds creator-census-web, pass the registry from a main checkout:
enclii secrets provision oidc --platform creator-census-web \
  --registry config/ecosystem-oidc-provision.yaml \
  --reason "creator-census web sign-in" --dry-run
enclii secrets provision oidc --platform creator-census-web \
  --registry config/ecosystem-oidc-provision.yaml \
  --reason "creator-census web sign-in"
# prints: ✓ creator-census-web client_id=jnc_… created=true intake=int_…
enclii secrets intake submit creator-census/web-session \
  --generate session_secret --reason "creator-census web session secret"
enclii secrets intake status int_<id>
```

Janua generates the client id (`jnc_` plus a random suffix) on the first run.
Nothing derives it from `client_key`. Copy it from the `client_id=` field of the
provision output into the census Deployment's `JANUA_CLIENT_ID`. Then pin it as
`janua_client.client_id` in `config/ecosystem-oidc-provision.yaml` (both copies)
so that later runs reconcile the client and never create a second one.
`external_secret_refreshed: false` is expected on both intakes until the census
repo creates the `creator-census-web` ExternalSecret. ESO syncs the ExternalSecret
when it is created, so create it only after both properties are in Vault.

### family-history (2026-10-01)

family-history is a public product: the web (`fh-app.madfam.io`, landing
`fh.madfam.io`) signs in through Janua with the confidential client
`family-history-web`, and the API validates bearers for audience
`family-history-api`. Its three Vault properties share one path,
`secret/family-history`, and reach two ExternalSecrets in the `family-history`
namespace. The database URLs are not in Vault. Like creator-census, they come
from the onboarding project Secret `family-history-secrets`.

| Property in `secret/family-history` | Target | Written by | ExternalSecret | Env var |
|---|---|---|---|---|
| `fh_early_access_allowlist` | `family-history/api-access` | operator, masked prompt | `family-history-api` | `FH_EARLY_ACCESS_ALLOWLIST` |
| `auth_janua_client_id` | `family-history/web-oidc` | `enclii secrets provision oidc --platform family-history-web` | `family-history-web` | `AUTH_JANUA_CLIENT_ID` |
| `auth_janua_client_secret` | `family-history/web-oidc` | `enclii secrets provision oidc --platform family-history-web` | `family-history-web` | `AUTH_JANUA_CLIENT_SECRET` |
| `fh_session_secret` | `family-history/web-session` | `enclii secrets intake submit family-history/web-session --generate fh_session_secret` | `family-history-web` | `FH_SESSION_SECRET` |

Unlike creator-census, the provisioner files the client **id** as well as the
secret (nauta-style), so the id never sits in the public repository. The
allowlist is a comma-separated list of Janua subjects or emails. It must not be
empty: an empty value fails closed and nobody gets in. The production client
has no localhost redirect URI; local development registers its own client.

Owner sequence, after the registry change is deployed and the Vault policy
re-applied (`ASSERT_PATH=family-history bash
scripts/apply-switchyard-vault-policy-remote.sh`):

```bash
export ENCLII_API_ENDPOINT=https://api.enclii.dev
# Creating a Janua client needs a Janua admin session (`--profile admin`).
# Until a CLI release embeds family-history-web, pass the registry from a main checkout:
enclii --profile admin secrets provision oidc --platform family-history-web \
  --registry config/ecosystem-oidc-provision.yaml \
  --reason "family-history web sign-in" --dry-run
enclii --profile admin secrets provision oidc --platform family-history-web \
  --registry config/ecosystem-oidc-provision.yaml \
  --reason "family-history web sign-in"
# prints: ✓ family-history-web client_id=jnc_… created=true intake=int_…
enclii secrets intake submit family-history/web-session \
  --generate fh_session_secret --reason "family-history web session secret"
enclii secrets intake submit family-history/api-access \
  --reason "family-history early-access allowlist"   # masked prompt
enclii secrets intake status int_<id>
```

Then pin the printed `jnc_…` id as `janua_client.client_id` for
`family-history-web` in `config/ecosystem-oidc-provision.yaml` (both copies) so
later runs reconcile the client instead of creating a second one. ESO syncs
each ExternalSecret all-or-nothing, so `family-history-web` stays NotReady until
both `web-oidc` and `web-session` are written, and `family-history-api` until
`api-access` is.

### Digital-twins machine edges (2026-10-03)

Nine Janua `client_credentials` clients, one per machine edge. No human types,
sees or stores either value: `enclii secrets provision oidc` creates or rotates
each client through the operator's Janua platform-admin session and files the
id/secret pair straight into the consumer's Vault path. All are confidential,
`client_credentials` only, with no redirect URIs. The three org-bound clients are
bound to the MADFAM Ecosystem organization (slug `madfam-ecosystem`), which Janua
names `<template>.<org slug>`.

| Platform id | Janua client | Audience | Scopes | Target | ExternalSecret (planned) |
|---|---|---|---|---|---|
| `pravara-yantra4d-step-reader` | `pravara-yantra4d-step-reader` | `yantra4d-api` | `yantra4d:render` | `pravara-mes/yantra4d-step-reader` | `pravara-service-clients` |
| `pravara-fabrication-prep` | `pravara-fabrication-prep` | `fabrication-prep-api` | `fabrication-prep:slice` | `pravara-mes/fabrication-prep-client` | `pravara-service-clients` |
| `pravara-asset-shells-publisher-madfam-ecosystem` | `pravara-asset-shells-publisher.madfam-ecosystem` | `asset-shells-api` | `asset-shells:publish-instances`, `asset-shells:read` | `pravara-mes/asset-shells-publisher-madfam-ecosystem` | `pravara-service-clients` |
| `yantra4d-asset-shells-publisher` | `yantra4d-asset-shells-publisher` | `asset-shells-api` | `asset-shells:publish-types` | `yantra4d/asset-shells-publisher` | `yantra4d-service-clients` |
| `fashion-cabinet-asset-shells-publisher` | `fashion-cabinet-asset-shells-publisher` | `asset-shells-api` | `asset-shells:publish-types` | `fashion-cabinet/asset-shells-publisher` | `fashion-cabinet-service-clients` |
| `forj-pravara-intake-madfam-ecosystem` | `forj-pravara-intake.madfam-ecosystem` | `pravara-api` | `pravara-mes:jobs` | `forj/pravara-intake` | `forj-service-clients` |
| `cotiza-pravara-intake-madfam-ecosystem` | `cotiza-pravara-intake.madfam-ecosystem` | `pravara-api` | `pravara-mes:jobs` | `digifab-quoting/pravara-intake` | `digifab-quoting-service-clients` |
| `zavlo-cfdi-emitter` | `zavlo-cfdi-emitter` | `karafiel-api` | `cfdi:issue` | `zavlo/cfdi-emitter` | `zavlo-service-clients` |
| `routecraft-billing-relay` | `routecraft-billing-relay` | `dhanam-api` | `billing:events` | `routecraft/billing-relay` | `routecraft-service-clients` |

Each consumer reads its pair from a **dedicated** `<app>-service-clients` Secret
owned by its own ExternalSecret, never from a hand-maintained Secret: an Owner
ExternalSecret over an existing Secret deletes every key it does not produce.
The consumer repositories add those ExternalSecrets. Until one exists, intake
still merges the pair into Vault and reports `external_secret_refreshed: false`;
that is expected, and ESO reads the pair on its first sync.

An existing client is matched by name and its secret is **rotated**, because a
stored secret is never retrievable. `--grace-hours 0` retires the previous secret
at once (see [`--grace-hours`](../cli/commands/secrets.md#enclii-secrets-provision-oidc));
use it only when no live consumer depends on that secret.
`pravara-fabrication-prep` needs Janua to reserve audience `fabrication-prep-api`
and scope `fabrication-prep:slice` first; until then Janua refuses its creation and
the loop stops at it.

Owner sequence, after the registry change is deployed (switchyard-api digest bump)
and from an up-to-date checkout:

```bash
cd ~/labspace/enclii && git pull --ff-only
cd ~/labspace/enclii && ASSERT_PATH=pravara-mes bash scripts/apply-switchyard-vault-policy-remote.sh < ~/.config/madfam/vault-admin.token
# expect: APPLIED_OK_asserted_path_present
make -C ~/labspace/enclii install-cli CLI_INSTALL_DIR=$HOME/.local/bin
enclii login --profile admin   # only if the admin session expired
for p in pravara-yantra4d-step-reader yantra4d-asset-shells-publisher fashion-cabinet-asset-shells-publisher zavlo-cfdi-emitter routecraft-billing-relay forj-pravara-intake-madfam-ecosystem cotiza-pravara-intake-madfam-ecosystem pravara-asset-shells-publisher-madfam-ecosystem pravara-fabrication-prep; do enclii secrets provision oidc --profile admin --platform "$p" --grace-hours 0 --json --reason "digital-twins machine edges" || break; done
```

The JSON carries no secret: `janua_client_id`, `created`, `rotated_secret`,
`grace_period_hours`, `old_secrets_expire_at`, `intake_id` and the key names. Then
pin each printed `jnc_…` id as `janua_client.client_id` in
`config/ecosystem-oidc-provision.yaml` (both copies) so later runs reconcile the
pinned client.

### Voxa → Selva inference edge (2026-10-04)

One more Janua `client_credentials` client, same mechanics as the digital-twins
edges above: `voxa-selva` (Janua client `voxa-selva`, audience `selva-office`,
scope `selva:infer`, org-bound to `madfam-ecosystem`), filed at
`voxa/selva-client` → `secret/voxa` and delivered by a planned, dedicated
`voxa-service-clients` ExternalSecret in the `voxa` namespace that maps
`selva_client_id` / `selva_client_secret` to `SELVA_CLIENT_ID` /
`SELVA_CLIENT_SECRET`. Voxa's API ignores the pair until `SELVA_ENABLED=true`,
and every request it sends is `X-Sensitivity: restricted`, so Selva must have
its local model backend before turning it on is useful.

Owner sequence, after the registry change is deployed (switchyard-api digest
bump) and from an up-to-date checkout:

```bash
cd ~/labspace/enclii && git pull --ff-only
cd ~/labspace/enclii && ASSERT_PATH=voxa bash scripts/apply-switchyard-vault-policy-remote.sh < ~/.config/madfam/vault-admin.token
# expect: APPLIED_OK_asserted_path_present
make -C ~/labspace/enclii install-cli CLI_INSTALL_DIR=$HOME/.local/bin
enclii login --profile admin   # only if the admin session expired
enclii secrets provision oidc --profile admin --platform voxa-selva --json --reason "voxa word suggestions through selva (restricted)"
```

Then pin the printed `jnc_…` id as `janua_client.client_id` for `voxa-selva` in
both registry copies.

 `crea-map` cross-reads
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
