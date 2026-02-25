---
title: Testing Guide
description: Testing strategy for Enclii platform including unit, integration, and E2E tests
sidebar_position: 4
tags: [guides, testing, integration-tests, e2e, ci-cd]
---

# Enclii Testing Guide

This document describes the testing strategy for Enclii platform features, including integration tests for critical blocker fixes (P0/P1).

## Test Categories

### 1. Unit Tests
Located in `*_test.go` files alongside source code.

### 2. Integration Tests
Located in `tests/integration/` directory.

### 3. End-to-End Tests
Located in `tests/e2e/` directory.

---

## Critical Blocker Integration Tests

### Test Suite 1: PVC Persistence (P0)

**Objective**: Verify that PostgreSQL and Redis data persists across pod restarts.

#### Test: PostgreSQL Data Persistence
```bash
# File: tests/integration/pvc_persistence_test.go

Test: TestPostgreSQLPersistence
1. Deploy PostgreSQL with PVC (postgres.yaml)
2. Write test data to database
3. Delete PostgreSQL pod
4. Wait for new pod to start
5. Verify test data still exists
Expected: Data persists after pod restart
```

**Manual Test Steps**:
```bash
# 1. Apply PostgreSQL with PVC (dev-only manifest, not used in production)
kubectl apply -f infra/k8s/base/postgres.yaml

# 2. Write test data
kubectl exec -it postgres-<pod-id> -- psql -U postgres -d enclii_dev
enclii_dev=# CREATE TABLE test (id INT, data TEXT);
enclii_dev=# INSERT INTO VALUES (1, 'persistence-test');
enclii_dev=# SELECT * FROM test;
 id |      data
----+------------------
  1 | persistence-test
enclii_dev=# \q

# 3. Delete pod to trigger restart
kubectl delete pod postgres-<pod-id>

# 4. Wait for new pod
kubectl wait --for=condition=ready pod -l app=postgres --timeout=60s

# 5. Verify data persists
kubectl exec -it postgres-<new-pod-id> -- psql -U postgres -d enclii_dev -c "SELECT * FROM test;"
 id |      data
----+------------------
  1 | persistence-test

# SUCCESS: Data persisted across pod restart
```

#### Test: Redis Cache Persistence
```bash
# File: tests/integration/pvc_persistence_test.go

Test: TestRedisPersistence
1. Deploy Redis with PVC (redis.yaml)
2. Write test data to Redis (SET persistence-test "value")
3. Trigger BGSAVE to flush to disk
4. Delete Redis pod
5. Wait for new pod to start
6. Verify test data still exists (GET persistence-test)
Expected: Data persists after pod restart
```

**Manual Test Steps**:
```bash
# 1. Apply Redis with PVC
kubectl apply -f infra/k8s/base/redis.yaml

# 2. Write test data
kubectl exec -it redis-<pod-id> -- redis-cli
127.0.0.1:6379> SET persistence-test "this-should-survive-restart"
OK
127.0.0.1:6379> BGSAVE
Background saving started
127.0.0.1:6379> exit

# 3. Delete pod
kubectl delete pod redis-<pod-id>

# 4. Wait for new pod
kubectl wait --for=condition=ready pod -l app=redis --timeout=60s

# 5. Verify data persists
kubectl exec -it redis-<new-pod-id> -- redis-cli GET persistence-test
"this-should-survive-restart"

# SUCCESS: Cache persisted across pod restart
```

---

### Test Suite 2: Service Volume Support (P0)

**Objective**: Verify that services can be deployed with persistent volumes and data is accessible.

#### Test: Service with Single Volume
```bash
# File: tests/integration/service_volumes_test.go

Test: TestServiceSingleVolume
1. Create service with 1 volume (examples/stateful-service.yaml)
2. Deploy service
3. Verify PVC created with correct size/storageClass
4. Verify volume mounted at correct path in pod
5. Write test file to mounted volume
6. Restart pod
7. Verify file still exists
Expected: PVC created, volume mounted, data persists
```

**Manual Test Steps**:
```bash
# 1. Deploy service with volume
enclii deploy --service file-processor --env dev

# 2. Verify PVC created
kubectl get pvc -n enclii-{project-id}
# NAME                          STATUS   CAPACITY   STORAGECLASS
# file-processor-uploads        Bound    50Gi       standard
# file-processor-cache          Bound    10Gi       fast-ssd

# 3. Verify volumes mounted
kubectl exec -it file-processor-<pod-id> -- df -h | grep /data
/dev/sdb  50G  /data/uploads
/dev/sdc  10G  /data/cache

# 4. Write test file
kubectl exec -it file-processor-<pod-id> -- sh -c 'echo "test-data" > /data/uploads/test.txt'

# 5. Delete pod
kubectl delete pod file-processor-<pod-id>

# 6. Verify file persists
kubectl exec -it file-processor-<new-pod-id> -- cat /data/uploads/test.txt
test-data

# SUCCESS: Volume mounted and data persisted
```

#### Test: Service with Multiple Volumes
Same as above but with multiple volumes, verifying each independently.

---

### Test Suite 3: Custom Domain & TLS (P1)

**Objective**: Verify custom domains create Ingress with automatic TLS certificates.

#### Test: Custom Domain Creation
```bash
# File: tests/integration/custom_domain_test.go

Test: TestCustomDomainCreation
1. Deploy service
2. Add custom domain via API
3. Verify Ingress resource created
4. Verify Ingress has correct host rules
5. Verify Ingress has cert-manager annotations
6. Verify Certificate resource created
Expected: Ingress and Certificate resources exist with correct config
```

**Manual Test Steps**:
```bash
# 1. Deploy service
enclii deploy --service api-gateway --env production

# 2. Add custom domain
curl -X POST https://api.enclii.io/v1/services/{id}/domains \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "domain": "api.example.com",
    "environment": "production",
    "tls_enabled": true,
    "tls_issuer": "letsencrypt-staging"
  }'

# 3. Verify Ingress created
kubectl get ingress -n enclii-{project-id}
# NAME          HOSTS              ...
# api-gateway   api.example.com    ...

# 4. Check Ingress configuration
kubectl get ingress api-gateway -n enclii-{project-id} -o yaml
# Should have:
# - host: api.example.com
# - annotations:
#     cert-manager.io/cluster-issuer: letsencrypt-staging
#     kubernetes.io/ingress.class: nginx
# - tls:
#   - hosts:
#     - api.example.com
#     secretName: api-gateway-api-example-com-tls

# 5. Verify Certificate resource
kubectl get certificate -n enclii-{project-id}
# NAME                             READY
# api-gateway-api-example-com-tls  True

# SUCCESS: Ingress and Certificate created correctly
```

#### Test: TLS Certificate Issuance
```bash
# File: tests/integration/custom_domain_test.go

Test: TestTLSCertificateIssuance
1. Add custom domain with tls_enabled=true
2. Configure DNS to point to ingress
3. Wait for cert-manager to issue certificate (max 5 min)
4. Verify Certificate status is Ready
5. Verify Secret contains valid TLS certificate
6. Test HTTPS connection
Expected: Valid TLS certificate issued by Let's Encrypt
```

**Manual Test Steps**:
```bash
# Prerequisites:
# - Domain DNS configured: api.example.com -> ingress IP
# - cert-manager installed

# 1. Add domain (see previous test)

# 2. Watch Certificate resource
kubectl get certificate -n enclii-{project-id} -w
# api-gateway-api-example-com-tls  False  Issuing certificate...
# api-gateway-api-example-com-tls  True   Certificate issued

# 3. Verify Secret created
kubectl get secret api-gateway-api-example-com-tls -n enclii-{project-id}
kubectl get secret api-gateway-api-example-com-tls -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
# Issuer: CN=R3,O=Let's Encrypt,C=US
# Subject: CN=api.example.com
# X509v3 Subject Alternative Name: DNS:api.example.com

# 4. Test HTTPS
curl -v https://api.example.com/health
# * SSL certificate verify ok
# < HTTP/2 200
# SUCCESS: Valid TLS certificate issued
```

#### Test: Domain Verification
```bash
# File: tests/integration/custom_domain_test.go

Test: TestDomainVerification
1. Add custom domain
2. Add DNS TXT record with verification code
3. Call verify endpoint
4. Verify domain.Verified = true
5. Verify domain.VerifiedAt is set
Expected: Domain marked as verified in database
```

**Manual Test Steps**:
```bash
# 1. Add domain (returns verification code)
curl -X POST https://api.enclii.io/v1/services/{id}/domains \
  -d '{"domain": "verify.example.com", "environment": "production"}'
# Response: {"domain_id": "...", "verification_value": "enclii-verification=abc123"}

# 2. Add TXT record
# verify.example.com  TXT  enclii-verification=abc123

# 3. Verify DNS propagation
dig TXT verify.example.com
# verify.example.com.  300  IN  TXT  "enclii-verification=abc123"

# 4. Call verify endpoint
curl -X POST https://api.enclii.io/v1/services/{id}/domains/{domain_id}/verify

# Response: {"message": "domain verified successfully", "domain": {...}}

# 5. Check database
psql -c "SELECT domain, verified, verified_at FROM custom_domains WHERE domain='verify.example.com';"
#        domain        | verified |      verified_at
# ---------------------+----------+------------------------
#  verify.example.com  | true     | 2025-11-20 12:34:56

# SUCCESS: Domain verified
```

---

### Test Suite 4: Path-Based Routing (P1)

**Objective**: Verify routes create correct Ingress path rules.

#### Test: Single Route
```bash
# File: tests/integration/routes_test.go

Test: TestSingleRoute
1. Add custom domain
2. Add route: /api/v1, Prefix, port 8080
3. Verify Ingress updated with path rule
4. Test HTTP request to /api/v1/users routes correctly
Expected: Request routed to service
```

#### Test: Multiple Routes
```bash
Test: TestMultipleRoutes
1. Add custom domain
2. Add routes: /api/v1, /api/v2, /docs
3. Verify Ingress has all path rules
4. Test each path routes correctly
Expected: All paths route to correct service/port
```

---

## Automated Test Execution

### Prerequisites
```bash
# Install dependencies
go get -u github.com/stretchr/testify/assert
go get -u sigs.k8s.io/controller-runtime/pkg/client

# Set up test cluster
kind create cluster --name enclii-test
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.yaml
# Apply dev-only manifests (not in production kustomization)
kubectl apply -f infra/k8s/base/secrets.dev.yaml
kubectl apply -f infra/k8s/base/postgres.yaml
kubectl apply -k infra/k8s/base/
```

### Run Integration Tests
```bash
# Run all integration tests
go test ./tests/integration/... -v

# Run specific test suite
go test ./tests/integration/pvc_persistence_test.go -v

# Run with coverage
go test ./tests/integration/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### CI/CD Integration
```yaml
# .github/workflows/integration-tests.yml
name: Integration Tests
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - name: Create kind cluster
        uses: helm/kind-action@v1
      - name: Install cert-manager
        run: kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.yaml
      - name: Run integration tests
        run: go test ./tests/integration/... -v
```

---

## Test Coverage Goals

- Unit tests: ≥ 80%
- Integration tests: ≥ 70%
- E2E tests: Critical user flows

---

## Troubleshooting Tests

### Test Fails: PVC Not Bound
```bash
# Check storage class exists
kubectl get storageclass
# If missing, create default storageclass for kind:
kubectl apply -f tests/fixtures/storageclass.yaml
```

### Test Fails: Certificate Not Issued
```bash
# Check cert-manager logs
kubectl logs -n cert-manager deployment/cert-manager

# Common issues:
# 1. DNS not configured (staging issuer bypasses this)
# 2. Rate limit hit (use staging issuer for tests)
# 3. Firewall blocking Let's Encrypt (port 80/443)
```

### Test Fails: Ingress Not Created
```bash
# Check reconciler logs
kubectl logs -n enclii-system deployment/switchyard-api

# Check if reconciler is running
kubectl get pods -n enclii-system
```

---

## Contributing Tests

1. Write test following existing patterns
2. Test locally with kind cluster
3. Ensure test is idempotent (can run multiple times)
4. Add cleanup in test teardown
5. Document manual test steps
6. Update this guide

---

## Future Test Improvements

- [ ] Automated integration test runner in CI
- [ ] Performance tests for reconciler
- [ ] Chaos engineering tests (pod failures, network partitions)
- [ ] Load tests for API endpoints
- [ ] Security tests (penetration testing)

---

## Related Documentation

- **Getting Started**: [Quick Start Guide](/docs/getting-started/QUICKSTART) | [Development Guide](/docs/getting-started/DEVELOPMENT)
- **CLI**: [CLI Reference](/docs/cli/)
- **Troubleshooting**: [Build Failures](/docs/troubleshooting/build-failures) | [Deployment Issues](/docs/troubleshooting/deployment-issues)
- **Infrastructure**: [Infrastructure Overview](/docs/infrastructure/)
- **Production**: [Production Checklist](/docs/production/PRODUCTION_CHECKLIST)
