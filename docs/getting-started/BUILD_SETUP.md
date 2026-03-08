# Build Pipeline Setup Guide

This guide walks you through setting up the Enclii build pipeline for local development or production deployment.

## Overview

The Enclii build pipeline supports:
- **Buildpacks**: Automatic builds for Node.js, Go, Python, Ruby, Java applications
- **Dockerfile**: Custom container builds
- **Auto-detection**: Automatically chooses the best build strategy
- **Build caching**: Speeds up subsequent builds
- **Registry integration**: Pushes images to any container registry

## Prerequisites

- Linux or macOS operating system
- Root/sudo access for installing tools
- Internet connection for downloading dependencies
- A container registry account (GitHub Container Registry, Docker Hub, etc.)

## Quick Start

```bash
# 1. Run the automated setup script
chmod +x scripts/setup-build-tools.sh
./scripts/setup-build-tools.sh

# 2. Configure environment
cp .env.example .env
# Edit .env with your settings

# 3. Login to container registry
docker login ghcr.io -u YOUR_USERNAME -p YOUR_TOKEN

# 4. Start the API
make run-switchyard

# 5. Test a build
curl -X POST http://localhost:8080/v1/services/{service-id}/build \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"git_sha": "main"}'
```

## Detailed Setup

### 1. Install Build Tools

The build pipeline requires two tools: Docker and Pack CLI.

#### Option A: Automated Installation (Recommended)

Run the provided setup script:

```bash
cd /path/to/enclii
chmod +x scripts/setup-build-tools.sh
./scripts/setup-build-tools.sh
```

The script will:
- ✅ Install Docker (if not present)
- ✅ Install Pack CLI for Cloud Native Buildpacks
- ✅ Create build directories
- ✅ Verify installation

#### Option B: Manual Installation

##### Install Docker

**Linux:**
```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
sudo usermod -aG docker $USER
# Log out and back in for group changes to take effect
```

**macOS:**
```bash
# Download and install Docker Desktop from:
# https://www.docker.com/products/docker-desktop
```

**Verify:**
```bash
docker --version
docker info
```

##### Install Pack CLI

**Linux:**
```bash
PACK_VERSION="v0.32.1"
curl -sSL "https://github.com/buildpacks/pack/releases/download/${PACK_VERSION}/pack-${PACK_VERSION}-linux.tgz" | \
    sudo tar -C /usr/local/bin/ --no-same-owner -xzv pack
```

**macOS (with Homebrew):**
```bash
brew install buildpacks/tap/pack
```

**macOS (without Homebrew):**
```bash
PACK_VERSION="v0.32.1"
curl -sSL "https://github.com/buildpacks/pack/releases/download/${PACK_VERSION}/pack-${PACK_VERSION}-macos.tgz" | \
    sudo tar -C /usr/local/bin/ --no-same-owner -xzv pack
```

**Verify:**
```bash
pack --version
```

### 2. Configure Environment

Copy the example environment file and customize it:

```bash
cp .env.example .env
```

**Required Settings:**

```bash
# Build directories
BUILD_WORK_DIR=/tmp/enclii-builds
BUILD_CACHE_DIR=/var/cache/enclii-builds
BUILD_TIMEOUT=1800  # 30 minutes

# Container registry
REGISTRY=ghcr.io/your-org  # Change to your registry

# Database
ENCLII_DB_URL=postgres://user:pass@localhost:5432/enclii?sslmode=disable
```

**Create directories:**

```bash
mkdir -p /tmp/enclii-builds
sudo mkdir -p /var/cache/enclii-builds
sudo chmod 777 /var/cache/enclii-builds
```

### 3. Configure Container Registry

You need to authenticate with your container registry to push built images.

#### GitHub Container Registry (ghcr.io)

1. Create a Personal Access Token (PAT) on GitHub:
   - Go to Settings → Developer settings → Personal access tokens
   - Create token with `write:packages` scope

2. Login to registry:
   ```bash
   echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_USERNAME --password-stdin
   ```

3. Update `.env`:
   ```bash
   REGISTRY=ghcr.io/your-username
   ```

#### Docker Hub

```bash
docker login -u YOUR_DOCKER_USERNAME
# Update .env: REGISTRY=docker.io/your-username
```

#### Google Container Registry (GCR)

```bash
gcloud auth configure-docker
# Update .env: REGISTRY=gcr.io/your-project
```

#### AWS Elastic Container Registry (ECR)

```bash
aws ecr get-login-password --region us-east-1 | \
    docker login --username AWS --password-stdin 123456789.dkr.ecr.us-east-1.amazonaws.com
# Update .env: REGISTRY=123456789.dkr.ecr.us-east-1.amazonaws.com
```

### 4. Verify Installation

Check that all tools are properly installed:

```bash
# Check Docker
docker info

# Check Pack CLI
pack version

# Check build directories
ls -la /tmp/enclii-builds
ls -la /var/cache/enclii-builds

# Check registry authentication
cat ~/.docker/config.json | jq '.auths'
```

### 5. Run Database Migrations

Ensure the database schema is up to date:

```bash
cd apps/switchyard-api
go run cmd/api/main.go migrate up
```

### 6. Start the API Server

```bash
# From repository root
make run-switchyard

# Or manually:
cd apps/switchyard-api
go run cmd/api/main.go
```

The API will start on `http://localhost:8080` by default.

### 7. Test the Build Pipeline

#### Create a test user and project:

```bash
# Register a user
curl -X POST http://localhost:8080/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "password": "TestPassword123!",
    "name": "Test User"
  }'

# Save the access token from response
TOKEN="<access_token_from_response>"

# Create a project
curl -X POST http://localhost:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-project",
    "slug": "test-project"
  }'
```

#### Create a service:

```bash
curl -X POST http://localhost:8080/v1/projects/test-project/services \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "hello-world",
    "git_repo": "https://github.com/heroku/node-js-sample.git",
    "build_config": {
      "type": "auto"
    }
  }'

# Save the service ID from response
SERVICE_ID="<service_id_from_response>"
```

#### Trigger a build:

```bash
curl -X POST http://localhost:8080/v1/services/$SERVICE_ID/build \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "git_sha": "main"
  }'

# Save the release ID from response
RELEASE_ID="<release_id_from_response>"
```

#### Monitor build progress:

```bash
# Check release status (building → ready/failed)
curl http://localhost:8080/v1/services/$SERVICE_ID/releases \
  -H "Authorization: Bearer $TOKEN"

# Check API logs
tail -f logs/switchyard-api.log
```

#### Expected build timeline:

- **Cloning**: 5-30 seconds (depends on repo size)
- **First build**: 3-8 minutes (downloading base images)
- **Subsequent builds**: 30 seconds - 2 minutes (with cache)

## Build Types

### Auto-Detection

The builder automatically detects the build type based on files in the repository:

| File Found | Build Type | Language/Framework |
|------------|------------|-------------------|
| `Dockerfile` | dockerfile | Custom |
| `package.json` | buildpacks | Node.js |
| `go.mod` | buildpacks | Go |
| `requirements.txt` | buildpacks | Python |
| `Gemfile` | buildpacks | Ruby |
| `pom.xml` | buildpacks | Java/Maven |

### Buildpacks

Cloud Native Buildpacks automatically detect and build your application:

**Supported Languages:**
- Node.js (npm, yarn, pnpm)
- Go
- Python (pip, pipenv, poetry)
- Ruby (bundler)
- Java (Maven, Gradle)
- .NET Core
- PHP

**Builder Image:** `paketocommunity/builder-ubi-base:latest`

**Customize in service config:**
```json
{
  "build_config": {
    "type": "buildpack",
    "buildpack": "heroku/nodejs"
  }
}
```

### Dockerfile

For custom builds, provide a Dockerfile in your repository:

```dockerfile
FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
CMD ["node", "server.js"]
```

**Customize Dockerfile location:**
```json
{
  "build_config": {
    "type": "dockerfile",
    "dockerfile": "deploy/Dockerfile"
  }
}
```

## Troubleshooting

### Build fails with "pack: command not found"

**Solution:** Install Pack CLI
```bash
# Linux
curl -sSL "https://github.com/buildpacks/pack/releases/download/v0.32.1/pack-v0.32.1-linux.tgz" | \
    sudo tar -C /usr/local/bin/ --no-same-owner -xzv pack

# macOS
brew install buildpacks/tap/pack
```

### Build fails with "docker: command not found"

**Solution:** Install and start Docker
```bash
# Check if installed
docker --version

# Check if running
docker info

# Start Docker daemon (Linux)
sudo systemctl start docker
```

### Build fails with "permission denied while connecting to Docker daemon"

**Solution:** Add user to docker group
```bash
sudo usermod -aG docker $USER
# Log out and back in
```

### Build fails with "failed to push image: unauthorized"

**Solution:** Authenticate with registry
```bash
# GitHub Container Registry
docker login ghcr.io -u YOUR_USERNAME -p YOUR_TOKEN

# Docker Hub
docker login -u YOUR_USERNAME

# Verify
cat ~/.docker/config.json | jq '.auths'
```

### Build times out after 30 minutes

**Solution:** Increase timeout
```bash
# In .env
BUILD_TIMEOUT=3600  # 60 minutes
```

### Build uses too much disk space

**Solution:** Clean up old build artifacts
```bash
# Clean build work directory
rm -rf /tmp/enclii-builds/*

# Clean Docker images
docker image prune -a

# Clean buildpack cache
rm -rf /var/cache/enclii-builds/*
```

### Build logs are missing or incomplete

**Solution:** Check API logs
```bash
# View API logs
tail -f logs/switchyard-api.log

# Enable debug logging
# In .env:
ENCLII_LOG_LEVEL=debug
```

## Performance Optimization

### 1. Enable Build Caching

Build caching significantly speeds up subsequent builds:

```bash
# In .env
BUILD_CACHE_DIR=/var/cache/enclii-builds

# Ensure directory is writable
sudo chmod 777 /var/cache/enclii-builds
```

### 2. Limit Concurrent Builds

Prevent resource exhaustion by limiting concurrent builds:

```bash
# In .env
MAX_CONCURRENT_BUILDS=3
```

### 3. Use Faster Builder

For Node.js builds, consider using the Heroku builder:

```bash
# In .env
BUILDPACKS_BUILDER=heroku/builder:22
```

### 4. Docker BuildKit

Enable Docker BuildKit for faster builds:

```bash
# In .env
DOCKER_BUILDKIT=1
```

### 5. Pre-pull Base Images

Pre-pull commonly used base images to speed up first builds:

```bash
docker pull paketocommunity/builder-ubi-base:latest
docker pull node:18-alpine
docker pull golang:1.21-alpine
docker pull python:3.11-slim
```

## Security Best Practices

### 1. Use Private Registry

Never push images to public registries in production:

```bash
# Use private GitHub Container Registry
REGISTRY=ghcr.io/your-private-org

# Or private Docker Hub repository
REGISTRY=docker.io/your-private-username
```

### 2. Scan Images for Vulnerabilities

(Coming in Week 3)

```bash
# Enable vulnerability scanning
ENABLE_VULNERABILITY_SCAN=true

# Requires trivy installed
brew install trivy
```

### 3. Sign Images

(Coming in Week 3)

```bash
# Enable image signing
ENABLE_IMAGE_SIGNING=true

# Requires cosign installed
brew install cosign
```

### 4. Generate SBOM

(Coming in Week 3)

```bash
# Enable SBOM generation
ENABLE_SBOM=true

# Requires syft installed
brew install syft
```

### 5. Rotate Registry Credentials

Regularly rotate container registry credentials:

```bash
# Generate new token on GitHub/Docker Hub
# Update Docker login
docker logout ghcr.io
docker login ghcr.io -u YOUR_USERNAME -p NEW_TOKEN
```

## Production Deployment

### System Requirements

**Minimum:**
- 2 CPU cores
- 4 GB RAM
- 20 GB disk space
- Docker 20.10+
- Pack CLI 0.29+

**Recommended:**
- 4+ CPU cores
- 8+ GB RAM
- 50+ GB disk space (for build cache)
- SSD storage
- Dedicated build server

### High Availability

For production, run multiple build workers:

```bash
# Start multiple API instances with load balancer
# Each instance can handle MAX_CONCURRENT_BUILDS

# Instance 1
PORT=8080 make run-switchyard

# Instance 2
PORT=8081 make run-switchyard

# Instance 3
PORT=8082 make run-switchyard

# Load balancer (nginx/haproxy) distributes build requests
```

### Monitoring

Monitor build pipeline health:

```bash
# Check build status
curl http://localhost:8080/health

# Check build tools
curl http://localhost:8080/v1/build/status

# View metrics (Prometheus)
curl http://localhost:9090/metrics
```

### Backup Build Cache

Regularly backup the build cache to speed up recovery:

```bash
# Backup
tar -czf build-cache-backup.tar.gz /var/cache/enclii-builds

# Restore
tar -xzf build-cache-backup.tar.gz -C /
```

## Next Steps

- [Deploy your first service](QUICKSTART.md)
- [Testing Guide](../guides/TESTING_GUIDE.md) - Writing and running tests

## Support

For issues or questions:
- Check [Troubleshooting](#troubleshooting) section above
- Review [Architecture docs](../architecture/ARCHITECTURE.md)
- Open an issue on GitHub
- Contact the platform team

---

**Build Pipeline Version**: 1.0.0
**Last Updated**: 2025-01-19
**Status**: Production Ready (95%)
