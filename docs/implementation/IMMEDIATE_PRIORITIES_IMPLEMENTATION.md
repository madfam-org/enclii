# Immediate Priorities Implementation Plan

## Status: IN PROGRESS

This document tracks the implementation of critical fixes to make Enclii production-ready.

---

## Summary of Work Completed

### ✅ **1. Enhanced Database Repository Layer**

**File:** `apps/switchyard-api/internal/db/repositories.go`

**Changes:**
- Added `GetByID(ctx, id)` method to DeploymentRepository
- Added `ListByRelease(ctx, releaseID)` method to DeploymentRepository
- Added `GetLatestByService(ctx, serviceID)` method to DeploymentRepository
- Added `context` import for context-aware database operations

**Impact:** Enables querying deployments by service, which is critical for CLI commands like `logs` and `ps`.

---

## Work In Progress

### 🔄 **1. CLI Logs Command** (`packages/cli/internal/cmd/logs.go`)

**Status:** Partially implemented
**Current State:**
- Added imports for `context` and `client`
- Implemented API client initialization
- Added helpful error messages guiding users to alternative solutions

**Next Steps:**
1. Add API endpoint: `GET /v1/services/:id/deployments/latest`
2. Wire up CLI to fetch latest deployment by service name
3. Stream logs from deployment using existing `GetLogs` API endpoint
4. Handle follow mode with SSE/WebSocket

**Blocker:** Need to map service name → service ID → latest deployment ID

---

### 🔄 **2. CLI PS Command** (`packages/cli/internal/cmd/ps.go`)

**Status:** Not started
**Current State:** Showing mock data

**Implementation Plan:**
1. Add API endpoint: `GET /v1/services?environment={env}` with deployment status
2. Update API client with `ListServicesWithStatus(ctx, projectSlug, environment)` method
3. Wire up CLI to fetch real data from API
4. Format output with real deployment information

**Dependencies:**
- Requires service listing with embedded deployment status
- May need to add status aggregation logic in API handlers

---

### 🔄 **3. Rollback Logic** (`apps/switchyard-api/internal/k8s/client.go:265`)

**Status:** Partially implemented
**Current State:** TODO comment at line 265 - "Track previous images"

**Implementation Plan:**
1. **Database Changes:**
   - Add `previous_image` column to deployments table OR
   - Query releases table to get N-1 release image

2. **K8s Client Enhancement:**
   ```go
   func (c *Client) RollbackDeployment(ctx, name, namespace, previousImage string) error {
       // Get current deployment
       // Update to use previous image from release history
       // Monitor rollout status
       // Return success/failure
   }
   ```

3. **API Handler Update:**
   - In `RollbackDeployment` handler, query release history
   - Get previous release image
   - Pass to K8s client for rollback

**Dependencies:**
- Release history tracking (already available via `ListByService`)
- Deployment-to-release mapping (already exists)

---

### 🔄 **4. Build Pipeline** (`apps/switchyard-api/internal/api/handlers.go:306`)

**Status:** Mock implementation (10-second sleep)
**Current State:** TODO comment - "Clone repository and trigger build"

**Implementation Plan:**

#### **Phase 1: Repository Cloning**
```go
func (h *Handler) cloneRepository(gitURL, gitSHA, workDir string) error {
    // Use go-git library to clone repository
    // Checkout specific SHA
    // Return path to cloned repo
}
```

#### **Phase 2: BuildKit Integration**
```go
func (h *Handler) buildWithBuildKit(ctx, dockerfile, workDir, imageTag string) error {
    // Connect to BuildKit daemon
    // Build image with context
    // Tag and push to registry
    // Return image URI
}
```

#### **Phase 3: Buildpacks Support**
```go
func (h *Handler) buildWithBuildpacks(ctx, workDir, imageTag string) error {
    // Detect project type
    // Run Cloud Native Buildpacks
    // Push to registry
    // Return image URI
}
```

#### **Phase 4: Wire It All Together**
```go
func (h *Handler) triggerBuild(service, release, gitSHA) {
    // 1. Clone repo at gitSHA
    // 2. Detect build type (Dockerfile vs Buildpacks)
    // 3. Execute appropriate build
    // 4. Generate SBOM
    // 5. Sign image with cosign (future)
    // 6. Update release status
}
```

**Dependencies:**
- BuildKit daemon running (local or remote)
- Container registry credentials
- go-git library for cloning
- pack CLI for buildpacks

---

### 🔴 **5. UI Authentication** (`apps/switchyard-ui/app/projects/page.tsx:41`)

**Status:** Hardcoded token
**Current State:** `'Authorization': 'Bearer your-token-here'`

**Implementation Plan:**

#### **Phase 1: Auth Context Provider**
```typescript
// app/context/AuthContext.tsx
export function AuthProvider({ children }) {
  const [token, setToken] = useState<string | null>(null);
  const [user, setUser] = useState<User | null>(null);

  // OIDC login flow
  // Token refresh logic
  // Logout

  return <AuthContext.Provider value={{ token, user, login, logout }}>
}
```

#### **Phase 2: Protected API Client**
```typescript
// lib/api.ts
export async function fetchWithAuth(endpoint: string, options = {}) {
  const { token } = useAuth();

  return fetch(endpoint, {
    ...options,
    headers: {
      'Authorization': `Bearer ${token}`,
      ...options.headers,
    },
  });
}
```

#### **Phase 3: Update All Components**
- Replace hardcoded tokens with `useAuth()` hook
- Add login/logout UI
- Handle token expiration
- Implement refresh token flow

---

## New API Endpoints Needed

### 1. **Get Latest Deployment for Service**
```
GET /v1/services/:id/deployments/latest
GET /v1/services/:id/deployments/latest?environment=prod

Response:
{
  "deployment_id": "...",
  "service_id": "...",
  "release_id": "...",
  "environment": "prod",
  "status": "running",
  "health": "healthy",
  "replicas": 2,
  "created_at": "...",
  ...
}
```

### 2. **List Services with Status** (for ps command)
```
GET /v1/projects/:slug/services?environment=dev&include_status=true

Response:
{
  "services": [
    {
      "id": "...",
      "name": "api",
      "current_deployment": {
        "id": "...",
        "status": "running",
        "health": "healthy",
        "replicas": "2/2",
        "version": "v2024.01.15",
        "uptime": "2h 15m"
      }
    }
  ]
}
```

### 3. **Get Release History for Rollback**
```
GET /v1/services/:id/releases?limit=10

Response:
{
  "releases": [
    { "id": "...", "version": "v2", "image_uri": "...", "git_sha": "abc123", "created_at": "..." },
    { "id": "...", "version": "v1", "image_uri": "...", "git_sha": "def456", "created_at": "..." }
  ]
}
```

---

## Technical Debt & Risks

### 🔴 **Critical**
1. **Build Pipeline is Simulated** - Blocks any real deployments
2. **No Image Registry Configuration** - Can't push/pull images
3. **Rollback Uses Hardcoded String** - Will fail in production

### 🟡 **High Priority**
1. **No Error Recovery in Build Process** - Failed builds don't clean up
2. **Missing Build Logs** - No way to debug build failures
3. **No Build Timeouts** - Runaway builds could hang forever
4. **Authentication in UI** - Security risk with hardcoded tokens

### 🟢 **Medium Priority**
1. **No Log Streaming** - Only batch log retrieval works
2. **CLI Commands Don't Validate API Connection** - Poor UX on errors
3. **No Progress Indicators** - Users don't know if commands are working

---

## Implementation Timeline

### **Week 1: Core Infrastructure**
- [x] Add deployment repository methods
- [ ] Add new API endpoints for deployment queries
- [ ] Wire up CLI logs command (end-to-end)
- [ ] Wire up CLI ps command (end-to-end)
- [ ] Fix rollback with release history

### **Week 2: Build Pipeline**
- [ ] Integrate go-git for repository cloning
- [ ] Add BuildKit client and build execution
- [ ] Add Buildpacks support
- [ ] Implement build logging and error handling
- [ ] Add build timeouts and cleanup

### **Week 3: Polish & Testing**
- [ ] Fix UI authentication with OIDC
- [ ] Add integration tests for deploy flow
- [ ] Add error recovery and retry logic
- [ ] Create runbooks for common operations
- [ ] Performance testing and optimization

### **Week 4: Production Readiness**
- [ ] Security audit and penetration testing
- [ ] Load testing
- [ ] Documentation updates
- [ ] Alpha deployment to internal cluster
- [ ] Migration of first production service

---

## Testing Strategy

### **Unit Tests**
- [ ] Repository methods (deployments)
- [ ] API client methods (logs, ps, rollback)
- [ ] Build pipeline components
- [ ] K8s client rollback logic

### **Integration Tests**
- [ ] End-to-end deploy workflow
- [ ] Log streaming from K8s to CLI
- [ ] Rollback with verification
- [ ] Build → Release → Deploy pipeline

### **E2E Tests**
- [ ] Full service lifecycle (create → deploy → rollback → delete)
- [ ] Multi-environment deployment
- [ ] Preview environment provisioning
- [ ] Failure scenarios and recovery

---

## Success Criteria

### **Alpha Readiness**
- ✅ Database schema supports all operations
- ⏳ CLI commands work end-to-end (in progress)
- ⏳ Build pipeline executes real builds (not started)
- ⏳ Rollback works reliably (partially done)
- ❌ UI authentication implemented (not started)
- ❌ Integration tests pass (not started)

### **Production Readiness**
- Deploy ≥1 real service successfully
- Zero critical bugs in core workflows
- All CLI commands have <5% error rate
- Build time P95 < 8 minutes
- Rollback time P95 < 30 seconds
- 99% uptime over 14-day period

---

## Notes & Decisions

### **Decision: Deployment Query Strategy**
We added `GetLatestByService` method to avoid complex API endpoint design. This allows CLI to query deployments directly by service ID.

### **Decision: Build Pipeline Approach**
Will support both Dockerfile and Buildpacks to provide flexibility. BuildKit will be primary for Dockerfile builds.

### **Decision: Authentication**
UI will use OIDC with JWT tokens. API already supports this; just need to wire up the frontend.

---

## Contributors
- Implementation started: 2025-01-19
- Last updated: 2025-01-19
- Primary developer: Claude

---

## Related Documents
- Software Specification: `SOFTWARE_SPEC.md` (repo root)
- [MVP Implementation](./MVP_IMPLEMENTATION.md) - MVP status
- [Architecture](../architecture/ARCHITECTURE.md) - System architecture
