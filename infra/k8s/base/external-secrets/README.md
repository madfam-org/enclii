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

ESO is deployed with both the legacy `kubernetes-store` ClusterSecretStore and
the active `vault-store` ClusterSecretStore for HashiCorp Vault KV v2. New
production service secrets should use Vault-backed ExternalSecrets and Enclii
operator workflows; the Kubernetes provider remains only for compatibility
while older bridges are retired.

The `vault-store` Kubernetes auth binding is aligned with the bootstrap role
created by `scripts/cluster-ops-deploy.sh`: `eso-reader` is bound to the
`external-secrets` ServiceAccount in the `external-secrets` namespace, without
requesting a custom JWT audience. If Vault reports HTTP 403 on
`auth/kubernetes/login`, verify that role binding before adding per-service
ExternalSecrets.

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
