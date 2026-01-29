# Infrastructure Audit - Issue Tracker

**Generated:** 2024-11-19  
**Total Issues:** 27 (7 CRITICAL, 12 HIGH, 8 MEDIUM)

---

## CRITICAL ISSUES (7) - PRODUCTION BLOCKED

| # | Issue | File | Line | Severity | Category | Status |
|---|-------|------|------|----------|----------|--------|
| 1 | Hard-coded DB credentials | `postgres.yaml` | 21-27 | CRITICAL | Secrets | NEW |
| 2 | Missing resource limits (PostgreSQL) | `postgres.yaml` | N/A | CRITICAL | Database | NEW |
| 3 | No security context (PostgreSQL) | `postgres.yaml` | N/A | CRITICAL | Database | NEW |
| 4 | Non-persistent storage (PostgreSQL) | `postgres.yaml` | 34-35 | CRITICAL | Storage | NEW |
| 5 | No health checks (PostgreSQL) | `postgres.yaml` | N/A | CRITICAL | Observability | NEW |
| 6 | Overprivileged ClusterRole | `rbac.yaml` | 11-37 | CRITICAL | RBAC | NEW |
| 7 | Missing default-deny NetworkPolicies | `network-policies.yaml` | N/A | CRITICAL | Network | NEW |
| 8 | No TLS/HTTPS | `ingress-nginx.yaml` | 9-20 | CRITICAL | Security | NEW |
| 9 | Hard-coded JWT secrets | `secrets.yaml` | 42-52 | CRITICAL | Secrets | NEW |
| 10 | Exposed Docker registry token | `secrets.yaml` | 62-63 | CRITICAL | Secrets | NEW |
| 11 | No Pod Security Standards | All manifests | N/A | CRITICAL | Security | NEW |
| 12 | Non-persistent Redis storage | `redis.yaml` | 64-65 | CRITICAL | Storage | NEW |
| 13 | Non-persistent Jaeger storage | `monitoring.yaml` | N/A | CRITICAL | Storage | NEW |

---

## HIGH SEVERITY ISSUES (12)

| # | Issue | File | Line | Severity | Category | Status |
|---|-------|------|------|----------|----------|--------|
| 14 | Missing PodDisruptionBudget (API) | `switchyard-api.yaml` | N/A | HIGH | HA | NEW |
| 15 | ImagePullPolicy: Never | `switchyard-api.yaml` | 36-37 | HIGH | Deployment | NEW |
| 16 | Missing startup probe | `switchyard-api.yaml` | N/A | HIGH | Observability | NEW |
| 17 | Non-persistent Redis (base) | `redis.yaml` | 64-65 | HIGH | Storage | NEW |
| 18 | Single Redis replica (base) | `redis.yaml` | 12 | HIGH | HA | NEW |
| 19 | Missing Redis auth | `redis.yaml` | N/A | HIGH | Security | NEW |
| 20 | Deprecated ingress annotation | `ingress-nginx.yaml` | 7 | HIGH | Deployment | NEW |
| 21 | No security headers (ingress) | `ingress-nginx.yaml` | N/A | HIGH | Security | NEW |
| 22 | No rate limiting (ingress) | `ingress-nginx.yaml` | N/A | HIGH | Security | NEW |
| 23 | Jaeger missing security context | `monitoring.yaml` | 43 | HIGH | Security | NEW |
| 24 | Invalid image digest (prod) | `production/kustomization.yaml` | 26 | HIGH | Deployment | NEW |
| 25 | No image pull secrets | All overlays | N/A | HIGH | Deployment | NEW |

---

## MEDIUM SEVERITY ISSUES (8)

| # | Issue | File | Line | Severity | Category | Status |
|---|-------|------|------|----------|----------|--------|
| 26 | Missing PDB (Redis) | `redis.yaml` | N/A | MEDIUM | HA | NEW |
| 27 | Namespace set to default | `base/kustomization.yaml` | 21 | MEDIUM | Deployment | NEW |
| 28 | Missing base ConfigMap | `base/kustomization.yaml` | N/A | MEDIUM | Configuration | NEW |
| 29 | Jaeger no HA (single pod) | `monitoring.yaml` | 32 | MEDIUM | HA | NEW |
| 30 | Jaeger UI unauthenticated | `monitoring.yaml` | 50-51 | MEDIUM | Security | NEW |
| 31 | ServiceMonitor CRD dependency | `monitoring.yaml` | 3 | MEDIUM | Deployment | NEW |
| 32 | Kind cluster missing security flags | `dev/kind-config.yaml` | N/A | MEDIUM | Development | NEW |
| 33 | Namespace missing Pod Security labels | `dev/namespace.yaml` | N/A | MEDIUM | Security | NEW |
| 34 | Multiple DB URL copies | `secrets.yaml` | 17, 74 | MEDIUM | Secrets | NEW |

---

## ISSUE DETAILS

### CRITICAL - 1: Hard-Coded Database Credentials
**File:** `/home/user/enclii/infra/k8s/base/postgres.yaml`  
**Lines:** 21-27  
**Severity:** CRITICAL  
**Category:** Secrets Management  
**Status:** NEW  

**Description:**
Database credentials hardcoded in plain text in Git repository.
```yaml
- name: POSTGRES_PASSWORD
  value: "password"
```

**Impact:**
- Credentials exposed in Git history permanently
- Anyone with repo access has database credentials
- Cannot safely revoke credentials

**Remediation:**
1. Implement external secret management (Sealed Secrets or Vault)
2. Remove secrets.yaml from Git history using `git filter-branch`
3. Rotate all database credentials immediately

**Effort:** 2 days  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 2: PostgreSQL Missing Resource Limits
**File:** `/home/user/enclii/infra/k8s/base/postgres.yaml`  
**Lines:** N/A  
**Severity:** CRITICAL  
**Category:** Resource Management  
**Status:** NEW  

**Description:**
No CPU or memory requests/limits defined for PostgreSQL container.

**Impact:**
- PostgreSQL can consume all node resources
- Starves other pods
- Cluster-wide instability
- Node OOMKill events

**Remediation:**
```yaml
resources:
  requests:
    memory: "1Gi"
    cpu: "500m"
  limits:
    memory: "2Gi"
    cpu: "1000m"
```

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 3: PostgreSQL Missing Security Context
**File:** `/home/user/enclii/infra/k8s/base/postgres.yaml`  
**Lines:** N/A  
**Severity:** CRITICAL  
**Category:** Pod Security  
**Status:** NEW  

**Description:**
Container has no security context, may run as root.

**Impact:**
- Privilege escalation risks
- Violates Pod Security Standards
- Compliance violations

**Remediation:**
Add security context with non-root user (UID 999), drop all capabilities.

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 4: PostgreSQL Non-Persistent Storage
**File:** `/home/user/enclii/infra/k8s/base/postgres.yaml`  
**Lines:** 34-35  
**Severity:** CRITICAL  
**Category:** Data Persistence  
**Status:** NEW  

**Description:**
Using emptyDir for database storage - data lost on pod restart.

**Impact:**
- Complete data loss on pod restart
- No disaster recovery
- Application data integrity broken

**Remediation:**
Implement PersistentVolumeClaim with 20Gi storage.

**Effort:** 2 days  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 5: PostgreSQL No Health Checks
**File:** `/home/user/enclii/infra/k8s/base/postgres.yaml`  
**Lines:** N/A  
**Severity:** CRITICAL  
**Category:** Observability  
**Status:** NEW  

**Description:**
Missing readiness and liveness probes.

**Impact:**
- Kubernetes doesn't know when DB is ready
- Pod failures not detected
- Cascading failures

**Remediation:**
Add probes using `pg_isready` command.

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 6: Overprivileged RBAC
**File:** `/home/user/enclii/infra/k8s/base/rbac.yaml`  
**Lines:** 11-37  
**Severity:** CRITICAL  
**Category:** RBAC & Security  
**Status:** NEW  

**Description:**
ClusterRole with broad permissions across all namespaces including delete verbs.

**Impact:**
- Compromised pod can delete resources anywhere
- Can access secrets in any namespace
- Violates least privilege principle

**Remediation:**
1. Change ClusterRole to Role (namespace-scoped)
2. Remove delete verbs
3. Add resourceNames for sensitive resources
4. Restrict to only needed resources

**Effort:** 2 days  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 7: Missing Default-Deny NetworkPolicies
**File:** `/home/user/enclii/infra/k8s/base/network-policies.yaml`  
**Lines:** N/A  
**Severity:** CRITICAL  
**Category:** Network Security  
**Status:** NEW  

**Description:**
No default deny policies; all pods can communicate with all pods.

**Impact:**
- Lateral movement not prevented
- No network segmentation
- Violates Zero Trust principle

**Remediation:**
Add namespace-level default deny policies for ingress and egress.

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 8: Unrestricted Ingress (No TLS)
**File:** `/home/user/enclii/infra/k8s/base/ingress-nginx.yaml`  
**Lines:** 9-20  
**Severity:** CRITICAL  
**Category:** Security  
**Status:** NEW  

**Description:**
No HTTPS/TLS configured; all traffic in HTTP plaintext.

**Impact:**
- Credentials exposed in transit
- Man-in-the-middle attacks possible
- Non-compliance with security standards

**Remediation:**
1. Add TLS section with certificate
2. Implement cert-manager with Let's Encrypt
3. Add security headers

**Effort:** 2 days  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 9: Hard-Coded JWT Secrets
**File:** `/home/user/enclii/infra/k8s/base/secrets.yaml`  
**Lines:** 42-52  
**Severity:** CRITICAL  
**Category:** Secrets Management  
**Status:** NEW  

**Description:**
JWT secret is placeholder string, not actual RSA keys.

**Impact:**
- Any attacker can forge JWT tokens
- Authentication bypass possible
- "dev-" prefix in production

**Remediation:**
Generate proper RSA keys and use Sealed Secrets.

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 10: Exposed Docker Registry Token
**File:** `/home/user/enclii/infra/k8s/base/secrets.yaml`  
**Lines:** 62-63  
**Severity:** CRITICAL  
**Category:** Secrets Management  
**Status:** NEW  

**Description:**
Base64-encoded Docker registry credentials exposed in Git.

**Impact:**
- Registry access token compromised
- Can pull/push private images
- Requires immediate token rotation

**Remediation:**
1. Revoke exposed token in GitHub
2. Use Sealed Secrets for credentials
3. Implement credential scanning

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 11: No Pod Security Standards Enforcement
**File:** All manifests  
**Lines:** N/A  
**Severity:** CRITICAL  
**Category:** Pod Security  
**Status:** NEW  

**Description:**
No Pod Security Standards labels or policies enforced.

**Impact:**
- Privileged containers could be deployed
- No admission control
- Compliance violations

**Remediation:**
Add pod-security.kubernetes.io labels to namespaces.

**Effort:** 1 day  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 12: Non-Persistent Redis Storage
**File:** `/home/user/enclii/infra/k8s/base/redis.yaml`  
**Lines:** 64-65  
**Severity:** CRITICAL  
**Category:** Data Persistence  
**Status:** NEW  

**Description:**
Using emptyDir for Redis cache - data lost on pod restart.

**Impact:**
- Cache data lost on restart
- Session data loss
- Poor user experience

**Remediation:**
Implement PersistentVolumeClaim for Redis data.

**Effort:** 2 days  
**Priority:** P0 (Blocking)  

---

### CRITICAL - 13: Non-Persistent Jaeger Storage
**File:** `/home/user/enclii/infra/k8s/base/monitoring.yaml`  
**Lines:** N/A  
**Severity:** CRITICAL  
**Category:** Observability  
**Status:** NEW  

**Description:**
Jaeger traces stored in memory; lost on pod restart.

**Impact:**
- Trace data lost
- No historical debugging capability
- Critical for incident investigation

**Remediation:**
Add persistent storage for Jaeger traces.

**Effort:** 2 days  
**Priority:** P0 (Blocking)  

---

## Remediation Timeline

### Phase 1 (Days 1-5): CRITICAL BLOCKING ISSUES
- [ ] Implement external secret management
- [ ] Remove secrets from Git history
- [ ] Add PostgreSQL security context & resource limits
- [ ] Add PostgreSQL persistent storage
- [ ] Implement default-deny NetworkPolicies
- [ ] Fix RBAC to use Role + restrict permissions
- [ ] Add TLS to ingress

**Effort:** 5 days (1 person)

### Phase 2 (Days 6-10): HIGH PRIORITY ISSUES
- [ ] Implement PostgreSQL HA
- [ ] Add Pod Disruption Budgets
- [ ] Fix image pull policy
- [ ] Add image pull secrets
- [ ] Implement backup/restore
- [ ] Add Jaeger security context
- [ ] Fix invalid image digest

**Effort:** 5 days (1 person)

### Phase 3 (Days 11-15): MEDIUM & GA READINESS
- [ ] Implement admission control (Kyverno)
- [ ] Add monitoring & alerting
- [ ] Document disaster recovery
- [ ] Configure autoscaling
- [ ] Security scanning

**Effort:** 5 days (1 person)

### Validation (Days 16-17)
- [ ] CIS Kubernetes Benchmark scan
- [ ] Penetration testing
- [ ] Compliance validation

**Effort:** 2 days (1 person)

**Total:** 16-17 days for production readiness

---

## Appendix: Quick Fix Commands

### Remove secrets from Git history
```bash
git filter-branch --tree-filter 'rm -f infra/k8s/base/secrets.yaml' HEAD
```

### Generate RSA keys for JWT
```bash
openssl genrsa -out private-key.pem 4096
openssl rsa -in private-key.pem -pubout -out public-key.pem
```

### Check Kubernetes best practices
```bash
kubesec scan infra/k8s/production/
```

### Scan container images for vulnerabilities
```bash
trivy image ghcr.io/madfam/switchyard-api:latest
```

### Run CIS Benchmark
```bash
kube-bench run --targets=node,policies > cis-benchmark.txt
```

