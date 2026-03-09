# Service Outage Remediation Playbook

Last updated: 2026-03-09

## Overview

This playbook covers diagnosing and remediating service outages visible on `status.madfam.io`. The status page monitors 30 endpoints across 9 project groups.

## Quick Start

```bash
# From foundry-core (or any host with KUBECONFIG)
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

### Current Cluster (foundry-core)

| Resource | Spec |
|----------|------|
| CPU | AMD Ryzen 5 3600 (6C/12T) |
| RAM | 64 GB |
| Storage | 2x 512GB NVMe |
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
| Tezca (web, api, admin) | 3 | ~1.5 GB |
| Yantra4D (landing, studio, api, admin) | 4 | ~2.0 GB |
| Karafiel (web, api, admin) | 3 | ~1.5 GB |
| Forgesight (app, api, admin) | 3 | ~1.5 GB |
| Pravara MES (ui, api, gateway) | 3 | ~1.5 GB |
| Madfam CMS | 1 | ~0.25 GB |
| **Total** | **17** | **~8.25 GB** |

### When to Scale

- **> 85% RAM**: Set resource limits on non-critical ecosystem pods, scale down monitoring
- **> 90% RAM**: Add a 3rd worker node or upgrade foundry-core RAM
- **Check live**: `kubectl top nodes` + `kubectl describe node foundry-core | grep -A 10 "Allocated resources"`

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
| 4d.madfam.io | yantra4d | yantra4d-landing |
| 4d-app.madfam.io | yantra4d | yantra4d-studio |
| 4d-api.madfam.io | yantra4d | yantra4d-api |
| 4d-admin.madfam.io | yantra4d | yantra4d-admin |
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

- [ ] SSH to foundry-core, run `./scripts/diagnose-outages.sh`
- [ ] Check `kubectl top nodes` — confirm < 85% memory
- [ ] For each failing namespace: check pods, fix image pulls, restart
- [ ] Create Pravara MES DNS CNAMEs (mes, mes-api, mes-admin) via Cloudflare API
- [ ] Create Karafiel DNS CNAMEs (kf, kf-app, kf-api, kf-admin) via Cloudflare API
- [ ] Update live Cloudflare tunnel config with new Karafiel hostnames
- [ ] Restart status page pod: `kubectl rollout restart deployment/status-madfam -n status`
- [ ] Verify all 30 endpoints respond (non-502)
- [ ] Confirm status.madfam.io shows all 9 groups
