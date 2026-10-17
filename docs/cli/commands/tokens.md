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
| `--scopes` | string | | Comma-separated role list: `admin`, `developer` or `viewer` (default: developer, capped at your role). See [Scopes](#scopes). |
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
ID                                    NAME        CREATED           LAST USED         EXPIRES
3f2b8c1e-4d5a-4e6f-9a7b-8c9d0e1f2a3b  ci-deploy   2026-02-15 09:30  2026-05-02 08:11  2026-05-16 09:30
7a6b5c4d-3e2f-4a1b-8c9d-0e1f2a3b4c5d  local-dev   2026-04-20 12:00  (never)           2026-07-19 12:00
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
  Token: enclii_3f7d9b2c8e1a4f6d...truncated
===================================================================

```

**STDOUT:**
```
ID:      c4d5e6f7-a8b9-4c0d-9e1f-2a3b4c5d6e7f
Name:    ci-deploy
Created: 2026-05-02T17:32:14Z
Expires: 2026-07-31T17:32:14Z
```

### Create a short-lived token

```bash
enclii tokens create --name short-lived --expires-in 24h
```

### Pipe creation metadata into a CI config

```bash
enclii tokens create --name release-bot --json > token-metadata.json
# The plaintext token still goes to STDERR — capture it manually.
```

### Revoke a token without confirmation

```bash
enclii tokens revoke 3f2b8c1e-4d5a-4e6f-9a7b-8c9d0e1f2a3b --force
```

**Output:**
```
Token '3f2b8c1e-4d5a-4e6f-9a7b-8c9d0e1f2a3b' revoked.
```

## Security

- **Never commit tokens to version control.** Use a CI secret store (GitHub Actions secrets, GitLab CI variables, Vault).
- The plaintext token is only available at creation time; it is hashed before storage and cannot be recovered.
- Revoke tokens that may have been exposed immediately. Rotation cost is low; recovery cost from a leak is high.
- Set the shortest practical `--expires-in` for the use case, and revoke tokens you no longer need.

### Scopes

Scopes are roles. The API accepts `admin`, `developer` and `viewer` and rejects any other string with `400`. A token's requests run with the highest role its scopes name (developer when none is given), and never above the role the token's owner holds at the time of the request: demoting an account demotes its tokens at once.

You cannot create a token above your own role (`403`). Only an account whose role is admin, or a platform admin, can create an `admin` token. A token cannot create a token stronger than itself.

Tokens created before this check may still carry other scope strings such as `deploy` or `read`. They are shown by `list`/`get` and grant nothing beyond developer.

Tokens are sent as `Authorization: Bearer enclii_…`, which is also how the [TypeScript SDK](../../sdk/typescript/authentication.md)'s `token` option uses them.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `1` | Any error: invalid arguments or flags (for example missing `--name` or an invalid `--expires-in`), API errors (including `403 Forbidden`), or an expired/invalid API token |

## See Also

- [`enclii login`](./login.md) - Interactive browser login (preferred for humans)
- [`enclii whoami`](./whoami.md) - Show current authenticated identity
- [`enclii audit`](./audit.md) - Audit token use and revocation events
