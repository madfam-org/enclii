# enclii deployments

Query deployment runs across services.

## Synopsis

```bash
enclii deployments <subcommand> [flags]
```

**Aliases:** `deps`

## Description

The `deployments` command is the read-only query surface for deployment runs — the runtime side of releases. It complements [`enclii deploy`](./deploy.md) (the imperative action verb that triggers a deployment) and [`enclii releases`](./releases.md) (the catalog of build artifacts).

Mental model:

- **`enclii releases`** lists *build artifacts* — immutable image+spec tuples produced by the build pipeline.
- **`enclii deploy`** is the *action verb* that rolls out a release to an environment.
- **`enclii deployments`** lists *deployment runs* — one record per release-rollout, with status, health, and replica state.

This command mirrors the `/deployments` page in the consumer web UI. All subcommands are read-only and accept `--json`.

## Subcommands

### `list`

List deployments. Without `--service`, returns the cross-service feed; with `--service`, scoped to one service.

```bash
enclii deployments list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (optional — omit for cross-service list) |
| `--limit` | int | `50` | Maximum number of deployments |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `get`

Show full details for a single deployment.

```bash
enclii deployments get <deployment_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `latest`

Show the latest deployment for a service, including the resolved release image URI.

```bash
enclii deployments latest --service <id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (required) |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `by-version`

Resolve a deployment by Heroku-style version number (`v1`, `v2`, ...). Pass the integer without the `v` prefix.

```bash
enclii deployments by-version --service <id> --version <n> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service ID (required) |
| `--version` | int | `0` | Heroku-style version number (required, positive integer) |
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### Cross-service deployment feed

```bash
enclii deployments list --limit 20
```

**Output:**
```
ID        SERVICE   VERSION  STATUS     STARTED           FINISHED
dep_3f7d  svc_a3b4  v42      succeeded  2026-05-02 09:12  2026-05-02 09:14
dep_2c4e  svc_b2c3  v17      running    2026-05-02 09:11  2026-05-02 09:11
dep_1a2b  svc_a3b4  v41      succeeded  2026-05-01 18:30  2026-05-01 18:32
```

### Deployments for a single service

```bash
enclii deployments list --service svc_storefront --limit 10
```

### Inspect a deployment

```bash
enclii deployments get dep_3f7d9b2c
```

**Output:**
```
ID:         dep_3f7d9b2c
Release:    rel_c4d5e6f7
Env:        env_storefront_prod
Status:     succeeded
Health:     healthy
Replicas:   3
Version:    v42
Created:    2026-05-02T09:12:14Z
Updated:    2026-05-02T09:14:01Z
```

### Latest deployment for a service

```bash
enclii deployments latest --service svc_storefront
```

**Output:**
```
ID:       dep_3f7d9b2c
Status:   succeeded
Health:   healthy
Version:  v42
Created:  2026-05-02T09:12:14Z
Image:    ghcr.io/madfam-org/storefront@sha256:abc123...
```

### Resolve a specific version

```bash
enclii deployments by-version --service svc_storefront --version 42
```

### Pipe to jq for status filtering

```bash
enclii deployments list --json --limit 200 | \
  jq '.deployments[] | select(.status == "failed")'
```

## Notes

- IDs in the table view are truncated to 8 characters for readability. Pass the full ID to `get` and `by-version`; `enclii deployments list --json` returns full IDs.
- `latest` returns the most recent deployment regardless of status. To find the most recent successful deployment, filter the JSON output of `list` by `status == "succeeded"`.
- A failed deployment is not automatically rolled back; use [`enclii rollback`](./rollback.md) to revert.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing `--service`, non-positive `--version`) |
| `50` | Authentication error |

## See Also

- [`enclii deploy`](./deploy.md) - Trigger a deployment (action verb)
- [`enclii releases`](./releases.md) - List build artifacts
- [`enclii rollback`](./rollback.md) - Roll back to a previous deployment
- [`enclii ps`](./ps.md) - Process and replica status
