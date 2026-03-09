# Switchyard API

The Enclii control plane API server.

## Overview

Switchyard is the core API that powers the Enclii platform. It manages:
- Projects, environments, and services
- Build and deployment orchestration
- Domain routing and TLS certificates
- User authentication and RBAC
- Audit logging and observability

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Switchyard API                          │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────────────┐│
│  │  Auth   │  │Projects │  │Services │  │  Deployments   ││
│  │Handlers │  │Handlers │  │Handlers │  │   Handlers     ││
│  └────┬────┘  └────┬────┘  └────┬────┘  └───────┬────────┘│
│       │            │            │               │          │
│  ┌────▼────────────▼────────────▼───────────────▼────────┐│
│  │                    Gin Router                          ││
│  │              + Middleware Stack                        ││
│  └────────────────────────┬──────────────────────────────┘│
│                           │                                │
│  ┌────────────────────────▼──────────────────────────────┐│
│  │                   Service Layer                        ││
│  │  (Business Logic, Validation, Orchestration)          ││
│  └────────────────────────┬──────────────────────────────┘│
│                           │                                │
│  ┌─────────┐  ┌──────────▼──────────┐  ┌─────────────────┐│
│  │PostgreSQL│  │     Repository     │  │      Redis      ││
│  │ (Data)   │  │      Layer         │  │   (Sessions)    ││
│  └──────────┘  └────────────────────┘  └─────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.22+
- PostgreSQL 15+
- Redis 7+

### Development Setup

```bash
# Clone repository
git clone https://github.com/madfam-org/enclii.git
cd enclii/apps/switchyard-api

# Copy environment template
cp .env.example .env
# Edit .env with your configuration

# Start dependencies
docker-compose up -d postgres redis

# Start the server (migrations run automatically)
go run cmd/api/main.go
# Server running at http://localhost:8080
```

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | API server port |
| `DATABASE_URL` | - | PostgreSQL connection string |
| `REDIS_URL` | `redis://localhost:6379` | Redis connection string |
| `OIDC_ISSUER` | - | OIDC provider URL (Janua) |
| `OIDC_AUDIENCE` | `enclii` | Expected JWT audience |
| `LOG_LEVEL` | `info` | Logging level |
| `ENCRYPTION_KEY` | - | 32-byte key for secret encryption |

## Project Structure

```
apps/switchyard-api/
├── cmd/
│   ├── api/           # Main API server entry point
│   └── migrate/       # Database migration tool
├── internal/
│   ├── api/           # HTTP handlers (28 files)
│   ├── auth/          # Authentication middleware
│   ├── config/        # Configuration loading
│   ├── database/      # Database connection and migrations
│   ├── models/        # Database models
│   ├── repository/    # Data access layer
│   └── service/       # Business logic layer
├── migrations/        # SQL migration files
└── docs/              # API documentation
```

## API Endpoints

See the [OpenAPI specification](../../docs/api/openapi.yaml) for complete documentation.

### Summary

| Category | Endpoints | Description |
|----------|-----------|-------------|
| Health | 2 | Liveness and readiness probes |
| Auth | 5 | Login, logout, token refresh |
| Projects | 5 | CRUD operations |
| Environments | 4 | Environment management |
| Services | 8 | Service configuration |
| Builds | 4 | Build triggering and status |
| Deployments | 6 | Deployment management |
| Domains | 5 | Custom domain configuration |
| Teams | 5 | Team and member management |

## Middleware Stack

```go
router.Use(
    middleware.RequestID(),       // Add request ID header
    middleware.Logger(),          // Structured logging
    middleware.Recovery(),        // Panic recovery
    middleware.CORS(),            // CORS headers
    middleware.RateLimit(),       // Rate limiting
    middleware.Auth(),            // JWT validation
    middleware.RBAC(),            // Role-based access control
    middleware.Audit(),           // Audit logging
)
```

## Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run specific package
go test ./internal/api/...

# Run integration tests
go test -tags=integration ./...
```

## Database Migrations

Migrations are raw SQL files in two locations:
- `internal/db/migrations/` — Core schema (genesis, admin foundation, etc.)
- `migrations/` — Incremental migrations

```bash
# Apply migrations in production (via kubectl)
kubectl exec -n enclii deploy/switchyard-api -- psql "$DATABASE_URL" -f /path/to/migration.sql

# Verify migration applied
kubectl exec -n enclii deploy/switchyard-api -- psql "$DATABASE_URL" -c "\dt"
```

## Building

```bash
# Build binary
go build -o bin/switchyard-api cmd/api/main.go

# Build with version info
go build -ldflags "-X main.version=v1.0.0" -o bin/switchyard-api cmd/api/main.go

# Build Docker image
docker build -t switchyard-api .
```

## Deployment

The API runs on Enclii itself (dogfooding):

```bash
enclii deploy --service switchyard-api --env production
```

See [DOGFOODING_GUIDE.md](../../docs/guides/DOGFOODING_GUIDE.md) for details.

## Related Components

- **[Switchyard UI](../switchyard-ui/)** - Web dashboard
- **[CLI](../../packages/cli/)** - Command-line interface
- **[Roundhouse](../roundhouse/)** - Build workers
- **[Reconcilers](../reconcilers/)** - Kubernetes controllers

## License

Apache 2.0 - See [LICENSE](../../LICENSE)
