# Refactoring Candidates - Codebase Audit

**Generated:** 2026-01-24
**Thresholds:** Error >800 lines | Warning 600-800 lines

## Critical Files (>800 lines) - 19 files

These files will **block commits** until refactored below 800 lines.

| File | Lines | Priority | Reason |
|------|-------|----------|--------|
| `apps/switchyard-api/internal/repositories/repositories.go` | 1899 | P0 | God object - multiple repository patterns |
| `apps/switchyard-api/internal/reconciler/service.go` | 1731 | P0 | Core reconciler - already modified for port logic |
| `apps/switchyard-api/internal/api/webhook_handlers.go` | 1404 | P1 | Multiple webhook handlers mixed |
| `apps/switchyard-api/tests/helpers.go` | 1152 | P2 | Test helpers - lower priority |
| `apps/switchyard-api/internal/api/types.go` | 1122 | P1 | Type definitions - consider splitting by domain |
| `apps/switchyard-api/internal/service/project_service.go` | 1084 | P1 | Project service business logic |
| `apps/switchyard-api/internal/api/project_handlers.go` | 1048 | P1 | REST handlers - split by operation type |
| `apps/switchyard-api/internal/api/environment_handlers.go` | 965 | P2 | Environment handlers |
| `apps/switchyard-api/internal/service/service_service.go` | 932 | P2 | Service management logic |
| `apps/switchyard-api/internal/service/build_service.go` | 899 | P2 | Build orchestration |
| `apps/switchyard-api/internal/reconciler/controller.go` | 895 | P2 | Reconciler controller |
| `apps/switchyard-api/internal/api/service_handlers.go` | 878 | P2 | Service REST handlers |
| `apps/switchyard-api/internal/service/environment_service.go` | 856 | P2 | Environment business logic |
| `apps/switchyard-api/internal/api/deployment_handlers.go` | 844 | P2 | Deployment handlers |
| `apps/switchyard-api/internal/service/deployment_service.go` | 832 | P3 | Deployment logic |
| `apps/switchyard-api/internal/api/build_handlers.go` | 821 | P3 | Build handlers |
| `apps/admin-console/components/TunnelMonitor.tsx` | 815 | P3 | React component |
| `apps/switchyard-api/internal/repositories/project_repository.go` | 812 | P3 | Project DB layer |
| `apps/switchyard-api/internal/api/router.go` | 805 | P3 | Route definitions |

## Warning Files (600-800 lines) - 11 files

These files trigger warnings but don't block commits.

| File | Lines | Notes |
|------|-------|-------|
| `apps/switchyard-api/internal/repositories/environment_repository.go` | 798 | Environment DB queries |
| `apps/switchyard-api/internal/repositories/deployment_repository.go` | 756 | Deployment DB queries |
| `apps/switchyard-api/internal/repositories/service_repository.go` | 744 | Service DB queries |
| `apps/switchyard-api/internal/reconciler/resources.go` | 723 | K8s resource generation |
| `apps/switchyard-ui/components/ProjectCard.tsx` | 698 | React component |
| `apps/switchyard-api/internal/service/auth_service.go` | 687 | Auth business logic |
| `apps/switchyard-api/internal/api/auth_handlers.go` | 665 | Auth REST handlers |
| `apps/switchyard-api/internal/repositories/build_repository.go` | 654 | Build DB queries |
| `apps/switchyard-api/internal/api/middleware.go` | 643 | HTTP middleware |
| `apps/roundhouse/internal/builder/builder.go` | 632 | Build worker |
| `apps/admin-console/app/dashboard/page.tsx` | 620 | Dashboard page |

---

## Refactoring Patterns

### Pattern 1: Split by Domain
**Applies to:** `repositories.go`, `types.go`

```
repositories.go (1899 lines)
  → project_repo.go
  → environment_repo.go
  → service_repo.go
  → deployment_repo.go
  → build_repo.go
  → common_repo.go

types.go (1122 lines)
  → project_types.go
  → environment_types.go
  → service_types.go
  → api_types.go
  → common_types.go
```

### Pattern 2: Extract Handlers
**Applies to:** `webhook_handlers.go`, `project_handlers.go`

```
webhook_handlers.go (1404 lines)
  → github_webhook.go
  → gitlab_webhook.go
  → generic_webhook.go
  → webhook_validation.go

project_handlers.go (1048 lines)
  → project_crud.go
  → project_settings.go
  → project_members.go
  → project_imports.go
```

### Pattern 3: Service Layer Decomposition
**Applies to:** `project_service.go`, `service_service.go`

```
project_service.go (1084 lines)
  → project_core.go
  → project_settings.go
  → project_imports.go
  → project_validation.go

service_service.go (932 lines)
  → service_core.go
  → service_deployment.go
  → service_config.go
```

### Pattern 4: Controller Split
**Applies to:** `reconciler/service.go`, `reconciler/controller.go`

```
reconciler/service.go (1731 lines)
  → service_reconcile.go
  → service_resources.go
  → service_networking.go
  → service_validation.go

reconciler/controller.go (895 lines)
  → controller_core.go
  → controller_events.go
  → controller_sync.go
```

---

## Priority Order

1. **P0 (Immediate):** `repositories.go`, `reconciler/service.go`
   - Largest files, most frequently modified
   - High risk of merge conflicts

2. **P1 (High):** `webhook_handlers.go`, `types.go`, `project_service.go`, `project_handlers.go`
   - Core API functionality
   - Moderate change frequency

3. **P2 (Medium):** All files 850-970 lines
   - Secondary handlers and services
   - Can be addressed incrementally

4. **P3 (Low):** Files 800-850 lines
   - Lower change frequency
   - Address when touching anyway

---

## Pre-commit Hook Behavior

The pre-commit hook at `scripts/hooks/pre-commit` now enforces:

- **Error (blocks commit):** Files >800 lines
- **Warning (allows commit):** Files 600-800 lines

### Exclusions

The following are excluded from file length checks:
- Test files (`*_test.go`, `*.test.ts`, `*.spec.ts`, files in `/tests/`)
- Generated files (`*generated*`, `*.gen.*`, `mock_*`)
- Non-source files (only checks `.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.py`)

### Bypass

To bypass the check (not recommended):
```bash
git commit --no-verify -m "message"
```
