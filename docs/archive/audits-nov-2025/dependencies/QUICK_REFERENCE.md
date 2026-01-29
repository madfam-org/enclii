# Enclii Dependency Management - Quick Reference Guide

**Last Updated:** 2025-11-20

---

## TL;DR - Critical Issues

1. **Missing go.sum files** - Generate with `go mod tidy`
2. **Missing package-lock.json** - Generate with `npm ci`
3. **Floating image tags** - Pin Alpine to version 3.20
4. **Version mismatches** - Sync integration tests to main Go/K8s versions
5. **No security scanning** - Add Trivy to CI pipeline

---

## GO DEPENDENCIES

### Current Status
- **Modules:** 5 (API, CLI, SDK, Reconcilers, Tests)
- **Direct dependencies:** 23
- **Go versions:** 1.21-1.24.7 (MISMATCHED)
- **go.sum files:** MISSING (ALL 5 MODULES)

### Quick Commands

```bash
# Check dependencies
go mod graph
go mod tidy

# Verify integrity
go mod verify

# List dependencies
go list -m all

# Update specific package
go get -u github.com/package/name

# Remove unused
go mod tidy

# Vendor dependencies
go mod vendor
```

### Version Requirements

```
Module                    | Go Version | Status
--------------------------|-----------|----------
apps/switchyard-api      | 1.23.0    | ✅ OK
apps/reconcilers         | 1.22      | ⚠️ Update
packages/cli             | 1.22      | ⚠️ Update
packages/sdk-go          | 1.22      | ⚠️ Update
tests/integration        | 1.21      | ❌ URGENT
Installed runtime        | 1.24.7    | ✅ OK
```

### Key Dependencies

```
Gin Framework       v1.10.0    (Web framework)
Cobra CLI           v1.8.0     (CLI framework)
PostgreSQL Driver   v1.10.9    (⚠️ Slightly outdated)
Redis Client        v9.3.1     (Cache)
K8s Client          v0.29.0    (Kubernetes API)
OpenTelemetry       v1.21.0    (Tracing)
Logrus              v1.9.3     (Logging)
```

### When to Update

```
Security patches:     Immediately (within 48 hours)
Bug fixes:           Within 1 week
Minor updates:       Monthly review
Major versions:      Quarterly evaluation
```

### Tools

- **golangci-lint** - Linting (pin version in CI)
- **go mod** - Built-in dependency management
- **go get** - Package updates

---

## NPM DEPENDENCIES

### Current Status
- **UI Framework:** Next.js 14.0.0 with React 18.2.0
- **Direct dependencies:** 10
- **Lock file:** MISSING ❌
- **Node version:** v22.21.1

### Package Status

```
next          ^14.0.0    ✅ Current (major v15 coming)
react         ^18.2.0    ✅ Current (major v19 coming)
typescript    ^5.0.0     ✅ Current
tailwindcss   ^3.3.0     ✅ Current
eslint        ^8.57.0    ⚠️ Major v9 available
```

### Quick Commands

```bash
# Install dependencies
npm install

# Clean install with lock file
npm ci

# Generate lock file only
npm install --package-lock-only

# Check for vulnerabilities
npm audit

# Update packages
npm update
npm outdated

# Interactive update
npm update [package-name]
```

### Setup New Environment

```bash
# 1. Generate lock file
npm install --package-lock-only

# 2. Clean install on CI/deployment
npm ci

# 3. Install production dependencies only
npm ci --omit=dev
```

### Audit Compliance

```bash
# Check vulnerabilities
npm audit

# Fix automatically
npm audit fix

# Force fixes (breaking changes possible)
npm audit fix --force

# In CI pipeline
npm audit --audit-level=moderate  # Fail on moderate+
```

### Adding Packages

```bash
# Production dependency
npm install package-name

# Development dependency
npm install --save-dev package-name

# Update lock file
npm install  # Automatically updates package-lock.json
```

### Version Constraints in package.json

```json
{
  "version": "1.0.0",
  "engines": {
    "node": ">=20.0.0 <24.0.0",
    "npm": ">=9.0.0"
  },
  "dependencies": {
    "react": "^18.2.0",      // >=18.2.0 <19.0.0
    "next": "^14.0.0"        // >=14.0.0 <15.0.0
  }
}
```

---

## CONTAINER DEPENDENCIES

### Base Images

```
golang:1.24.7-alpine3.20   (Builder stage)  ⚠️ Currently: 1.22-alpine
alpine:3.20                (Runtime)        ❌ Currently: latest (CRITICAL)
postgres:15                (Dev DB)         ⚠️ EOL Nov 2025
redis:7-alpine             (Cache)          ✅ Current
```

### Docker Commands

```bash
# Build image
docker build -f apps/switchyard-api/Dockerfile -t enclii:latest .

# Scan for vulnerabilities
trivy image enclii:latest

# Check base image vulnerabilities
trivy image alpine:3.20
trivy image golang:1.24.7-alpine3.20

# Pin image version in docker-compose
image: postgres:15.0  # Use specific version, not latest
```

### Dockerfile Best Practices

```dockerfile
# PIN VERSIONS (not floating tags)
FROM golang:1.24.7-alpine3.20 AS builder
RUN apk add --no-cache package-name=version

# Multi-stage builds (already implemented)
FROM alpine:3.20 AS runtime

# Non-root user
RUN adduser -D -u 1000 appuser
USER appuser

# Health checks
HEALTHCHECK --interval=30s --timeout=3s CMD command

# Explicit EXPOSE
EXPOSE 8080
```

### Image Updates

```bash
# Check for updates
docker pull golang:latest
docker inspect golang:1.24.7-alpine3.20

# Alpine release info
# 3.20: Supported until Nov 2025
# 3.19: Supported until May 2025
# 3.18: Supported until May 2024
```

---

## KUBERNETES DEPENDENCIES

### Supported Versions

```yaml
Kubernetes:  1.28.0 - 1.30+ (n-3 supported)
K8s API:     v0.29.0
cert-manager: v1.13.2
nginx-ingress: v1.8.0 (CURRENTLY FLOATING at 'main')
PostgreSQL: 15 (EOL Nov 2025, upgrade to 16)
Redis: 7.x
```

### Key Controllers

```
cert-manager/v1.13.2          (TLS cert automation)
sigs.k8s.io/controller-runtime (K8s operators)
nginx-ingress (v1.8.0)        (Ingress controller)
```

### Manifest Versions

```yaml
# Always specify version in manifests
apiVersion: v1
kind: Deployment

metadata:
  name: my-deployment
  labels:
    app.kubernetes.io/version: "1.0.0"

spec:
  template:
    spec:
      containers:
      - image: ghcr.io/madfam/service:v1.0.0  # ALWAYS PIN
```

### Update Path

```
Current K8s: v1.28.0
Minor patch updates: Safe (1.28.0 → 1.28.5)
Minor version updates: Test first (1.28 → 1.29)
Major updates: Plan carefully (k9s dependencies)
```

---

## CI/CD & BUILD TOOLS

### Tool Versions

```
Go:         1.24.7
Node:       22.21.1
Kind:       v0.20.0
Kubernetes: v1.28.0
Docker:     24.0.0+
kubectl:    1.28.0
```

### Verify Tools

```bash
go version
node --version
kind --version
kubectl version
docker version

# Using .tool-versions (asdf)
asdf install
asdf current
```

### Makefile Targets

```bash
make bootstrap         # Install deps
make build-all         # Build all components
make test              # Run tests
make lint              # Linting
make check-deps        # Verify dependencies (proposed)
```

### CI/CD Workflow

```yaml
# .github/workflows/integration-tests.yml
GO_VERSION: '1.23'              # Update from 1.21
KIND_VERSION: 'v0.20.0'         # Pinned
KUBERNETES_VERSION: 'v1.28.0'   # Pinned
```

---

## SECURITY & SCANNING

### Current Status
- ✅ No GPL/AGPL dependencies (SaaS-safe)
- ❌ No SBOM generation
- ❌ No image signing
- ❌ No container scanning
- ❌ No npm audit in CI
- ⚠️ Missing go.sum (verification impossible)

### Security Tools

```bash
# Container scanning
trivy image ghcr.io/madfam/switchyard-api:latest

# SBOM generation
syft ghcr.io/madfam/switchyard-api:latest

# Image signing
cosign sign ghcr.io/madfam/switchyard-api:latest

# npm security
npm audit

# Go vulnerability check
go list -json -m all | nancy sleuth  # Using nancy
govulncheck ./...                     # Using govulncheck
```

### Enable in .env.build

```bash
ENABLE_SBOM=true                # Generate SBOM
ENABLE_IMAGE_SIGNING=true       # Sign images with cosign
ENABLE_VULNERABILITY_SCAN=true  # Scan with trivy
```

---

## COMMON TASKS

### Update a Go Package

```bash
# To specific version
go get github.com/package/name@v1.5.0

# To latest
go get -u github.com/package/name

# Verify and tidy
go mod tidy
go mod verify
```

### Update npm Package

```bash
# Check for updates
npm outdated

# Update single package
npm update package-name

# Update all (respects semver)
npm update

# Commit lock file
git add package-lock.json
```

### Add New Go Dependency

```bash
go get github.com/new/package
go mod tidy
go mod verify
git add go.mod go.sum
```

### Add New npm Package

```bash
npm install package-name
npm install --save-dev dev-package
git add package-lock.json
```

### Upgrade Go Version

```bash
# Edit go.mod in affected modules
go 1.24

# Rebuild
go mod tidy
go mod download

# Test
go test ./...
```

### Upgrade Node Version

```bash
# Edit package.json
"engines": {
  "node": ">=22.0.0 <24.0.0"
}

# Test with nvm
nvm install 22.21.1
nvm use 22.21.1
npm install
npm test
```

---

## TROUBLESHOOTING

### "go mod verify" fails
**Cause:** Missing go.sum file
**Fix:** Run `go mod tidy`

### Build fails with missing go.sum
**Cause:** New module without go.sum
**Fix:** `cd [module] && go mod download && go mod tidy`

### npm install gives different versions
**Cause:** Missing package-lock.json
**Fix:** `npm install --package-lock-only && npm ci`

### Docker image changes size
**Cause:** Floating base image tag
**Fix:** Pin exact version: `FROM alpine:3.20` not `FROM alpine`

### K8s tests fail on version mismatch
**Cause:** Incompatible K8s library versions
**Fix:** Sync version across all go.mod files

### Image signing fails in CI
**Cause:** cosign not installed or credentials missing
**Fix:** Install cosign, configure COSIGN_KEY in secrets

---

## POLICIES

### Update Timeline
```
Critical Security: Within 48 hours
High Priority Bug: Within 1 week
Feature/Minor: Monthly review
Major Versions: Quarterly evaluation
```

### Approval Requirements
```
Security patches:     Automatic (if CI passes)
Bug fixes (<1MB):    Automatic (if CI passes)
Major dependencies:  Code review required
GPL/AGPL licenses:   Legal review required
```

### Testing Requirements
```
Go updates:  Run: go test ./...
npm updates: Run: npm test
Image:       Run: docker build && trivy scan
K8s:         Run in integration test cluster
```

---

## RESOURCES

- **Go Modules:** https://golang.org/doc/modules
- **npm documentation:** https://docs.npmjs.com/
- **Kubernetes:** https://kubernetes.io/docs/
- **Container Security:** https://owasp.org/www-project-docker-top-10/
- **SBOM:** https://www.cisa.gov/sbom
- **Trivy:** https://aquasecurity.github.io/trivy/

---

## CONTACTS

- **Go questions:** Backend team lead
- **npm questions:** Frontend team lead
- **Container/K8s:** DevOps/Platform team
- **Security:** Security team
- **Escalations:** Technical steering committee

---

**This guide last updated:** 2025-11-20
**Next review date:** 2025-12-20
**Owner:** Platform Engineering
