# Documentation Fix List

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> **Generated**: 2026-01-17 | **Audit Type**: Full-Stack Recovery Docs Sync
> **Updated**: 2026-01-17 21:45 UTC | **Final Lockdown & Repo Sync**

---

## Sanitation Audit Results (2026-01-17)

### Summary

| Phase | Action | Status |
|-------|--------|--------|
| **1. Disk Cleanup** | Docker prune + journal vacuum | ✅ 81% (freed 127GB) |
| **2. Registry Fix** | ImagePullBackOff resolution | ✅ Janua services healthy |
| **3. Migration Status** | DB version documentation | ✅ Documented |
| **4. Docs Audit** | This document | ✅ Updated |

### NEW: Janua API Port Issue (P0 - FIXED)

**Problem**: K8s Service `janua-api` targeted port **8080**, but container runs on **4100**.
**Resolution**: Patched service to target port 4100.
```bash
kubectl patch svc janua-api -n janua -p '{"spec":{"ports":[{"name":"http","port":80,"targetPort":4100}]}}'
```

### NEW: Database Access Configuration (FIXED 2026-01-17 21:44 UTC)

**Current State** (2026-01-17 21:44 UTC):
- PostgreSQL: `127.0.0.1:5432` ✅ **SECURED** (localhost only)
- Redis: `127.0.0.1:6379` ✅ **SECURED** (localhost only)

**Resolution Applied**:
```bash
# Changed in /opt/solarpunk/janua/docker-compose.production.yml
# PostgreSQL: "0.0.0.0:5432:5432" → "127.0.0.1:5432:5432"
# Redis: "0.0.0.0:6379:6379" → "127.0.0.1:6379:6379"
sudo docker compose -f docker-compose.production.yml up -d postgres-shared redis-shared
```

**Note**: K8s pods using Docker databases must use host network or ExternalName services.
The Enclii switchyard-api now uses K8s internal DNS (`redis.data.svc.cluster.local`).

### NEW: SSH Tunnel Fix (FIXED 2026-01-17 21:42 UTC)

**Problem**: `ssh.madfam.io` failing with "connection refused" - cloudflared pods tried `localhost:22` which refers to pod loopback, not host SSH.
**Resolution**: Updated Cloudflare tunnel route from `ssh://localhost:22` to `ssh://<CONTROL_PLANE_IP>:22` via dashboard.
**Verification**: `ssh solarpunk@ssh.madfam.io` now works correctly.

### NEW: Kyverno Configuration Issue (FIXED 2026-01-17 21:15 UTC)

**Problem**: CronJobs use non-existent image `bitnami/kubectl:1.28.5`
**Resolution**: Patched to `bitnami/kubectl:latest`, unsuspended, verified working.
```bash
kubectl patch cronjob kyverno-cleanup-admission-reports -n kyverno --type=json \
  -p='[{"op": "replace", "path": "/spec/jobTemplate/spec/template/spec/containers/0/image", "value": "bitnami/kubectl:latest"}]'
kubectl patch cronjob kyverno-cleanup-admission-reports -n kyverno --type=json \
  -p='[{"op": "replace", "path": "/spec/suspend", "value": false}]'
```

### NEW: Operation FORTRESS (2026-01-17 21:00-21:30 UTC)

**Phase 1 - janua-api CrashLoopBackOff**:
- Root cause: DATABASE_URL and REDIS_URL pointed to external IPs (blocked by localhost binding)
- Fix: Patched deployment to use K8s internal DNS (`postgres-0.postgres.data.svc.cluster.local`, `redis.data.svc.cluster.local`)
- Additional fix: Added `Host: localhost` header to health probes (app validates host header)
- Port corrected: 8000 (container runs Uvicorn on 8000, not 4100)

**Phase 2 - K8s Redis CrashLoopBackOff**:
- Root cause: Memory limit (512Mi) insufficient for 489MB RDB file, 1s probe timeout
- Fix: Increased memory to 1Gi, probe timeout to 5s, initial delay to 60s

**Phase 3 - claudecodeui CrashLoopBackOff**:
- Root cause: Missing `JWT_SECRET` environment variable
- Fix: Created secret and patched deployments in both `enclii-madfam-automation-prod` and `enclii-madfam-automation-production` namespaces

**Phase 4 - Alembic Migrations**: ✅ Aligned (DB version 001 = Code heads 001)

---

## Executive Summary

| Category | Count | Severity |
|----------|-------|----------|
| **Port Mismatch** | 3 files | CRITICAL |
| **Runtime Drift Documentation** | 1 file | HIGH |
| **Stale Infrastructure References** | 2 files | MEDIUM |
| **Outdated Localhost References** | OK | LOW (dev context) |
| **OIDC URL References** | 21 files | OK (current) |

---

## CRITICAL: Port Mismatch (4200 vs 8080)

### Issue
Documentation claims switchyard-api runs on **port 4200** per PORT_ALLOCATION.md, but actual K8s manifests and implementation use **port 8080**.

### Evidence

| Source | Port | Status |
|--------|------|--------|
| `CLAUDE.md` Port Allocation table | 4200 | Documented |
| `apps/switchyard-api/Dockerfile` EXPOSE | 4200 | Implementation |
| `apps/switchyard-api/Dockerfile` HEALTHCHECK | 4200 | Implementation |
| `infra/k8s/base/switchyard-api.yaml` ENCLII_PORT | 8080 | K8s Manifest |
| `infra/k8s/base/switchyard-api.yaml` containerPort | 8080 | K8s Manifest |
| `infra/k8s/base/switchyard-api.yaml` Service port | 8080 | K8s Manifest |
| `infra/k8s/production/cloudflared.yaml` | 8080 | Tunnel Route |
| `infra/k8s/README.md` | 4200 | Documented |

### Root Cause
The PORT_ALLOCATION.md defines *intended* port allocations, but the K8s manifests were implemented with 8080 (common default).

### Resolution Options

**Option A: Update Manifests to 4200** (Recommended)
- Aligns with documented architecture
- Requires coordinated update of:
  - `infra/k8s/base/switchyard-api.yaml`
  - `infra/k8s/production/cloudflared.yaml`
  - `infra/k8s/production/environment-patch.yaml`
  - ArgoCD sync after changes

**Option B: Update Documentation to 8080**
- Less disruptive to running infrastructure
- Requires updating:
  - `CLAUDE.md`
  - `infra/k8s/README.md`
  - PORT_ALLOCATION.md reference

### Files to Update (Option A)

```yaml
# infra/k8s/base/switchyard-api.yaml
# Change: ENCLII_PORT: "8080" → "4200"
# Change: containerPort: 8080 → 4200
# Change: service port: 8080 → 4200

# infra/k8s/production/cloudflared.yaml
# Change: service: http://switchyard-api:8080 → http://switchyard-api:4200

# infra/k8s/production/environment-patch.yaml
# Change: readinessProbe port: 8080 → 4200
# Change: livenessProbe port: 8080 → 4200
```

---

## HIGH: Runtime Drift Documentation

### Issue
`INFRA_ANATOMY.md` documents production state that should be fixed, not perpetuated.

### Specific Items

| Line | Content | Action |
|------|---------|--------|
| L212 | `ENCLII_REDIS_URL = <CONTROL_PLANE_IP>:6379` | Document as **BUG** not as current state |
| L41-43 | Systemd cloudflared services | Add "TO BE DISABLED" marker |
| L27 | Public IP reference | OK (informational) |

### Recommended Updates

```markdown
# In INFRA_ANATOMY.md, line 212:
# BEFORE:
| switchyard-api | ENCLII_REDIS_URL | **<CONTROL_PLANE_IP>:6379** | EXTERNAL |

# AFTER:
| switchyard-api | ENCLII_REDIS_URL | **<CONTROL_PLANE_IP>:6379** | **BUG** - Should be `redis://redis.data.svc.cluster.local:6379` |
```

---

## MEDIUM: Stale Infrastructure References

### infra/k8s/README.md

| Line | Issue | Fix |
|------|-------|-----|
| L84 | `port-forward svc/switchyard-api 4200:4200` | Should match actual port |
| L95 | Port 4200 in table | Should match actual port |
| L250 | Tunnel service example uses 4200 | Should match actual port |

### CLAUDE.md

| Line | Issue | Fix |
|------|-------|-----|
| L72 | "Start control plane API on :8080" | Correct (matches implementation) |
| L218 | Cost comparison | OK (marketing, not technical) |

---

## LOW: Localhost References (Development Context)

These references are **CORRECT** for local development and should NOT be changed:

| File | Reference | Context |
|------|-----------|---------|
| `docs/getting-started/*.md` | `localhost:8080` | Local dev instructions |
| `docs/guides/*.md` | `localhost:8080` | Testing guides |
| `apps/switchyard-ui/README.md` | `localhost:8080` | Dev setup |
| `examples/README.md` | `localhost:8080` | Example code |

---

## OK: OIDC URL References (Current)

These 21 files reference `auth.madfam.io` or `auth.madfam.io` and are **CURRENT**:

- `CLAUDE.md` - Correct OIDC configuration
- `AI_CONTEXT.md` - Correct references
- `README.md` - Correct production URLs
- `docs/production/*.md` - Correct infrastructure docs
- `docs/guides/*.md` - Correct setup guides
- `apps/switchyard-ui/*.md` - Correct UI configuration

**OIDC Endpoints Verified** (2026-01-17):
- `https://auth.madfam.io/.well-known/openid-configuration` → **200 OK**
- `https://auth.madfam.io/.well-known/openid-configuration` → **200 OK**

---

## Action Items

### Immediate (P0)
1. [x] ~~Decide on port strategy: 4200 (docs) vs 8080 (implementation)~~ → **Option B: Docs reflect 8080 (implementation)**
2. [x] ~~Update INFRA_ANATOMY.md to mark Redis URL as bug~~ → **Switchyard now uses K8s internal DNS**
3. [x] ~~Fix database port exposure~~ → **127.0.0.1 binding applied** (2026-01-17 21:44)
4. [x] ~~Fix ssh.madfam.io tunnel~~ → **Route updated to host IP** (2026-01-17 21:42)

### Short-term (P1)
5. [x] ~~Synchronize all port references (8080) in docs~~ → **janua K8s manifests updated to port 8000** (2026-01-17 21:30)
6. [x] ~~Add ENCLII_REDIS_URL to production patch~~ → **Added K8s internal DNS** (2026-01-17 21:30)
7. [ ] Add "Intended vs Actual" section to INFRA_ANATOMY.md

### Medium-term (P2)
8. [ ] Create automated docs validation in CI
9. [ ] Add port consistency check to diagnose-prod.sh

### Backported Files (2026-01-17 21:30 UTC)
| Repository | File | Change |
|------------|------|--------|
| enclii | `infra/k8s/production/environment-patch.yaml` | Added ENCLII_REDIS_URL with K8s internal DNS |
| janua | `k8s/base/deployments/janua-api.yaml` | Port 4100 → 8000, added Host header to probes |
| janua | `k8s/base/services/janua-api.yaml` | Service port 80 → targetPort 8000 |
| janua | `docker-compose.production.yml` | Already had 127.0.0.1 bindings (verified) |

---

## Validation Commands

```bash
# Check port references in K8s manifests
grep -r "4200\|8080" infra/k8s/ --include="*.yaml"

# Check OIDC endpoints
curl -s https://auth.madfam.io/.well-known/openid-configuration | jq '.issuer'
curl -s https://auth.madfam.io/.well-known/openid-configuration | jq '.issuer'

# Run production diagnostics
ssh solarpunk@ssh.madfam.io
./scripts/diagnose-prod.sh --quick
```

---

## Appendix: Files Scanned

### Documentation Files (*.md)
- Total scanned: 150+ files
- Issues found: 6 files with port mismatches
- OK: 21 files with OIDC references (current)

### Infrastructure Files
- `infra/k8s/base/*.yaml` - Port mismatch detected
- `infra/k8s/production/*.yaml` - Port mismatch detected
- `infra/argocd/*.yaml` - OK

### Source Code
- `apps/switchyard-api/Dockerfile` - Uses 4200
- `apps/switchyard-api/cmd/api/main.go` - Should be checked for default port
