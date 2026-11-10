# Plausible CE: wiring and operations runbook

**Status:** spec only. The manifests are in
[`infra/k8s/production/plausible/`](README.md);
nothing deploys them yet.
**Applies to:** Plausible Community Edition v3.2.1 at `https://plausible.madfam.io`.

> Boundary checkpoint: public-safe runbook. No values, node identities or
> private record links. Intake target ids, Vault paths and key NAMES are
> routing contracts, not secrets.

Steps run in this order. Each step names who runs it. Steps marked
**adapter gap** use raw cluster access that Enclii has no verb for yet; they
are listed under "Adapter gaps" at the end, and the wiring PR records them in
`docs/ADAPTER_GAPS.md`. Don't normalise the raw command.

This runbook sits next to the manifests, not under `docs/`, on purpose: a
`docs/` change rebuilds and redeploys docs-site, and this spec PR deploys
nothing.

## 1. Intake the keys (operator, then server-side generation)

1. Add `secret/data/plausible` and `secret/data/plausible/*` to
   `scripts/provision-switchyard-vault-writer.sh`. The owner re-runs it with the
   Vault admin token, the only credential the owner handles.
2. Add these targets to
   `apps/switchyard-api/internal/secretsintake/registry.yaml`, in a PR that
   rebuilds switchyard-api:

   | Target id | Vault path | Key | Generate | Read by |
   | --- | --- | --- | --- | --- |
   | `plausible/secret-key-base` | `secret/plausible` | `secret_key_base` | `bytes: 64` (86 chars; Plausible needs ≥ 32 bytes) | ExternalSecret `plausible-app-secrets` |
   | `plausible/clickhouse` | `secret/plausible` | `clickhouse_password` | default (32 bytes) | `plausible-app-secrets` |
   | `plausible/postgres` | `secret/plausible` | `db_password` | default (32 bytes) | `plausible-db-credentials` |

   The backup key already exists: `enclii/addon-db-backups` writes
   `secret/enclii:addon_backup_*`, which `plausible-backup-credentials` reads.
3. Mint each one once, server-side, so no person sees a value:
   `enclii secrets intake submit plausible/<target> --generate <key> --reason "…"`.
   A re-run rotates the key. Rotating `db_password` after bootstrap also needs
   the role password changed in Postgres.

Generated values are base64url, which is safe inside the connection URLs the
pods build.

## 2. DNS (owner)

Keep the public route closed until step 4 has created the first account.

- **Route:** `plausible.madfam.io` → `http://plausible.plausible.svc.cluster.local:8000`
  through the shared Cloudflare tunnel. Use the Enclii tunnel-route procedure
  in [`docs/cli/commands/providers.md`](../../../../docs/cli/commands/providers.md#cloudflare-tunnels-apply-operator-procedure):
  dry run, review every row, then apply with `--expect-plan`.
- **DNS:** a proxied record for `plausible.madfam.io` on the tunnel. This is an
  owner step.
- **Edge hygiene:** strip any inbound `X-Plausible-IP` header for this host.
  Plausible trusts that header ahead of `CF-Connecting-IP`, so a visitor could
  otherwise spoof the address that feeds the daily visitor hash and the
  country lookup. Leave `CF-Connecting-IP` as Cloudflare sets it.

## 3. Wire the manifests (operator PR)

1. Clear the image admission gap
   ([README, "Before wiring"](README.md#before-wiring)).
   The recommendation is a digest-preserving copy into `ghcr.io/madfam-org`.
2. Add `infra/argocd/projects/plausible/config.json` (content in the README).
   Update `tests/scripts/test_plausible_spec_only.py` in the same PR.
3. After merge, the ApplicationSet creates `plausible-services`. Wait for it to
   become Synced and Healthy. If it is Degraded with every pod healthy, check
   the three ExternalSecrets for `Ready=False`: a Vault key was not minted.

## 4. Smoke test (operator)

1. **Database.** `plausible-db` reports healthy. If it shows `Instance Status
   Extraction Error`, the cnpg-system → 8000 rule is missing or not rendered.
   WAL archiving is healthy (`ContinuousArchiving=True`). Run one on-demand
   backup, and keep `immediate: false` on the schedule.
2. **ClickHouse.** `plausible-events-db` passes `/ping`. The `plausible` user
   logs in with its password, and with no password it is refused (password
   set, `from_env` resolved).
3. **Plausible.** The `createdb` and `migrate` init containers exit 0, and
   `GET /api/system/health/ready` returns 200 from inside the namespace.
4. **First account (adapter gap).** Before any route exists, reach the Service
   through a short-lived port-forward and register the owner's account.
   Plausible opens registration on its first launch and is invite-only after
   that. Turn on two-factor authentication.
5. **Public route.** Open it (step 2), then check:
   - `GET https://plausible.madfam.io/api/system/health/ready` → 200;
   - `GET /js/script.js` → 200 `application/javascript`;
   - `GET /storybook` → 404 (CVE-2026-8467 regression check).
6. **One event.** Add a smoke site in the dashboard. `POST /api/event` with
   that domain returns 202 with no `x-plausible-dropped` header. The visit
   appears in the realtime view.
7. **Security context.** The containers ran with a read-only root filesystem.
   If Plausible fails to write under `/app`, add an emptyDir for the path it
   names, not a writable root.

**Backups and recovery:**
- **Postgres:** the CNPG Barman store under `plausible/plausible-db`. Restore
  bootstraps a new Cluster from that object store.
- **ClickHouse:** a Longhorn volume backup (high-churn group, daily R2).
  Restore the volume, then start the StatefulSet. MergeTree drops any part
  that is incomplete in the crash-consistent copy.
- **After rotating the backup key:** the CNPG instance manager caches it until
  the Cluster object changes, so touch a Cluster annotation (adapter gap).

## 5. Add the snippets (product repos, one PR each)

- **Script.** Each site loads the Plausible script from `plausible.madfam.io`.
  CE v3.2.1 serves both the site-specific `/js/pa-<id>.js` with a
  `plausible.init({...})` bootstrap and the legacy `/js/script.js` with
  `data-domain`.
- **CSP.** Where a site has a CSP, add `https://plausible.madfam.io` to
  `script-src` and `connect-src`. Where the policy uses `strict-dynamic`, put
  the nonce on the script and on the bootstrap.
- **Events.**
  - Journey events: `demo_start`, `pricing_view`, `cta_click`,
    `checkout_start`, with props `product`, `sku` and `target`.
  - No personal data in any property.
  - Synthetic traffic never loads the script, and internal browsers set
    `localStorage.plausible_ignore = "true"`.
- **Cross-site links.** Links between MADFAM properties carry
  `utm_source=<origin host>&utm_medium=ecosystem&utm_campaign=<surface>`.
  Every checkout link also carries attribution through to Dhanam (`attr_*` in
  the payment metadata).
- **Tenant pages.** Only MADFAM's own public pages get the snippet. Embedded
  widgets on client sites and white-label client workspaces never load it.

The per-site file plan lives in the private operations record.

## 6. Weekly report (separate build)

- **Why outside Plausible:** CE has no funnels and no revenue goals.
- **What it computes, each week:**
  - sources (including an AI-assistant segment: a source regex over
    chatgpt, claude, gemini, copilot, perplexity, deepseek, grok, mistral,
    phind, you.com, meta.ai);
  - entry pages per property;
  - journey events;
  - checkout starts;
  - paid, from Dhanam's stored `attr_*` attribution.
- **Data access:** read Plausible through a read-only path minted server-side,
  never a key a person creates. The options are an owner ruling: Stats API v2
  with a server-minted key, or a read-only ClickHouse user.
- **Delivery:** the owner's channel.

## Privacy and compliance

- **Cookieless.** Plausible sets no cookies and stores nothing on the device,
  so it needs no consent banner.
- **IP handling.** The visitor IP is never stored. Plausible hashes it with
  the site domain, the user agent and a salt that it deletes every 24 hours,
  so visits cannot be linked across days.
- **Geolocation.** Country level only, from the bundled DB-IP Lite database.
  There is no MaxMind key and no city lookup.
- **Data minimisation.**
  - Custom properties never carry personal data.
  - No cross-site visitor ID is sent to Plausible.
  - Attribution tied to an account stays in Dhanam (`attribution_touches`),
    outside Plausible.
- **Retention.** CE has no retention setting and keeps events indefinitely.
  The proposal is 25 months, enough for year-over-year comparisons, enforced
  by a reviewed ClickHouse TTL once Plausible's migrations have created the
  tables. That is an owner ruling. ClickHouse's own `query_log` keeps 30 days.
- **Mexico (LFPDPPP 2025).**
  - The output is aggregated and dissociated (disociado). It is very likely
    not personal data, but the aviso de privacidad must still disclose the
    tracking technology.
  - Proposed wording for counsel: «Usamos analítica web propia y sin cookies
    (Plausible Community Edition, alojada por MADFAM). No guarda tu dirección
    IP ni identificadores persistentes; sólo cifras agregadas, como páginas
    vistas, sitio de origen y país.»
  - The counsel review and the aviso updates on each site are owner steps.

## Adapter gaps (record in `docs/ADAPTER_GAPS.md` when wiring)

| Gap | Planned workaround | Target adapter |
| --- | --- | --- |
| First dashboard account | A short-lived break-glass port-forward to the `plausible` Service, once per instance, before the public route exists | An audited one-shot reach of a namespace Service for a named path, or a server-side account seed |
| Barman credential reload on a git-declared CNPG cluster | A break-glass annotation on the Cluster after the backup key rotates (the addon sweep stamps addon clusters only) | Extend the sweep's credential-version stamp to labelled git-declared clusters, or the Barman Cloud Plugin |
