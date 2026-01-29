# Enclii Testing Improvement Roadmap

**Document Version:** 1.0
**Last Updated:** November 20, 2025
**Status:** Ready for Implementation

---

## Quick Reference: Current State vs. Target State

### Current State (Baseline)
```
Total Test Files:     20 files
Overall Coverage:     ~3-5%
Critical Packages:    0% tested
Untested Packages:    17 of 25 (68%)
Frontend Tests:       0%
Integration Tests:    4 files (partial automation)
E2E Tests:           0%
Load Tests:          0%
Security Tests:      0%
Time to Execute:     ~5 minutes (unit) + 45 min (integration)
```

### Target State (12-Month Goal)
```
Total Test Files:     200+ files
Overall Coverage:     80%+
Critical Packages:    95%+ coverage
Untested Packages:    0 (all tested)
Frontend Tests:       80%+
Integration Tests:    20+ files (fully automated)
E2E Tests:           10+ workflows
Load Tests:          3+ scenarios
Security Tests:      10+ scenarios
Time to Execute:     ~10 minutes (unit) + 30 min (integration)
```

---

## Phase 1: Foundation (Weeks 1-2) - 40 Hours

### Goal: Establish testing infrastructure and fix broken tests

### Task 1.1: Fix Test Suite Execution (5 hours)
**Owner:** Backend Lead

**Current State:**
- Some tests may have compilation errors
- Coverage reporting not properly configured
- No pre-commit hooks

**Actions:**
```bash
# 1. Run all tests to identify failures
cd /home/user/enclii
make test 2>&1 | tee test-results.log

# 2. Fix any compilation errors
# For each error, update the corresponding test file

# 3. Verify all tests pass
make test

# 4. Generate coverage report
make test-coverage

# 5. Check coverage
go tool cover -html=coverage.out
```

**Definition of Done:**
- [ ] All unit tests pass with `make test`
- [ ] Coverage report generates without warnings
- [ ] Coverage.out files created for all packages
- [ ] Documentation updated in README

### Task 1.2: Setup Coverage Tracking (4 hours)
**Owner:** DevOps/CI Lead

**Implementation:**

1. **Install codecov.io integration:**

```yaml
# .github/workflows/coverage.yml
name: Coverage Reports
on:
  pull_request:
    branches: [main, develop]
  push:
    branches: [main, develop]

jobs:
  coverage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Generate coverage
        run: |
          cd apps/switchyard-api
          go test -coverprofile=coverage.out ./...
          
      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./apps/switchyard-api/coverage.out
          flags: unittests
          fail_ci_if_error: false
```

2. **Add coverage thresholds to pre-commit hook:**

```bash
# .git/hooks/pre-commit
#!/bin/bash
set -e

echo "Running tests..."
cd apps/switchyard-api
coverage=$(go test -coverprofile=coverage.tmp ./... | grep coverage | awk '{print $NF}' | sed 's/%//')

if (( $(echo "$coverage < 50" | bc -l) )); then
    echo "Coverage below 50%: ${coverage}%"
    exit 1
fi

echo "Coverage: ${coverage}%"
exit 0
```

**Definition of Done:**
- [ ] codecov.io account created
- [ ] GitHub Actions workflow added
- [ ] Coverage badge added to README
- [ ] Pre-commit hook installed

### Task 1.3: Create Test Data Factories (8 hours)
**Owner:** Backend Engineer

**Create:** `/apps/switchyard-api/internal/testutil/factories.go`

```go
package testutil

import (
    "github.com/google/uuid"
    "github.com/madfam/enclii/packages/sdk-go/pkg/types"
)

// ProjectFactory creates test projects
type ProjectFactory struct {
    ID          uuid.UUID
    Name        string
    Slug        string
    Description string
}

func NewProjectFactory() *ProjectFactory {
    return &ProjectFactory{
        ID:          uuid.New(),
        Name:        "Test Project",
        Slug:        "test-project",
        Description: "A test project",
    }
}

func (f *ProjectFactory) WithName(name string) *ProjectFactory {
    f.Name = name
    return f
}

func (f *ProjectFactory) WithSlug(slug string) *ProjectFactory {
    f.Slug = slug
    return f
}

func (f *ProjectFactory) Build() *types.Project {
    return &types.Project{
        ID:          f.ID,
        Name:        f.Name,
        Slug:        f.Slug,
        Description: f.Description,
    }
}

// ServiceFactory creates test services
type ServiceFactory struct {
    ID          uuid.UUID
    ProjectID   uuid.UUID
    Name        string
    GitRepo     string
}

func NewServiceFactory() *ServiceFactory {
    return &ServiceFactory{
        ID:        uuid.New(),
        ProjectID: uuid.New(),
        Name:      "test-service",
        GitRepo:   "https://github.com/test/test-service.git",
    }
}

func (f *ServiceFactory) Build() *types.Service {
    return &types.Service{
        ID:        f.ID,
        ProjectID: f.ProjectID,
        Name:      f.Name,
        GitRepo:   f.GitRepo,
    }
}
```

**Usage in Tests:**

```go
func TestCreateService(t *testing.T) {
    service := NewServiceFactory().
        WithName("api-server").
        Build()
    
    // Use service in test
}
```

**Definition of Done:**
- [ ] Factory file created
- [ ] Builder pattern implemented
- [ ] All entity types have factories
- [ ] Factory documentation added

### Task 1.4: Add Test Helper Documentation (3 hours)
**Owner:** Tech Lead

**Create:** `/docs/TESTING.md`

```markdown
# Testing Guide for Enclii

## Running Tests Locally

### Unit Tests
```bash
# All tests
make test

# Specific package
go test ./apps/switchyard-api/internal/api

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Integration Tests
```bash
# Requires Kind cluster
make kind-up
make test-integration
```

## Writing Tests

### Table-Driven Tests
```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {"valid input", "test", "result"},
        {"empty input", "", "error"},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test logic
        })
    }
}
```

### Using Test Factories
```go
project := NewProjectFactory().
    WithName("my-project").
    Build()
```
```

**Definition of Done:**
- [ ] Testing documentation created
- [ ] Examples added
- [ ] Guidelines documented
- [ ] Linked from README

### Task 1.5: Setup Pre-commit Hooks (5 hours)
**Owner:** DevOps Engineer

**Create:** `.pre-commit-config.yaml`

```yaml
repos:
  - repo: local
    hooks:
      - id: go-test
        name: Go Tests
        entry: bash -c 'cd apps/switchyard-api && go test -race ./...'
        language: system
        pass_filenames: false
        stages: [commit]
      
      - id: go-fmt
        name: Go Format
        entry: bash -c 'go fmt ./...'
        language: system
        pass_filenames: false
      
      - id: go-vet
        name: Go Vet
        entry: bash -c 'go vet ./...'
        language: system
        pass_filenames: false
      
      - id: golangci-lint
        name: golangci-lint
        entry: golangci-lint run
        language: system
        pass_filenames: false
```

**Definition of Done:**
- [ ] Pre-commit config created
- [ ] Hooks tested locally
- [ ] Installation documentation added
- [ ] All developers have hooks installed

---

## Phase 2: Critical Path Tests (Weeks 3-4) - 45 Hours

### Goal: Achieve 50% coverage of critical packages

### Task 2.1: Database Integration Tests (16 hours)
**Owner:** Backend Engineer (DB specialist)

**Create:** `/apps/switchyard-api/internal/db/db_test.go`

```go
package db

import (
    "context"
    "testing"
    
    "github.com/google/uuid"
    "github.com/stretchr/testify/require"
)

func TestProjectRepository_CRUD(t *testing.T) {
    ctx := context.Background()
    repo := setupTestRepository(t)
    
    // Create
    project := &types.Project{
        ID:   uuid.New(),
        Name: "Test Project",
        Slug: "test-project",
    }
    
    err := repo.Project.Create(ctx, project)
    require.NoError(t, err)
    
    // Read
    retrieved, err := repo.Project.GetByID(ctx, project.ID)
    require.NoError(t, err)
    require.Equal(t, project.Name, retrieved.Name)
    
    // Update
    project.Name = "Updated Project"
    err = repo.Project.Update(ctx, project)
    require.NoError(t, err)
    
    // Delete
    err = repo.Project.Delete(ctx, project.ID)
    require.NoError(t, err)
    
    // Verify deletion
    _, err = repo.Project.GetByID(ctx, project.ID)
    require.Error(t, err)
}

func TestProjectRepository_Transactions(t *testing.T) {
    ctx := context.Background()
    repo := setupTestRepository(t)
    
    // Test transaction rollback
    err := repo.Transaction(ctx, func(tx *Repository) error {
        project := &types.Project{
            ID:   uuid.New(),
            Name: "Rollback Test",
            Slug: "rollback-test",
        }
        return tx.Project.Create(ctx, project)
        // Simulated failure
        return errors.New("simulated error")
    })
    
    require.Error(t, err)
}

func TestProjectRepository_ConcurrentAccess(t *testing.T) {
    ctx := context.Background()
    repo := setupTestRepository(t)
    
    // Test concurrent reads
    for i := 0; i < 100; i++ {
        go func(id int) {
            _, _ = repo.Project.GetByID(ctx, uuid.New())
        }(i)
    }
}
```

**Coverage targets:**
- [ ] Create operations: 100%
- [ ] Read operations: 100%
- [ ] Update operations: 100%
- [ ] Delete operations: 100%
- [ ] Transactions: 90%
- [ ] Constraints: 95%

### Task 2.2: Authentication & Authorization Tests (16 hours)
**Owner:** Backend Engineer (Security specialist)

**Create:** `/apps/switchyard-api/internal/auth/auth_integration_test.go`

```go
package auth

import (
    "context"
    "testing"
    "time"
    
    "github.com/google/uuid"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestJWTManager_TokenLifecycle(t *testing.T) {
    manager, _ := NewJWTManager("test-secret-key-32-chars-long!!")
    
    userID := uuid.New()
    email := "test@example.com"
    role := "developer"
    
    // Generate token
    token, err := manager.GenerateAccessToken(userID, email, role)
    require.NoError(t, err)
    require.NotEmpty(t, token)
    
    // Verify token
    claims, err := manager.VerifyToken(token)
    require.NoError(t, err)
    assert.Equal(t, userID.String(), claims.Subject)
    assert.Equal(t, email, claims.Email)
    
    // Test expired token
    expiredToken := manager.generateExpiredToken(userID, email, role)
    _, err = manager.VerifyToken(expiredToken)
    require.Error(t, err)
    assert.Equal(t, "token expired", err.Error())
}

func TestPasswordManager_HashAndVerify(t *testing.T) {
    pm := NewPasswordManager()
    
    password := "SecurePassword123!"
    
    // Hash password
    hashed, err := pm.HashPassword(password)
    require.NoError(t, err)
    assert.NotEqual(t, password, hashed)
    
    // Verify correct password
    err = pm.VerifyPassword(hashed, password)
    require.NoError(t, err)
    
    // Verify incorrect password
    err = pm.VerifyPassword(hashed, "WrongPassword")
    require.Error(t, err)
}

func TestAuthService_MultiUserScenarios(t *testing.T) {
    // Test concurrent authentication
    ctx := context.Background()
    auth := NewAuthService(mockRepo, mockCache)
    
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            user := createTestUser(id)
            token, _ := auth.CreateSession(ctx, user)
            require.NotEmpty(t, token)
        }(i)
    }
    wg.Wait()
}
```

**Coverage targets:**
- [ ] JWT generation: 100%
- [ ] JWT verification: 95%
- [ ] Password operations: 100%
- [ ] Session management: 90%
- [ ] RBAC checks: 95%

### Task 2.3: Validation Tests (8 hours)
**Owner:** Backend Engineer

**Enhance:** `/apps/switchyard-api/internal/validation/validator_test.go`

```go
// Add tests for:
// - String validation (length, pattern, unicode)
// - Email validation (RFC 5322 compliant)
// - Slug validation (lowercase, hyphens only)
// - Numeric ranges
// - Custom rules
// - Batch validation

func TestValidator_StringLength(t *testing.T) {
    tests := []struct {
        input       string
        minLen      int
        maxLen      int
        shouldError bool
    }{
        {"valid", 1, 10, false},
        {"", 1, 10, true},           // Too short
        {"this is way too long", 1, 10, true}, // Too long
        {"café", 1, 10, false},      // Unicode
        {"🚀", 1, 10, false},         // Emoji
    }
    
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            err := ValidateStringLength(tt.input, tt.minLen, tt.maxLen)
            if tt.shouldError {
                require.Error(t, err)
            } else {
                require.NoError(t, err)
            }
        })
    }
}
```

**Definition of Done:**
- [ ] All validation rules covered
- [ ] Edge cases tested
- [ ] Performance acceptable (<100ms)
- [ ] Documentation updated

### Task 2.4: Kubernetes Integration Tests (5 hours)
**Owner:** Platform Engineer

**Create:** `/apps/switchyard-api/internal/k8s/k8s_integration_test.go`

```go
package k8s

import (
    "context"
    "testing"
    
    "github.com/stretchr/testify/require"
    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
)

func TestK8sClient_Deployment(t *testing.T) {
    client := setupTestK8sClient(t)
    ctx := context.Background()
    
    // Create deployment
    deployment := &appsv1.Deployment{
        // ... spec
    }
    
    created, err := client.CreateDeployment(ctx, "default", deployment)
    require.NoError(t, err)
    require.NotNil(t, created)
    
    // Get deployment
    retrieved, err := client.GetDeployment(ctx, "default", created.Name)
    require.NoError(t, err)
    
    // Delete deployment
    err = client.DeleteDeployment(ctx, "default", created.Name)
    require.NoError(t, err)
}
```

**Definition of Done:**
- [ ] Deployment CRUD tested
- [ ] Service CRUD tested
- [ ] ConfigMap injection tested
- [ ] Health check verification tested

---

## Phase 3: Frontend & E2E Testing (Weeks 5-6) - 30 Hours

### Task 3.1: Setup Frontend Testing (10 hours)

**Create:** `/apps/switchyard-ui/jest.config.js`

```javascript
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'jsdom',
  roots: ['<rootDir>/app'],
  testMatch: ['**/__tests__/**/*.ts?(x)', '**/?(*.)+(spec|test).ts?(x)'],
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx'],
  collectCoverageFrom: [
    'app/**/*.{ts,tsx}',
    '!app/**/*.d.ts',
    '!app/**/*.stories.tsx',
  ],
  coveragePathIgnorePatterns: ['/node_modules/', '/.next/'],
  setupFilesAfterEnv: ['<rootDir>/jest.setup.js'],
};
```

**Create:** `/apps/switchyard-ui/jest.setup.js`

```javascript
import '@testing-library/jest-dom';

// Mock Next.js router
jest.mock('next/router', () => ({
  useRouter: () => ({
    push: jest.fn(),
    pathname: '/',
    query: {},
  }),
}));
```

**Create:** `/apps/switchyard-ui/app/__tests__/layout.test.tsx`

```typescript
import { render, screen } from '@testing-library/react';
import RootLayout from '../layout';

describe('RootLayout', () => {
  it('renders the layout', () => {
    render(
      <RootLayout>
        <div>Test Child</div>
      </RootLayout>
    );
    
    expect(screen.getByText('Test Child')).toBeInTheDocument();
  });
});
```

**Definition of Done:**
- [ ] Jest properly configured
- [ ] React Testing Library installed
- [ ] 5+ component tests created
- [ ] Coverage reports generated

### Task 3.2: Create E2E Test Scenarios (20 hours)

**Create:** `/tests/e2e/playwright.config.ts`

```typescript
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'html',
  use: {
    baseURL: 'http://localhost:3000',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'npm run dev',
    url: 'http://localhost:3000',
    reuseExistingServer: !process.env.CI,
  },
});
```

**Create:** `/tests/e2e/deploy.spec.ts`

```typescript
import { test, expect } from '@playwright/test';

test('complete deployment workflow', async ({ page }) => {
  // Login
  await page.goto('/login');
  await page.fill('[name="email"]', 'test@example.com');
  await page.fill('[name="password"]', 'password123');
  await page.click('button[type="submit"]');
  
  // Create project
  await page.goto('/projects');
  await page.click('button:has-text("New Project")');
  await page.fill('[name="name"]', 'Test Project');
  await page.click('button:has-text("Create")');
  
  // Verify project created
  await expect(page.locator('text=Test Project')).toBeVisible();
});
```

**Definition of Done:**
- [ ] Playwright configured
- [ ] 10+ E2E scenarios created
- [ ] All major workflows covered
- [ ] Screenshots on failure

---

## Success Metrics & SLOs

### Phase 1 Completion (Week 2)
```
✓ All unit tests passing
✓ Coverage tooling working
✓ Test factories created
✓ Pre-commit hooks deployed
✓ Documentation complete
Status: Ready for Phase 2
```

### Phase 2 Completion (Week 4)
```
✓ Database coverage: 80%+
✓ Auth coverage: 85%+
✓ Validation coverage: 90%+
✓ Overall coverage: 30%+
✓ No flaky tests
Status: Ready for Phase 3
```

### Phase 3 Completion (Week 6)
```
✓ Frontend coverage: 60%+
✓ E2E tests: 10+ scenarios
✓ Overall coverage: 50%+
✓ CI/CD fully automated
✓ Zero manual steps
Status: Foundation complete
```

### Long-term Goals (Month 6)
```
Target: 80% overall coverage
- Critical packages: 95%+
- Database: 90%+
- Auth: 95%+
- UI: 85%+
- E2E: 15+ scenarios
- Load tests: 3+ profiles
- Security tests: 10+ scenarios
```

---

## Resource Requirements

### Phase 1 (Weeks 1-2)
- 1 Backend Lead (5 hours)
- 1 DevOps Engineer (4 hours)
- 1 Backend Engineer (20 hours)
- 1 Tech Lead (3 hours)
- **Total: 32 hours, 2 people**

### Phase 2 (Weeks 3-4)
- 2 Backend Engineers (45 hours)
- **Total: 45 hours, 2 people**

### Phase 3 (Weeks 5-6)
- 1 Frontend Engineer (10 hours)
- 1 QA Engineer (20 hours)
- **Total: 30 hours, 2 people**

**Total Project: 107 hours, 2-3 concurrent engineers, 6 weeks**

---

## Monitoring & Reporting

### Weekly Metrics
```
- Test count growth
- Coverage percentage
- Test execution time
- Flaky test count
- PR comment rate
```

### Coverage Dashboard
```
- By package breakdown
- Coverage trend (weekly)
- Test growth chart
- Failure rate tracking
```

### Definition of Success
```
✓ Coverage never decreases on main
✓ All tests pass in < 15 minutes
✓ Zero flaky tests in CI
✓ Coverage badges passing
✓ Team satisfied with testing experience
```

