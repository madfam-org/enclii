# enclii addon

Manage database addons — fresh, isolated managed Postgres instances scoped to a service.

## Synopsis

```bash
enclii addon <subcommand> [flags]
```

**Aliases:** `addons`

## Description

The `addon` command provisions and manages database addons for your services. Each addon is an isolated CloudNativePG cluster created in your project namespace; credentials are surfaced as a Kubernetes Secret and auto-injected into the bound service as `DATABASE_URL` (or a custom env-var name).

The CLI talks to switchyard-api, which validates the requested plan, provisions the cluster, and returns a Secret reference — the raw password is never exposed to the CLI. Plans are billed at the project level and can be inspected with `enclii addon plans`.

See `docs/architecture/managed-db-addon.md` for the full design.

## Subcommands

### `plans`

List available managed-database plans.

```bash
enclii addon plans [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--engine` | string | | Filter by engine (`postgres`, `redis`, `mysql`) |
| `--json` | bool | `false` | JSON output |

### `create`

Create a new managed database addon.

```bash
enclii addon create <name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--plan` | string | | Plan code (required, e.g. `standard-0`) |
| `--engine` | string | `postgres` | Engine (`postgres`, `redis`, `mysql`) |
| `--project` | string | active project | Project slug |
| `--service` | string | | Service ID to bind `DATABASE_URL` to |
| `--env-var` | string | `DATABASE_URL` | Env var name for the binding |
| `--environment-id` | string | | Environment UUID (optional) |
| `--json` | bool | `false` | JSON output |

### `ls`

List database addons.

```bash
enclii addon ls [flags]
```

**Aliases:** `list`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | string | all accessible | Project slug |
| `--json` | bool | `false` | JSON output |

### `destroy`

Destroy a database addon. **Irreversible** — the underlying CloudNativePG cluster and all its data are deleted.

```bash
enclii addon destroy <addon_id> [flags]
```

**Aliases:** `delete`, `rm`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | `false` | Skip confirmation prompt |

## Examples

### List available plans

```bash
enclii addon plans
```

### Create a 1 GB Postgres bound to a service

```bash
enclii addon create my-db --plan standard-0 --service my-api
```

The credentials land in the service environment as `DATABASE_URL` automatically.

### Create with a custom env-var name

```bash
enclii addon create cache-db --plan standard-0 --service my-api --env-var CACHE_URL
```

### List addons in a project

```bash
enclii addon ls --project my-api
```

### Destroy an addon

```bash
enclii addon destroy 123e4567-e89b-12d3-a456-426614174000 --yes
```

## Notes

- Addon names are namespaced inside the project; they need only be unique within a project.
- The bound service rolls automatically once the credentials secret exists.
- `enclii addon` is the canonical name; `enclii addons` is the only alias.
- `enclii db` is a **different** command ([`db`](./db.md) — read-only platform Postgres inspection) and has never reached this subtree. Use `enclii addon …` for every addon operation.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing plan/project, invalid engine) |
| `50` | Authentication error |

## See Also

- [`enclii secrets`](./secrets.md) - Inspect injected `DATABASE_URL`
- [`enclii projects`](./projects.md) - Manage projects
- [`enclii deploy`](./deploy.md) - Roll the bound service after addon changes
