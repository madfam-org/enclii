---
title: Security FAQ
description: Security practices, compliance, and data protection
sidebar_position: 4
tags: [faq, security, compliance, data-protection]
---

# Security FAQ

Questions about Enclii's security practices, data protection, and compliance.

## Data Protection

### How is my data protected?

**At rest**:
- Database encryption (AES-256)
- Encrypted persistent volumes
- Secrets encrypted with envelope encryption

**In transit**:
- TLS 1.3 for all external connections
- mTLS for internal cluster communication
- Cloudflare edge for DDoS protection

**Access control**:
- RBAC (Role-Based Access Control)
- JWT tokens with short expiry
- API key scoping

### Where is my data stored?

All data is stored in **Germany (EU)** on Hetzner infrastructure:
- Physical security: Tier 3+ data centers
- Data residency: GDPR-compliant EU location
- Backups: Cloudflare R2 (also EU-based)

### Who can access my data?

- **You and your team**: Based on RBAC roles
- **MADFAM admins**: For operational support (audited)
- **No third parties**: We don't sell or share data

Admin access is logged and follows the principle of least privilege.

### How long is data retained?

| Data Type | Retention | Configurable |
|-----------|-----------|--------------|
| Application data | Until deleted | Yes |
| Logs | 30 days | Yes (7-90 days) |
| Metrics | 90 days | Yes |
| Backups | 30 days | Yes (7-365 days) |
| Audit logs | 1 year | Enterprise only |

### Can I delete my data?

Yes. You have full control:

```bash
# Delete a service (and its data)
enclii services delete <service-id> --confirm

# Delete entire project
enclii projects delete <project-id> --confirm
```

For complete account deletion, contact support.

## Authentication & Authorization

### How does authentication work?

Enclii uses **Janua SSO** (our identity platform) for authentication:
- OAuth 2.0 / OpenID Connect
- JWT tokens (RS256 signed)
- Multi-factor authentication (optional)
- GitHub OAuth integration

### What authentication methods are supported?

- **Email/Password**: Standard login
- **GitHub OAuth**: Sign in with GitHub
- **SSO/SAML**: Enterprise customers
- **API Keys**: For CI/CD and automation

### How are sessions managed?

- Access tokens: 24 hours (configurable)
- Refresh tokens: 29 days
- Session termination: Logout from all devices
- Automatic revocation: On password change

### What roles are available?

| Role | Capabilities |
|------|-------------|
| **Viewer** | Read access to projects/services |
| **Developer** | Deploy, manage services, view logs |
| **Admin** | Full project management, secrets |
| **Owner** | Organization administration, billing |

## Secrets Management

### How are secrets stored?

Secrets are managed through the **Lockbox** subsystem:
- Encrypted at rest (AES-256)
- Never logged or exposed in build output
- Injected as environment variables at runtime
- Rotation support

### How do I add secrets?

```bash
# CLI
enclii secrets set --service <id> DATABASE_URL="postgres://..."

# Or via UI
# Project → Service → Settings → Environment
```

### Are secrets visible in logs?

No. Secrets are:
- Redacted from build logs (`***`)
- Not included in error messages
- Not exposed in Kubernetes manifests
- Only injected at runtime

### Can I rotate secrets?

Yes. Best practices:
1. Add new secret value
2. Deploy to propagate
3. Remove old secret value

Or use Lockbox rotation features (coming soon).

## Infrastructure Security

### How is the cluster secured?

**Kubernetes security**:
- k3s with hardened configuration
- Pod Security Standards (restricted)
- Network Policies for isolation
- No privileged containers

**Network security**:
- Cloudflare Tunnel (no exposed ports)
- Zero-trust architecture
- Internal traffic encrypted

### What about container security?

- **No root**: Containers run as non-root users
- **Read-only filesystem**: Where possible
- **Resource limits**: Prevent noisy neighbors
- **Image scanning**: SBOM generation, vulnerability scanning

### Is multi-tenancy secure?

Yes. Tenant isolation is enforced via:
- Kubernetes namespaces
- Network Policies
- Resource quotas
- RBAC separation

Enterprise customers can get dedicated namespaces or nodes.

### How do you handle vulnerabilities?

1. **Monitoring**: CVE tracking for dependencies
2. **Patching**: Critical patches within 24 hours
3. **Notification**: Security advisories for affected users
4. **Audit**: Regular security audits

## Compliance

### What compliance standards do you meet?

**Current**:
- GDPR (EU data protection)
- SOC 2 Type I (in progress)

**Planned**:
- SOC 2 Type II
- ISO 27001
- HIPAA (enterprise)

### Is Enclii GDPR compliant?

Yes. GDPR compliance includes:
- EU data residency
- Right to deletion
- Data portability
- Privacy by design
- DPA available for enterprise

### Do you have a DPA?

Data Processing Agreement available for enterprise customers. Contact legal@enclii.dev.

### What about PCI DSS?

Enclii is not PCI DSS certified. For payment processing:
- Use a certified payment processor (Stripe, etc.)
- Don't store card data on Enclii
- Follow PCI DSS guidelines in your application

## Incident Response

### What happens if there's a security incident?

1. **Detection**: Automated monitoring and alerts
2. **Containment**: Immediate isolation
3. **Investigation**: Root cause analysis
4. **Notification**: Affected users informed within 72 hours
5. **Resolution**: Fix and post-mortem

### How do I report a security issue?

**Responsible disclosure**:
- Email: security@enclii.dev
- PGP key available on request
- Response within 48 hours

We do not have a bug bounty program at this time.

### Is there a status page?

Yes: https://status.enclii.dev

Subscribe for:
- Incident notifications
- Maintenance windows
- Performance updates

## Network Security

### What about DDoS protection?

Cloudflare provides:
- L3/L4 DDoS mitigation
- L7 attack protection
- Rate limiting
- Bot management

### Are there IP restrictions?

Yes, for enterprise:
- IP allowlisting
- VPN access
- Private endpoints

### What ports are exposed?

Only **443 (HTTPS)** via Cloudflare Tunnel. No other ports are exposed to the internet.

## Related Documentation

- **Infrastructure**: [Cloudflare Integration](/infrastructure/CLOUDFLARE)
- **Authentication**: [SSO Integration](/integrations/sso)
- **General FAQ**: [General Questions](/faq/general)
- **Compliance Webhooks**: [Compliance Webhooks](/integrations/compliance-webhooks)
