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

## Alerts

Two PrometheusRules ship with this manifest (in
`infra/k8s/production/cloudflared-probe.yaml`):

- **`CloudflaredProbeBackendUnreachable`** — `cloudflared_probe_reachable_total == 0` for 2m.
  Severity: critical. Routes through the existing alertmanager `severity: critical` route.
- **`CloudflaredProbeNotRunning`** — no probe iterations recorded in 5m.
  Severity: warning. Catches the probe itself crashlooping.

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
