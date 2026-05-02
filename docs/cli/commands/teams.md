# enclii teams

Manage teams, memberships, and invitations.

## Synopsis

```bash
enclii teams <subcommand> [flags]
```

**Aliases:** `team`

## Description

The `teams` command manages teams — the access-control unit grouping users for shared access to projects. Members hold a role (`owner`, `admin`, `member`, `viewer`). Invitations are time-bounded; cancel unaccepted ones with `teams invitations-cancel`.

This command mirrors the `/teams` page in the consumer web UI at `enclii.dev`. All endpoints are served under `/v1/teams/*` by Switchyard. Read subcommands accept `--json` for stable machine-readable output; mutating subcommands require `--force` to skip the interactive `[y/N]` prompt.

## Subcommands

### `list`

List all teams.

```bash
enclii teams list [flags]
```

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `get`

Get team details by slug.

```bash
enclii teams get <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `create`

Create a new team.

```bash
enclii teams create --name <name> --slug <slug>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | | Team display name (required) |
| `--slug` | string | | Team URL slug (required) |

### `update`

Update team metadata.

```bash
enclii teams update <slug> --name <name>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--name` | string | | New team display name |

### `delete`

Delete a team.

```bash
enclii teams delete <slug> [--force]
```

**Aliases:** `rm`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

### `members`

List members of a team.

```bash
enclii teams members <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `members-update`

Change a member's role.

```bash
enclii teams members-update <slug> <member_id> --role <role>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--role` | string | | New role: `owner`\|`admin`\|`member`\|`viewer` (required) |

### `members-remove`

Remove a member from a team.

```bash
enclii teams members-remove <slug> <member_id> [--force]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

### `invite`

Invite a user to a team via email. The server sends an invitation email with a time-bounded acceptance link. Default role if `--role` is omitted is `member`.

```bash
enclii teams invite <slug> --email <email> [--role <role>]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--email` | string | | Invitee email address (required) |
| `--role` | string | `member` | Role: `owner`\|`admin`\|`member`\|`viewer` |

### `invitations`

List pending invitations for a team.

```bash
enclii teams invitations <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `invitations-cancel`

Cancel a pending invitation.

```bash
enclii teams invitations-cancel <slug> <invitation_id>
```

## Examples

### List teams

```bash
enclii teams list
```

**Output:**
```
SLUG      NAME       MEMBERS  CREATED
platform  Platform   12       2026-01-12 09:30
storefront Storefront 7        2026-02-04 14:11
```

### Create a team

```bash
enclii teams create --name "Platform" --slug platform
```

**Output:**
```
Team created: Platform (platform)
```

### Invite a user

```bash
enclii teams invite platform --email alice@example.com --role member
```

**Output:**
```
Invitation sent to alice@example.com as member (expires 2026-05-09T17:32:14Z)
```

### List pending invitations as JSON

```bash
enclii teams invitations platform --json
```

### Remove a member without confirmation

```bash
enclii teams members-remove platform usr_a3b4c5 --force
```

## Notes

- The `owner` role grants full team control including deletion. Demoting the only owner is rejected by the server.
- `teams delete` is irreversible and cascades to all team-scoped projects, memberships, and invitations.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing required flag, invalid role) |
| `50` | Authentication error |

## See Also

- [`enclii projects`](./projects.md) - Manage projects owned by a team
- [`enclii tokens`](./tokens.md) - Personal API tokens for CI access
- [`enclii audit`](./audit.md) - Audit team membership changes
