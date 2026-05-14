# Domain truth remediation runbook

Last updated: 2026-05-13

## Current state

`https://app.enclii.dev/domains` is live and backed by real Switchyard API endpoints, but it is not yet a complete source of truth for every routed hostname.

Observed production evidence on 2026-05-13:

- `GET /v1/domains` returned 8 domain rows.
- `GET /v1/domains/stats` returned 8 verified domains and 0 TLS-enabled domains.
- Coverage metadata reported 3 of 25 projects represented.
- The oldest unverified row was absent (`oldest_unverified_age_seconds=-1`).
- After restoring ConfigMap inventory RBAC, Kubernetes/Cloudflare tunnel config exposed 354 routed hostnames.
- `GET /v1/domains/reconcile` matched all 8 DB domains, found 0 DB-only domains, and found 346 route-only hostnames.
- Inventory warnings were eliminated. Route-only hostnames are now classified as `status_page_catalog` when they come only from `enclii/status-config-madfam`, because that ConfigMap is an observed status-page catalog, not proof of a live route.
- The exclusion is persisted in `domain_inventory_exclusions` by migration `026_domain_inventory_exclusions`; the handler falls back to the same built-in compatibility rule if a live database has not applied that migration yet.
- Route-only drift is now split into raw, actionable, and excluded counts. `summary.drift_detected` is based on DB-only plus actionable route-only hostnames, not excluded catalog entries.
- Tulana-specific evidence: `tulana.madfam.io` and `api.tulana.madfam.io` appeared only in `enclii/status-config-madfam`, had no registered Enclii DB rows, and public DNS returned no answer. Treat them as catalog intent until DNS, Enclii domain rows, tunnel routes, and K8s deployment exist.
- Public HTTPS checks for the 8 API rows completed valid TLS handshakes, even though the persisted TLS-enabled stat still reported `0`.

## What changed in the first remediation wave

The API now attaches public evidence to each `/v1/domains` row:

- public DNS resolution status
- public TLS handshake status
- public HTTP response status
- probe timestamp
- bounded error detail

The UI treats valid public DNS + valid TLS + any HTTP response as external proof. This corrects the most visible false negative: domains that work publicly no longer have to appear unhealthy solely because the persisted Cloudflare verifier is stale.

Important distinction: public evidence does not mutate `custom_domains`. It exposes drift between live public reality and persisted verifier state.

The API also exposes an admin-only dry-run reconciliation endpoint:

```text
GET /v1/domains/reconcile
```

This endpoint compares registered `custom_domains` against route hostnames observed from the configured Cloudflare Tunnel route manager plus Kubernetes ingress/configmap inventory. It returns:

- `matched`: domains present in both Enclii DB and route inventory.
- `db_only`: domains registered in Enclii but not observed in route inventory.
- `route_only`: raw route/catalog hostnames not registered in Enclii.
- `actionable_route_only`: route-only hostnames that still require DB backfill, route cleanup, or owner classification.
- `excluded_route_only`: explicitly classified non-route catalog/system hostnames that are retained for auditability but do not count as drift.
- `summary.drift_detected`: true when either `db_only` or `actionable_route_only` is non-empty.

The endpoint is read-only and does not backfill or delete anything.

The `/domains` UI fetches this endpoint opportunistically. When drift exists,
operators see a warning banner with matched, DB-only, and route-only counts.
If the caller is not allowed to access the admin-only endpoint, the table still
loads and simply omits reconciliation metadata.

The second remediation wave closes infrastructure blockers that prevented the
truth surface from converging:

- Switchyard API RBAC includes read-only cluster ConfigMap access for hostname inventory used by reconciliation.
- The durable `kaniko-builds-runasroot` PolicyException matches both Kaniko Jobs and Pods so Kyverno autogen rules do not block Roundhouse build Job creation.
- The emergency deploy workflow carries image digests from the successful build matrix through artifacts and uses GHCR manifest lookup only as a retrying fallback.

## Truth model

A domain is fully truthful only when these evidence sources reconcile:

- Enclii DB: `custom_domains`, project, service, environment ownership.
- Cloudflare DNS: record type, content, proxied state, zone.
- Cloudflare Tunnel: hostname route and target service.
- Kubernetes: ingress, HTTPRoute, tunnel config, service target.
- Public internet: DNS resolution, TLS handshake, HTTP response.

## Status semantics

- `active`: Enclii verifier says active, or public evidence proves DNS + TLS + HTTP reachability.
- `provisioning`: registered but not yet externally proven.
- `failed`: backend verifier reported error, or future reconciler evidence proves failure.
- `orphaned`: domain row lacks a resolvable service/environment owner.
- `unknown`: insufficient evidence.
- `stale`: UI overlay when verifier freshness is older than the configured threshold.

HTTP `404` is not automatically a domain failure. API roots commonly return `404`; for domain truth, the important evidence is that DNS resolves, TLS validates, and the hostname reaches an HTTP server.

## Remaining remediation work

Priority order:

1. Add a persisted reconciliation table for domain evidence instead of probing only at request time.
2. Add Cloudflare DNS zone inventory and Gateway API HTTPRoute collectors to `/v1/domains/reconcile`.
3. Add UI/admin workflows for maintaining `domain_inventory_exclusions` instead of relying on migration-seeded records only.
4. Backfill all live routed hostnames into either `custom_domains` or the persisted exclusion registry.
5. Fix Cloudflare SSL lookup to resolve the correct zone per domain, including apex domains, wildcard certificates, and proxied A/AAAA records.
6. Persist verifier timestamps independently: DNS checked, TLS checked, route checked, HTTP checked.
7. Update `/v1/domains/stats` to report inventory coverage, route drift, DNS drift, TLS validity, HTTP reachability, stale evidence, and orphan count.
8. Add production smoke gates that fail when route inventory and `/v1/domains` drift beyond the allowed threshold.

## Acceptance criteria for "fully truthful"

- Every live routed hostname is listed or explicitly excluded with a reason.
- Every row has an owner or an orphan/unmanaged classification.
- Every row has fresh DNS, route, TLS, and HTTP evidence.
- Public HTTPS false negatives are zero for known-good hostnames.
- Project coverage is 25 of 25, or every missing project has a documented no-domain/excluded reason.
- Stale evidence older than 24 hours is zero outside planned incidents.
- The UI never presents API fetch freshness as verifier freshness.

## Deployment pipeline prerequisites for domain-truth releases

The domains page depends on the Switchyard API/UI release pipeline. Before retrying a production release, verify these build-system invariants:

- `switchyard-api` must enqueue builds through Roundhouse, not fall back to in-process Docker builds. The durable NetworkPolicy is `switchyard-api-roundhouse-egress`.
- `switchyard-api` must set `ENCLII_ROUNDHOUSE_API_KEY` from the same `enclii-secrets/internal-api-key` used by Roundhouse `SWITCHYARD_API_KEY`; otherwise Roundhouse internal enqueue can return `401` and Switchyard degrades into in-process build fallback.
- Roundhouse build Jobs must not set `securityContext.capabilities.drop: ["ALL"]`; Kaniko needs default in-container capabilities such as `CAP_CHOWN` to unpack OCI layers. Enforce this with the durable `kaniko-builds-runasroot` PolicyException for `require-run-as-nonroot` and `restrict-capabilities`, while keeping the container host-unprivileged.
- Roundhouse callbacks must use the Switchyard Kubernetes Service URL `http://switchyard-api`, not `http://switchyard-api:4200`; the Service exposes port `80` and forwards to container port `4200`.
- Keep the callback path durable with `roundhouse-switchyard-callback-egress` and `switchyard-api-roundhouse-callback-ingress` so service reconciliation cannot remove it.
- Kaniko build Jobs and Pods intentionally require the durable `kaniko-builds-runasroot` PolicyException for `require-run-as-nonroot` and `restrict-capabilities`; they must still set `privileged: false`, `allowPrivilegeEscalation: false`, and `seccompProfile: RuntimeDefault`.
- If bootstrapping from an older Roundhouse worker image, use only a short-lived PolicyException scoped to `enclii-builds` `build-*` Jobs/Pods for the missing fields, deploy the corrected Roundhouse image, then remove the temporary exception.
- `enclii-builds` must contain `ghcr-credentials` for Kaniko registry auth and `git-credentials` when private Git fetches are required. Do not hand-copy long term; reconcile these from the platform secret source of truth before treating the build pipeline as fully stable.
- Monorepo Dockerfiles built by Roundhouse must copy local `replace ../../packages/...` modules into the paths expected by `go.mod`; otherwise Kaniko can clone the repo and still fail at `go mod download`.
- Roundhouse `REGISTRY` must match the production image namespace `ghcr.io/madfam-org/enclii`; otherwise successful builds attempt to push unauthorized package names like `ghcr.io/madfam-org/switchyard-api`.
- Switchyard UI builds require the `enclii-builds/npm-madfam-token` secret and Roundhouse must expose it as `NPM_MADFAM_TOKEN` with a token-less `--build-arg=NPM_MADFAM_TOKEN`; do not store npm tokens in service `build_config`.
