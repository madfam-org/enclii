---
title: Quick Start Guide
description: Get Enclii running locally in under 10 minutes
sidebar_position: 1
tags: [getting-started, quickstart, setup]
---

# Enclii MVP - Quick Start Guide

This guide will help you get the Enclii MVP running locally in under 10 minutes.

## Prerequisites

- Docker & Docker Compose
- Go 1.22+
- Node.js 20+
- kubectl
- kind (for local Kubernetes)

**macOS:** `brew install go node kind kubectl docker`

## 1. Bootstrap the Environment

```bash
git clone <repo-url> && cd enclii
make bootstrap
```

This installs dependencies and sets up the Go workspace.

## 2. Start with Docker Compose (Easiest)

```bash
# Copy environment variables
cp .env.example .env

# Start services
docker-compose -f docker-compose.dev.yml up -d

# Check health
curl http://localhost:8080/health
```

## 3. Or use Local Kubernetes

```bash
# Create kind cluster
make kind-up

# Install infrastructure
make infra-dev

# Build and run locally
make build-all
make run-switchyard  # Terminal 1
make run-ui         # Terminal 2
```

## 4. Test the CLI

```bash
# Build CLI
make build-cli

# Initialize a sample service
mkdir test-service && cd test-service
../bin/enclii init

# Check the generated service.yaml
cat service.yaml

# Try CLI commands
../bin/enclii ps
../bin/enclii logs api
../bin/enclii version
```

## 5. Access the UI

Open http://localhost:3000 to see the dashboard.

## Next Steps

- Read `README.md` for full documentation
- Check `CLAUDE.md` for development guidance
- Review `SOFTWARE_SPEC.md` for the complete vision

## Troubleshooting

- **Port conflicts:** Change ports in `.env`
- **Database issues:** `docker-compose down -v` then `up -d`
- **Build failures:** Ensure Go 1.22+ and run `go clean -modcache`

## Key Files

- `service.yaml` - Service configuration
- `Makefile` - Common commands
- `.env.example` - Environment variables
- `docker-compose.dev.yml` - Local development stack

## Related Documentation

- **Next Steps**: [Development Guide](/docs/getting-started/DEVELOPMENT)
- **CLI Reference**: [CLI Commands](/docs/cli/)
- **Deployment**: [Deploying Services](/docs/guides/ONBOARDING_GUIDE)
- **Troubleshooting**: [Common Issues](/docs/troubleshooting/)
- **FAQ**: [Frequently Asked Questions](/docs/faq/)