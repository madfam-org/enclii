# Secrets Management Strategy

**Last Updated:** 2026-06-15
**Current Approach:** HashiCorp Vault (self-hosted) + ESO vault-store
**Provider:** Self-hosted HashiCorp Vault (Community Edition) at vault.madfam.io
**Coverage:** ~160 secrets across 16 namespaces via 19 ExternalSecret resources

---

## Current State

All production secrets are stored in self-hosted HashiCorp Vault (KV v2 engine) and synced to Kubernetes namespaces via the External Secrets Operator (ESO) with a `vault-store` ClusterSecretStore. The Vault UI is accessible at `https://vault.madfam.io`.

**Architecture:**
- Vault pod runs in the `vault` namespace (Helm chart)
- ESO authenticates via scoped `eso-reader` token (`external-secrets/vault-eso-token`); Kubernetes auth repair in progress
- 19+ ExternalSecret resources cover 16 namespaces (~160 secrets)
- Secrets refresh every 15 minutes
- Audit: `/dev/stderr` + `/vault/audit/vault_audit.log`

**2026-06-16 rebuild:** Lost custody → destroy PVCs → new Bitwarden break-glass → backfill from K8s. See private `internal-devops/runbooks/2026-06-16-vault-rebootstrap-complete.md`.

**Custody:** Financial keys → `secret/dhanam` (Dhanam). Shared Resend → `secret/comms` (Enclii platform). See private decision `2026-06-16-platform-comms-and-dhanam-secret-custody.md`.

**Migration script:** `scripts/vault-secret-migration.sh` — reads existing K8s secrets and writes to Vault KV v2.

## Revisit Thresholds

Upgrade to a dedicated secrets management provider when **any** of these thresholds is crossed:

### Operational Complexity

| Signal | Threshold | Why it matters |
|--------|-----------|----------------|
| Total secrets across cluster | > 50 | Manual tracking becomes error-prone |
| Secrets requiring rotation | > 10 per quarter | Manual rotation doesn't scale |
| Namespaces with unique secrets | > 8 | Cross-namespace copying becomes a maintenance burden |
| Team members managing secrets | > 3 | No audit trail of who changed what |

### Security Requirements

| Signal | Threshold | Why it matters |
|--------|-----------|----------------|
| Compliance requirements | SOC 2, HIPAA, PCI | Auditors require centralized audit logs for secret access |
| Customer-owned secrets | Any | Customer data requires encryption-at-rest with managed keys |
| Automatic rotation needed | Any credential | Native K8s secrets don't rotate; manual process introduces downtime risk |
| Secret access audit | Required by policy | `kubectl` access logs are insufficient for compliance |

### Infrastructure Growth

| Signal | Threshold | Why it matters |
|--------|-----------|----------------|
| Kubernetes clusters | > 1 | Secrets must sync across clusters; manual copying is fragile |
| CI/CD pipelines needing secrets | > 3 | Hardcoded secrets in pipelines are a breach vector |
| External services with API keys | > 15 | Sprawl makes it impossible to track which keys are active |
| Environments (dev/staging/prod) | > 2 with separate secrets | Environment-specific secret sets multiply management overhead |

### Incident-Driven

| Signal | Immediate action |
|--------|-----------------|
| Secret leaked in git history | Rotate all affected secrets, adopt sealed/external secrets |
| Unauthorized secret access | Implement audit logging immediately |
| Failed rotation causes outage | Adopt automated rotation |
| Compliance audit finding | Implement whatever the auditor requires |

## Provider Options

### Tier 1: GitOps-Native (Low Complexity)

#### Sealed Secrets (Bitnami)

- **What:** Encrypt secrets client-side, commit ciphertext to git, controller decrypts in-cluster
- **Cost:** $0 (open source)
- **Fits when:** You want secrets in git without plaintext exposure
- **Limitations:** No automatic rotation, no audit log, no cross-cluster sync, re-encrypt on key rotation
- **Integrates with:** ArgoCD (natively), existing GitOps workflow

```
Developer → kubeseal → encrypted YAML → git → ArgoCD → SealedSecret controller → K8s Secret
```

### Tier 2: Managed SaaS (Low Ops Overhead)

#### Doppler

- **What:** Cloud-hosted secrets manager with K8s operator and CLI
- **Cost:** Free up to 5 users, $18/user/month after
- **Fits when:** Small team, want zero self-hosting overhead, need environment-based secret sets
- **Limitations:** Vendor dependency, data leaves your infrastructure
- **Integrates with:** ESO (already installed), GitHub Actions, CLI

#### Infisical

- **What:** Open-source secrets manager, can be self-hosted or SaaS
- **Cost:** Free (self-hosted), $8/user/month (cloud)
- **Fits when:** Want Doppler-like UX with self-hosting option
- **Limitations:** Smaller community than Vault, newer project
- **Integrates with:** ESO, Kubernetes operator, GitHub Actions

### Tier 3: Self-Hosted (Full Control)

#### HashiCorp Vault (Community Edition)

- **What:** Industry-standard secrets engine with dynamic secrets, PKI, transit encryption
- **Cost:** $0 (open source), significant ops overhead
- **Fits when:** > 100 secrets, compliance requirements, need dynamic database credentials or PKI
- **Limitations:** Complex to operate (unsealing, HA, backups), high resource usage (~512MB-1GB RAM)
- **Integrates with:** ESO (already installed), Kubernetes auth, Janua (OIDC auth to Vault)

```
Vault (self-hosted) ← K8s ServiceAccount auth ← ESO ← ExternalSecret → K8s Secret
                    ← OIDC auth (via Janua) ← Human operators (UI/CLI)
```

#### OpenBao (Vault fork, MPL-2.0)

- **What:** Community fork of Vault after HashiCorp's license change to BSL
- **Cost:** $0 (truly open source)
- **Fits when:** Same as Vault, but you want MPL-2.0 licensing guarantee
- **Limitations:** Smaller community, fewer enterprise integrations
- **Integrates with:** Same as Vault (API-compatible)

### Not Recommended

| Provider | Why not |
|----------|---------|
| AWS Secrets Manager / GCP Secret Manager / Azure Key Vault | Vendor lock-in contradicts Enclii's zero-vendor-lock-in principle |
| Building into Janua | Different trust domain — see architectural note below |

### Why Not Janua?

Janua is an authentication/authorization service. Secrets management is a different concern:

- **Different trust domains:** Combining identity + secrets means a single breach exposes everything
- **Different consumers:** Janua serves humans (OIDC flows); secrets serve machines (pod injection)
- **Different lifecycles:** JWT rotation (minutes) vs. API key rotation (days/months)
- **Scope creep:** Building a secrets engine means building encrypted storage, versioning, sync agents, rotation workflows — a second product

Janua **can** issue machine-to-machine tokens (OAuth client credentials) that authenticate workloads to a *separate* secrets store. That's a legitimate extension point.

## Decision: Self-Hosted HashiCorp Vault

**Decided:** February 4, 2026 (Wave 15 audit)

After evaluating all options, **self-hosted HashiCorp Vault (Community Edition)** is the chosen path for when external secrets management is needed. Rationale:

- ESO is already deployed and supports Vault natively
- Vault integrates with Janua via OIDC for human operator access
- Vault supports Kubernetes auth for automated pod access
- Dynamic secrets, audit logging, and PKI cover all foreseeable needs
- No vendor dependency (self-hosted, open source)
- OpenBao is an acceptable alternative if licensing concerns arise

**Explicitly NOT using:** Doppler (previous references in config comments have been cleaned up).

### Vault Deployment Trigger Criteria

Deploy Vault when **ANY** of the following conditions is met:

| # | Trigger | Rationale |
|---|---------|-----------|
| 1 | First external client onboarded | Multi-tenant secret isolation required |
| 2 | SOC2 audit preparation begins | Auditable secrets management is mandatory |
| 3 | Revenue threshold reached | Justifies operational overhead |
| 4 | Team size exceeds 3 engineers with production access | Access control + audit trails needed |

**Current state:** Multiple triggers were met (>50 secrets, >8 namespaces, >15 external API keys). Vault is deployed.

## Current Architecture

```
Vault (vault namespace)
  └─ KV v2 engine at secret/
  └─ K8s auth (eso-reader role)
  └─ OIDC auth (Janua) for operators
  └─ Audit log (/vault/audit/audit.log)
       │
       ▼
ESO (external-secrets namespace)
  └─ ClusterSecretStore: vault-store
       │
       ▼
19 ExternalSecret resources → 16 namespaces
```

See [EXTERNAL_SECRETS.md](./EXTERNAL_SECRETS.md) for the full ExternalSecret inventory.

## Secret Intake (chat-safe operator handoff)

When operators must supply credentials that must **never** appear in agent chat
transcripts, use Enclii Secret Intake instead of pasting into Cursor or committing
to git.

| Surface | Role |
|---------|------|
| `enclii secrets intake submit <target> --reason "..."` | Masked CLI / file / stdin → Vault |
| `enclii secrets intake status <intake_id>` | Agent-safe poll (no values) |
| `POST /v1/secrets/intake` | Same via API (admin JWT) |

**P0 prerequisite:** `vault-credentials` K8s secret in `enclii` namespace with a
scoped Vault writer token. Bootstrap:

```bash
VAULT_TOKEN_FILE=/path/to/vault-admin.token \
  ./scripts/provision-switchyard-vault-writer.sh
```

Registry targets: `apps/switchyard-api/internal/secretsintake/registry.yaml`.  
Runbook: [Secret Intake](../runbooks/SECRET_INTAKE.md).  
Finish-line orchestration: `scripts/finish-line-secret-intake.sh`.  
Policy: [internal-devops decision](https://github.com/madfam-org/internal-devops/blob/main/decisions/2026-06-15-secret-intake-protocol.md).

Until `vault-credentials` exists, intake returns `503 vault_writer_disabled`.

## Operations

For Vault operations (unseal, rotation, backup), see [Vault Operations Runbook](../runbooks/VAULT_OPERATIONS.md).

## Related Documentation

- [External Secrets Operator](./EXTERNAL_SECRETS.md)
- [GitOps with ArgoCD](./GITOPS.md)
- [Secret Intake Runbook](../runbooks/SECRET_INTAKE.md)
