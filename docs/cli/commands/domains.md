# enclii domains

Manage custom domains for services.

## Synopsis

```bash
enclii domains <subcommand> [flags]
```

**Aliases:** `domain`, `dns`

## Description

The `domains` command manages custom domains for your services. Each domain requires DNS verification before it becomes active. The workflow is: add the domain, configure DNS records (CNAME + TXT), then verify ownership. TLS certificates are provisioned automatically via cert-manager once verification succeeds.

The command reads service context from `service.yaml` in the current directory. You can override this with the `--service` and `--file` flags.

## Subcommands

### `domains list`

List all custom domains for a service.

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name (uses `service.yaml` if not specified) |
| `--env`, `-e` | string | | Filter by environment |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |
| `--all`, `-a` | bool | `false` | Show all domain details (CNAME, created date) |

### `domains add`

Add a custom domain to a service.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name (uses `service.yaml` if not specified) |
| `--env`, `-e` | string | `production` | Target environment |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |
| `--tls` | bool | `true` | Enable TLS |
| `--tls-issuer` | string | | TLS issuer (`letsencrypt-prod`, `letsencrypt-staging`) |

### `domains remove`

Remove a custom domain from a service.

**Aliases:** `rm`, `delete`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name |
| `--env`, `-e` | string | | Environment (required if domain exists in multiple envs) |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |
| `--force` | bool | `false` | Skip confirmation prompt |

### `domains verify`

Verify domain ownership by checking for the required DNS TXT record. Run this after adding the TXT record shown by `domains add`.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name |
| `--env`, `-e` | string | | Environment |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |

### `domains status`

Show domain status and DNS instructions. Without a domain argument, shows status of all domains (equivalent to `domains list`). With a domain argument, shows detailed status including verification state, TLS configuration, and DNS setup instructions if the domain is not yet verified.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name |
| `--env`, `-e` | string | | Environment |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |
| `--verbose`, `-v` | bool | `false` | Show detailed information |

## Examples

### List Domains for a Service
```bash
enclii domains list --service my-api
```

**Output:**
```
DOMAIN              STATUS    TLS       VERIFIED
api.example.com     active    enabled   ✓
staging.example.com pending   enabled   ✗
```

### List Domains with Full Details
```bash
enclii domains list --service my-api --all
```

**Output:**
```
DOMAIN              STATUS    TLS       VERIFIED  CNAME                              CREATED
api.example.com     active    enabled   ✓         abc123.cfargotunnel.com            2026-01-15
staging.example.com pending   enabled   ✗         abc123.cfargotunnel.com            2026-03-10
```

### List Domains for a Specific Environment
```bash
enclii domains list --service my-api --env production
```

### Add a Custom Domain
```bash
enclii domains add api.example.com --service my-api --env production
```

**Output:**
```
Domain api.example.com added to my-api (production)

DNS Configuration Required:

   1. Add a CNAME record:
      api.example.com  CNAME  abc123.cfargotunnel.com

   2. Add a TXT record for verification:
      api.example.com  TXT  enclii-verification=d4e5f6a7-...

   3. Run verification:
      enclii domains verify api.example.com --service my-api

DNS changes may take up to 24 hours to propagate.
```

### Add a Domain with Staging TLS
```bash
enclii domains add staging.example.com --service my-api --env staging --tls-issuer letsencrypt-staging
```

### Verify Domain Ownership
```bash
enclii domains verify api.example.com --service my-api
```

**Output (success):**
```
Domain api.example.com verified successfully!
TLS certificate will be provisioned automatically.
Your domain should be active within 5 minutes.
```

**Output (failure):**
```
Domain api.example.com not verified

Expected TXT record not found. Please add:
   api.example.com  TXT  enclii-verification=d4e5f6a7-...

You can check your DNS with:
   dig TXT api.example.com

DNS changes may take up to 24 hours to propagate.
```

### Check Domain Status
```bash
enclii domains status api.example.com --service my-api
```

**Output:**
```
Domain Status: api.example.com
──────────────────────────────────────────────────

  Service:        my-api
  Status:         active
  TLS:            enabled (letsencrypt-prod)
  Verified:       ✓ Verified (2026-01-15 14:30)
  Created:        2026-01-15 14:25:00

Domain is active and serving traffic.
```

### Remove a Domain
```bash
enclii domains remove api.example.com --service my-api
```

**Output:**
```
About to remove domain: api.example.com
Continue? [y/N]: y
Domain api.example.com removed from my-api
Remember to remove DNS records for this domain.
```

### Remove a Domain Without Confirmation
```bash
enclii domains remove api.example.com --service my-api --force
```

### Use service.yaml Context (No --service Flag)
```bash
# In a directory with service.yaml
enclii domains list
enclii domains add api.example.com
enclii domains status
```

## Domain Lifecycle

1. **Add** -- Register the domain with `domains add`. Status: `pending`.
2. **Configure DNS** -- Add the CNAME and TXT records shown in the output.
3. **Verify** -- Run `domains verify` to confirm DNS ownership. Status: `verified`.
4. **TLS provisioned** -- cert-manager automatically issues a certificate. Status: `active`.
5. **Traffic served** -- Cloudflare tunnel routes traffic to your service.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Command successful |
| `10` | Validation error (invalid domain, missing service) |
| `30` | Operation failed (API error, domain not found) |
| `50` | Authentication error |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy a service (auto-provisions domains from `enclii.yaml`)
- [`enclii ps`](./ps.md) - Check service status
- [`enclii logs`](./logs.md) - View service logs
- [Service Spec Reference](../../reference/service-spec.md) - Domain configuration in `enclii.yaml`
