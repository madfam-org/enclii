# enclii deploy

Build and deploy a service to an environment, optionally as a canary.

## Synopsis

```bash
enclii deploy [flags]
enclii deploy ls [service] [flags]
enclii deploy show <v{n}|uuid> [service]
```

## Description

`enclii deploy` builds the service described by the current directory's `service.yaml` (or the file passed with `--file`) at the current git commit and deploys it to the target environment.

The flow is:

1. Read the git commit of the working directory (the command must run inside a git repository).
2. Parse the service spec (`service.yaml` by default).
3. Ensure the project, the service, and the target environment exist, creating them if they do not.
4. Trigger a build for the commit and wait for it to finish (build timeout: 10 minutes).
5. Deploy the resulting release to the environment with a rolling update.
6. With `--wait`, poll until the deployment is healthy (deploy timeout: 5 minutes). Without it, the command returns as soon as the deployment is initiated and prints the `enclii logs <service> -f` command to follow it.

The default strategy is a rolling update. Pass `--canary` to run a canary rollout instead (see [Canary deploys](#canary-deploys)). There are no `--strategy`, `--release`, `--skip-build`, `--timeout`, `--message`, or `--dry-run` flags.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `dev` | Environment to deploy to (for example `dev`, `staging`, `prod`). Created if it does not exist. |
| `--file`, `-f` | string | `service.yaml` | Path to the service spec file |
| `--wait`, `-w` | bool | `false` | Wait for the deployment to complete |
| `--canary` | string | | Deploy as a canary at this traffic percentage (`20%` or `20`). Range 5-50. |
| `--validation-window` | string | `10m` | How long the canary must stay healthy before auto-promote (for example `10m`, `30m`) |
| `--smoke-endpoint` | string | | Optional http(s) URL probed during canary validation (HTTP 200 = healthy) |
| `--change-ticket` | string | | Change ticket URL (required for production canary rollouts) |

Note that `-f` is `--file` here, not "follow". To follow a deployment's logs, use [`enclii logs <service> -f`](./logs.md).

## Subcommands

### `ls`

List a service's deployment history with Heroku-style v-numbers. Output columns: version, status, deployment id (short), created-at. If `service` is omitted, the service from this directory's `service.yaml` is used.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit`, `-n` | int | `20` | Maximum number of deployments to show |

### `show`

Show one deployment. The target is either a v-number (`v42`, which requires the `service` argument) or a full deployment UUID (`service` optional). Prints the deployment's id, status, health, replicas, release, environment, and creation time.

## Examples

### Deploy to the default (`dev`) environment

```bash
enclii deploy
```

### Deploy to staging and wait for it to become healthy

```bash
enclii deploy --env staging --wait
```

### Deploy a spec that is not in the current directory

```bash
enclii deploy -f services/api/service.yaml --env staging
```

### Canary deploy to production

```bash
enclii deploy --env prod --canary 10 --validation-window 30m \
  --change-ticket https://tracker.example.com/CHG-1234
```

### Inspect deployment history

```bash
enclii deploy ls api --limit 5
enclii deploy show v42 api
```

## Canary deploys

With `--canary N`, the command builds a fresh release, then starts a canary rollout that routes N% of traffic to the new digest by replica proportion. It holds for the validation window, then auto-promotes if healthy or auto-rolls-back if not. The command tails the rollout until it reaches a terminal state and exits non-zero (code `30`) if the canary is rolled back or fails.

Constraints reported by the CLI: the percentage must be between 5 and 50, the service needs at least 2 replicas, and StatefulSets are not supported.

Use [`enclii canary`](./canary.md) to inspect, promote, or abort a running rollout.

## Build Process

1. **Detect build type** (Nixpacks, Dockerfile, or Buildpacks)
2. **Build container image** with provenance metadata
3. **Generate SBOM** (Software Bill of Materials)
4. **Sign image** with cosign
5. **Push to registry** (ghcr.io/madfam-org)

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Deployment successful (or initiated, without `--wait`) |
| `1` | Other error (for example not in a git repository, or the spec file could not be parsed) |
| `10` | Validation error (invalid `--canary` percentage or `--validation-window`) |
| `20` | Build failed |
| `30` | Deployment failed, or the canary was rolled back / failed |
| `40` | Timeout (build after 10 minutes, deployment after 5 minutes) |

## See Also

- [`enclii canary`](./canary.md) - Inspect, promote, or abort a canary rollout
- [`enclii rollback`](./rollback.md) - Revert a deployment
- [`enclii deployments`](./deployments.md) - Query deployment runs across services
- [`enclii ps`](./ps.md) - Check service status
- [`enclii logs`](./logs.md) - View service logs
- [Service Spec Reference](../../reference/service-spec.md) — service configuration
