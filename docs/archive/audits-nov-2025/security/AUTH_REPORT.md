# Authentication & Authorization System Audit - Enclii

**Date:** November 19, 2025
**Status:** Alpha (v0.1)
**Finding Level:** CRITICAL - Multiple compliance and implementation gaps

---

## EXECUTIVE SUMMARY

The Enclii authentication and authorization system has significant gaps in implementation and design that prevent compliance with SOC 2 requirements. While JWT-based authentication with RSA signing is partially implemented, the RBAC system is rudimentary, database schemas for user/team/role management are entirely missing, and there are critical bugs in auth initialization.

**Key Issues:**
1. No user/team/role database schema (0% implemented)
2. OIDC configured but not integrated (broken initialization)
3. Role constants referenced but not defined
4. No environment-level permissions
5. No audit logging
6. No project-level authorization enforcement

---

## 1. SSO & AUTH IMPLEMENTATION

### Current Status

**Files Examined:**
- `/home/user/enclii/apps/switchyard-api/internal/auth/jwt.go`
- `/home/user/enclii/apps/switchyard-api/internal/config/config.go`
- `/home/user/enclii/apps/switchyard-api/cmd/api/main.go`
- `/home/user/enclii/packages/cli/internal/config/config.go`

### JWT Implementation (PARTIAL - ~40%)

```go
// apps/switchyard-api/internal/auth/jwt.go (Lines 27-34)
type Claims struct {
    UserID      uuid.UUID `json:"user_id"`
    Email       string    `json:"email"`
    Role        string    `json:"role"`
    ProjectIDs  []string  `json:"project_ids,omitempty"`
    TokenType   string    `json:"token_type"` // "access" or "refresh"
    jwt.RegisteredClaims
}
```

**What's Implemented:**
- RSA-2048 key pair generation (lines 67-69)
- Access token (15 min default) + refresh token (30 day default) pair generation
- Token signing with RS256 algorithm
- Token validation with signature verification
- Basic role claim in JWT

**What's Missing:**
- Token issuance endpoint (no `/login` or `/auth` endpoint)
- Token expiration enforcement in handlers
- Token revocation/blacklist mechanism
- Session management
- Multi-factor authentication (MFA)

### OIDC Configuration (DEFINED BUT NOT INTEGRATED - 0%)

```go
// apps/switchyard-api/internal/config/config.go (Lines 20-23)
OIDCIssuer       string
OIDCClientID     string
OIDCClientSecret string

// Defaults set to (Lines 47-49)
viper.SetDefault("oidc-issuer", "http://localhost:5556")
viper.SetDefault("oidc-client-id", "enclii")
viper.SetDefault("oidc-client-secret", "enclii-secret")
```

**CRITICAL BUG:** Initialization Mismatch

```go
// apps/switchyard-api/cmd/api/main.go (Lines 62-66)
authManager, err := auth.NewJWTManager(
    cfg.OIDCIssuer,           // ← WRONG: should be time.Duration
    cfg.OIDCClientID,         // ← WRONG: should be time.Duration
    cfg.OIDCClientSecret,
)
```

**Actual Function Signature:**
```go
func NewJWTManager(tokenDuration, refreshDuration time.Duration) (*JWTManager, error)
```

**Status:** This code will compile (Go is weakly typed for string -> interface{}) but will fail at runtime.

### CLI Authentication (NOT IMPLEMENTED - 0%)

**README claims:** `./bin/enclii auth login # opens browser (dev OIDC)`

**Reality:** 
- No `auth` command exists in CLI
- Available commands: `init`, `deploy`, `logs`, `ps`, `rollback`, `version`
- Token is only read from environment variable `ENCLII_API_TOKEN`
- No browser-based login flow

**Files:**
- `/home/user/enclii/packages/cli/internal/cmd/` (no auth.go)
- `/home/user/enclii/packages/cli/internal/config/config.go` (Line 17): `APIToken string` - read from env, not set via login

### Token Flow Issues

1. **No token issuance:** How do users get their first token?
2. **No token exchange:** OIDC code → token exchange not implemented
3. **No token refresh:** RefreshToken method exists but never called from handlers
4. **No PKCE:** No proof key for code exchange (if OIDC were implemented)

---

## 2. RBAC SCHEMA ANALYSIS

### Database Schema Status

**Examined Files:**
- `/home/user/enclii/apps/switchyard-api/internal/db/migrations/001_initial_schema.up.sql`
- `/home/user/enclii/apps/switchyard-api/internal/db/repositories.go`

**CRITICAL FINDING:** Users, Teams, Roles, and Permissions tables **DO NOT EXIST**

```sql
-- What EXISTS (001_initial_schema.up.sql):
CREATE TABLE projects (...)
CREATE TABLE environments (...)
CREATE TABLE services (...)
CREATE TABLE releases (...)
CREATE TABLE deployments (...)

-- What's MISSING (required for RBAC):
-- NO: users table
-- NO: teams table
-- NO: roles table
-- NO: permissions table
-- NO: user_roles table
-- NO: team_members table
-- NO: project_access table
-- NO: audit_logs table
```

### Role Definition Issues

**References in Code:**
```go
// apps/switchyard-api/internal/api/handlers.go (Lines 76-96)
v1.POST("/projects", h.auth.RequireRole(types.RoleAdmin), h.CreateProject)
v1.POST("/projects/:slug/services", h.auth.RequireRole(types.RoleDeveloper), h.CreateService)
v1.POST("/services/:id/build", h.auth.RequireRole(types.RoleDeveloper), h.BuildService)
v1.POST("/services/:id/deploy", h.auth.RequireRole(types.RoleDeveloper), h.DeployService)
v1.POST("/deployments/:id/rollback", h.auth.RequireRole(types.RoleDeveloper), h.RollbackDeployment)
```

**Role Constants:**
- `types.RoleAdmin` - REFERENCED but **NOT DEFINED**
- `types.RoleDeveloper` - REFERENCED but **NOT DEFINED**
- `types.RoleViewer` - Mentioned in README but **NOT DEFINED**

**Where they should be** (`packages/sdk-go/pkg/types/types.go`):
```go
// File is 135 lines - NO role constants defined
// Expected to see:
// const (
//     RoleAdmin    = "admin"
//     RoleDeveloper = "developer"
//     RoleViewer    = "viewer"
//     RoleOwner     = "owner"
// )
```

### Current RBAC Model

**Claims in JWT (Minimal):**
```go
type Claims struct {
    UserID      uuid.UUID
    Email       string
    Role        string           // ← Single role, flat structure
    ProjectIDs  []string         // ← No environment info
}
```

**Limitations:**
1. **Single role per user** - No role hierarchy
2. **Project-level only** - No environment-specific roles
3. **No permissions** - Just role name strings
4. **No groups/teams** - Hardcoded to individual users
5. **No inheritance** - Admin doesn't inherit Developer permissions

### Team Model

**SOFTWARE_SPEC.md mentions:**
- Owner, Admin, Developer, ReadOnly team roles

**Reality:**
- No Team table in database
- No team membership structure
- No team-level access control
- No group-based permissions

---

## 3. CURRENT PERMISSION MODEL

### Authorization Middleware

**File:** `apps/switchyard-api/internal/auth/jwt.go` (Lines 236-271)

```go
func (j *JWTManager) RequireRole(roles ...string) gin.HandlerFunc {
    return func(c *gin.Context) {
        userRole, exists := c.Get("user_role")
        if !exists {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "User role not found"})
            c.Abort()
            return
        }

        roleStr, ok := userRole.(string)
        if !ok {
            c.JSON(http.StatusInternalServerError, ...)
            c.Abort()
            return
        }

        // SIMPLE STRING COMPARISON - No permission matrix
        hasRole := false
        for _, role := range roles {
            if roleStr == role {
                hasRole = true
                break
            }
        }

        if !hasRole {
            c.JSON(http.StatusForbidden, ...)
            c.Abort()
            return
        }

        c.Next()
    }
}
```

**Strengths:**
- Simple, fast implementation
- Prevents unauthorized access at route level

**Weaknesses:**
- Exact role match required (no Admin > Developer hierarchy)
- No permission granularity
- No environment context in decision
- No dynamic permission loading

### Project-Level Authorization

**File:** `apps/switchyard-api/internal/auth/jwt.go` (Lines 273-308)

```go
func (j *JWTManager) RequireProjectAccess() gin.HandlerFunc {
    return func(c *gin.Context) {
        projectSlug := c.Param("slug")
        if projectSlug == "" {
            c.Next()
            return
        }

        projectIDs, exists := c.Get("project_ids")
        if !exists {
            c.JSON(http.StatusForbidden, gin.H{"error": "No project access"})
            c.Abort()
            return
        }

        projectIDList, ok := projectIDs.([]string)
        if !ok {
            c.JSON(http.StatusInternalServerError, ...)
            c.Abort()
            return
        }

        // For now, allow access if user has any project access
        // In production, implement proper project-level authorization
        if len(projectIDList) == 0 {
            userRole, _ := c.Get("user_role")
            if userRole != "admin" && userRole != "owner" {
                c.JSON(http.StatusForbidden, ...)
                c.Abort()
                return
            }
        }

        c.Next()
    }
}
```

**CRITICAL:** Comment on lines 295-296:
```
// For now, allow access if user has any project access
// In production, implement proper project-level authorization
```

**Current Behavior:**
- No actual project slug validation against projectIDs
- Always allows if user has ANY project
- TODO not implemented

### Permission Enforcement Gaps

1. **No environment-level permissions:**
   ```
   Current:  Admin OR Developer for all operations
   Required: Admin in Prod, Developer in Staging, Viewer in any environment
   ```

2. **No resource-level checks:**
   ```
   No verification that:
   - User accessing service X has access to service X
   - User accessing deployment Y in project Z has access to project Z
   - User can only see logs they have access to
   ```

3. **No operation-level granularity:**
   ```
   Current:  "Can deploy" (all or nothing)
   Required: "Can deploy to staging", "Can deploy to prod", "Can view logs", etc.
   ```

4. **No audit trail:**
   ```
   No logging of:
   - Who accessed what resource
   - When permissions changed
   - Unauthorized access attempts
   ```

---

## 4. SECURITY ISSUES & COMPLIANCE GAPS

### SOC 2 Requirements vs. Implementation

| Requirement | Status | Gap |
|---|---|---|
| **Access Control** | 10% | No granular user/role database; binary role checks only |
| **Audit Logging** | 0% | No audit_logs table; no operation logging |
| **Authentication** | 30% | JWT works but OIDC broken; no SSO integration; no MFA |
| **Authorization** | 20% | Role-based but not fine-grained; no environment awareness |
| **Session Management** | 0% | No session table; no token revocation; no login history |
| **User Management** | 0% | No users table; no admin console for user management |
| **Change Log** | 0% | No audit trail for permission changes |

### Critical Missing Tables

```sql
-- USERS & TEAMS
CREATE TABLE users (
    id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255),
    sso_provider VARCHAR(50),
    sso_id VARCHAR(255),
    created_at TIMESTAMP,
    last_login TIMESTAMP,
    active BOOLEAN DEFAULT true
);

CREATE TABLE teams (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    organization_id UUID,
    created_at TIMESTAMP
);

CREATE TABLE team_members (
    user_id UUID REFERENCES users(id),
    team_id UUID REFERENCES teams(id),
    role VARCHAR(50), -- owner, admin, member, viewer
    created_at TIMESTAMP,
    PRIMARY KEY (user_id, team_id)
);

-- ROLES & PERMISSIONS
CREATE TABLE roles (
    id UUID PRIMARY KEY,
    name VARCHAR(255) UNIQUE,
    description TEXT,
    organization_id UUID
);

CREATE TABLE permissions (
    id UUID PRIMARY KEY,
    name VARCHAR(255) UNIQUE,
    resource VARCHAR(255),
    action VARCHAR(255),
    -- e.g. resource='service', action='deploy:production'
    description TEXT
);

CREATE TABLE role_permissions (
    role_id UUID REFERENCES roles(id),
    permission_id UUID REFERENCES permissions(id),
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE project_access (
    user_id UUID REFERENCES users(id),
    project_id UUID REFERENCES projects(id),
    role VARCHAR(50), -- owner, admin, developer, viewer
    environment_id UUID REFERENCES environments(id), -- per-env roles
    created_at TIMESTAMP,
    PRIMARY KEY (user_id, project_id, environment_id)
);

-- AUDIT LOGGING
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,
    user_id UUID REFERENCES users(id),
    action VARCHAR(255),
    resource_type VARCHAR(100),
    resource_id UUID,
    changes JSONB,
    ip_address VARCHAR(45),
    user_agent TEXT,
    timestamp TIMESTAMP DEFAULT NOW()
);

-- SESSION & TOKEN MANAGEMENT
CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    user_id UUID REFERENCES users(id),
    token_hash VARCHAR(255),
    created_at TIMESTAMP,
    expires_at TIMESTAMP,
    last_used TIMESTAMP,
    revoked_at TIMESTAMP,
    ip_address VARCHAR(45)
);
```

### Missing API Endpoints

```
POST   /v1/auth/login              - OIDC/password login
POST   /v1/auth/logout             - Revoke token
POST   /v1/auth/refresh            - Refresh access token
POST   /v1/auth/callback           - OIDC redirect callback

GET    /v1/users                   - List users (admin only)
POST   /v1/users                   - Create user (admin)
GET    /v1/users/:id               - Get user
PATCH  /v1/users/:id               - Update user
DELETE /v1/users/:id               - Delete user

POST   /v1/teams                   - Create team
GET    /v1/teams/:id               - Get team
PATCH  /v1/teams/:id               - Update team
POST   /v1/teams/:id/members       - Add member
DELETE /v1/teams/:id/members/:uid  - Remove member
PATCH  /v1/teams/:id/members/:uid  - Change role

POST   /v1/projects/:id/access     - Grant project access
GET    /v1/projects/:id/access     - List project access
PATCH  /v1/projects/:id/access/:uid - Change role
DELETE /v1/projects/:id/access/:uid - Revoke access

GET    /v1/audit-logs              - List audit events
```

---

## 5. SPECIFIC CODE LOCATIONS NEEDING REFACTORING

### Priority 1 - Critical (Breaks functionality)

**1. JWT Manager Initialization Bug**
- **File:** `apps/switchyard-api/cmd/api/main.go` (Lines 62-66)
- **Issue:** Wrong parameter types passed to NewJWTManager
- **Fix:** Either fix NewJWTManager signature to accept OIDC config OR change initialization to pass time.Duration

**2. Missing Role Constants**
- **File:** `packages/sdk-go/pkg/types/types.go`
- **Issue:** RoleAdmin, RoleDeveloper referenced but not defined
- **Add:**
```go
const (
    RoleOwner     = "owner"
    RoleAdmin     = "admin"
    RoleDeveloper = "developer"
    RoleReadOnly  = "readonly"
)
```

**3. No Token Issuance**
- **File:** `apps/switchyard-api/internal/api/handlers.go`
- **Issue:** No login endpoint exists
- **Add:** Auth handler with login/logout endpoints

### Priority 2 - High (Missing core functionality)

**4. User Database Schema**
- **File:** `apps/switchyard-api/internal/db/migrations/`
- **Create:** `002_add_users_teams_rbac.up.sql`
- **Add:** users, teams, team_members, project_access tables

**5. RBAC Middleware**
- **File:** `apps/switchyard-api/internal/auth/`
- **Create:** `rbac.go` with:
  - Permission check middleware
  - Environment-aware authorization
  - Project access validation

**6. Audit Logging**
- **File:** `apps/switchyard-api/internal/db/`
- **Create:** audit_log.go repository and middleware

### Priority 3 - Medium (Design improvements)

**7. OIDC Integration**
- **File:** `apps/switchyard-api/internal/auth/`
- **Create:** `oidc.go` with:
  - OIDC provider integration
  - Code exchange flow
  - Callback handling

**8. Permission Model**
- **File:** `apps/switchyard-api/internal/auth/`
- **Create:** `permissions.go` with:
  - Permission definition enum
  - Hierarchical roles
  - Environment-aware checks

**9. CLI Auth Command**
- **File:** `packages/cli/internal/cmd/`
- **Create:** `auth.go` with:
  - Login command
  - Token management
  - Config persistence

---

## 6. COMPLIANCE READINESS CHECKLIST

### Authentication
- [ ] OIDC integration working (currently broken)
- [ ] JWT token issuance endpoint
- [ ] Token refresh mechanism
- [ ] Token revocation/blacklist
- [ ] Password reset flow
- [ ] MFA support
- [ ] Session timeout

### Authorization
- [ ] User management (CRUD)
- [ ] Team/group management
- [ ] Role definitions
- [ ] Permission matrix
- [ ] Environment-level access control
- [ ] Project-level access control
- [ ] Service-level access control

### Audit & Compliance
- [ ] Audit log table
- [ ] Log all auth events (login, logout, permission change)
- [ ] Log all data access (who accessed what resource when)
- [ ] Immutable audit trail
- [ ] Audit log retention policy
- [ ] Change history for resources

### User Management
- [ ] User database schema
- [ ] User provisioning (create, update, delete)
- [ ] User deprovisioning (cleanup on delete)
- [ ] Admin console for user management
- [ ] Bulk user operations
- [ ] User status tracking (active, inactive, suspended)

### Testing
- [ ] Unit tests for auth middleware
- [ ] Integration tests for RBAC
- [ ] Permission boundary tests
- [ ] Audit logging tests
- [ ] Token expiration tests

---

## 7. SUMMARY & RECOMMENDATIONS

### Current State
- **JWT signing:** Implemented (40%)
- **OIDC:** Configured but broken (0%)
- **RBAC:** Minimal string-based roles (20%)
- **User management:** No schema, no endpoints (0%)
- **Audit logging:** No implementation (0%)
- **Overall Auth maturity:** ~12% of what SOC 2 requires

### Immediate Actions (Week 1)
1. Create users, teams, roles, permissions tables (Migration #2)
2. Fix JWT manager initialization bug
3. Define role constants
4. Create audit_logs table and middleware
5. Add project_access table with environment awareness

### Short-term (Weeks 2-4)
1. Implement OIDC flow correctly
2. Add login/logout endpoints
3. Add user CRUD endpoints
4. Add team management endpoints
5. Implement permission checking middleware
6. Add audit log queries/API

### Medium-term (Weeks 5-8)
1. Add MFA support
2. Implement token revocation
3. Add bulk user operations
4. Create admin console UI
5. Add comprehensive tests
6. Complete SOC 2 documentation

### Risk Assessment
- **High Risk:** OIDC broken, no user schema, no audit logs
- **Medium Risk:** No project-level auth, no audit API
- **Low Risk:** Missing MFA, no token revocation (can be added incrementally)

---

## Appendix: Configuration Reference

**Environment Variables for Auth:**
```bash
# API Server
ENCLII_OIDC_ISSUER=https://auth.example.com
ENCLII_OIDC_CLIENT_ID=enclii-prod
ENCLII_OIDC_CLIENT_SECRET=<secret>

# CLI
ENCLII_API_TOKEN=<jwt-token>
ENCLII_API_ENDPOINT=https://api.enclii.dev
```

**JWT Defaults (Hardcoded):**
- Token Duration: 15 minutes
- Refresh Duration: 30 days
- Key Algorithm: RS256 (RSA-2048)
- Token Type: Bearer

