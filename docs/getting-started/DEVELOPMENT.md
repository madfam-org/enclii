---
title: Development Guide
description: Complete development environment setup and workflow guide
sidebar_position: 2
tags: [getting-started, development, setup, workflow]
---

# Enclii Development Guide

## Table of Contents
- [Development Environment Setup](#development-environment-setup)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Testing Guide](#testing-guide)
- [Debugging](#debugging)
- [Contributing](#contributing)

## Development Environment Setup

### Prerequisites

Install required tools:

**macOS:**
```bash
# Core tools
brew install go node pnpm docker kubectl helm kind

# Development tools
brew install golangci-lint cosign trivy jq yq

# Optional tools
brew install tilt skaffold k9s
```

**Linux:**
```bash
# Use your package manager (apt, yum, etc.)
sudo apt update
sudo apt install -y golang nodejs docker.io kubectl

# Install pnpm
npm install -g pnpm

# Install kind
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.23.0/kind-linux-amd64
chmod +x ./kind && sudo mv ./kind /usr/local/bin/
```

### Initial Setup

1. **Clone repository:**
```bash
git clone git@github.com:madfam/enclii.git
cd enclii
```

2. **Bootstrap environment:**
```bash
make bootstrap
```

This command:
- Installs Go dependencies
- Installs Node dependencies
- Sets up Git hooks
- Configures workspaces

3. **Create local cluster:**
```bash
make kind-up
make infra-dev
```

4. **Set environment variables:**
```bash
cp .env.example .env
# Edit .env with your configuration
source .env
```

## Project Structure

```
enclii/
├── apps/                       # Application services
│   ├── switchyard-api/        # Control plane API
│   │   ├── cmd/               # Entry points
│   │   ├── internal/          # Private packages
│   │   │   ├── api/          # HTTP handlers
│   │   │   ├── auth/         # Authentication
│   │   │   ├── cache/        # Redis cache
│   │   │   ├── db/           # Database layer
│   │   │   ├── k8s/          # Kubernetes client
│   │   │   └── reconciler/   # Reconciliation logic
│   │   └── pkg/               # Public packages
│   ├── switchyard-ui/         # Web UI
│   │   ├── app/              # Next.js app directory
│   │   ├── components/       # React components
│   │   └── lib/              # Utilities
│   └── reconcilers/           # Kubernetes operators
├── packages/                   # Shared packages
│   ├── cli/                   # CLI tool
│   ├── sdk-go/               # Go SDK
│   └── sdk-js/               # JavaScript SDK
├── infra/                      # Infrastructure configs
│   ├── k8s/                  # Kubernetes manifests
│   ├── terraform/            # IaC definitions
│   └── dev/                  # Local development
├── docs/                       # Documentation
├── scripts/                    # Build/deploy scripts
└── tests/                      # Integration tests
```

## Development Workflow

### Running Services Locally

**Start all services:**
```bash
# Terminal 1: API
make run-switchyard

# Terminal 2: UI
make run-ui

# Terminal 3: Reconcilers
make run-reconcilers
```

**Using Tilt (recommended for hot reload):**
```bash
tilt up
# Access Tilt UI: http://localhost:10350
```

### Code Development

#### API Development

1. **Make changes to API code**
2. **Run tests:**
```bash
cd apps/switchyard-api
go test ./...
```

3. **Run locally:**
```bash
go run cmd/api/main.go
```

4. **Test endpoints:**
```bash
curl -X GET http://localhost:8080/health
```

#### UI Development

1. **Start development server:**
```bash
cd apps/switchyard-ui
pnpm dev
```

2. **Make changes** - Hot reload enabled
3. **Run tests:**
```bash
pnpm test
pnpm test:e2e
```

#### CLI Development

1. **Build CLI:**
```bash
make build-cli
```

2. **Test commands:**
```bash
./bin/enclii --help
./bin/enclii init my-project
```

### Database Migrations

**Create migration:**
```bash
cd apps/switchyard-api
migrate create -ext sql -dir migrations -seq add_new_table
```

**Run migrations:**
```bash
migrate -path migrations -database "$ENCLII_DB_URL" up
```

**Rollback:**
```bash
migrate -path migrations -database "$ENCLII_DB_URL" down 1
```

## Testing Guide

### Unit Tests

**Go tests:**
```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run specific package
go test ./apps/switchyard-api/internal/api/...

# Run with race detection
go test -race ./...
```

**JavaScript tests:**
```bash
cd apps/switchyard-ui
pnpm test
pnpm test:watch  # Watch mode
pnpm test:coverage
```

### Integration Tests

```bash
# Run integration tests
make test-integration

# Run specific integration test
go test -tags=integration ./tests/integration/...
```

### End-to-End Tests

```bash
# Start services
make run-switchyard
make run-ui

# Run E2E tests
cd tests/e2e
pnpm test
```

### Benchmark Tests

```bash
# Run benchmarks
make test-benchmark

# Run specific benchmark
go test -bench=BenchmarkServiceReconciler ./...
```

### Test Coverage

```bash
# Generate coverage report
make test-coverage

# View HTML report
open apps/switchyard-api/coverage.html
```

## Debugging

### Local Debugging

#### VS Code Configuration

`.vscode/launch.json`:
```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug API",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/apps/switchyard-api/cmd/api",
      "env": {
        "ENCLII_DB_URL": "postgres://postgres:password@localhost:5432/enclii_dev",
        "ENCLII_LOG_LEVEL": "debug"
      }
    },
    {
      "name": "Debug CLI",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/packages/cli/cmd/enclii",
      "args": ["deploy", "--debug"]
    }
  ]
}
```

#### Delve Debugging

```bash
# Debug API
dlv debug ./apps/switchyard-api/cmd/api

# Debug with arguments
dlv debug ./packages/cli/cmd/enclii -- deploy --debug

# Attach to running process
dlv attach $(pgrep switchyard-api)
```

### Kubernetes Debugging

**Debug pods:**
```bash
# Get pod logs
kubectl logs -f deployment/switchyard-api

# Execute into pod
kubectl exec -it deployment/switchyard-api -- /bin/sh

# Port forward for debugging
kubectl port-forward deployment/switchyard-api 8080:8080

# Describe pod issues
kubectl describe pod <pod-name>
```

**Debug with k9s:**
```bash
k9s
# Use shortcuts:
# :pods - List pods
# :svc - List services
# :logs - View logs
```

### Database Debugging

**Connect to database:**
```bash
# Local database
psql postgres://postgres:password@localhost:5432/enclii_dev

# Pod database
kubectl exec -it deployment/postgres -- psql -U postgres enclii_dev
```

**Query examples:**
```sql
-- Check recent deployments
SELECT * FROM deployments 
ORDER BY created_at DESC 
LIMIT 10;

-- Debug slow queries
EXPLAIN ANALYZE 
SELECT * FROM services 
WHERE project_id = 'abc123';
```

### Performance Profiling

**CPU profiling:**
```go
import _ "net/http/pprof"

// In main.go
go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

```bash
# Capture CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# View in browser
go tool pprof -http=:8080 profile.pb.gz
```

**Memory profiling:**
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

## Contributing

### Code Style

**Go:**
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` and `golangci-lint`
- Write tests for new functionality
- Keep functions small and focused

**TypeScript/JavaScript:**
- Use TypeScript for type safety
- Follow React best practices
- Use functional components and hooks
- Write unit tests with Jest

### Git Workflow

1. **Create feature branch:**
```bash
git checkout -b feature/add-new-feature
```

2. **Make changes and commit:**
```bash
git add .
git commit -m "feat: add new feature"
```

3. **Push and create PR:**
```bash
git push origin feature/add-new-feature
```

### Commit Guidelines

Follow conventional commits:
- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation
- `style:` Formatting
- `refactor:` Code restructuring
- `test:` Adding tests
- `chore:` Maintenance

### Pull Request Process

1. **Before submitting:**
   - Run tests: `make test`
   - Run linter: `make lint`
   - Update documentation
   - Add changelog entry

2. **PR description should include:**
   - Problem description
   - Solution approach
   - Testing performed
   - Breaking changes (if any)

3. **Review process:**
   - Automated CI checks must pass
   - Code review from maintainer
   - Address feedback
   - Squash and merge

### Local Development Tips

**Speed up builds:**
```bash
# Enable BuildKit
export DOCKER_BUILDKIT=1

# Use build cache
docker buildx create --use
docker buildx build --cache-from type=local,src=/tmp/.buildx-cache .
```

**Mock external services:**
```go
// Use interfaces for dependencies
type Database interface {
    GetProject(id string) (*Project, error)
}

// Create mocks for testing
type MockDatabase struct {
    mock.Mock
}
```

**Debug network issues:**
```bash
# Check service connectivity
kubectl run debug --image=busybox -it --rm --restart=Never -- sh
nc -zv service-name 8080
```

### Performance Optimization

**Database queries:**
- Use prepared statements
- Add appropriate indexes
- Batch operations when possible
- Use connection pooling

**API responses:**
- Implement caching strategies
- Use pagination for lists
- Compress responses (gzip)
- Minimize JSON payload size

**Container images:**
- Use multi-stage builds
- Minimize layers
- Use alpine base images
- Remove unnecessary files

## Troubleshooting Common Issues

### Issue: Port already in use
```bash
# Find process using port
lsof -i :8080

# Kill process
kill -9 <PID>
```

### Issue: Database connection failed
```bash
# Check postgres is running
docker ps | grep postgres

# Restart postgres
docker restart postgres
```

### Issue: Kind cluster not accessible
```bash
# Delete and recreate cluster
make kind-down
make kind-up
```

### Issue: Go module errors
```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
go mod download
```

---

## Resources

- [Go Documentation](https://golang.org/doc/)
- [Kubernetes Documentation](https://kubernetes.io/docs/)
- [Next.js Documentation](https://nextjs.org/docs)
- [Docker Documentation](https://docs.docker.com/)

## Support

- Slack: #enclii-dev
- Issues: [GitHub Issues](https://github.com/madfam-org/enclii/issues)
- Wiki: [Internal Wiki](https://wiki.enclii.dev)

---

## Related Documentation

- **Getting Started**: [Quick Start Guide](/docs/getting-started/QUICKSTART)
- **CLI**: [CLI Reference](/docs/cli/) | [Deploy Command](/docs/cli/commands/deploy)
- **Guides**: [Testing Guide](/docs/guides/TESTING_GUIDE) | [Onboarding Guide](/docs/guides/ONBOARDING_GUIDE)
- **SDK**: [TypeScript SDK](/docs/sdk/typescript/)
- **Troubleshooting**: [Build Failures](/docs/troubleshooting/build-failures) | [Deployment Issues](/docs/troubleshooting/deployment-issues)
- **Production**: [Production Checklist](/docs/production/PRODUCTION_CHECKLIST)
- **Architecture**: [Platform Architecture](/docs/architecture/ARCHITECTURE)