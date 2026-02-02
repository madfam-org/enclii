# External Secrets Operator

**Last Updated:** February 2026
**Status:** Operational
**Active Provider:** `kubernetes-store` (cross-namespace secret copying)

---

## Overview

Enclii uses the External Secrets Operator (ESO) to synchronize secrets across Kubernetes namespaces. Currently, secrets are managed as native Kubernetes secrets in the `enclii` namespace and copied to other namespaces via the `kubernetes-store` ClusterSecretStore.

For future secrets management strategy and provider upgrade thresholds, see [SECRETS_MANAGEMENT.md](./SECRETS_MANAGEMENT.md).

## Architecture

```
┌─────────────────────────────────────────┐
│       Source: enclii namespace          │
│  (secrets created via kubectl)          │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│     External Secrets Operator           │
│     (external-secrets namespace)        │
│  ┌─────────────────────────────────────┐│
│  │ ClusterSecretStore: kubernetes-store││
│  │ Provider: kubernetes (cross-ns)     ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│   Target namespaces (via ExternalSecret)│
│  • monitoring, argocd, etc.             │
└─────────────────────────────────────────┘
```

## Configuration

### ClusterSecretStore

Located at `infra/k8s/base/external-secrets/cluster-secret-store.yaml`:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: kubernetes-store
spec:
  provider:
    kubernetes:
      remoteNamespace: enclii
      server:
        caProvider:
          type: ConfigMap
          name: kube-root-ca.crt
          namespace: kube-system
          key: ca.crt
      auth:
        serviceAccount:
          name: external-secrets
          namespace: external-secrets
```

### Creating an ExternalSecret

To copy a secret from `enclii` namespace to another namespace:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: my-credentials
  namespace: target-namespace
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: kubernetes-store
    kind: ClusterSecretStore
  target:
    name: my-credentials
    creationPolicy: Owner
  dataFrom:
    - extract:
        key: my-credentials  # name of secret in enclii namespace
```

## Operations

### Check Status

```bash
# Verify ClusterSecretStore is valid
kubectl get clustersecretstores -o wide

# List all ExternalSecrets
kubectl get externalsecrets -A

# Check operator health
kubectl get pods -n external-secrets
```

### Force Refresh

```bash
kubectl annotate externalsecret <name> -n <namespace> \
  force-sync=$(date +%s) --overwrite
```

### Verify Synced Secret

```bash
# Check if target secret exists (don't print values)
kubectl get secret <name> -n <namespace> -o jsonpath='{.data}' | jq 'keys'
```

## Troubleshooting

```bash
# Check ExternalSecret status
kubectl get externalsecret <name> -n <namespace> -o yaml | yq '.status'

# Check operator logs
kubectl logs -n external-secrets -l app.kubernetes.io/name=external-secrets -f

# Verify service account RBAC
kubectl auth can-i get secrets --as=system:serviceaccount:external-secrets:external-secrets
```

## Related Documentation

- [Secrets Management Strategy](./SECRETS_MANAGEMENT.md)
- [GitOps with ArgoCD](./GITOPS.md)
- [Cloudflare Integration](./CLOUDFLARE.md)
