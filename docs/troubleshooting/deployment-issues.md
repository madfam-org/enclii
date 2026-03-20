---
title: Deployment Issues
description: Resolve deployment problems and rollback procedures
sidebar_position: 4
tags: [troubleshooting, deployment, kubernetes, rollback]
---

# Deployment Issues Troubleshooting

This guide helps resolve issues with deploying services to the Enclii platform.

## Prerequisites

- [CLI installed](/cli/)
- Service configured with successful build

## Quick Diagnosis

```bash
# Check deployment status
enclii ps --service <service-id>

# View recent deployments
enclii deployments list --service <service-id>

# Check deployment details
enclii deployments get <deployment-id>

# View service logs
enclii logs <service-name> -f
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
enclii logs <service-name> --previous

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
enclii logs <service-name> --previous
```

2. **Verify environment variables**:
```bash
enclii services env list --service <service-id>
```

3. **Check for common startup issues**:
   - Missing `DATABASE_URL` or connection strings
   - Port mismatch (app listens on different port than configured)
   - File permission errors

4. **Debug locally** with same environment:
```bash
enclii services env export --service <service-id> > .env
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
# Quick rollback to previous release
enclii rollback <service-name>

# Rollback to specific release
enclii rollback <service-name> --release <release-id>

# List available releases for rollback
enclii releases list --service <service-id>
```

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
# Deploy with canary (gradual rollout)
enclii deploy --strategy canary --canary-percent 10

# Check canary status
enclii deployments get <deployment-id>

# Promote canary to full rollout
enclii deployments promote <deployment-id>

# Abort canary
enclii deployments abort <deployment-id>
```

### Blue-Green Deployments

```bash
# Deploy new version alongside existing
enclii deploy --strategy blue-green

# Switch traffic to new version
enclii deployments switch <deployment-id>

# Rollback by switching back
enclii deployments switch --to previous
```

## Monitoring Deployments

### Real-time Status

```bash
# Watch deployment progress
enclii ps --watch

# Stream logs during deployment
enclii logs <service-name> -f
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
# List current environment
enclii services env list --service <service-id>

# Set new variable
enclii services env set --service <service-id> KEY=value

# Set secret (encrypted)
enclii secrets set --service <service-id> SECRET_KEY=sensitive-value
```

## Related Documentation

- **Build Issues**: [Build Failures](./build-failures)
- **Networking**: [Networking Troubleshooting](./networking)
- **CLI Deploy Command**: [enclii deploy](/cli/commands/deploy)
- **Service Spec**: [Service Specification](/reference/service-spec)
