# enclii canary

Manage in-flight canary rollouts.

## Synopsis

```bash
enclii canary <subcommand> [flags]
```

## Description

A canary rollout is started by `enclii deploy --canary=N%`. The platform then drives the rollout through a state machine in the background:

```
pending → running → validating → promoting → succeeded
                   ↘ aborted / rolled-back
```

The `canary` subtree lets you observe a rollout, **manually promote** it (skipping the validation window), or **abort it** with a recorded reason. Reasons are written to the audit log so post-incident reviews can trace why a canary was overridden.

Canary rollouts are scoped per service; pass `--service` to identify the target service.

## Subcommands

### `status`

Show canary rollout status, optionally tailing until the rollout reaches a terminal state.

```bash
enclii canary status <rollout_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service name or UUID (required — used to build the endpoint URL) |
| `--follow`, `-f` | bool | `false` | Tail until rollout reaches a terminal state |

### `promote`

Manually promote a canary, short-circuiting the validation window.

```bash
enclii canary promote <rollout_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service name or UUID |

### `rollback`

Manually abort a canary rollout. The previous stable revision continues serving 100% of traffic.

```bash
enclii canary rollback <rollout_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service` | string | | Service name or UUID |
| `--reason` | string | | Reason (captured in audit log) |

## Examples

### Watch a canary until it reaches a terminal state

```bash
enclii canary status rol_abc123 --service my-api --follow
```

### Promote a canary that has passed your manual smoke tests

```bash
enclii canary promote rol_abc123 --service my-api
```

### Abort a canary that's misbehaving

```bash
enclii canary rollback rol_abc123 --service my-api --reason "error rate spiked > 5% on /checkout"
```

### Start a canary deploy and watch it

```bash
enclii deploy --service my-api --env production --canary 10
# (rollout id is printed on stdout)
enclii canary status <rollout_id> --service my-api -f
```

## Notes

- `--service` is required because canary rollouts are addressed by `(service, rollout_id)` — the rollout id is **not** globally unique across services.
- `promote` is irreversible without a fresh deploy; it transitions the canary directly to the `promoting` state.
- `rollback` can be issued at any point before the rollout enters `succeeded`. Once a rollout has succeeded, use `enclii rollback` instead.
- Provide a meaningful `--reason` on rollback — it surfaces in `enclii audit` and in incident reports.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing service or rollout ID) |
| `30` | Rollout terminal state reached with failure (only when `--follow` is used) |
| `50` | Authentication error |

## See Also

- [`enclii deploy`](./deploy.md) - Start a canary rollout (`--canary N%`)
- [`enclii rollback`](./rollback.md) - Roll back a fully-succeeded deploy
- [`enclii observe`](./observe.md) - Service health metrics that drive canary validation
- [`enclii audit`](./audit.md) - See recorded rollback reasons
