# Contributing to Enclii

Thank you for your interest in contributing to Enclii! This guide covers everything you need to get started.

## Prerequisites

- **Go** >= 1.24
- **Node.js** >= 20
- **pnpm** >= 9
- **Docker** >= 24
- **kubectl** >= 1.29
- **kind** >= 0.23 (for local development)
- **Helm** >= 3.14

macOS:
```bash
brew install go node pnpm kind helm kubectl docker
```

## Local Development Setup

```bash
# 1. Clone the repo
git clone https://github.com/madfam-org/enclii
cd enclii

# 2. Install dependencies and git hooks
make bootstrap

# 3. Start local services
docker-compose up -d postgres redis

# 4. Database is auto-migrated on API startup

# 5. Start the control plane API
make run-switchyard  # Starts on :8080

# 6. Start the web UI (in a separate terminal)
make run-ui          # Starts on :3000
```

## Code Structure

```
enclii/
  apps/
    switchyard-api/   # Control plane API (Go)
    switchyard-ui/    # Web dashboard (Next.js)
    roundhouse/       # Build workers (Go)
    waybill/          # Infrastructure cost metering (Go)
    dispatch/         # Admin control platform (Next.js)
    status/           # Status page (Next.js)
    landing/          # Landing page (Next.js)
    docs/             # Documentation site (Next.js)
  packages/
    cli/              # `enclii` CLI (Go)
    sdk-go/           # Go SDK with shared types
  infra/
    terraform/        # Infrastructure as Code
    k8s/              # Kubernetes manifests
    argocd/           # GitOps configuration
```

## Development Workflow

### Branch Strategy

We use **trunk-based development** on `main`.

1. Create a feature branch from `main`:
   ```bash
   git checkout -b feat/my-feature
   ```
2. Make your changes with small, focused commits
3. Open a PR when ready for review

### Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/) for changelog generation:

```
feat(api): add custom domain provisioning endpoint
fix(cli): correct exit code on auth timeout
docs(readme): update architecture diagram
chore(deps): bump Go to 1.24
```

### Validation Before Commit

Always run validation before pushing:

```bash
# Run all checks
make precommit

# Or run individually:
# Go
golangci-lint run ./apps/switchyard-api/...
golangci-lint run ./apps/waybill/...
golangci-lint run ./packages/cli/...

# TypeScript
cd apps/switchyard-ui && pnpm typecheck && pnpm lint
```

### Testing

```bash
# All unit tests
make test

# Specific Go package
go test ./apps/switchyard-api/internal/api/...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# E2E tests
make e2e
```

## Pull Request Process

1. Ensure all checks pass (`make precommit`)
2. Write a clear PR description explaining **what** and **why**
3. Keep PRs focused -- one feature or fix per PR
4. Request review from a maintainer
5. Address review feedback with new commits (don't force-push)

## Code Style

- **Go**: Follow standard Go conventions. We use `golangci-lint` for enforcement
- **TypeScript**: Follow the existing patterns. We use ESLint + Prettier
- **Naming**: Match existing project conventions (see `CLAUDE.md` for details)

## License

By contributing to Enclii, you agree that your contributions will be licensed under the [AGPL-3.0 License](./LICENSE).
