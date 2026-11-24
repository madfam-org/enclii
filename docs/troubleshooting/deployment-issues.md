---
title: Deployment Issues
description: Resolve deployment problems and rollback procedures
sidebar_position: 4
tags: [troubleshooting, deployment, kubernetes, rollback]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Deployment Issues Troubleshooting

This guide helps resolve issues with deploying services to the Enclii platform.

## Prerequisites

- [CLI installed](/cli/)
- Service configured with successful build

## Before You Start

The commands below read your project and service from shell variables, so
each one runs as written once they are set. Set them once per terminal
session:

| Variable | Set it to | Where to find it |
|---|---|---|
| `PROJECT` | your project's slug | `enclii projects list` |
| `SERVICE` | the service's name | `enclii projects services "$PROJECT"` |
| `ENVIRONMENT` | the environment, for example `production` | `enclii projects environments "$PROJECT"` |

Commands that take a service id (`--service-id`, `--service`) read it
from the project's service list:

```bash
SERVICE_ID=$(enclii projects services "$PROJECT" --json | jq -r --arg s "$SERVICE" '.[] | select(.name == $s) | .id')
```

## Quick Diagnosis

```bash
# Check service status
enclii ps --env "$ENVIRONMENT" --project "$PROJECT"

# View recent deployments
enclii deployments list --service "$SERVICE_ID"

# Check the latest deployment's details
enclii deployments latest --service "$SERVICE_ID"

# View service logs
enclii logs "$SERVICE" --env "$ENVIRONMENT" -f
```

## Common Deployment Errors

### Deployment Timeout

**Symptom**: "Deployment timed out waiting for pods to be ready"

**Causes**:
- Application takes too long to start
- Health checks failing
- Resource constraints
- Image pull issues

**Solutions**:

1. **Check service logs for startup errors**:
```bash
enclii logs "$SERVICE" --env "$ENVIRONMENT" -n 500

# Direct pod debugging (kubectl — when you need K8s event details)
kubectl describe pod -n <namespace> -l app=<service>
```

2. **Increase startup timeout** in service config:
```yaml
readinessProbe:
  initialDelaySeconds: 60  # Give more time to start
  periodSeconds: 10
  failureThreshold: 6
```

3. **Optimize application startup**:
   - Defer non-critical initialization
   - Use lazy loading for dependencies
   - Reduce container image size

### Health Check Failures

**Symptom**: "Readiness probe failed" or "Liveness probe failed"

**Causes**:
- Health endpoint not configured
- Wrong port or path
- Application crashed during startup
- Dependency unavailable (database, cache)

**Solutions**:

1. **Verify health endpoint works locally**:
```bash
curl http://localhost:3000/health
# Expected: HTTP 200 with body
```

2. **Check health endpoint configuration**:
```yaml
healthCheck: /health  # Or /healthz, /-/ping

readinessProbe:
  path: /health
  port: 3000

livenessProbe:
  path: /health
  port: 3000
```

3. **Implement proper health endpoint**:

```javascript
// Express example
app.get('/health', (req, res) => {
  // Check critical dependencies
  const dbOk = await checkDatabase();
  if (!dbOk) {
    return res.status(503).json({ status: 'unhealthy', db: 'down' });
  }
  res.json({ status: 'healthy' });
});
```

4. **Make liveness check simple** (avoid dependency checks):
```javascript
app.get('/healthz', (req, res) => {
  res.send('ok');  // Just confirm process is running
});
```

### CrashLoopBackOff

**Symptom**: Pod repeatedly crashes and restarts

**Causes**:
- Application crash on startup
- Missing environment variables
- Port conflict
- Permission issues
- Missing secrets/config

**Solutions**:

1. **Check logs from crashed container**:
```bash
enclii logs "$SERVICE" --env "$ENVIRONMENT" -n 500

# The CLI has no --previous flag; for the previous container's output (break-glass):
kubectl logs -n <namespace> <pod> --previous
```

2. **Verify environment variables** (run in the service's directory, or pass `-f path/to/service.yaml`):
```bash
enclii secrets list --env "$ENVIRONMENT"
```

3. **Check for common startup issues**:
   - Missing `DATABASE_URL` or connection strings
   - Port mismatch (app listens on different port than configured)
   - File permission errors

4. **Debug locally** with same environment. There is no env export command; `--json` gives a machine-readable list (add `--reveal` to include secret values, which is audit-logged):
```bash
enclii secrets list --env "$ENVIRONMENT" --json
docker run --env-file .env <image>
```

### Image Pull Errors

**Symptom**: "ImagePullBackOff" or "ErrImagePull"

**Causes**:
- Image doesn't exist
- Registry authentication issue
- Network connectivity
- Image name typo

**Solutions**:

1. **Verify image exists**:
```bash
# Check in GitHub Container Registry
docker pull ghcr.io/madfam-org/<service>:<tag>
```

2. **Check registry credentials** (kubectl — raw secret inspection):
```bash
kubectl get secret -n <namespace> registry-pull-secret -o yaml
```

3. **Verify image name format**:
   - Must be lowercase
   - Valid format: `ghcr.io/org/name:tag`

### Resource Limit Exceeded

**Symptom**: "OOMKilled" or pod evicted

**Causes**:
- Application uses more memory than allocated
- Memory leak
- Limits too restrictive

**Solutions**:

1. **Check current resource usage** (kubectl — metrics-server query):
```bash
kubectl top pod -n <namespace> -l app=<service>
```

2. **Increase resource limits**:
```yaml
resources:
  requests:
    cpu: "100m"
    memory: "128Mi"
  limits:
    cpu: "500m"
    memory: "512Mi"
```

3. **Profile application memory**:
   - Node.js: Use `--max-old-space-size`
   - Java: Tune JVM heap settings
   - Go: Profile with pprof

### Rollback Required

**Symptom**: New deployment causes issues, need to revert

**Solutions**:

```bash
# Quick rollback to the previous deployment
enclii rollback "$SERVICE" --env "$ENVIRONMENT"

# List deployments with their v-numbers
enclii deploy ls "$SERVICE"

# Flip traffic in under 30s instead of re-committing a manifest
enclii rollback "$SERVICE" --env "$ENVIRONMENT" --instant --reason "bad release"
```

To roll back to a specific deployment instead of the previous one, pass its
v-number from `enclii deploy ls` after the service name. A production
`--instant` rollback also needs `--change-ticket` with the change ticket's URL.

### Pod Scheduling Issues

**Symptom**: "Unschedulable" or "0/N nodes are available"

**Causes**:
- Insufficient cluster resources
- Node selector/affinity not matched
- Taints and tolerations

**Solutions**:

1. **Check cluster capacity** (kubectl — node-level inspection, no CLI equivalent):
```bash
kubectl describe nodes | grep -A5 "Allocated resources"
```

2. **Reduce resource requests** if over-provisioned:
```yaml
resources:
  requests:
    cpu: "50m"      # Reduced from 100m
    memory: "64Mi"  # Reduced from 128Mi
```

3. **Contact admin** if cluster scaling needed

## Deployment Strategies

### Canary Deployments

```bash
# Deploy with canary: 10% of traffic, auto-promote after the validation window
enclii deploy --env "$ENVIRONMENT" --canary 10 --validation-window 10m
```

The deploy prints `Canary rollout started:` followed by the rollout id. Each
of `enclii canary status` (`-f` tails it until a terminal state),
`enclii canary promote` (full rollout now) and `enclii canary rollback`
(abort, with a `--reason`) takes that rollout id and `--service "$SERVICE"`.

Production canaries also need `--change-ticket` with the change ticket's URL. See [enclii canary](/cli/commands/canary).

### Blue-Green Deployments

There is no blue-green strategy. `enclii deploy` does a rolling update by default or a canary with `--canary N`; for a fast revert use `enclii rollback` with the service name and `--instant`.

## Monitoring Deployments

### Real-time Status

```bash
# Check deployment progress (ps has no watch mode; re-run it, or deploy with --wait)
enclii ps --env "$ENVIRONMENT" --project "$PROJECT"
enclii deploy ls "$SERVICE"

# Stream logs during deployment
enclii logs "$SERVICE" --env "$ENVIRONMENT" -f
```

### Via kubectl (Admin Access — when CLI doesn't expose enough detail)

```bash
# Direct K8s rollout status
kubectl rollout status deployment/<service> -n <namespace>

# View deployment events
kubectl describe deployment/<service> -n <namespace>

# Watch pod status
kubectl get pods -n <namespace> -l app=<service> -w
```

## Environment Variables

### Common Configuration Issues

| Variable | Issue | Solution |
|----------|-------|----------|
| `DATABASE_URL` | Connection refused | Verify network policy allows egress |
| `PORT` | Address in use | Ensure PORT matches container config |
| `NODE_ENV` | Wrong behavior | Set explicitly to "production" |

### Managing Secrets

```bash
# Run in the service's directory, or pass -f path/to/service.yaml
# List current environment
enclii secrets list --env "$ENVIRONMENT"

# Set new variable
enclii secrets set LOG_LEVEL=info --env "$ENVIRONMENT"

# Set secret (encrypted), read from SECRET_KEY in your shell so the value never
# appears in this document or your shell history
enclii secrets set SECRET_KEY="$SECRET_KEY" --secret --env "$ENVIRONMENT"
```

## Related Documentation

- **Build Issues**: [Build Failures](./build-failures)
- **Networking**: [Networking Troubleshooting](./networking)
- **CLI Deploy Command**: [enclii deploy](/cli/commands/deploy)
- **Service Spec**: [Service Specification](/reference/service-spec)
