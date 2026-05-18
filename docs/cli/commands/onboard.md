# `enclii onboard`

Onboard a new repository with full provisioning — ArgoCD registration, namespace setup, database creation, K8s secrets, and R2 storage in a single command.

For apps that require authentication, run Janua OAuth bootstrap from the product repo as part of the same onboarding change. Enclii owns runtime provisioning; Janua owns identity provisioning; the product repo owns both desired-state manifests.

## Usage

```bash
enclii onboard --repo <org/repo> [flags]
```

## Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--repo` | Yes | — | GitHub repo in `org/name` format |
| `--project` | No | repo name | Project name |
| `--manifest-path` | No | `k8s/production` | K8s manifest path in repo |
| `--branch` | No | `main` | Branch to track |
| `--db-name` | No | — | Postgres database name to create |
| `--db-password` | No | prompted | Postgres role password |
| `--db-extensions` | No | — | Comma-separated Postgres extensions |
| `--secrets-file` | No | — | Path to `.env` file with K8s secret entries |
| `--r2-bucket` | No | — | R2 bucket name to create |
| `--secret-name` | No | `<project>-credentials` | K8s Secret name for provisioned secrets |
| `--preflight` | No | `false` | Run manifest preflight validation before onboarding |
| `--dry-run` | No | `false` | Preview what would be provisioned |
| `--skip-postgres` | No | `false` | Skip Postgres provisioning |
| `--skip-secrets` | No | `false` | Skip secrets provisioning |
| `--skip-r2` | No | `false` | Skip R2 provisioning |

## What It Does

The command executes a multi-step provisioning pipeline via `POST /v1/admin/onboard`:

1. Validate `enclii.yaml` from the target repo
2. Create project record in Enclii DB
3. Create service record(s) from `enclii.yaml`
4. **Validate manifest path** — checks the path exists in the repo and contains YAML files
5. Register ArgoCD desired state. Current production still uses a legacy Enclii
   repo `config.json` write; new implementation work targets runtime ArgoCD
   reconciliation from the client repo declaration. Operators can opt into the
   runtime path with `ENCLII_ARGOCD_REGISTRATION_MODE=runtime`.
6. Preserve the zero-touch boundary by rejecting new app-specific Enclii catalog
   entries outside the legacy allowlist.
7. Create K8s namespace with required labels, **default-deny NetworkPolicy**, and GHCR credentials
8. Provision domains from `enclii.yaml` (Cloudflare tunnel routes + DNS CNAMEs)
9. Register onboarding in DB, including `status.entries[]` for later status
   ConfigMap projection without editing the Enclii repo
10. Create Postgres database + role, grant privileges, update PgBouncer (if `--db-name`)
11. Create K8s Secret with entries from `.env` file (if `--secrets-file`)
12. Create R2 bucket + append R2 credentials to K8s Secret (if `--r2-bucket`)

Authentication provisioning is intentionally not hardcoded in Enclii. The product repo should provide `infra/oauth-redirect-uris.json` and `scripts/bootstrap-ecosystem.sh`, then call Janua's zero-touch `POST /api/v1/oauth/clients/register` endpoint. This keeps Janua client state product-owned and avoids Enclii repo edits.

**Status reporting**: The response includes a `step_results` array and an overall status:
- `completed` — all steps succeeded
- `partial` — non-critical steps failed (e.g., domain provisioning, R2)
- `failed` — a critical step failed (namespace creation or legacy ArgoCD registration)

If `--preflight` is set, manifest validation runs first via `POST /v1/admin/onboard/preflight`. Violations (Kyverno policy failures, YAML parse errors) are printed and the command exits without onboarding.

## Examples

### Basic onboarding (no database or secrets)

```bash
enclii onboard --repo madfam-org/madfam-site --project madfam-site
```

### Full provisioning

```bash
enclii onboard --repo madfam-org/karafiel \
  --project karafiel \
  --manifest-path infra/k8s/production \
  --db-name karafiel \
  --db-password "$(openssl rand -base64 32)" \
  --db-extensions "pgcrypto,uuid-ossp" \
  --secrets-file ./karafiel.env \
  --r2-bucket karafiel-uploads
```

### Custom secret name

```bash
enclii onboard --repo madfam-org/karafiel \
  --project karafiel \
  --secret-name karafiel-secrets \
  --secrets-file ./karafiel.env
```

### Preflight validation before onboarding

```bash
enclii onboard --repo madfam-org/forgesight \
  --project forgesight \
  --preflight \
  --db-name forgesight \
  --secrets-file ./forgesight.env
```

### Auth-enabled app onboarding

```bash
# Product-owned Janua desired state
cat infra/oauth-redirect-uris.json

# Register or converge the Janua client
scripts/bootstrap-ecosystem.sh

# Provision runtime through Enclii
enclii onboard --repo madfam-org/forgesight \
  --project forgesight \
  --preflight \
  --db-name forgesight \
  --secrets-file ./forgesight.env \
  --r2-bucket forgesight
```

### Dry run

```bash
enclii onboard --repo madfam-org/forgesight \
  --db-name forgesight \
  --secrets-file ./forgesight.env \
  --r2-bucket forgesight \
  --dry-run
```

### Secrets file format

Standard `.env` format — comments and blank lines are ignored:

```env
# Karafiel production secrets
JANUA_CLIENT_ID=jnc_abc123
JANUA_CLIENT_SECRET=jns_xyz789
DATABASE_URL=postgresql://karafiel:pass@pgbouncer.data.svc.cluster.local:6432/karafiel
REDIS_URL=redis://redis.data.svc.cluster.local:6379/4
DJANGO_SECRET_KEY=random-secret-key
SENTRY_DSN=https://abc@sentry.io/123
```

The secret is created as `<project>-credentials` in the project's namespace (or the name specified by `--secret-name`).

## Standalone Provisioning

For already-onboarded projects, use the standalone endpoints:

```bash
# Provision just a database
curl -X POST "https://api.enclii.dev/v1/admin/provision/postgres" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"namespace": "karafiel", "spec": {"database_name": "karafiel", "role_password": "..."}}'

# Provision just secrets
curl -X POST "https://api.enclii.dev/v1/admin/provision/secrets" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"namespace": "karafiel", "secrets": [{"key": "FOO", "value": "bar"}]}'

# Provision just an R2 bucket
curl -X POST "https://api.enclii.dev/v1/admin/provision/r2" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"namespace": "karafiel", "bucket_name": "karafiel-uploads"}'
```

## Requirements

- Admin role (all provisioning endpoints are behind `RequireAdmin` middleware)
- `ENCLII_POSTGRES_ADMIN_URL` must be set on switchyard-api for database provisioning
- Cloudflare API token + account ID must be set for R2 provisioning
- K8s in-cluster client must be available for secrets + PgBouncer provisioning

## Security

- Database/role names validated against `^[a-z][a-z0-9_]{0,62}$` — no SQL injection possible
- Secret values rejected if they contain placeholder strings (`your_key_here`, `TODO`, etc.)
- Passwords prompted interactively when `--db-password` is omitted (never in shell history)
- All provisioning actions logged with project name, actor, and timestamp
