# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x.x   | :white_check_mark: |

## Reporting a Vulnerability

Enclii is a deployment platform that handles sensitive infrastructure and application secrets. We take security extremely seriously.

### How to Report

**Please DO NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via email to: **security@madfam.io**

Include the following information:
- Type of issue (e.g., container escape, privilege escalation, secret exposure, etc.)
- Full paths of source file(s) related to the issue
- Location of the affected source code (tag/branch/commit or direct URL)
- Any special configuration required to reproduce the issue
- Step-by-step instructions to reproduce the issue
- Proof-of-concept or exploit code (if possible)
- Impact of the issue, including how an attacker might exploit it

### Response Timeline

- **Initial Response**: Within 24 hours (critical infrastructure)
- **Status Update**: Within 72 hours
- **Resolution Target**: Within 14 days for critical issues

### Bug Bounty

We are working on establishing a bug bounty program. In the meantime, we offer:
- Public acknowledgment (with permission)
- Swag and recognition
- Potential financial rewards for critical findings

## Security Architecture

### Infrastructure Security
- **Network Isolation**: Services run in isolated network namespaces
- **Secret Management**: Encrypted at rest and in transit
- **TLS Everywhere**: All internal and external communication encrypted
- **Cloudflare Tunnel**: No exposed ports, zero-trust networking

### Container Security
- **Rootless Containers**: Enforced via Kyverno `require-run-as-nonroot` policy (Enforce mode)
- **Capability Dropping**: Enforced via Kyverno `restrict-capabilities` policy (Enforce mode) — all containers must drop `ALL` capabilities
- **Read-only Filesystems**: Containers use read-only root filesystem with explicit `emptyDir` mounts for writable paths
- **Resource Limits**: CPU/memory limits prevent resource exhaustion
- **Security Scanning**: Images scanned for vulnerabilities

### Access Control
- **RBAC**: Role-based access control for all resources
- **Audit Logging**: All actions logged and traceable
- **MFA Support**: Multi-factor authentication via Janua

## Security Best Practices for Enclii Users

### Secrets Management
- Never commit secrets to git
- Use Enclii's secret management for all sensitive values
- Rotate secrets regularly
- Use separate secrets per environment

### Deployment Security
- Enable deployment approvals for production
- Use canary deployments for risk mitigation
- Configure resource limits appropriately
- Enable health checks and auto-rollback

### Network Security
- Use internal networking for service-to-service communication
- Configure appropriate rate limits
- Enable WAF rules for public endpoints

## Compliance

Enclii infrastructure is designed with:
- SOC 2 Type II principles in mind (see [`docs/compliance/SOC2_CONTROLS_MAPPING.md`](docs/compliance/SOC2_CONTROLS_MAPPING.md))
- GDPR data residency awareness
- ISO 27001 security controls

### SOC 2 Remediation Highlights
- **Session revocation fail-closed**: When Redis is unavailable, sessions are treated as revoked (deny access) to prevent unauthorized access
- **Audit log persistence**: File-based JSONL fallback (`/var/log/enclii/audit-fallback.jsonl`) ensures audit entries survive database outages, with a 30-second recovery worker for replay
- **Incident response**: See [`docs/runbooks/INCIDENT_RESPONSE.md`](docs/runbooks/INCIDENT_RESPONSE.md)

## Git History IP Exposure

### Decision

Server IP addresses for the Enclii bare-metal infrastructure (Hetzner dedicated servers) were present in early git commits within Terraform configuration, Cloudflare tunnel configs, and deployment scripts. After evaluation, the decision was made to **keep the existing git history intact** rather than rewrite it.

### Rationale

1. **History rewriting is destructive**: Force-pushing a rewritten history would invalidate all existing commit SHAs, break references in issues/PRs, and disrupt any downstream forks or CI caches. The operational risk of a full `git filter-repo` exceeds the exposure risk.
2. **IPs alone are insufficient for attack**: The exposed values are server IPs only. No SSH keys, API tokens, database credentials, or TLS private keys were ever committed. An attacker with only an IP address cannot gain access to the infrastructure.
3. **Cloudflare Tunnel eliminates direct exposure**: All inbound traffic routes through Cloudflare Tunnel (see mitigation below). The servers have **no publicly exposed ports** -- firewall rules drop all ingress except Cloudflare tunnel traffic and SSH from a hardcoded allowlist. Even with the IP, there is no open TCP port to connect to.
4. **Defense in depth**: Multiple independent layers (tunnel, firewall, NetworkPolicy, Kyverno admission control, RBAC) mean that IP knowledge does not provide a viable attack path.

### Accepted Risk

- **Risk level**: Low
- **Attack surface**: IP addresses of Hetzner dedicated servers visible in git history
- **Impact if exploited**: None in isolation -- no ports are exposed, no credentials accompany the IPs
- **Review cadence**: Re-evaluated quarterly during infrastructure security reviews

## Cloudflare Tunnel Mitigation

All production traffic enters the cluster through Cloudflare Tunnel, implementing a zero-trust networking model.

### Architecture

```
Internet --> Cloudflare Edge (TLS termination, DDoS, WAF)
         --> cloudflared pods (2 replicas, RollingUpdate)
         --> Kubernetes ClusterIP Services (port 80)
         --> Application containers (targetPort 4xxx)
```

### Key Properties

- **Zero exposed node ports**: All host-level ports are firewalled. The only ingress path is through the Cloudflare tunnel.
- **Tunnel authentication**: `cloudflared` authenticates to Cloudflare using a per-tunnel credential file. The credential is stored as a Kubernetes Secret managed via External Secrets Operator (ESO) backed by HashiCorp Vault.
- **Route isolation**: Each service is mapped to a specific hostname in the tunnel configuration (`infra/k8s/production/cloudflared-unified.yaml`). Unknown hostnames receive a 404.
- **DDoS protection**: Cloudflare edge absorbs volumetric attacks before traffic reaches the tunnel.
- **WAF rules**: Cloudflare Web Application Firewall rules are enabled for public-facing endpoints.
- **mTLS readiness**: The tunnel supports Cloudflare Access policies for service-to-service authentication when needed.

### Firewall Rules

Server-level firewall (iptables/nftables) enforces:
- **ALLOW**: Cloudflare tunnel traffic (outbound-initiated, no inbound ports required)
- **ALLOW**: SSH from a hardcoded IP allowlist (infrastructure operators only)
- **DROP**: All other inbound traffic

## Password and Secret Rotation Policy

### Rotation Schedule

| Secret Type | Rotation Frequency | Responsible Party | Method |
|---|---|---|---|
| Cloudflare tunnel credentials | Annually or on compromise | Infrastructure lead | Regenerate via `cloudflared tunnel token`, update Vault |
| Database passwords (PostgreSQL) | 90 days | Infrastructure lead | Vault dynamic secrets or manual rotation + ESO sync |
| Redis passwords | 90 days | Infrastructure lead | Update Vault secret, ESO propagates to cluster |
| JWT signing keys (Janua OIDC) | 180 days or on compromise | Janua maintainer | JWKS rotation via Janua admin, old key kept for grace period |
| API tokens (inter-service) | 90 days | Service owner | Regenerate token, update Vault, ESO propagates |
| GitHub webhook secrets | 180 days | Infrastructure lead | Regenerate in GitHub settings, update Vault |
| Container registry tokens (GHCR) | 180 days | Infrastructure lead | Regenerate PAT, update Vault |
| Backup encryption keys | Annually | Infrastructure lead | Generate new key, re-encrypt backups, update Vault |

### Rotation Procedure

1. **Generate** new secret value using a cryptographically secure method (`openssl rand -base64 32` or equivalent)
2. **Store** the new value in HashiCorp Vault at the appropriate path
3. **Verify** ESO synchronization propagates the new Kubernetes Secret to the target namespace
4. **Restart** affected pods (rolling restart) to pick up the new secret
5. **Validate** service health via `enclii ps --wide` and health check endpoints
6. **Revoke** the old secret value after confirming the new one is active
7. **Audit log** the rotation event with timestamp, operator, and affected services

### Emergency Rotation

In the event of a suspected compromise:
1. Immediately rotate the affected secret following the procedure above
2. Review audit logs for unauthorized access during the exposure window
3. Notify affected service owners within 1 hour
4. File an incident report per the [Incident Response Runbook](docs/runbooks/INCIDENT_RESPONSE.md)

## Monitoring Plan

### Infrastructure Monitoring

| Signal | Tool | Alert Threshold | Notification Channel |
|---|---|---|---|
| Node CPU/memory | Prometheus + node-exporter | >85% sustained 5 min | Slack #infra-alerts |
| Pod restarts | Prometheus kube-state-metrics | >3 restarts in 15 min | Slack #infra-alerts |
| Disk usage | Prometheus + node-exporter | >80% used | Slack #infra-alerts |
| Longhorn volume health | Longhorn metrics | Degraded or faulted | Slack #infra-alerts, PagerDuty |
| Certificate expiry | cert-manager metrics | <14 days remaining | Slack #infra-alerts |

### Security Monitoring

| Signal | Tool | Alert Threshold | Notification Channel |
|---|---|---|---|
| Failed authentication attempts | Janua audit logs + Prometheus | >10 failures/min from single IP | Slack #security-alerts |
| Unauthorized API access (401/403) | Switchyard API metrics | >50/min sustained | Slack #security-alerts |
| Kyverno policy violations | Kyverno metrics | Any `Enforce` violation | Slack #security-alerts |
| ArgoCD sync drift | ArgoCD metrics | Out-of-sync >10 min | Slack #infra-alerts |
| Webhook HMAC failures | Switchyard API logs | Any failure | Slack #security-alerts |
| SSH login events | systemd journal (sshd) | Any successful login | Slack #security-alerts |

### Application Health

| Signal | Tool | Alert Threshold | Notification Channel |
|---|---|---|---|
| API error rate (5xx) | Prometheus + Grafana | >2% of requests for 2 min | Slack #app-alerts, PagerDuty |
| API latency (p95) | Prometheus + Grafana | >2s for 5 min | Slack #app-alerts |
| Build queue depth | BullMQ metrics via Roundhouse | >10 queued for 10 min | Slack #infra-alerts |
| Health check failures | Status page auto-incidents | 2 consecutive failures | Slack #infra-alerts, status page |
| Backup job failures | CronJob exit codes via Prometheus | Any non-zero exit | Slack #infra-alerts |

### Dashboards

Pre-provisioned Grafana dashboards (auto-provisioned via ConfigMap):
- **Cluster Capacity**: CPU, memory, disk across all nodes
- **API Latency**: Request rate, error rate, p50/p95/p99 latency by endpoint
- **ArgoCD Sync**: Sync status, drift events, reconciliation duration
- **Longhorn Health**: Volume status, replica count, IOPS
- **Cost Trends**: Resource usage mapped to Hetzner cost estimates
- **Node Maintenance**: GC runs, reclaimed space, Prometheus export status
- **Roundhouse Builds**: Build duration, queue depth, success rate
- **Secrets Rotation**: Last rotation timestamp, upcoming expirations

### Incident Response Integration

- **Critical alerts** (PagerDuty): API down, data loss risk, security breach indicators
- **Warning alerts** (Slack): Degraded performance, approaching capacity limits, policy violations
- **Repeat interval**: Critical every 1 hour, Warning every 12 hours (Alertmanager config)
- **Escalation**: Unacknowledged critical alerts escalate after 30 minutes per [Incident Response Runbook](docs/runbooks/INCIDENT_RESPONSE.md)
