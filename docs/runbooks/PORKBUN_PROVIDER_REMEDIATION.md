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

## Domain authority preflight

Before applying DNS, verify the apex exists and identify the active authority.
For `phynd.app`, RDAP shows Porkbun as registrar and Cloudflare nameservers as
the delegated authority. DNS writes should therefore use Enclii's Cloudflare
provider path when available, with Porkbun operations reserved for registrar
inventory or fallback authority checks.

```bash
curl -sS -i -L https://rdap.org/domain/phynd.app
dig @ns-tld1.charlestonroadregistry.com phynd.app NS
```

The guarded end-to-end runner performs this preflight before planning or
applying DNS:

```bash
scripts/remediate-phynd-app-host.sh
scripts/remediate-phynd-app-host.sh --apply
```

## Read operations

```bash
enclii providers porkbun domains --json
enclii providers porkbun domains phynd.app --json
enclii providers porkbun nameservers phynd.app --json
enclii providers porkbun dns phynd.app --json
enclii providers porkbun renewals --json
```

## DNS fallback create

Use this only when Cloudflare authority is unavailable through Enclii and
registrar-level DNS is the active recovery path.

```bash
enclii providers porkbun dns-apply crm.phynd.app \
  --domain phynd.app \
  --type CNAME \
  --content c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com \
  --apply \
  --reason "restore PhyndCRM app host through Enclii"
```

Current semantics:

- Creates the record when absent.
- No-ops when the existing record already matches.
- Blocks on existing records with different content until explicit update/delete support is added.

## Nameserver delegation

Use this only if the registrar apex needs to be delegated or repaired. Do not
change nameservers when the current delegation is already correct.

```bash
enclii providers porkbun nameservers-apply phynd.app \
  --nameservers <cloudflare-ns-1>,<cloudflare-ns-2> \
  --apply \
  --reason "delegate phynd.app to Enclii-managed Cloudflare"
```

After delegation, rerun:

```bash
enclii providers cloudflare dns-apply crm.phynd.app --json
```

The expected outcome is that Cloudflare no longer reports `blocked_by_dns_authority`.

## Completion criteria for `crm.phynd.app`

- `enclii providers porkbun domains phynd.app --json` succeeds.
- `enclii providers porkbun nameservers phynd.app --json` reflects the intended delegation.
- Public DNS resolves `crm.phynd.app`.
- `https://crm.phynd.app` reaches the generic PhyndCRM authenticated app and not the MADFAM tenant slice.
- `https://crm.madfam.io` immediately routes to the MADFAM tenant Janua SSO flow.
- `https://status.madfam.io/api/status` no longer lists `https://crm.phynd.app` as affected.
