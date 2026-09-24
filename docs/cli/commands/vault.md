# enclii vault

Inspect the cluster-internal HashiCorp Vault deployment.

## Synopsis

```bash
enclii vault <subcommand> [flags]
```

## Description

`enclii vault` is a thin wrapper for **status and health inspection** of the cluster-internal HashiCorp Vault. It does **not** read or write secrets. Audited backfill from Kubernetes Secrets to Vault is exposed through `enclii secrets vault-backfill`, and secret values are still never printed by the CLI.

For the operator procedure to initialize Vault after ArgoCD syncs the Application, see `internal-devops/runbooks/vault-bootstrap.md`.

## Subcommands

### `status`

Print Vault initialization and seal state.

```bash
enclii vault status [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--addr` | string | cluster-internal DNS | Override Vault address |
| `--json` | bool | `false` | Emit machine-readable JSON instead of human text |

The command calls Vault's `/v1/sys/health` endpoint and reports:

- Initialization state (`initialized` / `not initialized`)
- Seal state (`sealed` / `unsealed`)
- Standby state (for HA Vault)
- Server version

It then prints a next-step hint: the bootstrap runbook when Vault is not initialized, the unseal procedure when it is sealed, or "Vault is initialized and unsealed". If Vault cannot be reached, it prints `Vault: unreachable at <addr>` with a port-forward hint. All of these states exit `0`: `vault status` reports state, it does not gate on it. Check `Initialized`/`Sealed` (or the `--json` fields) in scripts.

## Examples

### Check Vault status

```bash
enclii vault status
```

**Output** (field layout; values depend on your cluster):
```
Vault address:  <addr>
Version:        <vault version>
Initialized:    true
Sealed:         false
Standby:        false

Status: Vault is initialized and unsealed.
```

A `Cluster name:` line is added when Vault reports one.

### JSON output for monitoring

```bash
enclii vault status --json
```

### Override the Vault address

```bash
enclii vault status --addr https://vault.example.internal:8200
```

The `--addr` flag is also honoured via the `ENCLII_VAULT_ADDR` and `VAULT_ADDR` environment variables.

## Notes

- The CLI reaches Vault via the **cluster-internal** DNS by default. To run from outside the cluster, port-forward the Vault Service or pass `--addr` with a reachable URL.
- `enclii vault status` does **not** require admin role on Enclii — it's read-only against Vault's public health endpoint. Vault's own auth still applies.
- This command intentionally has no `read` / `write` / `unseal` subcommands. Unseal is performed by a quorum of operators using shamir keys held outside the platform; see the bootstrap runbook.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Status reported, whatever the state: initialized, not initialized, sealed, or unreachable |
| `1` | The health response could not be read, or an invalid flag was passed |

## See Also

- [`enclii admin clusters`](./admin.md#clusters) - Inspect clusters where Vault runs
- [`enclii secrets`](./secrets.md) - Service-level secrets (Selva-managed)
- [`enclii db`](./db.md) - Inspect Postgres backed by Vault-stored credentials
