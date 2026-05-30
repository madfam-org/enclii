# Support tiers & status page (draft)

> **Status:** Internal draft — align with [SLA_DRAFT.md](./SLA_DRAFT.md) before customer publish.

---

## Support tiers (proposed)

| Tier | Audience | Channels | Initial response (P1) | Hours |
|------|----------|----------|-------------------------|-------|
| **Community** | Self-hosted OSS | GitHub Issues, docs | Best effort | Community |
| **Essentials** | Managed single-project | Email, docs | 4h business | Business |
| **Pro** | Production teams | Email + Slack (shared) | 1h 24×7 for P1 | 24×7 P1 |
| **Enterprise** | Contract | Dedicated Slack, phone escalation | 30m 24×7 | Custom |

Severity definitions match [INCIDENT_RESPONSE.md](../runbooks/INCIDENT_RESPONSE.md).

---

## What each tier includes

| Capability | Community | Essentials | Pro | Enterprise |
|------------|-----------|------------|-----|------------|
| Platform updates | Self-serve | Managed deploy window | Managed + notice | Custom |
| Backup/restore assist | Docs | Drill on request | Quarterly drill | Custom RPO |
| Security advisories | Public | Email | Email + Slack | Dedicated comms |
| Billing/budget alerts | N/A | Waybill email | Waybill + Slack | Custom |

---

## Status page process

| Item | Owner | Action |
|------|-------|--------|
| Public status URL | Ops/GTM | **Live** — [status.enclii.dev](https://status.enclii.dev) (HTTP 200, 2026-05-30) |
| Incident comms | On-call | Post within 15m of P1 confirm; update every 60m |
| Maintenance | Platform | ≥72h notice on status page + email for paid tiers |
| Historical uptime | SRE | Export from monitoring; feeds SLA evidence |

**Existing automation:** [status-page-health.yml](../../.github/workflows/status-page-health.yml) probes public endpoints.

---

## Customer-facing checklist (before GA)

- [x] Status page live and linked from docs/enclii.dev  
- [ ] Support email and escalation path documented  
- [ ] Pro/Enterprise Slack invite process defined  
- [ ] Runbook link in status page footer (authenticated customers)  

---

## Related

- [SLA_DRAFT.md](./SLA_DRAFT.md)  
- [GA_CHANGELOG_DRAFT.md](./GA_CHANGELOG_DRAFT.md)  
- [docs/faq/billing.md](../faq/billing.md) — tier pricing reference
