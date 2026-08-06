# cloudflared-probe

Synthetic intra-cluster probe that proves `cloudflared` can actually reach
backend Services from inside the `cloudflare-tunnel` namespace.

## Why this exists

`status.enclii.dev` probes the **public Cloudflare edge**. When a backend
returns 502, the public probe sees 502 — but it cannot tell us *where* the
break is. Three failure modes all produce the same external symptom:

1. Origin app down (real outage).
2. Tunnel routes mis-configured (cloudflared has no rule for this hostname).
3. **CNI / NetworkPolicy drops cloudflared → backend traffic** (silent: the
   pods are healthy, the routes are correct, but in-cluster policy denies
   the connection).

Mode 3 has bitten us before and is invisible from outside the cluster. This
probe closes that gap.

## How it closes the gap

The probe Deployment runs in the `cloudflare-tunnel` namespace and
deliberately re-uses cloudflared's `app.kubernetes.io/component: ingress`
label. Any NetworkPolicy or CNI rule scoped to the cloudflared component
applies equally to the probe — so **if the probe can't reach a Service,
cloudflared can't either**.

Each iteration (default 60s) the probe HTTP-GETs every target's
`*.svc.cluster.local` URL — the same in-cluster DNS cloudflared resolves
when forwarding tunnel traffic to origin pods.

## Metrics

Exposed on port `9090`, scraped via the standard `prometheus.io/scrape`
pod annotation:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `cloudflared_probe_reachable` | gauge | namespace, service, port, name | 1 = HTTP `expected_status` returned, 0 = anything else (timeout, connection refused, status mismatch) |
| `cloudflared_probe_reachable_total` | gauge | namespace, service, port | Same signal, no `name` label — the alert key |
| `cloudflared_probe_latency_seconds` | histogram | namespace, service, port | End-to-end probe latency |
| `cloudflared_probe_runs_total` | counter | — | Number of probe iterations completed; staleness detector |
| `cloudflared_probe_errors_total` | counter | namespace, service, port, error_class | Increments on every failed probe; useful for distinguishing timeout vs status_mismatch vs connection_refused |
| `cloudflared_probe_target_misconfigured` | gauge | namespace, service, port, reason | 1 = the target's Service exists but does not publish the dialed port (`reason=port_not_published`), or the Service is missing (`reason=service_missing`) |
| `cloudflared_probe_service_check_unavailable` | gauge | — | 1 = the Service cross-check could not run at all (no token, RBAC denied, API unreachable, or `SERVICE_PORT_CHECK=false`) |

> **Label note.** The scrape overwrites `namespace` with the *probe's* own
> namespace (`cloudflare-tunnel`). The target's namespace arrives as
> `exported_namespace`. Verified on the live series 2026-08-06. `service` and
> `port` survive untouched.

## Misconfigured target vs. blocked traffic

On 2026-08-06 the probe dialed `dhanam-api:3000` and `janua-api:8080` —
container ports those Services do **not** publish (both publish 80). Dialing a
Service port that does not exist times out exactly like a NetworkPolicy drop,
so the probe logged `probe_blocked` with the hint *"check NetworkPolicy /
CNI"*. Wrong diagnosis: it masked the real one and made a genuine outage
indistinguishable from standing noise.

The probe now reads each target's Service from the Kubernetes API (`get` on
`services`, granted by the ClusterRole in the manifest; TTL-cached for 300s)
and compares the published `spec.ports[].port` against the port it dials:

- Service publishes the port → behaviour unchanged; a failure is a real
  reachability fault and the `probe_blocked` hint says so explicitly.
- Service exists but does **not** publish the port → `probe_misconfigured` log
  event and `cloudflared_probe_target_misconfigured=1`. `probe_blocked` is
  **not** emitted, so the wrong diagnosis is never asserted.
- Check cannot run → `cloudflared_probe_service_check_unavailable=1` and the
  old hint, annotated to say the cross-check did not run. An unverifiable
  target is never reported as verified-good.

Service manifests live in the product repos, so no static check in this repo
can do this comparison. `scripts/check-probe-targets.py` covers the part that
*is* local: url port, `port` field and `<service>.<namespace>` host must agree.

## Alerts

Alert rules that actually fire live in the `prometheus-rules` ConfigMap
(`infra/k8s/production/monitoring/prometheus.yaml`, key
`secret-integrity-rules.yml`):

- **`CloudflaredProbeTargetMisconfigured`** — `cloudflared_probe_target_misconfigured == 1` for 5m. Severity: warning.
- **`CloudflaredProbeServiceCheckUnavailable`** — cross-check dark for 30m. Severity: warning.

Two `PrometheusRule` objects also ship with the manifest
(`CloudflaredProbeBackendUnreachable`, `CloudflaredProbeNotRunning`), **but
they are not loaded**: this cluster has no prometheus-operator, and the running
Prometheus reads rules only from `/etc/prometheus/rules/*.yml` (the
`prometheus-rules` ConfigMap). Verified 2026-08-06 — `/api/v1/rules` lists 18
groups, none of them `cloudflared-probe`. They are retained so the intent stays
version-controlled and starts working if an operator is adopted.

## Adding a target

Edit the `cloudflared-probe-targets` ConfigMap in
`infra/k8s/production/cloudflared-probe.yaml`:

```json
{
  "name": "<short label, becomes a metric label>",
  "url": "http://<service>.<namespace>.svc.cluster.local:<port>/<healthpath>",
  "expected_status": 200,
  "namespace": "<svc namespace>",
  "service": "<svc name>",
  "port": <int>
}
```

Use cluster-internal DNS, not public hostnames — the whole point is to test
the *cloudflared* path, not the public edge.

The probe pod restarts automatically on ConfigMap change (Stakater Reloader
opt-in via the deployment annotation).

## Local development

```bash
cd infra/cloudflared-probe
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt pytest
.venv/bin/python -m pytest tests/ -v
```

Build the image locally:

```bash
docker build -t cloudflared-probe:dev infra/cloudflared-probe
docker run --rm -p 9090:9090 \
  -v "$PWD/infra/cloudflared-probe/sample-targets.json:/etc/cloudflared-probe/targets.json:ro" \
  cloudflared-probe:dev
curl localhost:9090/metrics
```

## Operational scope

**Will:** detect when in-cluster traffic from the cloudflare-tunnel namespace
to a backend Service is blocked.

**Will not:**
- Replace `status.enclii.dev` (that probes the *public* edge — different signal,
  both are needed).
- Test app-level health beyond the configured health endpoint.
- Auto-remediate. NetworkPolicy fixes are still operator-driven; the alert
  routes through alertmanager.
