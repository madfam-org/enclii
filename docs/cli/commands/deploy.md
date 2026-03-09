# enclii deploy

Deploy a service to an environment.

## Synopsis

```bash
enclii deploy [flags]
```

## Description

The `deploy` command builds and deploys your service to the specified environment. It reads the `enclii.yaml` configuration, triggers a build, creates a release, and deploys to the target environment with the specified strategy.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `preview` | Target environment: `preview`, `staging`, `production` |
| `--strategy` | string | `rolling` | Deployment strategy: `rolling`, `blue-green`, `canary` |
| `--canary-percent` | int | `10` | Initial traffic percentage for canary deployments |
| `--wait`, `-w` | bool | `true` | Wait for deployment to complete |
| `--timeout` | duration | `10m` | Deployment timeout |
| `--skip-build` | bool | `false` | Use existing release (requires `--release`) |
| `--release` | string | | Specific release ID to deploy |
| `--message`, `-m` | string | | Deployment message/description |
| `--dry-run` | bool | `false` | Validate without deploying |

## Examples

### Deploy to Preview
```bash
enclii deploy
# Deploys to preview environment with rolling strategy
```

### Deploy to Production with Canary
```bash
enclii deploy --env production --strategy canary --canary-percent 5
```

**Output:**
```
Building service...
  Detected: Node.js (nixpacks)
  Building: ████████████████████ 100%
  Image: ghcr.io/acme/api:v1.2.3

Creating release...
  Release: rel_abc123
  Commit:  a1b2c3d (feat: add user endpoint)
  SBOM:    generated

Deploying to production...
  Strategy: canary (5% initial traffic)
  Progress: ████████████████████ 100%

Deployment successful!
  URL:     https://api.acme.com
  Release: rel_abc123
  Status:  healthy (5% traffic)

Next: Monitor metrics, then run:
  enclii deploy --env production --release rel_abc123 --canary-percent 100
```

### Blue-Green Deployment
```bash
enclii deploy --env staging --strategy blue-green
```

### Deploy Specific Release
```bash
enclii deploy --env production --skip-build --release rel_abc123
```

### Dry Run (Validate Only)
```bash
enclii deploy --env production --dry-run
```

## Deployment Strategies

### Rolling (Default)
Gradually replaces old instances with new ones. Zero downtime, but both versions run briefly during transition.

```bash
enclii deploy --strategy rolling
```

### Blue-Green
Deploys to inactive environment, then switches traffic atomically. Instant rollback capability.

```bash
enclii deploy --strategy blue-green
```

### Canary
Routes a percentage of traffic to new version. Gradually increase if metrics are healthy.

```bash
# Initial canary deployment
enclii deploy --strategy canary --canary-percent 10

# Promote to full traffic after validation
enclii deploy --release rel_abc123 --canary-percent 100
```

## Build Process

1. **Detect build type** (Nixpacks, Dockerfile, or Buildpacks)
2. **Build container image** with provenance metadata
3. **Generate SBOM** (Software Bill of Materials)
4. **Sign image** with cosign
5. **Push to registry** (ghcr.io/madfam-org)

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Deployment successful |
| `10` | Validation error (invalid config) |
| `20` | Build failed |
| `30` | Deployment failed |
| `40` | Timeout |

## See Also

- [`enclii rollback`](./rollback.md) - Revert deployment
- [`enclii ps`](./ps.md) - Check deployment status
- [`enclii logs`](./logs.md) - View deployment logs
- [Service Spec Reference](../../reference/service-spec.md) — deployment strategy configuration
