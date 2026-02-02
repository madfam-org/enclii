# Data Classification Policy

Defines data sensitivity levels and handling requirements for the Enclii platform.

**Last reviewed:** 2026-02-01
**Owner:** Platform Engineering
**Review cadence:** Annually

---

## Classification Levels

Enclii uses four classification levels. All data produced, processed, or stored by the platform must be assigned one of these levels.

| Level | Label | Color Code |
|-------|-------|------------|
| 1 | Public | Green |
| 2 | Internal | Blue |
| 3 | Confidential | Orange |
| 4 | Restricted | Red |

---

## Level 1 -- Public

### Definition

Information intended for public consumption. Disclosure poses no risk to the platform or its users.

### Examples in Enclii

- Marketing website content (`enclii.dev`)
- Public documentation (`docs.enclii.dev`)
- Status page information (`status.enclii.dev`)
- OpenAPI specification (`docs/api/openapi.yaml`)
- Open-source dependency lists
- Public changelog and release notes

### Handling Requirements

| Aspect | Requirement |
|--------|-------------|
| Storage | No restrictions |
| Transit | HTTPS preferred, not required |
| Access control | None required |
| Sharing | Unrestricted |
| Disposal | No special procedures |
| Logging | Not required |

---

## Level 2 -- Internal

### Definition

Information used in day-to-day operations. Not intended for public disclosure but would cause minimal harm if exposed.

### Examples in Enclii

- Source code (private repositories)
- Non-sensitive configuration files (`infra/k8s/base/`, `infra/argocd/apps/`)
- Internal architecture documentation (`docs/architecture/`)
- CI/CD pipeline definitions (`.github/workflows/`)
- Development environment configurations
- Non-sensitive Kubernetes manifests
- Team communication and meeting notes
- Internal runbooks and operational procedures

### Handling Requirements

| Aspect | Requirement |
|--------|-------------|
| Storage | Private repositories or internal systems |
| Transit | HTTPS or SSH required |
| Access control | Authenticated access; GitHub org membership |
| Sharing | Within the organization only |
| Disposal | Delete from all storage locations |
| Logging | Access logging recommended |

---

## Level 3 -- Confidential

### Definition

Sensitive information that could cause significant harm to the platform or its users if disclosed. Includes personally identifiable information (PII) and business-critical data.

### Examples in Enclii

- User account data (email, name, organization)
- Audit logs (`apps/switchyard-api/internal/audit/`)
- Deployment metadata (service configs, environment variables without secrets)
- Database contents (PostgreSQL user and project data)
- Session data (Redis session store)
- Build logs containing project source paths
- RBAC role assignments and access grants
- Cost tracking and billing data (Waybill)
- Incident reports and security findings
- Vulnerability scan results

### Handling Requirements

| Aspect | Requirement |
|--------|-------------|
| Storage | Encrypted at rest (Longhorn PVC encryption, R2 server-side encryption) |
| Transit | TLS 1.2+ required (enforced by Cloudflare tunnel) |
| Access control | RBAC with least-privilege; role-based (admin/developer/viewer) |
| Sharing | Need-to-know basis; approval required for external sharing |
| Disposal | Secure deletion; verify removal from backups within retention window |
| Logging | All access logged via audit middleware |
| Backup | Daily backups to R2 with encryption; 30-day retention |
| Breach notification | Within 72 hours of confirmed breach |

---

## Level 4 -- Restricted

### Definition

Highly sensitive information whose disclosure would cause severe harm. Unauthorized access could compromise the entire platform.

### Examples in Enclii

- Database credentials (`ENCLII_DB_URL`)
- OIDC client secrets (`ENCLII_OIDC_ISSUER` credentials)
- API signing keys (cosign private keys)
- JWT signing keys (RS256 private key in Janua)
- GitHub webhook HMAC secrets
- Container registry credentials (ghcr.io tokens)
- Cloudflare tunnel tokens and API keys
- Hetzner API tokens
- Terraform state containing credentials (`infra/terraform/terraform.tfvars`)
- Kubernetes ServiceAccount tokens
- Backup encryption keys
- `.env` and `.env.local` files containing secrets

### Handling Requirements

| Aspect | Requirement |
|--------|-------------|
| Storage | Secrets manager only (Lockbox/Vault, Kubernetes Secrets); never in Git |
| Transit | TLS 1.2+ with mutual authentication where possible |
| Access control | Named individuals only; two-person rule for rotation |
| Sharing | Never via email, chat, or unencrypted channels |
| Disposal | Cryptographic erasure; rotate all dependent credentials |
| Logging | All access and modifications logged; real-time alerting on anomalies |
| Backup | Encrypted; stored separately from data backups |
| Rotation | Minimum quarterly; immediately on personnel change |
| Breach notification | Immediate (within 1 hour); trigger credential rotation |

---

## Handling Matrix Summary

| Requirement | Public | Internal | Confidential | Restricted |
|-------------|--------|----------|-------------|------------|
| Encryption at rest | No | No | Yes | Yes |
| Encryption in transit | Preferred | Required | Required (TLS 1.2+) | Required (TLS 1.2+) |
| Access logging | No | Recommended | Required | Required + alerting |
| Background check | No | No | No | Yes |
| Backup encryption | No | No | Yes | Yes (separate keys) |
| Retention policy | None | 1 year | 3 years | 1 year (then rotate/destroy) |
| Incident response | N/A | 48 hours | 72 hours | 1 hour |

---

## Classification Responsibilities

| Role | Responsibility |
|------|---------------|
| Data owner | Assign classification level at creation |
| Platform engineers | Implement technical controls per classification |
| All personnel | Handle data according to its classification level |
| Platform lead | Review classifications annually; approve exceptions |

---

## Enforcement

### Technical Controls

- **Git pre-commit hooks** -- Scan for secrets in staged files (installed via `make bootstrap`).
- **Kyverno policies** -- Block pods that mount secrets as environment variables in plaintext logs.
- **Kubernetes RBAC** -- Namespace-scoped access; secret access restricted to service accounts.
- **Cloudflare tunnel** -- All external traffic encrypted; no plaintext ingress paths.
- **Audit middleware** -- Logs all API access to Confidential and Restricted data.

### Violations

Handling data above its authorized level or below its required controls constitutes a policy violation. Violations are:

1. Logged as a security incident.
2. Reviewed by the platform lead within 24 hours.
3. Remediated with corrective action documented.
