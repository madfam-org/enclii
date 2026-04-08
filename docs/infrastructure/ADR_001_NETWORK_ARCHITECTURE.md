# ADR-001: Network Architecture & Security Laws

> **Status**: Accepted
> **Date**: 2026-01-17
> **Authors**: Infrastructure Team
> **Supersedes**: None
> **Context**: Operation FORTRESS (Infrastructure Stabilization) + Operation SENTINEL (Automated Guards)

---

## Summary

This Architecture Decision Record (ADR) establishes the foundational network security laws for the MADFAM infrastructure ecosystem. These laws are **non-negotiable** and are **automatically enforced** via Kyverno policies and the Sentinel audit CronJob.

---

## The Seven Laws

### Law 1: Database Port Binding

> **All databases MUST bind to `127.0.0.1` or ClusterIP. Public WAN binding is FORBIDDEN.**

**Rationale**: Public database exposure is the #1 cause of data breaches. During Operation FORTRESS, we discovered PostgreSQL and Redis were bound to `0.0.0.0`, accessible from the public internet.

**Implementation**:
- Docker Compose: Use `127.0.0.1:5432:5432` NOT `5432:5432` or `0.0.0.0:5432:5432`
- Kubernetes: Use `ClusterIP` service type, never `NodePort` with hostPort
- Kyverno Policy: `block-host-ports` rejects any Pod with hostPort

**Enforcement**:
```yaml
# Kyverno ClusterPolicy: block-host-ports
spec:
  validationFailureAction: Enforce
  rules:
    - name: deny-host-ports
      validate:
        pattern:
          spec:
            containers:
              - ports:
                  - hostPort: "!*"
```

**Verification**:
```bash
# Check for exposed ports
ss -tlnp | grep -E "0\.0\.0\.0:(5432|6379|3306|27017)"
# Expected: No output (nothing bound to 0.0.0.0)
```

---

### Law 2: Janua API Port Standard

> **Janua API runs on port `8000`. Ports `4100` and `8080` are DEPRECATED.**

**Rationale**: Historical drift caused confusion with multiple port references (4100 in docs, 8080 in old manifests, 8000 in actual container). Operation FORTRESS standardized on port 8000 (Uvicorn default).

**Canonical Configuration**:
```yaml
# K8s Deployment
containers:
  - name: janua-api
    ports:
      - containerPort: 8000
    env:
      - name: PORT
        value: "8000"

# K8s Service
spec:
  ports:
    - port: 80
      targetPort: 8000
```

**Port Hierarchy**:
| Layer | Port | Description |
|-------|------|-------------|
| Container | 8000 | Uvicorn listener |
| K8s Service | 80 | Internal cluster routing |
| Cloudflare Tunnel | 80 | Routes to Service port |
| Public | 443 | HTTPS via Cloudflare |

**Deprecated References**:
- ❌ `containerPort: 4100` - Legacy documentation
- ❌ `containerPort: 8080` - Common default assumption
- ❌ `ENCLII_PORT: 8080` - Switchyard historical

---

### Law 3: Redis Access Path

> **Redis MUST be accessed via `redis://redis.data.svc.cluster.local:6379`. External IPs are FORBIDDEN.**

**Rationale**: During Operation FORTRESS, we discovered Switchyard using `<HOST_IP>:6379` (public IP) which:
1. Exposed Redis traffic to the public internet
2. Failed when we locked down to localhost binding
3. Created unnecessary external network hops

**Canonical URLs**:
```yaml
# K8s Services (correct)
REDIS_URL: "redis://redis.data.svc.cluster.local:6379"
ENCLII_REDIS_URL: "redis://redis.data.svc.cluster.local:6379"

# Docker (localhost only)
REDIS_URL: "redis://127.0.0.1:6379"
```

**Forbidden Patterns**:
```yaml
# NEVER use these in production
REDIS_URL: "redis://<HOST_IP>:6379"  # External IP
REDIS_URL: "redis://0.0.0.0:6379"         # Wildcard bind
REDIS_URL: "redis://foundry-cp:6379"       # Hostname resolution issues
```

**Sentinel Audit Check**:
```bash
# Detect external Redis URLs
kubectl get deployments -A -o json | \
  jq -r '.items[].spec.template.spec.containers[].env[]? |
  select(.name | test("REDIS")) |
  select(.value | test("95\\.217|0\\.0\\.0\\.0")) | .value'
# Expected: No output
```

---

### Law 4: Tunnel Architecture

> **NO `systemd` tunnels. ONLY K8s ConfigMap tunnels are permitted.**

**Rationale**: Operation FORTRESS discovered a "Triple Tunnel Conflict" causing 530 errors:
1. `cloudflared.service` (systemd) - DISABLED
2. `cloudflared-janua.service` (systemd) - DISABLED
3. K8s cloudflared pods (ConfigMap) - THE ONLY VALID PATH

Systemd tunnels cannot route to K8s ClusterIPs, causing routing failures when services migrate to K8s.

**Current Architecture**:
```
Internet → Cloudflare Edge → K8s cloudflared pods → ClusterIP Services
                             (cloudflare-tunnel ns)
```

**Configuration**:
- Source of Truth: `infra/k8s/production/cloudflared-unified.yaml`
- Namespace: `cloudflare-tunnel`
- Replicas: 2 (for HA)

**Systemd Status** (must be disabled):
```bash
systemctl is-enabled cloudflared.service cloudflared-janua.service
# Expected: disabled disabled
```

**Enforcement**:
- Sentinel CronJob: CHECK 4 verifies no systemd tunnels are active
- Manual check: `systemctl is-active cloudflared*` should return `inactive`

---

### Law 5: PR Validation Gate

> **All PRs MUST pass the `infra-audit` check before merge.**

**Rationale**: Manual fixes applied during incidents must be backported to Git (IaC). The Sentinel audit ensures infrastructure drift is detected automatically.

**Current Implementation**:
- CronJob: `infra-audit` runs daily at 09:00 UTC
- Namespace: `sentinel`
- Checks:
  1. No `0.0.0.0` port bindings
  2. No `CrashLoopBackOff` pods > 10 minutes
  3. No `hostPort` usage
  4. No systemd tunnels active
  5. Health endpoints responding (auth.madfam.io, api.enclii.dev)

**Future CI Integration** (P2):
```yaml
# .github/workflows/infra-audit.yaml
- name: Run Sentinel Audit
  run: |
    kubectl create job ci-audit-${{ github.sha }} \
      --from=cronjob/infra-audit -n sentinel
    kubectl wait --for=condition=complete job/ci-audit-${{ github.sha }} \
      -n sentinel --timeout=120s
```

**Manual Trigger**:
```bash
kubectl create job manual-audit-$(date +%s) --from=cronjob/infra-audit -n sentinel
kubectl logs -n sentinel -l job-name --tail=100
```

---

### Law 6: Conservation Law (Differential Builds)

> **CI/CD pipelines MUST use change detection. Rebuilding unchanged services is FORBIDDEN.**

**Rationale**: Operation LAST MILE identified that full-monorepo rebuilds waste compute resources, increase deployment time, and create unnecessary registry churn. Each service should only rebuild when its source or dependencies actually change.

**Implementation Strategy**:
1. **Change Detection**: Use `dorny/paths-filter` or equivalent to detect affected services
2. **Dependency Mapping**: Track shared packages that trigger dependent rebuilds
3. **Conditional Jobs**: Each build job should have `if: needs.changes.outputs.<service> == 'true'`

**Canonical Implementation** (Janua example):
```yaml
# GitHub Actions with dorny/paths-filter
jobs:
  changes:
    name: Detect Changes
    runs-on: ubuntu-latest
    outputs:
      api: ${{ steps.filter.outputs.api }}
      admin: ${{ steps.filter.outputs.admin }}
    steps:
      - uses: dorny/paths-filter@v2
        id: filter
        with:
          filters: |
            api:
              - 'apps/api/**'
              - 'Dockerfile.api'
            admin:
              - 'apps/admin/**'
              - 'packages/ui/**'      # Shared dependency
              - 'packages/sdk/**'     # Shared dependency

  build-api:
    needs: changes
    if: needs.changes.outputs.api == 'true' || github.event_name == 'workflow_dispatch'
    # ... build steps
```

**Required Filters**:
| Service | Triggers |
|---------|----------|
| API | `apps/api/**`, `Dockerfile.api` |
| Frontend | `apps/<frontend>/**`, `packages/ui/**`, `Dockerfile.<frontend>` |
| Shared Package Change | All dependent frontends must rebuild |
| Lock File Change | All services that use that package manager |

**Escape Hatch**: `workflow_dispatch` allows manual full rebuild when needed.

**Verification**:
```bash
# Check GitHub Actions run - should skip unchanged services
gh run list --workflow=docker-build.yml --limit=5
# Jobs marked "skipped" indicate conservation law is working
```

---

### Law 7: The API Mandate

> **All tenant operations (onboarding, config, secrets) MUST be performed via Enclii/Janua APIs or CLIs. Direct database/SSH access is restricted strictly to Core Platform debugging (Foundry).**

**Rationale**: During Operations LIFTOFF and GOVERNOR, we identified a pattern of relying on "bare metal" access (SSH, SQL execution, kubectl exec into pods) instead of using our own ecosystem APIs. This violates the principle of self-deployment and creates:
1. Security gaps (bypassing audit logs)
2. Reproducibility issues (manual steps not captured in IaC)
3. Missing API coverage (if an operation requires SSH, the API is incomplete)

**Permitted Operations by Layer**:
| Layer | Permitted Access | Examples |
|-------|------------------|----------|
| Core Platform (Foundry) | SSH, kubectl exec, direct DB | Debugging, emergency recovery |
| Tenant Services | Enclii/Janua API + CLI ONLY | OAuth clients, deployments, secrets |
| Infrastructure | Terraform, K8s manifests | Namespace creation, resource allocation |

**Forbidden Patterns**:
```bash
# NEVER do these for tenant operations
kubectl exec -it postgres-pod -- psql  # Use API instead
ssh foundry-cp 'kubectl ...'           # Use enclii CLI instead
curl -X POST janua-api/internal/...    # Use public API endpoints
```

**Required Actions When API is Missing**:
1. Document the missing capability as a feature request
2. Log tech debt in `TECH_DEBT.md` with severity
3. Implement the API before relying on bare metal as permanent solution

**Tech Debt Tracking**:
```yaml
# Example tech debt entry
feature: Database Provisioning API
status: MISSING
workaround: kubectl exec into postgres pod
severity: HIGH
ticket: ENCLII-XXX
```

**Enforcement**:
- Code review: PRs containing `kubectl exec`, `psql`, or direct SSH commands for tenant operations must be rejected
- Audit: Sentinel should log any direct database access patterns
- CI: Fail builds that include hardcoded database credentials or connection strings

---

## Enforcement Mechanisms

### Kyverno Policies (Preventive)

| Policy | Scope | Action |
|--------|-------|--------|
| `block-host-ports` | Pods | Enforce (block) |
| `require-health-probes` | Deployments | Enforce (block) |
| `block-latest-ifnotpresent` | Pods | Enforce (block) |

### Sentinel CronJob (Detective)

| Check | Frequency | Alert |
|-------|-----------|-------|
| Port Bindings | Daily 09:00 UTC | Exit code 1 |
| CrashLoopBackOff | Daily 09:00 UTC | Exit code 1 |
| hostPort Usage | Daily 09:00 UTC | Exit code 1 |
| Systemd Tunnels | Daily 09:00 UTC | Exit code 1 |
| Health Endpoints | Daily 09:00 UTC | Exit code 1 |

---

## Related Documents

| Document | Purpose |
|----------|---------|
| `INFRA_ANATOMY.md` | Current infrastructure state |
| `DOCS_FIX_LIST.md` | Active documentation issues |
| `kyverno-guards.yaml` | Kyverno policy definitions |
| `sentinel-cronjob.yaml` | Audit job configuration |

---

## Revision History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | 2026-01-17 | Operation FORTRESS | Initial laws established |
| 1.1 | 2026-01-17 | Operation SENTINEL | Added enforcement mechanisms |
| 1.2 | 2026-01-18 | Operation LAST MILE | Added Law 6: Conservation Law (differential builds) |
| 1.3 | 2026-01-18 | Operation GOVERNOR | Added Law 7: The API Mandate (API-first tenant operations) |

---

## Appendix: Quick Reference Card

```
┌─────────────────────────────────────────────────────────────────┐
│                    ADR-001 QUICK REFERENCE                      │
├─────────────────────────────────────────────────────────────────┤
│ LAW 1: Databases → 127.0.0.1 or ClusterIP ONLY                 │
│ LAW 2: Janua API → Port 8000 (not 4100, not 8080)              │
│ LAW 3: Redis → redis.data.svc.cluster.local:6379               │
│ LAW 4: Tunnels → K8s ConfigMap ONLY (no systemd)               │
│ LAW 5: PRs → Must pass infra-audit                              │
│ LAW 6: CI/CD → Differential builds (skip unchanged)            │
│ LAW 7: Tenants → API/CLI ONLY (no SSH/kubectl exec)            │
├─────────────────────────────────────────────────────────────────┤
│ KYVERNO:  block-host-ports, require-health-probes,             │
│           block-latest-ifnotpresent                             │
│ SENTINEL: kubectl logs -n sentinel -l job-name                  │
└─────────────────────────────────────────────────────────────────┘
```

---

*This ADR is automatically enforced. Violations will be blocked by Kyverno or flagged by Sentinel.*
# Trigger rebuild - 1768726478
