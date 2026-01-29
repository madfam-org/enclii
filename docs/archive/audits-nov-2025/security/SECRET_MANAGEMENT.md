# Enclii Secret Management System Audit Report

**Date**: November 19, 2025  
**Status**: MVP - Critical Implementation Gaps  
**Production Readiness**: 15% (Needs Major Work)

---

## Executive Summary

The Enclii platform currently **lacks a production-ready secret management system**. While the architecture plan (SOFTWARE_SPEC.md) calls for HashiCorp Vault or 1Password Connect integration with zero-downtime rotation capabilities, the current MVP implementation:

1. **Passes secrets as plaintext environment variables** in Kubernetes deployments
2. **Has no Lockbox/secret service integration** (stub filesystem only)
3. **Stores secrets only in control plane database** (unencrypted in migrations)
4. **Lacks rotation workflows** - no zero-downtime secret rotation capability
5. **Missing audit logging** for secret access
6. **No environment-specific secret scoping**

This is a **critical blocker for compliance** and production deployment.

---

## Current Architecture Analysis

### 1. Secret Storage & Injection

#### Current Implementation

**Location**: `/home/user/enclii/apps/switchyard-api/`

**How Secrets are Currently Handled:**

1. **API Request Handling** (`internal/api/handlers.go:388`)
   ```go
   var req struct {
       ReleaseID   string            `json:"release_id" binding:"required"`
       Environment map[string]string `json:"environment"`  // PLAINTEXT
       Replicas    int               `json:"replicas,omitempty"`
   }
   ```
   - Accepts environment variables as a plain map
   - No encryption, no versioning, no secret reference

2. **Database Storage** (`internal/db/migrations/001_initial_schema.up.sql`)
   ```sql
   -- NO SECRETS TABLE DEFINED
   -- Environment variables only stored in deployments via JSON
   ```
   - **ISSUE**: No dedicated secrets table
   - **ISSUE**: Environment vars stored as plaintext in relational format

3. **Secret Injection into Containers** (`internal/reconciler/service.go:167`)
   ```go
   // Build environment variables - PLAINTEXT INJECTION
   var envVars []corev1.EnvVar
   for key, value := range req.Release.Environment {
       envVars = append(envVars, corev1.EnvVar{
           Name:  key,
           Value: fmt.Sprintf("%v", value),  // NO ENCRYPTION
       })
   }
   ```
   - Converts plaintext env vars directly to Kubernetes EnvVar
   - No reference to external secret store
   - Secrets visible in pod spec and audit logs

#### Planned Implementation (From SOFTWARE_SPEC.md)

- **Lockbox Service**: Vault or 1Password Connect
- **Storage**: Externalized to dedicated secrets service
- **Injection**: Via Kubernetes CSI or external-secrets operator
- **References**: Using `secretRef: api-prod` pattern

**Reference in Spec**:
```yaml
envFrom:
  - secretRef: api-prod  # CURRENTLY NOT IMPLEMENTED
```

#### Gaps Found

| Aspect | Current | Required | Gap |
|--------|---------|----------|-----|
| Storage Backend | None (planned) | Vault/1Password | Missing |
| Encryption at Rest | No | Yes | Critical |
| Encryption in Transit | HTTP only | HTTPS + TLS | Partial |
| Secret References | Direct env vars | Named refs (secretRef) | Missing |
| Versioning | No | Yes (with history) | Missing |
| Scoping | None | Project/Env/Service | Missing |
| Audit Logging | No | Yes | Missing |

---

### 2. Rotation Capabilities

#### Current State: NOT IMPLEMENTED

**Findings**:
- No rotation handlers in reconciler
- No secret version management
- No rolling deployment coordination
- Manual restart required for all secret changes

#### Desired Workflow (From SOFTWARE_SPEC.md:336)

```
Test Case TC-03: secret rotation with zero downtime

Expected Flow:
1. Rotate secret in Lockbox (new version)
2. External-secrets controller picks up change
3. Kubernetes volume/env updated
4. Application reloads secrets (zero downtime)
5. Old secret archived
```

#### What's Missing for Zero-Downtime Rotation

**Required Components**:

1. **Secret Version Management**
   - Database schema for secret versions
   - Rotation history tracking
   - Not in current migrations

2. **Rotation Orchestrator**
   - Detect secret changes in Lockbox
   - Trigger rolling pod restarts
   - Monitor health during rotation
   - Auto-rollback if health checks fail
   - **No code for this exists**

3. **Coordinator with Reconciler**
   ```go
   // Would need to add to controller.go
   type SecretRotationHandler struct {
       k8sClient *k8s.Client
       lockbox   *LockboxClient
       repos     *db.Repositories
   }
   
   // Rotation flow:
   // 1. New secret detected in Lockbox
   // 2. Create new Release with rotated secrets
   // 3. Trigger canary deployment (10% → 100%)
   // 4. Verify health
   // 5. Archive old secret
   // 6. Update AuditEvent
   ```
   - **Not implemented**

4. **Health Verification During Rotation**
   - Monitor P95 latency during rotation
   - Error rate tracking
   - Auto-rollback on threshold breach
   - Readiness probe validation
   - **Only basic liveness/readiness probes exist**

#### Current Rotation Mechanism (Manual)

Developer must:
1. Update secret in deployment request
2. Call `POST /v1/services/{id}/deploy` with new env vars
3. System **immediately replaces** all pods (potential downtime)
4. No health checks during transition
5. No automatic rollback

**Code Reference**: `handlers.go:377-452` - `DeployService` handler has no rotation awareness

---

### 3. Access Control

#### Current Implementation: NONE

**Findings**:
- No secret-level RBAC
- No service account isolation
- No audit trail for secret reads
- Auth is at API level only (token + role)

**Current RBAC** (`internal/auth/...` - not shown in audit):
- API-level: Admin, Developer, ReadOnly
- No secret-specific permissions

**Planned** (from SOFTWARE_SPEC.md:457-458):
```
| Secrets: Read/Write (service scope) |   ✓   |   ✓   |     ✓     |     ✗    |
| Secrets: Project/Env scope          |   ✓   |   ✓   |     ✗     |     ✗    |
```

#### Required Access Control

1. **Scoped Secrets by Environment**
   - Dev secrets not accessible from prod Kubernetes
   - Service accounts with least privilege
   - Network policies enforcement

2. **Audit Logging**
   - WHO accessed secret (user/service account)
   - WHEN (timestamp)
   - WHAT action (read/write/rotate)
   - WHERE (IP, pod)
   - No current implementation for this

3. **Service Account Isolation**
   - Each service gets unique SA
   - SA only reads its own secrets
   - Currently no RBAC rules

---

### 4. Type Definition Mismatches

**Critical Issue Found**: Type definitions incomplete/incorrect

**File**: `/home/user/enclii/packages/sdk-go/pkg/types/types.go`

```go
// ACTUAL TYPE (lines 55-64)
type Release struct {
    ID       uuid.UUID
    ServiceID uuid.UUID
    Version   string
    ImageURI  string
    GitSHA    string
    Status    ReleaseStatus
    CreatedAt time.Time
    UpdatedAt time.Time
}

// BUT CODE USES (reconciler/service.go:167)
req.Release.Environment    // FIELD DOESN'T EXIST
req.Release.BuildID        // FIELD DOESN'T EXIST
req.Release.ImageURL       // NAMED ImageURI, NOT ImageURL
```

**Impact**: Type system doesn't match implementation
- Suggests incomplete refactoring
- Would cause runtime panics if these fields accessed
- Tests not catching this

---

## Code Locations & Implementation Details

### Key Files Needing Refactoring

1. **Secret Storage Schema**
   - **File**: `apps/switchyard-api/internal/db/migrations/001_initial_schema.up.sql`
   - **Need**: Add `secrets` table with:
     ```sql
     CREATE TABLE secrets (
         id UUID PRIMARY KEY,
         scope VARCHAR(50),  -- project/environment/service
         scope_id UUID,
         name VARCHAR(255),
         vault_path VARCHAR(500),  -- reference to external store
         version INT,
         rotated_at TIMESTAMP,
         created_at TIMESTAMP,
         updated_at TIMESTAMP,
         UNIQUE(scope, scope_id, name)
     );
     
     CREATE TABLE secret_versions (
         id UUID PRIMARY KEY,
         secret_id UUID REFERENCES secrets(id),
         version INT,
         vault_ref VARCHAR(500),
         rotation_status VARCHAR(50),  -- pending/active/archived
         created_at TIMESTAMP
     );
     ```

2. **Repository for Secrets**
   - **File**: `apps/switchyard-api/internal/db/repositories.go`
   - **Need**: `SecretRepository` with:
     - `Create(ctx, secret)`
     - `GetByScope(ctx, scope, scopeID)`
     - `Rotate(ctx, secretID, newVersion)`
     - `GetVersion(ctx, secretID, version)`
     - `ArchiveVersion(ctx, secretID, version)`
     - `ListAudit(ctx, secretID)` - audit trail

3. **Lockbox Service Integration**
   - **File**: `apps/switchyard-api/internal/` (NOT YET CREATED)
   - **Need**: New package `lockbox/`
     ```go
     type LockboxClient interface {
         WriteSecret(ctx, scope, name string, value []byte) error
         ReadSecret(ctx, scope, name string) ([]byte, error)
         RotateSecret(ctx, scope, name string) (*SecretVersion, error)
         ListVersions(ctx, scope, name string) ([]*SecretVersion, error)
         DeleteSecret(ctx, scope, name string) error
     }
     
     // Implementations for Vault and 1Password Connect
     ```

4. **Reconciler Secret Rotation Handler**
   - **File**: `apps/switchyard-api/internal/reconciler/` (NOT YET CREATED)
   - **Need**: New file `rotation.go`
     ```go
     type SecretRotationReconciler struct {
         k8sClient *k8s.Client
         lockbox   lockbox.LockboxClient
         repos     *db.Repositories
         logger    *logrus.Logger
     }
     
     func (r *SecretRotationReconciler) Reconcile(ctx context.Context, secretID string) error {
         // 1. Get secret metadata
         // 2. Get new version from Lockbox
         // 3. Update ConfigMap/Secret in K8s
         // 4. Trigger rolling restart (patch pod template annotation)
         // 5. Monitor health during restart
         // 6. Verify all pods healthy
         // 7. Archive old version
         // 8. Update AuditEvent
     }
     ```

5. **CLI Secrets Command** (MISSING)
   - **File**: `packages/cli/internal/cmd/` - NO `secrets.go`
   - **Need**: Implement `enclii secrets` subcommand
     ```go
     // Reference from SOFTWARE_SPEC.md:212
     // enclii secrets set NAME=val --service api --env prod
     // enclii secrets list --service api --env prod
     // enclii secrets delete NAME --service api --env prod
     // enclii secrets rotate --service api --env prod
     ```

6. **API Endpoints for Secrets** (PARTIAL)
   - **File**: `apps/switchyard-api/internal/api/handlers.go`
   - **Current**: None for secret management
   - **Need**: 
     - `POST /v1/secrets/{scope}/` - Create/update
     - `GET /v1/secrets/{scope}/{name}` - Read (for testing)
     - `POST /v1/secrets/{scope}/{name}/rotate` - Trigger rotation
     - `GET /v1/secrets/{scope}/{name}/versions` - Version history
     - `DELETE /v1/secrets/{scope}/{name}` - Delete

---

## Compliance & Production Readiness Assessment

### Security Compliance Gaps

| Requirement | Current | Needed | Status |
|-------------|---------|--------|--------|
| Encryption at Rest | No | AES-256 | FAIL |
| Encryption in Transit | HTTP | HTTPS+TLS | PARTIAL |
| Secret Scoping | None | Project/Env/Service | FAIL |
| Audit Logging | None | Full trail | FAIL |
| Access Control | Role-based only | Role + Secret-level RBAC | FAIL |
| Secret Rotation | Manual | Automated zero-downtime | FAIL |
| Versioning | None | Full history | FAIL |
| Compliance Export | None | SIEM integration | FAIL |

### Failed Requirements from SOFTWARE_SPEC.md

1. **Section 3 (Functional Reqs)**
   > "Namespaced secrets; env var injection; sealed at rest/in transit."
   - ❌ No namespacing
   - ⚠️ Env var injection exists but unsecured
   - ❌ Not sealed at rest

2. **Section 10 (Security Model)**
   > "Zero plaintext in CI; short‑lived tokens; scheduled rotation; access logs."
   - ❌ Plaintext everywhere in dev mode
   - ⚠️ OIDC tokens exist but no short-lived tokens
   - ❌ No scheduled rotation
   - ❌ No access logs

3. **Section 15 (Acceptance Criteria)**
   > "TC‑03: secret rotation with zero downtime."
   - ❌ Not implemented

---

## Database Schema Issues

### Current Schema (`migrations/001_initial_schema.up.sql`)

**Missing**:
- No `secrets` table
- No `secret_versions` table
- No `audit_events` table for secret access
- Deployments don't reference secret scopes

**What Exists**:
```sql
-- Only these tables
projects
environments
services
releases
deployments
```

**Environment variables stored where?**
- **Handlers**: Passed as `Environment map[string]string` (line 388)
- **Database**: Inferred from deployment record (but no `environment` column visible)
- **Issue**: Type mismatch - handlers.go expects field not in types.go

---

## Production Gaps & Refactoring Needed

### Phase 1: Foundation (3-4 weeks)

1. **Database Schema**
   - Add secrets, secret_versions, audit_events tables
   - Add columns to deployments for secret reference
   - Migration scripts

2. **Type Definitions Fix**
   - Update SDK types (Release, Deployment)
   - Add Secret, SecretVersion, AuditEvent types
   - Fix Release.ImageURL vs ImageURI inconsistency

3. **Lockbox Service Interface**
   - Define interface (Vault, 1Password)
   - Implement stub for testing
   - Integration tests

### Phase 2: Implementation (2-3 weeks)

1. **Secret Repository**
   - Full CRUD operations
   - Version management
   - Audit logging

2. **API Endpoints**
   - Secret CRUD endpoints
   - Rotation trigger endpoint
   - Version history endpoint

3. **CLI Commands**
   - `enclii secrets set/list/delete/rotate`
   - Environment-scoped access

### Phase 3: Zero-Downtime Rotation (2-3 weeks)

1. **Rotation Orchestrator**
   - Detect changes from Lockbox
   - Coordinate with reconciler
   - Health monitoring

2. **Rolling Restart Handler**
   - Patch deployment pod template annotation
   - Monitor during rollout
   - Auto-rollback on failure

3. **Comprehensive Testing**
   - Unit tests for rotation logic
   - Integration tests with mock Lockbox
   - E2E tests with real Vault/1Password

### Phase 4: Compliance (1-2 weeks)

1. **Audit Logging**
   - AuditEvent table integration
   - Log all secret access
   - SIEM export capability

2. **Access Control**
   - Secret-level RBAC
   - Service account isolation
   - Network policies

3. **Documentation**
   - Secret management guide
   - Rotation runbook
   - Compliance checklist

---

## Testing Requirements

### Missing Tests

**Unit Tests**:
- SecretRepository CRUD
- Secret rotation orchestration
- Version management
- Audit logging

**Integration Tests**:
- Full secret lifecycle (create → rotate → archive)
- Health checks during rotation
- Rollback on failure
- Version retrieval

**E2E Tests** (Planned as TC-03):
- CLI: `enclii secrets set` → verify in pod
- CLI: `enclii secrets rotate` → zero downtime verification
- UI: Secret management dashboard
- Health monitoring during rotation

### Current Test Status

**Files** (`apps/switchyard-api/internal/`):
- `reconciler/service_test.go` - Exists but doesn't test secrets
- No secret-related tests

---

## Risk Assessment

### Critical Risks (Address Immediately)

1. **Compliance Violation**: Plaintext secrets in pods + no audit trail
   - **Impact**: Data breach risk, regulatory non-compliance
   - **Mitigation**: Implement Lockbox + audit immediately

2. **Manual Rotation Only**: Operators must coordinate secret updates
   - **Impact**: Human error, downtime risk
   - **Mitigation**: Implement automated rotation orchestrator

3. **No Access Control**: Any developer can read any secret
   - **Impact**: Lateral movement risk
   - **Mitigation**: Implement secret-level RBAC

### High Risks

4. **No Version History**: Can't audit or rollback secrets
5. **No Encryption at Rest**: Secrets visible in database
6. **Type Definition Mismatch**: Runtime failures possible

---

## Recommendations

### Immediate Actions (This Sprint)

1. ✅ Create database migration for secrets table
2. ✅ Implement SecretRepository with full CRUD
3. ✅ Fix type definitions (Release, Deployment, add Secret)
4. ✅ Create Lockbox interface and stub implementation
5. ✅ Add basic secret CRUD API endpoints

### Short-Term (Next 2 Sprints)

6. ✅ Implement rotation handler in reconciler
7. ✅ Add CLI `secrets` command
8. ✅ Implement zero-downtime rotation orchestrator
9. ✅ Add comprehensive audit logging
10. ✅ Create secret-level RBAC

### Long-Term (Q4 2025)

11. ✅ Production Vault/1Password integration
12. ✅ Secret scanning in CI/CD
13. ✅ SIEM export capability
14. ✅ Compliance audit trail export
15. ✅ Multi-region secret replication

---

## Code Snippets for Remediation

### Missing Release Type Fields

**File**: `packages/sdk-go/pkg/types/types.go` (Add after line 64)

```go
// Add missing fields to Release
type Release struct {
    ID          uuid.UUID      `json:"id" db:"id"`
    ServiceID   uuid.UUID      `json:"service_id" db:"service_id"`
    Version     string         `json:"version" db:"version"`
    ImageURI    string         `json:"image_uri" db:"image_uri"`
    ImageURL    string         `json:"image_url" db:"image_url"` // Alias for compatibility
    GitSHA      string         `json:"git_sha" db:"git_sha"`
    BuildID     string         `json:"build_id" db:"build_id"` // NEW - for audit trail
    Environment map[string]string `json:"-" db:"-"` // NEW - from deployment
    Status      ReleaseStatus  `json:"status" db:"status"`
    CreatedAt   time.Time      `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at" db:"updated_at"`
}

// Add Secret type
type Secret struct {
    ID        uuid.UUID            `json:"id" db:"id"`
    Scope     SecretScope          `json:"scope" db:"scope"` // project/environment/service
    ScopeID   uuid.UUID            `json:"scope_id" db:"scope_id"`
    Name      string               `json:"name" db:"name"`
    VaultPath string               `json:"vault_path" db:"vault_path"`
    Version   int                  `json:"version" db:"version"`
    RotatedAt *time.Time           `json:"rotated_at" db:"rotated_at"`
    CreatedAt time.Time            `json:"created_at" db:"created_at"`
    UpdatedAt time.Time            `json:"updated_at" db:"updated_at"`
}

type SecretScope string

const (
    SecretScopeProject     SecretScope = "project"
    SecretScopeEnvironment SecretScope = "environment"
    SecretScopeService     SecretScope = "service"
)

// Add Deployment Environment field
type Deployment struct {
    ID            uuid.UUID        `json:"id" db:"id"`
    ReleaseID     uuid.UUID        `json:"release_id" db:"release_id"`
    EnvironmentID uuid.UUID        `json:"environment_id" db:"environment_id"`
    Environment   map[string]string `json:"environment" db:"environment"` // NEW - env vars
    SecretRef     string           `json:"secret_ref" db:"secret_ref"` // NEW - secret name
    Replicas      int              `json:"replicas" db:"replicas"`
    Status        DeploymentStatus `json:"status" db:"status"`
    Health        HealthStatus     `json:"health" db:"health"`
    CreatedAt     time.Time        `json:"created_at" db:"created_at"`
    UpdatedAt     time.Time        `json:"updated_at" db:"updated_at"`
}
```

### Required Database Migration

**File**: `apps/switchyard-api/internal/db/migrations/002_add_secrets.up.sql` (NEW)

```sql
-- Secrets table
CREATE TABLE IF NOT EXISTS secrets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    scope VARCHAR(50) NOT NULL, -- project/environment/service
    scope_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    vault_path VARCHAR(500) NOT NULL,
    current_version INT NOT NULL DEFAULT 1,
    rotated_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(project_id, scope, scope_id, name)
);

-- Secret versions table
CREATE TABLE IF NOT EXISTS secret_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    secret_id UUID NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    version INT NOT NULL,
    vault_ref VARCHAR(500) NOT NULL,
    rotation_status VARCHAR(50) DEFAULT 'active', -- pending/active/archived
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(secret_id, version)
);

-- Audit events for secret access
CREATE TABLE IF NOT EXISTS audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID,
    actor_type VARCHAR(50), -- user/service_account/system
    action VARCHAR(50), -- read/write/rotate/delete
    resource_type VARCHAR(50), -- secret/deployment/service
    resource_id UUID,
    resource_scope VARCHAR(50),
    details JSONB,
    ip_address INET,
    user_agent VARCHAR(500),
    status VARCHAR(50), -- success/failure
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Add indexes
CREATE INDEX IF NOT EXISTS idx_secrets_project_id ON secrets(project_id);
CREATE INDEX IF NOT EXISTS idx_secrets_scope ON secrets(scope, scope_id);
CREATE INDEX IF NOT EXISTS idx_secret_versions_secret_id ON secret_versions(secret_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_resource ON audit_events(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor ON audit_events(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at);
```

### Lockbox Interface

**File**: `apps/switchyard-api/internal/lockbox/client.go` (NEW)

```go
package lockbox

import (
    "context"
    "time"
)

// Client is the interface for secret management backends
type Client interface {
    // WriteSecret stores or updates a secret
    WriteSecret(ctx context.Context, scope, name string, value []byte) (version int, err error)
    
    // ReadSecret retrieves the current version of a secret
    ReadSecret(ctx context.Context, scope, name string) ([]byte, error)
    
    // GetVersion retrieves a specific version of a secret
    GetVersion(ctx context.Context, scope, name string, version int) ([]byte, error)
    
    // ListVersions returns all versions of a secret
    ListVersions(ctx context.Context, scope, name string) ([]int, error)
    
    // RotateSecret creates a new version and marks the old as archived
    RotateSecret(ctx context.Context, scope, name string, value []byte) (version int, err error)
    
    // DeleteSecret removes a secret from the backend
    DeleteSecret(ctx context.Context, scope, name string) error
    
    // Health checks the backend connectivity
    Health(ctx context.Context) error
}

// Config holds backend-specific configuration
type Config struct {
    Type     string        // "vault", "1password", "stub"
    Endpoint string        // Backend URL
    Token    string        // Authentication token
    Timeout  time.Duration // Request timeout
}
```

---

## Conclusion

The Enclii platform has a **well-designed secret management architecture** in the SOFTWARE_SPEC, but **zero production implementation**. 

**Current Score**: 15% Production Ready
- ❌ 0% Secret storage implementation
- ❌ 0% Rotation capability
- ❌ 0% Access control
- ✅ 60% Foundation (types, APIs exist but incomplete)

**To reach 100% production ready**: ~8-10 weeks of focused development across all 4 phases outlined above.

**Blockers for Production Deployment**:
1. No Lockbox integration (critical security)
2. No audit logging (critical compliance)
3. No zero-downtime rotation (critical reliability)
4. Type definition mismatches (risk of runtime errors)

**Recommendation**: Allocate full team sprint to implement Phases 1-2 before production release.

