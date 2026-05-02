# enclii export

Export everything Enclii holds about your project.

## Synopsis

```bash
enclii export --project <slug> [--wait --out <path>]
enclii export <list|status|download> [flags]
```

## Description

`enclii export` initiates a tenant data export. The pipeline produces a single tarball containing:

- Kubernetes manifests (project, services, deployments, cron jobs)
- `pg_dump` of each bound database addon
- R2 blob inventory (the index — **not** the blob contents)
- Secret **references** (names and types only; values are not exported)
- Audit timeline scoped to your project

Tarballs live in R2 for **14 days**. Each download URL is a fresh **15-minute** pre-signed link.

Production exports require **Human-In-The-Loop (HITL) approval** from a second project admin before the pipeline runs. Non-production exports start immediately.

The default invocation initiates an export and prints the export id; pass `--wait` to also poll until ready and download the tarball locally with sha256 verification.

## Flags (top-level)

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug to export |
| `--wait` | bool | `false` | Block until the export is ready, then download |
| `--out` | string | `./enclii-export-<slug>-<ts>.tar.gz` | Where to write the tarball |
| `--poll-interval` | duration | `10s` | Polling cadence when `--wait` is set |

## Subcommands

### `list`

List recent tenant exports for a project.

```bash
enclii export list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |

### `status`

Show the status of a tenant export.

```bash
enclii export status <export_id>
```

### `download`

Download a previously-ready tenant export.

```bash
enclii export download <export_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--out` | string | `./<export_id>.tar.gz` | Target path |

## Examples

### Initiate an export, return immediately

```bash
enclii export --project acme
```

The export id is printed to stdout. The pipeline runs server-side; you can check progress later with `enclii export status`.

### Initiate, wait, and download

```bash
enclii export --project acme --wait --out ./acme.tar.gz
```

This blocks until the tarball is ready (or HITL approval lands for production), downloads it, and verifies sha256.

### List recent exports

```bash
enclii export list --project acme
```

### Check the status of an in-flight export

```bash
enclii export status exp_abc123
```

### Download a previously-ready export

```bash
enclii export download exp_abc123 --out acme.tar.gz
```

## Notes

- Secret values are deliberately not in the tarball. To migrate secrets to a new project, use [`enclii secrets`](./secrets.md) after restore.
- Pre-signed download URLs expire after 15 minutes. Re-run `enclii export download` to mint a fresh URL.
- The 14-day retention is enforced by an R2 lifecycle rule. After expiry, you must re-run `enclii export` from scratch.
- Production exports queue waiting for HITL approval; the export id is returned immediately so you can poll without blocking your terminal.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing project, invalid export id) |
| `40` | Timeout waiting for export to become ready (`--wait`) |
| `50` | Authentication error |

## See Also

- [`enclii secrets`](./secrets.md) - Manage secrets (not exported)
- [`enclii projects`](./projects.md) - List projects you can export
- [`enclii audit`](./audit.md) - Project audit timeline (subset is included in the export)
