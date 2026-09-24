# enclii rollback

Rollback a service to a previous deployment.

## Synopsis

```bash
enclii rollback [service] [v{n}|digest] [flags]
```

## Description

The `rollback` command reverts a service to a previous deployment. With no target it rolls back to the previous running deployment. The target can be given positionally or with `--to`:

| Target form | Example | Resolves to |
|-------------|---------|-------------|
| v-number | `v42` | The deployment numbered v42 (preferred: v-numbers are monotonic per service and never reused) |
| Short id | `abc1234` | The deployment whose id starts with this prefix |
| UUID | `--to <uuid>` | That exact deployment |

`--to` overrides a positional target when both are set. The service name is required.

### Strategies

- **Default (manifest commit):** writes the previous image tag and lets the Deployment controller perform a rolling update. Takes a few minutes; the rollback is durably captured in git, which ArgoCD keeps as the state of record.
- **`--instant` (selector flip):** flips the Service selector at the routing layer. Traffic shifts in under 30 seconds when the previous ReplicaSet is still running, and under 90 seconds when it has to scale back up. ArgoCD reconciles in the background. Production instant rollbacks require `--change-ticket`.

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `service` | Yes | Service name to roll back |
| `v{n}\|digest` | No | Target deployment (v-number or id prefix). Defaults to the previous running deployment. |

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `dev` | Environment to roll back in |
| `--to`, `-t` | string | | Specific release/deployment ID or v-number (for example `v42`). Overrides the positional target. |
| `--instant` | bool | `false` | Flip traffic at the routing layer instead of re-committing a manifest |
| `--reason` | string | | Optional rollback reason, captured in the audit log |
| `--change-ticket` | string | | Change ticket URL (required for production instant rollbacks) |

There are no `--to-revision`, `--to-release`, `--dry-run`, `--wait`, or `--timeout` flags. Use [`enclii deploy ls`](./deploy.md#ls) to list v-numbers before choosing a target.

## Examples

### Roll back to the previous deployment

```bash
enclii rollback api --env prod
```

### Roll back to a specific v-number

```bash
enclii deploy ls api
enclii rollback api v42 --env prod
```

### Roll back to a deployment by UUID

```bash
enclii rollback api --to 3f0c2a4e-5b6d-4e7f-8a9b-0c1d2e3f4a5b --env staging
```

### Instant rollback of a bad production push

```bash
enclii rollback api --env prod --instant \
  --reason "error rate spike after v43" \
  --change-ticket https://tracker.example.com/CHG-1234
```

The instant path prints the time the flip took, whether a scale-up was needed, the number of ready replicas, and the from/to v-numbers.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Rollback initiated / completed |
| `1` | Other error (for example the service was not found) |
| `10` | Validation error (missing service name, or a target that does not resolve) |
| `30` | Rollback failed |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy a service; `enclii deploy ls` lists v-numbers
- [`enclii canary`](./canary.md) - Abort a canary that has not yet succeeded
- [`enclii ps`](./ps.md) - Check service status
- [`enclii logs`](./logs.md) - Debug deployment issues
