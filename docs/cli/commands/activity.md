# enclii activity

Stream lifecycle events (deploys, builds, env-var changes).

## Synopsis

```bash
enclii activity <subcommand> [flags]
```

## Description

The `activity` command streams and filters platform lifecycle events: deploy started/succeeded/failed, build failed, env-var changed, and similar curated events. It is distinct from [`enclii audit`](./audit.md) — activity is the human-friendly lifecycle stream you would wire up to a status board, while audit is the full forensic log used for compliance.

This command mirrors the `/activity` page in the consumer web UI. All subcommands are read-only and accept `--json`.

## Subcommands

### `list`

List recent lifecycle events.

```bash
enclii activity list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--action` | string | | Filter by action name |
| `--resource-type` | string | | Filter by resource type |
| `--limit` | int | `50` | Maximum number of events to return |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `actions`

List the valid `--action` filter values supported by the server. Use this to discover available action names rather than hard-coding them.

```bash
enclii activity actions [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `resource-types`

List the valid `--resource-type` filter values supported by the server.

```bash
enclii activity resource-types [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### Recent activity (default 50)

```bash
enclii activity list
```

**Output:**
```
TIMESTAMP         ACTION              RESOURCE_TYPE  RESOURCE          ACTOR
2026-05-02 09:14  deploy.succeeded    service        svc_storefront    usr_a3b4c5
2026-05-02 09:12  deploy.start        service        svc_storefront    usr_a3b4c5
2026-05-02 08:48  build.failed        release        rel_c4d5e6        usr_b2c3d4
```

### Filter by action

```bash
enclii activity list --action deploy.succeeded --limit 20
```

### Filter by resource type

```bash
enclii activity list --resource-type service --limit 100
```

### Discover valid filter values

```bash
enclii activity actions
```

**Output:**
```
build.start
build.succeeded
build.failed
deploy.start
deploy.succeeded
deploy.failed
secret.set
secret.delete
```

### Pipe events to a watcher

```bash
enclii activity list --action deploy.failed --json --limit 100 | \
  jq -r '.events[] | "\(.timestamp) \(.resource)"'
```

## Notes

- `activity` is intentionally lossy — it surfaces only meaningful lifecycle transitions, not every API call. For full coverage use `audit`.
- The `actions` and `resource-types` lists are authoritative; if the server adds new event categories they will appear there before being documented here.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error |
| `50` | Authentication error |

## See Also

- [`enclii audit`](./audit.md) - Full forensic audit log
- [`enclii deployments`](./deployments.md) - Detailed deployment runs
- [`enclii observe`](./observe.md) - Real-time service observability
