---
title: Porkbun per-tenant registrar credentials
description: How Enclii operates a registrar account that belongs to a client rather than to MADFAM
sidebar_position: 21
tags: [infrastructure, dns, porkbun, registrar, tenancy, secrets]
---

# Porkbun per-tenant registrar credentials

## The problem this solves

Porkbun API keys are scoped to **one Porkbun account**. Every Porkbun operation
Enclii shipped before this change authenticated with a single global pair —
`ENCLII_PORKBUN_API_KEY` / `ENCLII_PORKBUN_SECRET_API_KEY`, projected into
switchyard-api from `secret/enclii` — which belongs to MADFAM's own account.

That is correct for `madfam.io` and every other domain the estate registered
itself. It is *structurally unable* to touch a domain a client holds in the
client's own Porkbun login. The failure is not a permission error: Porkbun
answers **`INVALID_DOMAIN`**, which reads exactly like a typo. So "operate the
client's registrar through the platform API" was never a permission to be
granted — it was an impossible call.

`creatumundo.mx` is the live case. It was transferred into Crea Tu Mundo's own
Porkbun account on 2026-09-05. Enclii has already created its Cloudflare zone,
which stays `pending` until the nameservers move at the registrar — and that
move is precisely the call the global key cannot make.

## Credential resolution

Every Porkbun operation asks which account it is talking to before it does
anything else. Resolution order:

| # | Signal | Result |
|---|--------|--------|
| 1 | `--tenant <id>`, or `--project <slug>` whose slug a tenant claims | that tenant |
| 2 | the owning tenant of the operation's domain, by suffix | that tenant |
| 3 | nothing claims it | MADFAM's global credentials |

A resolved tenant then splits two ways:

- **Tenant declares no registrar binding** → its domains live in MADFAM's
  account, so the global credentials are used. The tenant is still named in the
  response, for the operator's benefit.
- **Tenant declares a tenant-owned registrar binding** → switchyard-api reads
  that tenant's key pair from its Vault path at request time.

Two rules make this safe:

**Explicit scope beats domain inference.** An operator who typed `--tenant crea`
and silently reached MADFAM's account would be told the domain does not exist,
with nothing on screen explaining why.

**A `--tenant`/`--project` value no tenant claims is REFUSED**, not treated as a
request for the global account. It is overwhelmingly a typo, and answering a
typo with MADFAM's key produces `INVALID_DOMAIN` for a domain that exists. The
error names the bad id and tells the operator to correct it — never to drop the
flag, which would be the wrong-account call.

**A tenant scope that cannot produce credentials FAILS.** It never falls back to
the global key. Falling back would send the operation to the wrong registrar
account and report `INVALID_DOMAIN` for a domain that plainly exists — the
single most confusing outcome available. The fallback in row 3 is for domains
nobody claims, never for a claim that could not be honoured.

## Why Vault at request time rather than another env var

The Cloudflare precedent — `h.config.CloudflareAPIToken`, projected by
ExternalSecrets into the pod — works because there is exactly **one** Cloudflare
account for the whole estate. Registrar accounts are per client and arrive
whenever a client onboards. An env-var pair per tenant would mean, for each new
client: an ExternalSecret edit, a config field, a redeploy, and a pod restart.

switchyard-api already holds a Vault client for exactly this shape of problem
(`h.vaultClient`; the kalya feed provisioner reads `secret/kalya` at request
time the same way). Using it means a new tenant is **a Vault write plus a
registry entry** — no restart, and no new deployment surface.

There is deliberately **no ExternalSecret** for these credentials. Nothing
should mount a client's registrar keys into a pod's environment.

## Where the pieces live

| Piece | Location |
|-------|----------|
| Tenant → registrar binding | `apps/switchyard-api/internal/ecosystem/tenants.json` |
| Resolver | `apps/switchyard-api/internal/api/porkbun_credential_scope.go` |
| Intake target (how the key pair reaches Vault) | `apps/switchyard-api/internal/secretsintake/registry.yaml` |
| Operator one-shot | `scripts/operator/porkbun-tenant-credentials.sh` |
| CLI verbs | `packages/cli/internal/cmd/providers.go` |
| Vault writer policy | `scripts/provision-switchyard-vault-writer.sh` |

:::warning The Vault policy is not applied by merging

`secret/crea` carries **read** capability as well as write, because unlike every
other intake path Switchyard reads it back on every registrar operation. CI's
intake/policy parity gate proves the block exists in git; it cannot prove the
policy was applied to the running Vault. Until an operator re-applies it, the
first `--tenant crea` call 403s and surfaces as "credentials missing" —
indistinguishable from never having run the intake.

The helper below takes the admin token **on stdin, never in argv**, so it never
reaches `ps`, shell history, or a log:

```bash
bash scripts/apply-switchyard-vault-policy-remote.sh   # prompts silently
```

It re-reads the live policy afterwards and prints
`APPLIED_OK_asserted_path_present` when `secret/data/crea` is really there. Set
`ASSERT_PATH=<path>` to prove a different one.

Directly, if you already have a shell with Vault reachable:

```bash
VAULT_TOKEN=<admin> POLICY_ONLY=1 bash scripts/provision-switchyard-vault-writer.sh
```

:::

The binding for CTM:

```json
{
  "id": "crea",
  "displayName": "Crea Tu Mundo",
  "domainSuffixes": ["creatumundo.mx"],
  "projects": ["crea-map", "nauta"],
  "registrar": {
    "provider": "porkbun",
    "account": "tenant",
    "vaultPath": "secret/crea",
    "apiKeyProperty": "porkbun_api_key",
    "secretKeyProperty": "porkbun_secret_key"
  }
}
```

The Vault path and property names here must match the intake registry entry
exactly. A mismatch resolves to "credentials missing" — never to a wrong-account
call.

## Loading a tenant's credentials

One command, run by a human operator. It prompts silently, sends the values
straight to Vault through Enclii, and verifies them against the live Porkbun
API. No agent, log, shell history, or terminal scrollback ever holds a value.

```bash
ENCLII_TENANT=crea VERIFY_DOMAIN=creatumundo.mx \
  scripts/operator/porkbun-tenant-credentials.sh
```

Equivalent by hand:

```bash
enclii secrets intake submit crea/porkbun-registrar \
  --reason "load CTM Porkbun registrar credentials"
enclii providers porkbun ping --tenant crea
```

### The one manual step that stays manual

In the **client's** Porkbun dashboard, per domain:

> Domain Management → the domain → Details → enable **API Access**

Porkbun refuses every API call for a domain that has not been opted in, and
reports it identically to a bad key. Nothing in Enclii can flip this toggle.

Two commands tell the failures apart:

- `enclii providers porkbun ping --tenant crea` — validates the key pair alone,
  naming no domain. Fails ⇒ wrong or mistyped key.
- `enclii providers porkbun renewals --tenant crea` — lists each domain with
  `apiAccess`. `0` ⇒ the toggle is off.

## CLI usage for CTM

Read-only, safe at any time:

```bash
enclii providers porkbun credentials --tenant crea   # which account, is it usable
enclii providers porkbun ping        --tenant crea   # validate the key pair live
enclii providers porkbun domains     --tenant crea   # what the account holds
enclii providers porkbun renewals    --tenant crea   # expiry, autoRenew, apiAccess
enclii providers porkbun nameservers creatumundo.mx --tenant crea
enclii providers porkbun dns         creatumundo.mx --tenant crea
```

`--project crea-map` and `--project nauta` resolve to the same account, so an
operator working in a project context does not have to learn a second
vocabulary.

Mutating verbs are dry-run by default; `--apply` requires `--reason`:

```bash
# Dry run first — always.
enclii providers porkbun nameservers-apply creatumundo.mx --tenant crea \
  --nameservers <ns1>,<ns2>

enclii providers porkbun nameservers-apply creatumundo.mx --tenant crea \
  --nameservers <ns1>,<ns2> \
  --apply --reason "delegate creatumundo.mx to its Enclii-managed Cloudflare zone"
```

```bash
enclii providers porkbun auto-renew-apply creatumundo.mx --tenant crea --auto-renew on
enclii providers porkbun auto-renew-apply creatumundo.mx --tenant crea --auto-renew on \
  --apply --reason "protect the client apex from lapsing"
```

Every response carries a `credentialScope` block naming the tenant, the account,
how the scope was decided, and the Vault path consulted — never a value.

## Verified end-to-end recipe (tenant `crea`, 2026-09-07)

This ran green against the live estate. Run the three steps in order — each one
fails in a way that looks like the previous step's problem if it is skipped.

```bash
# 1. Apply the Vault writer policy to the RUNNING Vault. Merging #527 did not
#    do this. Prompts silently for the admin token; nothing reaches argv.
bash scripts/apply-switchyard-vault-policy-remote.sh
#    → expect: APPLIED_OK_asserted_path_present

# 2. Load the tenant's Porkbun key pair into secret/crea, then verify it live.
ENCLII_TENANT=crea VERIFY_DOMAIN=creatumundo.mx \
  bash scripts/operator/porkbun-tenant-credentials.sh

# 3. Confirm through the CLI.
enclii providers porkbun credentials --tenant crea   # which account, is it usable
enclii providers porkbun ping        --tenant crea   # validate the key pair live
enclii providers porkbun domains creatumundo.mx --tenant crea
```

### CLI gotchas that cost time on the first run

**These verbs need CLI `v1.0.0-alpha.9` or later.** `--tenant` and `providers
porkbun ping` landed in [#527](https://github.com/madfam-org/enclii/pull/527)
and first shipped in **`v1.0.0-alpha.9`**. `v1.0.0-alpha.8` was cut from the
commit immediately before #527, so on alpha.8 the flag is rejected as unknown
and `ping` does not exist, which reads like a broken install. Check the binary
and upgrade from the [releases page](https://github.com/madfam-org/enclii/releases)
if it is older:

```bash
enclii version 2>&1
```

**`enclii whoami` prints on stderr.** `whoami`, `login`, and `logout` report
through cobra's `cmd.Println`, which writes to `OutOrStderr()`; the CLI never
calls `SetOut`. `enclii whoami > /tmp/who` therefore captures an empty file and
reads as "not logged in". Use `enclii whoami 2>&1` (`whoami` has no JSON output).

**A plain `enclii login` follows whichever Janua identity the browser session
holds.** The PKCE flow completes against the existing `auth.madfam.io` session,
and estate cookie precedence (janua J9) means a browser logged into a *client*
application resolves that identity. The CLI then receives the wrong one, and
every `--tenant` call afterwards fails on authorization rather than on anything
to do with the tenant. Since `v1.0.0-alpha.10`, choose the identity instead:
`enclii login --prompt select_account` shows Janua's account chooser, and
`enclii --profile admin login --no-browser --prompt login` keeps the operator
identity in its own profile (open the printed URL in a private window) without
touching the browser's session. Confirm with `enclii whoami 2>&1` (or
`enclii --profile admin whoami 2>&1`). See
[`enclii login`](../cli/commands/login.md#several-identities-profiles-and-account-switching).

## What is deliberately not wired

**Domain renewal.** Porkbun's `/domain/renew` spends account credit and requires
the caller to pass the exact current renewal price in pennies. An automated
apply would be a money mutation gated on a price the platform would have to
guess. Read `providers porkbun renewals` and renew in the dashboard.
`auto-renew-apply` covers the case that actually causes outages.

**DNS record update and delete.** Unchanged from before: `dns-apply` creates a
missing record and refuses to overwrite a conflicting one.

## Adding another client's registrar account

1. Add the tenant to `tenants.json` with its `domainSuffixes`, `projects`, and a
   `registrar` block pointing at its Vault path.
2. Add a matching intake target to `secretsintake/registry.yaml` — same path,
   same property names, and **no** `external_secret`.
3. Update the target-count assertions in
   `apps/switchyard-api/internal/secretsintake/registry_test.go`.
4. Add `secret/data/<path>` **and** `secret/data/<path>/*` blocks with
   `read` capability to `scripts/provision-switchyard-vault-writer.sh`. CI's
   intake/policy parity gate fails without this.
5. Deploy switchyard-api, re-apply the Vault policy (`POLICY_ONLY=1`), then run
   the one-shot with `ENCLII_TENANT=<id>`.

## Related

- [DNS Setup (Porkbun)](/infrastructure/dns-setup-porkbun)
- [Cloudflare Integration](/infrastructure/CLOUDFLARE)
- [Domain and email DNS onboarding](/runbooks/DOMAIN_AND_EMAIL_DNS_ONBOARDING)
