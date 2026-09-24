---
title: providers
description: Audited MADFAM provider workflows for GitHub, Cloudflare, Porkbun, Hetzner, and Resend
---

# `enclii providers`

`enclii providers` is the contract-first replacement layer for direct `gh`,
Cloudflare, Porkbun, Hetzner, and Resend tooling in MADFAM operations.

Mutating commands are dry-run by default. Use `--apply --reason "..."` only
when the corresponding provider adapter is wired and the audit reason is clear.

:::note `--tenant` needs CLI `v1.0.0-alpha.9` or later

`--tenant` and `providers porkbun ping` landed in
[#527](https://github.com/madfam-org/enclii/pull/527) and first shipped in
**`v1.0.0-alpha.9`**. On `v1.0.0-alpha.8` and older the flag is rejected as
unknown and `ping` does not exist. Check with `enclii version 2>&1` and upgrade
from the [releases page](https://github.com/madfam-org/enclii/releases).

:::

Read-only commands call live Switchyard adapters when configured. Current first
coverage includes GitHub workflow runs, repository Actions secrets, GHCR package
metadata/versions, branch protection, Cloudflare DNS, Cloudflare tunnel status,
Cloudflare DNS apply for zones Enclii controls, tunnel route inventory, Porkbun
domain inventory, Porkbun DNS reads, Porkbun DNS create fallback, Porkbun
renewal reads, Porkbun credential/ping checks, Porkbun auto-renew apply, and
Porkbun nameserver apply (against MADFAM's registrar account or, with
`--tenant`/`--project`, a client's own). Hetzner, Cloudflare Access, and R2
remain contract surfaces until their adapters are wired; missing coverage
returns `adapter_unconfigured`.

## Commands

| Command | Purpose |
|---------|---------|
| `enclii providers capabilities` | List server-supported provider capabilities |
| `enclii providers github runs|rerun|cancel|secrets|packages|protection` | GitHub Actions, repo secrets, GHCR, branch protection |
| `enclii providers cloudflare credentials|zones|zone-add-apply|zone-settings-apply|dns|dns-apply|tunnels|tunnels-apply|access|r2|hostnames` | Credential readiness, zones, DNS, tunnels, Access, R2, custom hostnames |
| `enclii providers porkbun credentials|ping|domains|dns|dns-apply|renewals|nameservers|nameservers-apply|auto-renew-apply` | Domain inventory, DNS create fallback, renewal state, auto-renew, registrar delegation — per registrar account |
| `enclii providers hetzner nodes|lb|vswitch|storage|firewall` | Robot/Cloud nodes, DR LB, vSwitch, storage boxes, firewall |
| `enclii providers resend credentials|domains|domain|domain-add-apply|domain-dns-apply|domain-verify-apply|emails|send-test-apply` | Resend transactional email: API key and sender readiness, domain inventory and DNS records, register a domain, apply its DNS records via Cloudflare, trigger verification, recent sends, test send |

## Examples

```bash
enclii providers capabilities
enclii providers github runs madfam-org/digifab-quoting --json
enclii providers github packages madfam-org/enclii --json
enclii providers cloudflare zones --json
enclii providers cloudflare zone-add-apply kalya.app --apply --reason "create Cloudflare zone for newly acquired apex"
enclii providers cloudflare zone-settings-apply kalya.app --apply --reason "apply Enclii HTTPS posture to kalya.app"
enclii providers cloudflare dns cotiza.studio
enclii providers cloudflare dns-apply app.example.com --project example --service web --apply --reason "point app host at Enclii tunnel"
enclii providers cloudflare tunnels --json
enclii providers cloudflare tunnels-apply --project example --apply --reason "reconcile junction tunnel routes to correct K8s backends"
enclii providers porkbun dns-apply crm.phynd.app --domain phynd.app --type CNAME --content c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com --apply --reason "restore PhyndCRM app host through Enclii"
enclii providers porkbun nameservers-apply phynd.app --nameservers ns1.cloudflare.com,ns2.cloudflare.com --apply --reason "delegate phynd.app to Enclii-managed Cloudflare"
enclii providers porkbun ping --tenant crea
enclii providers porkbun domains --tenant crea
enclii providers porkbun nameservers-apply creatumundo.mx --tenant crea --nameservers <NS1>,<NS2> --apply --reason "delegate the client apex to its Enclii-managed Cloudflare zone"
enclii providers github rerun 25430873929 --apply --reason "re-run after GHCR token scope fix"
```

## Required Mutation Flags

| Flag | Description |
|------|-------------|
| `--apply` | Execute instead of returning a dry-run plan |
| `--reason` | Audit reason; required with `--apply` |
| `--idempotency-key` | Optional retry key for safely repeating an operation |
| `--project`, `--service` | Enclii scope selectors passed to the operation contract |
| `--namespace`, `-n` | Kubernetes or provider namespace scope |
| `--tenant` | Ecosystem tenant scope; selects per-tenant provider credentials |
| `--json` | Emit machine-readable JSON |

Command-specific flags: `porkbun dns-apply` takes `--domain`, `--name` (both derived from the target when omitted), `--type` (default `CNAME`), `--content` (default: the Enclii tunnel CNAME), and `--ttl`; `porkbun auto-renew-apply` requires `--auto-renew on|off`; `porkbun nameservers-apply` takes `--nameservers`.

## Remaining Adapter Work

- GitHub `rerun` and `cancel` remain contract-only.
- GitHub `packages` now reads GHCR package metadata and recent versions; write
  operations for package visibility/deletion are intentionally out of scope.
- Cloudflare `zones` reads the account's zone inventory. `zone-add-apply`
  creates the zone for an apex domain, and `zone-settings-apply` applies
  Enclii's HTTPS posture to it. For a newly acquired apex the order is
  `zone-add-apply` -> registrar delegation (`providers porkbun
  nameservers-apply`) -> `zone-settings-apply` -> `dns-apply`.
- Cloudflare `dns-apply` creates, updates, or no-ops DNS records when the target
  zone is visible to the configured Enclii Cloudflare account. It blocks with
  `blocked_by_dns_authority` when the apex zone still needs registrar
  delegation/import. For `TXT`/`MX`/`NS`/`SRV`, a record's identity **includes
  its content**, so several records can coexist at one name: an apply whose
  content differs from what is already there ADDS a record rather than
  overwriting one ([#530](https://github.com/madfam-org/enclii/issues/530),
  fixed in [#536](https://github.com/madfam-org/enclii/pull/536)). See
  [Cloudflare DNS apply](#cloudflare-dns-apply).
- Cloudflare `tunnels-apply` reconciles junction hostnames to the correct in-cluster service URL using `resolveServiceNamespace`; use instead of `junctions add` when live tunnel routes drift.
- Cloudflare `access` and `r2` remain contract-only.
- Cloudflare `hostnames` currently reads DNS-shaped state; full SaaS custom
  hostname inventory is a follow-up.
- Porkbun `dns-apply` currently supports idempotent create/no-op semantics and
  blocks on existing records with different content until explicit update/delete
  support is added.
- Porkbun `nameservers-apply` supports registrar delegation updates through the
  Porkbun credentials of whichever registrar account the operation is scoped to.
- Porkbun credentials are per Porkbun **account**. Domains a client holds in the
  client's own Porkbun login are unreachable with the estate's global key —
  Porkbun answers `INVALID_DOMAIN`, which reads like a typo. Scope such
  operations with `--tenant <id>` or `--project <slug>`; see
  [Porkbun per-tenant credentials](/infrastructure/porkbun-tenant-credentials).
- Porkbun domain `renew` is deliberately not wired: it spends account credit and
  requires the caller to pass the exact current price, so it stays a dashboard
  action. `auto-renew-apply` covers the case that actually causes outages.
- Hetzner surfaces are declared but not yet backed by clients.

## Cloudflare DNS apply

Apply or dry-run Cloudflare DNS through Enclii instead of provider UI/API calls.

```bash
enclii providers cloudflare dns-apply app.example.com --type CNAME --content <TUNNEL_CNAME>
enclii providers cloudflare dns-apply app.example.com --type CNAME --proxied true --apply --reason "route customer domain through Enclii"
```

Without `--apply`, the command requests a dry-run plan. With `--apply`, `--reason` is required.

### Several records of one type at one name

A name can hold more than one `TXT`, `MX`, `NS`, or `SRV` record, and usually
should: an apex `TXT` name carries an SPF record *plus* every provider's
verification token, and mail providers ship an `MX` pair. For those four types
a record's identity is **name + type + content**, so:

| Live state at that name+type | Plan |
| --- | --- |
| nothing | `create` |
| same content, same proxied/priority | `noop` |
| different content | `create` — the existing records are untouched |
| different content, with `--replace` | `update` — overwrites one existing record |

The dry-run plan is what the apply executes; both read the live set once and
decide from the same function. The response names every record already at that
name (`existingRecordsAtName`) so an add shows what it is joining and a
`--replace` shows what it is choosing between.

`CNAME` (and `A`/`AAAA`) keep single-record semantics: a `dns-apply` against a
name that already has one is an `update` of that record, which is what
repointing a host means.

```bash
# Proton verification TXT and SPF TXT coexist at the apex — no --replace, no dashboard.
enclii providers cloudflare dns-apply example.com --type TXT \
  --content 'protonmail-verification=<token>' --apply --reason "Proton domain ownership"
enclii providers cloudflare dns-apply example.com --type TXT \
  --content 'v=spf1 include:_spf.protonmail.ch ~all' --apply --reason "Proton SPF"
```

`--replace` is the deliberate, auditable way to overwrite a record's value — a
rotated verification token, say. It overwrites exactly one record and warns
about both the value it destroys and the records at that name it leaves alone.
Like `--allow-pending-zone`, it accepts only a literal `true`.

:::warning Fixed in #536 — read this if you are following an older runbook

Before [#536](https://github.com/madfam-org/enclii/pull/536), a record was keyed
by name + type only, so a second `TXT` or `MX` at one name was applied as a
destructive `update` of the first: the dry-run said `create`, the apply said
`updated`, and the earlier record was gone
([#530](https://github.com/madfam-org/enclii/issues/530) — on 2026-09-07 an SPF
TXT destroyed a live Proton ownership TXT). Any runbook step that says "add the
second TXT/MX in the Cloudflare dashboard as break-glass" is obsolete; use
`dns-apply`.

:::

### MX priority

`--priority` is a first-class flag. The pre-#536 form — the preference typed
into `--content` as `"10 host"` — still works and is split back out server-side,
so existing runbooks keep working; `--priority` wins when both are given. The
response reports which was used (`priority_source: argument | content`).

```bash
# The standard provider MX pair, both through Enclii.
enclii providers cloudflare dns-apply example.com --type MX --priority 10 \
  --content mail.protonmail.ch --apply --reason "primary MX"
enclii providers cloudflare dns-apply example.com --type MX --priority 20 \
  --content mailsec.protonmail.ch --apply --reason "backup MX"

# Equivalent, back-compat form.
enclii providers cloudflare dns-apply example.com --type MX \
  --content '10 mail.protonmail.ch' --apply --reason "primary MX"
```

An `MX`/`SRV` create with no priority at all defaults to `10` and says so
(`priority_defaulted: true`). A priority that is not a number in `0..65535` is
refused as `invalid_request` rather than silently defaulted. Priority `0` is a
legal preference and is preserved.

Before #536, an MX apply reached Cloudflare with no `priority` field at all —
Cloudflare rejects that with HTTP 400, and every provider error below the
handler was rendered as `502`, so the apply failed deterministically with no
message. That is fixed; see below for how errors read now.

### Reading a failure

`dns-apply` no longer answers `502` for a record Cloudflare refused:

| Status | Meaning |
| --- | --- |
| `400 invalid_request` | an argument was malformed (e.g. a non-numeric `--priority`) |
| `422 provider_apply_failed` | Cloudflare rejected the record; the summary and warnings carry Cloudflare's own message |
| `409 provider_apply_failed` | a record with that content already exists at that name |
| `424 blocked_by_dns_authority` | the zone is not delegated/visible, or the token cannot see it |
| `502 provider_read_failed` / `provider_apply_failed` | the provider was genuinely unreachable or answered something unparseable — a `502` still does not say whether the write landed, so re-read the zone before retrying |

Worked example (Proton Mail, plus coexistence with Resend), the verified brand-host
onboarding sequence, and the post-NS-switch resolver caveat:
[Domain and email DNS onboarding](/runbooks/DOMAIN_AND_EMAIL_DNS_ONBOARDING).

## Cloudflare zone settings apply

Run after the zone goes **active** (a `pending` zone has no settings to read).
Applies Enclii's HTTPS posture as a set — `always_use_https=on`,
`automatic_https_rewrites=on`, `min_tls_version=1.2` — because they are only
meaningful together.

```bash
enclii providers cloudflare zone-settings-apply example.com --apply --reason "apply Enclii HTTPS posture"
curl -sSI http://example.com/ | head -1   # expect: HTTP/1.1 301 Moved Permanently
```

`zone_absent` in the dry-run means the zone was never created — run
`zone-add-apply` first. A setting reported `not-editable` is a zone-plan
limitation, not a failure.

## Cloudflare credential readiness

```bash
enclii providers cloudflare credentials --json
```

This is a contract-read surface for provider credential readiness. Treat it as advisory until the server-side endpoint returns concrete configured/missing provider keys.
