# Tulana SKU evidence contract for Enclii

Date: 2026-05-29

Status: active contract for Tulana commercial-GA readiness

## Direct surfaces

| Surface | URL |
| --- | --- |
| Marketing/docs root | `https://enclii.dev` |
| App | `https://app.enclii.dev` |
| API | `https://api.enclii.dev` |
| Docs | `https://docs.enclii.dev` |
| Status | `https://status.enclii.dev` |
| Projects dashboard | `https://app.enclii.dev/projects` |

## Enclii's role in Tulana

Enclii is both:

1. a commercial MADFAM platform SKU family that Tulana should evaluate for GA
   readiness; and
2. the operations substrate that provides production evidence for sibling
   services, including Tulana, madfam-crawler, Selva, Phynd CRM, Dhanam, Tezca,
   Avala, Cotiza, and ForgeSight.

These roles must stay separate. A green Enclii operations card does not make the
Enclii commercial SKU GA-ready by itself, and an Enclii commercial blocker should
not imply sibling service outage.

## Evidence Tulana needs

| Evidence area | Required Enclii source |
| --- | --- |
| Product surfaces | Public URLs above plus route/health proof |
| SKU identity | Dhanam catalogue rows for Enclii tiers |
| Competitor universe | PaaS/deployment comparators such as Railway, Render, Fly.io, Heroku, Vercel, and self-hosted Kubernetes alternatives |
| Cost basis | Cluster cost allocation, build minutes, storage, bandwidth, backup, observability, and support labor |
| Reliability proof | `/v1/projects/cards`, status page, incident logs, SLOs, backup/restore evidence |
| Buyer signal | Internal adoption, migration use cases, pilots, Phynd CRM leads, or WTP/PMF evidence |
| Campaign limits | Do not claim fully managed database, Vault, or self-serve signup readiness unless current docs and production evidence support it |

## Readiness gates

Enclii SKUs can be marked campaign-ready only when:

- product-tier rows exist in the Dhanam catalogue and Tulana mirror;
- public surfaces resolve and match the expected product;
- project-card health has current evidence and does not rely on stale service
  rows;
- cost assumptions have owner and confidence metadata;
- competitor observations are fresh or explicitly waived;
- any beta/staged features are excluded from campaign claims.

## Follow-up implementation

- Keep project-card truth work in `docs/runbooks/PROJECT_CARD_TRUTH.md` and
  `docs/runbooks/PROJECT_CARD_HEALTH_REMEDIATION_2026-05-29.md`.
- Expose card evidence with enough structure for Tulana to ingest without
  cluster access.
- Add or update Dhanam catalogue rows before Tulana claims Enclii SKU coverage.
- Keep `docs/production/COMMERCIAL_GA_TRACKER.md` aligned with the Tulana
  readiness state.
