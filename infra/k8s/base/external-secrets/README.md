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
`external-secrets` ServiceAccount in the `external-secrets` namespace with
`bound_audiences=vault`. If Vault reports HTTP 403 on
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

## Cross-app secret reads (autoprovision one app's secret into another)

When an app needs a secret **another app already holds** — e.g. crea-map needs
janua's internal service key to send notification email — do **not** copy the
value or run a Vault write. Reference the source app's Vault path directly in the
**consumer's** ExternalSecret:

```yaml
# in the CONSUMER app's ExternalSecret (secretStoreRef: vault-store):
- secretKey: JANUA_INTERNAL_API_KEY
  remoteRef: { key: secret/janua, property: internal_api_key }
```

**Why this works — one cluster-wide reader role.** `vault-store` authenticates as
the single `eso-reader` Vault role bound to the `external-secrets` ServiceAccount
(not a per-app role), so an ExternalSecret in **any** namespace may reference
**any** `secret/*` path. This is the sanctioned pattern, not a workaround:
`secret/comms` (shared Resend credentials) is read this way by `janua-secrets`,
`enclii-secrets` and `madfam-site-secrets`, and `janua-secrets` itself reads
`secret/janua`, `secret/comms`, `secret/coupler` and `secret/dhanam`. It is a
**live reference** (ESO re-reads Vault every `refreshInterval`), never a copy that
can drift.

**Before you add the line — prove the source property EXISTS.** ESO is
all-or-nothing per ExternalSecret: one missing property fails the **whole** object
and the pod starts with no env (the "N4 lesson"). Verify by finding an existing
ExternalSecret that already maps the same source property, and confirming that
source app is serving (its own all-or-nothing map being healthy proves the
property is populated):

```bash
# is the property populated in the source path?
grep -rn "property: internal_api_key" infra/k8s/base/external-secrets/vault-secrets/
# is the source app up? (all-or-nothing → serving proves the property exists)
curl -s -o /dev/null -w '%{http_code}\n' https://auth.madfam.io/health
```

The key lands in the consumer pod's env via `envFrom` and takes effect on the
next pod **roll**. Prefer this cross-reference over `enclii secrets set`
(which needs the raw value) whenever the secret already lives in Vault under
another app's path.

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

- [Secrets Management Strategy](../../../../docs/infrastructure/SECRETS_MANAGEMENT.md)
- [External Secrets Operator](../../../../docs/infrastructure/EXTERNAL_SECRETS.md)
- [GitOps with ArgoCD](../../../../docs/infrastructure/GITOPS.md)
