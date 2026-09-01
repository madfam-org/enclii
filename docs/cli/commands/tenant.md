# `enclii tenant`

Provision a whole client from one manifest — the Janua organization that is its identity root, its entitlements and OAuth clients, the Enclii project, namespace, apps, managed Postgres, secrets, buckets and domains, the Nauta workspace, and the Kalya tenant.

> **Design preview.** `tenant apply` validates a manifest and prints the ordered plan. It does **not** execute anything. Three sibling-platform seams must land first; see [RFC: CLIENT-IN-A-DAY](../../rfcs/2026-09-01-client-in-a-day.md).

## Usage

```bash
enclii tenant apply -f <manifest>
enclii tenant validate -f <manifest>
```

## Flags

| Flag | Required | Default | Description |
|------|----------|---------|-------------|
| `-f`, `--file` | Yes | — | Path to the tenant manifest |

`apply` also carries a hidden `--execute` flag. It returns a not-implemented error citing the RFC and calls nothing. It exists so the unimplemented path fails loudly and by name rather than being a silently absent capability.

## The manifest

`apiVersion: enclii.dev/v1alpha`, `kind: Tenant`. One document, one client, all environments.

```yaml
apiVersion: enclii.dev/v1alpha
kind: Tenant
metadata:
  name: crea                      # THE joining key across every platform
  displayName: Crea Tu Mundo Autismo
spec:
  janua:
    org:
      ownerEmail: owner@example.org
    tiers:
      enclii: pro                 # essentials | pro | madfam
    oauthClients:
      - logicalKey: crea-map
        redirectURIs: ["https://crea-map.example.mx/api/auth/callback"]
  apps:
    - name: crea-map
      repo: madfam-org/crea-map
      manifest: enclii.yaml       # the app's own manifest stays authoritative
      environments:
        - name: production
          domains:
            - host: crea-map.example.mx
              tls: true
          envFrom:
            - secret: crea-map-secrets
  db:
    name: crea_map
    clones:
      - name: crea_map_staging
        from: crea_map
  secrets:
    - name: crea-map-secrets
      keys: [DATABASE_URL, JANUA_CLIENT_SECRET]   # KEY NAMES ONLY
  buckets:
    - name: crea-map-uploads
  nauta:
    workspace:
      tier: FRACTIONAL_CTO        # SELF_SERVE | PROJECT | FRACTIONAL_CTO
      hostnames:
        - host: crea.example.mx
          primary: true
  kalya:
    tenantFile: ../kalya/prisma/provision/ctm-tenant.json
```

`metadata.name` is capped at 63 characters — nauta's `workspaces.slug` is `VarChar(63)`, the tightest constraint of the four platforms that key on it.

## What validation catches

Every rule below exists because its absence has already cost something:

- **Nested domain labels.** Cloudflare Universal SSL covers an apex and one label below it. `map.crea.example.mx` resolves, serves a TLS error, and reads as an outage. The error names the flat-label host to use instead.
- **Duplicate hosts** across apps or environments — one host resolves to exactly one backend, and a second capture rewrites the first's tunnel route.
- **Secret values in the key list.** `keys: [FOO=bar]` is rejected; the diagnostic does not echo the value back.
- **`envFrom` referencing an undeclared secret**, and **`db.clones[].from`** referencing a database this manifest does not declare (cloning across owners means a new DB role, which means a pgbouncer userlist edit).
- **Nauta hostnames without exactly one primary** — nauta expects this but does not enforce it in the database.
- **Missing `ownerEmail`** — an org created without a named owner is owned by the operator who ran the command, and janua has no ownership-transfer endpoint.

Every problem in a manifest is reported in one run, not one per run.

## Provisioning order

The order is dependency-driven. Two constraints are load-bearing:

- **The Janua org is step 1.** It produces the UUID every later step keys on. Kalya's `Tenant.id` *is* that UUID and is immutable after creation.
- **Domains come after services.** Capturing a domain for a `metadata.name` that resolves to no live workload is what rewrote eight identity hostnames to a nonexistent backend on 2026-08-27 (#468). On a fresh tenant, route-first ordering guarantees that failure.

## Idempotency and failure

Every step is check-then-act: read by natural key, classify as `created | unchanged | converged | drift | failed | skipped`, then act. Applying an unchanged manifest is a no-op.

**Nothing is ever deleted**, including partial work. There is no `tenant destroy` and no rollback. Half a tenant is recoverable by re-running; a deleted organization whose UUID is already embedded in a Kalya `Tenant.id` is not. On failure: read the failed steps, fix the cause, re-run the same manifest.

## Example

```bash
$ enclii tenant apply -f tenants/crea.yaml
Tenant: crea (Crea Tu Mundo Autismo)
Project: crea   Namespace: crea

=== ORDERED PLAN — DRY RUN, nothing is executed ===

  #   STEP                              OWNER   DETAIL
  1   janua org                         janua   slug=crea owner=owner@example.org
  2   janua entitlements                janua   enclii=pro kalya=essentials nauta=pro
  ...
  15  kalya tenant                      kalya   ... (Tenant.id = the janua org UUID from step 1, immutable)

  4 step(s) are BLOCKED on a sibling-platform seam:
    [1] janua org — GAP-1: org-create is gated on a platform-admin user JWT, not X-Internal-API-Key
  Until those land, run these steps by hand in the order above.

NOT EXECUTED: 15 step(s) planned, 0 performed. Execution is unimplemented.
```

## Related

- [RFC: CLIENT-IN-A-DAY](../../rfcs/2026-09-01-client-in-a-day.md) — schema, orchestration order, idempotency contract, and the sibling-platform gaps
- [`enclii onboard`](./onboard.md) — the single-app pipeline this generalizes; unchanged and still the supported path for onboarding one repo
- [`enclii admin provision secrets`](./admin.md) — how secret values actually reach a namespace
