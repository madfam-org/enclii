# `enclii onboard`

Onboard a new repository with full provisioning — ArgoCD registration, namespace setup, database creation, K8s secrets, and R2 storage in a single command.

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
| `--dry-run` | No | `false` | Preview what would be provisioned |
| `--skip-postgres` | No | `false` | Skip Postgres provisioning |
| `--skip-secrets` | No | `false` | Skip secrets provisioning |
| `--skip-r2` | No | `false` | Skip R2 provisioning |

## What It Does

The command executes an 11-step provisioning pipeline via `POST /v1/admin/onboard`:

1. Validate `enclii.yaml` from the target repo
2. Create project record in Enclii DB
3. Create service record(s) from `enclii.yaml`
4. Generate ArgoCD `config.json`
5. Auto-commit `config.json` to `infra/argocd/projects/<name>/` in the enclii repo
6. Create K8s namespace with required labels + copy GHCR credentials
7. Provision domains from `enclii.yaml` (Cloudflare tunnel routes + DNS CNAMEs)
8. Register onboarding in DB
9. Create Postgres database + role, grant privileges, update PgBouncer (if `--db-name`)
10. Create K8s Secret with entries from `.env` file (if `--secrets-file`)
11. Create R2 bucket + append R2 credentials to K8s Secret (if `--r2-bucket`)

Steps 9-11 are optional and independent — failure in one does not block others.

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

The secret is created as `<project>-credentials` in the project's namespace.

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
