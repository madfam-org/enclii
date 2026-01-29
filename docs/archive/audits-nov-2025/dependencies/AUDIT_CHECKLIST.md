# Dependency Audit - Action Checklist

**Status:** Ready for Implementation
**Priority:** URGENT
**Estimated Effort:** 2-3 sprints to full resolution

---

## CRITICAL PRIORITY (Complete Immediately)

### Go Module Fixes
- [ ] **Generate go.sum file for switchyard-api**
  ```bash
  cd /home/user/enclii/apps/switchyard-api
  go mod tidy
  go mod verify
  git add go.sum
  ```

- [ ] **Generate go.sum file for reconcilers**
  ```bash
  cd /home/user/enclii/apps/reconcilers
  go mod tidy
  go mod verify
  git add go.sum
  ```

- [ ] **Generate go.sum file for CLI**
  ```bash
  cd /home/user/enclii/packages/cli
  go mod tidy
  go mod verify
  git add go.sum
  ```

- [ ] **Generate go.sum file for SDK**
  ```bash
  cd /home/user/enclii/packages/sdk-go
  go mod tidy
  go mod verify
  git add go.sum
  ```

- [ ] **Generate go.sum file for integration tests**
  ```bash
  cd /home/user/enclii/tests/integration
  go mod tidy
  go mod verify
  git add go.sum
  ```

### npm Fixes
- [ ] **Generate package-lock.json**
  ```bash
  cd /home/user/enclii/apps/switchyard-ui
  npm install --package-lock-only
  git add package-lock.json
  ```

- [ ] **Add node/npm version specifications**
  Edit `package.json`:
  ```json
  "engines": {
    "node": ">=20.0.0 <24.0.0",
    "npm": ">=9.0.0"
  }
  ```

### Container Fixes
- [ ] **Pin golang builder image version**
  Edit `apps/switchyard-api/Dockerfile`:
  ```dockerfile
  FROM golang:1.24.7-alpine3.20 AS builder
  ```

- [ ] **Pin alpine runtime image version**
  Edit `apps/switchyard-api/Dockerfile`:
  ```dockerfile
  FROM alpine:3.20
  ```

- [ ] **Add security context to Dockerfile**
  ```dockerfile
  USER appuser
  ```

---

## HIGH PRIORITY (This Week)

### Go Version Synchronization
- [ ] **Update integration tests Go version**
  Edit `tests/integration/go.mod`:
  ```
  go 1.23
  ```

- [ ] **Sync K8s dependencies in integration tests**
  Edit `tests/integration/go.mod`:
  ```
  k8s.io/api v0.29.0
  k8s.io/apimachinery v0.29.0
  k8s.io/client-go v0.29.0
  ```

- [ ] **Update testify in integration tests**
  Edit `tests/integration/go.mod`:
  ```
  github.com/stretchr/testify v1.10.0
  ```

### CI/CD Updates
- [ ] **Update CI workflow Go version**
  Edit `.github/workflows/integration-tests.yml`:
  ```yaml
  GO_VERSION: '1.23'
  ```

- [ ] **Pin nginx-ingress version**
  Replace floating `main` with specific version:
  ```yaml
  - run: |
      kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.8.0/deploy/static/provider/kind/deploy.yaml
  ```

### Tool Version File
- [ ] **Create .tool-versions for asdf**
  ```
  golang 1.24.7
  nodejs 22.21.1
  kind 0.20.0
  kubectl 1.28.0
  docker 24.0.0
  ```

---

## MEDIUM PRIORITY (This Month)

### Security Scanning
- [ ] **Add Trivy image scanning to CI**
  Create `.github/workflows/container-scan.yml`

- [ ] **Enable SBOM generation**
  Update `.env.build`:
  ```
  ENABLE_SBOM=true
  ```

- [ ] **Enable image signing**
  Update `.env.build`:
  ```
  ENABLE_IMAGE_SIGNING=true
  ```

- [ ] **Add npm audit to CI**
  Edit `.github/workflows/integration-tests.yml`:
  ```yaml
  - run: npm audit --audit-level=moderate
  ```

### Documentation
- [ ] **Create LICENSES.md**
  Document all dependencies and their licenses

- [ ] **Create DEPENDENCIES.md**
  Complete dependency inventory with update strategy

- [ ] **Create UPGRADE_GUIDE.md**
  Instructions for major version upgrades

- [ ] **Document K8s support matrix**
  Specify minimum/maximum supported versions

### Configuration
- [ ] **Create .npmrc for production builds**
  ```
  registry=https://registry.npmjs.org/
  legacy-peer-deps=false
  audit=true
  ```

- [ ] **Update Makefile**
  Add targets for dependency checks:
  ```makefile
  check-deps:
    go mod tidy && go mod verify
    npm audit
  ```

---

## MEDIUM PRIORITY (Next Quarter)

### Dependency Updates
- [ ] **Update lib/pq PostgreSQL driver**
  Current: v1.10.9 → Target: v1.10.11+
  
- [ ] **Update Jaeger**
  Current: 1.48 → Target: 1.51+
  
- [ ] **Update PostgreSQL in docker-compose**
  Current: 15 → Target: 16

- [ ] **Update eslint**
  Current: 8.57.0 → Plan for 9.0.0+

### Automation
- [ ] **Implement Dependabot**
  Create `.github/dependabot.yml`:
  ```yaml
  version: 2
  updates:
    - package-ecosystem: "gomod"
      directory: "/"
      schedule:
        interval: "weekly"
    - package-ecosystem: "npm"
      directory: "/apps/switchyard-ui"
      schedule:
        interval: "weekly"
  ```

- [ ] **Configure Renovate (alternative)**
  Create `renovate.json`

---

## LOW PRIORITY (Strategic)

### Major Version Planning
- [ ] **Plan Next.js 15 migration**
  Currently: 14.0.0, monitor 15.x releases

- [ ] **Monitor React 19 release**
  Currently: 18.2.0, evaluate impact

- [ ] **Plan Go 1.25 compatibility**
  Currently: 1.23, assess when released

- [ ] **Review Kubernetes v1.31 support**
  Currently testing: 1.28, plan forward compatibility

### Long-term Maintenance
- [ ] **Establish dependency update schedule**
  - Security patches: within 48 hours
  - Bug fixes: within 1 week
  - Minor updates: monthly review
  - Major updates: quarterly evaluation

- [ ] **Document dependency choices**
  Why each dependency is chosen and alternatives considered

- [ ] **Create vendor directory strategy**
  Decide on `go mod vendor` usage

---

## VERIFICATION STEPS

After completing each section, verify with:

### Go Modules
```bash
# Verify all go.sum files exist
find . -name "go.sum" -type f | wc -l
# Should output: 5

# Verify no import errors
go mod graph

# Verify checksums
go mod verify  # Run in each module directory
```

### npm
```bash
# Verify package-lock.json exists
ls -la apps/switchyard-ui/package-lock.json

# Verify lock file is valid
npm ls

# Run audit
npm audit
```

### Container
```bash
# Build and verify
docker build -f apps/switchyard-api/Dockerfile .

# Scan with trivy
trivy image <image-id>
```

### CI/CD
```bash
# Verify workflow syntax
gh workflow list
gh workflow view integration-tests.yml
```

---

## ESTIMATED EFFORT

| Section | Effort | Timeline |
|---------|--------|----------|
| Critical Priority | 4-6 hours | Day 1 |
| High Priority | 8-10 hours | Week 1 |
| Medium Priority (Security) | 6-8 hours | Week 2-3 |
| Medium Priority (Docs) | 4-6 hours | Week 3-4 |
| Medium Priority (Updates) | 6-8 hours | Month 1 |
| Low Priority | 10-15 hours | Q1 2025+ |
| **TOTAL** | **38-53 hours** | **2-3 sprints** |

---

## SUCCESS METRICS

- [ ] All go.sum files present and verified
- [ ] package-lock.json committed and reproducible
- [ ] All container images pinned to specific versions
- [ ] CI/CD passes with dependency scanning enabled
- [ ] npm audit shows zero vulnerabilities (audit-level moderate)
- [ ] Dockerfile passes Trivy scan
- [ ] Overall health score improves to 8.5+/10
- [ ] All team members trained on dependency management process

---

## CONTACTS & ESCALATION

- **Questions about Go dependencies:** Developer lead
- **Questions about npm/frontend:** Frontend lead
- **Security concerns:** Security team
- **CI/CD issues:** DevOps/Platform team
- **Prioritization:** Technical steering committee

---

**Checklist Created:** 2025-11-20
**Last Updated:** 2025-11-20
**Owner:** Technical Lead
**Status:** Ready for sprint planning
