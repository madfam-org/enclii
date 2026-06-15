# Secrets Management Guide

> **Canonical doc:** [`docs/infrastructure/SECRETS_MANAGEMENT.md`](../docs/infrastructure/SECRETS_MANAGEMENT.md) (Vault + ESO + [Secret Intake](../docs/runbooks/SECRET_INTAKE.md)). This file is legacy; do not treat Sealed Secrets as the current production path.

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


## ⚠️ CRITICAL SECURITY NOTICE

This document is retained for historical context and migration reference. Do
not use the raw `kubectl`, `kubeseal`, or direct secret-creation examples below
for routine production operations. Current production secret handling is
HashiCorp Vault plus External Secrets Operator through the Enclii control-plane
workflow documented in the canonical secrets guide.

The secrets in `infra/k8s/base/secrets.dev.yaml` are **DEVELOPMENT ONLY** and must **NEVER** be used in production. Production uses `secrets.production.yaml` which contains `jwt-secrets` and `postgres-credentials` (pointing to the real database in the `data` namespace).

## Legacy Secret Management Patterns

The options below are legacy patterns or background context. They are not the
current MADFAM production operating path.

#### 1. Sealed Secrets (legacy GitOps option)

**Why it existed**: GitOps-friendly, encrypted at rest, decrypted only in-cluster

**Setup**:
```bash
# Install sealed-secrets controller
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.0/controller.yaml

# Install kubeseal CLI
brew install kubeseal

# Create a secret and seal it
kubectl create secret generic postgres-credentials \
  --from-literal=username=prod_user \
  --from-literal=password='<STRONG_PASSWORD>' \
  --dry-run=client -o yaml | \
  kubeseal -o yaml > sealed-secret.yaml

# Commit sealed-secret.yaml to git (safe!)
git add sealed-secret.yaml
```

#### 2. External Secrets Operator

**Why**: Centralized secret management, integrates with Vault/AWS Secrets Manager/GCP Secret Manager

**Setup**:
```bash
# Install External Secrets Operator
helm repo add external-secrets https://charts.external-secrets.io
helm install external-secrets external-secrets/external-secrets -n external-secrets-system --create-namespace

# Create ExternalSecret resource
cat <<EOF | kubectl apply -f -
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: postgres-credentials
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: vault-backend
    kind: SecretStore
  target:
    name: postgres-credentials
  data:
  - secretKey: password
    remoteRef:
      key: database/postgres
      property: password
EOF
```

#### 3. HashiCorp Vault

**Why**: Enterprise-grade secret management, dynamic secrets, audit logging

**Setup**:
```bash
# Install Vault
helm repo add hashicorp https://helm.releases.hashicorp.com
helm install vault hashicorp/vault

# Store secrets
vault kv put secret/database/postgres password='<STRONG_PASSWORD>'

# Use Vault Agent Injector in pods
kubectl annotate pod my-app \
  vault.hashicorp.com/agent-inject="true" \
  vault.hashicorp.com/agent-inject-secret-database="secret/database/postgres" \
  vault.hashicorp.com/role="enclii-app"
```

## Secret Rotation

### Database Passwords

```bash
# Using zero-downtime rotation (built into Enclii)
enclii secrets rotate --secret postgres-password --service my-service

# Manual rotation with Vault
vault write database/rotate-root/postgres-connection
```

### JWT Signing Keys

```bash
# Generate new RSA keypair
openssl genrsa -out private-new.pem 2048
openssl rsa -in private-new.pem -pubout -out public-new.pem

# Update secret (keep old key for token validation)
kubectl create secret generic jwt-secrets \
  --from-file=private-key=private-new.pem \
  --from-file=public-key=public-new.pem \
  --from-file=private-key-old=private-old.pem \
  --dry-run=client -o yaml | kubeseal -o yaml | kubectl apply -f -

# Rolling restart switchyard-api
kubectl rollout restart deployment/switchyard-api
```

### Container Registry Credentials

```bash
# Create GitHub Personal Access Token with `read:packages` scope
# Update registry secret
kubectl create secret docker-registry registry-secret \
  --docker-server=ghcr.io \
  --docker-username=<GITHUB_USERNAME> \
  --docker-password=<GITHUB_TOKEN> \
  --dry-run=client -o yaml | kubeseal -o yaml | kubectl apply -f -
```

## Security Best Practices

### 1. Never Commit Secrets to Git

✅ **DO**:
- Use sealed secrets or external secret managers
- Use environment variables for local development
- Use `.env.example` with placeholder values
- Add `*.env` to `.gitignore`

❌ **DON'T**:
- Commit plaintext secrets to git
- Use weak passwords even for development
- Share secrets via Slack/email
- Hardcode secrets in code

### 2. Use Strong Secrets

Generate cryptographically strong secrets:

```bash
# Random password (32 characters)
openssl rand -base64 32

# Random hex (64 characters)
openssl rand -hex 32

# UUID
uuidgen
```

### 3. Principle of Least Privilege

Each service should have its own credentials:

```yaml
# ❌ Bad: All services use same database user
database-url: postgres://admin:password@postgres/enclii

# ✅ Good: Each service has limited permissions
database-url: postgres://switchyard_user:password@postgres/enclii
database-url: postgres://readonly_user:password@postgres/enclii
```

### 4. Enable SSL/TLS

Always use encrypted connections:

```yaml
# ❌ Bad
database-url: postgres://user:pass@host/db?sslmode=disable

# ✅ Good
database-url: postgres://user:pass@host/db?sslmode=require

# ✅ Better: Verify certificate
database-url: postgres://user:pass@host/db?sslmode=verify-full&sslrootcert=/path/to/ca.crt
```

### 5. Audit Secret Access

Enable audit logging for secret access:

```yaml
# Kubernetes audit policy
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
- level: RequestResponse
  resources:
  - group: ""
    resources: ["secrets"]
```

## Compliance Requirements

### SOC 2

- ✅ Secrets must be encrypted at rest
- ✅ Secrets must be encrypted in transit (TLS)
- ✅ Secret access must be audited
- ✅ Secrets must be rotated periodically (90 days recommended)
- ✅ Secrets must not be committed to version control

### HIPAA

- ✅ Secrets must be encrypted with AES-256 or stronger
- ✅ Access to secrets must require MFA for administrators
- ✅ Secret rotation must be automated
- ✅ Secrets must have access logs

## Migration from Development Secrets

### Step 1: Audit Current Secrets

```bash
# Find all secrets in the cluster
kubectl get secrets --all-namespaces

# Find hardcoded secrets in code
grep -r "password\|secret\|token" apps/
```

### Step 2: Create Production Secrets

Use the secrets.yaml.TEMPLATE as a starting point:

```bash
cp infra/k8s/base/secrets.yaml.TEMPLATE infra/k8s/production/secrets.yaml
# Edit secrets.yaml with production values
# Seal the secrets
kubeseal -f infra/k8s/production/secrets.yaml -o yaml > infra/k8s/production/sealed-secrets.yaml
```

### Step 3: Update Deployments

```yaml
# Update deployment to use new secret names
envFrom:
- secretRef:
    name: postgres-credentials  # Now a sealed secret
```

### Step 4: Verify

```bash
# Test that app can access secrets
kubectl exec -it deployment/switchyard-api -- env | grep DATABASE

# Check audit logs
kubectl logs -n kube-system deployment/audit-policy-webhook
```

### Step 5: Remove Development Secrets

```bash
# Delete development secrets
kubectl delete secret postgres-credentials
kubectl delete secret jwt-secrets

# Update .gitignore
echo "infra/k8s/*/secrets.yaml" >> .gitignore
echo "!infra/k8s/*/sealed-secrets.yaml" >> .gitignore
```

## Troubleshooting

### Secret Not Found

```bash
# Check if secret exists
kubectl get secret postgres-credentials

# Check if sealed secret was created
kubectl get sealedsecret postgres-credentials

# Check controller logs
kubectl logs -n kube-system deployment/sealed-secrets-controller
```

### Permission Denied

```bash
# Check service account
kubectl get serviceaccount switchyard-api -o yaml

# Check RBAC
kubectl auth can-i get secrets --as=system:serviceaccount:default:switchyard-api
```

### Secret Not Decrypted

```bash
# Check sealed secret status
kubectl describe sealedsecret postgres-credentials

# Re-seal with correct certificate
kubeseal --fetch-cert > pub-cert.pem
kubeseal --cert pub-cert.pem -f secret.yaml -o yaml > sealed-secret.yaml
```

## GHCR Credentials (madfam-bot)

### Overview

All private image pulls across the cluster use a single `ghcr-credentials` secret (type `docker-registry`), sourced from the `madfam-bot` GitHub account's fine-grained PAT.

### Covered Namespaces

| Namespace | Purpose |
|-----------|---------|
| `enclii` | Core platform services (switchyard-api, switchyard-ui, roundhouse) |
| `janua` | SSO authentication service |
| `dhanam` | Financial tracking service |
| `enclii-builds` | Kaniko build jobs (push + pull) |
| `argocd` | GitOps image updater |

### Creating a madfam-bot PAT

1. Log in as `madfam-bot` on GitHub
2. Go to **Settings → Developer settings → Fine-grained tokens**
3. Create token with:
   - **Name**: `ghcr-cluster-pull` (or similar)
   - **Resource owner**: `madfam-org`
   - **Expiry**: 1 year
   - **Repository access**: All repositories (or specific repos)
   - **Permissions**: `read:packages` (add `write:packages` for build pushes)
4. Copy the token immediately

### Running the Rotation Script

**GitHub Actions (preferred for single-namespace unblock):**

1. Open **madfam-org/enclii** → Actions → **Rotate GHCR credentials (namespace)**
2. `namespace`: target (e.g. `janua`, `dhanam`, `enclii`)
3. `reason`: audit string (≥12 characters)

Workflow: `.github/workflows/rotate-ghcr-namespace.yml` — uses `GHCR_PAT` and
in-cluster kubeconfig on ARC runners when `ARC_BOOTSTRAP_COMPLETE=true`.

**Local script (all namespaces):**

```bash
# From the enclii repo root:
GHCR_USERNAME=madfam-bot GHCR_PAT=ghp_xxx ./scripts/rotate-ghcr-credentials.sh

# Dry run (preview changes):
GHCR_USERNAME=madfam-bot GHCR_PAT=ghp_xxx ./scripts/rotate-ghcr-credentials.sh --dry-run
```

The script creates/updates the `ghcr-credentials` docker-registry secret in all 5 namespaces using idempotent `--dry-run=client | kubectl apply`.

### Rotation Schedule

- **Frequency**: Annually (or immediately if compromised)
- **Reminder**: Set a calendar event for 30 days before PAT expiry
- **Health check**: A monthly CronJob (`ghcr-credential-check`) validates that the secret exists in all namespaces and creates Warning events if missing

### Monitoring

```bash
# Check the CronJob status:
kubectl get cronjob ghcr-credential-check -n enclii

# View recent job runs:
kubectl get jobs -n enclii -l app.kubernetes.io/name=ghcr-credential-check

# Check for warning events:
kubectl get events --field-selector reason=MissingRegistryCredentials -A
```

## References

- [Kubernetes Secrets](https://kubernetes.io/docs/concepts/configuration/secret/)
- [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets)
- [External Secrets Operator](https://external-secrets.io)
- [HashiCorp Vault](https://www.vaultproject.io/docs)
- [OWASP Secrets Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html)
