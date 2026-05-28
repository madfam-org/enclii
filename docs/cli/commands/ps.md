# enclii ps

List services and their runtime status.

## Synopsis

```bash
enclii ps [flags]
```

## Description

The `ps` command displays the status of services in your project. It uses
deployment records for version/uptime and overlays the same runtime health feed
used by `enclii observe health`, so probe-derived health and replica counts do
not get stuck on stale deployment-row values.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `dev` | Environment label shown in output |

## Examples

### List All Services
```bash
enclii ps
```

**Output:**
```
NAME              STATUS       HEALTH       REPLICAS     VERSION                       UPTIME
api               running      healthy      2/2          argocd-abc1234 (abc1234)      5d 2h
web               running      healthy      2/2          argocd-def5678 (def5678)      5d 2h
```

### Filter by Environment
```bash
enclii ps --env production
```

## Status Values

| Status | Description |
|--------|-------------|
| `running` | Latest deployment is running |
| `pending` | Latest deployment is still pending or health is degraded before deployment status is known |
| `failed` | Latest deployment failed or runtime health is unhealthy before deployment status is known |
| `unknown` | No deployment/runtime signal is available |

## Health Values

| Health | Description |
|--------|-------------|
| `healthy` | Runtime health reports all pods ready |
| `degraded` | Runtime health reports some pods not ready |
| `unhealthy` | Runtime health reports no ready pods or failed probes |
| `unknown` | Runtime health could not be resolved and no fallback health is available |

## See Also

- [`enclii logs`](./logs.md) - View service logs
- [`enclii deploy`](./deploy.md) - Deploy a service
- [`enclii rollback`](./rollback.md) - Rollback deployment
