# Enclii Porkbun Provider Remediation

Date: 2026-05-16
Scope: Enclii-first registrar recovery for domains that are not yet under the configured Cloudflare zone authority.

## Purpose

Use Enclii provider operations for Porkbun domain inventory, DNS fallback, renewal visibility, and nameserver delegation. Direct Porkbun API calls or console edits are break-glass only when Enclii is unavailable.

## Required configuration

Switchyard API must receive these values through Enclii-managed secrets:

- `ENCLII_PORKBUN_API_KEY`
- `ENCLII_PORKBUN_SECRET_API_KEY`
- `ENCLII_PORKBUN_API_BASE_URL`, optional, defaults to `https://api.porkbun.com/api/json/v3`

Do not print or copy secret values into chat, logs, docs, or shell history.

## Read operations

```bash
enclii providers porkbun domains --json
enclii providers porkbun domains phyne.app --json
enclii providers porkbun nameservers phyne.app --json
enclii providers porkbun dns phyne.app --json
enclii providers porkbun renewals --json
```

## DNS fallback create

Use this only when Cloudflare returns `blocked_by_dns_authority` and registrar-level DNS must be restored before Cloudflare delegation/import is complete.

```bash
enclii providers porkbun dns-apply app.phyne.app \
  --domain phyne.app \
  --type CNAME \
  --content c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com \
  --apply \
  --reason "restore PhyneCRM app host through Enclii"
```

Current semantics:

- Creates the record when absent.
- No-ops when the existing record already matches.
- Blocks on existing records with different content until explicit update/delete support is added.

## Nameserver delegation

Use this when the durable fix is to delegate the registrar apex to the Enclii-managed Cloudflare zone.

```bash
enclii providers porkbun nameservers-apply phyne.app \
  --nameservers <cloudflare-ns-1>,<cloudflare-ns-2> \
  --apply \
  --reason "delegate phyne.app to Enclii-managed Cloudflare"
```

After delegation, rerun:

```bash
enclii providers cloudflare dns-apply app.phyne.app --json
```

The expected outcome is that Cloudflare no longer reports `blocked_by_dns_authority`.

## Completion criteria for `app.phyne.app`

- `enclii providers porkbun domains phyne.app --json` succeeds.
- `enclii providers porkbun nameservers phyne.app --json` reflects the intended delegation.
- Public DNS resolves `app.phyne.app`.
- `https://app.phyne.app` reaches the generic PhyneCRM authenticated app and not the MADFAM tenant slice.
- `https://crm.madfam.io` immediately routes to the MADFAM tenant Janua SSO flow.
- `https://status.madfam.io/api/status` no longer lists `https://app.phyne.app` as affected.
