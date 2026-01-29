# Enclii Infrastructure Configuration Audit Report

**Date:** 2024-11-19  
**Scope:** /infra/ directory (923 lines of YAML configuration)  
**Audit Status:** CRITICAL ISSUES FOUND

---

## Executive Summary

The infrastructure configuration contains several **CRITICAL security and production-readiness issues** that must be addressed before production deployment. The main concerns are:

1. **Secrets Management**: Hard-coded credentials and secrets in Git repository
2. **Database Configuration**: Single-replica database with non-persistent storage
3. **RBAC Overprivileging**: Broad cluster-level permissions without proper scoping
4. **Missing HA/DR**: No high availability, backup, or disaster recovery configuration
5. **Security Gaps**: Missing Pod Security Standards, admission policies, and image verification

---

## 1. KUBERNETES MANIFESTS REVIEW

### 1.1 Switchyard API Deployment

**File:** `/home/user/enclii/infra/k8s/base/switchyard-api.yaml`

#### Issue 1.1.1 - Image Pull Policy for Production
**Line:** 36-37  
**Severity:** HIGH  
**Issue:** `imagePullPolicy: Never` is set with comment "For local development"
```yaml
image: switchyard-api:latest
imagePullPolicy: Never # For local development
```
**Impact:** This will cause pod failures in any real cluster where the image is not pre-loaded. Production deployments cannot function.

**Recommended Fix:**
- Use `imagePullPolicy: IfNotPresent` (default) for staging/production
- Implement in environment-specific patches (staging/production)
- Remove the comment and set appropriate image registry

#### Issue 1.1.2 - Missing Pod Disruption Budget
**Line:** N/A (Missing)  
**Severity:** MEDIUM  
**Issue:** No PodDisruptionBudget defined for switchyard-api

**Impact:** During node maintenance or pod evictions, all replicas could be removed simultaneously, causing service downtime.

**Recommended Fix:**
```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: switchyard-api-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: switchyard-api
```

#### Issue 1.1.3 - Missing Resource Limits for Base
**Line:** 69-75  
**Severity:** MEDIUM (though requests/limits are defined)  
**Issue:** Development requests (100m CPU, 128Mi RAM) may be too low for production workloads

**Impact:** Services may be throttled or OOMKilled under load.

**Recommended Fix:** Already properly configured in production patch, but consider:
- Test resource allocation under realistic load
- Implement HPA (Horizontal Pod Autoscaling) for dynamic scaling

#### Issue 1.1.4 - Missing Startup Probe
**Line:** 76-95  
**Severity:** LOW  
**Issue:** No startup probe for slow-starting applications

**Impact:** If the application takes longer than 30 seconds to start (readiness probe initialDelaySeconds), it could be killed prematurely.

**Recommended Fix:**
```yaml
startupProbe:
  httpGet:
    path: /health/ready
    port: 8080
  failureThreshold: 30
  periodSeconds: 10
```

---

### 1.2 PostgreSQL Deployment

**File:** `/home/user/enclii/infra/k8s/base/postgres.yaml`

#### Issue 1.2.1 - CRITICAL: Hard-Coded Credentials in Manifest
**Line:** 21-27  
**Severity:** CRITICAL  
**Issue:** Database credentials exposed in plain text in Git repository
```yaml
env:
  - name: POSTGRES_DB
    value: "enclii_dev"
  - name: POSTGRES_USER
    value: "postgres"
  - name: POSTGRES_PASSWORD
    value: "password"
```
**Impact:** 
- Anyone with repository access has production database credentials
- Violates security standards (CIS Benchmarks, SOC 2, ISO 27001)
- Exposed to Git history forever

**Recommended Fix:**
- Remove from manifest immediately
- Use Kubernetes Secrets instead
- Implement secret management tool (Sealed Secrets, Vault, External Secrets Operator)
- Rotate all database credentials immediately
- Add secrets.yaml to .gitignore

#### Issue 1.2.2 - CRITICAL: Missing Resource Limits
**Line:** N/A  
**Severity:** CRITICAL  
**Issue:** No CPU or memory requests/limits defined
```yaml
containers:
  - name: postgres
    image: postgres:15
    # No resources: section
```
**Impact:**
- PostgreSQL can consume unlimited node resources
- Starves other pods, causing cluster-wide instability
- No guaranteed minimum resources
- Can cause node OOMKill events

**Recommended Fix:**
```yaml
resources:
  requests:
    memory: "1Gi"
    cpu: "500m"
  limits:
    memory: "2Gi"
    cpu: "1000m"
```

#### Issue 1.2.3 - CRITICAL: No Security Context
**Line:** N/A  
**Severity:** CRITICAL  
**Issue:** Container runs with default privileges
```yaml
spec:
  containers:
    - name: postgres
      # No securityContext defined
```
**Impact:**
- Container could run as root
- Privilege escalation risks
- Violates Pod Security Standards

**Recommended Fix:**
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 999
  runAsGroup: 999
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: false
  capabilities:
    drop:
      - ALL
  fsGroup: 999
```

#### Issue 1.2.4 - CRITICAL: Non-Persistent Storage (Data Loss Risk)
**Line:** 34-35  
**Severity:** CRITICAL  
**Issue:** Using emptyDir volume for database storage
```yaml
volumes:
  - name: postgres-storage
    emptyDir: {}
```
**Impact:**
- All data lost when pod restarts
- No disaster recovery possible
- Breaks application data integrity

**Recommended Fix:**
```yaml
# Option 1: Use PersistentVolumeClaim
volumeMounts:
  - name: postgres-storage
    mountPath: /var/lib/postgresql/data
    subPath: postgres
volumes:
  - name: postgres-storage
    persistentVolumeClaim:
      claimName: postgres-pvc

# Then create:
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: postgres-pvc
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi
```

#### Issue 1.2.5 - CRITICAL: No Health Checks
**Line:** N/A  
**Severity:** CRITICAL  
**Issue:** Missing readiness and liveness probes
```yaml
containers:
  - name: postgres
    # No readinessProbe or livenessProbe
```
**Impact:**
- Kubernetes doesn't know when database is ready
- Failures not detected, causing cascading failures
- Invalid connections to unhealthy database

**Recommended Fix:**
```yaml
readinessProbe:
  exec:
    command:
      - /bin/sh
      - -c
      - pg_isready -U postgres
  initialDelaySeconds: 10
  periodSeconds: 5
  timeoutSeconds: 5
  failureThreshold: 3

livenessProbe:
  exec:
    command:
      - /bin/sh
      - -c
      - pg_isready -U postgres
  initialDelaySeconds: 30
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

#### Issue 1.2.6 - Single Replica (No HA)
**Line:** 9  
**Severity:** HIGH  
**Issue:** Production database with only 1 replica
```yaml
replicas: 1
```
**Impact:**
- Any pod failure or maintenance causes complete outage
- No failover capability
- RTO = Pod restart time (2-5 minutes)

**Recommended Fix:**
- Deploy PostgreSQL using StatefulSet with streaming replication
- Minimum 3 replicas for production
- Implement backup/standby database
- Use managed database service (AWS RDS, CloudSQL, Azure Database) for production

#### Issue 1.2.7 - Outdated Base Image
**Line:** 20  
**Severity:** MEDIUM  
**Issue:** Using generic `postgres:15` without specific patch version
```yaml
image: postgres:15
```
**Impact:**
- Unpredictable image pull behavior
- Could pull vulnerable version
- No reproducibility across clusters

**Recommended Fix:**
```yaml
image: postgres:15.4-alpine  # Specify exact version and use alpine for smaller footprint
```

---

### 1.3 Redis Deployment

**File:** `/home/user/enclii/infra/k8s/base/redis.yaml`

#### Issue 1.3.1 - Non-Persistent Storage
**Line:** 64-65  
**Severity:** HIGH  
**Issue:** Using emptyDir for Redis cache
```yaml
volumes:
  - name: redis-data
    emptyDir: {}
```
**Impact:**
- Cache data lost on pod restart
- Loss of session data and cached API responses
- Poor user experience on pod restarts

**Recommended Fix:**
```yaml
# Option 1: For HA, use StatefulSet with persistent volume
volumeMounts:
  - name: redis-data
    mountPath: /data
volumeClaimTemplates:  # Only works with StatefulSet
  - metadata:
      name: redis-data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 5Gi

# Option 2: Add Redis persistence configuration
command: 
  - redis-server
  - "--appendonly"
  - "yes"
  - "--appendfsync"
  - "everysec"
```

#### Issue 1.3.2 - Missing Redis Authentication
**Line:** N/A  
**Severity:** MEDIUM  
**Issue:** No password protection for Redis
```yaml
# No AUTH configured, anyone with network access can read/modify data
```
**Impact:**
- Unauthorized access to session data
- Data tampering risk
- Cache poisoning attacks

**Recommended Fix:**
```yaml
command:
  - redis-server
  - "--requirepass"
  - "$(REDIS_PASSWORD)"
  
env:
  - name: REDIS_PASSWORD
    valueFrom:
      secretKeyRef:
        name: redis-credentials
        key: password
```

#### Issue 1.3.3 - Single Replica in Base
**Line:** 12  
**Severity:** MEDIUM  
**Issue:** Although staging/production have more replicas, base is still 1
```yaml
replicas: 1
```
**Impact:**
- Even in staging, single point of failure
- Cache loss on pod restart

**Recommended Fix:** Consider using Redis Sentinel or Redis Cluster for HA cache

---

### 1.4 Ingress Configuration

**File:** `/home/user/enclii/infra/k8s/base/ingress-nginx.yaml`

#### Issue 1.4.1 - Deprecated Annotation
**Line:** 7  
**Severity:** MEDIUM  
**Issue:** Using deprecated ingress class annotation
```yaml
annotations:
  kubernetes.io/ingress.class: "nginx"
```
**Impact:**
- Will be removed in Kubernetes 1.30+
- Not compatible with newer Kubernetes versions

**Recommended Fix:**
```yaml
spec:
  ingressClassName: nginx
```

#### Issue 1.4.2 - Missing TLS Configuration
**Line:** 9-20  
**Severity:** HIGH  
**Issue:** No HTTPS/TLS configured
```yaml
spec:
  rules:
    - host: api.enclii.local
      http:
        paths:
          - path: /
            # No TLS section
```
**Impact:**
- All traffic in cleartext (HTTP)
- Credentials and API tokens exposed in transit
- Man-in-the-middle attacks possible
- Non-compliance with security standards

**Recommended Fix:**
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: enclii-ingress
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - api.enclii.local
      secretName: api-enclii-tls
  rules:
    - host: api.enclii.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: switchyard-api
                port:
                  number: 8080
```

#### Issue 1.4.3 - Missing Security Headers
**Line:** N/A  
**Severity:** MEDIUM  
**Issue:** No CORS, CSP, or security headers configured
```yaml
# Missing nginx annotations for security headers
```
**Impact:**
- XSS attacks possible
- CSRF vulnerabilities
- Clickjacking risks

**Recommended Fix:**
```yaml
annotations:
  nginx.ingress.kubernetes.io/enable-cors: "true"
  nginx.ingress.kubernetes.io/cors-allow-origin: "https://ui.enclii.local"
  nginx.ingress.kubernetes.io/add-base-url: "true"
  nginx.ingress.kubernetes.io/configuration-snippet: |
    more_set_headers "X-Frame-Options: DENY";
    more_set_headers "X-Content-Type-Options: nosniff";
    more_set_headers "X-XSS-Protection: 1; mode=block";
    more_set_headers "Referrer-Policy: strict-origin-when-cross-origin";
```

#### Issue 1.4.4 - Missing Rate Limiting
**Line:** N/A  
**Severity:** MEDIUM  
**Issue:** No rate limiting at ingress level
```yaml
# Missing rate-limit annotations
```
**Impact:**
- Vulnerability to DDoS attacks
- API abuse not prevented
- Service disruption risk

**Recommended Fix:**
```yaml
annotations:
  nginx.ingress.kubernetes.io/limit-rps: "100"
  nginx.ingress.kubernetes.io/limit-connections: "10"
```

---

### 1.5 RBAC Configuration

**File:** `/home/user/enclii/infra/k8s/base/rbac.yaml`

#### Issue 1.5.1 - CRITICAL: Overprivileged ClusterRole
**Line:** 11-37  
**Severity:** CRITICAL  
**Issue:** ClusterRole has broad permissions across all namespaces
```yaml
rules:
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["create", "get", "list", "watch"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets"]
  verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["create", "get", "list", "watch", "update", "patch"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses"]
  verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
```
**Impact:**
- Compromised pod can create/modify deployments in any namespace
- Can access secrets in any namespace
- Can create resources in other projects' namespaces
- Delete permissions on critical resources
- Violates principle of least privilege

**Recommended Fix:**
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role  # Use Role instead of ClusterRole
metadata:
  name: switchyard-api
  namespace: default
rules:
- apiGroups: [""]
  resources: ["services"]
  verbs: ["create", "get", "list", "watch", "update", "patch"]
  # Remove delete
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch"]  # Read-only for other app deployments
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create", "get", "list", "watch"]
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list"]  # Only what's needed
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses"]
  verbs: ["create", "get", "list", "watch", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: switchyard-api
  namespace: default
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: switchyard-api
subjects:
- kind: ServiceAccount
  name: switchyard-api
  namespace: default
```

#### Issue 1.5.2 - Missing Resource Name Restrictions
**Line:** 19-37  
**Severity:** MEDIUM  
**Issue:** No resourceNames restriction on sensitive resources
```yaml
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["create", "get", "list", "watch", "update", "patch"]
  # Missing resourceNames: ["specific-secret-name"]
```
**Impact:**
- Service can access ALL secrets and configmaps
- Credentials for other services exposed
- Data leakage risk

**Recommended Fix:**
```yaml
- apiGroups: [""]
  resources: ["secrets"]
  resourceNames: 
    - postgres-credentials
    - jwt-secrets
  verbs: ["get"]
```

---

### 1.6 Network Policies

**File:** `/home/user/enclii/infra/k8s/base/network-policies.yaml`

#### Issue 1.6.1 - CRITICAL: Missing Default Deny Policies
**Line:** N/A  
**Severity:** CRITICAL  
**Issue:** No default deny ingress/egress policies
```yaml
# No NetworkPolicy with empty selectors to deny all by default
```
**Impact:**
- All pods can communicate with all pods
- Lateral movement in case of compromise
- No network segmentation
- Violates Zero Trust network principle

**Recommended Fix:**
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all-ingress
spec:
  podSelector: {}
  policyTypes:
  - Ingress
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all-egress
spec:
  podSelector: {}
  policyTypes:
  - Egress
  egress:
  - to:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 53
    - protocol: UDP
      port: 53
```

#### Issue 1.6.2 - Unrestricted DNS Egress
**Line:** 18-22  
**Severity:** MEDIUM  
**Issue:** DNS egress allows to any destination
```yaml
- to: []  # Empty selector = all destinations
  ports:
  - protocol: UDP
    port: 53
```
**Impact:**
- DNS exfiltration attacks possible
- Data leakage through DNS tunneling
- Malware C2 communication possible

**Recommended Fix:**
```yaml
- to:
  - namespaceSelector:
      matchLabels:
        name: kube-system
  ports:
  - protocol: UDP
    port: 53
```

#### Issue 1.6.3 - Overly Broad Kubernetes API Access
**Line:** 47-53  
**Severity:** HIGH  
**Issue:** Kubernetes API access without namespace selector
```yaml
- to: []  # Empty selector = all destinations
  ports:
  - protocol: TCP
    port: 443
  - protocol: TCP
    port: 6443
```
**Impact:**
- Can access Kubernetes API without restrictions
- Could query other namespaces
- Service account token compromise risk

**Recommended Fix:**
```yaml
# Option 1: If reconciler needs K8s API
- to:
  - namespaceSelector: {}
    podSelector:
      matchLabels:
        component: kube-apiserver
  ports:
  - protocol: TCP
    port: 443

# Option 2: Better - use service account token to access only cluster-scoped APIs
# No egress rule needed, use RBAC instead
```

#### Issue 1.6.4 - Missing Postgres Egress Restrictions
**Line:** 92-113  
**Severity:** MEDIUM  
**Issue:** postgres-ingress policy defined but no default deny for postgres namespace
```yaml
# Allows traffic TO postgres from switchyard-api
# But doesn't prevent postgres FROM making outbound connections
```
**Impact:**
- Compromised database could connect to external services
- Data exfiltration possible
- Malware propagation risk

**Recommended Fix:**
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: postgres-default-deny-egress
spec:
  podSelector:
    matchLabels:
      app: postgres
  policyTypes:
  - Egress
  egress: []  # Deny all egress
```

#### Issue 1.6.5 - Missing Redis Namespace Selector
**Line:** 132-137  
**Severity:** LOW  
**Issue:** redis-ingress uses podSelector but pod might migrate to different namespace
```yaml
- from:
  - podSelector:
      matchLabels:
        app: switchyard-api
  # Missing namespaceSelector
```
**Recommended Fix:**
```yaml
- from:
  - namespaceSelector:
      matchLabels:
        name: default
    podSelector:
      matchLabels:
        app: switchyard-api
```

---

### 1.7 Monitoring Configuration

**File:** `/home/user/enclii/infra/k8s/base/monitoring.yaml`

#### Issue 1.7.1 - Jaeger Missing Security Context
**Line:** 43  
**Severity:** HIGH  
**Issue:** Jaeger container lacks security context
```yaml
spec:
  containers:
    - name: jaeger
      image: jaegertracing/all-in-one:1.48
      # No securityContext defined
```
**Impact:**
- Jaeger could run as root
- Access to trace data (sensitive information) without protection
- Violates Pod Security Standards

**Recommended Fix:**
```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  runAsGroup: 1000
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: false
  capabilities:
    drop:
      - ALL
```

#### Issue 1.7.2 - Jaeger All-in-One (No HA)
**Line:** 46  
**Severity:** MEDIUM  
**Issue:** Using single-instance Jaeger deployment
```yaml
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: jaeger
          image: jaegertracing/all-in-one:1.48
```
**Impact:**
- No distributed tracing redundancy
- Trace data loss if pod fails
- Single point of failure for observability

**Recommended Fix:** Consider Jaeger Operator or distributed deployment

#### Issue 1.7.3 - Non-Persistent Jaeger Storage
**Line:** N/A  
**Severity:** MEDIUM  
**Issue:** No persistent storage for traces
```yaml
# Jaeger all-in-one stores in memory/temporary storage
# No emptyDir, volume mounting, or backend storage configured
```
**Impact:**
- Trace data lost on pod restart
- No historical trace data for debugging
- Critical for incident investigation

**Recommended Fix:**
```yaml
# Option 1: Use emptyDir for better visibility (compared to none)
volumeMounts:
  - name: jaeger-data
    mountPath: /badger
volumes:
  - name: jaeger-data
    emptyDir: {}

# Option 2: Use persistent volume
volumeMounts:
  - name: jaeger-data
    mountPath: /badger
volumes:
  - name: jaeger-data
    persistentVolumeClaim:
      claimName: jaeger-pvc

# Option 3: Use Jaeger with Elasticsearch backend for production
```

#### Issue 1.7.4 - Missing Jaeger UI Access Control
**Line:** 50-51  
**Severity:** MEDIUM  
**Issue:** Jaeger UI (port 16686) exposed without authentication
```yaml
ports:
  - containerPort: 16686
    name: ui
# No authentication, TLS, or ingress restrictions
```
**Impact:**
- Traces contain sensitive application data (PII, API calls, errors)
- Unauthenticated access to trace data
- Information disclosure vulnerability

**Recommended Fix:**
- Add authentication via ingress
- Restrict access via NetworkPolicy
- Use service account authentication if needed

#### Issue 1.7.5 - ServiceMonitor CRD Dependency
**Line:** 3  
**Severity:** MEDIUM  
**Issue:** Uses `monitoring.coreos.com/v1` ServiceMonitor without checking if Prometheus Operator is installed
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
```
**Impact:**
- Will fail if Prometheus Operator not installed
- No fallback to manual ServiceMonitor discovery
- Breaks deployment on clusters without Prometheus Operator

**Recommended Fix:**
- Document Prometheus Operator dependency
- Provide fallback Prometheus configuration
- Or use Prometheus scrape config instead

---

### 1.8 Secrets Configuration

**File:** `/home/user/enclii/infra/k8s/base/secrets.yaml`

#### Issue 1.8.1 - CRITICAL: Hard-Coded Credentials in Git
**Line:** 13-17  
**Severity:** CRITICAL  
**Issue:** PostgreSQL credentials in plain text YAML
```yaml
stringData:
  username: postgres
  password: password
  database: enclii_dev
  database-url: "postgres://postgres:password@postgres:5432/enclii_dev?sslmode=disable"
```
**Impact:**
- Violates fundamental security principle
- Credentials in Git history forever
- Exposed to anyone with repository access
- Audit trail shows who accessed credentials
- Cannot be safely revoked

**Recommended Fix:**
1. Immediately rotate all database credentials
2. Remove secrets.yaml from Git history (git-filter-branch or similar)
3. Use external secret management:
   - Sealed Secrets (simple, built-in to K8s)
   - HashiCorp Vault (enterprise)
   - AWS Secrets Manager / Azure Key Vault / GCP Secret Manager
   - External Secrets Operator (generic solution)

4. Example with Sealed Secrets:
```yaml
apiVersion: bitnami.com/v1alpha1
kind: SealedSecret
metadata:
  name: postgres-credentials
spec:
  encryptedData:
    password: AgBmL3k...  # Encrypted with seal key
```

#### Issue 1.8.2 - CRITICAL: JWT Secrets Placeholder
**Line:** 42-52  
**Severity:** CRITICAL  
**Issue:** JWT secret is placeholder, not actual RSA keys
```yaml
stringData:
  jwt-secret: "dev-jwt-secret-key-change-in-production-use-rsa-keys"
  private-key: |
    -----BEGIN RSA PRIVATE KEY-----
    # Development only - use proper RSA keys in production
    -----END RSA PRIVATE KEY-----
```
**Impact:**
- Placeholder indicates key is NOT secure
- Any attacker can forge JWT tokens
- Authentication bypass possible
- "dev-" prefix should not exist in production

**Recommended Fix:**
```bash
# Generate proper RSA keys
openssl genrsa -out private-key.pem 4096
openssl rsa -in private-key.pem -pubout -out public-key.pem

# Create sealed secret
kubectl create secret generic jwt-secrets \
  --from-file=private-key=private-key.pem \
  --from-file=public-key=public-key.pem \
  --dry-run=client -o yaml | kubeseal -o yaml > jwt-secret-sealed.yaml
```

#### Issue 1.8.3 - CRITICAL: Docker Registry Credentials Exposed
**Line:** 62-63  
**Severity:** CRITICAL  
**Issue:** Base64-encoded credentials exposed in Git
```yaml
data:
  .dockerconfigjson: eyJhdXRocyI6eyJnaGNyLmlvIjp7InVzZXJuYW1lIjoiZ2l0aHViLXRva2VuIiwicGFzc3dvcmQiOiJnaXRodWJfcGF0X3Rva2VuIn19fQ==
```
**Decoded:**
```json
{"auths":{"ghcr.io":{"username":"github-token","password":"github_pat_token"}}}
```
**Impact:**
- Registry access token exposed
- Can pull/push private images
- Token can be revoked, but damage already done
- Requires rotation of all access tokens

**Recommended Fix:**
- Immediately revoke exposed token in GitHub
- Use Sealed Secrets for registry credentials
- Use short-lived tokens or service accounts
- Implement credential scanning in CI/CD

#### Issue 1.8.4 - CRITICAL: Database URL Exposed Multiple Places
**Line:** 17, 74  
**Severity:** CRITICAL  
**Issue:** Database URL duplicated in multiple secrets with credentials
```yaml
# In postgres-credentials secret:
database-url: "postgres://postgres:password@postgres:5432/enclii_dev?sslmode=disable"

# In switchyard-secret:
database-url: cG9zdGdyZXM6Ly9wb3N0Z3JlczpwYXNzd29yZEBwb3N0Z3JlczozNjMzL2VuY2xpaV9kZXY/c3NsbW9kZT1kaXNhYmxl
```
**Impact:**
- Credential repetition increases exposure surface
- Makes credential rotation harder (multiple places)
- Inconsistency risk (one rotated, one not)

**Recommended Fix:**
- Use single source of truth for credentials
- Reference secret from another secret if needed
- Implement secret versioning and rotation

#### Issue 1.8.5 - Missing TLS Certificate Secret
**Line:** 88-92  
**Severity:** HIGH  
**Issue:** TLS certificate is dummy/base64 placeholder
```yaml
data:
  tls.crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t  # Just base64("-----BEGIN CERTIFICATE-----")
  tls.key: LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0t  # Just base64("-----BEGIN PRIVATE KEY-----")
```
**Impact:**
- Not actual TLS certificate
- HTTPS/TLS will fail with certificate validation errors
- Indicates TLS not properly configured

**Recommended Fix:**
- Use cert-manager to auto-provision certificates
- Use Let's Encrypt for production

#### Issue 1.8.6 - Missing Secret Management Strategy
**Line:** N/A  
**Severity:** CRITICAL  
**Issue:** No external secret management integrated
```yaml
# All secrets hardcoded in Git repository
```
**Impact:**
- Violates security best practices
- Cannot rotate secrets without code changes
- Audit trail compromised
- Compliance violations (SOC 2, ISO 27001, PCI-DSS)

**Recommended Fix:**
Implement one of:
1. **Sealed Secrets** (simplest, no external dependencies)
2. **HashiCorp Vault** (enterprise, multi-cluster support)
3. **AWS Secrets Manager + External Secrets Operator**
4. **Azure Key Vault + External Secrets Operator**
5. **GCP Secret Manager + External Secrets Operator**

---

## 2. KUSTOMIZE STRUCTURE

**Files:**
- `/home/user/enclii/infra/k8s/base/kustomization.yaml`
- `/home/user/enclii/infra/k8s/staging/kustomization.yaml`
- `/home/user/enclii/infra/k8s/production/kustomization.yaml`

### Issue 2.1 - Base Namespace Set to Default
**Line:** 21 (base/kustomization.yaml)  
**Severity:** MEDIUM  
**Issue:** Kustomization sets namespace to "default"
```yaml
namespace: default
```
**Impact:**
- All resources deploy to default namespace
- Could conflict with other applications
- Makes multi-tenancy difficult
- Not recommended for production

**Recommended Fix:**
```yaml
# Remove namespace from base
# Set in environment-specific overlays
# base/kustomization.yaml: (remove namespace)
# staging/kustomization.yaml:
namespace: enclii-staging
# production/kustomization.yaml:
namespace: enclii-production
```

### Issue 2.2 - ConfigMap Missing from Base
**Line:** N/A  
**Severity:** MEDIUM  
**Issue:** No ConfigMap for base configuration (only environment-specific)
```yaml
# staging/kustomization.yaml has configMapGenerator
configMapGenerator:
  - name: env-config
# But base has no defaults
```
**Impact:**
- Harder to understand base defaults
- Duplication in staging/production
- Inconsistent structure

**Recommended Fix:**
Add base ConfigMap:
```yaml
# base/kustomization.yaml
configMapGenerator:
  - name: app-config
    literals:
      - APP_NAME=enclii
      - API_PORT=8080

# Then overlays can merge/override
```

### Issue 2.3 - Image Tag Mismatch in Production
**Line:** 26 (production/kustomization.yaml)  
**Severity:** HIGH  
**Issue:** Using digest without verification
```yaml
images:
  - name: switchyard-api
    digest: sha256:abcdef123456
```
**Impact:**
- Digest placeholder "abcdef123456" is not a real hash
- Image might not exist
- Deployment will fail

**Recommended Fix:**
```bash
# Get actual digest from registry
docker inspect ghcr.io/madfam/switchyard-api:v1.0.0

# Update with real digest
images:
  - name: switchyard-api
    newName: ghcr.io/madfam/switchyard-api
    digest: sha256:40e1b09b9328ec9e4ab45ef3e56e4a1c...
```

### Issue 2.4 - Missing Image Pull Secrets in Overlays
**Line:** N/A  
**Severity:** HIGH  
**Issue:** No imagePullSecrets defined anywhere
```yaml
# No reference to registry-secret
```
**Impact:**
- Cannot pull private images from registry
- Public registry only, no security

**Recommended Fix:**
```yaml
# staging/kustomization.yaml and production/kustomization.yaml
patches:
  - target:
      kind: Deployment
    patch: |-
      - op: add
        path: /spec/template/spec/imagePullSecrets
        value:
          - name: registry-secret
```

---

## 3. SECURITY ANALYSIS

### 3.1 Privileged Container Check
**Status:** PASS  
**Finding:** No privileged containers found in configuration
```yaml
# switchyard-api, redis, jaeger all properly restrict privileges
securityContext:
  allowPrivilegeEscalation: false
  runAsNonRoot: true
```

### 3.2 Service Account Permissions

**Status:** CRITICAL ISSUES  
See Issue 1.5.1 and 1.5.2

### 3.3 Network Isolation

**Status:** CRITICAL ISSUES  
See Issues 1.6.1 - 1.6.5

#### Summary:
- Missing default-deny policies
- Unrestricted DNS egress
- Overly broad Kubernetes API access
- No namespace isolation

### 3.4 Secret Exposure Check

**Status:** CRITICAL ISSUES  
See Issues 1.8.1 - 1.8.6

#### Summary:
- Secrets in Git repository
- Hard-coded credentials throughout
- Placeholder keys/certificates
- No secret rotation mechanism
- Multiple copies of sensitive data

### 3.5 Admission Control Policies

**Status:** NOT IMPLEMENTED

#### Issues:
- No Pod Security Standards enforced
- No Pod Security Policies
- No OPA/Kyverno policies
- No image signature verification
- No admission webhooks

**Recommended Fixes:**
```yaml
# Enforce Pod Security Standards
apiVersion: v1
kind: Namespace
metadata:
  name: enclii-production
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
# Container image policy (example with Kyverno)
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-image-digest
spec:
  validationFailureAction: enforce
  rules:
  - name: check-image-digest
    match:
      resources:
        kinds:
        - Pod
    validate:
      message: "Image must be referenced by digest"
      pattern:
        spec:
          containers:
          - image: "*/*/??*@sha256:*"
```

### 3.6 Container Image Policies

**Status:** NOT IMPLEMENTED

#### Issues:
- No digest pinning in base (only staging/production)
- No image signature verification
- No vulnerability scanning
- Using latest tags in some places

**Recommended Fixes:**
```bash
# Enable image signature verification with Sigstore/cosign
# Scan all images with Trivy
trivy image ghcr.io/madfam/switchyard-api:latest

# Pin all images to specific digests in production
# Never use 'latest' tag
```

---

## 4. PRODUCTION READINESS

### 4.1 High Availability Configuration

**Status:** MAJOR GAPS

#### Issues:
1. **PostgreSQL:**
   - Single replica (no failover)
   - No streaming replication
   - No standby database
   - Non-persistent storage
   - **Status:** NOT HA-READY

2. **Redis:**
   - Base: 1 replica
   - Staging: 2 replicas
   - Production: 3 replicas (better, but still not Sentinel/Cluster)
   - No persistent storage
   - **Status:** PARTIALLY HA

3. **Switchyard API:**
   - Base: 2 replicas
   - Staging: 3 replicas
   - Production: 5 replicas ✓
   - Has PDB potential (not configured) ✗
   - Good rolling update strategy ✓
   - **Status:** MOSTLY GOOD, needs PDB

#### Recommended Fixes:
```yaml
# 1. PostgreSQL HA Setup
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
spec:
  replicas: 3
  serviceName: postgres
  volumeClaimTemplates:
    - metadata:
        name: postgres-data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 50Gi
---
# 2. Pod Disruption Budget
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: switchyard-api-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: switchyard-api
---
# 3. Pod Disruption Budget for Redis
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: redis-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: redis
```

### 4.2 Backup Strategies

**Status:** NOT IMPLEMENTED

#### Issues:
- No backup CronJob
- No backup storage location
- No backup verification
- No RTO/RPO targets defined

**Recommended Implementation:**
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: postgres-backup
  namespace: enclii-production
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: postgres:15
            command:
            - /bin/sh
            - -c
            - |
              pg_dump -h postgres -U postgres enclii_prod > /backups/db-$(date +%Y%m%d-%H%M%S).sql
            volumeMounts:
            - name: backup-storage
              mountPath: /backups
            env:
            - name: PGPASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: password
          volumes:
          - name: backup-storage
            persistentVolumeClaim:
              claimName: backup-pvc
          restartPolicy: OnFailure
---
# Backup storage PVC
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: backup-pvc
  namespace: enclii-production
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
```

### 4.3 Disaster Recovery

**Status:** NOT IMPLEMENTED

#### Issues:
- No backup restore procedure documented
- No disaster recovery runbook
- No RTO/RPO defined
- No multi-region setup

**Recommended Approach:**
1. **Document RTO/RPO targets:**
   - RTO (Recovery Time Objective): < 4 hours
   - RPO (Recovery Point Objective): < 1 hour

2. **Implement backup to external storage:**
   - AWS S3, Azure Blob, GCS
   - Cross-region replication

3. **Test restore procedures:**
   - Weekly backup restoration test
   - Document and automate

### 4.4 Update Strategies

**Status:** PARTIALLY IMPLEMENTED

#### Good:
- Rolling update strategy configured ✓
- maxSurge and maxUnavailable configured ✓
- Different strategies for environments ✓

#### Issues:
- No pod disruption budgets ✗
- No readiness probe timeouts properly tuned ✗
- No revision history configured

**Recommended Improvements:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-api
spec:
  revisionHistoryLimit: 10  # Keep last 10 revisions for rollback
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
      
  # Ensure readiness probes are properly configured
  template:
    spec:
      readinessProbes:
        # Longer timeouts to ensure pod is truly ready before traffic
        periodSeconds: 3
        timeoutSeconds: 2
```

### 4.5 Health Checks and Probes

**Status:** MOSTLY GOOD

#### Good:
- Readiness probes configured ✓
- Liveness probes configured ✓
- Different timeouts for different environments ✓
- Health check paths defined ✓

#### Issues:
- PostgreSQL missing health checks ✗
- Jaeger missing health checks ✗
- No startup probes ✗
- Probe timeouts could be tighter

---

## 5. DEPLOYMENT CONFIGURATION

### 5.1 Docker Compose Setup

**Status:** NOT FOUND

**Issue:** No Docker Compose file in `/infra/`

**Recommended Implementation:**
```yaml
version: '3.8'
services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: enclii_dev
      POSTGRES_PASSWORD_FILE: /run/secrets/db_password
    secrets:
      - db_password
    volumes:
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    command: redis-server --requirepass $REDIS_PASSWORD
    environment:
      REDIS_PASSWORD: ${REDIS_PASSWORD:-}
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5

  switchyard-api:
    build: .
    ports:
      - "8080:8080"
    environment:
      ENCLII_DB_URL: postgres://postgres:password@postgres:5432/enclii_dev
      ENCLII_REDIS_URL: redis://:${REDIS_PASSWORD}@redis:6379
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

secrets:
  db_password:
    file: ./secrets/db_password.txt

volumes:
  postgres_data:
  redis_data:
```

### 5.2 Kind Cluster Configuration

**File:** `/home/user/enclii/infra/dev/kind-config.yaml`

**Status:** MOSTLY GOOD

#### Good:
- Multi-node setup (1 control plane + 2 workers) ✓
- Port mappings for ingress ✓
- kubeadm labels configured ✓

#### Issues:
1. Missing kubelet security flags
2. No pod CIDR configuration
3. No service CIDR configuration
4. No networking plugin specified

**Recommended Improvements:**
```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: enclii
networking:
  podSubnet: "10.244.0.0/16"
  serviceSubnet: "10.96.0.0/12"
  apiServerBindAddress: "0.0.0.0"
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
            # Add security flags
            enforce-node-allocatable: "pods,system-reserved"
            system-reserved: "cpu=100m,memory=256Mi"
            kube-reserved: "cpu=100m,memory=256Mi"
            feature-gates: "PodSecurityPolicy=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 80
        protocol: TCP
      - containerPort: 443
        hostPort: 443
        protocol: TCP
  - role: worker
    kubeadmConfigPatches:
      - |
        kind: JoinConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            enforce-node-allocatable: "pods"
  - role: worker
    kubeadmConfigPatches:
      - |
        kind: JoinConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            enforce-node-allocatable: "pods"
```

### 5.3 Local Development Setup

**File:** `/home/user/enclii/infra/dev/namespace.yaml`

**Status:** BASIC

#### Issues:
1. No Pod Security Standards labels
2. No network policy defaults
3. No resource quotas
4. No limit ranges

**Recommended Enhancements:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: enclii-system
  labels:
    name: enclii-system
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
---
apiVersion: v1
kind: ResourceQuota
metadata:
  name: enclii-quota
  namespace: enclii-system
spec:
  hard:
    requests.cpu: "10"
    requests.memory: "20Gi"
    limits.cpu: "20"
    limits.memory: "40Gi"
---
apiVersion: v1
kind: LimitRange
metadata:
  name: enclii-limits
  namespace: enclii-system
spec:
  limits:
  - max:
      cpu: "4"
      memory: "4Gi"
    min:
      cpu: "50m"
      memory: "32Mi"
    type: Container
```

---

## SUMMARY TABLE

| Category | Issue | Severity | Files | Count |
|----------|-------|----------|-------|-------|
| **Secrets Management** | Hard-coded credentials | CRITICAL | secrets.yaml, postgres.yaml | 6 |
| **Database** | Non-persistent storage, no HA, no health checks | CRITICAL | postgres.yaml | 4 |
| **RBAC** | Overprivileged ClusterRole | CRITICAL | rbac.yaml | 2 |
| **Network Policies** | Missing default-deny | CRITICAL | network-policies.yaml | 1 |
| **Security** | Missing Pod Security Standards | HIGH | All | 1 |
| **Ingress** | No TLS, deprecated annotations | HIGH | ingress-nginx.yaml | 3 |
| **Monitoring** | Missing security context, no persistence | HIGH | monitoring.yaml | 2 |
| **Kustomize** | Incorrect digest placeholder | HIGH | production/kustomization.yaml | 1 |
| **Redis** | Non-persistent, missing auth | HIGH | redis.yaml | 2 |
| **Images** | Pull policy for production | HIGH | switchyard-api.yaml | 1 |
| **Disaster Recovery** | Not implemented | HIGH | All | 1 |
| **Pod Disruption** | No PDBs configured | MEDIUM | Deployments | 3 |
| **Jaeger** | Missing health checks, no persistence | MEDIUM | monitoring.yaml | 2 |
| **Kind Config** | Missing security flags | MEDIUM | kind-config.yaml | 1 |

**Total Critical Issues:** 7  
**Total High Issues:** 12  
**Total Medium Issues:** 8

---

## PRIORITY FIX LIST

### Phase 1 (Immediate - BLOCKING)
1. Remove secrets from Git, implement secret management
2. Fix PostgreSQL security context and resource limits
3. Implement default-deny NetworkPolicies
4. Fix RBAC overprivilege with Role instead of ClusterRole
5. Add TLS/HTTPS to ingress

### Phase 2 (Critical - Before Production)
1. Implement PostgreSQL HA with StatefulSet
2. Add Pod Disruption Budgets
3. Implement backup/restore procedures
4. Add Pod Security Standards enforcement
5. Fix image pull policy and add image pull secrets

### Phase 3 (Important - Before General Availability)
1. Implement admission control policies (Kyverno/OPA)
2. Add monitoring and alerting configuration
3. Implement disaster recovery testing
4. Add capacity planning and autoscaling
5. Document runbooks and troubleshooting

---

## COMPLIANCE GAPS

- **CIS Kubernetes Benchmark:** Multiple failures in RBAC, Pod Security, Network Policies
- **SOC 2 Type II:** Secret management, access controls, audit logging
- **ISO 27001:** Information security, access control, cryptography
- **PCI-DSS:** Secret storage, network segmentation, monitoring
- **HIPAA:** Data protection, audit trails, encryption

All must be addressed for compliance certification.

