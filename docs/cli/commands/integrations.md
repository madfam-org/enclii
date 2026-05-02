# enclii integrations

Manage third-party integrations (GitHub, ...).

## Synopsis

```bash
enclii integrations <subcommand> [flags]
```

## Description

The `integrations` command manages connections to third-party providers. Today the platform exposes a single integration server-side: **GitHub**, surfaced under `enclii integrations github …`. Future providers will appear as siblings.

This command mirrors the `/integrations` page in the consumer web UI. All read subcommands accept `--json`.

## Subcommands

### `github status`

Show GitHub App connection status — whether the App is installed, the linked account, the installation ID, and granted scopes.

```bash
enclii integrations github status [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `github repos`

List repositories accessible to the GitHub App installation.

```bash
enclii integrations github repos [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--page` | int | `0` | Page number (1-based) |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `github branches`

List branches for a repository.

```bash
enclii integrations github branches <owner> <repo> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `github link`

Link a GitHub App installation to the current account. Run after completing the GitHub OAuth flow in the browser; provide the installation ID shown on the GitHub-side post-install screen.

```bash
enclii integrations github link --installation-id <id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--installation-id` | int64 | | GitHub App installation ID (required) |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `github analyze`

Analyze a repository for services and Dockerfiles. Returns the inferred service tree (path, language, Dockerfile location), useful as a precursor to onboarding.

```bash
enclii integrations github analyze <owner> <repo> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### Check connection status

```bash
enclii integrations github status
```

**Output:**
```
Connected:       true
Account:         madfam-org
Installation ID: 12345678
Scopes:          [contents:read pull_requests:write]
```

### List accessible repositories

```bash
enclii integrations github repos
```

**Output:**
```
FULL_NAME                  DEFAULT_BRANCH  PRIVATE
madfam-org/storefront      main            true
madfam-org/data-lake       main            true
madfam-org/marketing-site  main            false
```

### List branches

```bash
enclii integrations github branches madfam-org storefront
```

**Output:**
```
NAME            PROTECTED  SHA
main            true       a3b4c5d6
release/v0.5    true       c4d5e6f7
feature/checkout false     e6f7a8b9
```

### Link a fresh installation

```bash
enclii integrations github link --installation-id 12345678
```

**Output:**
```
Linked installation 12345678.
```

### Analyze a repo before onboarding

```bash
enclii integrations github analyze madfam-org storefront
```

**Output:**
```
NAME      PATH               LANGUAGE  DOCKERFILE
api       services/api       go        services/api/Dockerfile
worker    services/worker    go        services/worker/Dockerfile
ui        apps/ui            ts        apps/ui/Dockerfile

Dockerfiles: [services/api/Dockerfile services/worker/Dockerfile apps/ui/Dockerfile]
```

## Notes

- The GitHub App must be installed by an account owner before `link` will succeed. The installation ID is shown on the install confirmation page and on the App's `/settings/installations/<id>` URL.
- `analyze` is a heuristic — it inspects directory structure, Dockerfiles, and language hints. Validate the output before using it for onboarding.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing `--installation-id`) |
| `50` | Authentication error |

## See Also

- [`enclii onboard`](./onboard.md) - Onboard a service from a repository
- [`enclii projects`](./projects.md) - Project resource management
- [`enclii deploy`](./deploy.md) - Deploy a service
