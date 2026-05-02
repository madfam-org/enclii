# enclii audit

Query the consolidated audit log (auth, deploys, secrets, RFCs).

## Synopsis

```bash
enclii audit <subcommand> [flags]
```

## Description

The `audit` command queries the consolidated audit log across Janua (auth), Switchyard (deploys, secrets), and Selva (RFC ledgers). The server-side endpoint fans out across these systems and returns a merged, time-ordered view; the CLI renders the output.

Use `audit` for forensic queries (who did what, when, to which resource) and compliance evidence. For the curated lifecycle stream, see [`enclii activity`](./activity.md). This command mirrors the `/audit` page in the consumer web UI.

## Subcommands

### `list`

List audit log entries with optional filters.

```bash
enclii audit list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--actor` | string | | Filter by actor ID |
| `--action` | string | | Filter by action name |
| `--resource-type` | string | | Filter by resource type |
| `--resource-id` | string | | Filter by resource ID |
| `--from` | string | | ISO timestamp lower bound (inclusive) |
| `--to` | string | | ISO timestamp upper bound (exclusive) |
| `--limit` | int | `50` | Maximum number of entries to return |
| `--page` | int | `0` | Page number for pagination (1-based) |
| `--json` | bool | `false` | Emit machine-readable JSON |

### `export`

Export audit log entries as **CSV**. This is admin-gated server-side; non-admin callers receive a `403` surfaced as a validation error. With `--out`, the CSV is written to the given file; without it, CSV is streamed to stdout.

```bash
enclii audit export [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--from` | string | | ISO timestamp lower bound (inclusive) |
| `--to` | string | | ISO timestamp upper bound (exclusive) |
| `--actor` | string | | Filter by actor ID |
| `--out` | string | | Output file path (default: stdout) |

## Examples

### Recent activity for a single actor

```bash
enclii audit list --actor usr_a3b4c5 --limit 100
```

**Output:**
```
TIMESTAMP         ACTOR        ACTION              RESOURCE                  SOURCE
2026-05-02 09:14  usr_a3b4c5   deploy.start        service/svc_storefront    switchyard
2026-05-02 09:13  usr_a3b4c5   secret.set          service/svc_storefront    switchyard
2026-05-02 09:01  usr_a3b4c5   auth.login          user/usr_a3b4c5           janua
```

### Filter by action across a time window

```bash
enclii audit list \
  --action deploy.start \
  --from 2026-05-01T00:00:00Z \
  --to 2026-05-02T00:00:00Z
```

### Audit a specific resource

```bash
enclii audit list --resource-type service --resource-id svc_storefront --limit 200
```

### Pipe to `jq` for analysis

```bash
enclii audit list --action secret.set --json --limit 1000 | \
  jq '.entries[] | {ts: .timestamp, actor, resource: .resource_id}'
```

### Export a month of audit log to CSV (admin only)

```bash
enclii audit export --from 2026-04-01 --to 2026-05-01 --out audit-april.csv
```

**Output:**
```
Wrote audit-april.csv
```

### Stream CSV to a downstream pipeline

```bash
enclii audit export --from 2026-04-01 --to 2026-05-01 | \
  curl --data-binary @- https://siem.madfam.io/ingest
```

## Notes

- `audit export` requires admin role on the server. Without it, the call returns `403 Forbidden`.
- Timestamp filters accept any ISO-8601 string the server can parse (`2026-05-01`, `2026-05-01T12:00:00Z`).
- The merged view's `source` column tells you which system originated the event (`janua`, `switchyard`, `selva`).

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (export forbidden, malformed timestamp) |
| `50` | Authentication error |

## See Also

- [`enclii activity`](./activity.md) - Curated lifecycle event stream
- [`enclii tokens`](./tokens.md) - Audit token-issued requests
- [`enclii admin governance`](./admin.md#governance) - Governed-resource policy state
