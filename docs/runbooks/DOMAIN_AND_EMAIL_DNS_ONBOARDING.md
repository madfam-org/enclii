---
title: Domain and email DNS onboarding
description: The verified operator sequence for bringing a brand host and a mail provider onto a zone Enclii hosts
tags: [runbook, dns, cloudflare, domains, email, tenancy]
---

# Domain and email DNS onboarding

> **Boundary checkpoint (2026-09-07, platform on-call):** Public-safe runbook.
> The domain and mail-provider hostnames here are the client's own public DNS
> and the providers' documented public endpoints; no node identity, no
> credential, and no tunnel identifier appears. Per-domain values an operator
> must supply — the Proton DKIM `<hash>`, verification tokens, the tunnel CNAME,
> `enclii-verification=<id>` — are placeholders, not values. Private
> operational detail and the onboarding sink live in `internal-devops`
> (2026-09-07 CTM tenant onboarding). Policy:
> `docs/PUBLIC_REPO_BOUNDARY.md` (repo-boundary contract).

What an operator actually has to do — and what currently bites — to bring a new
host, and a mail provider's record set, onto a zone Enclii hosts in Cloudflare.

Every step below was executed against `creatumundo.mx` on **2026-09-07** during
the CTM tenant onboarding. Read
[How `dns-apply` decides](#how-dns-apply-decides) before you plan a record set.

:::info This runbook changed with #536

The 2026-09-07 run hit
[#530](https://github.com/madfam-org/enclii/issues/530): a record was keyed by
name + type only, so a second `TXT` or `MX` at one name was applied as a
destructive `update` of the first, and every apex `TXT`/`MX` in the tables below
had to be added in the Cloudflare dashboard as break-glass.
[#536](https://github.com/madfam-org/enclii/pull/536) fixed that. **The whole
Proton and Resend record set now goes through `dns-apply`.** If you are working
from a copy of this runbook, or from notes, that says "dashboard — collides",
that copy predates #536.

:::

## How `dns-apply` decides

### Several records of one type at one name

For `TXT`, `MX`, `NS` and `SRV`, a record's identity is **name + type +
content**. That is the shape DNS actually has: an apex `TXT` name carries an SPF
record *plus* every provider's verification token, and mail providers ship an
`MX` pair.

| Live state at that name+type | Plan |
|---|---|
| nothing | `create` |
| same content, same proxied/priority | `noop` |
| different content | `create` — existing records untouched |
| different content, with `--replace` | `update` — overwrites one existing record |

```bash
# Both survive. No --replace, no dashboard.
enclii providers cloudflare dns-apply creatumundo.mx \
  --type TXT --content 'protonmail-verification=<token>' \
  --apply --reason "prove domain ownership to Proton"
# → created

enclii providers cloudflare dns-apply creatumundo.mx \
  --type TXT --content 'v=spf1 include:_spf.protonmail.ch ~all' \
  --apply --reason "publish SPF"
# → created (1 existing TXT record left in place)
```

The dry-run plan is what the apply executes; both read the live set once and
decide from the same function. The response lists every record already at that
name (`existingRecordsAtName`), so an add shows what it is joining.

`CNAME`, `A` and `AAAA` keep single-record semantics: applying to a name that
already has one is an `update`, which is what repointing a host means.

Use `--replace` only to overwrite a record's **value** — a rotated verification
token, say. It overwrites exactly one record and warns about both the value it
destroys and the records at that name it leaves alone. Like
`--allow-pending-zone` it accepts only a literal `true`.

Re-read authoritative state after any record-set change:

```bash
dig +short TXT creatumundo.mx @<one of the zone's Cloudflare nameservers>
```

### MX priority

`--priority` is a first-class flag. The older form — the preference inside
`--content` as `'10 host'` — still works and is split back out server-side;
`--priority` wins when both are given, and the response reports which was used.

```bash
enclii providers cloudflare dns-apply creatumundo.mx \
  --type MX --priority 10 --content mail.protonmail.ch \
  --apply --reason "primary MX for Proton Mail"

enclii providers cloudflare dns-apply creatumundo.mx \
  --type MX --priority 20 --content mailsec.protonmail.ch \
  --apply --reason "backup MX for Proton Mail"
```

A create with no priority defaults to `10` and says so. A priority that is not a
number in `0..65535` is refused as `invalid_request` rather than silently
defaulted. Priority `0` is legal and preserved.

The pre-#536 symptom — `--type MX --apply` answering `502 origin_bad_gateway`
while its own dry-run planned cleanly — was this path: the MX create carried no
`priority` field at all, Cloudflare answered HTTP 400, and every provider error
below the handler was rendered as `502`.

### Reading a failure

| Status | Meaning |
|---|---|
| `400 invalid_request` | a malformed argument (e.g. a non-numeric `--priority`) |
| `422 provider_apply_failed` | Cloudflare rejected the record; the message is Cloudflare's own |
| `409 provider_apply_failed` | a record with that content already exists at that name |
| `424 blocked_by_dns_authority` | the zone is not delegated/visible to the token |
| `502` | the provider was genuinely unreachable or unparseable |

A `502` still does not tell you whether the origin committed the write — re-read
the zone before retrying.

## Worked example: Proton Mail on a zone Enclii hosts

The full record set Proton requires. Since
[#536](https://github.com/madfam-org/enclii/pull/536) every row goes through
`dns-apply`; the "Notes" column records why each one used to need the dashboard.

| Name | Type | Content | Notes |
|------|------|---------|-------|
| `@` | TXT | `protonmail-verification=<token>` | coexists with the apex SPF TXT |
| `@` | MX | `mail.protonmail.ch`, `--priority 10` | the pair coexists |
| `@` | MX | `mailsec.protonmail.ch`, `--priority 20` | the pair coexists |
| `@` | TXT | `v=spf1 include:_spf.protonmail.ch ~all` | coexists with the verification TXT |
| `protonmail._domainkey` | CNAME | `protonmail.domainkey.<hash>.domains.proton.ch` | only record at that name |
| `protonmail2._domainkey` | CNAME | `protonmail2.domainkey.<hash>.domains.proton.ch` | only record at that name |
| `protonmail3._domainkey` | CNAME | `protonmail3.domainkey.<hash>.domains.proton.ch` | only record at that name |
| `_dmarc` | TXT | `v=DMARC1; p=none; rua=mailto:<address>` | only TXT at that name |

`<hash>` is per-domain and shown in the Proton admin panel; it is not derivable.

The apex set — two TXT and the MX pair — applies in any order; each apply adds a
record and reports the ones it joined:

```bash
enclii providers cloudflare dns-apply creatumundo.mx --type TXT \
  --content 'protonmail-verification=<token>' \
  --apply --reason "prove domain ownership to Proton"

enclii providers cloudflare dns-apply creatumundo.mx --type TXT \
  --content 'v=spf1 include:_spf.protonmail.ch ~all' \
  --apply --reason "publish Proton SPF"

enclii providers cloudflare dns-apply creatumundo.mx --type MX \
  --priority 10 --content mail.protonmail.ch --proxied false \
  --apply --reason "primary MX for Proton Mail"

enclii providers cloudflare dns-apply creatumundo.mx --type MX \
  --priority 20 --content mailsec.protonmail.ch --proxied false \
  --apply --reason "backup MX for Proton Mail"
```

Confirm the apex holds the whole set, not the last one written:

```bash
dig +short TXT creatumundo.mx @<one of the zone's Cloudflare nameservers>   # expect 2 records
dig +short MX  creatumundo.mx @<one of the zone's Cloudflare nameservers>   # expect 10 and 20
```

The three DKIM CNAMEs and `_dmarc` are each the only record of their type at
their name:

```bash
for n in "" 2 3; do
  enclii providers cloudflare dns-apply "protonmail${n}._domainkey.creatumundo.mx" \
    --type CNAME --content "protonmail${n}.domainkey.<hash>.domains.proton.ch" \
    --proxied false \
    --apply --reason "Proton Mail DKIM key ${n:-1}"
done
```

:::warning DKIM CNAMEs must not be proxied

A proxied record answers with Cloudflare's own addresses, and the DKIM lookup
gets an address instead of the delegation it needs. Pass `--proxied false`
explicitly: `dns-apply` defaults `A`/`AAAA`/`CNAME` to proxied.

:::

### Coexisting with Resend

A domain that already sends transactional mail through Resend keeps those
records; Proton and Resend do not conflict, because they occupy different names:

| Name | Type | Owner |
|------|------|-------|
| `resend._domainkey` | TXT | Resend DKIM |
| `send` | MX | Resend bounce handling |
| `send` | TXT | Resend SPF, scoped to the `send` subdomain |

The one place they *do* meet is the apex SPF. Resend's own records live under
`send.<domain>`, so the apex `v=spf1 include:_spf.protonmail.ch ~all` above is
correct as written and does not need a Resend `include`. Verify with
`dig +short TXT send.<domain>` that the Resend records are intact — they are a
different name so an apex edit cannot reach them, and since #536 an apex edit no
longer overwrites its own neighbours either, but the check costs nothing.

## `zone-settings-apply`: the HTTPS posture step

Run this **after the zone goes active** (that is, after the registrar
delegation lands and Cloudflare stops reporting the zone as `pending`). A
pending zone has no settings to read.

```bash
enclii providers cloudflare zone-settings-apply creatumundo.mx \
  --apply --reason "apply Enclii HTTPS posture to the client apex"
```

It applies three settings as a set, because they are only meaningful together —
`always_use_https` with a TLS floor of 1.0 still leaves the connection
downgradeable while reading as secure in a browser:

| Setting | Desired | Why |
|---------|---------|-----|
| `always_use_https` | `on` | redirect plain HTTP at the edge, before it reaches the origin |
| `automatic_https_rewrites` | `on` | rewrite `http://` subresources so a page does not mix content |
| `min_tls_version` | `1.2` | TLS 1.0/1.1 are deprecated and still offered by default |

Verify from outside — a 301 is the whole point of the step:

```bash
curl -sSI http://creatumundo.mx/ | head -1
# HTTP/1.1 301 Moved Permanently
```

If the dry-run reports `zone_absent`, the zone was never created: run
`providers cloudflare zone-add-apply` first. If a setting reports
`not-editable`, that is a zone-plan limitation, not a failure of this command.

## Domain onboarding sequence for a brand host

Verified end to end on 2026-09-07 for a host on a zone MADFAM hosts.

### 1. `domains add` — **from the repo root**

```bash
cd <repo root>            # NOT optional; see below
enclii domains add crea-erp.creatumundo.mx \
  --service nauta-web --env production -f enclii.yaml
```

:::warning `-f` does not change the working directory

The manifest parser resolves manifest-relative paths — `spec.build.dockerfile`
above all — against **`os.Getwd()`**, not against the directory the `-f` file
lives in (`packages/cli/internal/spec/parser.go`, `projectDir, err :=
os.Getwd()`). Running this from a subdirectory fails validation with
`spec.build.dockerfile: file does not exist: <path>` for a Dockerfile that is
plainly there. Run it from the repo root.

:::

`domains add` prints the CNAME target and the
`enclii-verification=<id>` TXT value used in steps 2 and 4.

### 2. `dns-apply` — proxied CNAME to the tunnel

```bash
enclii providers cloudflare dns-apply crea-erp.creatumundo.mx \
  --type CNAME --content <TUNNEL_CNAME> --proxied true \
  --apply --reason "route the new brand host through the Enclii tunnel"
```

### 3. `tunnels-apply` — reconcile the tunnel ingress

```bash
enclii providers cloudflare tunnels-apply --project <project> \
  --apply --reason "publish the tunnel route for the new brand host"
```

**Read the plan before applying.** It must show *only* a `create` for the new
hostname, plus skips for everything already live. Any `update` or `delete`
against a hostname you did not just add means the plan is about to rewrite a
working route — stop and investigate. (This guard exists because a legacy
manifest's declared domains once rewrote live routes to a dead backend.)

### 4. `dns-apply` — the verification TXT

```bash
enclii providers cloudflare dns-apply crea-erp.creatumundo.mx \
  --type TXT --content 'enclii-verification=<id>' \
  --apply --reason "prove ownership of the new brand host to Enclii"
```

If the host already carries another TXT, this apply adds a second one and leaves
the first in place — see [How `dns-apply` decides](#how-dns-apply-decides).

### 5. `domains verify`

```bash
enclii domains verify crea-erp.creatumundo.mx --service nauta-web
```

TLS is issued automatically once verification succeeds.

### Interaction with manifest capture

Deploy-time manifest capture is **idempotent with this sequence** — a later
deploy that declares the same host does not fight the records created above.

But a **manifest-only merge builds nothing**: when `build-publish` detects no
changed service, no image is built, so no deploy runs and therefore no capture
happens. A host added to `enclii.yaml` in a docs- or manifest-only PR does not
become live on merge. Either run the sequence above, or use
`enclii domains reconcile <service>` to provision the declared hostname
server-side.

## Host redirects: no Enclii op yet (dashboard break-glass)

When a brand migrates, the old hosts must keep answering with a 301 to the new
ones. **Enclii has no adapter for this.** Cloudflare **Redirect Rules** are a
ruleset (`http_request_dynamic_redirect` phase), not DNS records and not tunnel
ingress, and `dns-apply` cannot express one: a redirect needs the edge to answer
with a `Location:` header instead of routing to a backend.

Tracked as [#538](https://github.com/madfam-org/enclii/issues/538), which
proposes:

```bash
enclii providers cloudflare redirect-apply crea-map.madfam.io \
  --to https://map.creatumundo.mx --status 301 --preserve-query \
  --apply --reason "brand migration: MADFAM host to the client's own apex"
```

Until that ships, this is **documented break-glass** — record actor, reason,
target, what you did and the result, per the Enclii-first contract.

### Break-glass procedure

1. **The source host must still resolve, and must be proxied.** A redirect rule
   only fires on a request Cloudflare actually receives. Leave the existing
   proxied record in place — do *not* delete the old host's DNS when you cut
   over, or the redirect never runs and clients get NXDOMAIN instead of a 301.

2. Cloudflare dashboard → the **source** host's zone → **Rules → Redirect
   Rules → Create rule**:

   | Field | Value |
   |---|---|
   | When incoming requests match | `Hostname` `equals` `<source host>` |
   | Type | Dynamic |
   | Expression | `concat("https://<target host>", http.request.uri.path)` |
   | Query string | Preserve |
   | Status code | `301` |

   Use `301` only once the target is confirmed serving — browsers and
   intermediaries cache a permanent redirect, and a premature one is expensive
   to walk back. Use `302` while you are still verifying.

3. **Mind the rule order.** Redirect rules are an ordered list and the first
   match wins; a new rule placed above an existing one silently changes that
   one's behaviour. Read the whole list before adding.

4. Verify from outside, and check the `Location` header — not just the status:

   ```bash
   curl -sSI https://crea-map.madfam.io/some/path | grep -i '^HTTP/\|^location'
   # HTTP/2 301
   # location: https://map.creatumundo.mx/some/path
   ```

5. **Do not create the redirect until public resolvers agree on the target** —
   see [the resolver caveat](#resolver-caveat-after-a-nameserver-switch) below.
   A 301 to a host that a stale resolver still cannot see is a cached failure.

Created this way on **2026-09-07** during the CTM onboarding:
`crea-map.madfam.io` → `https://map.creatumundo.mx` and `crea-erp.madfam.io` →
`https://erp.creatumundo.mx`, both 301. Nothing reconciles them: if someone
deletes one in the dashboard, no Enclii check notices.

## Resolver caveat after a nameserver switch

For up to the **old** zone's NS TTL after a registrar delegation change, clients
whose resolver still holds the previous nameservers resolve against the old
zone. During that window, on those clients only:

- a host that exists only in the new zone answers **NXDOMAIN**; and
- a DNS-only host that the old zone pointed at a Cloudflare-hosted CDN answers
  **Cloudflare error 1034**.

Both look exactly like a broken record you just wrote. They are not — the
authoritative check is against the new zone's own nameservers:

```bash
dig +short crea-erp.creatumundo.mx @<one of the zone's Cloudflare nameservers>
dig +short NS creatumundo.mx @1.1.1.1
dig +short NS creatumundo.mx @8.8.8.8
```

**Do not cut over redirects, and do not "fix" a record that already reads
correctly at the authoritative nameservers, until the public resolvers agree.**
Re-applying a correct record because a stale resolver disagreed is how a working
zone gets churned.

## CLI gotchas

### `enclii whoami` prints on stderr

`whoami`, `login`, and `logout` report through cobra's `cmd.Println`, which
writes to `OutOrStderr()`. The CLI never calls `SetOut`, so **all of that output
is on stderr**. `enclii whoami > /tmp/who` captures an empty file and reads as
"not logged in". Redirect with `2>&1` (`whoami` has no JSON output).

### This runbook needs CLI `v1.0.0-alpha.9` or later

**Every command in this runbook assumes a CLI release `>= v1.0.0-alpha.9`** (or
one built from `main`). `v1.0.0-alpha.9` (2026-09-08) was the first release to
carry #527 and #536; `v1.0.0-alpha.8` was cut 2026-09-06 from the commit
*before* any of 2026-09-07's work. Check with `enclii version 2>&1` and upgrade
from the [releases page](https://github.com/madfam-org/enclii/releases) if the
binary is older.

| Verb / flag | Landed in | In `v1.0.0-alpha.8`? |
|---|---|---|
| `--tenant`, `providers porkbun ping` | [#527](https://github.com/madfam-org/enclii/pull/527) | no — flag rejected as unknown, `ping` does not exist |
| `dns-apply --priority` | [#536](https://github.com/madfam-org/enclii/pull/536) | no — flag rejected as unknown |
| Non-destructive multi-record TXT/MX, `--replace`, the 400/409/422/424 taxonomy | [#536](https://github.com/madfam-org/enclii/pull/536) | no |

:::danger An alpha.8 CLI will destroy records this runbook says are safe

The missing flags fail loudly, which is survivable. The record-identity fix does
not. On `alpha.8`, `dns-apply` still keys a TXT/MX by name + type only, so
applying the apex SPF above onto a zone that already holds a Proton ownership
TXT is planned `create` and executed as a destructive `update` — issue
[#530](https://github.com/madfam-org/enclii/issues/530), which is exactly what
happened on a live client zone on 2026-09-07. Confirm your binary before you
follow the apex steps.

:::

```bash
go build -o ~/bin/enclii ./packages/cli/cmd/enclii
enclii providers porkbun ping --tenant crea      # exists only with #527
```

The same floor applies to the operator scripts that shell out to the CLI,
including `scripts/operator/npm-registry-admin-password-rotate.sh`.

### `enclii login` follows the browser's Janua session

`login` completes an OAuth PKCE flow in whatever browser session is already
authenticated at `auth.madfam.io`. Estate cookie precedence (janua J9) means a
browser logged into a **client** application resolves that identity, and the
CLI silently receives the wrong one — every subsequent `--tenant` call then
fails on authorization rather than on anything to do with the tenant.

**Log out of the client app in the browser before running `enclii login`**, and
confirm with `enclii whoami 2>&1` before you trust a session.

## Related

- [Porkbun per-tenant registrar credentials](/infrastructure/porkbun-tenant-credentials)
- [`enclii providers`](/cli/commands/providers)
- [`enclii domains`](/cli/commands/domains)
- [Cloudflare Integration](/infrastructure/CLOUDFLARE)
