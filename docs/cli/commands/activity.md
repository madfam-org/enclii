# enclii activity

Stream lifecycle events (deploys, builds, env-var changes).

## Synopsis

```bash
enclii activity <subcommand> [flags]
```

## Description

The `activity` command lists and filters Switchyard's own activity rows: deploys, rollbacks, builds, env-var changes, team changes and similar actions recorded by the Switchyard API (`GET /v1/activity`). It is distinct from [`enclii audit`](./audit.md), which merges the audit logs of Janua (auth), Switchyard and Selva into one forensic view for compliance.

This command mirrors the `/activity` page in the consumer web UI. All subcommands are read-only and accept `--json`.

## Subcommands

### `list`

List recent lifecycle events.

```bash
enclii activity list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--action` | string | | Filter by action name |
| `--resource-type` | string | | Filter by resource type |
| `--limit` | int | `50` | Maximum number of events to return, 1 to 100. The API answers `400` outside that range (servers before [#625](https://github.com/madfam-org/enclii/pull/625) silently used 50). |
| `--json` | bool | `false` | Emit machine-readable JSON |

The table shows each row's time, action, resource type, resource (its `resource_name`, or `resource_id` when the name is empty), actor (`actor_email`) and outcome (`success`, `failure` or `denied`); an empty field prints as `-`. `--json` prints the API's response unchanged in shape:

```json
{
  "activities": [
    {
      "id": "0b9f6c1e-3c7a-4d57-9a53-2f1f6f3f0a01",
      "timestamp": "2026-09-24T09:14:00Z",
      "actor_email": "dev@example.com",
      "actor_role": "developer",
      "action": "deploy",
      "resource_type": "service",
      "resource_id": "5d0c2b7e-8e0a-4f7e-b1a4-0a4c1d3e9f10",
      "resource_name": "storefront",
      "ip_address": "203.0.113.7",
      "user_agent": "enclii-cli/1.0.0",
      "outcome": "success",
      "context": {}
    }
  ],
  "count": 1,
  "limit": 50,
  "offset": 0
}
```

CLI releases before this fix read an `events` array, which the API has never sent, so `activity list` always printed "No activity events match the given filters." and `--json` printed `{"events": null}`.

### `actions`

List the valid `--action` filter values supported by the server. Use this to discover available action names rather than hard-coding them.

```bash
enclii activity actions [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

### `resource-types`

List the valid `--resource-type` filter values supported by the server.

```bash
enclii activity resource-types [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Emit machine-readable JSON |

## Examples

### Recent activity (default 50)

```bash
enclii activity list
```

**Output:**
```
TIMESTAMP         ACTION    RESOURCE_TYPE  RESOURCE      ACTOR            OUTCOME
2026-09-24 09:14  deploy    service        storefront    dev@example.com  success
2026-09-24 09:12  update    env_var        DATABASE_URL  dev@example.com  success
2026-09-24 08:48  rollback  service        storefront    ops@example.com  failure
```

### Filter by action

```bash
enclii activity list --action deploy --limit 20
```

### Filter by resource type

```bash
enclii activity list --resource-type service --limit 100
```

### Discover valid filter values

```bash
enclii activity actions
```

**Output:**
```
create
update
delete
deploy
rollback
build
login
logout
invite
join
leave
```

### Pipe events to a watcher

```bash
enclii activity list --action deploy --json --limit 100 | \
  jq -r '.activities[] | select(.outcome != "success") | "\(.timestamp) \(.resource_name)"'
```

## Notes

- `activity` covers Switchyard's rows only. For auth events and Selva RFC ledgers as well, use `audit`.
- The `actions` and `resource-types` lists are the filter values the server suggests; if the server adds new ones they will appear there before being documented here.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `1` | Any error: invalid arguments or flags, API errors (including `403 Forbidden`), or an expired/invalid API token |

## See Also

- [`enclii audit`](./audit.md) - Full forensic audit log
- [`enclii deployments`](./deployments.md) - Detailed deployment runs
- [`enclii observe`](./observe.md) - Real-time service observability
