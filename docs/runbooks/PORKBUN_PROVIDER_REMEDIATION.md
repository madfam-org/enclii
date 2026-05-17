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

For production, `infra/k8s/production/porkbun-credentials.externalsecret.yaml`
materializes `enclii-porkbun-credentials` from `secret/enclii` properties
`porkbun_api_key` and `porkbun_secret_api_key`. If `vault-store` is not Ready,
repair Vault Kubernetes auth before expecting the Porkbun adapter to activate:

```bash
VAULT_TOKEN="$TOKEN" ./scripts/repair-vault-eso-auth.sh
```

As of 2026-05-17, live `switchyard-api` is on the signed digest that supports
`providers.porkbun.dns-apply`. A dry-run should now return
`adapter_unconfigured` when credentials are absent; HTTP 404 for
`unsupported operation porkbun.dns-apply` means the core-services GitOps branch
has regressed to an older API image.

## Domain registration preflight

Before applying DNS, verify the apex exists. On 2026-05-17, registry RDAP for
`phyne.app` returned HTTP 404 with `phyne.app not found`, and the authoritative
`.app` nameserver returned NXDOMAIN. In that state, DNS writes cannot succeed:
the domain must first be registered/restored through the registrar account.

```bash
curl -sS -i -L https://rdap.org/domain/phyne.app
dig @ns-tld1.charlestonroadregistry.com phyne.app NS
```

The guarded end-to-end runner performs this preflight before planning or
applying DNS:

```bash
scripts/remediate-phyne-app-host.sh
scripts/remediate-phyne-app-host.sh --apply
```

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
