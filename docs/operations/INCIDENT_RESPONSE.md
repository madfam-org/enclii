# Incident Response Runbook

This document defines how the Enclii team detects, responds to, and recovers from production incidents. All on-call personnel must be familiar with these procedures.

**Infrastructure context:** 2-node k3s cluster (Hetzner AX41-NVME + VPS builder), Cloudflare tunnel ingress, self-hosted PostgreSQL and Redis, ArgoCD GitOps, Janua SSO.

---

## 1. Severity Classification

| Level | Definition | Response Time | Update Cadence | Examples |
|-------|-----------|---------------|----------------|----------|
| P1 - Critical | Complete service outage or data breach | 15 minutes | Every 30 min | api.enclii.dev unreachable; database corruption; credential leak; cluster node unresponsive |
| P2 - Major | Degraded service affecting multiple users | 30 minutes | Every 1 hour | Build pipeline stuck; auth/SSO failures; deployment reconciler broken; Longhorn volume degraded |
| P3 - Minor | Limited impact, workaround available | 4 hours | Daily | Single service deployment failure; non-critical pod crash-looping; slow API responses |
| P4 - Low | Cosmetic or minor issue, no user impact | Next business day | As needed | UI rendering glitch; log noise; outdated docs; non-user-facing config drift |

### Severity Decision Guide

1. Can users deploy or manage services? If no, at least P2.
2. Is customer data at risk? If yes, P1.
3. Is the control plane (api.enclii.dev) down? P1.
4. Is only one non-critical subsystem affected with a workaround? P3.

---

## 2. Escalation Matrix

| Severity | First Responder | Escalation (if no progress in 30 min) | Communication Channel |
|----------|----------------|---------------------------------------|----------------------|
| P1 | On-call engineer | All engineering + founder | Slack #incidents (war room), phone call |
| P2 | On-call engineer | Senior engineer | Slack #incidents |
| P3 | Assigned engineer | On-call engineer | Slack #engineering |
| P4 | Backlog ticket | N/A | GitHub Issues |

### Contact Priorities

- **Primary on-call:** Rotates weekly. Check the on-call schedule in Slack #on-call.
- **Secondary escalation:** Founder / infrastructure lead.
- **External vendors:** Hetzner support (hardware), Cloudflare support (tunnel/DNS).

---

## 3. Response Procedures

### 3.1 General Incident Lifecycle

```
Detection --> Triage --> Containment --> Eradication --> Recovery --> Post-Incident
```

**Detection:** Alerts from monitoring, customer reports, or automated health checks.

**Triage (first 15 minutes):**
1. Assign severity using the classification table above.
2. Open an incident thread in Slack #incidents with: severity, affected services, initial observations.
3. Claim the incident: "I am leading this incident."

**Containment:** Stop the bleeding. Prevent further damage without necessarily fixing root cause.

**Eradication:** Remove the root cause.

**Recovery:** Restore normal service and verify with health checks.

**Post-Incident:** Conduct review within 48 hours (see Section 5).

---

### 3.2 API Outage (api.enclii.dev)

**Symptoms:** HTTP 5xx from API, health check failures, UI cannot load data.

```bash
# 1. Verify the outage
curl -s https://api.enclii.dev/health | jq .

# 2. Check pod status
kubectl get pods -n enclii -l app=switchyard-api
kubectl describe pod -n enclii -l app=switchyard-api

# 3. Check logs for errors
kubectl logs -n enclii -l app=switchyard-api --tail=100

# 4. Check if Cloudflare tunnel is healthy
kubectl get pods -n cloudflare -l app=cloudflared
kubectl logs -n cloudflare -l app=cloudflared --tail=50

# 5. Restart if pod is crash-looping
kubectl rollout restart deploy/switchyard-api -n enclii
kubectl rollout status deploy/switchyard-api -n enclii

# 6. If tunnel is down, restart cloudflared
kubectl rollout restart deploy/cloudflared -n cloudflare
```

**Escalation trigger:** If API is not restored within 15 minutes, escalate to P1 war room.

---

### 3.3 Database Failure (PostgreSQL)

**Symptoms:** API returns database connection errors, migrations fail, data queries timeout.

```bash
# 1. Check PostgreSQL pod
kubectl get pods -n enclii -l app=postgresql
kubectl logs -n enclii -l app=postgresql --tail=100

# 2. Check PVC status (Longhorn)
kubectl get pvc -n enclii
kubectl get volumes.longhorn.io -n longhorn-system

# 3. Test connectivity from API pod
kubectl exec -n enclii deploy/switchyard-api -- /app/healthcheck db

# 4. If pod is down, check events
kubectl describe pod -n enclii -l app=postgresql

# 5. If storage issue, check Longhorn
kubectl get pods -n longhorn-system
kubectl port-forward svc/longhorn-frontend -n longhorn-system 8081:80
```

**Recovery:** If the PostgreSQL pod cannot start due to volume issues, restore from the latest R2 backup. Daily backups are stored in Cloudflare R2.

**Data loss protocol:** If data loss is confirmed, immediately classify as P1 and notify all stakeholders.

---

### 3.4 Auth/SSO Failure (Janua)

**Symptoms:** Users cannot log in, JWT validation errors, JWKS endpoint unreachable.

```bash
# 1. Test JWKS endpoint
curl -s https://auth.madfam.io/.well-known/jwks.json | jq .

# 2. Check Janua pods
kubectl get pods -n janua -l app=janua-api
kubectl logs -n janua -l app=janua-api --tail=100

# 3. Verify OIDC configuration
kubectl get configmap -n enclii switchyard-config -o yaml | grep OIDC

# 4. Test token verification
# From a working session, decode and inspect the JWT at jwt.io

# 5. Restart Janua if needed
kubectl rollout restart deploy/janua-api -n janua
```

**Workaround:** If Janua is fully down, API key authentication remains available for CI/CD pipelines. Interactive users will be blocked until SSO is restored.

---

### 3.5 Security Breach

**Symptoms:** Unauthorized access detected, suspicious API calls, credential exposure in logs or repos.

**Immediate actions (do not delay):**

1. **Contain:** Revoke compromised credentials immediately. Rotate all API keys and secrets.
2. **Isolate:** If a node is compromised, cordon it: `kubectl cordon <node-name>`
3. **Preserve evidence:** Do NOT delete logs. Export them to a secure location.
4. **Notify:** Escalate to P1 immediately. Inform founder and all engineers.

```bash
# Rotate Janua signing keys
# (Follow Janua key rotation procedure in janua repo docs)

# Revoke all active API keys
kubectl exec -n enclii deploy/switchyard-api -- /app/admin revoke-all-keys

# Check for unauthorized pods or workloads
kubectl get pods --all-namespaces | grep -v Running
kubectl get pods --all-namespaces -o wide
```

**Post-breach:** Full audit of access logs, timeline reconstruction, and disclosure assessment.

---

### 3.6 Build Pipeline Failure

**Symptoms:** Builds stuck in queue, Roundhouse workers not processing, GitHub webhooks not arriving.

```bash
# 1. Check build jobs
kubectl get jobs -n enclii-builds
kubectl logs -n enclii-builds job/<latest-job> --tail=100

# 2. Check Roundhouse workers
kubectl get pods -n enclii -l app=roundhouse
kubectl logs -n enclii -l app=roundhouse --tail=100

# 3. Check GitHub webhook delivery
# Go to GitHub repo Settings > Webhooks > Recent Deliveries

# 4. Check builder node status
kubectl get nodes
kubectl describe node foundry-builder-01

# 5. If builder node is NotReady, check k3s agent
ssh foundry-builder-01 "systemctl status k3s-agent"
```

---

## 4. Communication Templates

### 4.1 Internal Status Update (Slack #incidents)

```
INCIDENT UPDATE - [P1/P2/P3] - [Short Title]
Time: [HH:MM UTC]
Status: [Investigating / Identified / Monitoring / Resolved]
Impact: [What users are experiencing]
Current actions: [What the team is doing right now]
Next update: [When]
Lead: [Name]
```

### 4.2 External Customer Notification (status.enclii.dev)

**Investigating:**
```
We are investigating reports of [degraded performance / service disruption]
affecting [specific service]. Our team is actively working to identify the
cause. We will provide an update within [30 minutes / 1 hour].
```

**Identified:**
```
We have identified the cause of the [issue description]. Our team is
implementing a fix. [Service] may be intermittently unavailable during
this period. We expect resolution by [estimated time].
```

**Resolved:**
```
The issue affecting [service] has been resolved as of [HH:MM UTC].
All systems are operating normally. We will publish a full incident
report within 48 hours. We apologize for any inconvenience.
```

---

## 5. Post-Incident Review

Conduct a blameless retrospective within 48 hours of resolution for all P1 and P2 incidents. P3 incidents are reviewed at the team's discretion.

### 5.1 Retrospective Template

```
# Post-Incident Review: [Incident Title]

Date of incident: [YYYY-MM-DD]
Duration: [start time] to [end time] ([total minutes])
Severity: [P1/P2/P3]
Lead responder: [Name]
Review date: [YYYY-MM-DD]
Attendees: [Names]

## Summary
[2-3 sentence description of what happened and the impact.]

## Timeline
| Time (UTC) | Event |
|------------|-------|
| HH:MM | [First detection signal] |
| HH:MM | [Triage began] |
| HH:MM | [Root cause identified] |
| HH:MM | [Fix deployed] |
| HH:MM | [Service restored] |
| HH:MM | [Incident closed] |

## Root Cause
[Technical explanation of what went wrong and why.]

## Contributing Factors
- [Factor 1: e.g., missing health check, insufficient alerting]
- [Factor 2: e.g., single point of failure, unclear runbook]

## What Went Well
- [Positive aspect of the response]

## What Could Be Improved
- [Gap or delay identified during response]

## Action Items
| Action | Owner | Due Date | Status |
|--------|-------|----------|--------|
| [Specific remediation task] | [Name] | [Date] | Open |
| [Monitoring improvement] | [Name] | [Date] | Open |
| [Runbook update] | [Name] | [Date] | Open |
```

### 5.2 Review Principles

- **Blameless:** Focus on systems and processes, not individuals. People make the best decisions they can with available information.
- **Thorough:** Reconstruct the full timeline from detection through resolution.
- **Actionable:** Every review must produce at least one concrete action item with an owner and due date.
- **Shared:** Publish the review to the team. Transparency improves collective learning.

---

## Appendix: Quick Reference

| Scenario | First Command |
|----------|--------------|
| API down | `kubectl get pods -n enclii -l app=switchyard-api` |
| Database down | `kubectl get pods -n enclii -l app=postgresql` |
| Auth broken | `curl -s https://auth.madfam.io/.well-known/jwks.json` |
| Tunnel down | `kubectl get pods -n cloudflare -l app=cloudflared` |
| Node unreachable | `kubectl get nodes` |
| ArgoCD out of sync | `kubectl get applications -n argocd` |
| Storage degraded | `kubectl get volumes.longhorn.io -n longhorn-system` |
| Builds stuck | `kubectl get jobs -n enclii-builds` |
