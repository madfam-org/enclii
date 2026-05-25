# enclii secrets

Manage service secrets and environment variables.

## Synopsis

```bash
enclii secrets <subcommand> [flags]
```

**Aliases:** `secret`, `env`

## Description

The `secrets` command manages secrets and environment variables for your services. Secrets are encrypted at rest and masked in API responses. Regular environment variables are visible but can be promoted to secrets with the `--secret` flag.

The command reads `service.yaml` (or a custom spec file via `--file`) to resolve the target project and service. Changes to secrets and environment variables take effect on the next deployment.

## Subcommands

### `set`

Set one or more secrets or environment variables.

```bash
enclii secrets set KEY=VALUE [KEY2=VALUE2 ...] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--secret`, `-s` | bool | `false` | Mark as secret (encrypted at rest, masked in responses) |
| `--env`, `-e` | string | | Target environment (default: all environments) |
| `--file`, `-f` | string | `service.yaml` | Path to service.yaml specification file |

### `list`

List all secrets and environment variables for a service.

```bash
enclii secrets list [flags]
```

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | | Filter by environment |
| `--file`, `-f` | string | `service.yaml` | Path to service.yaml specification file |
| `--all`, `-a` | bool | `false` | Show all metadata (environment, last updated) |
| `--json` | bool | `false` | Emit machine-readable JSON instead of a table |
| `--reveal` | bool | `false` | Reveal secret values via the audit-logged reveal endpoint (one call per secret) |

### `get`

Get a specific secret or environment variable by key.

```bash
enclii secrets get KEY [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--file`, `-f` | string | `service.yaml` | Path to service.yaml specification file |
| `--reveal` | bool | `false` | Reveal secret value (logged for audit) |

### `delete`

Delete one or more secrets or environment variables.

```bash
enclii secrets delete KEY [KEY2 ...] [flags]
```

**Aliases:** `rm`, `remove`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--file`, `-f` | string | `service.yaml` | Path to service.yaml specification file |
| `--force` | bool | `false` | Skip confirmation prompt |

## Examples

### Set a Secret

```bash
enclii secrets set API_KEY=sk-live-abc123 --secret
```

**Output:**
```
Set API_KEY (secret)
Run 'enclii deploy' to apply changes to your running service
```

### Set a Regular Environment Variable

```bash
enclii secrets set DEBUG=true LOG_LEVEL=info
```

**Output:**
```
Set 2 variables
Run 'enclii deploy' to apply changes to your running service
```

### Set a Secret for a Specific Environment

```bash
enclii secrets set DATABASE_URL=postgres://user:pass@db:5432/mydb --secret --env production
```

### List All Variables

```bash
enclii secrets list
```

**Output:**
```
KEY            VALUE                    SECRET
API_KEY        ****                     [locked]
DEBUG          true
LOG_LEVEL      info
DATABASE_URL   ****                     [locked]
```

### List with Full Metadata

```bash
enclii secrets list --all --env production
```

**Output:**
```
KEY            VALUE   SECRET   ENVIRONMENT   UPDATED
API_KEY        ****    [locked] production    2026-03-15 09:30
DATABASE_URL   ****    [locked] production    2026-03-14 14:22
```

### Get a Specific Variable

```bash
enclii secrets get API_KEY
```

**Output:**
```
API_KEY=****
Use --reveal to see the actual value
```

### Reveal a Secret Value

```bash
enclii secrets get API_KEY --reveal
```

**Output:**
```
API_KEY=sk-live-abc123
Secret revealed - this action has been logged
```

### Delete a Variable

```bash
enclii secrets delete API_KEY
```

**Output:**
```
About to delete: API_KEY
Continue? [y/N]: y
Deleted API_KEY
Run 'enclii deploy' to apply changes to your running service
```

### Delete Multiple Variables Without Confirmation

```bash
enclii secrets delete API_KEY DATABASE_URL --force
```

### Use a Custom Spec File

```bash
enclii secrets list --file ./deploy/enclii.yaml
```

## Security

- Secrets are encrypted at rest in the control plane database.
- Secret values are masked (`****`) in all API responses and CLI output by default.
- Revealing a secret value via `--reveal` is recorded in the audit log.
- Secrets are injected into containers as environment variables at deploy time.
- Never commit secrets to version control. Use `enclii secrets set --secret` instead.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (invalid KEY=VALUE format, missing spec file) |
| `50` | Authentication error |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy to apply secret changes
- [`enclii ps`](./ps.md) - Check service status
- [`enclii onboard`](./onboard.md) - Onboard a service with initial secrets
- [Service Spec Reference](../../reference/service-spec.md) - Service configuration format

## `enclii secrets sync`

Refresh an ExternalSecret through Enclii's audited operator layer instead of using routine `kubectl` annotations.

```bash
enclii secrets sync forgesight-secrets --namespace forgesight
enclii secrets sync forgesight-secrets --namespace forgesight --apply --reason "provider path populated"
```

Without `--apply`, the command requests a dry-run plan. With `--apply`, `--reason` is required.

## `enclii secrets rotate`

Plan a secret rotation through the Enclii operation contract.

```bash
enclii secrets rotate npm-madfam-token --namespace npm-registry
enclii secrets rotate janua-jwt-signing-key --project janua --json
```

Rotation is plan-first until the Vault writer and dual-consumer cutover path are enabled server-side. Do not treat this as completed rotation execution yet.
