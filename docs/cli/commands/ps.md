# enclii ps

Last Updated: 2026-10-20

Public-safe CLI behavior; private operational evidence remains outside this repo
under the [repository boundary policy](../../PUBLIC_REPO_BOUNDARY.md).

List services and their runtime status.

## Synopsis

```bash
enclii ps [flags]
```

## Description

The `ps` command uses the read-only registered-environments inventory to resolve
the exact registered environment and its unique namespace, then includes runtime
services explicitly bound to that namespace.
Build-only services are excluded. Missing or ambiguous bindings return an error.
The runtime observation must identify the same service, project and namespace;
older API responses without namespace proof remain unknown. Cached service or
deployment health never substitutes for runtime evidence.

The latest deployment contributes version/uptime only when its environment and
release service match. Otherwise those columns remain `N/A`; this command does
not prove a particular release is deployed. Missing runtime observations, zero
ready replicas, degraded or unhealthy services return a nonzero exit status.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `dev` | Exact registered environment to observe |
| `--project`, `-p` | string | | Project slug. Defaults to `ENCLII_PROJECT`, then the local `service.yaml`/`.enclii.yml`, then your only project. |

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

### List Services in Another Project
```bash
enclii ps --project my-project
```

`enclii ps --project my-project --env staging` never inherits production health.
If the project has only production service bindings, staging returns an error
with no verified runtime services. This command does not create an environment,
bind workloads, start pods or modify a deployment.

## Status Values

| Status | Description |
|--------|-------------|
| `running` | Scoped runtime reports all desired replicas ready |
| `pending` | Scoped runtime reports only some desired replicas ready |
| `failed` | Scoped runtime reports no ready replicas |
| `unknown` | Scoped runtime proof is unavailable |

## Health Values

| Health | Description |
|--------|-------------|
| `healthy` | Runtime health reports all pods ready |
| `degraded` | Runtime health reports some pods not ready |
| `unhealthy` | Runtime health reports no ready pods or failed probes |
| `unknown` | Scoped runtime proof could not be resolved; no fallback is used |

## See Also

- [`enclii logs`](./logs.md) - View service logs
- [`enclii deploy`](./deploy.md) - Deploy a service
- [`enclii rollback`](./rollback.md) - Rollback deployment
