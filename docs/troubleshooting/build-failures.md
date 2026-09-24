---
title: Build Failures
description: Diagnose and fix build pipeline issues
sidebar_position: 3
tags: [troubleshooting, builds, buildpacks, docker]
---

# Build Failures Troubleshooting

This guide helps diagnose and fix issues with the Enclii build pipeline.

## Prerequisites

- [CLI installed](/cli/)
- Service configured and connected to a Git repository

## Quick Diagnosis

```bash
# Recent builds (releases) with version, git SHA, status, and the error message of failed builds
enclii releases <service-name>

# All builds, not just the last 10
enclii releases <service-name> --all
```

## Common Build Errors

### Build Detection Failed

**Symptom**: "No buildable project detected"

**Causes**:
- No Dockerfile present
- Project type not supported by Buildpacks
- Missing required files (package.json, go.mod, etc.)

**Solutions**:

1. **Add a Dockerfile** (most reliable):

```dockerfile
# Example for Node.js
FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --production
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
```

2. **Ensure buildpack-required files exist**:

| Runtime | Required Files |
|---------|---------------|
| Node.js | `package.json` |
| Go | `go.mod` |
| Python | `requirements.txt` or `Pipfile` |
| Ruby | `Gemfile` |
| Java | `pom.xml` or `build.gradle` |

3. **Set the root path** if in a monorepo. The CLI has no command that edits a service's root path; it comes from the checked-in service spec (`spec.build.source.git.path`, with `spec.build.context` for the build context). After fixing the spec, repair the stored service metadata (for example a stale `app_path` or `build_config`):

```bash
enclii services-sync --dir apps/my-service --project <project> --reconcile-existing
```

### Dependency Installation Failed

**Symptom**: "npm install failed" or "go mod download failed"

**Causes**:
- Private dependency access
- Network timeout
- Version conflicts
- Lockfile corruption

**Solutions**:

**For Node.js**:
```bash
# Regenerate lockfile locally
rm -rf node_modules package-lock.json
npm install
git add package-lock.json
git commit -m "fix: regenerate lockfile"
git push
```

**For Go**:
```bash
# Clear module cache and retidy
go clean -modcache
go mod tidy
git add go.mod go.sum
git commit -m "fix: tidy go modules"
git push
```

**For private dependencies**, configure authentication:
```bash
# Add npm token for private packages
# (run in the service's directory, or pass -f path/to/service.yaml)
enclii secrets set NPM_TOKEN=<token> --secret
```

### Out of Memory During Build

**Symptom**: "Build killed: OOMKilled" or process terminated unexpectedly

**Causes**:
- Large project or many dependencies
- Memory-intensive compilation (TypeScript, Webpack)
- Default memory limits too low

**Solutions**:

1. **Request larger build resources** (contact admin)

2. **Optimize build process**:

```dockerfile
# Use multi-stage builds
FROM node:20-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:20-alpine
WORKDIR /app
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/node_modules ./node_modules
CMD ["node", "dist/index.js"]
```

3. **Reduce parallelism**:
```json
// package.json
"scripts": {
  "build": "tsc --incremental"
}
```

### Build Timeout

**Symptom**: "Build timed out after 30 minutes"

**Causes**:
- Very large project
- Slow dependency downloads
- Hanging build step

**Solutions**:

1. **Identify slow step** by reviewing the build job's logs (the CLI shows build status and errors via `enclii releases <service-name>`, not step timings; see [Via kubectl](#via-kubectl-admin)):
```bash
kubectl logs -n enclii-builds job/<job-name> | grep -E "^\d+:\d+:"
```

2. **Cache dependencies**:
   - Use lockfiles for reproducible installs
   - Consider pre-building base images with common dependencies

3. **Split build steps** if using multi-service monorepo

### Docker Build Errors

**Symptom**: "docker build failed" with specific error

**Common Docker errors**:

| Error | Cause | Solution |
|-------|-------|----------|
| `COPY failed: file not found` | File path incorrect | Check paths relative to Dockerfile context |
| `FROM invalid reference format` | Invalid base image | Verify image name and tag exist |
| `RUN /bin/sh: command not found` | Missing binary in base image | Use correct base image or install dependency |
| `EXPOSE invalid port` | Non-numeric port | Use numeric port: `EXPOSE 3000` |

**Debugging Docker builds locally**:

```bash
# Build locally with same context
docker build -t test-build .

# Build with verbose output
docker build --progress=plain -t test-build .
```

### Registry Push Failed

**Symptom**: "Failed to push image to registry"

**Causes**:
- Registry authentication issue
- Image name/tag format error
- Registry quota exceeded

**Solutions**:

1. **Verify registry credentials**:
```bash
# Check registry is configured
kubectl get secret -n enclii registry-credentials
```

2. **Check image naming**:
   - Format: `ghcr.io/madfam-org/<service>:<tag>`
   - Tags must be lowercase
   - No special characters except `-` and `_`

3. **Check registry quota** on GitHub Container Registry settings

## Build Configuration

### Specifying Build Type

```yaml
# In service configuration
build:
  type: dockerfile  # or "buildpack"
  dockerfile: ./Dockerfile  # path to Dockerfile
  context: .  # build context
```

### Build Arguments

The CLI has no command for setting build arguments. Declare them in the service spec; `enclii services-sync` stores them in the service's build config (add `--reconcile-existing` for a service that already exists):

```yaml
spec:
  build:
    type: dockerfile
    buildArgs:
      NODE_ENV: production
```

### Ignoring Files

Create `.dockerignore`:
```
node_modules
.git
*.md
.env*
tests/
```

## Viewing Build Logs

### Via CLI

The CLI does not stream build logs. It shows each build's status and, for failed builds, the error message:

```bash
enclii releases <service-name>
enclii releases <service-name> -n 5
```

### Via kubectl (Admin)

```bash
# Find build job
kubectl get jobs -n enclii-builds

# View build logs
kubectl logs -n enclii-builds job/<job-name>

# View Roundhouse worker logs
kubectl logs -n enclii -l app=roundhouse -f
```

## Retrying Builds

There is no retry command. `enclii deploy` builds the current git commit of the service in `./service.yaml` (or `-f <path>`) and deploys it:

```bash
enclii deploy --env <env>
```

## Related Documentation

- **Deployment Issues**: [Deployment Troubleshooting](./deployment-issues)
- **Build Pipeline**: [Build Pipeline Implementation](/implementation/BUILD_PIPELINE_IMPLEMENTATION)
- **Service Spec**: [Service Specification Reference](/reference/service-spec)
