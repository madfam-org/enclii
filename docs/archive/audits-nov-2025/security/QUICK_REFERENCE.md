# Security Audit - Quick Reference Guide
**Generated**: November 20, 2025

---

## TL;DR - Status

| Status | Detail |
|--------|--------|
| **Overall Rating** | B (Good) |
| **Production Ready** | ❌ NO - 5 Critical Issues |
| **Timeline to Ready** | 5-7 weeks |
| **Most Critical** | UI: Hardcoded tokens + no authentication |

---

## Critical Vulnerabilities (Fix These First)

### P0-1: HTTP Without TLS
- **File**: `apps/switchyard-api/cmd/api/main.go:215`
- **Impact**: Token interception, MITM attacks
- **Fix**: Add TLS to HTTP server or document Kubernetes ingress handles TLS
- **Effort**: 4 hours
- **CVSS**: 7.5

### P0-2: Unbounded Rate Limiter
- **File**: `apps/switchyard-api/internal/middleware/security.go:20`
- **Impact**: Memory exhaustion DoS attack
- **Fix**: Use bounded LRU cache or Redis-based rate limiting
- **Effort**: 6 hours
- **CVSS**: 7.0

### P0-3: Audit Log Buffer Overflow
- **File**: `apps/switchyard-api/internal/audit/async_logger.go:49`
- **Impact**: Audit logs dropped under load → compliance violation
- **Fix**: Implement persistent queue (Kafka/RabbitMQ/PostgreSQL)
- **Effort**: 8 hours
- **CVSS**: 8.0

### P0-4: Hardcoded UI Tokens
- **File**: `apps/switchyard-ui/app/**/*.tsx` (8 instances)
- **Impact**: Credential exposure in source code
- **Fix**: Remove all "Bearer your-token-here" strings
- **Effort**: 8 hours
- **CVSS**: 8.5

### P0-5: No UI Authentication
- **File**: `apps/switchyard-ui/` (entire app)
- **Impact**: Complete authentication bypass
- **Fix**: Implement OAuth 2.0 / OIDC flow
- **Effort**: 40 hours
- **CVSS**: 9.0

---

## High Priority Issues (P1)

| Issue | File | Fix Time |
|-------|------|----------|
| X-Forwarded-For not validated | middleware/security.go:375 | 3h |
| Context uses Background() | auth/jwt.go:173 | 6h |
| Weak password validation | auth/password.go:49 | 4h |
| Goroutine leaks | middleware/security.go:401 | 4h |
| No CSRF protection (UI) | switchyard-ui/ | 8h |
| Missing security headers (UI) | switchyard-ui/ | 8h |
| No frontend input validation | switchyard-ui/ | 12h |
| SBOM imageURI not validated | sbom/syft.go:55 | 3h |

**Total P1 Effort**: 48 hours

---

## What's Working Well ✓

### Backend (Switchyard API)
- ✓ JWT with RS256 signing
- ✓ Session revocation via Redis
- ✓ 100% SQL injection prevention (parameterized queries)
- ✓ Comprehensive input validation
- ✓ Vault integration for secrets
- ✓ Image signing (Cosign) + SBOM (Syft)
- ✓ Provenance tracking (PR approvals)

### Infrastructure (Kubernetes)
- ✓ Pod hardening (non-root, no privilege escalation)
- ✓ Network policies (strict egress/ingress)
- ✓ RBAC properly scoped
- ✓ Seccomp enabled
- ✓ Database/Redis isolation

### Supply Chain
- ✓ Image signing with Cosign
- ✓ SBOM generation with Syft
- ✓ Provenance checking before deployment
- ✓ Compliance webhooks (Drata/Vanta)

---

## Severity Summary

### By CVSS Score
- **Critical (9.0)**: UI Authentication bypass
- **High (8.5)**: Hardcoded tokens exposure
- **High (8.0)**: Audit log buffer overflow
- **High (7.5)**: HTTP without TLS
- **High (7.0)**: Unbounded rate limiter DoS
- **Medium (3.5+)**: Various P1/P2 issues

### By Category
- **Authentication**: 3 issues (UI no-auth, hardcoded tokens, weak passwords)
- **Network Security**: 2 issues (HTTP, X-Forwarded-For)
- **Resource Management**: 2 issues (rate limiter, audit buffer)
- **Compliance**: 1 issue (audit logging)
- **Injection**: 1 issue (SBOM imageURI)

---

## Remediation Roadmap

### Week 1-2: Critical Fixes (30 hours)
1. Audit log persistent queue (8h)
2. Rate limiter bounds (6h)
3. TLS configuration (4h)
4. Remove UI hardcoded tokens (8h)
5. Begin UI auth implementation

### Week 3-4: High Priority (40+ hours)
1. Complete UI authentication (40h)
2. CSRF protection (8h)
3. X-Forwarded-For validation (3h)
4. Security headers (8h)

### Week 5+: Medium Priority (27 hours)
1. Pagination (12h)
2. Retry logic (8h)
3. Context propagation (6h)
4. Monitoring (4h)

---

## Testing Checklist

### Add Security Tests
- [ ] Rate limiter under 10,000+ concurrent requests
- [ ] Audit logging at 10,000 events/second
- [ ] SQL injection attempts (should all fail)
- [ ] Command injection attempts (cosign, syft)
- [ ] CSRF token validation
- [ ] XSS payload injection
- [ ] Input validation edge cases

### Security Tools to Run
```bash
# Static analysis
gosec ./...
semgrep --config=p/security-audit

# Dependency scanning
trivy image [container]
snyk test

# Secret scanning
git-secrets --scan
truffleHog filesystem . --json

# Dynamic testing
owasp-zap ... # After security fixes
burpsuite ... # Penetration test
```

---

## Frontend Security - What to Fix

### Immediate (Before Any UI Deployment)
1. **Remove Hardcoded Tokens**
   - Search all files for "Bearer your-token"
   - Remove test/development tokens
   - Add to git-secrets to prevent re-introduction

2. **Implement Authentication**
   - Add OAuth 2.0 / OIDC provider
   - Secure token storage (httpOnly cookies)
   - Token refresh mechanism
   - Protected routes with middleware

### High Priority (Before Production)
1. **Add CSRF Protection**
   - CSRF tokens in forms
   - SameSite=Strict on cookies
   - Validate tokens on POST/PUT/DELETE

2. **Security Headers**
   - Content-Security-Policy
   - X-Frame-Options: DENY
   - X-Content-Type-Options: nosniff
   - Strict-Transport-Security

3. **Input Validation**
   - Client-side validation before submission
   - Type checking for all inputs
   - Proper error messages

---

## Backend Security - What to Fix

### Immediate (Before Any Production Deployment)
1. **HTTP → TLS**
   - Configure TLS cert
   - Set MinVersion to TLS 1.3
   - Use strong cipher suites

2. **Rate Limiter**
   - Replace unbounded map with LRU cache
   - Add proper cleanup/context cancellation
   - Consider Redis for distributed limiting

3. **Audit Logging**
   - Use persistent queue (Kafka preferred)
   - Handle backpressure
   - Return errors to client
   - Add metrics

### High Priority (Next Sprint)
1. **Context Propagation**
   - Use request context everywhere
   - Enforce timeouts throughout stack

2. **Password Validation**
   - Require uppercase + lowercase + numbers
   - Check common password list
   - UI password strength meter

3. **X-Forwarded-For Validation**
   - Verify request from trusted proxy
   - Whitelist proxy IPs

---

## Compliance Checklist

### Before Production (SOC 2 Type II)
- [ ] Audit logging complete (no drops)
- [ ] RBAC documented
- [ ] Encryption in-transit (TLS)
- [ ] Encryption at-rest (Vault)
- [ ] Access controls tested
- [ ] Incident response plan
- [ ] Security monitoring active

### GDPR Compliance
- [ ] Audit trail preservation
- [ ] Data retention policies
- [ ] Right-to-be-forgotten implementation
- [ ] Data protection impact assessment

---

## Key Files to Review

### Authentication
- `apps/switchyard-api/internal/auth/jwt.go` (✓ Good)
- `apps/switchyard-api/internal/auth/password.go` (⚠️ Weak validation)
- `apps/switchyard-ui/app/**/*.tsx` (✗ No auth)

### Data Security
- `apps/switchyard-api/internal/db/repositories.go` (✓ Secure)
- `apps/switchyard-api/internal/validation/validator.go` (✓ Good)

### Network Security
- `apps/switchyard-api/cmd/api/main.go` (✗ No TLS)
- `apps/switchyard-api/internal/middleware/security.go` (⚠️ Rate limiter issues)

### Audit & Compliance
- `apps/switchyard-api/internal/audit/async_logger.go` (✗ Buffer overflow)
- `apps/switchyard-api/internal/compliance/` (✓ Good)

### Kubernetes
- `infra/k8s/base/rbac.yaml` (✓ Good)
- `infra/k8s/base/network-policies.yaml` (✓ Excellent)
- `infra/k8s/production/security-patch.yaml` (✓ Excellent)

### Supply Chain
- `apps/switchyard-api/internal/signing/cosign.go` (✓ Good)
- `apps/switchyard-api/internal/sbom/syft.go` (⚠️ Input validation)
- `apps/switchyard-api/internal/provenance/checker.go` (✓ Good)

---

## Quick Reference: Issues by Component

### Switchyard API Backend
- **Rating**: B+
- **Critical Issues**: TLS, rate limiter, audit logging
- **Fix Effort**: 18 hours
- **Test Focus**: Load testing, compliance

### Switchyard UI Frontend
- **Rating**: F
- **Critical Issues**: No auth, hardcoded tokens, no CSRF
- **Fix Effort**: 88+ hours
- **Test Focus**: Security headers, CSRF, XSS

### Kubernetes Infrastructure
- **Rating**: A-
- **Critical Issues**: None
- **Fix Effort**: 0 hours (nice to have: PDBs, HPA)
- **Test Focus**: Deployment validation

### CLI (Conductor)
- **Rating**: B
- **Issues**: No token refresh, no retry logic
- **Fix Effort**: 10 hours
- **Test Focus**: Connection resilience

---

## Production Deployment Checklist

- [ ] All P0 issues fixed
- [ ] All P1 issues fixed (recommended)
- [ ] TLS configured and tested
- [ ] Rate limiter bounded and tested
- [ ] Audit logs persist and tested
- [ ] UI authentication implemented
- [ ] No hardcoded credentials in code
- [ ] Load tests passed (10,000+ concurrent)
- [ ] OWASP ZAP scan passed
- [ ] Penetration test completed
- [ ] Secrets scanning enabled in CI/CD
- [ ] Security monitoring active
- [ ] Incident response plan documented
- [ ] Backup and recovery tested

---

## Support & Escalation

### Questions About This Audit?
- See full technical report: `SECURITY_AUDIT_COMPREHENSIVE_2025.md`
- See executive summary: `SECURITY_AUDIT_EXECUTIVE_SUMMARY_2025.md`

### Red Flags to Watch For
🚩 Deploying with HTTP (no TLS)
🚩 Hardcoded tokens in UI source
🚩 Audit logs dropping under load
🚩 Rate limiter using 100%+ memory

### Before Deployment
✓ Run automated security tests
✓ Have security review signed off
✓ Document any exceptions
✓ Plan for monitoring & alerts

---

**Last Updated**: November 20, 2025  
**Next Review**: After P0/P1 fixes (estimated: January 2025)  
**Audit Confidence**: 90%+
