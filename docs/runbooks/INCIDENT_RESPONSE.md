---
title: Incident Response Runbook
description: Procedures for detecting, responding to, and resolving production incidents on the Enclii platform
sidebar_position: 7
tags: [operations, runbook, incident-response, on-call]
---

# Incident Response Runbook

**Purpose:** Defines severity classifications, escalation paths, communication protocols, failure mode playbooks, and postmortem processes for production incidents on the Enclii platform.

**Last Updated:** March 2026

**Prerequisites:**
- `kubectl` access to enclii-production cluster (`KUBECONFIG=~/.kube/enclii-production`)
- SSH access to foundry-core via `ssh ssh.madfam.io`
- `enclii` CLI authenticated and configured
- Access to Cloudflare dashboard (dash.cloudflare.com)
- Access to Grafana dashboards (port-forward or tunnel)

---

## 1. Severity Classification

| Severity | Definition | SLO Impact | Response Time | Notification | Examples |
|----------|-----------|------------|---------------|--------------|----------|
| **P1 Critical** | Complete service outage or imminent data loss | Breaching 99.95% API availability | 15 minutes | Immediate page to Platform Lead + On-Call Engineer | Cloudflare tunnel down, database unrecoverable, cluster control plane failure |
| **P2 Major** | Significant degradation or single critical service down | At risk of breaching monthly SLO | 30 minutes | Page On-Call Engineer, notify Platform Lead | Single service outage (Janua, switchyard-api), build pipeline fully stuck, Longhorn volume degraded |
| **P3 Minor** | Non-critical service degraded, workaround available | No SLO breach expected | 2 hours | Notify On-Call Engineer via chat | Ecosystem service degraded, slow queries, non-critical CronJob failures, status page intermittent |
| **P4 Low** | Cosmetic issues, minor bugs, documentation gaps | None | Next business day | Log in issue tracker | UI rendering glitches, non-blocking log warnings, minor metric gaps |

### Severity Determination Flowchart

```
Is external traffic completely blocked?
  YES --> P1 (Cloudflare tunnel, DNS, cluster network)
  NO  --> Can users authenticate?
            NO  --> P1 if all auth is down, P2 if partial
            YES --> Is the control plane API responding?
                      NO  --> P1
                      YES --> Is a single non-critical service affected?
                                YES --> P3
                                NO  --> Is data at risk of loss?
                                          YES --> P1
                                          NO  --> P2
```

---

## 2. Escalation Matrix

| Role | Responsibility | P1 | P2 | P3 | P4 |
|------|---------------|-----|-----|-----|-----|
| **On-Call Engineer** | First responder, initial triage, executes playbooks | Immediate | Immediate | 2 hours | Next business day |
| **Platform Lead** | Escalation point, architectural decisions, coordinates cross-team | Immediate | 30 min | As needed | As needed |
| **Infrastructure Lead** | Node/cluster/storage/network expertise | 15 min | 1 hour | As needed | -- |
| **Security Lead** | Auth, secrets, access control, breach investigation | 15 min (if security-related) | 1 hour | As needed | -- |
| **Communications Lead** | Status page updates, stakeholder notifications | 15 min | 30 min | -- | -- |

### Escalation Rules

1. **P1 auto-escalates** to Platform Lead and Infrastructure Lead simultaneously. Do not wait for the On-Call Engineer to request escalation.
2. **P2 escalates to P1** if unresolved after 1 hour or if a second critical service becomes affected.
3. **P3 escalates to P2** if user-facing impact expands or persists beyond 4 hours.
4. **Any incident involving potential data loss** is automatically P1 regardless of other factors.
5. **Any incident involving security breach or unauthorized access** is automatically P1 and triggers the Security Lead.

---

## 3. Communication Templates

### 3A. Internal Notification (Chat/Page)

```
INCIDENT DECLARED: [P1/P2/P3/P4]

Service:     [affected service(s)]
Impact:      [user-facing description of impact]
Detection:   [how it was detected -- alert, user report, monitoring]
Started:     [timestamp UTC]
Responder:   [name/role of initial responder]
Status:      [Investigating / Identified / Mitigating / Resolved]

Next update: [time of next scheduled update]
```

**Update cadence by severity:**

| Severity | Update Frequency |
|----------|-----------------|
| P1 | Every 15 minutes until mitigated, then every 30 minutes |
| P2 | Every 30 minutes until mitigated, then every hour |
| P3 | Every 2 hours or on status change |
| P4 | On resolution only |

### 3B. Status Page Update (status.enclii.dev)

Post updates via the incidents API:

```bash
# Create incident
curl -X POST https://status.enclii.dev/api/incidents \
  -H "Authorization: Bearer $ADMIN_SECRET" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "[Service Name] - [Brief description]",
    "severity": "major",
    "message": "We are investigating reports of [description of user impact]. Our team is actively working on resolution.",
    "affectedServices": ["switchyard-api", "switchyard-ui"]
  }'
```

**Status page severity mapping:**

| Internal Severity | Status Page Severity | Status Page Wording |
|-------------------|---------------------|---------------------|
| P1 Critical | `major` | "Major outage affecting [services]" |
| P2 Major | `major` or `minor` | "Service degradation affecting [service]" |
| P3 Minor | `minor` | "Minor issue with [service]" |
| P4 Low | Do not post | -- |

**Status progression:** Investigating --> Identified --> Mitigating --> Resolved

### 3C. Post-Resolution Notification

```
INCIDENT RESOLVED: [P1/P2/P3/P4]

Service:     [affected service(s)]
Duration:    [start time] to [end time] ([total duration])
Root Cause:  [one-sentence summary]
Resolution:  [what was done to restore service]
Data Impact: [any data loss or corruption -- state "None" explicitly if none]

Postmortem:  [Scheduled for YYYY-MM-DD / Not required (P3/P4)]
```

---

## 4. Failure Mode Playbooks

### 4.1 Cloudflare Tunnel Down

**Severity:** P1 -- All external traffic is cut. No public endpoint is reachable.

**Detection signals:**
- External health checks fail for all `*.enclii.dev` and `*.madfam.io` domains
- Prometheus alert: no scrape data from external probes
- Status page unreachable from outside the cluster
- User reports of connection timeouts

**Diagnosis:**

```bash
# Step 1: Check cloudflared pod status
kubectl get pods -n cloudflare -l app=cloudflared

# Step 2: Check cloudflared logs for connection errors
kubectl logs -n cloudflare -l app=cloudflared --tail=50

# Step 3: Verify the tunnel configuration is mounted
kubectl describe deployment cloudflared -n cloudflare

# Step 4: Check if the tunnel connector is registered with Cloudflare
kubectl exec -n cloudflare deploy/cloudflared -- cloudflared tunnel info 2>/dev/null || echo "Cannot query tunnel info"
```

**Resolution steps:**

```bash
# Option A: Restart cloudflared pods (fixes transient connection issues)
kubectl rollout restart deployment/cloudflared -n cloudflare
kubectl rollout status deployment/cloudflared -n cloudflare --timeout=120s

# Option B: If pods are in CrashLoopBackOff, check the tunnel secret
kubectl get secret tunnel-credentials -n cloudflare
# If missing, the tunnel credential must be recreated from Cloudflare dashboard

# Option C: If Cloudflare edge is the issue (rare)
# 1. Check https://www.cloudflarestatus.com/ for Cloudflare outages
# 2. If Cloudflare is down, this is outside our control -- update status page and wait
# 3. If specific PoP is affected, consider Cloudflare support ticket
```

**Verification:**

```bash
# From outside the cluster (or a machine not on the cluster network)
curl -s -o /dev/null -w "%{http_code}" https://api.enclii.dev/health
# Expected: 200

curl -s -o /dev/null -w "%{http_code}" https://app.enclii.dev
# Expected: 200

# Verify all tunnel routes are serving
for domain in api.enclii.dev app.enclii.dev admin.enclii.dev auth.madfam.io status.enclii.dev; do
  echo "$domain: $(curl -s -o /dev/null -w '%{http_code}' https://$domain)"
done
```

**Escalation:** If not resolved within 15 minutes, SSH into foundry-core to verify node-level network connectivity:

```bash
ssh ssh.madfam.io
# Once on foundry-core:
systemctl status k3s
crictl pods | grep cloudflared
```

---

### 4.2 Database Unavailable (PostgreSQL)

**Severity:** P1 if control plane is affected (switchyard-api cannot serve requests). P2 if limited to a single ecosystem service.

**Detection signals:**
- `enclii logs switchyard-api` shows `connection refused` or `too many connections` errors
- API returns HTTP 500 on all data-dependent endpoints
- Prometheus alert: `PostgresDown` or `PostgresHighConnectionCount`

**Diagnosis:**

```bash
# Step 1: Check PostgreSQL pod status
kubectl get pods -n data -l app=postgres

# Step 2: Check PostgreSQL logs
kubectl logs -n data -l app=postgres --tail=100

# Step 3: Check PVC status (disk full is a common cause)
kubectl get pvc -n data
kubectl exec -n data deploy/postgres -- df -h /var/lib/postgresql/data

# Step 4: Check connection count
kubectl exec -n data deploy/postgres -- psql -U postgres -c "SELECT count(*) FROM pg_stat_activity;"

# Step 5: Check for long-running queries
kubectl exec -n data deploy/postgres -- psql -U postgres -c \
  "SELECT pid, now() - pg_stat_activity.query_start AS duration, query
   FROM pg_stat_activity
   WHERE state != 'idle'
   ORDER BY duration DESC
   LIMIT 10;"
```

**Resolution steps:**

```bash
# Scenario A: Pod crashed but PVC is healthy
kubectl delete pod -n data -l app=postgres
# Kubernetes will recreate the pod. Wait for it to be Ready.
kubectl wait --for=condition=Ready pod -n data -l app=postgres --timeout=120s

# Scenario B: Disk full
# 1. Identify and remove old WAL files or temp files
kubectl exec -n data deploy/postgres -- sh -c "du -sh /var/lib/postgresql/data/*"
# 2. If WAL accumulation, force a checkpoint
kubectl exec -n data deploy/postgres -- psql -U postgres -c "CHECKPOINT;"
# 3. If persistent, resize the PVC (Longhorn supports online expansion)
kubectl patch pvc postgres-data -n data -p '{"spec":{"resources":{"requests":{"storage":"20Gi"}}}}'

# Scenario C: Too many connections
# Terminate idle connections
kubectl exec -n data deploy/postgres -- psql -U postgres -c \
  "SELECT pg_terminate_backend(pid)
   FROM pg_stat_activity
   WHERE state = 'idle'
   AND query_start < now() - interval '10 minutes';"

# Scenario D: Data corruption -- restore from backup
# See docs/runbooks/DATABASE_RECOVERY.md for full restore procedure
```

**Verification:**

```bash
# Verify database accepts connections
kubectl exec -n data deploy/postgres -- psql -U postgres -c "SELECT 1;"
# Expected: returns 1

# Verify API health
curl -s https://api.enclii.dev/health | jq .
# Expected: {"status":"healthy","database":"connected"}

# Verify through CLI
enclii ps --wide
```

---

### 4.3 Authentication Service (Janua) Down

**Severity:** P1 if no cached sessions remain (all logins impossible). P2 if existing sessions still work but new logins fail.

**Detection signals:**
- HTTP 401/403 errors across multiple services simultaneously
- `enclii auth verify` fails
- JWKS endpoint unreachable: `curl https://auth.madfam.io/.well-known/jwks.json` returns error
- Users report being unable to log in to app.enclii.dev or admin.enclii.dev

**Diagnosis:**

```bash
# Step 1: Check Janua pod status
kubectl get pods -n janua -l app=janua-api

# Step 2: Check Janua logs for errors
kubectl logs -n janua -l app=janua-api --tail=100

# Step 3: Verify JWKS endpoint from inside the cluster
kubectl run curl-test --rm -it --image=curlimages/curl --restart=Never -- \
  curl -s https://auth.madfam.io/.well-known/jwks.json

# Step 4: Check if the issue is Janua itself or its dependencies
# Check Janua's database connection
kubectl logs -n janua -l app=janua-api --tail=50 | grep -i "database\|postgres\|connection"

# Step 5: Check Redis session store
kubectl get pods -n data -l app=redis
kubectl exec -n data deploy/redis -- redis-cli ping
# Expected: PONG
```

**Resolution steps:**

```bash
# Option A: Restart Janua pods (fixes transient startup issues)
kubectl rollout restart deployment/janua-api -n janua
kubectl rollout status deployment/janua-api -n janua --timeout=120s

# Option B: If Redis is down (sessions lost but auth still works for new logins)
kubectl delete pod -n data -l app=redis
kubectl wait --for=condition=Ready pod -n data -l app=redis --timeout=60s
# Note: All existing sessions will be invalidated. Users must re-authenticate.

# Option C: If Janua's database is the issue, follow Playbook 4.2 first

# Option D: If JWKS keys are corrupted or rotated unexpectedly
# Check Janua configuration for key rotation settings
kubectl get configmap -n janua
kubectl describe configmap janua-config -n janua
# If keys were rotated, all services caching old keys need restart
kubectl rollout restart deployment/switchyard-api -n enclii
kubectl rollout restart deployment/switchyard-ui -n enclii
kubectl rollout restart deployment/dispatch -n dispatch
```

**Verification:**

```bash
# Verify JWKS endpoint
curl -s https://auth.madfam.io/.well-known/jwks.json | jq '.keys | length'
# Expected: >= 1

# Verify authentication flow
enclii auth verify
# Expected: Token valid, displays user info

# Verify login works end-to-end
curl -s -o /dev/null -w "%{http_code}" https://app.enclii.dev
# Expected: 200 (or 302 redirect to auth)
```

---

### 4.4 API Control Plane Overloaded (switchyard-api)

**Severity:** P2 by default. Escalate to P1 if deployments and builds are fully blocked.

**Detection signals:**
- P95 latency > 2 seconds on API endpoints
- Error rate > 5% on `api.enclii.dev`
- `enclii ps` responds slowly or times out
- Prometheus alerts: `APIHighLatency`, `APIHighErrorRate`
- Grafana dashboard shows elevated request queuing

**Diagnosis:**

```bash
# Step 1: Check pod resource usage
kubectl top pods -n enclii -l app=switchyard-api

# Step 2: Check for pod restarts (OOMKilled, CrashLoopBackOff)
kubectl get pods -n enclii -l app=switchyard-api -o wide

# Step 3: Check recent logs for error patterns
enclii logs switchyard-api --tail=200 --level error

# Step 4: Check for slow database queries
kubectl exec -n data deploy/postgres -- psql -U postgres -c \
  "SELECT pid, now() - query_start AS duration, left(query, 80)
   FROM pg_stat_activity
   WHERE state = 'active' AND query_start < now() - interval '5 seconds'
   ORDER BY duration DESC;"

# Step 5: Check if HPA is active and at limits
kubectl get hpa -n enclii

# Step 6: Check for unusual traffic patterns (webhook storms, etc.)
enclii logs switchyard-api --tail=500 | grep -c "POST /v1/webhooks"
```

**Resolution steps:**

```bash
# Option A: Scale up replicas manually if HPA is not responding fast enough
kubectl scale deployment/switchyard-api -n enclii --replicas=3
# Wait for new pods to become ready
kubectl rollout status deployment/switchyard-api -n enclii --timeout=120s

# Option B: Kill slow queries if database is the bottleneck
kubectl exec -n data deploy/postgres -- psql -U postgres -c \
  "SELECT pg_terminate_backend(pid)
   FROM pg_stat_activity
   WHERE state = 'active'
   AND query_start < now() - interval '30 seconds'
   AND query NOT LIKE '%pg_stat_activity%';"

# Option C: If a specific endpoint is being hammered (webhook storm)
# Temporarily block at the Cloudflare level via WAF rule, or
# restart to clear accumulated goroutine/connection state
kubectl rollout restart deployment/switchyard-api -n enclii

# Option D: If OOMKilled, increase memory limits
kubectl patch deployment switchyard-api -n enclii --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"1Gi"}]'
```

**Verification:**

```bash
# Check API health and response time
time curl -s https://api.enclii.dev/health | jq .
# Expected: < 500ms response, {"status":"healthy"}

# Verify deployment operations work
enclii ps --wide
# Expected: All services listed, response within 2 seconds

# Check error rate has dropped
enclii logs switchyard-api --tail=100 --level error | wc -l
# Expected: Significantly fewer errors than during incident
```

---

### 4.5 Build Pipeline Stuck (Roundhouse)

**Severity:** P2 -- Deployments are blocked. No code can ship until resolved.

**Detection signals:**
- Builds queued for more than 10 minutes with no progress
- `enclii builds logs --latest` shows no output or stuck state
- Prometheus alert: `BuildQueueDepth` > 5 for > 10 minutes
- Users report pushes to GitHub not triggering builds

**Diagnosis:**

```bash
# Step 1: Check Roundhouse worker pods
kubectl get pods -n enclii -l app=roundhouse

# Step 2: Check Roundhouse logs
kubectl logs -n enclii -l app=roundhouse --tail=100

# Step 3: Check for stuck build jobs
kubectl get jobs -n enclii-builds --sort-by=.metadata.creationTimestamp
kubectl get pods -n enclii-builds --field-selector=status.phase!=Succeeded

# Step 4: Check Redis queue depth (build queue)
kubectl exec -n data deploy/redis -- redis-cli llen build:queue
kubectl exec -n data deploy/redis -- redis-cli llen build:processing

# Step 5: Check if webhook delivery is working
enclii logs switchyard-api --tail=100 | grep "webhook"

# Step 6: Check for resource pressure on builder node
kubectl top node foundry-builder-01
kubectl describe node foundry-builder-01 | grep -A5 "Allocated resources"
```

**Resolution steps:**

```bash
# Option A: Restart Roundhouse workers
kubectl rollout restart deployment/roundhouse -n enclii
kubectl rollout status deployment/roundhouse -n enclii --timeout=120s

# Option B: Clean up stuck build jobs
# List failed/stuck jobs
kubectl get jobs -n enclii-builds --field-selector=status.successful=0
# Delete stuck jobs (they will be retried)
kubectl delete jobs -n enclii-builds --field-selector=status.successful=0

# Option C: Clear Redis build queue if corrupted
kubectl exec -n data deploy/redis -- redis-cli del build:processing
# Note: This drops in-flight builds. They will need to be retriggered via git push.

# Option D: If builder node is resource-exhausted
# Clear dangling images and build cache
ssh ssh.madfam.io
# On foundry-builder-01 (via kubectl or SSH):
kubectl debug node/foundry-builder-01 -it --image=busybox -- sh -c "crictl rmi --prune"

# Option E: If webhook delivery failed, retrigger builds
# Push an empty commit to the affected repo, or use GitHub UI to redeliver the webhook
```

**Verification:**

```bash
# Verify Roundhouse is processing
kubectl logs -n enclii -l app=roundhouse --tail=20
# Expected: Shows build processing activity

# Trigger a test build and monitor
enclii builds logs --latest --follow
# Expected: Build progresses through stages

# Verify queue is draining
kubectl exec -n data deploy/redis -- redis-cli llen build:queue
# Expected: 0 or decreasing
```

---

## 5. General Incident Procedures

### On Detection

1. **Assess severity** using the classification table and flowchart in Section 1.
2. **Declare the incident** using the internal notification template (Section 3A).
3. **Update the status page** for P1 and P2 incidents (Section 3B).
4. **Begin the relevant playbook** from Section 4, or perform general triage if no playbook matches.
5. **Set a timer** for the next scheduled update per the cadence table.

### During Resolution

1. **Log all actions** with timestamps in the incident channel. Include commands run and their output.
2. **Communicate proactively** -- push updates even if the status has not changed ("Still investigating, no new findings").
3. **Avoid making unrelated changes** during an active incident. Focus only on restoring service.
4. **Take notes for the postmortem** as you go. Memory fades quickly after an incident.

### On Resolution

1. **Verify the fix** using the verification steps in the relevant playbook.
2. **Monitor for recurrence** for at least 30 minutes after resolution.
3. **Update the status page** to "Resolved" with a summary.
4. **Send the post-resolution notification** (Section 3C).
5. **Schedule a postmortem** for P1 and P2 incidents within 3 business days.

---

## 6. Postmortem Process

### When Required

| Severity | Postmortem Required | Deadline |
|----------|-------------------|----------|
| P1 | Yes, mandatory | Within 3 business days |
| P2 | Yes, mandatory | Within 5 business days |
| P3 | Optional, at team discretion | Within 2 weeks if held |
| P4 | No | -- |

### Postmortem Template

Store completed postmortems in `docs/postmortems/YYYY-MM-DD-brief-title.md`.

```markdown
# Postmortem: [Brief Title]

**Date of Incident:** YYYY-MM-DD
**Duration:** HH:MM (from detection to resolution)
**Severity:** P1/P2/P3
**Author:** [Name]
**Attendees:** [Names of participants in the postmortem review]

## Summary

[2-3 sentences describing what happened, the impact, and how it was resolved.]

## Impact

- **Users affected:** [number or description]
- **Duration of user-facing impact:** [minutes/hours]
- **SLO impact:** [which SLO was breached and by how much, or "No SLO breach"]
- **Data loss:** [Yes/No -- describe if yes]

## Timeline (UTC)

| Time | Event |
|------|-------|
| HH:MM | [First detection signal] |
| HH:MM | [Incident declared] |
| HH:MM | [Key diagnostic step or finding] |
| HH:MM | [Mitigation applied] |
| HH:MM | [Service restored] |
| HH:MM | [Monitoring confirmed stable] |

## Root Cause

[Detailed technical explanation of what caused the incident. Be specific -- include
the component, the failure mode, and why existing safeguards did not prevent it.]

## Contributing Factors

- [Factor 1: e.g., "No alerting for disk usage above 80%"]
- [Factor 2: e.g., "Single-replica database with no automatic failover"]
- [Factor 3: e.g., "Runbook did not cover this specific failure mode"]

## What Went Well

- [Positive observation 1: e.g., "Detection was fast due to external health checks"]
- [Positive observation 2: e.g., "Runbook for database recovery was accurate and complete"]

## What Went Poorly

- [Negative observation 1: e.g., "Took 20 minutes to find the correct kubectl context"]
- [Negative observation 2: e.g., "Status page was not updated until 30 minutes in"]

## Action Items

| Action | Owner | Priority | Deadline | Tracking |
|--------|-------|----------|----------|----------|
| [Specific, measurable action] | [Role/Name] | P1/P2/P3 | YYYY-MM-DD | [Issue link] |
| [Specific, measurable action] | [Role/Name] | P1/P2/P3 | YYYY-MM-DD | [Issue link] |

## Lessons Learned

[Key takeaways that should inform future incident response, architecture decisions,
or operational practices. Focus on systemic improvements, not individual blame.]
```

### Postmortem Principles

1. **Blameless.** Focus on systemic causes, not individual mistakes. The goal is to improve the system, not assign fault.
2. **Thorough.** Investigate until you understand the root cause, not just the proximate trigger.
3. **Actionable.** Every postmortem must produce at least one concrete action item with an owner and deadline.
4. **Shared.** Completed postmortems are shared with the full team. The value is in the collective learning.
5. **Tracked.** Action items are filed as issues and tracked to completion. A postmortem without follow-through is wasted effort.

---

## Related Documentation

- [Disaster Recovery](./DISASTER_RECOVERY.md)
- [Database Recovery](./DATABASE_RECOVERY.md)
- [Longhorn Volume Recovery](./LONGHORN_VOLUME_RECOVERY.md)
- [Cluster Remediation Operations](./CLUSTER_REMEDIATION_OPS.md)
- [Backup Coverage](./BACKUP_COVERAGE.md)
- [Infrastructure Anatomy](../infrastructure/INFRA_ANATOMY.md)
