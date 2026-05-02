# enclii tokens

Manage personal API tokens for CI and scripts.

## Synopsis

```bash
enclii tokens <subcommand> [flags]
```

**Aliases:** `token`

## Description

API tokens authenticate non-interactive callers (CI runners, deploy scripts, automation) against the Switchyard API. Treat them like passwords.

The full token plaintext value is shown **ONCE** at creation — there is no way to retrieve it later. If you lose a token, revoke it and create a new one. List and get subcommands return only metadata (id, name, scopes, created_at, last_used_at, expires_at) — never the secret itself.

This command mirrors the `/account/tokens` page in the consumer web UI.

## Subcommands

### `list`

List your API tokens (metadata only).

```bash
enclii tokens list [flags]
```

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `get`

Get token metadata. Never returns the secret value.

```bash
enclii tokens get <token_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `create`

Create a new API token. The full token value is printed to **STDERR** with a clear warning banner; **STDOUT** receives only metadata (or JSON metadata with `--json`). This separation lets you pipe metadata to other tools while keeping the secret out of structured output.

Default expiry is **90 days**. Use Go duration syntax extended with `d` for days: `24h`, `30d`, `90d`. Negative or zero durations are rejected.

```bash
enclii tokens create --name <name> [--expires-in <duration>] [--scopes <list>]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | | Human-readable token name (required) |
| `--expires-in` | string | `90d` | Token lifetime: e.g. `24h`, `30d`, `90d` |
| `--scopes` | string | | Comma-separated scope list (default: full account access) |
| `--json` | bool | `false` | Emit machine-readable JSON metadata to stdout |

### `revoke`

Revoke an API token immediately. Any CI runs using it start failing on the next request.

```bash
enclii tokens revoke <token_id> [--force]
```

**Aliases:** `rm`, `delete`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

## Examples

### List your tokens

```bash
enclii tokens list
```

**Output:**
```
ID                 NAME           CREATED           LAST USED         EXPIRES
tkn_a3b4c5d6e7f8   ci-deploy      2026-02-15 09:30  2026-05-02 08:11  2026-05-16 09:30
tkn_b2c3d4e5f6a7   local-dev      2026-04-20 12:00  (never)           2026-07-19 12:00
```

### Create a token for CI (default 90d expiry)

```bash
enclii tokens create --name ci-deploy
```

**STDERR:**
```

===================================================================
  STORE THIS TOKEN NOW. YOU WILL NOT SEE IT AGAIN.
===================================================================
  Token: encl_pat_3f7d9b2c8e1a4f6d...truncated
===================================================================

```

**STDOUT:**
```
ID:      tkn_c4d5e6f7a8b9
Name:    ci-deploy
Created: 2026-05-02T17:32:14Z
Expires: 2026-07-31T17:32:14Z
```

### Create a short-lived scoped token

```bash
enclii tokens create --name short-lived --expires-in 24h --scopes deploy,logs
```

### Pipe creation metadata into a CI config

```bash
enclii tokens create --name release-bot --json > token-metadata.json
# The plaintext token still goes to STDERR — capture it manually.
```

### Revoke a token without confirmation

```bash
enclii tokens revoke tkn_a3b4c5d6e7f8 --force
```

**Output:**
```
Token 'tkn_a3b4c5d6e7f8' revoked.
```

## Security

- **Never commit tokens to version control.** Use a CI secret store (GitHub Actions secrets, GitLab CI variables, Vault).
- The plaintext token is only available at creation time; it is hashed before storage and cannot be recovered.
- Revoke tokens that may have been exposed immediately. Rotation cost is low; recovery cost from a leak is high.
- Set the shortest practical `--expires-in` for the use case. Use `--scopes` to limit blast radius.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing `--name`, invalid duration) |
| `50` | Authentication error |

## See Also

- [`enclii login`](./login.md) - Interactive browser login (preferred for humans)
- [`enclii whoami`](./whoami.md) - Show current authenticated identity
- [`enclii audit`](./audit.md) - Audit token use and revocation events
