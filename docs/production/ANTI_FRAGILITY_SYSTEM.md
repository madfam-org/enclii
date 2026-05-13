# Enclii Anti-Fragility System

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Purpose**: Prevent regressions, detect drift, and ensure production stability.

## Problem Statement

This document addresses the root cause of the January 2026 production incident where:
1. `enclii.dev` returned 502 due to Cloudflare tunnel port mismatch (4204 vs 80)
2. Configuration in Cloudflare dashboard drifted from intended state
3. No automated detection or alerting existed

## Anti-Fragility Principles

1. **Configuration as Code** - All infrastructure config lives in Git
2. **Automated Validation** - Pre and post-deployment checks
3. **Drift Detection** - Continuous monitoring for config drift
4. **Health Checks** - Comprehensive endpoint monitoring
5. **Fast Feedback** - Immediate alerts on failures
6. **Immutable Deployments** - No manual dashboard changes

---

## 1. Service Health Check System

### 1.1 Health Check Script

Location: `scripts/health-check.sh`

Monitors all production endpoints and alerts on failures.

```bash
#!/bin/bash
# Production Health Check Script
# Run: ./scripts/health-check.sh
# Cron: */5 * * * * /path/to/health-check.sh

set -euo pipefail

# Configuration
SLACK_WEBHOOK="${ENCLII_SLACK_WEBHOOK:-}"
ENDPOINTS=(
    "https://enclii.dev|Landing Page"
    "https://app.enclii.dev|Dashboard"
    "https://api.enclii.dev/health|API Health"
    "https://docs.enclii.dev|Documentation"
    "https://auth.madfam.io/.well-known/openid-configuration|OIDC Discovery"
)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

# Results
FAILED=()
PASSED=()

check_endpoint() {
    local url="${1%%|*}"
    local name="${1##*|}"

    local http_code
    http_code=$(curl -sL -o /dev/null -w "%{http_code}" --max-time 10 "$url" 2>/dev/null || echo "000")

    if [[ "$http_code" =~ ^2[0-9][0-9]$ ]] || [[ "$http_code" == "302" ]]; then
        PASSED+=("$name: $http_code")
        echo -e "${GREEN}✓${NC} $name ($url): $http_code"
        return 0
    else
        FAILED+=("$name: $http_code ($url)")
        echo -e "${RED}✗${NC} $name ($url): $http_code"
        return 1
    fi
}

send_alert() {
    local message="$1"

    if [[ -n "$SLACK_WEBHOOK" ]]; then
        curl -s -X POST "$SLACK_WEBHOOK" \
            -H 'Content-type: application/json' \
            -d "{\"text\": \"$message\"}" > /dev/null
    fi

    echo -e "${RED}ALERT:${NC} $message"
}

main() {
    echo "=========================================="
    echo "Enclii Production Health Check"
    echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    echo "=========================================="

    for endpoint in "${ENDPOINTS[@]}"; do
        check_endpoint "$endpoint" || true
    done

    echo ""
    echo "=========================================="
    echo "Summary: ${#PASSED[@]} passed, ${#FAILED[@]} failed"
    echo "=========================================="

    if [[ ${#FAILED[@]} -gt 0 ]]; then
        local alert_msg="🚨 PRODUCTION ALERT: ${#FAILED[@]} service(s) failing\n"
        for failure in "${FAILED[@]}"; do
            alert_msg+="• $failure\n"
        done
        send_alert "$alert_msg"
        exit 1
    fi

    echo -e "${GREEN}All services healthy${NC}"
    exit 0
}

main "$@"
```

### 1.2 Kubernetes CronJob

Location: `infra/k8s/production/health-check-cronjob.yaml`

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: production-health-check
  namespace: monitoring
spec:
  schedule: "*/5 * * * *"  # Every 5 minutes
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 5
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: health-check
              image: curlimages/curl:8.5.0
              command:
                - /bin/sh
                - -c
                - |
                  FAILED=0
                  check() {
                    code=$(curl -sL -o /dev/null -w "%{http_code}" --max-time 10 "$1" || echo "000")
                    if [ "$code" != "200" ] && [ "$code" != "302" ]; then
                      echo "FAILED: $2 returned $code"
                      FAILED=1
                    else
                      echo "OK: $2"
                    fi
                  }
                  check "https://enclii.dev" "Landing Page"
                  check "https://app.enclii.dev" "Dashboard"
                  check "https://api.enclii.dev/health" "API"
                  check "https://auth.madfam.io/.well-known/openid-configuration" "OIDC"
                  exit $FAILED
          restartPolicy: OnFailure
```

---

## 2. Configuration Drift Detection

### 2.1 Cloudflare Config Validator

Location: `scripts/validate-cloudflare-config.sh`

Compares Cloudflare dashboard config against expected Git config.

```bash
#!/bin/bash
# Validate Cloudflare Tunnel Configuration
# Requires: CLOUDFLARE_API_TOKEN, CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_TUNNEL_ID

set -euo pipefail

EXPECTED_CONFIG="infra/k8s/production/expected-tunnel-config.json"

# Fetch current config from Cloudflare API
fetch_tunnel_config() {
    curl -s "https://api.cloudflare.com/client/v4/accounts/${CLOUDFLARE_ACCOUNT_ID}/cfd_tunnel/${CLOUDFLARE_TUNNEL_ID}/configurations" \
        -H "Authorization: Bearer ${CLOUDFLARE_API_TOKEN}" \
        -H "Content-Type: application/json" | jq '.result.config.ingress'
}

# Compare configurations
compare_configs() {
    local live_config="$1"
    local expected_config="$2"

    # Sort and normalize both configs
    local live_sorted=$(echo "$live_config" | jq -S '.')
    local expected_sorted=$(cat "$expected_config" | jq -S '.')

    if [ "$live_sorted" == "$expected_sorted" ]; then
        echo "✓ Configuration matches expected state"
        return 0
    else
        echo "✗ Configuration DRIFT detected!"
        echo ""
        echo "Differences:"
        diff <(echo "$expected_sorted") <(echo "$live_sorted") || true
        return 1
    fi
}

main() {
    echo "Validating Cloudflare Tunnel Configuration..."

    live_config=$(fetch_tunnel_config)
    compare_configs "$live_config" "$EXPECTED_CONFIG"
}

main
```

### 2.2 Expected Tunnel Config (Source of Truth)

Location: `infra/k8s/production/expected-tunnel-config.json`

```json
[
  {
    "hostname": "api.enclii.dev",
    "service": "http://switchyard-api.enclii.svc.cluster.local:80"
  },
  {
    "hostname": "app.enclii.dev",
    "service": "http://switchyard-ui.enclii.svc.cluster.local:80"
  },
  {
    "hostname": "enclii.dev",
    "service": "http://landing-page.enclii.svc.cluster.local:80"
  },
  {
    "hostname": "www.enclii.dev",
    "service": "http://landing-page.enclii.svc.cluster.local:80"
  },
  {
    "hostname": "docs.enclii.dev",
    "service": "http://docs-site.enclii.svc.cluster.local:80"
  },
  {
    "hostname": "auth.madfam.io",
    "service": "http://janua-api.janua.svc.cluster.local:4100"
  },
  {
    "service": "http_status:404"
  }
]
```

---

## 3. Pre-Deployment Validation

### 3.1 Deployment Checklist Script

Location: `scripts/pre-deploy-check.sh`

```bash
#!/bin/bash
# Pre-Deployment Validation
# Run before any production deployment

set -euo pipefail

echo "=========================================="
echo "Pre-Deployment Validation"
echo "=========================================="

ERRORS=0

# Check 1: Git status is clean
echo -n "Checking git status... "
if [[ -n $(git status --porcelain) ]]; then
    echo "FAIL: Uncommitted changes"
    ERRORS=$((ERRORS+1))
else
    echo "OK"
fi

# Check 2: On main branch
echo -n "Checking branch... "
BRANCH=$(git branch --show-current)
if [[ "$BRANCH" != "main" ]]; then
    echo "WARN: Not on main branch (on $BRANCH)"
else
    echo "OK"
fi

# Check 3: Tests pass
echo -n "Running tests... "
if ! make test > /dev/null 2>&1; then
    echo "FAIL: Tests failed"
    ERRORS=$((ERRORS+1))
else
    echo "OK"
fi

# Check 4: API builds
echo -n "Building API... "
if ! (cd apps/switchyard-api && go build ./...) > /dev/null 2>&1; then
    echo "FAIL: API build failed"
    ERRORS=$((ERRORS+1))
else
    echo "OK"
fi

# Check 5: UI builds
echo -n "Building UI... "
if ! (cd apps/switchyard-ui && npm run build) > /dev/null 2>&1; then
    echo "FAIL: UI build failed"
    ERRORS=$((ERRORS+1))
else
    echo "OK"
fi

# Check 6: Validate K8s manifests
echo -n "Validating K8s manifests... "
if ! kubectl --dry-run=client -f infra/k8s/production/ > /dev/null 2>&1; then
    echo "FAIL: Invalid K8s manifests"
    ERRORS=$((ERRORS+1))
else
    echo "OK"
fi

echo ""
if [[ $ERRORS -gt 0 ]]; then
    echo "=========================================="
    echo "BLOCKED: $ERRORS validation(s) failed"
    echo "=========================================="
    exit 1
fi

echo "=========================================="
echo "All validations passed"
echo "=========================================="
```

---

## 4. Post-Deployment Smoke Tests

### 4.1 Smoke Test Script

Location: `scripts/smoke-test.sh`

```bash
#!/bin/bash
# Post-Deployment Smoke Tests
# Run immediately after deployment

set -euo pipefail

TIMEOUT=60
INTERVAL=5

echo "=========================================="
echo "Post-Deployment Smoke Tests"
echo "=========================================="

wait_for_ready() {
    local url="$1"
    local name="$2"
    local elapsed=0

    echo -n "Waiting for $name... "

    while [[ $elapsed -lt $TIMEOUT ]]; do
        if curl -sL -o /dev/null -w "%{http_code}" --max-time 5 "$url" 2>/dev/null | grep -q "^2"; then
            echo "OK (${elapsed}s)"
            return 0
        fi
        sleep $INTERVAL
        elapsed=$((elapsed + INTERVAL))
    done

    echo "TIMEOUT after ${TIMEOUT}s"
    return 1
}

# Core smoke tests
FAILED=0

wait_for_ready "https://api.enclii.dev/health" "API" || FAILED=$((FAILED+1))
wait_for_ready "https://app.enclii.dev" "Dashboard" || FAILED=$((FAILED+1))
wait_for_ready "https://enclii.dev" "Landing Page" || FAILED=$((FAILED+1))

# API functionality test
echo -n "Testing API login redirect... "
LOGIN_REDIRECT=$(curl -sI "https://api.enclii.dev/v1/auth/login" 2>/dev/null | grep -i "location:" || echo "")
if [[ "$LOGIN_REDIRECT" == *"janua"* ]] || [[ "$LOGIN_REDIRECT" == *"auth.madfam.io"* ]]; then
    echo "OK"
else
    echo "FAIL: No OIDC redirect"
    FAILED=$((FAILED+1))
fi

echo ""
if [[ $FAILED -gt 0 ]]; then
    echo "=========================================="
    echo "SMOKE TESTS FAILED: $FAILED test(s)"
    echo "Consider rollback!"
    echo "=========================================="
    exit 1
fi

echo "=========================================="
echo "All smoke tests passed"
echo "=========================================="
```

---

## 5. Automated Deployment Pipeline

### 5.1 GitHub Actions Workflow

Location: `.github/workflows/deploy-production.yml`

```yaml
name: Deploy to Production

on:
  push:
    branches: [main]
    paths:
      - 'apps/**'
      - 'infra/**'
  workflow_dispatch:

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Pre-deployment validation
        run: ./scripts/pre-deploy-check.sh

      - name: Validate Cloudflare config
        env:
          CLOUDFLARE_API_TOKEN: ${{ secrets.CLOUDFLARE_API_TOKEN }}
          CLOUDFLARE_ACCOUNT_ID: ${{ secrets.CLOUDFLARE_ACCOUNT_ID }}
          CLOUDFLARE_TUNNEL_ID: ${{ secrets.CLOUDFLARE_TUNNEL_ID }}
        run: ./scripts/validate-cloudflare-config.sh

  deploy:
    needs: validate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Deploy to Kubernetes
        run: |
          # Deploy using existing scripts
          ./scripts/deploy-production.sh apply

      - name: Run smoke tests
        run: ./scripts/smoke-test.sh

      - name: Notify on success
        if: success()
        run: |
          curl -X POST ${{ secrets.SLACK_WEBHOOK }} \
            -d '{"text": "✅ Production deployment successful"}'

      - name: Notify on failure
        if: failure()
        run: |
          curl -X POST ${{ secrets.SLACK_WEBHOOK }} \
            -d '{"text": "🚨 Production deployment FAILED - check logs"}'
```

---

## 6. Service Port Registry

### 6.1 Port Allocation Documentation

Centralized port registry to prevent mismatches.

Location: `docs/PORT_REGISTRY.md`

| Service | Container Port | Service Port | Cloudflare Target |
|---------|---------------|--------------|-------------------|
| switchyard-api | 4200 | 80 | :80 |
| switchyard-ui | 4201 | 80 | :80 |
| landing-page | 8080 | 80 | :80 |
| docs-site | 3000 | 80 | :80 |
| janua-api | 4100 | 4100 | :4100 |

**Rule**: All external services use Service port 80 for Cloudflare routing.

---

## 7. Monitoring Dashboard

### 7.1 Grafana Dashboard Config

Key metrics to monitor:

```json
{
  "panels": [
    {
      "title": "Service Availability",
      "targets": [
        { "expr": "probe_success{job='blackbox'}" }
      ]
    },
    {
      "title": "Response Time",
      "targets": [
        { "expr": "probe_http_duration_seconds{job='blackbox'}" }
      ]
    },
    {
      "title": "HTTP Status Codes",
      "targets": [
        { "expr": "sum(rate(http_requests_total[5m])) by (status_code)" }
      ]
    }
  ]
}
```

---

## 8. Runbook: Production Incident Response

### 8.1 502 Bad Gateway

1. **Check cloudflared logs**: `kubectl logs -n cloudflare-tunnel -l app=cloudflared`
2. **Verify service ports match**:
   - Service YAML targetPort
   - Cloudflare dashboard service URL
   - Expected config in `expected-tunnel-config.json`
3. **Test internal connectivity**: `kubectl exec -n enclii deploy/switchyard-api -- wget -qO- http://<service>:<port>/`
4. **If port mismatch**: Update Cloudflare dashboard AND `expected-tunnel-config.json`
5. **Restart cloudflared**: `kubectl rollout restart deployment/cloudflared -n cloudflare-tunnel`

### 8.2 SSO/OIDC Failures

1. **Test OIDC discovery**: `curl https://auth.madfam.io/.well-known/openid-configuration`
2. **Check Janua API health**: `kubectl logs -n janua deploy/janua-api`
3. **Verify OIDC env vars**: `kubectl get deploy switchyard-api -n enclii -o jsonpath='{.spec.template.spec.containers[0].env}'`
4. **Test auth redirect**: `curl -I https://api.enclii.dev/v1/auth/login`

---

## Implementation Priority

1. **Immediate**: Health check script + cron
2. **Week 1**: Cloudflare config validator + expected config
3. **Week 2**: Pre/post deployment scripts
4. **Week 3**: GitHub Actions pipeline
5. **Week 4**: Grafana dashboard + alerts

---

## Summary

This anti-fragility system provides:

- **Prevention**: Pre-deployment validation catches issues before release
- **Detection**: Health checks and drift detection find problems early
- **Response**: Runbooks and smoke tests enable fast recovery
- **Learning**: Centralized documentation prevents repeat incidents

The key insight from this incident: **Configuration managed via dashboard UI will drift**.
The solution: **All config must be code-validated against expected state**.
