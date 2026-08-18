# Data API — auto-generated REST over your managed Postgres

Enclii can turn any managed Postgres addon into a REST API automatically, the
same way Supabase does. Under the hood this is [PostgREST](https://postgrest.org),
so any existing PostgREST / `@supabase/postgrest-js` client works unchanged.

> **Architecture & security model:** [`docs/architecture/data-api-postgrest.md`](../architecture/data-api-postgrest.md).

## TL;DR

```bash
# 1. You already have a managed Postgres addon (status: ready).
enclii addon ls

# 2. Turn on the REST API for it.
enclii addon api enable <addon_id>

# 3. Check status / get the URL.
enclii addon api info <addon_id>
#   Status:  ready
#   URL:     https://<addon>-<id>.data.enclii.dev

# 4. Mint a JWT to call it.
TOKEN=$(enclii addon api token <addon_id> --role authenticated)

# 5. Call your API.
curl https://<addon>-<id>.data.enclii.dev/your_table \
     -H "Authorization: Bearer $TOKEN"
```

## You are responsible for row-level security (RLS)

This is the most important thing on this page.

Enclii creates three Postgres roles for you (`authenticator`, `anon`,
`authenticated`) and wires the JWT signing secret. It does **not** grant those
roles access to any of your tables. **A freshly enabled data API returns nothing
until you grant access and write RLS policies** — this is deliberate ("closed by
default"), exactly like Supabase.

To expose a table safely:

```sql
-- 1. Turn on row-level security for the table.
ALTER TABLE public.todos ENABLE ROW LEVEL SECURITY;

-- 2. Let the API roles reach the table at all.
GRANT SELECT, INSERT, UPDATE, DELETE ON public.todos TO authenticated;
GRANT SELECT ON public.todos TO anon;               -- if public reads are OK

-- 3. Write a policy. This one lets an authenticated user see only their rows,
--    keyed on a `user_id` claim you put in the JWT.
CREATE POLICY "own rows" ON public.todos
  FOR SELECT TO authenticated
  USING (user_id = current_setting('request.jwt.claims', true)::json->>'user_id');
```

Without step 3, `authenticated` can reach the table (step 2) but RLS (step 1)
denies every row. Without step 1, RLS is off and the grants in step 2 expose the
whole table — only do that intentionally.

Run this SQL with any Postgres client using the addon's owner connection string
(`enclii addon` gives you the connection Secret reference; `DATABASE_URL` if the
addon is bound to a service).

## The roles

| Role | When it is used | What enclii grants it |
| ---- | --------------- | --------------------- |
| `authenticator` | The login PostgREST uses. Never used directly. | `LOGIN NOINHERIT`; may become `anon`/`authenticated`. No table access of its own. |
| `anon` | Requests with **no** (or an invalid) JWT. | `USAGE` on the exposed schema only. **No table grants** — you add them. |
| `authenticated` | Requests with a **valid** JWT whose `role` claim is `authenticated`. | Same: `USAGE` on the schema only. You add table grants + policies. |

## Minting tokens

```bash
# Default: role=authenticated, 1h TTL.
enclii addon api token <addon_id>

# Custom role, TTL, and extra claims your RLS policies key on.
enclii addon api token <addon_id> \
  --role authenticated \
  --ttl 3600 \
  --claim user_id=42 \
  --claim org=acme
```

Tokens are HS256, signed with the addon's data-API secret. The secret lives only
inside the cluster — enclii signs the token for you and returns just the token;
the secret is never exposed through the API or CLI.

For a production app you will usually mint tokens yourself from your own auth
service using the same signing secret (available to your workloads via the
addon's Kubernetes Secret), rather than calling the CLI per request. `enclii
addon api token` is for testing and bootstrapping.

## Calling the API (PostgREST syntax)

```bash
BASE=https://<addon>-<id>.data.enclii.dev
AUTH="Authorization: Bearer $TOKEN"

# Select specific columns with a filter.
curl "$BASE/todos?select=id,title&done=eq.false" -H "$AUTH"

# Insert.
curl "$BASE/todos" -H "$AUTH" -H "Content-Type: application/json" \
     -d '{"title":"ship the data API","done":false}'

# Update (with a filter) — Prefer header controls the response.
curl -X PATCH "$BASE/todos?id=eq.1" -H "$AUTH" \
     -H "Content-Type: application/json" -H "Prefer: return=representation" \
     -d '{"done":true}'
```

Full query grammar (embedding, ordering, full-text search, RPC, upsert) is the
standard PostgREST API: <https://postgrest.org/en/stable/references/api.html>.

## Exposing more than one schema

```bash
enclii addon api enable <addon_id> --schemas public,api
```

Every exposed schema needs its own `GRANT USAGE` + table grants + RLS, same as
`public`.

## Disabling

```bash
enclii addon api disable <addon_id> --yes
```

This tears down the PostgREST deployment, service, ingress, and JWT secret. Your
data and tables are untouched, and the `anon`/`authenticated` roles are left in
place so re-enabling reuses them.

## Limits (Sprint 1)

- **Postgres only.** Redis/MySQL addons have no data API (PostgREST is Postgres-only).
- **No GraphQL** — REST only.
- **One fixed replica** per addon (no scale-to-zero yet).
- **`db-max-rows` is capped at 1000** by default to prevent accidental full-table
  pulls; contact ops to raise it.
- **JWT rotation** is not yet a first-class verb; disable + re-enable rotates the
  secret.

See the [architecture doc](../architecture/data-api-postgrest.md#deferred-explicitly-out-of-sprint-1-scope)
for the full deferred list.
