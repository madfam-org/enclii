# Vault Operations Runbook

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Last Updated:** March 2026
**Vault URL:** https://vault.madfam.io
**Namespace:** vault
**Pod:** vault-0

---

## Quick Reference

```bash
# Check Vault status
kubectl exec -n vault vault-0 -- vault status

# Check seal status
kubectl exec -n vault vault-0 -- vault status -format=json | jq '.sealed'

# List all secrets in a namespace path
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" vault kv list secret/

# Read a secret
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" vault kv get secret/enclii

# Check ESO sync status
kubectl get externalsecrets -A
```

---

## Unsealing Vault

Vault seals itself on restart. It requires 3 of 5 unseal keys.

```bash
# Check if sealed
kubectl exec -n vault vault-0 -- vault status

# Unseal (repeat 3 times with different keys)
kubectl exec -n vault vault-0 -- vault operator unseal <KEY_1>
kubectl exec -n vault vault-0 -- vault operator unseal <KEY_2>
kubectl exec -n vault vault-0 -- vault operator unseal <KEY_3>

# Verify unsealed
kubectl exec -n vault vault-0 -- vault status
```

Unseal keys are stored securely outside the cluster. Contact the infrastructure lead for access.

---

## Secret Rotation

### Rotate a single secret

```bash
# 1. Update in Vault
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
  vault kv patch secret/<namespace> <key>=<new_value>

# 2. Force ESO refresh (instead of waiting 15 min)
kubectl annotate externalsecret <name> -n <namespace> \
  force-sync=$(date +%s) --overwrite

# 3. Restart affected pods to pick up new secret
kubectl rollout restart deploy/<service> -n <namespace>

# 4. Verify
kubectl get externalsecret <name> -n <namespace> -o jsonpath='{.status.conditions}'
```

### Rotate database credentials

```bash
# 1. Update password in PostgreSQL
kubectl exec -n data deploy/postgres -- psql -U postgres \
  -c "ALTER USER <user> WITH PASSWORD '<new_password>';"

# 2. Update Vault
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
  vault kv patch secret/<namespace> database_url="postgresql://<user>:<new_password>@..."

# 3. Force ESO refresh + restart pods (as above)
```

---

## Backup

### Manual backup of all Vault data

```bash
# Export all secret paths
for ns in enclii janua data cloudflare-tunnel dhanam selva tezca yantra4d \
  karafiel forgesight pravara-mes monitoring arc-runners enclii-builds \
  npm-registry madfam-site posthog longhorn-system kyverno; do
  echo "--- $ns ---"
  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
    vault kv get -format=json "secret/$ns" 2>/dev/null || echo "(empty)"
done
```

Store backups encrypted and off-cluster. Consider using Vault's built-in snapshot:

```bash
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
  vault operator raft snapshot save /tmp/vault-snapshot.snap
kubectl cp vault/vault-0:/tmp/vault-snapshot.snap ./vault-snapshot-$(date +%Y%m%d).snap
```

### Restore from snapshot

```bash
kubectl cp ./vault-snapshot.snap vault/vault-0:/tmp/vault-snapshot.snap
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
  vault operator raft snapshot restore /tmp/vault-snapshot.snap
```

---

## Adding Secrets for a New Namespace

1. Write secrets to Vault:
   ```bash
   kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
     vault kv put secret/<namespace> key1=val1 key2=val2
   ```

2. Create Vault policy:
   ```bash
   kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" \
     vault policy write <namespace>-read - <<EOF
   path "secret/data/<namespace>" {
     capabilities = ["read"]
   }
   EOF
   ```

3. Create ExternalSecret YAML at `infra/k8s/base/external-secrets/vault-secrets/<namespace>-secrets.yaml`

4. Commit and let ArgoCD sync, or apply directly:
   ```bash
   kubectl apply -f infra/k8s/base/external-secrets/vault-secrets/<namespace>-secrets.yaml
   ```

5. Verify:
   ```bash
   kubectl get externalsecret -n <namespace>
   kubectl get secret <name> -n <namespace> -o jsonpath='{.data}' | jq 'keys'
   ```

---

## Troubleshooting

### Vault pod not starting

```bash
kubectl describe pod vault-0 -n vault
kubectl logs vault-0 -n vault
# Check PVC
kubectl get pvc -n vault
```

### ExternalSecret stuck in "SecretSyncedError"

```bash
# Check ESO logs
kubectl logs -n external-secrets -l app.kubernetes.io/name=external-secrets -f

# Verify Vault is unsealed
kubectl exec -n vault vault-0 -- vault status
```

If the `ClusterSecretStore` itself is not ready:

```bash
kubectl get clustersecretstore vault-store
kubectl describe clustersecretstore vault-store
```

When `vault-store` reports `InvalidProviderConfig` and Vault returns
`permission denied` for `auth/kubernetes/login`, rebind the ESO Vault role with
an operator-approved Vault token:

```bash
VAULT_TOKEN="$TOKEN" ./scripts/repair-vault-eso-auth.sh
```

After `vault-store` is `Ready=True`, verify the target path and refresh the
specific ExternalSecret:

```bash
# Verify the path exists in Vault
kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$TOKEN" vault kv get secret/<namespace>

# Force refresh
kubectl annotate externalsecret <name> -n <namespace> force-sync=$(date +%s) --overwrite
```

### Vault sealed after pod restart

Vault seals on every restart. Set up auto-unseal (AWS KMS, GCP Cloud KMS, or Transit) to avoid manual intervention. Current setup requires manual unsealing with 3/5 keys.

---

## Switchyard Vault writer (secret intake + vault-backfill)

Switchyard-api can merge operator-supplied secrets into Vault KV v2 when
`ENCLII_SECRET_ROTATION_ENABLED=true` and `ENCLII_VAULT_TOKEN` is set from the
`vault-credentials` secret.

**Bootstrap (operator, break-glass Vault admin token — never in chat/git):**

```bash
VAULT_TOKEN_FILE=/path/to/vault-admin.token \
  ./scripts/provision-switchyard-vault-writer.sh
```

Add paths to an existing writer policy without rotating `vault-credentials`:

```bash
POLICY_ONLY=1 VAULT_TOKEN_FILE=/path/to/vault-admin.token \
  ./scripts/provision-switchyard-vault-writer.sh
```

**Verify:**

```bash
enclii secrets intake targets
```

**Submit (human only — masked):**

```bash
enclii secrets intake submit ceq/vast-api-key --reason "audit reason here"
```

See [Secret Intake Runbook](./SECRET_INTAKE.md) and private
`internal-devops/runbooks/2026-06-15-vault-bridge-gaps.md` for bridge status.

---

## Related Documentation

- [External Secrets Operator](../infrastructure/EXTERNAL_SECRETS.md)
- [Secrets Management Strategy](../infrastructure/SECRETS_MANAGEMENT.md)
- [Disaster Recovery](./DISASTER_RECOVERY.md)
