# enclii projects

Manage projects.

## Synopsis

```bash
enclii projects <subcommand> [flags]
```

**Aliases:** `project`

## Description

A project is the top-level container for services, environments, secrets, and budgets. The `projects` command manages the project resource itself; to manage services inside a project, see [`enclii services-sync`](./services-sync.md) and [`enclii services-delete`](./services-delete.md).

This command mirrors the `/projects` page in the consumer web UI. Read subcommands accept `--json`; `delete` requires `--force` to skip confirmation.

## Subcommands

### `list`

List all projects.

```bash
enclii projects list [flags]
```

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `get`

Get project details by slug.

```bash
enclii projects get <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `create`

Create a new project.

```bash
enclii projects create --name <name> --slug <slug>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | | Project display name (required) |
| `--slug` | string | | Project URL slug (required) |

### `delete`

Delete a project. This removes all services, environments, and secrets owned by the project — irreversibly.

```bash
enclii projects delete <slug> [--force]
```

**Aliases:** `rm`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

### `environments`

List environments for a project.

```bash
enclii projects environments <slug> [flags]
```

**Aliases:** `envs`, `env`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `services`

List services in a project (lightweight summary).

```bash
enclii projects services <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `reconcile-services`

Discover Deployments in a project's Kubernetes namespace and ensure the `services` table reflects them. Idempotent recovery for GitOps-onboarded projects missing service rows or `k8s_namespace` values.

```bash
enclii projects reconcile-services <slug> [flags]
```

Requires **admin** API token (`POST /v1/admin/projects/:slug/reconcile-services`).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### List all projects

```bash
enclii projects list
```

**Output:**
```
SLUG         NAME           CREATED
storefront   Storefront     2026-01-12 09:30
data-lake    Data Lake      2026-02-04 14:11
```

### Create a new project

```bash
enclii projects create --name "Storefront" --slug storefront
```

**Output:**
```
Project created: Storefront (storefront)
```

### Inspect a project in JSON

```bash
enclii projects get storefront --json
```

### List environments

```bash
enclii projects environments storefront
```

**Output:**
```
NAME         NAMESPACE              CREATED
production   storefront-prod        2026-01-12 09:31
staging      storefront-staging     2026-01-12 09:31
```

### List services in a project

```bash
enclii projects services storefront
```

**Output:**
```
NAME       KIND     CREATED
api        web      2026-01-12 09:35
worker     worker   2026-01-13 11:02
```

### Delete a project without confirmation

```bash
enclii projects delete legacy-app --force
```

### Reconcile services from cluster (admin)

```bash
enclii projects reconcile-services blueprint-harvester
```

## Notes

- Slugs are immutable once created. To rename, create a new project and migrate.
- `projects delete` cascades to all child services and their deployments. Recovery requires restore from backup.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing slug, invalid name) |
| `50` | Authentication error |

## See Also

- [`enclii services-sync`](./services-sync.md) - Sync services from `service.yaml`
- [`enclii services-delete`](./services-delete.md) - Delete a service from a project
- [`enclii teams`](./teams.md) - Teams that own projects
- [`enclii deploy`](./deploy.md) - Deploy a service in a project
