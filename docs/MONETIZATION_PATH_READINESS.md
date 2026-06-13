# Monetization-Path Readiness — Enclii Control Plane

**Last Updated:** 2026-06-13
**Scope:** Enclii's slice of the ecosystem First-Pesos roadmap. Canonical
cross-repo sequence: `internal-devops/roadmaps/2026-06-13-first-pesos-execution-roadmap.md`.

## Enclii's role

Enclii does not sell the first SKU — it **operates the service that does**
(Dhanam). The revenue-critical question for Enclii is therefore reliability:
*can we run a paying customer's billing service at high availability, and can we
deploy/roll back/observe it safely?*

The 2026-06-13 review verdict: **production-running beta, not yet HA-grade as the
sole ops dependency for revenue.** Deploy, GitOps, instant rollback, onboarding,
and previews work; the gaps below are what stand between that and "rely on Enclii
for paying Dhanam at HA."

## Blockers Enclii owns (from the roadmap ledger)

| # | Sev | Blocker | Exit evidence | Tracking |
|---|---|---|---|---|
| 1 | CRIT | Platform Postgres is single-instance (control-plane DB SPOF) and the shared `data/postgres` backs the Dhanam billing ledger | CNPG `postgres-ha` cutover complete; failover drill green | `docs/runbooks/POSTGRES_HA_CUTOVER.md`, RFC 0012 |
| 8 | MED | 30-day API SLO window incomplete (~ends 2026-06-29) | SLO window closed at target | `GA_READINESS_SCORECARD.md` |
| — | MED | Canary lacks Prometheus error-rate auto-rollback | Auto-rollback wired or documented manual gate | `apps/switchyard-api/internal/.../canary.go` |
| — | MED | Lockbox rotation controller code-complete but not wired in `cmd/api` | Controller started, or Vault-only rotation runbook published | `docs/runbooks/VAULT_OPERATIONS.md` |
| — | LOW | Switchyard outage blocks webhook-driven deploys/onboarding (running pods survive via ArgoCD self-heal) | Documented degradation contract + GitOps fallback verified | this doc |

## What "ready to operate paying Dhanam at HA" requires

- [ ] `postgres-ha` CNPG cutover done; the Dhanam billing DB sits on HA Postgres
      with synchronous replication and a verified failover drill.
- [ ] Restore drill executed with measured RPO/RTO (logged in `docs/runbooks/RESTORE_DRILL_LOG.md`).
- [ ] 30-day API SLO closed green; SLA/support tiers published.
- [ ] Canary auto-rollback wired (or the manual rollback gate documented and rehearsed).
- [ ] Secret rotation either runs (Lockbox controller wired) or is covered by a Vault-only runbook.
- [ ] Load test passes at expected launch traffic (README remaining-blocker item).

## Degradation contract (what happens if Switchyard is down)

| Concern | Behavior |
|---|---|
| Running services (dhanam, janua, …) | Continue — ArgoCD reconciles from Git, K8s keeps pods alive |
| GitOps deploys (push → CI → digest → ArgoCD) | Continue without Switchyard if CI commits digests and ArgoCD syncs |
| Webhook-triggered builds, onboarding, domain ops, instant rollback via API/UI | Blocked until Switchyard recovers |
| Session revocation | Fail-closed when Redis unavailable |

> For the revenue path this means: keep Dhanam deploys GitOps-driven so a
> control-plane outage degrades *operations*, not *availability*.

## Cross-references

- Postgres HA cutover: `docs/runbooks/POSTGRES_HA_CUTOVER.md`
- CNPG manifests: `infra/k8s/production/postgres-ha/`
- GA scorecard: `GA_READINESS_SCORECARD.md`, `REMAINING_OPS_GA.md`
- Roadmap (cross-repo): `internal-devops/roadmaps/2026-06-13-first-pesos-execution-roadmap.md`
