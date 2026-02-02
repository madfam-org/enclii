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
- **Incident response**: See [`docs/operations/INCIDENT_RESPONSE_RUNBOOK.md`](docs/operations/INCIDENT_RESPONSE_RUNBOOK.md)
