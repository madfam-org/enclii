# enclii volumes

Manage persistent volumes attached to a service.

## Synopsis

```bash
enclii volumes <subcommand> [flags]
```

**Aliases:** `volume`, `storage`, `pvc`

## Description

Persistent volumes are stored on the service record and materialized on deploy: the reconciler creates PVCs and mounts them into the workload. After changing volumes, **redeploy** the service for changes to take effect in the cluster.

Configure volumes via:

- **UI:** Service → Settings → Persistent volumes
- **CLI:** this command group
- **Spec reference:** [service-spec volumes](../../reference/service-spec.md#volumes)

## Subcommands

### `volumes list`

List volumes for a service.

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name |
| `--file`, `-f` | string | `service.yaml` | Path to service.yaml |

### `volumes set`

Replace all volumes from a JSON file.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name |
| `--file`, `-f` | string | `service.yaml` | Path to service.yaml |
| `--volumes-file` | string | | JSON array of volume objects (required) |

### `volumes add`

Add or update a single volume by name.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | | Volume name (required) |
| `--mount-path` | string | | Container mount path (required) |
| `--size` | string | `10Gi` | Size (e.g. `10Gi`) |
| `--storage-class` | string | `longhorn` | Storage class |
| `--access-mode` | string | `ReadWriteOnce` | Access mode |

### `volumes clear`

Remove all volumes from the service (does not delete existing PVCs until next reconcile; use Enclii ops for break-glass PVC cleanup).

## Examples

```bash
enclii volumes list

enclii volumes add --name data --mount-path /data --size 10Gi --storage-class longhorn

cat > volumes.json <<'EOF'
[
  {
    "name": "data",
    "mount_path": "/data",
    "size": "10Gi",
    "storage_class_name": "longhorn",
    "access_mode": "ReadWriteOnce"
  }
]
EOF
enclii volumes set --volumes-file volumes.json

enclii deploy --env production
```

## Staging proof

See [COMMERCIAL_GA_STAGING_PROOF.md](../../production/COMMERCIAL_GA_STAGING_PROOF.md) (`STORAGE_E2E_*` env vars).

## Related

- [Persistent volumes guide](../../guides/persistent-volumes.md)
- `enclii ops storage` — platform operator PVC inspection (admin)
