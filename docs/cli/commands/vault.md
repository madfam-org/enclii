# enclii vault

Inspect the cluster-internal HashiCorp Vault deployment.

## Synopsis

```bash
enclii vault <subcommand> [flags]
```

## Description

`enclii vault` is a thin wrapper for **status and health inspection** of the cluster-internal HashiCorp Vault. It does **not** read or write secrets — that path goes through RFC 0005 Selva tooling so the CLI cannot become a direct exfiltration path for secret values.

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

If Vault is not yet initialized, the command prints a hint pointing at the bootstrap runbook and exits non-zero.

## Examples

### Check Vault status

```bash
enclii vault status
```

**Output:**
```
Vault status
  Initialized: true
  Sealed:      false
  Standby:     false
  Version:     1.16.2
```

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
| `0` | Vault is initialized and reachable |
| `2` | Vault is unreachable or not yet initialized |
| `50` | Authentication error against the Enclii API (when running through `--api-endpoint`) |

## See Also

- [`enclii admin clusters`](./admin.md#clusters) - Inspect clusters where Vault runs
- [`enclii secrets`](./secrets.md) - Service-level secrets (Selva-managed)
- [`enclii db`](./db.md) - Inspect Postgres backed by Vault-stored credentials
