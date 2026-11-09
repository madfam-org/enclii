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

- **`wal-status`** — WAL archive freshness via `pg_stat_archiver` and backup history via the pgBackRest sidecar (cluster exec)
- **`schema`** — golang-migrate version, dirty flag, and GA column checks via Switchyard API (admin)

Mutating operations (`backup`, `restore`, point-in-time recovery) are intentionally **not** in this CLI. Run them through `kubectl exec` into the `pgbackrest` sidecar under operator supervision; see `docs/runbooks/POSTGRES_WAL_ARCHIVING.md` for the procedure.

> Note: `enclii db` and [`enclii addon`](./addon.md) are different subtrees. `enclii db` inspects the **platform** Postgres; `enclii addon` provisions **per-service managed** Postgres. There is no alias between them — to create or list addons, run `enclii addon create` / `enclii addon ls`.

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
| `--postgres-container` | string | `postgres` | Postgres container; `pg_stat_archiver` is read here with `psql` |
| `--stanza` | string | `main` | pgBackRest stanza name |
| `--json` | bool | `false` | Emit structured JSON instead of human output |

The command makes two read-only cluster calls: `pgbackrest info --output=json` in the sidecar, and a `SELECT` on `pg_stat_archiver` with `psql -U postgres` in the postgres container. It prints:

- Age of the last successfully archived WAL segment (`pg_stat_archiver.last_archived_time`): green under 15 min, yellow under 1 h, red at 1 h or more. These match the `PostgresWALArchiveLag` and `PostgresWALArchiveStale` alerts.
- Archiver failure count and last failed segment. A failure newer than the last success is red.
- Latest full / differential / incremental backup type and age
- R2 repo footprint (sum of backup delta sizes)
- Replica lag (stub for P1.1; wires up in P1.2 once replication lands)

### `schema`

Report DB migration version and GA-critical column presence (GA migration verify).

```bash
enclii db schema [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit structured JSON |

Calls `GET /v1/admin/db/schema`. Verifies `schema_migrations` version/dirty state and columns such as `services.rollout_blocked_reason` (migration 030). Requires **admin** API token.

Prefer this over break-glass `psql` when verifying GA migrations (ops task O-2).

## Examples

### Check WAL archive freshness for production

```bash
enclii db wal-status
```

### Verify migration 030 before the GA SLO clock (admin)

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

- The CLI requires `kubectl` context and permission to `exec` into the Postgres pod's `pgbackrest` sidecar and `postgres` container.
- The latest backup age is not a WAL age. If `pg_stat_archiver` cannot be read, the output says `Latest WAL age: unknown`, `--json` sets `latest_wal_age_s` to `null` and `wal_freshness_verified` to `false`, and the status is at best yellow ("freshness UNVERIFIED").
- Exit code `0` is returned even when the status is **degraded** — the human/JSON output reports the actual state. This makes the command safe to run in dashboards.
- If the sidecar cannot be reached or the stanza has not been created yet, the command exits `1`.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Successfully inspected (including degraded status) |
| `1` | Any error, including an unreachable sidecar or a stanza that is not created yet, and API errors for `schema` |

## See Also

- [`enclii addon`](./addon.md) - Manage tenant database addons
- [`enclii admin clusters`](./admin.md#clusters) - Manage clusters where Postgres runs
- [`enclii vault`](./vault.md) - Inspect Vault, which holds the pgBackRest credentials
