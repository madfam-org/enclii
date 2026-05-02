---
title: CLI Reference
description: Complete command reference for the Enclii CLI
sidebar_position: 1
tags: [cli, reference, commands, deployment]
---

# Enclii CLI Reference

The `enclii` command-line interface provides developers with tools to deploy, manage, and monitor services on the Enclii platform.

## Installation

### macOS (Homebrew)
```bash
brew install enclii/tap/enclii
```

### Linux
```bash
curl -sSL https://get.enclii.dev | bash
```

### From Source
```bash
git clone https://github.com/madfam-org/enclii.git
cd enclii
make build-cli
./bin/enclii --version
```

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
| [`services-delete`](./commands/services-delete.md) | Delete a service from a project |
| [`services-sync`](./commands/services-sync.md) | Synchronize service configuration |
| [`onboard`](./commands/onboard.md) | Onboard a new project with full provisioning |

### Configuration
| Command | Description |
|---------|-------------|
| [`secrets`](./commands/secrets.md) | Manage service secrets and environment variables |
| [`domains`](./commands/domains.md) | Manage custom domains for services |
| [`functions`](./commands/functions.md) | Manage serverless functions (scale-to-zero) |
| [`jobs`](./commands/jobs.md) | Manage cron and one-off scheduled jobs |
| [`junctions`](./commands/junctions.md) | Manage routing rules and ingress configuration |

### Teams & collaboration
| Command | Description |
|---------|-------------|
| [`teams`](./commands/teams.md) | Manage teams, memberships, invitations |
| [`integrations`](./commands/integrations.md) | Third-party integrations (GitHub) |

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
| [`vault`](./commands/vault.md) | Inspect cluster Vault deployment |

### Local & meta
| Command | Description |
|---------|-------------|
| [`local`](./commands/local.md) | Local development environment commands |
| [`version`](./commands/version.md) | Display CLI version information (`--json` available) |

## Global Flags

These flags are available for all commands:

| Flag | Description |
|------|-------------|
| `--api-endpoint` | API endpoint URL (default `https://api.enclii.dev`) |
| `--api-token` | Authentication token (overrides stored credentials) |
| `--log-level` | Log level: `debug`, `info`, `warn`, `error` |
| `--help`, `-h` | Show help for any command |

Most read subcommands across the CLI accept a `--json` flag for stable, machine-readable output. Most mutating subcommands accept `--force` to skip confirmation prompts.

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
