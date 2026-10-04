# `enclii onboard`

> **Boundary checkpoint (2026-10-04, platform ops):** public-safe. This page adds
> the generated-credential flags (`--generate-db-password`, `--app-role`,
> `--generate-secret` and their `--rotate-*` counterparts). No secret value,
> hostname of a private node, account identifier or real connection string
> appears here: the pooler host shown is the in-cluster service name, and every
> generated value stays in the project Secret. Operational detail stays in
> `madfam-org/internal-devops`. Policy:
> [`PUBLIC_REPO_BOUNDARY.md`](../../PUBLIC_REPO_BOUNDARY.md).

Onboard a new repository with full provisioning — ArgoCD registration, namespace setup, database creation, K8s secrets, and R2 storage in a single command.

For apps that require authentication, run Janua OAuth bootstrap from the product repo as part of the same onboarding change. Enclii owns runtime provisioning; Janua owns identity provisioning; the product repo owns both desired-state manifests.

## Usage

```bash
enclii onboard --repo <org/repo> [flags]
enclii onboard ensure --repo <org/repo> [flags]
```

## Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `--repo` | Yes | — | GitHub repo in `org/name` format |
| `--project` | No | repo name | Project name |
| `--manifest-path` | No | `k8s/production` | K8s manifest path in repo |
| `--branch` | No | `main` | Branch to track |
| `--db-name` | No | — | Postgres database name to create |
| `--db-password` | No | prompted | Postgres role password (no prompt when `--generate-db-password` is set) |
| `--db-extensions` | No | — | Comma-separated Postgres extensions |
| `--generate-db-password` | No | `false` | Generate the owner password server-side; write its URL to the project Secret. Mutually exclusive with `--db-password` |
| `--rotate-db-password` | No | `false` | Generate a new owner password and replace the stored URL (implies `--generate-db-password`) |
| `--db-url-key` | No | `DATABASE_URL` | Secret key for the generated owner URL |
| `--db-connection-limit` | No | unchanged | `CONNECTION LIMIT` for the owner role, 1–20 |
| `--app-role` | No | — | Runtime role to create, named `<db>_<suffix>` (e.g. `pravara_app`); generated password |
| `--app-role-connection-limit` | No | `5` | `CONNECTION LIMIT` for `--app-role`, 1–20 |
| `--app-role-url-key` | No | `APP_DATABASE_URL` | Secret key for the app role's pooled URL |
| `--app-role-password-key` | No | — | Also store the app role's bare password under this key (services with split settings) |
| `--rotate-app-role-password` | No | `false` | Generate a new password for an existing `--app-role` |
| `--generate-secret` | No | — | `KEY` or `KEY:bytes` (16–128, default 32): generate a project-Secret value. Repeatable |
| `--rotate-secret` | No | — | `KEY`: replace an existing generated value. Repeatable |
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

### Full provisioning with generated credentials

Nobody generates, types or holds a password or app secret:

```bash
enclii onboard --repo madfam-org/karafiel \
  --project karafiel \
  --manifest-path infra/k8s/production \
  --db-name karafiel --generate-db-password \
  --db-extensions "pgcrypto,uuid-ossp" \
  --app-role karafiel_app --app-role-connection-limit 8 \
  --generate-secret DJANGO_SECRET_KEY:48 \
  --r2-bucket karafiel-uploads
```

## Generated credentials

| Flag | What Switchyard does | Secret key (default) |
|------|----------------------|----------------------|
| `--generate-db-password` | Generates the owner password, sets it in Postgres as a SCRAM-SHA-256 verifier (the DDL never carries the plaintext), adds the owner to the PgBouncer userlist | `DATABASE_URL` = `postgresql://<owner>:<generated>@pgbouncer.data.svc.cluster.local:6432/<db>` |
| `--app-role <db>_app` | Creates `LOGIN NOSUPERUSER NOBYPASSRLS NOINHERIT NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT n` with a generated password, `GRANT CONNECT ON DATABASE`, adds it to the userlist | `APP_DATABASE_URL` (and `--app-role-password-key`, if set) |
| `--generate-secret KEY[:bytes]` | Draws `bytes` from `crypto/rand`, base64url without padding | `KEY` |

Rules:

- **Never printed.** Values are written to the project Secret and nowhere else:
  not to the response, the CLI output, the switchyard-api logs or the dry run.
  The CLI prints kind, name, action (`created`, `kept`, `rotated`) and key names.
- **Re-runs keep.** `enclii onboard ensure` with the same flags keeps an
  existing role and its Secret key, and an existing generated key. Only a
  `--rotate-*` flag replaces a value.
- **No implicit rotation.** A role that exists while its Secret key does not
  is a failed step, not a silent new password; re-run with the rotate flag.
- **Runtime roles only.** `--app-role` must start with `<db>_`. An existing
  role that is a superuser, has `BYPASSRLS` or owns the database is refused
  and left unchanged. Table grants stay with the application's migrations.
- **Connection budget.** The shared Postgres has 100 connections, so every
  role carries a `CONNECTION LIMIT` of 1–20. Size it to the pools that use it.
- **Validated first.** Bad flags (both password modes, a foreign role name, an
  out-of-range size or limit, two writers for one key, a secrets-file key that
  shadows a generated one) fail before any network call, and the server
  checks them again before any side effect.

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
REDIS_URL=redis://redis.data.svc.cluster.local:6379/4
SENTRY_DSN=https://abc@sentry.io/123
```

Keep generated values out of the file: database URLs come from
`--generate-db-password` / `--app-role`, random keys from `--generate-secret`.
A file key that collides with a generated one is rejected.

The secret is created as `<project>-credentials` in the project's namespace (or the name specified by `--secret-name`).

## `onboard ensure`

Re-runs the high-value onboarding reconciliation for an existing project, to repair partial runtime state without raw `kubectl`: namespace ensure, GHCR credential copy into the project namespace, ArgoCD application registration refresh, and a domain provisioning kick from `enclii.yaml`. It also converges generated credentials with the same flags as `onboard` (it never prompts, and provisions the owner role only with `--generate-db-password`). A `partial` or `failed` result exits non-zero.

```bash
enclii onboard ensure --repo madfam-org/my-app \
  --project my-app \
  --manifest-path k8s/overlays/production \
  --namespace my-app

# Add a runtime role to an existing project
enclii onboard ensure --repo madfam-org/pravara-mes --project pravara-mes \
  --secret-name pravara-secrets --db-name pravara \
  --app-role pravara_app --app-role-connection-limit 10
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--repo` | string | | GitHub repo in `org/name` format (required) |
| `--project` | string | repo name | Project name |
| `--manifest-path` | string | `k8s/overlays/production` | K8s manifest path in the repo |
| `--branch` | string | `main` | Branch to track |
| `--namespace` | string | project name | Kubernetes namespace |
| `--secret-name` | string | `<project>-credentials` | Secret that generated values are written to |
| `--db-name` | string | | Database: provisioned with `--generate-db-password`; the role's database with `--app-role` |
| `--db-extensions` | string | | Extensions (with `--generate-db-password`) |
| generation flags | | | `--generate-db-password`, `--rotate-db-password`, `--db-url-key`, `--db-connection-limit`, `--app-role*`, `--rotate-app-role-password`, `--generate-secret`, `--rotate-secret` — as above |

## Standalone Provisioning

For already-onboarded projects, use the standalone endpoints. Secrets also have a CLI path: [`enclii admin provision secrets`](./admin.md#admin-provision-secrets).

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
- Passwords prompted interactively when `--db-password` is omitted (never in shell history); `--generate-db-password` removes the prompt and the typed value altogether
- Generated values are written only to the project Secret and the PgBouncer userlist; Postgres receives a SCRAM verifier, and responses and logs carry names only
- The standalone `POST /v1/admin/provision/postgres` keeps `role_password` required and refuses `generate_password`: generation needs the project Secret that onboarding owns
- All provisioning actions logged with project name, actor, and timestamp
