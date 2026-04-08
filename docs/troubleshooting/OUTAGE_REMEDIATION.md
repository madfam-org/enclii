# Service Outage Remediation Playbook

Last updated: 2026-03-15

## Overview

This playbook covers diagnosing and remediating service outages visible on `status.madfam.io`. The status page monitors 30 endpoints across 9 project groups.

## Quick Start

```bash
# From foundry-cp (or any host with KUBECONFIG)
./scripts/diagnose-outages.sh
```

## Common Failure Modes

### 502 Bad Gateway

**Cause**: Cloudflare tunnel route exists but no backend pod is running.

**Diagnosis**:
```bash
# Check pods in the affected namespace
kubectl get pods -n <namespace>

# If no pods: check ArgoCD app
kubectl get application <app-name> -n argocd -o yaml

# If pods exist but crash: check logs
kubectl logs -n <namespace> deploy/<service> --tail=50

# Common sub-causes:
# - Image pull error (GHCR auth, missing image tag)
# - OOMKilled (resource limits too low)
# - CrashLoopBackOff (app startup failure, missing env vars/secrets)
```

**Fix**:
```bash
# Force ArgoCD sync
kubectl patch application <app-name> -n argocd --type merge -p '{"operation":{"sync":{}}}'

# Or restart deployment
kubectl rollout restart deployment/<service> -n <namespace>

# If OOMKilled, increase limits in the repo's K8s manifests
```

### NetworkPolicy Blocking Intra-Namespace Traffic

**Cause**: Default-deny policies block pod-to-pod traffic within the same namespace. Common when services deploy their own database/cache/broker pods (e.g., pravara-mes runs postgres-pravara + redis-pravara locally) but egress/ingress rules only target a shared namespace (e.g., `data`).

**Symptoms**:
- CrashLoopBackOff with database connection errors (but shared postgres in `data` namespace is healthy)
- 502 via Cloudflare tunnel (backend pod crashing)
- `kubectl logs` shows connection refused/timeout to in-namespace services

**Diagnosis**:
```bash
# Check if pod can reach its local database
kubectl exec -n <namespace> deploy/<app> -- nc -zv <db-svc>:5432

# List all NetworkPolicies in the namespace
kubectl get networkpolicy -n <namespace> -o yaml

# Look for egress rules — do they target the correct namespace?
# If selectors use namespaceSelector for 'data' but DB is in-namespace,
# the podSelector should NOT have a namespaceSelector (same-namespace default)
```

**Fix**: Add intra-namespace ingress rules for infrastructure pods and update egress rules to target local pod selectors:
```bash
# IMPORTANT: delete-and-recreate (not just apply) if policies lack
# the last-applied-configuration annotation (session 77 gotcha)
kubectl delete networkpolicies --all -n <namespace>
kubectl apply -f infra/k8s/policies/<namespace>-network-policies.yaml
```

**Real examples:**

| Namespace | Service | Symptom | Root Cause | Fix |
|-----------|---------|---------|------------|-----|
| tezca | tezca-worker, tezca-beat | `Connection refused` to `tezca-redis:6379` | Egress only allowed Redis in `data` NS, but Celery broker is `tezca-redis` in-namespace | Added intra-namespace Redis egress (`podSelector` with no `namespaceSelector`) + `tezca-redis-ingress` |
| karafiel | karafiel-beat, karafiel-worker | `Temporary failure in name resolution` for `redis.data.svc.cluster.local` | No egress policies existed for beat/worker — only api/web/admin had them | Added `karafiel-beat-egress` and `karafiel-worker-egress` (DNS + PG + Redis) |
| pravara-mes | pravara-api | `Connection refused` to `postgres-pravara:5432` | Egress targeted `data` NS but pravara-mes has in-namespace postgres | Added local `podSelector` egress (session 78) |

**Key diagnostic pattern:** `Connection refused` (ECONNREFUSED) in k3s can mean NetworkPolicy is blocking traffic — k3s sends TCP RST instead of silently dropping packets (unlike Calico which gives ETIMEDOUT).

### DNS Resolution Failure

**Cause**: Tunnel route configured but Cloudflare DNS CNAME never created.

**Diagnosis**:
```bash
dig +short <hostname>
# Empty = no DNS record
```

**Fix**: Create CNAME via Cloudflare API (same zone as madfam.io):
```bash
curl -X POST "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/dns_records" \
  -H "Authorization: Bearer ${CF_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"type":"CNAME","name":"<subdomain>","content":"<tunnel-id>.cfargotunnel.com","proxied":true}'
```

### Hostname Mismatch

**Cause**: Status page checks URL X, but tunnel routes use URL Y.

**Fix**: Align tunnel config (`expected-tunnel-config.json` + live Cloudflare tunnel config) with status page configmap URLs.

### Stale Status Page

**Cause**: Git configmap updated but pod not restarted / ArgoCD not synced.

**Fix**:
```bash
# Force ArgoCD sync
kubectl patch application status-services -n argocd --type merge -p '{"operation":{"sync":{}}}'

# Or manual restart
kubectl rollout restart deployment/status-madfam -n status
```

## Hardware Capacity

### Current Cluster (foundry-cp + foundry-worker-01 + foundry-builder-01)

| Resource | Spec |
|----------|------|
| CPU | See internal-devops for hardware specs |
| RAM | See internal-devops for hardware specs |
| Storage | See internal-devops for hardware specs |
| Role | Control plane + workloads |

### Capacity Thresholds

| Metric | Green | Yellow | Red |
|--------|-------|--------|-----|
| RAM Usage | < 75% (48GB) | 75-85% (48-54GB) | > 85% (54GB+) |
| CPU Usage | < 70% | 70-85% | > 85% |
| Pod Count | < 100 | 100-120 | > 120 |

### Estimating Additional Pod Load

Typical ecosystem service pod: 256-512MB RAM, 100-250m CPU.

| Service Group | Pods | Est. Memory |
|---------------|------|-------------|
| Tezca (web, api, admin, beat, worker, redis, es) | 7 | ~2.5 GB |
| Yantra4D (landing, studio, backend, admin, redis) | 5 | ~2.5 GB |
| Karafiel (web, api, admin, beat, worker) | 5 | ~2.0 GB |
| Forgesight (app, api, admin) | 3 | ~1.5 GB |
| Pravara MES (ui, api, centrifugo, emqx, pg, redis, telemetry) | 7 | ~3.0 GB |
| Madfam CMS | 1 | ~0.25 GB |
| **Total** | **28** | **~11.75 GB** |

### When to Scale

- **> 85% RAM**: Set resource limits on non-critical ecosystem pods, scale down monitoring
- **> 90% RAM**: Add another worker node or upgrade foundry-worker-01 RAM
- **Check live**: `kubectl top nodes` + `kubectl describe node foundry-worker-01 | grep -A 10 "Allocated resources"`

## Service Inventory

### Endpoint → Namespace Mapping

| Endpoint | Namespace | K8s Service |
|----------|-----------|-------------|
| api.enclii.dev | enclii | switchyard-api |
| app.enclii.dev | enclii | switchyard-ui |
| admin.enclii.dev | enclii | dispatch |
| docs.enclii.dev | enclii | docs-site |
| auth.madfam.io | janua | janua-api |
| dhan.am / app.dhan.am | dhanam | dhanam-web |
| api.dhan.am | dhanam | dhanam-api |
| admin.dhan.am | dhanam | dhanam-admin |
| tezca.mx | tezca | tezca-web |
| api.tezca.mx | tezca | tezca-api |
| admin.tezca.mx | tezca | tezca-admin |
| yantra4d.com | yantra4d | yantra4d-landing |
| app.yantra4d.com | yantra4d | yantra4d-studio |
| api.yantra4d.com | yantra4d | yantra4d-backend |
| admin.yantra4d.com | yantra4d | yantra4d-admin |
| kf.madfam.io | karafiel | karafiel-web |
| kf-app.madfam.io | karafiel | karafiel-web |
| kf-api.madfam.io | karafiel | karafiel-api |
| kf-admin.madfam.io | karafiel | karafiel-admin |
| forgesight.quest | forgesight | forgesight-www |
| app.forgesight.quest | forgesight | forgesight-app |
| admin.forgesight.quest | forgesight | forgesight-admin |
| api.forgesight.quest | forgesight | forgesight-api |
| madfam.io | madfam-site | madfam-web |
| cms.madfam.io | madfam-site | madfam-cms |
| mes.madfam.io | pravara-mes | pravara-ui |
| mes-api.madfam.io | pravara-mes | pravara-api |
| mes-admin.madfam.io | pravara-mes | pravara-gateway |

## Phase 2 Cluster-Side Checklist

After git-side fixes are merged:

- [ ] SSH to foundry-cp, run `./scripts/diagnose-outages.sh`
- [ ] Check `kubectl top nodes` — confirm < 85% memory
- [ ] For each failing namespace: check pods, fix image pulls, restart
- [ ] Create Pravara MES DNS CNAMEs (mes, mes-api, mes-admin) via Cloudflare API
- [ ] Create Karafiel DNS CNAMEs (kf, kf-app, kf-api, kf-admin) via Cloudflare API
- [ ] Update live Cloudflare tunnel config with new Karafiel hostnames
- [ ] Restart status page pod: `kubectl rollout restart deployment/status-madfam -n status`
- [ ] Verify all 30 endpoints respond (non-502)
- [ ] Confirm status.madfam.io shows all 9 groups
