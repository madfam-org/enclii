# Persistent volumes

Attach Longhorn (or other storage class) PVCs to your service workloads. This is **Commercial GA bet C**.

## Concepts

- **Volume spec** is stored on the service (`services.volumes` JSON).
- On **deploy**, the reconciler creates PVCs and mounts them at the configured paths.
- **ReadWriteOnce** is the default for databases and single-replica apps; use **ReadWriteMany** only when your storage class supports it.

## Declare in service.yaml

```yaml
spec:
  volumes:
    - name: data
      mountPath: /data
      size: "10Gi"
      storageClassName: longhorn
      accessMode: ReadWriteOnce
```

Field reference: [service-spec — Volumes](../reference/service-spec.md#volumes).

## UI

**Services → &lt;service&gt; → Settings → Persistent volumes** — add, edit, save, then redeploy.

## CLI

```bash
enclii volumes list
enclii volumes add --name data --mount-path /data --size 10Gi
enclii volumes set --volumes-file volumes.json
enclii deploy --env production
```

See [volumes CLI reference](../cli/commands/volumes.md).

## Verify

1. Save volumes (UI or CLI).
2. Deploy a **ready** release to the target environment.
3. Confirm the deployment reaches `running` / `healthy`.
4. For automated proof, set `STORAGE_E2E_*` (and `STORAGE_E2E_RELEASE_ID` for deploy slice) per [COMMERCIAL_GA_STAGING_PROOF.md](../production/COMMERCIAL_GA_STAGING_PROOF.md).

## Operator inspection

Platform operators use `enclii ops storage` for cluster PVC/PV health — not for routine tenant volume edits.

## Related

- [SELF_HOSTING — Persistent volumes](./SELF_HOSTING.md#persistent-volumes)
- [Migrating from Railway — Volumes](./migrating-from-railway.md)
