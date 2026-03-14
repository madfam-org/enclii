# Secret Rotation Log

This document tracks all credential rotations for the Enclii production cluster
and provides the standard operating procedure for performing rotations.

---

## Git History Disclosure

Prior to 2026-03-08, the file `infra/k8s/base/secrets.production.yaml` contained
plaintext credentials committed to git:

| Secret | Exposure | Status |
|--------|----------|--------|
| RSA private key (`jwt-secrets.private-key`) | Full PEM in git history | **Inert** -- key was a dev placeholder; production signing uses a separately-provisioned key applied via `kubectl` |
| PostgreSQL password (rotated) | Plaintext in git history | **Inert** -- password was rotated on-cluster; the value in git history no longer grants access |

**Remediation (2026-03-08):**
- `secrets.production.yaml` replaced with `MANAGED_VIA_KUBECTL` placeholder stubs
- Real secrets remain only on the cluster, applied via `kubectl create secret`
- ArgoCD `ignoreDifferences` already configured for Secret `.data`/`.stringData`
  (see `infra/argocd/apps/project-appset.yaml` lines 84-87)

> **Note:** Git history still contains the old values. Because both credentials
> have been rotated on-cluster, the historical exposure carries no active risk.
> If the repository is ever made public, consider using `git filter-repo` or
> BFG Repo-Cleaner to purge the old commits.

---

## Rotation Procedure

### Prerequisites

- `kubectl` configured with admin access to the production cluster
- Access to the `enclii` namespace (or target namespace)
- For JWT: `openssl` available locally

### 1. Rotate JWT Signing Key (RS256)

```bash
# Generate new RSA-2048 key pair
openssl genrsa -out /tmp/jwt-private.pem 2048

# Apply to cluster (dry-run + apply pattern avoids "already exists" errors)
kubectl -n enclii create secret generic jwt-secrets \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=jwt-issuer="enclii-production" \
  --from-file=private-key=/tmp/jwt-private.pem \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart switchyard-api to pick up new key
kubectl -n enclii rollout restart deployment/switchyard-api

# Verify pods healthy
kubectl -n enclii rollout status deployment/switchyard-api --timeout=120s

# Clean up local key material
rm /tmp/jwt-private.pem
```

**Impact:** All existing JWT tokens become invalid. Users must re-authenticate.
Plan rotations during a maintenance window.

### 2. Rotate PostgreSQL Password

```bash
# 1. Generate new password
NEW_PW="$(openssl rand -base64 24)"

# 2. Update the password in PostgreSQL first
kubectl -n data exec -it deploy/postgres -- \
  psql -U postgres -c "ALTER USER enclii WITH PASSWORD '${NEW_PW}';"

# 3. Update the Kubernetes secret
kubectl -n enclii create secret generic postgres-credentials \
  --from-literal=username="enclii" \
  --from-literal=password="${NEW_PW}" \
  --from-literal=database="enclii" \
  --from-literal=database-url="postgres://enclii:${NEW_PW}@postgres.data.svc.cluster.local:5432/enclii?sslmode=disable" \
  --dry-run=client -o yaml | kubectl apply -f -

# 4. Restart all pods that consume postgres-credentials
kubectl -n enclii rollout restart deployment/switchyard-api

# 5. Verify connectivity
kubectl -n enclii exec deploy/switchyard-api -- /app/healthcheck db

# 6. Clear the variable from shell history
unset NEW_PW
```

**Impact:** Brief downtime during pod restart. Coordinate with any services
that reference `postgres-credentials` (switchyard-api, postgres-exporter,
postgres-backup CronJob, restore-drill CronJob).

### 3. Post-Rotation Verification Checklist

- [ ] New secret applied to cluster (`kubectl get secret <name> -n enclii -o yaml`)
- [ ] Dependent deployments restarted and healthy
- [ ] Health endpoints returning 200 (`curl https://api.enclii.dev/health`)
- [ ] No authentication errors in logs (`kubectl logs -n enclii deploy/switchyard-api --tail=50`)
- [ ] Add entry to rotation log below

---

## Rotation History

| Date | Secret | Rotated By | Reason | Notes |
|------|--------|------------|--------|-------|
| 2026-03-08 | `jwt-secrets`, `postgres-credentials` | Platform team | Git exposure remediation | Placeholder stubs committed; cluster values unchanged (already rotated) |
| | | | | |

<!-- Add new entries above this line. Keep the empty row as a template. -->
