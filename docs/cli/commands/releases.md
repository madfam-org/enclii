# enclii releases

List releases (builds) for a service.

## Synopsis

```bash
enclii releases [service-name] [flags]
```

## Description

The `releases` command displays the build history for a service, including version, git SHA, build status, and error messages for failed builds. Releases are sorted by creation time and include a summary of overall build health.

Each release shows:
- Build version and abbreviated git SHA
- Build status (`ready`, `building`, `failed`)
- Relative timestamp (e.g., `3h ago`, `2d ago`)
- Error messages for failed builds (truncated to 200 characters)

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit`, `-n` | int | `10` | Number of releases to show |
| `--all`, `-a` | bool | `false` | Show all releases (overrides `--limit`) |
| `--id` | string | | Service ID (alternative to service name) |

## Examples

### List Releases by Service Name
```bash
enclii releases switchyard-api
```

**Output:**
```
Releases for service 70c1bded-7f28-4438-87ff-393efffd3bad (12 total, showing 10):

  ready     v1.4.2  (git: a1b2c3d4)  3h ago

  ready     v1.4.1  (git: e5f6a7b8)  1d ago

  failed    v1.4.0  (git: c9d0e1f2)  2d ago
         Error: nixpacks build failed: missing package.json in project root

  ready     v1.3.9  (git: 34a5b6c7)  5d ago

Summary: 9 ready, 2 failed, 1 building
```

### List Releases by Service ID
```bash
enclii releases --id 70c1bded-7f28-4438-87ff-393efffd3bad
```

### Show All Releases
```bash
enclii releases switchyard-api --all
```

### Limit to a Specific Count
```bash
enclii releases switchyard-api -n 5
```

## Service Resolution

The command accepts a service name as a positional argument or a service ID via the `--id` flag. When a name is provided, the command resolves it by listing services in the current project (configured via `enclii init` or the `project` config key, defaulting to `default`).

If the service name is not found, the command prints the available services in the project to help identify the correct name.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Releases listed successfully |
| `10` | Validation error (missing service name/ID, service not found) |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy a service
- [`enclii ps`](./ps.md) - Check service status
- [`enclii rollback`](./rollback.md) - Revert to a previous release
- [`enclii logs`](./logs.md) - View service logs
