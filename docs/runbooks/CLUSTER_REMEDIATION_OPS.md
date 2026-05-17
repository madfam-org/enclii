---
title: Cluster Remediation Operations
description: Runbook for pending cluster-side operations that require manual execution
sidebar_position: 6
tags: [operations, runbook, cluster, remediation]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Cluster Remediation Operations

**Purpose:** Consolidates all pending cluster-side operations into a single executable runbook. These are operations that cannot be applied through GitOps alone and require direct cluster access.

**Last Updated:** March 2026

**Prerequisites:**
- `kubectl` access to enclii-production cluster (`KUBECONFIG=~/.kube/enclii-production`)
- SSH access to foundry-cp (control plane) via `ssh -o ProxyCommand="cloudflared access ssh --hostname %h" solarpunk@ssh.madfam.io`
- Credentials for Vault initialization, GitHub PAT, and Cloudflare API token as noted per section

---

## Section 1 -- Legacy CronJob Cleanup (P2)

**Context:** Two CronJobs were removed from git but may still exist on the cluster as orphans. ArgoCD only prunes resources it manages; manually-created CronJobs persist until explicitly deleted.

**Pre-conditions:** PR with CronJob file deletions merged to main.

**Risk:** Low. These CronJobs are inactive and their removal has no side effects.

### Commands

```bash
kubectl delete cronjob image-cleanup -n kube-system --ignore-not-found
kubectl delete cronjob disk-cleanup -n enclii --ignore-not-found
```

### Verification

```bash
kubectl get cronjobs -A | grep -E "image-cleanup|disk-cleanup"
# Expected: no results
```

---

## Section 2 -- Backup Credential Secrets (P1) {#section-2--backup-credential-secrets-p1}

**Context:** Two backup CronJobs (`github-repos-backup` and `cloudflare-config-backup`) are deployed via ArgoCD but depend on secrets that must be created manually. Without these secrets, the jobs fail on every scheduled run.

**Pre-conditions:** CronJob manifests synced by ArgoCD (`infra/k8s/production/backup/`). R2 backup credentials (`r2-backup-credentials` secret) already exist in the `data` namespace.

**Risk:** Low. Creating secrets does not disrupt running workloads.

**Templates:** `infra/k8s/production/backup/github-backup-secrets.yaml.template` and `infra/k8s/production/backup/cloudflare-api-secrets.yaml.template`

### 2A. GitHub Backup Credentials

Required: A GitHub PAT with `repo` read scope for the `madfam-org` organization.

```bash
kubectl create secret generic github-backup-credentials \
  -n data \
  --from-literal=github-pat="<GITHUB_PAT_WITH_REPO_READ_SCOPE>" \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 2B. Cloudflare API Credentials

Required: A Cloudflare API token with DNS Read and Tunnel Read permissions.

```bash
kubectl create secret generic cloudflare-api-credentials \
  -n data \
  --from-literal=api-token="<CF_API_TOKEN_WITH_DNS_READ_AND_TUNNEL_READ>" \
  --from-literal=zone-id-enclii="<ENCLII_DEV_ZONE_ID>" \
  --from-literal=zone-id-madfam="<MADFAM_IO_ZONE_ID>" \
  --from-literal=account-id="<CF_ACCOUNT_ID>" \
  --dry-run=client -o yaml | kubectl apply -f -
```

### Verification

```bash
# Confirm secrets exist
kubectl get secrets -n data | grep -E "github-backup|cloudflare-api"
# Expected: both secrets listed

# Run a manual GitHub backup job to confirm credentials work
kubectl create job github-backup-manual --from=cronjob/github-repos-backup -n data
kubectl logs -n data job/github-backup-manual -c clone-bundle -f
kubectl logs -n data job/github-backup-manual -c upload -f

# Run a manual Cloudflare backup job
kubectl create job cloudflare-backup-manual --from=cronjob/cloudflare-config-backup -n data
kubectl logs -n data job/cloudflare-backup-manual -c backup -f

# Cleanup test jobs
kubectl delete job github-backup-manual cloudflare-backup-manual -n data
```

---

## Section 3 -- Vault Cluster Deployment (P1)

**Context:** Vault ArgoCD application (`infra/argocd/apps/vault.yaml`), Helm values (`infra/helm/vault/values.yaml`), and 19 ExternalSecret manifests (`infra/k8s/base/external-secrets/vault-secrets/`) are all committed to git. ArgoCD will deploy the Vault pod automatically, but Vault requires manual initialization, unsealing, auth configuration, and secret population before ExternalSecrets can sync.

**Pre-conditions:**
- ArgoCD vault app synced (PR #64 merged)
- Longhorn CSI operational (provides `longhorn` StorageClass for Vault's 5Gi data + 2Gi audit PVCs)
- ESO operator running (`external-secrets` namespace)
- Backup credentials created (Section 2) -- recommended before Vault deployment so you can immediately back up Vault init keys

**Risk:** Medium. Vault initialization produces irreplaceable unseal keys. Loss of these keys means permanent data loss for all Vault-managed secrets.

### Step 1: Verify ArgoCD Syncs Vault

```bash
kubectl get application vault -n argocd
# Expected: Synced (may show Degraded or Progressing until Vault is initialized)

kubectl get pods -n vault
# Expected: vault-0 Running (but not Ready -- readiness probe fails until initialized)
```

### Step 2: Initialize Vault

```bash
kubectl exec -n vault vault-0 -- vault operator init \
  -key-shares=5 -key-threshold=3 -format=json > vault-init.json
```

**CRITICAL:** Store `vault-init.json` securely OFF-CLUSTER immediately. This file contains the 5 unseal keys and the initial root token. Recommended storage:
- Password manager (1Password, Bitwarden)
- Encrypted USB drive in a physically secure location
- Split keys across multiple secure storage locations

Loss of unseal keys = permanent loss of all Vault data.

### Step 3: Unseal Vault

Vault requires 3 of 5 unseal keys. Run the unseal command three times with different keys:

```bash
# Extract keys from init output (or enter manually from secure storage)
UNSEAL_KEY_1=$(jq -r '.unseal_keys_b64[0]' vault-init.json)
UNSEAL_KEY_2=$(jq -r '.unseal_keys_b64[1]' vault-init.json)
UNSEAL_KEY_3=$(jq -r '.unseal_keys_b64[2]' vault-init.json)

kubectl exec -n vault vault-0 -- vault operator unseal "$UNSEAL_KEY_1"
kubectl exec -n vault vault-0 -- vault operator unseal "$UNSEAL_KEY_2"
kubectl exec -n vault vault-0 -- vault operator unseal "$UNSEAL_KEY_3"

# Verify unsealed
kubectl exec -n vault vault-0 -- vault status
# Expected: Sealed = false
```

### Step 4: Enable KV Secrets Engine

```bash
ROOT_TOKEN=$(jq -r '.root_token' vault-init.json)

kubectl exec -n vault vault-0 -- sh -c "
  export VAULT_TOKEN='$ROOT_TOKEN'
  vault secrets enable -path=secret kv-v2
"
```

### Step 5: Configure Kubernetes Auth Method

If Vault is already initialized and unsealed but `vault-store` is
`InvalidProviderConfig` with a Vault Kubernetes auth HTTP 403, use the focused
repair wrapper. It requires an operator-approved Vault token and never prints
the token:

```bash
VAULT_TOKEN="$ROOT_TOKEN" ./scripts/repair-vault-eso-auth.sh
```

Then verify:

```bash
kubectl get clustersecretstore vault-store
```

For a first bootstrap, the equivalent manual commands are:

```bash
kubectl exec -n vault vault-0 -- sh -c "
  export VAULT_TOKEN='$ROOT_TOKEN'
  vault auth enable kubernetes
  vault write auth/kubernetes/config \
    kubernetes_host='https://kubernetes.default.svc:443'
"
```

### Step 6: Configure ESO Reader Policy and Role

```bash
kubectl exec -n vault vault-0 -- sh -c "
  export VAULT_TOKEN='$ROOT_TOKEN'

  # Create read-only policy for ESO
  vault policy write eso-reader - <<POLICY
path \"secret/data/*\" {
  capabilities = [\"read\"]
}
path \"secret/metadata/*\" {
  capabilities = [\"read\", \"list\"]
}
POLICY

  # Bind policy to ESO service account
  vault write auth/kubernetes/role/eso-reader \
    bound_service_account_names=external-secrets \
    bound_service_account_namespaces=external-secrets \
    bound_audiences=vault \
    policies=eso-reader \
    ttl=1h
"
```

### Step 7: Write Initial Secrets

Migrate existing K8s secrets to Vault for all 19 namespaces covered by ExternalSecret manifests. Extract current secret values from running K8s secrets and write them to Vault.

```bash
# Example: Extract current enclii secrets and write to Vault
# First, get current values from the K8s secret:
kubectl get secret enclii-secrets -n enclii -o json | jq -r '.data | to_entries[] | "\(.key)=\(.value | @base64d)"'

# Then write to Vault:
kubectl exec -n vault vault-0 -- sh -c "
  export VAULT_TOKEN='$ROOT_TOKEN'
  vault kv put secret/enclii \
    database_url='<value>' \
    redis_url='<value>' \
    janua_client_id='<value>' \
    janua_client_secret='<value>' \
    github_webhook_secret='<value>' \
    switchyard_api_key='<value>' \
    jwt_secret='<value>' \
    oidc_issuer='<value>' \
    oidc_redirect_uri='<value>' \
    postgres_password='<value>' \
    r2_access_key_id='<value>' \
    r2_secret_access_key='<value>' \
    r2_endpoint='<value>' \
    redis_password='<value>' \
    github_token='<value>' \
    cloudflare_api_token='<value>' \
    cloudflare_tunnel_token='<value>' \
    argocd_webhook_secret='<value>' \
    dispatch_secret_key='<value>' \
    status_admin_secret='<value>' \
    waybill_api_key='<value>' \
    roundhouse_registry_password='<value>' \
    posthog_api_key='<value>'
"
```

Repeat for each namespace. The full list of namespaces with ExternalSecret manifests:

| Namespace | Manifest |
|-----------|----------|
| enclii | `enclii-secrets.yaml` |
| janua | `janua-secrets.yaml` |
| data | `data-secrets.yaml` |
| dhanam | `dhanam-secrets.yaml` |
| monitoring | `monitoring-secrets.yaml` |
| cloudflare-tunnel | `cloudflare-secrets.yaml` |
| tezca | `tezca-secrets.yaml` |
| yantra4d | `yantra4d-secrets.yaml` |
| karafiel | `karafiel-secrets.yaml` |
| forgesight | `forgesight-secrets.yaml` |
| pravara-mes | `pravara-mes-secrets.yaml` |
| autoswarm | `autoswarm-secrets.yaml` |
| status | (covered by `enclii-secrets.yaml`) |
| kyverno | `kyverno-secrets.yaml` |
| arc-runners | `arc-runners-secrets.yaml` |
| enclii-builds | `enclii-builds-secrets.yaml` |
| npm-registry | `npm-registry-secrets.yaml` |
| longhorn-system | `longhorn-secrets.yaml` |
| posthog | `posthog-secrets.yaml` |
| madfam-site | `madfam-site-secrets.yaml` |

### Step 8: Revoke Root Token

After all secrets are written and ESO is syncing, revoke the root token for security:

```bash
kubectl exec -n vault vault-0 -- sh -c "
  export VAULT_TOKEN='$ROOT_TOKEN'
  vault token revoke '$ROOT_TOKEN'
"
```

Generate a new limited token when administrative access is needed:

```bash
# Re-authenticate with unseal keys if root access is needed later:
kubectl exec -n vault vault-0 -- vault operator generate-root -init
```

### Verification

```bash
# Check all ExternalSecrets sync status
kubectl get externalsecrets -A
# Expected: all show Status: SecretSynced

# Detailed status check
kubectl get externalsecrets -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}: {.status.conditions[0].reason}{"\n"}{end}'

# Verify a specific synced secret has data
kubectl get secret enclii-secrets -n enclii -o jsonpath='{.data}' | jq 'keys'

# Check Vault application health in ArgoCD
kubectl get application vault -n argocd
# Expected: Synced, Healthy
```

**Post-deployment:** See [Vault Operations Runbook](./VAULT_OPERATIONS.md) for ongoing operations (unsealing after restarts, secret rotation, backup procedures).

---

## Section 4 -- Cosign Enforce Activation (P1)

**Context:** The Kyverno ClusterPolicy `verify-image-signatures` is committed to git at `infra/k8s/base/kyverno/policies/image-policies.yaml` with `validationFailureAction: Enforce`. It uses keyless Cosign verification with GitHub Actions OIDC (`issuer: https://token.actions.githubusercontent.com`) and a `subjectRegExp` scoped to MADFAM GitHub Actions workflows on `main` or version tags. The policy only applies to namespaces with the label `enclii.dev/verify-signatures: "true"`.

**Pre-conditions:**
- PR #47 merged (Kyverno policy in Enforce mode)
- All `ghcr.io/madfam-org` images in target namespaces are signed via CI (cosign keyless signing in GitHub Actions)
- Kyverno operator running and healthy
- PolicyExceptions exist for ecosystem namespaces with unsigned images (autoswarm, posthog, etc.)

**Risk:** Medium. Enabling on a namespace blocks all pod creation for unsigned `ghcr.io/madfam-org` images. Always verify signatures before labeling a namespace.

### Step 1: Verify All Images Are Signed

```bash
# List all unique ghcr.io/madfam-org images running on the cluster
kubectl get pods -A -o jsonpath='{range .items[*]}{range .spec.containers[*]}{.image}{"\n"}{end}{end}' \
  | grep ghcr.io/madfam-org | sort -u

# Verify each image has a cosign signature from an approved MADFAM workflow
# (replace image references from the output above)
for img in $(kubectl get pods -A -o jsonpath='{range .items[*]}{range .spec.containers[*]}{.image}{"\n"}{end}{end}' | grep ghcr.io/madfam-org | sort -u); do
  echo -n "$img: "
  cosign verify \
    --certificate-identity-regexp="^https://github\\.com/madfam-org/[A-Za-z0-9_.-]+/\\.github/workflows/[A-Za-z0-9_.-]+\\.ya?ml@refs/(heads/main|tags/v[0-9].*)$" \
    --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
    "$img" 2>/dev/null && echo "SIGNED" || echo "UNSIGNED"
done
```

If any images are UNSIGNED, do NOT proceed with labeling that namespace. Either:
- Rebuild the image via CI (which signs automatically), or
- Create a PolicyException for that namespace

### Step 2: Enable Per-Namespace (Gradual Rollout)

Start with the `enclii` namespace where all images are first-party and signed via CI:

```bash
# Phase 1: Enclii namespace (core platform services)
kubectl label ns enclii enclii.dev/verify-signatures=true --overwrite

# Monitor pod status -- existing pods are unaffected, only new pods are checked
kubectl get pods -n enclii -w
# Wait 60 seconds, verify no disruption
```

```bash
# Phase 2: Status and monitoring namespaces
for ns in status monitoring; do
  kubectl label ns "$ns" enclii.dev/verify-signatures=true --overwrite
  echo "Labeled $ns -- checking pods..."
  kubectl get pods -n "$ns"
done
```

```bash
# Phase 3: Remaining first-party namespaces (after verifying signatures)
for ns in enclii-builds; do
  kubectl label ns "$ns" enclii.dev/verify-signatures=true --overwrite
done
```

**Warning:** Do NOT label ecosystem namespaces (`tezca`, `yantra4d`, `karafiel`, `forgesight`, `pravara-mes`, `autoswarm`, `dhanam`) unless their images are signed or a PolicyException is in place. These namespaces use images that may not go through the signing CI pipeline.

**Kyverno gotcha:** If a namespace has `enclii.dev/verify-signatures=true` but contains unsigned images, `kubectl rollout restart` will fail because the new pods cannot be created. You must either:
1. Remove the label: `kubectl label ns <ns> enclii.dev/verify-signatures-`, or
2. Sign the images first, or
3. Add a PolicyException for that namespace

### Verification

```bash
# Test: Attempt to deploy an unsigned image (should be blocked)
kubectl run test-unsigned --image=nginx:latest -n enclii --dry-run=server
# Expected output: admission webhook denied the request
#   (blocked by verify-image-signatures policy)

# Test: Verify a signed image can deploy
kubectl run test-signed --image=ghcr.io/madfam-org/switchyard-api:latest -n enclii --dry-run=server
# Expected: pod created (dry-run)

# Check Kyverno policy reports for violations
kubectl get policyreport -A
```

---

## Section 5 -- PostHog Orphaned Volume Cleanup (P0)

**Context:** The PostHog Helm deployment was paused (chart v30.46.0 is fundamentally broken -- unmaintained since May 2023, ClickHouse migrations expect multi-cluster topology + AWS MSK). ArgoCD sync is paused for the PostHog application. Up to 3 Longhorn volumes (~44GB total) may remain detached, consuming disk space on foundry-worker-01.

**Pre-conditions:** None. This is a cleanup operation.

**Risk:** Low. PostHog is not running; these volumes contain no active data. Analytics are handled by the Cloudflare Worker proxy at `analytics.madfam.io`.

### Step 1: Identify Orphaned Volumes

```bash
# Check for detached Longhorn volumes
kubectl get volumes.longhorn.io -n longhorn-system \
  -o custom-columns='NAME:.metadata.name,STATE:.status.state,ROBUSTNESS:.status.robustness,SIZE:.spec.size' \
  | grep -E "detached|unknown"

# Check PostHog PVCs specifically
kubectl get pvc -n posthog 2>/dev/null || echo "No posthog namespace or no PVCs"
```

### Step 2: Delete Orphaned PostHog PVCs and Volumes

```bash
# Delete all PVCs in the posthog namespace (Longhorn volumes auto-cleanup with Retain/Delete policy)
kubectl delete pvc --all -n posthog --ignore-not-found

# Wait 30 seconds for Longhorn to process volume deletion
sleep 30

# If volumes persist as detached after PVC deletion:
kubectl get volumes.longhorn.io -n longhorn-system | grep -i detach
# For each remaining orphaned volume:
# kubectl delete volumes.longhorn.io <volume-name> -n longhorn-system
```

### Step 3: Optionally Clean Up the Namespace

If PostHog will not be redeployed in the near term:

```bash
# Check what remains in the namespace
kubectl get all -n posthog

# The namespace itself is managed by ArgoCD (CreateNamespace=true) --
# do NOT delete it manually or ArgoCD will recreate it on next sync.
# Instead, leave it empty or pause the ArgoCD app (already paused).
```

### Verification

```bash
# Confirm no detached volumes remain (or only intentionally detached ones)
kubectl get volumes.longhorn.io -n longhorn-system | grep -c detached
# Expected: 0 (or known non-PostHog volumes only)

# Check disk space recovered on foundry-worker-01 (via SSH)
ssh -o ProxyCommand="cloudflared access ssh --hostname %h" solarpunk@ssh.madfam.io \
  "df -h / && df -h /var/lib/longhorn"
```

---

## Section 6 -- Restore Drill Execution (P2)

**Context:** The `postgres-restore-drill` CronJob runs monthly (1st of each month, 5 AM UTC) and validates that database backups can be restored successfully. This section covers running a manual drill to confirm the backup pipeline is functional, especially after creating backup credentials (Section 2).

**Pre-conditions:**
- `r2-backup-credentials` secret exists in the `data` namespace
- At least one successful `postgres-backup` job has run (daily at 3 AM UTC)

**Risk:** Low. The restore drill is non-destructive. It creates a temporary database (`enclii_restore_test`), validates it, and drops it.

### Run Manual Restore Drill

```bash
kubectl create job restore-drill-manual --from=cronjob/postgres-restore-drill -n data
kubectl logs -n data job/restore-drill-manual -f
```

### Interpret Results

A successful drill ends with:

```
=== RESTORE DRILL PASSED ===
  Backup: YYYYMMDD_HHMMSS.sql.gz
  Tables: N
  Time: YYYY-MM-DDTHH:MM:SSZ
```

A failed drill requires investigation. See [Restore Drill Log](./RESTORE_DRILL_LOG.md) for common failure modes and actions.

### Record Results

Add a row to the results table in `docs/runbooks/RESTORE_DRILL_LOG.md`:

| Field | Source |
|-------|--------|
| Date | Current date |
| Backup Source | Filename from drill output |
| Tables Restored | Table count from drill output |
| Duration | Elapsed time from drill output |
| Pass/Fail | Final status from drill output |
| Operator | Your initials |
| Notes | Any observations |

### Cleanup

```bash
kubectl delete job restore-drill-manual -n data

# Verify cleanup
kubectl get job restore-drill-manual -n data 2>/dev/null || echo "Job cleaned up"
```

---

## Section 7 -- ESO CRD Migration Plan (P1, Deferred) {#section-7--eso-crd-migration-plan-p1-deferred}

**Context:** The External Secrets Operator is pinned at v0.9.11 (uses `v1beta1` CRDs). The target version is v0.16.2 (uses `v1` CRDs). The current version is stable and functional. Migration requires a maintenance window because CRD upgrades are irreversible and affect all ExternalSecret resources cluster-wide.

**Current configuration:** `infra/argocd/apps/external-secrets-operator.yaml` (chart version `0.9.11`, 2 replicas, PDB enabled).

**Pre-conditions:**
- Scheduled maintenance window communicated to stakeholders
- All ExternalSecrets in `SecretSynced` state before starting
- Vault operational (Section 3) so secrets can be re-synced after migration
- Cluster backup completed (k3s datastore + PostgreSQL)

**Risk:** High. CRD migration affects all namespaces. If the new CRDs are incompatible with existing ExternalSecret manifests, secret syncing stops cluster-wide until resolved.

### Pre-Migration Backup

```bash
# Backup all ESO custom resources
kubectl get externalsecrets -A -o yaml > eso-externalsecrets-backup-$(date +%Y%m%d).yaml
kubectl get clustersecretstores -o yaml > eso-clustersecretstores-backup-$(date +%Y%m%d).yaml
kubectl get secretstores -A -o yaml > eso-secretstores-backup-$(date +%Y%m%d).yaml

# Backup current CRDs
kubectl get crd externalsecrets.external-secrets.io -o yaml > eso-crd-externalsecrets-$(date +%Y%m%d).yaml
kubectl get crd clustersecretstores.external-secrets.io -o yaml > eso-crd-clustersecretstores-$(date +%Y%m%d).yaml
kubectl get crd secretstores.external-secrets.io -o yaml > eso-crd-secretstores-$(date +%Y%m%d).yaml

# Verify backup files are non-empty
wc -l eso-*-backup-*.yaml eso-crd-*.yaml
```

### Migration Steps

1. **Scale down ESO operator** to prevent reconciliation during CRD swap:

    ```bash
    kubectl scale deployment external-secrets -n external-secrets --replicas=0
    kubectl scale deployment external-secrets-webhook -n external-secrets --replicas=0
    kubectl scale deployment external-secrets-cert-controller -n external-secrets --replicas=0
    ```

2. **Update ArgoCD app** to target new chart version. Edit `infra/argocd/apps/external-secrets-operator.yaml`:

    ```yaml
    targetRevision: 0.16.2  # was 0.9.11
    ```

    Commit and push. ArgoCD will apply the new CRDs and upgrade the Helm release.

3. **Verify CRD migration**:

    ```bash
    kubectl get crd externalsecrets.external-secrets.io -o jsonpath='{.spec.versions[*].name}'
    # Expected: should include v1 (may also include v1beta1 for backward compatibility)
    ```

4. **Scale up ESO operator**:

    ```bash
    kubectl scale deployment external-secrets -n external-secrets --replicas=2
    kubectl scale deployment external-secrets-webhook -n external-secrets --replicas=2
    kubectl scale deployment external-secrets-cert-controller -n external-secrets --replicas=1
    ```

5. **Verify all ExternalSecrets reconcile**:

    ```bash
    kubectl get externalsecrets -A
    # All should show SecretSynced within 15 minutes (the configured refreshInterval)

    # Watch for errors
    kubectl logs -n external-secrets -l app.kubernetes.io/name=external-secrets -f --tail=50
    ```

### Rollback

If ExternalSecrets fail to sync after migration:

```bash
# Revert the chart version in ArgoCD app
# Edit infra/argocd/apps/external-secrets-operator.yaml back to 0.9.11
# Commit and push, or:
kubectl edit application external-secrets -n argocd
# Change targetRevision back to 0.9.11

# If CRDs are incompatible, restore from backup
kubectl apply -f eso-crd-externalsecrets-*.yaml
kubectl apply -f eso-crd-clustersecretstores-*.yaml
kubectl apply -f eso-crd-secretstores-*.yaml

# Restore ExternalSecret resources if needed
kubectl apply -f eso-externalsecrets-backup-*.yaml
kubectl apply -f eso-clustersecretstores-backup-*.yaml
kubectl apply -f eso-secretstores-backup-*.yaml
```

---

## Execution Priority

| Section | Priority | Risk | Est. Duration | Dependencies |
|---------|----------|------|---------------|--------------|
| 5. PostHog Cleanup | P0 | Low | 10 min | None |
| 1. CronJob Cleanup | P2 | Low | 2 min | Git PR merged |
| 2. Backup Credentials | P1 | Low | 10 min | Credentials available |
| 6. Restore Drill | P2 | Low | 15 min | Section 2 |
| 3. Vault Deploy | P1 | Medium | 30 min | Section 2 (for credential backup) |
| 4. Cosign Enforce | P1 | Medium | 20 min | PR #47 merged, images signed |
| 7. ESO Migration | P1 | High | 45 min | Maintenance window, Section 3 |

**Recommended execution order:** 5 > 1 > 2 > 6 > 3 > 4 > 7 (deferred)

This order minimizes risk by starting with zero-dependency cleanups, then establishing backup infrastructure, then deploying new systems, and deferring the highest-risk migration until a dedicated maintenance window.

---

## Related Documentation

- [Vault Operations Runbook](./VAULT_OPERATIONS.md) -- Ongoing Vault management (unsealing, rotation, backup)
- [Backup Coverage Report](./BACKUP_COVERAGE.md) -- Backup schedule, retention, alerting
- [Restore Drill Log](./RESTORE_DRILL_LOG.md) -- Monthly restore drill results
- [Database Recovery Runbook](./DATABASE_RECOVERY.md) -- PostgreSQL backup and recovery procedures
- [Disaster Recovery Runbook](./DISASTER_RECOVERY.md) -- Full cluster failure scenarios
- [External Secrets Documentation](../infrastructure/EXTERNAL_SECRETS.md) -- ESO architecture and configuration
- [GitOps with ArgoCD](../infrastructure/GITOPS.md) -- ArgoCD App-of-Apps pattern
- [Storage (Longhorn)](../infrastructure/STORAGE.md) -- Longhorn CSI volume management
