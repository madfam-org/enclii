# Vendor Risk Assessment

Risk assessment of third-party vendors integrated into the Enclii platform.

**Last reviewed:** 2026-02-01
**Owner:** Platform Engineering
**Review cadence:** Annually

---

## Assessment Summary

| Vendor | Services Used | Risk Level | Data Sensitivity | Assessment Status |
|--------|--------------|------------|-----------------|-------------------|
| Hetzner | Compute (dedicated, VPS) | Medium | Restricted | Complete |
| Cloudflare | CDN, Tunnel, R2, DNS, WAF | Medium | Confidential | Complete |
| GitHub | Code hosting, CI/CD, Registry | Medium | Confidential | Complete |

---

## Hetzner Online GmbH

### Services Used

| Service | Purpose | Enclii Component |
|---------|---------|-----------------|
| AX41-NVMe Dedicated Server | foundry-core control plane node | k3s control plane, PostgreSQL, Redis, ArgoCD |
| Cloud VPS (The Forge) | foundry-builder-01 worker node | GitHub Actions runners, CI builds |

### Data Shared with Vendor

| Data Type | Classification | Details |
|-----------|---------------|---------|
| Server disk contents | Restricted | Encrypted volumes contain all platform data |
| Network traffic metadata | Internal | Flow logs visible to hosting provider |
| Account/billing info | Confidential | Payment details, contact information |

### Security Posture

| Control | Status |
|---------|--------|
| ISO 27001 certification | Yes |
| SOC 2 Type II | No (ISO 27001 accepted as equivalent) |
| Physical security | German data center standards; 24/7 security, biometric access |
| Data location | Germany (EU GDPR jurisdiction) |
| DDoS protection | Basic included; Cloudflare provides primary DDoS mitigation |
| Encryption at rest | Customer responsibility (Longhorn CSI encryption) |
| Network isolation | VLAN and firewall available; Enclii uses Cloudflare tunnel instead |

### Risk Analysis

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Provider outage | Low | High | Daily backups to R2; architecture ready for multi-provider |
| Physical server compromise | Very Low | Critical | Full-disk encryption; secrets in Kubernetes Secrets/Vault |
| Data center disaster | Very Low | Critical | Backups in Cloudflare R2 (geographically separate) |
| Provider insolvency | Very Low | High | Standard hardware; k3s portable to any provider |
| Unauthorized access by staff | Very Low | Critical | Disk encryption; network traffic via encrypted tunnel |

### Mitigation Controls

- All data at rest encrypted via Longhorn CSI.
- Zero exposed ports; all traffic via Cloudflare tunnel (no direct Hetzner network exposure).
- Daily PostgreSQL backups to Cloudflare R2 (off-provider).
- k3s cluster is portable; migration to alternative provider requires DNS and tunnel reconfiguration only.
- No vendor lock-in on compute layer.

---

## Cloudflare, Inc.

### Services Used

| Service | Purpose | Enclii Component |
|---------|---------|-----------------|
| Cloudflare Tunnel | Zero-trust ingress (replaces LoadBalancer) | `infra/k8s/production/cloudflared-unified.yaml` |
| Cloudflare R2 | Object storage (backups, SBOMs, artifacts) | Backup targets, SBOM storage |
| Cloudflare DNS | Domain management for enclii.dev, madfam.io | All public domains |
| Cloudflare for SaaS | Custom domain support (first 100 free) | Customer vanity domains |
| Cloudflare WAF | Web application firewall | Edge security layer |
| DDoS Protection | Volumetric attack mitigation | All public endpoints |

### Data Shared with Vendor

| Data Type | Classification | Details |
|-----------|---------------|---------|
| HTTP request/response metadata | Internal | Headers, IPs, URLs (visible at edge) |
| TLS-terminated traffic | Confidential | Cloudflare terminates TLS; re-encrypts to tunnel |
| R2 stored objects | Confidential | Backups, SBOMs (encrypted at rest by R2) |
| DNS records | Public | Domain configuration |

### Security Posture

| Control | Status |
|---------|--------|
| SOC 2 Type II | Yes |
| ISO 27001 | Yes |
| PCI DSS Level 1 | Yes |
| FedRAMP Moderate | Yes |
| Data processing agreement | Available (GDPR DPA) |
| Encryption at rest (R2) | Yes (AES-256) |
| Encryption in transit | Yes (TLS 1.2+ enforced) |
| Access logging | Yes (audit logs available) |

### Risk Analysis

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Cloudflare service outage | Low | High | Tunnel architecture allows fallback to direct ingress |
| TLS inspection / data exposure | Very Low | High | Cloudflare SOC 2 + contractual obligations |
| R2 data loss | Very Low | Medium | PostgreSQL is primary; R2 holds backup copies |
| Vendor lock-in (tunnel) | Low | Medium | Tunnel is replaceable with any reverse proxy or LoadBalancer |
| Pricing changes | Low | Low | Current cost is near-zero; budget for alternatives exists |

### Mitigation Controls

- Tunnel architecture is a convenience, not a dependency; can revert to NodePort + external LB.
- R2 stores backup copies; primary data lives on-cluster.
- All R2 objects encrypted at rest (AES-256, Cloudflare-managed keys).
- Cloudflare for SaaS is used only for vanity domains; core platform does not depend on it.
- DNS can be migrated to any registrar with standard zone export.

---

## GitHub, Inc. (Microsoft)

### Services Used

| Service | Purpose | Enclii Component |
|---------|---------|-----------------|
| GitHub Repositories | Source code hosting (private repos) | All platform code |
| GitHub Actions | CI/CD pipeline execution | `.github/workflows/` |
| GitHub Container Registry (ghcr.io) | Container image storage | Built images, signed with cosign |
| GitHub Webhooks | Build trigger pipeline | HMAC-verified webhook to Switchyard |
| GitHub OAuth | Developer authentication federation | Janua SSO GitHub provider |

### Data Shared with Vendor

| Data Type | Classification | Details |
|-----------|---------------|---------|
| Source code | Internal | All platform repositories |
| CI/CD secrets | Restricted | Stored in GitHub Actions Secrets (encrypted) |
| Container images | Internal | Application images in ghcr.io |
| Webhook payloads | Internal | Push events with commit metadata |
| OAuth tokens | Restricted | Short-lived; scoped to authorized repos |

### Security Posture

| Control | Status |
|---------|--------|
| SOC 2 Type II | Yes |
| SOC 3 | Yes |
| ISO 27001 | Yes |
| FedRAMP (Azure Gov) | Yes (via Microsoft) |
| Encryption at rest | Yes (AES-256) |
| Encryption in transit | Yes (TLS 1.2+) |
| 2FA enforcement | Yes (enforced on organization) |
| Audit logging | Yes (organization audit log) |
| IP allow-lists | Available (not currently used) |
| Secret scanning | Yes (enabled on all repos) |

### Risk Analysis

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| GitHub outage | Low | Medium | Local Git clones; CI can be migrated to self-hosted runners |
| Source code exposure (breach) | Very Low | High | Code is operational, not containing secrets; secrets in Vault |
| CI/CD secret compromise | Very Low | Critical | Secrets scoped per environment; rotation procedures in place |
| Vendor lock-in (Actions) | Low | Medium | CI workflows are standard YAML; portable to GitLab CI, Drone |
| Registry unavailability | Low | Medium | Images cached on cluster nodes; Longhorn PVCs persist state |
| Account compromise | Very Low | Critical | 2FA enforced; SSO via Janua; branch protection rules |

### Mitigation Controls

- 2FA enforced for all organization members.
- Branch protection on `main`: required reviews, required CI checks, no force push.
- GitHub Actions secrets are encrypted and scoped; never logged in CI output.
- Webhook payloads verified with HMAC signatures before processing.
- Container images signed with cosign; Kyverno rejects unsigned images.
- Secret scanning enabled; alerts on any credential committed to repository.
- GitHub OAuth tokens are short-lived and scoped to minimum required permissions.

---

## Vendor Review Schedule

| Vendor | Next Review | Reviewer |
|--------|------------|----------|
| Hetzner | 2026-08-01 | Platform Engineering |
| Cloudflare | 2026-08-01 | Platform Engineering |
| GitHub | 2026-08-01 | Platform Engineering |

Reviews are triggered early if:
- Vendor announces a security incident.
- Vendor changes terms of service or data processing agreement.
- Enclii changes how vendor services are used.
- A new vendor is onboarded.

---

## Vendor Onboarding Criteria

Before adding a new vendor, the following must be evaluated:

1. **Compliance certifications** -- Minimum SOC 2 Type II or ISO 27001.
2. **Data processing agreement** -- Must support GDPR requirements.
3. **Data residency** -- Identify where data is stored and processed.
4. **Exit strategy** -- Document how to migrate away from the vendor.
5. **Security questionnaire** -- Complete vendor security assessment.
6. **Approval** -- Platform lead sign-off required.
