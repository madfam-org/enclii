# Secrets Management Strategy

**Last Updated:** February 4, 2026 (Wave 15 audit)
**Current Approach:** Kubernetes native secrets + ESO cross-namespace copying
**Chosen Future Provider:** Self-hosted HashiCorp Vault (Community Edition)
**Next Review:** When any trigger criteria below is met

---

## Current State

Secrets are created manually via `kubectl create secret` in the `enclii` namespace. The External Secrets Operator (ESO) with a `kubernetes-store` ClusterSecretStore copies secrets to other namespaces as needed. There is no external secrets provider.

**Why this works today:**
- Single cluster (2 nodes), single team
- ~10-15 secrets total across all namespaces
- Low rotation frequency (credentials change quarterly at most)
- All secret creation is manual and traceable to a human operator

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

**Current state:** None of these triggers are met. Manual K8s secrets remain appropriate.

## Recommended Upgrade Path

```
Current                     Next step (when triggered)
─────────────────────────   ─────────────────────────
K8s native secrets          HashiCorp Vault (self-hosted)
+ ESO kubernetes-store      + ESO vault-store
                            + K8s auth for pods
                            + OIDC auth (Janua) for operators
                            + Dynamic DB credentials
                            + Audit logging

Trigger: now                Trigger: ANY criteria above
```

## Implementation Notes

When upgrading providers:

1. ESO is already installed and operational — only the ClusterSecretStore provider config changes
2. Vault/OpenBao commented config is preserved in git history (removed Feb 2026 cleanup)
3. The `kubernetes-store` can coexist with an external provider during migration
4. Create ExternalSecret resources gradually — migrate one namespace at a time

## Vault ExternalSecret Resources

Once Vault is deployed and initialized, ExternalSecret resources will sync secrets from Vault into Kubernetes namespaces.

### Available ExternalSecrets

| Resource | Namespace | Vault Path | Secrets |
|----------|-----------|------------|---------|
| `enclii-secrets` | enclii | `secret/enclii` | DATABASE_URL, REDIS_URL, JANUA_CLIENT_ID, JANUA_CLIENT_SECRET, GITHUB_WEBHOOK_SECRET, SWITCHYARD_API_KEY |
| `janua-secrets` | janua | `secret/janua` | DATABASE_URL, REDIS_URL, JWT_SIGNING_KEY, GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET |
| `data-secrets` | data | `secret/data` | POSTGRES_PASSWORD, REDIS_PASSWORD |
| `cloudflare-secrets` | cloudflare-tunnel | `secret/cloudflare` | TUNNEL_TOKEN |

### Configuration

All ExternalSecrets use:
- **Store**: `vault-store` (ClusterSecretStore)
- **API Version**: `external-secrets.io/v1beta1` (ESO v0.9.11)
- **Refresh Interval**: 15 minutes
- **Deletion Policy**: Retain (secrets persist if ExternalSecret is deleted)

### Files

Located at `infra/k8s/base/external-secrets/vault-secrets/`:
- `enclii-secrets.yaml`
- `janua-secrets.yaml`
- `data-secrets.yaml`
- `cloudflare-secrets.yaml`

### Verification

After Vault deployment, verify ExternalSecrets are syncing:
```bash
kubectl get externalsecrets -A
kubectl describe externalsecret enclii-secrets -n enclii
```

## Related Documentation

- [External Secrets Operator](./EXTERNAL_SECRETS.md)
- [GitOps with ArgoCD](./GITOPS.md)
