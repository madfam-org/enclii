---
title: providers
description: Audited MADFAM provider workflows for GitHub, Cloudflare, Porkbun, and Hetzner
---

# `enclii providers`

`enclii providers` is the contract-first replacement layer for direct `gh`,
Cloudflare, Porkbun, and Hetzner tooling in MADFAM operations.

Mutating commands are dry-run by default. Use `--apply --reason "..."` only
when the corresponding provider adapter is wired and the audit reason is clear.

Read-only commands call live Switchyard adapters when configured. Current first
coverage includes GitHub workflow runs, repository Actions secrets, GHCR package
metadata/versions, branch protection, Cloudflare DNS, Cloudflare tunnel status,
Cloudflare DNS apply for zones Enclii controls, tunnel route inventory, Porkbun
domain inventory, Porkbun DNS reads, Porkbun DNS create fallback, Porkbun
renewal reads, and Porkbun nameserver apply. Hetzner, Cloudflare Access, and R2
remain contract surfaces until their adapters are wired; missing coverage
returns `adapter_unconfigured`.

## Commands

| Command | Purpose |
|---------|---------|
| `enclii providers capabilities` | List server-supported provider capabilities |
| `enclii providers github runs|rerun|cancel|secrets|packages|protection` | GitHub Actions, repo secrets, GHCR, branch protection |
| `enclii providers cloudflare dns|dns-apply|tunnels|tunnels-apply|access|r2|hostnames` | DNS, tunnels, Access, R2, custom hostnames |
| `enclii providers porkbun domains|dns|dns-apply|renewals|nameservers|nameservers-apply` | Domain inventory, DNS create fallback, renewal state, registrar delegation |
| `enclii providers hetzner nodes|lb|vswitch|storage|firewall` | Robot/Cloud nodes, DR LB, vSwitch, storage boxes, firewall |

## Examples

```bash
enclii providers capabilities
enclii providers github runs madfam-org/digifab-quoting --json
enclii providers github packages madfam-org/enclii --json
enclii providers cloudflare dns cotiza.studio
enclii providers cloudflare dns-apply app.example.com --project example --service web --apply --reason "point app host at Enclii tunnel"
enclii providers cloudflare tunnels --json
enclii providers cloudflare tunnels-apply --project example --apply --reason "reconcile junction tunnel routes to correct K8s backends"
enclii providers porkbun dns-apply crm.phynd.app --domain phynd.app --type CNAME --content c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com --apply --reason "restore PhyndCRM app host through Enclii"
enclii providers porkbun nameservers-apply phynd.app --nameservers ns1.cloudflare.com,ns2.cloudflare.com --apply --reason "delegate phynd.app to Enclii-managed Cloudflare"
enclii providers github rerun 25430873929 --apply --reason "re-run after GHCR token scope fix"
```

## Required Mutation Flags

| Flag | Description |
|------|-------------|
| `--apply` | Execute instead of returning a dry-run plan |
| `--reason` | Audit reason; required with `--apply` |
| `--idempotency-key` | Optional retry key for safely repeating an operation |
| `--project`, `--service` | Enclii scope selectors passed to the operation contract |

## Remaining Adapter Work

- GitHub `rerun` and `cancel` remain contract-only.
- GitHub `packages` now reads GHCR package metadata and recent versions; write
  operations for package visibility/deletion are intentionally out of scope.
- Cloudflare `dns-apply` creates, updates, or no-ops DNS records when the target
  zone is visible to the configured Enclii Cloudflare account. It blocks with
  `blocked_by_dns_authority` when the apex zone still needs registrar
  delegation/import.
- Cloudflare `tunnels-apply` reconciles junction hostnames to the correct in-cluster service URL using `resolveServiceNamespace`; use instead of `junctions add` when live tunnel routes drift.
- Cloudflare `access` and `r2` remain contract-only.
- Cloudflare `hostnames` currently reads DNS-shaped state; full SaaS custom
  hostname inventory is a follow-up.
- Porkbun `dns-apply` currently supports idempotent create/no-op semantics and
  blocks on existing records with different content until explicit update/delete
  support is added.
- Porkbun `nameservers-apply` supports registrar delegation updates through the
  configured Switchyard Porkbun credentials.
- Hetzner surfaces are declared but not yet backed by clients.

## Cloudflare DNS apply

Apply or dry-run Cloudflare DNS through Enclii instead of provider UI/API calls.

```bash
enclii providers cloudflare dns-apply app.example.com --type CNAME --content <TUNNEL_CNAME>
enclii providers cloudflare dns-apply app.example.com --type CNAME --proxied true --apply --reason "route customer domain through Enclii"
```

Without `--apply`, the command requests a dry-run plan. With `--apply`, `--reason` is required.

## Cloudflare credential readiness

```bash
enclii providers cloudflare credentials --json
```

This is a contract-read surface for provider credential readiness. Treat it as advisory until the server-side endpoint returns concrete configured/missing provider keys.
