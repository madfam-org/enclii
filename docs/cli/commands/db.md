# enclii db

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


Inspect the platform database (read-only).

## Synopsis

```bash
enclii db <subcommand> [flags]
```

## Description

The `db` subtree exposes read-only inspection of the platform Postgres instance:

- **`wal-status`** — WAL archive freshness via pgBackRest sidecar (cluster exec)
- **`schema`** — golang-migrate version, dirty flag, and GA column checks via Switchyard API (admin)

Mutating operations (`backup`, `restore`, point-in-time recovery) are intentionally **not** in this CLI. Run them through `kubectl exec` into the `pgbackrest` sidecar under operator supervision; see `docs/runbooks/POSTGRES_WAL_ARCHIVING.md` for the procedure.

> Note: `enclii db` and `enclii addon` are different subtrees. `enclii addon` is also reachable via the alias `enclii db`, but the `wal-status` subcommand documented here lives only under `enclii db wal-status`.

## Subcommands

### `wal-status`

Report WAL archive freshness, backup history, and replica lag for the production in-cluster Postgres.

```bash
enclii db wal-status [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--namespace` | string | `data` | Postgres namespace |
| `--label` | string | `app=postgres` | Pod selector for the Postgres primary |
| `--sidecar` | string | `pgbackrest` | pgBackRest sidecar container name |
| `--stanza` | string | `main` | pgBackRest stanza name |
| `--json` | bool | `false` | Emit structured JSON instead of human output |

The command does a read-only cluster call: it parses `pgbackrest info --output=json` from the sidecar and prints:

- Most recent WAL archive segment age (color-coded against the RPO threshold)
- Latest full / differential / incremental backup type and age
- R2 repo footprint (sum of backup delta sizes)
- Replica lag (stub for P1.1; wires up in P1.2 once replication lands)

### `schema`

Report DB migration version and GA-critical column presence (Commercial GA migration verify).

```bash
enclii db schema [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit structured JSON |

Calls `GET /v1/admin/db/schema`. Verifies `schema_migrations` version/dirty state and columns such as `services.rollout_blocked_reason` (migration 030). Requires **admin** API token.

## Examples

### Check WAL archive freshness for production

```bash
enclii db wal-status
```

### Verify migration 030 before Stability GA clock (admin)

```bash
enclii db schema
```

### Same, in JSON for CI

```bash
enclii db wal-status --json
```

### Inspect a staging Postgres in a renamed namespace

```bash
enclii db wal-status --namespace data-staging --label app=postgres-staging
```

### Custom stanza name

```bash
enclii db wal-status --stanza staging-main
```

## Notes

- The CLI requires `kubectl` context and permission to `exec` into the Postgres pod's `pgbackrest` sidecar.
- Exit code `0` is returned even when the status is **degraded** — the human/JSON output reports the actual state. This makes the command safe to run in dashboards.
- Exit code `2` indicates the sidecar could not be reached or the stanza has not been created yet.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Successfully inspected (including degraded status) |
| `2` | Sidecar unreachable or stanza not yet created |
| `50` | Authentication error |

## See Also

- [`enclii addon`](./addon.md) - Manage tenant database addons
- [`enclii admin clusters`](./admin.md#clusters) - Manage clusters where Postgres runs
- [`enclii vault`](./vault.md) - Inspect Vault, which holds the pgBackRest credentials
