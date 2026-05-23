# Enclii platform SLA (draft)

> **Status:** Draft for legal/GTM review — **not** customer-facing until signed.  
> **Aligns with:** [SOFTWARE_SPEC.md](../architecture/SOFTWARE_SPEC.md) availability targets.

---

## 1. Scope

This SLA applies to the **Enclii control plane API** (`api.enclii.dev` or customer-dedicated API hostname) for paid production plans. It does **not** cover:

- Customer application uptime (workloads on customer clusters)
- Third-party providers (GitHub, Cloudflare, Hetzner) except where Enclii operates the integration
- Scheduled maintenance announced ≥72 hours in advance
- Force majeure and customer-caused misconfiguration

---

## 2. Availability commitment

| Metric | Target | Measurement window |
|--------|--------|-------------------|
| API availability | **99.95%** monthly | Excludes documented maintenance |
| API error budget | 0.05% (~21.6 min/month) | 5xx from Enclii API edge, excluding customer auth failures |

**Availability** = successful responses (`2xx`/`3xx`/`4xx` excluding rate-limit) / total requests, measured at the API load balancer.

---

## 3. Service credits (placeholder)

| Monthly uptime | Credit (% of monthly platform fee) |
|----------------|-----------------------------------|
| &lt; 99.95% and ≥ 99.0% | 10% |
| &lt; 99.0% and ≥ 95.0% | 25% |
| &lt; 95.0% | 50% |

Credits require ticket within 30 days. Maximum credit per month: 50% of fees. *Legal to finalize tiers.*

---

## 4. Support response (placeholder)

| Severity | Definition | Initial response |
|----------|------------|------------------|
| P1 | Control plane down or data loss risk | 1 hour (24×7) |
| P2 | Degraded deploy/build for multiple tenants | 4 hours business |
| P3 | Single-tenant or non-blocking defect | 1 business day |

*Map to published support tiers before GA.*

---

## 5. Exclusions

- Preview environments (best-effort)
- Beta features explicitly labeled
- Customer cluster resource exhaustion (CPU, disk, Longhorn) on shared infrastructure
- Incidents during break-glass operations documented in post-incident review

---

## 6. Evidence for Stability GA

Before publishing externally:

- [ ] 30 consecutive days at ≥99.95% measured availability (post Phase 0 deploy)
- [ ] Zero unmitigated Sev-1 &gt;7 days open
- [ ] Restore drill evidence on file
- [ ] Legal review of credit and support tables

---

## Related

- [GA_REMEDIATION_PLAN.md](./GA_REMEDIATION_PLAN.md) §3.3
- [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md) — Commercial wrap
