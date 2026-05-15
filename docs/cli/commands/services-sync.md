# enclii services-sync

Synchronize checked-in Enclii service specs with the control plane.

## Synopsis

```bash
enclii services-sync --dir <path> --project <slug> [flags]
```

## Description

The `services-sync` command scans a directory for Enclii service specs
(`apiVersion: enclii.dev/v1` or `enclii.dev/v1alpha`, `kind: Service`) and
registers missing services in the Enclii control plane.

By default, existing services are left untouched. Use `--reconcile-existing`
when the checked-in spec should repair persisted service metadata drift, such
as an empty `git_repo`, stale `app_path`, stale auto-deploy fields, or stale
`build_config`.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dir` | string | `.` | Directory containing Enclii service YAML files |
| `--project` | string | `enclii` | Project slug to register services under |
| `--dry-run` | bool | `false` | Show what would be done without making changes |
| `--ignore-project-mismatch` | bool | `false` | Sync specs even when `metadata.project` differs from `--project` |
| `--reconcile-existing` | bool | `false` | Update existing service metadata when it differs from the checked-in spec |

## Examples

### Register missing services

```bash
enclii services-sync --dir . --project enclii
```

### Dry run before applying

```bash
enclii services-sync --dir apps/app --project forgesight --dry-run
```

### Repair existing service metadata drift

```bash
enclii services-sync --dir apps/app --project forgesight --dry-run --reconcile-existing
enclii services-sync --dir apps/app --project forgesight --reconcile-existing
```

This updates metadata only. It does not build or deploy. Run `enclii deploy`
after the service record is aligned.

## Validation

Before syncing, the CLI validates:

- YAML syntax.
- Enclii service specs only; Kubernetes `kind: Service` manifests are ignored.
- `metadata.project` alignment unless `--ignore-project-mismatch` is set.
- Service source metadata used by Enclii builds and webhooks.

## Relationship to deploy

| Command | Effect |
|---------|--------|
| `services-sync` | Creates missing service records and optionally reconciles existing metadata |
| `deploy` | Builds, creates release, and deploys |

Typical workflow:

```bash
# 1. Update checked-in .enclii.yml/service specs locally.

# 2. Inspect planned control-plane metadata changes.
enclii services-sync --dir apps/app --project forgesight --dry-run --reconcile-existing

# 3. Apply metadata reconciliation.
enclii services-sync --dir apps/app --project forgesight --reconcile-existing

# 4. Deploy when ready.
enclii deploy -f apps/app/.enclii.yml --env prod --wait
```

## See also

- [`enclii init`](./init.md) - Create initial configuration.
- [`enclii deploy`](./deploy.md) - Deploy the service.
- [Service Specification Reference](../../reference/service-spec.md).
