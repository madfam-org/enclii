# Monetization-Path Readiness — Enclii Control Plane

> **Boundary checkpoint (2026-09-05, platform ops):** public-safe. No node
> identity, IP, hardware SKU, tunnel id, secret value, price, cost or margin
> appears here — the metering gate below names UNITS and EVIDENCE, never rates.
> Commercial figures and the cross-repo gate registry live in
> `madfam-org/internal-devops`. Policy:
> [`PUBLIC_REPO_BOUNDARY.md`](./PUBLIC_REPO_BOUNDARY.md) and the canonical
> repo-boundary contract in `madfam-org/internal-devops`.

**Last Updated:** 2026-09-05
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

## Metering gates

A gate here is a claim the platform is not yet allowed to make. It stays UNMET
until the evidence beside it exists — not until the code that would produce the
evidence has merged.

### `fragua.build-minute-truth` — **UNMET**

**The claim it would license:** *a tenant's billed build minutes equal the
minutes that tenant's jobs actually consumed.*

**Why it is not met.** Until 2026-09 nothing measured build minutes at all:
`switchyard-api` credited a flat `3.0` minutes per release and billed overage
against that literal, and Roundhouse computed a real duration and told nobody.
Three things have now shipped — Waybill metric units, the Weighbridge
controller, and consumers that read the meter instead of inventing a number —
but **shipping the machinery is not the evidence.** Nothing has yet compared
one day's numbers against each other.

**Acceptance evidence, in full.** All four, for the same tenant and the same
UTC day:

1. **Waybill agrees with Weighbridge.** That tenant's `build_minutes` for the
   day, summed from `hourly_usage`, equals the sum of `duration_seconds / 60`
   over the Weighbridge `build.completed` events for that tenant in the same
   window. Exact equality, not "close": both sides are sums of the same
   events, so any gap is a lost or duplicated event and needs explaining, not
   rounding.
2. **Roundhouse agrees where both saw the work.** For every T3 build present
   in both streams — matched on build id — the Roundhouse
   `duration_seconds` and the Weighbridge `duration_seconds` for the runner
   that hosted it agree within the pod's own start-up and teardown window.
   Builds present in only one stream are **listed and explained**, not
   dropped: Weighbridge sees jobs Roundhouse never handles, and a Roundhouse
   build missing from Weighbridge means the meter missed a pod.
3. **The unattributed count is zero for that tenant.**
   `weighbridge_runners_unattributed_total` did not move during the window.
   A nonzero count means minutes were burned that the meter could not file,
   and any total for the day is low by an unknown amount.
4. **The rejected count is zero for that tenant.**
   `weighbridge_events_rejected_total` did not move during the window.
   Rejected events are not spooled and not retried; the minutes in them are
   gone, so a day containing any is not evidence of anything.

**What being met would still not license.** That minutes are *priced*
correctly, that cache or egress are metered at all (they are not — no producer
exists), or that minutes burned while Weighbridge was down were captured (they
were not; there is no replay). Each needs its own gate.

**Where the evidence goes.** The cross-repo gate registry in
`madfam-org/internal-devops` is the system of record for gate STATE. This entry
is the public statement of what the gate means and what would satisfy it, so
that a reader of this repo can tell whether a build-minute number they are
looking at is allowed to be trusted. Do not mark it met here.

**Related:** the meter's own runbook at `docs/runbooks/WEIGHBRIDGE.md` states
the single claim Weighbridge licenses and the six it does not. It is
deliberately not linked from here yet: this entry lands independently of the
controller, and a link to a file that is not on `main` is a broken link, which
is how a reader learns to stop following them.

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
