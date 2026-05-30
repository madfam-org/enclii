# Gate 4 — 30-day SLO window log

> **SLO target:** 99.95% API availability ([SOFTWARE_SPEC.md](../architecture/SOFTWARE_SPEC.md))  
> **Window:** **2026-05-30** → **~2026-06-29** (O-12)  
> **Hygiene script:** `./scripts/gate4-slo-hygiene.sh` · `make gate4-slo-hygiene`  
> **Dashboard:** Prometheus SLO rules (production monitoring stack)

Append a row weekly (or after incidents) with `./scripts/gate4-slo-hygiene.sh --append-log`.

---

## Checkpoints

| Date (UTC) | Checks passed | Checks failed | Notes | Owner |
|------------|---------------|---------------|-------|-------|
| 2026-05-30T20:40Z | 7 | 0 | all green | Ops |
| 2026-05-30T19:01Z | 7 | 0 | first checkpoint — all green | Ops |
| 2026-05-30T19:06Z | 7 | 0 | post-deploy adapters 4/4; commercial proof [26692385415](https://github.com/madfam-org/enclii/actions/runs/26692385415) 9 pass | Ops |

---

## Evidence links

| Item | Reference |
|------|-----------|
| SLO start sign-off | [GA_READINESS_SCORECARD.md §Gate 4](./GA_READINESS_SCORECARD.md) |
| Restore drill | [RESTORE_DRILL_LOG.md](../runbooks/RESTORE_DRILL_LOG.md) (2026-05-30) |
| Commercial proof CI | [.github/workflows/commercial-ga-proof.yml](../../.github/workflows/commercial-ga-proof.yml) — [26692385415](https://github.com/madfam-org/enclii/actions/runs/26692385415) (daily + Gate 4 public hygiene) |
| Incident process | [INCIDENT_RESPONSE.md](../runbooks/INCIDENT_RESPONSE.md) |

---

## Exit criteria (Gate 4 sign-off)

- [ ] 30 consecutive days at ≥99.95% measured API availability
- [ ] Zero unmitigated Sev-1 open &gt;7 days during window
- [x] Weekly hygiene checkpoints recorded (this log) — in progress
- [ ] No unresolved P0 platform regressions at window end

After sign-off → proceed to [Gate 5 publish](./GA_READINESS_SCORECARD.md) (O-19–O-21).
