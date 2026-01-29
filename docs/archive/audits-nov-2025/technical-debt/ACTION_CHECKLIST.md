# TECHNICAL DEBT - PRIORITIZED ACTION CHECKLIST
## Phase-by-Phase Implementation Guide

**Last Updated:** November 20, 2025  
**Status:** Ready for implementation  
**Tracking:** Use this as your master checklist for Phase 1-4 completion

---

## PHASE 1: CRITICAL BLOCKERS (Weeks 1-2)

### Week 1: Security & Infrastructure (40 hours)

#### SECURITY FIXES (20 hours)

- [ ] **SEC-001: Fix Hardcoded Database Credentials** (2h)
  - Location: `/apps/switchyard-api/internal/backup/postgres.go:381-396`
  - Action: Move to environment variables
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **SEC-002: Enable Database SSL/TLS** (1h)
  - Location: `/apps/switchyard-api/internal/config/config.go:59`
  - Action: Change `sslmode=disable` to `sslmode=require`
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **SEC-003: Fix CORS Configuration** (1h)
  - Location: `/apps/switchyard-api/internal/middleware/security.go:428`
  - Action: Restrict to specific origins (use function-based config)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **SEC-004: Implement External Secret Management** (8h)
  - Action: Setup Sealed Secrets or Vault integration
  - Sub-tasks:
    - [ ] Choose secret management tool (1h)
    - [ ] Setup integration (3h)
    - [ ] Migrate existing secrets (2h)
    - [ ] Document for team (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **SEC-005: Remove Secrets from Git History** (2h)
  - Action: Purge from Git history, rotate all exposed credentials
  - Sub-tasks:
    - [ ] Identify all secrets in history (30min)
    - [ ] Use BFG Repo-Cleaner to remove (1h)
    - [ ] Rotate all credentials (30min)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **SEC-006: Fix Rate Limiting Issues** (6h)
  - Location: `/apps/switchyard-api/internal/middleware/security.go:68-104`
  - Sub-tasks:
    - [ ] Bound unbounded rate limiter map (2h)
    - [ ] Add LRU eviction (2h)
    - [ ] Fix goroutine cleanup (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

#### INFRASTRUCTURE FIXES (20 hours)

- [ ] **INFRA-001: Add Security Context to PostgreSQL** (3h)
  - Location: `/infra/k8s/base/postgres.yaml`
  - Action: Add runAsNonRoot, readOnlyRootFilesystem, add resource limits
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **INFRA-002: Implement Persistent Storage for PostgreSQL** (4h)
  - Action: Replace emptyDir with PersistentVolumeClaim
  - Sub-tasks:
    - [ ] Create PVC manifest (1h)
    - [ ] Update PostgreSQL deployment (1h)
    - [ ] Test persistence (1h)
    - [ ] Document procedure (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **INFRA-003: Add Default-Deny NetworkPolicies** (6h)
  - Action: Implement Kubernetes NetworkPolicies
  - Sub-tasks:
    - [ ] Create default-deny policy (1h)
    - [ ] Create ingress policies (2h)
    - [ ] Create egress policies (2h)
    - [ ] Test and validate (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **INFRA-004: Fix RBAC (Replace ClusterRole with Role)** (4h)
  - Location: `/infra/k8s/base/rbac.yaml`
  - Action: Convert ClusterRole to Role, add RoleBinding
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **INFRA-005: Add TLS to Ingress with Cert-Manager** (3h)
  - Action: Configure cert-manager, update Ingress with TLS
  - Sub-tasks:
    - [ ] Install cert-manager (1h)
    - [ ] Create ClusterIssuer (1h)
    - [ ] Update Ingress (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

---

### Week 2: Testing Foundation (40 hours)

#### BUILD & COMPILATION FIXES (6 hours)

- [ ] **TEST-001: Fix DB Package Compilation Error** (1h)
  - Location: `/apps/switchyard-api/internal/db/connection.go:98-106`
  - Issue: Format string errors (%.0f → %d)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-002: Fix K8s Package Compilation Error** (1h)
  - Location: `/apps/switchyard-api/internal/k8s/client.go`
  - Issue: Undefined types, missing imports
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-003: Fix Backup Package Compilation Error** (1h)
  - Location: `/apps/switchyard-api/internal/backup/postgres.go`
  - Issue: Wrong API usage (WithContext)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-004: Fix Builder Package Compilation Error** (1h)
  - Location: `/apps/switchyard-api/internal/builder/git.go`
  - Issue: go-git type mismatches
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-005: Fix Cache Package Compilation Error** (0.5h)
  - Location: `/apps/switchyard-api/internal/cache/redis.go`
  - Issue: redis.Options field changes
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-006: Fix Provenance Package Compilation Error** (0.5h)
  - Location: `/apps/switchyard-api/internal/provenance/checker.go`
  - Issue: UUID type conversion
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

#### TEST INFRASTRUCTURE (34 hours)

- [ ] **TEST-007: Setup Integration Test Infrastructure** (8h)
  - Sub-tasks:
    - [ ] Add testcontainers setup (3h)
    - [ ] Create Postgres test container (2h)
    - [ ] Create Redis test container (2h)
    - [ ] Document setup (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-008: Create Test Fixtures & Data Builders** (8h)
  - Sub-tasks:
    - [ ] Create project fixtures (2h)
    - [ ] Create service fixtures (2h)
    - [ ] Create deployment fixtures (2h)
    - [ ] Create secret fixtures (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-009: Setup Mock Objects** (8h)
  - Sub-tasks:
    - [ ] Fix handler mocks (2h)
    - [ ] Create service mocks (2h)
    - [ ] Create database mocks (2h)
    - [ ] Create Kubernetes client mocks (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-010: Setup CI/CD Pipeline with Coverage Tracking** (6h)
  - Sub-tasks:
    - [ ] Create GitHub Actions workflow (2h)
    - [ ] Add coverage reporting (2h)
    - [ ] Setup coverage badges (1h)
    - [ ] Document process (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **TEST-011: Team Training on Testing Patterns** (4h)
  - Sub-tasks:
    - [ ] Document testing style guide (1h)
    - [ ] Create example tests (1h)
    - [ ] Conduct team training (1.5h)
    - [ ] Create FAQ (0.5h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

---

## PHASE 1 COMPLETION CHECKLIST

- [ ] All security fixes complete (20h)
- [ ] All infrastructure fixes complete (20h)
- [ ] All compilation errors fixed (6h)
- [ ] Test infrastructure operational (34h)
- [ ] `make test` passes with 0 errors
- [ ] CI/CD pipeline green
- [ ] No secrets in Git (verify with: `git log -S 'password' --all`)
- [ ] Database SSL enabled and verified
- [ ] Network policies deployed and tested
- [ ] PostgreSQL persistent storage working
- [ ] Team trained on testing patterns

**Phase 1 Status:** [ ] Not Started [ ] 50% Complete [ ] 90% Complete [ ] COMPLETE ✅

**Actual Hours Used:** _____ / 80 budgeted

---

## PHASE 2: HIGH PRIORITY (Weeks 3-5)

### Week 3: Critical Path Tests (45 hours)

- [ ] **AUTH-001: Authentication & Authorization Tests** (12h)
  - Tests needed:
    - [ ] JWT generation and validation (3h)
    - [ ] Password hashing and verification (2h)
    - [ ] Session management and revocation (3h)
    - [ ] Token refresh flow (2h)
    - [ ] RBAC enforcement (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **VAL-001: Input Validation Tests** (10h)
  - Tests needed:
    - [ ] Project name validation (2h)
    - [ ] Service configuration validation (2h)
    - [ ] Environment variable validation (2h)
    - [ ] Git repository URL validation (2h)
    - [ ] DNS name validation (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **DB-001: Database Integration Tests** (12h)
  - Tests needed:
    - [ ] CRUD operations for all entities (4h)
    - [ ] Transaction handling (3h)
    - [ ] Foreign key constraints (2h)
    - [ ] Concurrent access patterns (2h)
    - [ ] Error scenarios (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **K8S-001: Kubernetes Client Integration Tests** (8h)
  - Tests needed:
    - [ ] Deployment creation and updates (2h)
    - [ ] Service creation and discovery (2h)
    - [ ] ConfigMap and Secret handling (2h)
    - [ ] Ingress configuration (1h)
    - [ ] Error handling (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **Code Review & Fixes** (3h)
  - [ ] Review tests with security engineer
  - [ ] Fix any issues found
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

### Week 4: Core Services Tests (40 hours)

- [ ] **API-001: API Handler Tests Expansion** (12h)
  - Tests needed:
    - [ ] Project creation and management (3h)
    - [ ] Service lifecycle (3h)
    - [ ] Deployment endpoints (3h)
    - [ ] Build endpoints (3h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **BUILDER-001: Builder Service Tests** (10h)
  - Tests needed:
    - [ ] Buildpack detection (2h)
    - [ ] Build execution (3h)
    - [ ] SBOM generation (2h)
    - [ ] Image signing (2h)
    - [ ] Error handling (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **CACHE-001: Cache Service Tests** (8h)
  - Tests needed:
    - [ ] Cache hit/miss scenarios (2h)
    - [ ] TTL expiration (2h)
    - [ ] Cache invalidation (2h)
    - [ ] Error scenarios (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **CLI-001: CLI Command Integration Tests** (8h)
  - Tests needed:
    - [ ] Init command (2h)
    - [ ] Deploy command (2h)
    - [ ] Logs command (2h)
    - [ ] Rollback command (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **Code Review & Fixes** (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

### Week 5: Infrastructure & UI (40 hours)

- [ ] **UI-001: Fix Hardcoded Auth Tokens** (8h)
  - Location: 8 locations in `/apps/switchyard-ui/`
  - Action: Remove hardcoded tokens, implement token injection
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **UI-002: Implement UI Authentication Flow** (10h)
  - Sub-tasks:
    - [ ] Create login page (3h)
    - [ ] Implement OAuth/OIDC flow (4h)
    - [ ] Add session management (2h)
    - [ ] Add logout functionality (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **UI-003: Add CSRF Protection** (4h)
  - Action: Implement CSRF tokens on all forms
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **INFRA-006: Implement PostgreSQL HA** (8h)
  - Sub-tasks:
    - [ ] Convert to StatefulSet (3h)
    - [ ] Setup replication (3h)
    - [ ] Add Pod Disruption Budget (1h)
    - [ ] Test failover (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **INFRA-007: Implement Backup/Restore Procedures** (8h)
  - Sub-tasks:
    - [ ] Create backup scripts (3h)
    - [ ] Configure automated backups (2h)
    - [ ] Test restore procedures (2h)
    - [ ] Document procedures (1h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

- [ ] **Code Review & Testing** (2h)
  - Owner: [TBD]
  - Status: [ ] Not Started [ ] In Progress [ ] Complete

---

## PHASE 2 COMPLETION CHECKLIST

- [ ] Authentication & authorization tests passing (12h tests + infrastructure)
- [ ] Input validation tests passing (10h tests)
- [ ] Database integration tests passing (12h tests)
- [ ] Kubernetes client tests passing (8h tests)
- [ ] API handler tests expanded (12h tests)
- [ ] Builder service tests complete (10h tests)
- [ ] Cache service tests complete (8h tests)
- [ ] CLI command tests complete (8h tests)
- [ ] UI authentication flow working
- [ ] UI hardcoded tokens removed
- [ ] PostgreSQL HA operational (3+ replicas)
- [ ] Backup/restore procedures tested
- [ ] Test coverage >= 50% overall
- [ ] Critical packages >= 95% coverage
- [ ] 0 CRITICAL severity issues remaining

**Phase 2 Status:** [ ] Not Started [ ] 50% Complete [ ] 90% Complete [ ] COMPLETE ✅

**Actual Hours Used:** _____ / 125 budgeted

---

## PHASE 3: PRODUCTION READY (Weeks 6-8)

### Week 6: Integration & Security (40 hours)

- [ ] **E2E-001: Deployment Pipeline E2E Tests** (15h)
- [ ] **SEC-007: Compliance & Security Tests** (12h)
- [ ] **AUDIT-001: Audit Logging Tests** (8h)
- [ ] **VAULT-001: Vault Integration Tests** (5h)

**Status:** [ ] Not Started [ ] In Progress [ ] Complete

### Week 7: Code Quality & Refactoring (40 hours)

- [ ] **REVOKE-001: Implement Token Revocation** (12h)
- [ ] **REFACTOR-001: Split Monolithic handlers.go** (10h)
- [ ] **RBAC-001: Complete RBAC Enforcement** (8h)
- [ ] **CLEANUP-001: Fix Resource Leaks & Goroutine Issues** (4h)
- [ ] **CONFIG-001: Add Configuration Validation** (4h)
- [ ] **CONST-001: Extract Magic Numbers to Constants** (2h)

**Status:** [ ] Not Started [ ] In Progress [ ] Complete

### Week 8: Frontend & Polish (35 hours)

- [ ] **JEST-001: Jest Configuration** (3h)
- [ ] **COMPONENTS-001: React Component Tests** (15h)
- [ ] **A11Y-001: Accessibility Testing** (8h)
- [ ] **DOCS-001: Documentation Updates** (9h)

**Status:** [ ] Not Started [ ] In Progress [ ] Complete

---

## PHASE 4: GA READY (Weeks 9-10)

### Week 9: Governance & Monitoring (25 hours)

- [ ] **ADMISSION-001: Implement Kyverno Admission Control** (12h)
- [ ] **MONITORING-001: Setup Prometheus/Grafana Monitoring** (10h)
- [ ] **SCALING-001: Configure HPA/VPA** (3h)

**Status:** [ ] Not Started [ ] In Progress [ ] Complete

### Week 10: Testing & Documentation (25 hours)

- [ ] **SECURITY-SCAN-001: Security Scanning in CI/CD** (6h)
- [ ] **LOAD-001: Load Testing** (8h)
- [ ] **PERF-001: Performance Tuning** (6h)
- [ ] **DOCS-FINAL-001: Final Documentation Polish** (5h)

**Status:** [ ] Not Started [ ] In Progress [ ] Complete

---

## QUICK WINS (Do This Week - 10 hours)

- [ ] **QW-001: Fix Compilation Errors** (4h)
  - Owner: [TBD]
  - Status: [ ] Complete

- [ ] **QW-002: Extract Magic Numbers to Constants** (2h)
  - Owner: [TBD]
  - Status: [ ] Complete

- [ ] **QW-003: Remove localhost Defaults** (1h)
  - Owner: [TBD]
  - Status: [ ] Complete

- [ ] **QW-004: Add Configuration Validation** (3h)
  - Owner: [TBD]
  - Status: [ ] Complete

---

## IMPLEMENTATION TRACKING TEMPLATE

### Weekly Status Report

**Week:** _____  
**Phase:** _____

#### Completed Tasks (This Week)
- [ ] Task 1: [Hours used]
- [ ] Task 2: [Hours used]

**Total Hours Used:** _____ / budgeted  
**Velocity:** _____ hours/week

#### Completed Deliverables
- ✅ 
- ✅ 

#### Issues & Blockers
- Issue 1: Impact: _____ Timeline: _____
- Issue 2: Impact: _____ Timeline: _____

#### Next Week Priorities
1. [Task]
2. [Task]
3. [Task]

#### Test Coverage Progress
- Previous: _%
- Current: _%
- Target: _%

#### Issues Fixed This Week
- CRITICAL: [count] (was: ___, now: ___)
- HIGH: [count] (was: ___, now: ___)
- MEDIUM: [count] (was: ___, now: ___)

---

## DEPENDENCIES & BLOCKERS

### Current Blockers

| Issue | Severity | Owner | Resolution |
|-------|----------|-------|-----------|
| [Name] | CRITICAL | [TBD] | [Plan] |

### Critical Path Dependencies

1. **Compilation Errors → Test Infrastructure** (blocking)
   - Must complete TEST-001 through TEST-006 before other tests
   
2. **Security Fixes → Infrastructure Fixes** (parallel)
   - Can run in parallel but security should finish first

3. **Phase 1 → Phase 2** (sequential)
   - Phase 2 blocked until Phase 1 complete

---

## SIGN-OFF CHECKLIST

### Phase 1 Sign-Off

- [ ] All CRITICAL security issues fixed
- [ ] All CRITICAL infrastructure issues fixed
- [ ] Test suite executable (0 compilation errors)
- [ ] CI/CD pipeline operational
- [ ] Team confirms Phase 1 complete
- [ ] **Signed Off By:** ________________ **Date:** _________

### Phase 2 Sign-Off

- [ ] 50%+ test coverage achieved
- [ ] No CRITICAL severity issues remaining
- [ ] PostgreSQL HA working
- [ ] UI authentication functional
- [ ] **Signed Off By:** ________________ **Date:** _________

### Phase 3 Sign-Off

- [ ] 80%+ test coverage achieved
- [ ] No HIGH severity issues remaining
- [ ] E2E tests passing
- [ ] **Signed Off By:** ________________ **Date:** _________

### Phase 4 Sign-Off

- [ ] SOC 2 audit passed
- [ ] Load tested (500 req/s)
- [ ] Production deployment approved
- [ ] **Signed Off By:** ________________ **Date:** _________

---

**Checklist Created:** November 20, 2025  
**Next Review:** Weekly  
**Last Updated:** _____________________

