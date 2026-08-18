# enclii buckets

Manage object storage buckets (Cloudflare R2) for an existing project.

## Synopsis

```bash
enclii buckets <subcommand> [flags]
```

**Aliases:** `bucket`, `r2`, `object-storage`

> Not named `storage`: `enclii volumes` already claims `storage` as an alias
> for cluster block storage (PVC/PV), and `enclii ops storage` means the same
> thing. Object storage gets its own unambiguous name.

## Description

The `buckets` command is the day-2 lifecycle for object storage. Each bucket is
provisioned with its **own** Cloudflare API token, scoped to that single bucket
with Object Read & Write — services never share credentials, and no service is
handed an account-wide key.

Credentials are written to the project's Kubernetes Secret (and mirrored to
Vault when Vault is configured) as five keys:

| Key | Purpose |
|-----|---------|
| `R2_BUCKET_NAME` | Bucket name |
| `R2_ENDPOINT_URL` | Account S3-compatible endpoint |
| `R2_ACCESS_KEY_ID` | Cloudflare API token ID |
| `R2_SECRET_ACCESS_KEY` | SHA-256 of the token value |
| `STORAGE_BACKEND` | `r2` |

The raw credential is never printed by the CLI or returned by the API — only a
reference to the Secret it was written to, mirroring `enclii addon create`.

Unlike `enclii onboard --r2-bucket`, these commands touch **nothing** but the
bucket and the project's Secret: no ArgoCD registration, no namespace creation,
no domain provisioning. They are safe to run against a live service.

## Subcommands

### `create`

Create or adopt a bucket and mint scoped credentials for it.

```bash
enclii buckets create <bucket> --project <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | string | active project | Project slug |
| `--namespace` | string | project slug | Kubernetes namespace |
| `--secret-name` | string | `<project>-credentials` | Kubernetes Secret name |
| `--rotate` | bool | `false` | Mint a fresh token even if credentials already exist |
| `--json` | bool | `false` | JSON output |

Behaviour:

- **Idempotent.** If the project already holds a complete credential set for
  this bucket, it is adopted and left untouched — no new token is minted, so
  re-running does not break a running service. Use `--rotate` to force a fresh
  token.
- **Bucket already exists in Cloudflare?** Adopted, not an error.
- **Bucket owned by another project?** Refused with `409`, never rebound.
- **Existing binding pointing at a different bucket?** Refused with `409`;
  destroy the binding first if the move is intended.

After a `provisioned` or `rotated` result, redeploy the service so it picks up
the new keys.

### `ls`

List the object storage bindings a project actually holds, read from the
cluster rather than from an intent record — so an incomplete or hand-patched
binding shows up as what it is.

```bash
enclii buckets ls [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | string | active project | Project slug |
| `--namespace` | string | project slug | Kubernetes namespace |
| `--json` | bool | `false` | JSON output |

Drift findings for the namespace are printed to stderr. **The command exits
non-zero if any finding is critical**, so it works as a CI gate.

### `destroy`

Revoke the bucket's Cloudflare token and remove the R2 keys from the project's
Secret.

```bash
enclii buckets destroy <bucket> --project <slug> --yes [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | string | active project | Project slug |
| `--namespace` | string | project slug | Kubernetes namespace |
| `--secret-name` | string | `<project>-credentials` | Kubernetes Secret name |
| `--delete-bucket` | bool | `false` | Also delete the bucket **and its objects** (irreversible) |
| `--yes` | bool | `false` | Skip confirmation prompt |

The bucket and its objects are **kept** unless `--delete-bucket` is passed.
Unbinding is reversible (re-run `buckets create`); deleting stored objects is
not. Revocation takes effect immediately — a service still using the token
starts failing until it is re-provisioned.

## Auditing credential isolation

```bash
enclii ops storage r2-audit                       # every namespace
enclii ops storage r2-audit -n karafiel           # one namespace
enclii admin ga-verify                            # includes the audit as a gate
```

The audit reports, per Secret:

| Finding | Severity | Meaning |
|---------|----------|---------|
| `missing_credentials` | critical | `STORAGE_BACKEND=r2` with no access key pair — configured for R2 with no way to authenticate |
| `bucket_mismatch` | critical | `R2_BUCKET_NAME` differs from the bucket enclii provisioned for this service |
| `shared_credentials` | critical | The same access key is installed in more than one namespace |
| `bucket_shared_across_namespaces` | critical | One bucket referenced from more than one namespace |
| `unmanaged_credentials` | warning | R2 keys with no `enclii.dev/r2-bucket` provenance annotation |
| `orphan_credentials` | warning | A secret key with no bucket to use it on |

Access key IDs are reduced to a truncated SHA-256 fingerprint in all output, so
credential sharing is detectable without any credential being echoed.

## Required Cloudflare permissions

switchyard-api's `CLOUDFLARE_API_TOKEN` must hold, on the enclii account:

- **API Tokens Write** — to create account-owned tokens
- **Workers R2 Storage Bucket Item Write** — Cloudflare refuses to let a token
  grant a permission it does not itself hold

Without these, provisioning fails with an explicit error naming the missing
permission rather than emitting `STORAGE_BACKEND=r2` with no keys.

## API

### Bucket lifecycle

| Method | Path |
|--------|------|
| `POST` | `/v1/projects/:slug/storage/buckets` |
| `GET` | `/v1/projects/:slug/storage/buckets` |
| `DELETE` | `/v1/projects/:slug/storage/buckets/:bucket` |
| `POST` | `/v1/ops/storage/r2-audit` |

### Object API (Supabase-Storage-style)

The object API operates on the **contents** of a bucket a project already owns.
The primary upload and download path never streams bytes through the API pod —
the API mints a short-lived presigned URL and the client transfers directly to
Cloudflare R2.

| Method | Path | Role | Purpose |
|--------|------|------|---------|
| `GET` | `/v1/projects/:slug/storage/buckets/:bucket/objects` | project access | List objects (query: `prefix`, `limit`) |
| `POST` | `/v1/projects/:slug/storage/buckets/:bucket/objects/presign-upload` | developer | Mint a presigned **PUT** URL (body: `key`, `content_type`, `expiry_seconds`) |
| `GET` | `/v1/projects/:slug/storage/buckets/:bucket/objects/presign-download` | project access | Mint a presigned **GET** URL (query: `key`, `expiry_seconds`) |
| `POST` | `/v1/projects/:slug/storage/buckets/:bucket/objects/upload` | developer | Direct passthrough upload for tiny files (≤ 5 MiB; query: `key`) |
| `DELETE` | `/v1/projects/:slug/storage/buckets/:bucket/objects` | developer | Delete an object (`key` in query or body) |

#### Presigned upload flow

```bash
# 1. Ask the API for a presigned PUT URL (developer role)
curl -X POST \
  https://<api>/v1/projects/<slug>/storage/buckets/<bucket>/objects/presign-upload \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"key":"uploads/report.csv","content_type":"text/csv","expiry_seconds":900}'
# → {"url":"https://<bucket>.r2.cloudflarestorage.com/...","method":"PUT","key":"uploads/report.csv","expires_in":900}

# 2. Upload the bytes straight to R2 — the API pod is never in the data path
curl -X PUT "<url>" -H "Content-Type: text/csv" --data-binary @report.csv
```

Download is the mirror image: `GET …/presign-download?key=uploads/report.csv`
returns a presigned GET URL the client fetches directly.

#### Isolation model

Every object operation is confined to the calling project on three independent
axes, so no single mistake crosses a tenant boundary:

1. **Own-credential minting.** Presigned URLs are signed with the *project's own*
   bucket-scoped R2 credentials (`R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY`
   from the project's Secret — the same keys the running service uses). Those
   keys are physically incapable of signing a request for another project's
   bucket; isolation does not depend on an authorization check being correct.
2. **Bucket-ownership check.** The `:bucket` in the path must match the bucket
   recorded in the project's Secret. Naming a bucket the project does not own is
   refused with `409` before anything is minted.
3. **Key namespacing.** Every object key is stored under `projects/<slug>/`, and
   keys are rejected (not silently rewritten) if they attempt path traversal
   (`..`, `.`), are absolute, contain backslashes, or carry control characters.
   The internal prefix is stripped from every response, so callers work in a
   flat per-project keyspace.

When object storage is not configured on the platform (no secrets provisioner),
these endpoints return `503`, matching the bucket-lifecycle endpoints. A project
with no bucket bound returns `404`; an R2-side failure surfaces as `502`.

## See also

- [`addon`](./addon.md) — managed databases
- [`volumes`](./volumes.md) — cluster block storage on a service
- [`ops`](./ops.md) — operator workflows, including `ops storage`
