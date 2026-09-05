# Weighbridge — the CI runner meter

> **Boundary checkpoint (2026-09-05, platform ops):** this runbook is
> public-safe. No node identity — hostnames, IP addresses, hardware SKUs — no
> tunnel id, no chat id, no secret value, no price, cost or margin appears
> here. Secrets are named, never shown. Node roles and namespaces are named;
> the machines behind them are not. Operational detail that would reveal
> topology lives in `madfam-org/internal-devops`. Policy:
> [`docs/PUBLIC_REPO_BOUNDARY.md`](../PUBLIC_REPO_BOUNDARY.md) and the
> canonical repo-boundary contract in `madfam-org/internal-devops`.

**Component:** `weighbridge` (namespace `monitoring`)
**Manifest:** `infra/k8s/production/monitoring/weighbridge.yaml`
**ArgoCD app:** `weighbridge` (`infra/argocd/apps/weighbridge.yaml`)
**Code:** `apps/waybill/internal/weighbridge/`, `apps/waybill/cmd/weighbridge/`
**Build:** `.github/workflows/weighbridge.yml`

---

## The claim this licenses, and the claim it does not

**It licenses exactly one sentence: _a tenant's minute count is measured, not
constructed._**

Before Weighbridge, nothing in this estate could say how many CI minutes
anybody had used. `switchyard-api` credited a flat `3.0` minutes per release
and billed overage against that number; Roundhouse computed a real build
duration and told nobody. Weighbridge reads the runner pods the platform itself
created and reports the two timestamps the API server recorded: when the slot
was claimed, and when the last container stopped. A tenant cannot opt out,
because a workflow does not get to report its own weight.

**It does not license any of the following.** Each is a separate claim needing
separate evidence:

| Not licensed | Why not |
|---|---|
| "Every minute is captured." | A runner pod that finishes **and is deleted** while Weighbridge is down is never observed. There is no replay — Kubernetes keeps no history of a deleted pod. The size of that gap is unmeasured. |
| "Cache usage is metered." | Nothing in a runner pod's status reports cache bytes. `cache_bytes_read` / `cache_bytes_written` are **absent** from every event this emits, not zero. |
| "Egress is metered." | Same: no per-pod byte counter exists. `egress_bytes` is absent. |
| "Minutes are attributed to the right tenant." | Today there is one org-wide scale set and no tenant→project mapping. Runners with no `enclii.dev/project-id` label are **counted and dropped**, not filed under a guess. Per-tenant attribution arrives with per-tenant scale sets. |
| Any availability, RPO, RTO or SLA figure. | Nothing here measures or promises availability of anything. |
| Any price. | Weighbridge emits units. No rate, tier or currency appears anywhere in its code, its manifest or this document. |

---

## What it emits

One `build.completed` event per completed runner pod, to Waybill
`POST /internal/events`.

```
metrics   duration_seconds   the runner container's own start→finish
          slot_seconds       pod creation → last container terminating
metadata  source             always "weighbridge" (the meter of record)
          outcome            succeeded | failed
          scale_set          from the ARC scale-set label
          tenant             from enclii.dev/tenant, when present
          repo               from the EphemeralRunner CR, when still readable
          workflow           "
          job                "
          runner_image_digest  from the runner container's imageID
```

Nothing else. `apps/waybill/internal/weighbridge/weighbridge_test.go` asserts
the JSON envelope carries no field beyond this set — a meter that quietly grows
a field is a meter whose consumers cannot be reviewed.

**`slot_seconds` is not `duration_seconds`.** A job that holds a slot for ten
minutes and spends six of them building consumes ten slot-minutes and six
build-minutes. Pool capacity is sized against the former. An emitter that
cannot observe slot time omits the field rather than substituting the other
one.

**Idempotency key = the pod's UID.** A property of the artefact, minted once by
the API server, unchanged by any restart, re-list or code change. Waybill's
partial unique index (migration 040) refuses the second write, so re-observing
a finished pod is a no-op rather than a double charge.

**This is the meter of record.** Two other streams carry build durations — a
post-step in the reusable workflow, and Roundhouse reporting its own T3 builds
with `source: roundhouse`. They exist to be **compared** against this one.
Summing across sources double counts.

---

## Signals

| Metric | Read it as |
|---|---|
| `weighbridge_runners_observed_total` | terminal runner pods reduced to an observation. The denominator. |
| `weighbridge_events_emitted_total` | events Waybill accepted. |
| `weighbridge_events_rejected_total` | Waybill refused, or the POST never arrived. **These minutes are lost** — there is no local spool and the pod is gone. |
| `weighbridge_events_duplicate_total` | pods re-observed after emission. Nonzero is **normal and healthy**: it is what an informer resync looks like, and it is the proof that dedup is working. |
| `weighbridge_runners_unattributed_total` | terminal pods with no project. Minutes are being burned that nobody can be shown. |

Scraped by the `rules-eval` Prometheus via the PodMonitor in the manifest.

### Alert: `WeighbridgeNoEventsWhileRunnersActive` (critical)

Fires when all three hold for 30 minutes:

1. no events emitted in two hours;
2. `arc_pool_running_runners` shows the pool served jobs in that window (this
   is the gauge the ARC pool-health detector pushes every five minutes, so an
   idle weekend does not page anyone);
3. Weighbridge itself is up and being scraped.

It routes on **existing** severity routing — `severity: critical` already goes
to `critical-receiver` in `alertmanager.yaml`. No receiver was added.

**What this alert does not cover:** Weighbridge being absent, crash-looping or
unscraped. Conjunct 3 deliberately silences the rule in exactly that state,
because otherwise it fires continuously between the manifest merging and the
first image build — and an alert that is always on teaches people to ignore it.
A missing Deployment is an ArgoCD health problem and shows there.

---

## Triage

### `weighbridge_events_rejected_total` is climbing

Waybill is unreachable or refusing. In order:

1. Is Waybill serving? The meter posts to `http://waybill.enclii.svc.cluster.local`
   — the Service's port 80, the same address `switchyard-api` uses. The
   NetworkPolicies name **8080**, the pod's container port, which is what the
   CNI evaluates after kube-proxy has translated the Service port. The two
   numbers are supposed to differ.
2. **Does the `waybill` Service exist?** It is *not* in this repo's manifests —
   it is created by the switchyard reconciler for the Deployment it manages
   (`enclii.dev/managed-by: switchyard`). If the reconciler ever stops creating
   it, the DNS name stops resolving and every event is rejected. This is a
   real GitOps gap: the address the meter depends on is not in Git.
3. Is the NetworkPolicy in place? Two objects have to agree —
   `weighbridge-egress` in `monitoring`, and `waybill-ingress-weighbridge` in
   `enclii` (`infra/k8s/base/network-policies.yaml`). A CNI drop produces a
   timeout that looks exactly like a Waybill outage.
4. Is Waybill enforcing `X-API-Key` while the secret is missing? The key is
   delivered by the `weighbridge-waybill-key` ExternalSecret and consumed as an
   **optional** `secretKeyRef`, so a missing secret does not stop the pod — it
   makes every POST a 401 once Waybill's own `INTERNAL_API_KEY` is set.

Rejected events are **not retried and not spooled**. The minutes in them are
gone. Cross-check the period against the Roundhouse stream before trusting a
total for a window in which this counter moved.

### `weighbridge_runners_unattributed_total` is climbing

Runner pods carry no `enclii.dev/project-id` label and no
`WEIGHBRIDGE_DEFAULT_PROJECT_ID` is configured. This is the expected state on
the shared org-wide pool today: the minutes are dropped rather than filed under
a placeholder project that somebody would eventually be invoiced for.

Two ways out, both deliberate decisions rather than fixes:

- stamp `enclii.dev/project-id` on the scale set's runner pod template (the
  Wave 2 per-tenant path); or
- set `WEIGHBRIDGE_DEFAULT_PROJECT_ID` on the Deployment, which files **all**
  shared-pool minutes under one project. Only do this when that is actually
  true.

### `weighbridge_events_duplicate_total` is large

Not a fault. Every informer resync and every restart re-observes the pods still
present, and the local seen-set suppresses them. If it is large *and*
`emitted` is flat, look at restart count: a crash-looping meter re-lists on
every start.

### The pod is in `ImagePullBackOff`

Expected immediately after the manifest first merges, and only then. The image
line ships as an **all-zero sentinel digest**, which cannot resolve to
anything; `.github/workflows/weighbridge.yml` builds, signs, pushes and commits
the real digest on the first push to `main` touching `apps/waybill/**`, and
ArgoCD rolls that commit. If the state persists past one workflow run, read the
run's "Guard against silent stale deploy" step — it distinguishes "no image was
built" from "image built but never pinned".

---

## Changing the meter

- **Never change the idempotency key.** It is the pod UID. A key derived from
  anything the process computes — a payload hash, a timestamp — changes when
  the code changes and re-bills history.
- **Never add a field to the event without updating the envelope test.** The
  test is the review surface.
- **Never emit a zero for something nobody measured.** Absent means "nobody
  measured"; zero is a claim.
- **Never run more than one replica.** Two observers both POST; Waybill records
  one, but the counters become unreadable. The Deployment is `Recreate` for the
  same reason.

## Related

- Metric units and aggregation: `apps/waybill/internal/events/types.go`,
  `apps/waybill/internal/aggregation/hourly.go`
- ARC pool health (the neighbouring `monitoring`-namespace ARC watcher):
  `infra/k8s/production/arc/pool-health-alert.yaml`
- Runner pool operations: `infra/k8s/production/arc/README.md`
