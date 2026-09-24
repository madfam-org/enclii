# enclii local

Manage the local MADFAM development environment.

## Synopsis

```bash
enclii local <subcommand> [flags]
```

## Description

The `local` command group runs the MADFAM ecosystem on a workstation against shared local infrastructure, matching the production topology:

- Shared PostgreSQL (one database per service, for example `janua_dev`, `enclii_dev`)
- Shared Redis (a DB index per service)
- MinIO for object storage
- MailHog for email testing

The infrastructure is started with `docker compose` from `ops/local/docker-compose.shared.yml` in a `solarpunk-foundry` checkout. The commands expect sibling checkouts of `solarpunk-foundry`, `janua`, and `enclii` under `$HOME/labspace`; they fail with "compose file not found" when the foundry checkout is missing. `docker`, `docker compose`, and `lsof` must be on `PATH`.

## Subcommands

| Subcommand | Description |
|------------|-------------|
| [`up`](#up) | Start infrastructure and services |
| [`down`](#down) | Stop services and (by default) infrastructure |
| [`status`](#status) | Show infrastructure containers and service ports |
| [`logs`](#logs) | View infrastructure container logs |
| [`infra`](#infra) | Start only the shared infrastructure |

---

## up

Start the local development environment. Without arguments, it starts the shared infrastructure and then both known services, `janua` and `enclii`. With service names, it starts only those services (infrastructure still starts first unless `--skip-infra` is set). Unknown service names are skipped with a warning.

### Synopsis
```bash
enclii local up [services...] [flags]
```

### Flags
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--skip-infra` | bool | `false` | Skip infrastructure startup (assumes it is already running) |

There are no `--build`, `--detach`, `--infra-only`, or `--service` flags; use [`enclii local infra`](#infra) to start only the infrastructure, and pass service names as arguments.

### What starts

| Service | Processes | Ports |
|---------|-----------|-------|
| infrastructure | `docker compose up -d`, then waits up to 60 seconds for PostgreSQL and Redis | 5432, 6379, 9000/9001 (MinIO), 8025 (MailHog) |
| `janua` | Alembic migrations, the API (uvicorn), and the dashboard, admin, docs, and website Next.js apps | 4100-4104 |
| `enclii` | The switchyard API (`go run ./cmd/api` with `ENCLII_AUTH_MODE=local`) | 4200 |

### Examples

```bash
# Start everything
enclii local up

# Start infrastructure + Janua only
enclii local up janua

# Infrastructure is already running: start only Enclii
enclii local up enclii --skip-infra
```

**Output** (tail, after the per-step progress lines, for `enclii local up`):
```
✅ Local environment is ready!

📋 Service URLs:
   PostgreSQL:    localhost:5432
   Redis:         localhost:6379
   MinIO Console: http://localhost:9001
   MailHog:       http://localhost:8025

   Janua:
     API:       http://localhost:4100
     Dashboard: http://localhost:4101
     Admin:     http://localhost:4102
     Docs:      http://localhost:4103
     Website:   http://localhost:4104

   Enclii:
     API: http://localhost:4200
     UI:  http://localhost:4201
     CLI: export ENCLII_API_ENDPOINT=http://localhost:4200
```

---

## down

Stop the local development environment. It stops the application processes (`uvicorn`, `next-server`, `switchyard`), then runs `docker compose down` for the infrastructure unless `--keep-infra` is set. Named volumes are not removed.

### Synopsis
```bash
enclii local down [flags]
```

### Flags
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--keep-infra` | bool | `false` | Stop services but keep the infrastructure (PostgreSQL, Redis, etc.) running |

### Examples

```bash
# Stop everything
enclii local down

# Stop services, keep databases running
enclii local down --keep-infra
```

---

## status

Show the infrastructure containers (`docker compose ps`) and whether each known port has a listener: Janua 4100-4104, Enclii 4200-4201, PostgreSQL 5432, Redis 6379, MinIO 9000/9001, and MailHog 8025.

### Synopsis
```bash
enclii local status
```

This command has no flags; there is no JSON output mode.

---

## logs

View logs for the infrastructure containers through `docker compose logs`. Pass a compose service name (for example `postgres`) to limit the output to one container.

### Synopsis
```bash
enclii local logs [service] [flags]
```

### Flags
| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--follow`, `-f` | bool | `false` | Follow log output |
| `--lines`, `-n` | int | `100` | Number of lines to show |

### Examples

```bash
# All infrastructure logs
enclii local logs

# PostgreSQL logs
enclii local logs postgres

# Follow all logs
enclii local logs -f

# Last 50 lines
enclii local logs -n 50
```

---

## infra

Start only the shared infrastructure (PostgreSQL, Redis, MinIO, MailHog). Useful when you run services by hand or under a debugger. It prints the local connection details when the infrastructure is ready.

### Synopsis
```bash
enclii local infra
```

This command takes no arguments or flags; it has no `list`, `add`, or `remove` actions.

## See Also

- [`enclii init`](./init.md) - Initialize service configuration
- [`enclii deploy`](./deploy.md) - Deploy to remote environments
