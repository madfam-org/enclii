# Domain truth remediation runbook

Last updated: 2026-05-13

## Current state

`https://app.enclii.dev/domains` is live and backed by real Switchyard API endpoints, but it is not yet a complete source of truth for every routed hostname.

Observed production evidence on 2026-05-13:

- `GET /v1/domains` returned 8 domain rows.
- `GET /v1/domains/stats` returned 0 verified domains and 0 TLS-enabled domains.
- Coverage metadata reported 3 of 25 projects represented.
- The oldest unverified row was about 17.5 days old.
- Kubernetes/Cloudflare tunnel config exposed about 34 route-like hostnames.
- Public HTTPS checks for the 8 API rows completed valid TLS handshakes, even though persisted DB verifier fields still said `verified=false` and `tls_enabled=false`.

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
- `route_only`: routed hostnames not registered in Enclii.
- `summary.drift_detected`: true when either `db_only` or `route_only` is non-empty.

The endpoint is read-only and does not backfill or delete anything.

The `/domains` UI fetches this endpoint opportunistically. When drift exists,
operators see a warning banner with matched, DB-only, and route-only counts.
If the caller is not allowed to access the admin-only endpoint, the table still
loads and simply omits reconciliation metadata.

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
3. Add an explicit domain exclusion registry for platform/internal/system hostnames.
4. Backfill all live routed hostnames into either `custom_domains` or the exclusion registry.
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
- Roundhouse build Jobs must set `securityContext.capabilities.drop: ["ALL"]`; otherwise Kyverno `restrict-capabilities` rejects `build-*` Jobs in `enclii-builds`.
- Roundhouse callbacks must use the Switchyard Kubernetes Service URL `http://switchyard-api`, not `http://switchyard-api:4200`; the Service exposes port `80` and forwards to container port `4200`.
- Keep the callback path durable with `roundhouse-switchyard-callback-egress` and `switchyard-api-roundhouse-callback-ingress` so service reconciliation cannot remove it.
- If bootstrapping from an older Roundhouse worker image, use only a short-lived PolicyException scoped to `enclii-builds` `build-*` Jobs, deploy the corrected Roundhouse image, then remove the exception.
