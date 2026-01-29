# Enclii Codebase: Quick Reference Guide

## Component Status at a Glance

| Component | Location | Status | Implementation | Key Files |
|-----------|----------|--------|-----------------|-----------|
| **Switchyard API** | `apps/switchyard-api/` | ✅ 75% | 70 Go files, 27 packages | `cmd/api/main.go`, `internal/api/*.go` |
| **Conductor CLI** | `packages/cli/` | ✅ 70% | 12 Go files, 6 commands | `cmd/enclii/main.go`, `internal/cmd/*.go` |
| **Switchyard UI** | `apps/switchyard-ui/` | ⚠️ 20% | 6 TSX files (mock data) | `app/page.tsx`, `app/projects/*.tsx` |
| **SDK-Go** | `packages/sdk-go/` | ✅ Complete | 3 Go files, type defs | `pkg/types/types.go` |
| **Reconcilers** | `apps/reconcilers/` | ❌ Stub | Only go.mod | (Empty - logic in switchyard-api) |
| **Roundhouse** | (Missing) | ❌ 0% | Not implemented | - |
| **Junctions** | (Partial in API) | ⚠️ 40% | Domain mgmt + ingress | `internal/reconciler/` |
| **Timetable** | (Missing) | ❌ 0% | Not implemented | - |
| **Signal** | (Partial) | ⚠️ 50% | Logging + partial metrics | `internal/monitoring/`, `internal/logging/` |
| **Waybill** | (Missing) | ❌ 0% | Not implemented | - |
| **Lockbox** | `internal/lockbox/` | ✅ Complete | Vault integration | `internal/lockbox/vault.go` |

---

## Critical Issues

### 🔴 BLOCKING BUG: Authentication Middleware

**File:** `/home/user/enclii/apps/switchyard-api/internal/middleware/auth.go` (lines 90-96)

**Problem:**
```go
// WRONG: Validates HMAC instead of RSA
if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
    return nil, jwt.ErrSignatureInvalid
}
return a.jwtSecret, nil  // WRONG: Should return publicKey
```

**Fix:**
```go
// CORRECT: Validate RSA
if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
    return nil, jwt.ErrSignatureInvalid
}
return a.publicKey, nil  // Use public key for verification
```

**Impact:** All authentication requests fail with 401 Unauthorized

**Effort:** 3 lines of code, 1 day testing

---

## Repository Structure

```
/home/user/enclii/
├── apps/
│   ├── switchyard-api/      ✅ MAIN APPLICATION (22,621 LOC)
│   │   ├── cmd/api/         - Entry point
│   │   ├── internal/
│   │   │   ├── api/         - HTTP handlers (10 handler files)
│   │   │   ├── auth/        - JWT, passwords (RS256)
│   │   │   ├── middleware/  - Auth, CSRF, security
│   │   │   ├── services/    - Business logic (Auth, Project, Deployment)
│   │   │   ├── db/          - Repositories, migrations (4 migrations)
│   │   │   ├── builder/     - Build service
│   │   │   ├── reconciler/  - K8s reconciliation (784 LOC)
│   │   │   ├── k8s/         - K8s client
│   │   │   ├── lockbox/     - Vault integration
│   │   │   ├── audit/       - Audit logging
│   │   │   ├── monitoring/  - Prometheus metrics
│   │   │   ├── cache/       - Redis caching
│   │   │   ├── validation/  - Input validation
│   │   │   └── 15 more...
│   ├── switchyard-ui/       ⚠️ MINIMAL (6 TSX files)
│   │   ├── app/
│   │   ├── contexts/        - AuthContext (stub)
│   │   ├── lib/             - API client (mock)
│   │   └── middleware.ts
│   └── reconcilers/         ❌ STUB (only go.mod)
├── packages/
│   ├── cli/                 ✅ FUNCTIONAL (12 Go files)
│   │   ├── cmd/enclii/      - Main entry
│   │   └── internal/
│   │       ├── cmd/         - Commands: init, deploy, logs, ps, rollback
│   │       ├── client/      - API client wrapper
│   │       ├── spec/        - YAML parser
│   │       └── config/      - Config management
│   └── sdk-go/              ✅ COMPLETE (3 Go files)
│       └── pkg/types/       - Shared type definitions
├── infra/
│   ├── k8s/
│   │   ├── base/            ✅ K8s manifests (10 files)
│   │   ├── staging/         ✅ Staging overlays
│   │   └── production/      ✅ Production overlays
│   ├── dev/                 ✅ Kind config
│   └── (terraform/ MISSING) ❌
├── tests/integration/       ✅ COMPREHENSIVE (5 Go files)
│   ├── pvc_persistence_test.go
│   ├── service_volumes_test.go
│   ├── custom_domain_test.go
│   ├── routes_test.go
│   └── helpers.go
├── dogfooding/              ✅ SERVICE SPECS READY (6 YAML files)
│   ├── switchyard-api.yaml
│   ├── switchyard-ui.yaml
│   ├── janua.yaml          ⚠️ References separate repo (not deployed)
│   ├── landing-page.yaml
│   ├── docs-site.yaml
│   └── status-page.yaml
├── docs/                    ✅ COMPREHENSIVE (23 markdown files)
├── examples/                ✅ EXAMPLES (5 YAML files)
└── (Other config files)
```

---

## Switchyard API Endpoints

### Authentication
- `POST /auth/register` - Register user
- `POST /auth/login` - Login, get JWT
- `POST /auth/refresh` - Refresh token
- `POST /auth/logout` - Revoke session
- `POST /auth/jwks` - ❌ **NOT IMPLEMENTED**

### Projects
- `GET /projects` - List projects
- `POST /projects` - Create project
- `GET /projects/:slug` - Get project
- `PUT /projects/:slug` - Update project
- `DELETE /projects/:slug` - Delete project

### Services
- `GET /projects/:slug/services` - List services
- `POST /projects/:slug/services` - Create service
- `GET /projects/:slug/services/:name` - Get service
- `PUT /projects/:slug/services/:name` - Update service
- `DELETE /projects/:slug/services/:name` - Delete service

### Deployments
- `POST /services/:id/deployments` - Deploy
- `GET /services/:id/deployments` - List deployments
- `GET /services/:id/deployments/:id` - Get deployment
- `PUT /services/:id/deployments/:id/rollback` - Rollback

### Builds
- `POST /services/:id/builds` - Trigger build
- `GET /services/:id/builds/:id` - Get build status
- `GET /services/:id/builds/:id/logs` - Build logs

### Domains & Routes
- `POST /services/:id/domains` - Add custom domain
- `GET /services/:id/domains` - List domains
- `DELETE /services/:id/domains/:domain` - Remove domain

### Topology
- `GET /services/:id/topology` - Dependency graph

### Health
- `GET /health` - Service health
- `GET /metrics` - Prometheus metrics

---

## CLI Commands

### Implemented ✅
- `enclii init` - Scaffold new service
- `enclii deploy` - Build & deploy with environment selection
- `enclii logs` - Stream logs (polling, not real-time)
- `enclii ps` - List services with status
- `enclii rollback` - Revert to previous release
- `enclii version` - Show version

### Missing ❌
- `enclii auth login` - OAuth/Janua login
- `enclii secrets set/get` - Secrets management
- `enclii scale` - Autoscaling config
- `enclii routes add/remove` - Route management

---

## Database Schema

**4 Migrations (8 SQL files):**

1. **001_initial_schema** - Core tables
   - users, projects, services, environments
   - releases, deployments, project_access
   - custom_domains, routes

2. **002_compliance_schema** - Audit & compliance
   - audit_logs (immutable)
   - approval_records
   - compliance_events

3. **003_rotation_audit_logs** - Secret rotation history

4. **004_custom_domains_routes** - Extended routing

**10 Repository Types:**
- ProjectRepository
- EnvironmentRepository
- ServiceRepository
- ReleaseRepository
- DeploymentRepository
- UserRepository
- ProjectAccessRepository
- AuditLogRepository
- ApprovalRecordRepository
- RotationAuditLogRepository
- CustomDomainRepository
- RouteRepository

---

## Security Features

✅ RS256 (RSA) JWT authentication
✅ RBAC: admin, developer, viewer roles
✅ Audit logging with immutable trail
✅ Security headers: HSTS, CSP, X-Frame-Options, etc.
✅ CSRF token generation & validation
✅ Vault integration (secrets rotation)
✅ Input validation framework
✅ Session revocation (Redis)
✅ GitHub PR approval checking
✅ Vanta/Drata compliance webhooks

❌ CRITICAL BUG: Middleware validates HMAC instead of RSA
❌ API keys not implemented
❌ Image signing (Cosign) stub only

---

## Testing Summary

**Unit Tests:** 22 Go test files
- auth_test.go (JWT, passwords)
- middleware_test.go (CSRF, security)
- services_test.go (business logic)
- builder_test.go (build system)
- ... and 18 more

**Integration Tests:** 5 Go test files
- PVC persistence tests
- Service volume tests
- Custom domain tests
- Route tests
- Runs on Kind cluster, 45min timeout

**Frontend Tests:** 0 files ❌
- No Jest/Vitest tests
- No component tests
- All UI is mock data

---

## Key Files by Functionality

### Authentication
- `apps/switchyard-api/internal/auth/jwt.go` (472 LOC) - **RS256 correct**
- `apps/switchyard-api/internal/middleware/auth.go` (195 LOC) - **BUG HERE**
- `apps/switchyard-api/internal/services/auth.go` (440 LOC) - Auth service

### Build System
- `apps/switchyard-api/internal/builder/` - Build orchestration
- `.github/workflows/integration-tests.yml` - CI/CD pipeline

### Kubernetes
- `apps/switchyard-api/internal/reconciler/service.go` (784 LOC) - K8s reconciler
- `infra/k8s/base/` - K8s manifests
- `infra/k8s/production/` - Production overlays
- `infra/k8s/staging/` - Staging overlays

### Database
- `apps/switchyard-api/internal/db/migrations/` - SQL migrations
- `apps/switchyard-api/internal/db/repositories.go` (1,160 LOC) - All repos

### CLI
- `packages/cli/cmd/enclii/main.go` - Entry point
- `packages/cli/internal/cmd/deploy.go` - Deploy command
- `packages/cli/internal/spec/parser.go` - YAML parsing

### API
- `apps/switchyard-api/cmd/api/main.go` - Server setup
- `apps/switchyard-api/internal/api/handlers.go` - Handler setup
- `apps/switchyard-api/internal/api/*_handlers.go` - Endpoint handlers

---

## Development Setup

```bash
make bootstrap           # Install deps
make kind-up           # Create local K8s
make infra-dev         # Install ingress, cert-manager
make run-switchyard    # Start API :8080
make run-ui            # Start UI :3000
make test              # Run all tests
make lint              # Lint code
```

---

## Production Timeline

**Week 1-2:** Fix critical bugs, infrastructure setup  
**Week 3-4:** Janua integration  
**Week 5-6:** Dogfooding setup  
**Week 7-8:** Load testing, security audit, launch  

---

## Estimated Work Remaining

| Task | Effort | Priority |
|------|--------|----------|
| Fix auth middleware bug | 1 day | CRITICAL |
| Implement missing CLI commands | 2-3 days | HIGH |
| Complete UI implementation | 3-4 weeks | HIGH |
| Janua integration (OAuth) | 2-3 weeks | HIGH |
| Roundhouse (build workers) | 1-2 weeks | MEDIUM |
| Timetable (cron jobs) | 2 weeks | MEDIUM |
| Waybill (cost tracking) | 2-3 weeks | MEDIUM |
| Signal (observability) | 1-2 weeks | MEDIUM |
| Infrastructure/Terraform | 2-3 weeks | MEDIUM |
| **TOTAL** | **6-8 weeks** | - |

---

## Quick Troubleshooting

**API won't authenticate?**
→ Check `/home/user/enclii/apps/switchyard-api/internal/middleware/auth.go` line 92 (HMAC bug)

**Can't find component X?**
→ See "Component Status" table above (Roundhouse, Timetable, Waybill are missing)

**UI showing mock data?**
→ UI is 80% stub - no real API integration yet

**Tests failing?**
→ Check Kind cluster is running: `kind get clusters`

---

**Report Generated:** November 20, 2025  
**Full Audit:** See `CODEBASE_AUDIT_COMPREHENSIVE_2025.md`
