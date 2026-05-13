# External Secrets Operator Configuration

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


Centralized secret management for Enclii using External Secrets Operator (ESO).

## Architecture

```
HashiCorp Vault (future)        Kubernetes (current)
       │                               │
       ▼                               ▼
ClusterSecretStore              ClusterSecretStore
  (vault provider)              (kubernetes provider)
       │                               │
       ▼                               ▼
ExternalSecret (per namespace)  ExternalSecret (per namespace)
       │                               │
       ▼                               ▼
Kubernetes Secret (auto-synced) Kubernetes Secret (auto-synced)
```

## Current State

ESO is deployed with a `kubernetes-store` ClusterSecretStore that copies secrets between namespaces. There is no external secrets provider yet. This is intentional — see [SECRETS_MANAGEMENT.md](../../../docs/infrastructure/SECRETS_MANAGEMENT.md) for upgrade trigger criteria.

**Chosen future provider:** Self-hosted HashiCorp Vault (Community Edition)

**Trigger criteria for Vault deployment (ANY of):**
1. First external client onboarded (multi-tenant secret isolation required)
2. SOC2 audit preparation begins (auditable secrets management mandatory)
3. Revenue threshold reached (justifies operational overhead)
4. Team size exceeds 3 engineers with production access

## Quick Start (Current: Kubernetes Provider)

### 1. Verify ClusterSecretStore

```bash
kubectl get clustersecretstore
# Should show kubernetes-store with STATUS: Valid
```

### 2. Create ExternalSecrets

```bash
kubectl apply -f example-external-secret.yaml
```

## Future: HashiCorp Vault Integration

When trigger criteria are met, the migration path is:

1. Deploy Vault (self-hosted, in-cluster or dedicated node)
2. Configure Kubernetes auth backend in Vault
3. Add a `vault-store` ClusterSecretStore alongside the existing `kubernetes-store`
4. Migrate ExternalSecrets one namespace at a time to use `vault-store`
5. Decommission `kubernetes-store` when migration is complete

```
Vault (self-hosted) ← K8s ServiceAccount auth ← ESO ← ExternalSecret → K8s Secret
                    ← OIDC auth (via Janua) ← Human operators (UI/CLI)
```

## Secret Rotation

ESO automatically syncs secrets based on `refreshInterval`:
- Default: 1 hour
- For sensitive secrets, reduce to 5-15 minutes
- For static config, increase to 24 hours

## Troubleshooting

### ExternalSecret not syncing
```bash
kubectl describe externalsecret <name> -n <namespace>
# Check events for errors
```

### ClusterSecretStore invalid
```bash
kubectl describe clustersecretstore <store-name>
# Verify auth secret exists and token is valid
```

### Secret not created
```bash
kubectl get events -n <namespace> --field-selector reason=SyncFailed
```

## Security Best Practices

1. **Least Privilege**: Scope service accounts to minimum required secrets
2. **Rotation**: Implement rotation policy (automated when Vault is deployed)
3. **Audit**: Review secret access logs (Vault provides this natively)
4. **RBAC**: Limit who can view ExternalSecrets in Kubernetes
5. **Namespacing**: Use ExternalSecret (not ClusterExternalSecret) for namespace isolation

## Related Documentation

- [Secrets Management Strategy](../../../docs/infrastructure/SECRETS_MANAGEMENT.md)
- [External Secrets Operator](../../../docs/infrastructure/EXTERNAL_SECRETS.md)
- [GitOps with ArgoCD](../../../docs/infrastructure/GITOPS.md)
