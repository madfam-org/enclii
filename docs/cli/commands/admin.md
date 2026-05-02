# enclii admin

Platform operator commands (admin-console parity).

## Synopsis

```bash
enclii admin <subcommand> [flags]
```

## Description

The `admin` subtree exposes operator-only commands that mirror the **admin-console** web portal at `admin.enclii.dev`. **Most subcommands require admin role on the calling identity**; non-admin callers receive `403 Forbidden`.

Subcommands are read-only by default; mutations require `--force` so they cannot be executed by accident in CI scripts that pass through every flag. Read subcommands accept `--json` for stable machine-readable output.

The tree:

| Group | Purpose |
|-------|---------|
| `fleet` | Manage bare-metal hosts (inventory, firmware, partition, power) |
| `topology` | Show fleet/cluster topology graph |
| `clusters` | Manage Kubernetes clusters registered with the platform |
| `drift` | Inspect and resolve cluster drift events |
| `propagation` | Manage cross-cluster propagation policies |
| `governance` | Manage governed resources and their policies |
| `costs` | Inspect platform-level cost allocations and summaries |
| `vclusters` | Manage virtual clusters (storage/infrastructure tab) |

## Fleet

### `admin fleet list`

```bash
enclii admin fleet list [--json]
```

### `admin fleet get`

```bash
enclii admin fleet get <id>
```

### `admin fleet register`

```bash
enclii admin fleet register --hostname <h> --region <r> [--role worker|control] --force
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--hostname` | string | | Host DNS name (required) |
| `--region` | string | | Region/zone (required) |
| `--role` | string | | Role tag (`worker`\|`control`) |
| `--force` | bool | `false` | Confirm registration |

### `admin fleet firmware`

```bash
enclii admin fleet firmware <id> --version <v> --force
```

### `admin fleet partition`

```bash
enclii admin fleet partition <id> --layout <name> --force
```

### `admin fleet wipe`

**Irreversible.** Wipes all data on a host. When `--force` is omitted, the CLI prompts for confirmation **twice** — once for `[y/N]` and once for typed `yes` — to make accidental triggering effectively impossible. Passing `--force` skips both prompts.

```bash
enclii admin fleet wipe <id> [--force]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip both interactive confirmations |

### `admin fleet power`

```bash
enclii admin fleet power <id> --state on|off|reboot --force
```

## Topology

### `admin topology`

Show the fleet/cluster topology graph as JSON.

```bash
enclii admin topology
```

## Clusters

### `admin clusters list` / `get` / `register` / `update` / `deregister`

```bash
enclii admin clusters list [--json]
enclii admin clusters get <id>
enclii admin clusters register --name <n> --kubeconfig-file <path> --force
enclii admin clusters update <id> --name <n> --force
enclii admin clusters deregister <id> [--force]
```

`register` reads the kubeconfig from the local filesystem and uploads it; the file must be readable by the calling user. `deregister` prompts before deletion unless `--force` is supplied.

## Drift

### `admin drift list` / `get` / `resolve`

```bash
enclii admin drift list [--status <s>] [--json]
enclii admin drift get <id>
enclii admin drift resolve <id> --reason <text> --force
```

`resolve` requires a free-text `--reason` for the audit trail.

## Propagation

### `admin propagation list` / `get` / `create` / `delete`

```bash
enclii admin propagation list [--json]
enclii admin propagation get <id>
enclii admin propagation create \
  --name <n> \
  --source-cluster <id> \
  --target-clusters <id1,id2> \
  --resource-kind <Kind> \
  --force
enclii admin propagation delete <id> [--force]
```

`--target-clusters` is a comma-separated list of cluster IDs.

## Governance

### `admin governance list-resources` / `get-resource` / `create-resource` / `update-policy` / `delete-resource`

```bash
enclii admin governance list-resources [--json]
enclii admin governance get-resource <id>
enclii admin governance create-resource --kind <k> --name <n> --owner <o> --force
enclii admin governance update-policy <id> --policy-file <path> --force
enclii admin governance delete-resource <id> [--force]
```

`--policy-file` is read from the local filesystem and uploaded as the new policy body.

## Costs

### `admin costs allocations` / `summary` / `allocate`

```bash
enclii admin costs allocations [--from <ts>] [--to <ts>] [--json]
enclii admin costs summary [--from <ts>] [--to <ts>] [--json]
enclii admin costs allocate --resource <id> --tenant <id> --amount-cents <n> --force
```

`--amount-cents` accepts amounts in minor currency units (e.g. `1250` for $12.50).

## VClusters

### `admin vclusters list` / `get` / `provision` / `teardown` / `kubeconfig`

```bash
enclii admin vclusters list [--json]
enclii admin vclusters get <id>
enclii admin vclusters provision --name <n> --node <node_id> --force
enclii admin vclusters teardown <id> [--force]
enclii admin vclusters kubeconfig <id> [--out <path>]
```

`kubeconfig` writes raw YAML to stdout, or to `--out <path>` (mode `0600`).

## Examples

### Fleet inventory in JSON

```bash
enclii admin fleet list --json
```

### Open drift events

```bash
enclii admin drift list --status open
```

**Output:**
```
ID        RESOURCE                     SEVERITY  DETECTED              STATUS
drft_a3b  cluster-1/ns-prod/svc-api    high      2026-05-02T08:14:00Z  open
drft_c4d  cluster-2/ns-data/sts-pg     medium    2026-05-02T07:02:11Z  open
```

### Cost summary for January

```bash
enclii admin costs summary --from 2026-01-01 --to 2026-01-31
```

### Fetch a vCluster kubeconfig to disk

```bash
enclii admin vclusters kubeconfig vc_a3b4c5 --out /tmp/vc-kubeconfig.yaml
```

**Output:**
```
Kubeconfig written to /tmp/vc-kubeconfig.yaml
```

### Wipe a host (double confirmation if `--force` is omitted)

```bash
enclii admin fleet wipe host-7
# Wipe host host-7? This destroys all data. [y/N]: y
# Type 'yes' again to confirm wipe: yes
# Wipe initiated.
```

## Notes

- The whole subtree assumes admin role; without it, every call fails fast with `403`.
- Mutating commands intentionally require `--force` even in interactive use, so a single command line shows the operator's full intent. `wipe` and `deregister`-class commands additionally prompt without `--force`.
- For day-to-day non-admin work, prefer the consumer commands: [`enclii projects`](./projects.md), [`enclii teams`](./teams.md), [`enclii deployments`](./deployments.md).

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing required flag, missing `--force`, invalid state) |
| `50` | Authentication error (including `403 Forbidden` for non-admin callers) |

## See Also

- [`enclii audit`](./audit.md) - Audit log of admin actions
- [`enclii activity`](./activity.md) - Lifecycle event stream
- [`enclii observe`](./observe.md) - Service observability
