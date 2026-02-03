# Deprecated External Secrets Providers

This directory contains deprecated external secrets provider configurations.

## Doppler (DEPRECATED - Feb 2026)

**Status:** NOT IN USE - Files archived for reference only

**Decision:** Doppler will NOT be used. Future secrets management will use self-hosted Hashicorp Vault.

**Rationale:**
- Doppler adds monthly cost and external dependency
- Self-hosted Vault provides better cost efficiency and control
- Vault integrates well with Kubernetes RBAC and audit logging

**Migration Plan:**
1. Continue using Kubernetes-native secrets (current approach)
2. When ready, deploy self-hosted Hashicorp Vault
3. Configure ESO with Vault provider
4. Migrate secrets incrementally

**Files:**
- `doppler-cluster-secret-store.yaml` - ClusterSecretStore config (never applied)
- `doppler-secret-store-secret.yaml.template` - Auth secret template
