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

| Command | Description |
|---------|-------------|
| [`login`](./commands/login.md) | Authenticate with Enclii via SSO |
| [`logout`](./commands/logout.md) | Clear local authentication credentials |
| [`whoami`](./commands/whoami.md) | Display current authenticated user |
| [`init`](./commands/init.md) | Initialize a new service configuration |
| [`deploy`](./commands/deploy.md) | Deploy a service to an environment |
| [`ps`](./commands/ps.md) | List services and their status |
| [`logs`](./commands/logs.md) | Stream or fetch service logs |
| [`rollback`](./commands/rollback.md) | Rollback to a previous deployment |
| [`services sync`](./commands/services-sync.md) | Synchronize service configuration |
| [`local`](./commands/local.md) | Local development environment commands |
| [`onboard`](./commands/onboard.md) | Onboard a new project with full provisioning |
| [`version`](./commands/version.md) | Display CLI version information |

## Global Flags

These flags are available for all commands:

| Flag | Description |
|------|-------------|
| `--config` | Path to config file (default: `~/.enclii/config.yaml`) |
| `--project` | Override project context |
| `--env` | Target environment (preview, staging, production) |
| `--output`, `-o` | Output format: `table`, `json`, `yaml` |
| `--verbose`, `-v` | Enable verbose output |
| `--quiet`, `-q` | Suppress non-essential output |
| `--help`, `-h` | Show help for any command |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `ENCLII_API_URL` | API endpoint (default: `https://api.enclii.dev`) |
| `ENCLII_TOKEN` | Authentication token (alternative to login) |
| `ENCLII_PROJECT` | Default project ID |
| `ENCLII_ENV` | Default environment |
| `ENCLII_LOG_LEVEL` | Logging verbosity: `debug`, `info`, `warn`, `error` |
| `NO_COLOR` | Disable colored output |

## Configuration File

The CLI uses a YAML configuration file at `~/.enclii/config.yaml`:

```yaml
# API configuration
api_url: https://api.enclii.dev

# Authentication (managed by `enclii login`)
auth:
  token: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9...
  refresh_token: ...
  expires_at: 2025-02-01T00:00:00Z

# Default project and environment
defaults:
  project: proj_abc123
  environment: staging

# Output preferences
output:
  format: table
  color: true
```

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

- **Getting Started**: [Quick Start Guide](/docs/getting-started/QUICKSTART)
- **SDK Alternative**: [TypeScript SDK](/docs/sdk/typescript/) for programmatic access
- **Troubleshooting**: [Auth Problems](/docs/troubleshooting/auth-problems) | [Deployment Issues](/docs/troubleshooting/deployment-issues)
- **FAQ**: [General FAQ](/docs/faq/general)
- **Reference**: [Service Specification](/docs/reference/service-spec) | [GitHub Integration](/docs/integrations/github)
