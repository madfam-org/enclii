# synthetic-flow-probe

Layer-7 synthetic user-journey probe. Sibling of [cloudflared-probe](../cloudflared-probe/)
(layer-3) and [synthetic-revenue-probe](../../packages/synthetic-revenue-probe/)
(end-to-end revenue loop).

## Why this exists

The cloudflared-probe answers "can cloudflared reach the backend Service?"
That catches CNI / NetworkPolicy / route-config breakage but is **blind to
application-layer breakage**:

- A CSP `form-action` directive that doesn't include the auth host → login
  silently broken in the browser, but every backend healthcheck is green.
- A CORS misconfiguration → preflight rejected, login broken, backends green.
- A broken redirect chain → user lands on a 404 after SSO, backends green.
- An expired session-signing key → JWTs fail validation, backends green.
- A stale rebrand string in the login form → users confused, backends green.

These are exactly the incidents that took us hours to debug in the
2026-05-04 session because none of the existing observability surfaced them.
This probe runs **multi-step HTTP journeys** that exercise the same code
paths a real user does, and alerts when any step fails.

## How it works

Each journey is a YAML file in `journeys/` describing a sequence of HTTP
steps with assertions:

```yaml
platform: selva
journey: admin-login
schedule_seconds: 300
safety_class: read-only
steps:
  - name: open-login-page
    method: GET
    url: https://app.selva.town/login
    expect_status: 200
    expect_no_csp_violation: true   # ← would have caught the Janua CSP incident
  - name: post-credentials
    method: POST
    url: https://auth.madfam.io/api/v1/auth/login
    body_form:
      email: ${ADMIN_EMAIL}
      password: ${ADMIN_PASSWORD}
    expect_url_contains: app.selva.town
  - name: hit-protected-route
    method: GET
    url: https://app.selva.town/api/v1/me
    expect_json_contains:
      email: admin@madfam.io
```

The probe loads every YAML in `JOURNEYS_DIR` at startup, spawns a thread
per journey, and runs each on its own schedule. Cookies persist across
steps within a journey but are discarded between runs.

## Phase 1 scope

Three journeys ship in this PR:

| Platform | Journey | URL | Why |
| --- | --- | --- | --- |
| `selva` | `admin-login` | `app.selva.town` | Operator UI for the office stack |
| `karafiel` | `admin-login` | `app.karafiel.mx` | Compliance dashboard |
| `dhanam` | `admin-login` | `app.dhan.am` | Billing / financial dashboard |

All three share the Janua SSO step. **Janua-side breakage trips all three
simultaneously** — that triple-fail pattern is the diagnostic signal: look
at Janua first, not at the individual platforms.

## Sandbox boundary

Each journey declares `safety_class: read-only | mutating`. The probe
**refuses to run mutating journeys against `ENVIRONMENT=production`** —
Phase 1 is read-only only. Phase 2 will introduce mutating-with-cleanup
against a narrow allowlist.

The credentials Secret holds **read-only** operator credentials. It must
not contain anything that grants write capability against prod data.

## Metrics

Exposed on port `9090`, scraped via the standard `prometheus.io/scrape`
pod annotation:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `synthetic_journey_pass_total` | counter | platform, journey | Successful end-to-end runs |
| `synthetic_journey_fail_total` | counter | platform, journey, step, reason | Failed runs; reason is one of `status_mismatch`, `wrong_redirect`, `csp_violation`, `body_assertion`, `timeout`, `network`, `missing_header`, `missing_credential`, `other` |
| `synthetic_journey_skipped_total` | counter | platform, journey, reason | Runs that were not attempted (missing creds, mutating-against-prod) |
| `synthetic_journey_step_latency_seconds` | histogram | platform, journey, step | Per-step latency |
| `synthetic_journey_last_run_timestamp` | gauge | platform, journey | Unix ts of most recent run — staleness detector |
| `synthetic_journey_consecutive_failures` | gauge | platform, journey | Resets to 0 on a pass — alert key |

## Alerts

Two PrometheusRules ship with this manifest:

- **`SyntheticJourneyConsecutivelyFailing`** — `consecutive_failures >= 3`,
  i.e., journey has been broken for ~15+ minutes assuming the default 5-min
  schedule. **Severity: critical**, routes to `#alerts-critical` via the
  existing alertmanager `severity: critical` route.
- **`SyntheticJourneyStepLatencyRegression`** — step p95 has more than
  doubled vs. the prior 1h baseline. **Severity: warning**, routes to
  `#alerts-warning`.
- **`SyntheticFlowProbeNotRunning`** — no journey runs recorded in 10m.
  Severity: warning. Catches the probe itself crashlooping.

## Local development

```bash
cd infra/synthetic-flow-probe
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt pytest
.venv/bin/python -m pytest tests/ -v
```

Run against a fixture journey set (no creds → journeys will be SKIPPED):

```bash
mkdir -p /tmp/journeys
cp journeys/*.yaml /tmp/journeys/
JOURNEYS_DIR=/tmp/journeys METRICS_PORT=19090 ENVIRONMENT=local \
  python probe.py
curl localhost:19090/metrics
```

Build the image locally:

```bash
docker build -t synthetic-flow-probe:dev infra/synthetic-flow-probe
docker run --rm -p 9090:9090 \
  -v "$PWD/infra/synthetic-flow-probe/journeys:/etc/synthetic-flow-probe/journeys:ro" \
  -e ENVIRONMENT=local \
  synthetic-flow-probe:dev
```

## Adding a journey

1. Drop a new YAML file in `journeys/` (or `journeys/<platform>-<flow>.yaml`).
2. Verify it parses: `pytest tests/test_probe.py::test_phase1_journey_yamls_load_with_real_manifest_content`
3. Open a PR. The shipped journeys are loaded into the ConfigMap at
   `infra/k8s/production/synthetic-flow-probe.yaml` — keep the two in sync.
4. ArgoCD reconciles the manifest within 3 minutes; the probe pod restarts
   automatically on ConfigMap change (Stakater Reloader).

## Operational scope

**Will:**
- Run multi-step HTTP journeys that exercise the same code path a real user does.
- Detect CSP form-action regressions, CORS misconfigurations, broken redirects,
  expired sessions, missing JSON keys, and other application-layer breakage.
- Run on a fast cadence (default 5 min) so silent breakage is caught in
  ~15 minutes instead of hours.

**Will not:**
- Replace the cloudflared-probe (different signal — that one catches in-cluster
  network breakage; this one catches user-visible breakage at the public path).
- Run mutating operations against production (Phase 1 boundary).
- Render JavaScript / SPA correctness — that's Phase 2 (Playwright).
- Auto-remediate. All fixes are still operator-driven; the alert routes
  through alertmanager.

## Phase 2 (deferred — explicitly out of scope for this PR)

- Playwright-based browser probe for SPA correctness (DOM assertions,
  visual regression, JavaScript error capture).
- Mutating journeys with cleanup, against a narrow allowlist of pre-prod
  tenants (e.g., create a synthetic compliance review, verify it lands in
  the queue, delete it).
- Multi-tenant journeys — log in as N representative customer accounts to
  catch customer-specific breakage.
- Per-journey custom assertions via JSONPath / regex.
