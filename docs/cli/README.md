---
title: CLI Reference
description: Complete command reference for the Enclii CLI
sidebar_position: 1
tags: [cli, reference, commands, deployment]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Enclii CLI Reference

The `enclii` command-line interface provides developers with tools to deploy, manage, and monitor services on the Enclii platform.

For MADFAM operations, Enclii is the required manipulation layer. Use the web
UI, API, or CLI for routine production provisioning, deployment, observability,
domains, secrets, provider operations, scaling, rollback, and remediation.

- Use `enclii ops` instead of routine `kubectl`, ArgoCD, Longhorn, Kyverno,
  ExternalSecrets, Vault, or ARC workflows.
- Use `enclii providers` instead of routine `gh`, Cloudflare, Porkbun, or
  Hetzner tooling.
- Use raw `kubectl`, `helm`, SSH, provider CLIs/APIs, `docker exec`, or direct
  container access only for platform bootstrap or documented break-glass
  emergencies when Enclii is unavailable or lacks an implemented adapter.
- Record missing adapter gaps so the next remediation lands in Enclii instead
  of becoming permanent operator folklore.

## Installation

### macOS (Homebrew)
```bash
brew install enclii/tap/enclii
```

### Linux / from source (any OS with Go 1.22+)
```bash
git clone https://github.com/madfam-org/enclii.git
cd enclii
make install-cli           # builds + installs to /usr/local/bin/enclii
```

Override the destination with `CLI_INSTALL_DIR`:

```bash
make install-cli CLI_INSTALL_DIR=$HOME/.local/bin
```

To build without installing:

```bash
make build-cli
./bin/enclii version
```

### Windows

Install via WSL2 and follow the Linux instructions above. A native PowerShell installer is on the roadmap.

## Authentication

Before using most commands, authenticate with Enclii:

```bash
# Interactive login (opens browser for SSO)
enclii login

# Verify authentication
enclii whoami

# Logout when done
enclii logout
```

## Quick Start

```bash
# 1. Initialize a new service
enclii init --name my-app

# 2. Deploy to preview environment
enclii deploy --env preview

# 3. Check deployment status
enclii ps

# 4. View logs
enclii logs my-app -f

# 5. Deploy to production
enclii deploy --env production
```

## Commands Overview

### Authentication & identity
| Command | Description |
|---------|-------------|
| [`signup`](./commands/signup.md) | Create a new Enclii account (browser-based) |
| [`login`](./commands/login.md) | Authenticate with Enclii via SSO |
| [`logout`](./commands/logout.md) | Clear local authentication credentials |
| [`whoami`](./commands/whoami.md) | Display current authenticated user |
| [`tokens`](./commands/tokens.md) | Manage personal API tokens (CI/CD) |

### Projects, services, deployments
| Command | Description |
|---------|-------------|
| [`init`](./commands/init.md) | Initialize a new service configuration |
| [`projects`](./commands/projects.md) | List, create, inspect projects |
| [`deploy`](./commands/deploy.md) | Deploy a service to an environment |
| [`deployments`](./commands/deployments.md) | Query deployment runs (alias `deps`) |
| [`releases`](./commands/releases.md) | List releases (build artifacts) for a service |
| [`ps`](./commands/ps.md) | List services and their status |
| [`logs`](./commands/logs.md) | Stream or fetch service logs |
| [`rollback`](./commands/rollback.md) | Rollback to a previous deployment |
| [`canary`](./commands/canary.md) | Manage in-flight canary rollouts |
| [`services-delete`](./commands/services-delete.md) | Delete a service from a project |
| [`services-sync`](./commands/services-sync.md) | Synchronize service configuration |
| [`onboard`](./commands/onboard.md) | Onboard a new project with full provisioning |

### Configuration
| Command | Description |
|---------|-------------|
| [`secrets`](./commands/secrets.md) | Manage service secrets and environment variables |
| [`domains`](./commands/domains.md) | Manage custom domains for services |
| [`previews`](./commands/previews.md) | List and manage PR preview environments |
| [`volumes`](./commands/volumes.md) | Manage persistent volumes on a service |
| [`functions`](./commands/functions.md) | Manage serverless functions (scale-to-zero) |
| [`jobs`](./commands/jobs.md) | Manage cron and one-off scheduled jobs |
| [`junctions`](./commands/junctions.md) | Manage routing rules and ingress configuration |
| [`addon`](./commands/addon.md) | Manage database addons (managed Postgres) |
| [`webhooks`](./commands/webhooks.md) | Manage outbound lifecycle webhook subscriptions |

### Teams & collaboration
| Command | Description |
|---------|-------------|
| [`teams`](./commands/teams.md) | Manage teams, memberships, invitations |
| [`integrations`](./commands/integrations.md) | Third-party integrations (GitHub) |
| [`billing`](./commands/billing.md) | View spend, manage budgets, inspect alerts |
| [`export`](./commands/export.md) | Export everything Enclii holds about your project |

### Observability & audit
| Command | Description |
|---------|-------------|
| [`observe`](./commands/observe.md) | Service metrics, health, errors, alerts (alias `metrics`) |
| [`activity`](./commands/activity.md) | Lifecycle event feed |
| [`audit`](./commands/audit.md) | Consolidated audit log + CSV export |

### Platform operations (admin role)
| Command | Description |
|---------|-------------|
| [`admin`](./commands/admin.md) | Platform operator commands (mirrors admin-console portal) |
| [`ops`](./commands/ops.md) | Audited Kubernetes, Argo, Longhorn, Kyverno, ARC replacement workflows |
| [`providers`](./commands/providers.md) | Audited GitHub, Cloudflare, Porkbun, and Hetzner replacement workflows |
| [`vault`](./commands/vault.md) | Inspect cluster Vault deployment |
| [`db`](./commands/db.md) | Inspect the platform database (read-only WAL status) |

### Local & meta
| Command | Description |
|---------|-------------|
| [`local`](./commands/local.md) | Local development environment commands |
| [`version`](./commands/version.md) | Display CLI version information (`--json` available) |
| [`completion`](./commands/completion.md) | Generate shell autocompletion scripts |

## Global Flags

These flags are available for all commands:

| Flag | Description |
|------|-------------|
| `--api-endpoint` | API endpoint URL (default `https://api.enclii.dev`) |
| `--api-token` | Authentication token (overrides stored credentials) |
| `--log-level` | Log level: `debug`, `info`, `warn`, `error` |
| `--help`, `-h` | Show help for any command |

Most read subcommands across the CLI accept a `--json` flag for stable, machine-readable output. Most mutating subcommands accept `--force` to skip confirmation prompts. The `ops` and `providers` replacement-layer commands are plan-first: mutations require `--apply --reason "..."`; without `--apply`, they request a dry-run plan.

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ENCLII_API_ENDPOINT` | API endpoint (default: `https://api.enclii.dev`) |
| `ENCLII_API_TOKEN` | Authentication token (alternative to `enclii login`) |
| `ENCLII_OIDC_ISSUER` | OIDC issuer URL for self-hosted deployments (default: `https://auth.madfam.io`) |
| `ENCLII_OIDC_CLIENT_ID` | OIDC client ID for self-hosted deployments |
| `ENCLII_VAULT_ADDR` / `VAULT_ADDR` | Override Vault address for `enclii vault status` |
| `ENCLII_PROJECT` | Default project slug |
| `ENCLII_ENVIRONMENT` | Default environment |
| `ENCLII_LOG_LEVEL` | Logging verbosity: `debug`, `info`, `warn`, `error` |

## Credentials Storage

OAuth credentials are persisted to `~/.enclii/credentials.json` (mode `0600`):

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "rt_...",
  "token_type": "Bearer",
  "expires_at": "2026-08-01T00:00:00Z",
  "issuer": "https://auth.madfam.io"
}
```

Tokens are auto-refreshed on the next CLI invocation when within 60 seconds of expiry, provided a refresh token is present. Run `enclii login` again if the refresh token has been revoked.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `10` | Validation error (invalid input) |
| `20` | Build failed |
| `30` | Deployment failed |
| `40` | Timeout |
| `50` | Authentication error |

## Examples

### Deploy with Canary Strategy
```bash
enclii deploy --env production --strategy canary --canary-percent 10
```

### View Logs with Filtering
```bash
enclii logs my-app --since 1h --level error -f
```

### Rollback to Previous Version
```bash
enclii rollback my-app --to-revision 5
```

### Local Development
```bash
# Start local environment
enclii local up

# View local logs
enclii local logs

# Stop local environment
enclii local down
```

## Related Documentation

- **Getting Started**: [Quick Start Guide](/getting-started/QUICKSTART)
- **SDK Alternative**: [TypeScript SDK](/sdk/typescript/) for programmatic access
- **Troubleshooting**: [Auth Problems](/troubleshooting/auth-problems) | [Deployment Issues](/troubleshooting/deployment-issues)
- **FAQ**: [General FAQ](/faq/general)
- **Reference**: [Service Specification](/reference/service-spec) | [GitHub Integration](/integrations/github)
