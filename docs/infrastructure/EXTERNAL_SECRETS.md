# External Secrets Operator

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Last Updated:** March 2026
**Status:** Operational (Vault-backed)
**Active Providers:** `vault-store` (HashiCorp Vault KV v2) + `kubernetes-store` (legacy, cross-namespace)

---

## Overview

Enclii uses the External Secrets Operator (ESO) to synchronize secrets from HashiCorp Vault into Kubernetes namespaces. All ~160 production secrets across 16 namespaces are stored in Vault and synced via ExternalSecret resources.

For secrets management strategy and Vault deployment details, see [SECRETS_MANAGEMENT.md](./SECRETS_MANAGEMENT.md).
For Vault operations (unseal, rotation, backup), see [Vault Operations Runbook](../runbooks/VAULT_OPERATIONS.md).

## Architecture

```
┌─────────────────────────────────────────┐
│    HashiCorp Vault (vault namespace)    │
│    KV v2 engine at secret/              │
│    UI: https://vault.madfam.io          │
└─────────────────────────────────────────┘
                    │
          K8s ServiceAccount auth
                    │
                    ▼
┌─────────────────────────────────────────┐
│     External Secrets Operator           │
│     (external-secrets namespace)        │
│  ┌─────────────────────────────────────┐│
│  │ ClusterSecretStore: vault-store     ││
│  │ Provider: vault (KV v2)            ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│   16 namespaces (via ExternalSecret)    │
│  • enclii, janua, data, dhanam, tezca  │
│  • yantra4d, karafiel, forgesight, ... │
└─────────────────────────────────────────┘
```

## Configuration

### ClusterSecretStore (Vault)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: vault-store
spec:
  provider:
    vault:
      server: "http://vault.vault.svc.cluster.local:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "eso-reader"
          serviceAccountRef:
            name: external-secrets
            namespace: external-secrets
```

### ClusterSecretStore (Legacy kubernetes-store)

Still available for backward compatibility at `infra/k8s/base/external-secrets/cluster-secret-store.yaml`.

### Creating an ExternalSecret (Vault-backed)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: my-service-secrets
  namespace: target-namespace
  labels:
    app.kubernetes.io/managed-by: enclii
spec:
  refreshInterval: 15m
  secretStoreRef:
    name: vault-store
    kind: ClusterSecretStore
  target:
    name: my-service-secrets
    creationPolicy: Owner
    deletionPolicy: Retain
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: secret/target-namespace
        property: database_url
```

## ExternalSecret Inventory

| Resource | Namespace | Vault Path | Key Count |
|----------|-----------|------------|-----------|
| `enclii-secrets` | enclii | `secret/enclii` | 23 |
| `janua-secrets` | janua | `secret/janua` | 9 |
| `data-secrets` | data | `secret/data` | 8 |
| `cloudflare-secrets` | cloudflare-tunnel | `secret/cloudflare` | 1 |
| `dhanam-secrets` | dhanam | `secret/dhanam` | 17 |
| `autoswarm-secrets` | autoswarm | `secret/autoswarm` | 3 |
| `tezca-secrets` | tezca | `secret/tezca` | 11 |
| `yantra4d-secrets` | yantra4d | `secret/yantra4d` | 3 |
| `karafiel-secrets` | karafiel | `secret/karafiel` | 15 |
| `forgesight-secrets` | forgesight | `secret/forgesight` | 9 |
| `pravara-mes-secrets` | pravara-mes | `secret/pravara-mes` | 11 |
| `monitoring-secrets` | monitoring | `secret/monitoring` | 3 |
| `arc-runners-secrets` | arc-runners | `secret/arc-runners` | 3 |
| `enclii-builds-secrets` | enclii-builds | `secret/enclii-builds` | 3 |
| `npm-registry-secrets` | npm-registry | `secret/npm-registry` | 1 |
| `madfam-site-secrets` | madfam-site | `secret/madfam-site` | 2 |
| `longhorn-secrets` | longhorn-system | `secret/longhorn-system` | 1 |
| `kyverno-secrets` | kyverno | `secret/kyverno` | 1 |

Files located at `infra/k8s/base/external-secrets/vault-secrets/`.

## Operations

### Check Status

```bash
# Verify ClusterSecretStore is valid
kubectl get clustersecretstores -o wide

# List all ExternalSecrets and their sync status
kubectl get externalsecrets -A

# Check operator health
kubectl get pods -n external-secrets

# Verify a specific secret synced
kubectl get secret <name> -n <namespace> -o jsonpath='{.data}' | jq 'keys'
```

### Force Refresh

```bash
kubectl annotate externalsecret <name> -n <namespace> \
  force-sync=$(date +%s) --overwrite
```

### Add a New Secret

1. Write to Vault: `vault kv put secret/<namespace> key=value`
2. Add entry to the namespace's ExternalSecret YAML in `infra/k8s/base/external-secrets/vault-secrets/`
3. Commit and let ArgoCD sync, or `kubectl apply -f` directly

## Troubleshooting

```bash
# Check ExternalSecret status
kubectl get externalsecret <name> -n <namespace> -o yaml | yq '.status'

# Check operator logs
kubectl logs -n external-secrets -l app.kubernetes.io/name=external-secrets -f

# Verify Vault connectivity from ESO
kubectl exec -n vault vault-0 -- vault kv get secret/<namespace>

# Verify service account can authenticate to Vault
kubectl exec -n vault vault-0 -- vault read auth/kubernetes/role/eso-reader
```

## Related Documentation

- [Secrets Management Strategy](./SECRETS_MANAGEMENT.md)
- [Vault Operations Runbook](../runbooks/VAULT_OPERATIONS.md)
- [GitOps with ArgoCD](./GITOPS.md)
- [Cloudflare Integration](./CLOUDFLARE.md)
