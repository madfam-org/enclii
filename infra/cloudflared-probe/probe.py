"""Cloudflared synthetic intra-cluster probe.

Runs in the `cloudflare-tunnel` namespace alongside cloudflared. Mirrors
cloudflared's pod labels so any NetworkPolicy that gates cloudflared egress
also gates this probe — meaning a blocked probe is positive evidence that
cloudflared itself is being blocked at the policy / CNI layer.

Reads target list from /etc/cloudflared-probe/targets.json (mounted from
ConfigMap). Each target:
    {"name": "karafiel-api",
     "url": "http://karafiel-api.karafiel.svc.cluster.local:8000/.../live",
     "expected_status": 200,
     "namespace": "karafiel",
     "service": "karafiel-api",
     "port": 8000}

Emits Prometheus metrics on :9090/metrics:
    cloudflared_probe_reachable{namespace, service, port, name}  (gauge: 1|0)
    cloudflared_probe_reachable_total{namespace, service, port}  (gauge alias for alerting)
    cloudflared_probe_latency_seconds{namespace, service, port}  (histogram)
    cloudflared_probe_runs_total                                  (counter)

Logs structured JSON. Run interval: PROBE_INTERVAL_SECONDS (default 60).
"""

from __future__ import annotations

import json
import logging
import os
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import requests
from prometheus_client import (
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    start_http_server,
)

LOG = logging.getLogger("cloudflared-probe")

DEFAULT_TARGETS_PATH = "/etc/cloudflared-probe/targets.json"
DEFAULT_INTERVAL_SECONDS = 60
DEFAULT_TIMEOUT_SECONDS = 5
DEFAULT_METRICS_PORT = 9090


@dataclass(frozen=True)
class Target:
    name: str
    url: str
    expected_status: int
    namespace: str
    service: str
    port: int

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "Target":
        for required in ("name", "url", "namespace", "service", "port"):
            if required not in raw:
                raise ValueError(f"target missing required key: {required!r} in {raw!r}")
        return cls(
            name=str(raw["name"]),
            url=str(raw["url"]),
            expected_status=int(raw.get("expected_status", 200)),
            namespace=str(raw["namespace"]),
            service=str(raw["service"]),
            port=int(raw["port"]),
        )


def load_targets(path: str) -> list[Target]:
    """Load and validate targets from a JSON file.

    The file may be either:
      - a JSON list of target dicts, or
      - a JSON object with a "targets" key holding such a list

    Raises ValueError on malformed input.
    """
    text = Path(path).read_text(encoding="utf-8")
    data = json.loads(text)
    if isinstance(data, dict) and "targets" in data:
        items = data["targets"]
    else:
        items = data
    if not isinstance(items, list):
        raise ValueError(f"targets file must be a list or {{'targets': [...]}}, got {type(items).__name__}")
    return [Target.from_dict(t) for t in items]


@dataclass
class ProbeResult:
    target: Target
    reachable: bool
    status_code: int | None
    latency_seconds: float
    error: str | None


def probe_target(target: Target, timeout: float) -> ProbeResult:
    """Fire one HTTP GET at target.url and classify the result.

    A target is "reachable" iff the HTTP response status equals expected_status.
    Any transport-layer error (DNS, TCP, TLS, timeout) → reachable=False with
    status_code=None and a populated `error`.
    """
    start = time.monotonic()
    try:
        # Disable any HTTP_PROXY / HTTPS_PROXY env vars — we are deliberately
        # testing the in-cluster path, which must not transit a proxy.
        resp = requests.get(
            target.url,
            timeout=timeout,
            allow_redirects=False,
            proxies={"http": "", "https": ""},
        )
        latency = time.monotonic() - start
        ok = resp.status_code == target.expected_status
        return ProbeResult(
            target=target,
            reachable=ok,
            status_code=resp.status_code,
            latency_seconds=latency,
            error=None if ok else f"unexpected status {resp.status_code}",
        )
    except requests.RequestException as exc:
        latency = time.monotonic() - start
        return ProbeResult(
            target=target,
            reachable=False,
            status_code=None,
            latency_seconds=latency,
            error=f"{type(exc).__name__}: {exc}",
        )


class Metrics:
    """Bundle the prometheus_client metric handles for one probe loop."""

    def __init__(self, registry: CollectorRegistry | None = None) -> None:
        self.registry = registry or CollectorRegistry()
        labels = ("namespace", "service", "port", "name")
        # Per-target reachability gauge — preferred for dashboards.
        self.reachable = Gauge(
            "cloudflared_probe_reachable",
            "1 if the in-cluster URL is reachable from cloudflare-tunnel ns, else 0",
            labels,
            registry=self.registry,
        )
        # Alias (no name label) for alerting — alerts fire per (ns,svc,port)
        # rather than per individual probe entry.
        self.reachable_agg = Gauge(
            "cloudflared_probe_reachable_total",
            "Aggregate reachability gauge per (namespace,service,port). 1=any probe reachable, 0=all blocked.",
            ("namespace", "service", "port"),
            registry=self.registry,
        )
        self.latency = Histogram(
            "cloudflared_probe_latency_seconds",
            "Latency of intra-cluster probe HTTP GET in seconds",
            ("namespace", "service", "port"),
            buckets=(0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0),
            registry=self.registry,
        )
        self.runs = Counter(
            "cloudflared_probe_runs_total",
            "Number of probe iterations completed",
            registry=self.registry,
        )
        self.errors = Counter(
            "cloudflared_probe_errors_total",
            "Number of probe HTTP errors (DNS, TCP, TLS, timeout, status mismatch)",
            ("namespace", "service", "port", "error_class"),
            registry=self.registry,
        )

    def record(self, result: ProbeResult) -> None:
        t = result.target
        common = {"namespace": t.namespace, "service": t.service, "port": str(t.port)}
        self.reachable.labels(**common, name=t.name).set(1 if result.reachable else 0)
        self.reachable_agg.labels(**common).set(1 if result.reachable else 0)
        self.latency.labels(**common).observe(result.latency_seconds)
        if not result.reachable:
            error_class = (
                "status_mismatch"
                if result.status_code is not None
                else (result.error or "unknown").split(":", 1)[0]
            )
            self.errors.labels(**common, error_class=error_class).inc()


def run_once(targets: list[Target], metrics: Metrics, timeout: float) -> list[ProbeResult]:
    """Probe every target sequentially, record metrics, log warnings.

    Sequential is fine: target list is small (~5–20), interval is 60s,
    individual probes timeout in 5s. Worst case 100s on a fully-blocked list,
    well within the 60s scrape interval if the next iteration overlaps —
    Prometheus tolerates stale samples.
    """
    results: list[ProbeResult] = []
    for target in targets:
        result = probe_target(target, timeout=timeout)
        metrics.record(result)
        if result.reachable:
            LOG.info(
                json.dumps(
                    {
                        "event": "probe_ok",
                        "target": target.name,
                        "url": target.url,
                        "status": result.status_code,
                        "latency_ms": round(result.latency_seconds * 1000, 1),
                    }
                )
            )
        else:
            LOG.warning(
                json.dumps(
                    {
                        "event": "probe_blocked",
                        "target": target.name,
                        "url": target.url,
                        "namespace": target.namespace,
                        "service": target.service,
                        "port": target.port,
                        "status": result.status_code,
                        "error": result.error,
                        "latency_ms": round(result.latency_seconds * 1000, 1),
                        "hint": "cloudflared CANNOT reach this Service either — check NetworkPolicy / CNI",
                    }
                )
            )
        results.append(result)
    metrics.runs.inc()
    return results


def main() -> int:
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(message)s",
        stream=sys.stdout,
    )

    targets_path = os.environ.get("TARGETS_PATH", DEFAULT_TARGETS_PATH)
    interval = int(os.environ.get("PROBE_INTERVAL_SECONDS", str(DEFAULT_INTERVAL_SECONDS)))
    timeout = float(os.environ.get("PROBE_TIMEOUT_SECONDS", str(DEFAULT_TIMEOUT_SECONDS)))
    metrics_port = int(os.environ.get("METRICS_PORT", str(DEFAULT_METRICS_PORT)))

    try:
        targets = load_targets(targets_path)
    except (FileNotFoundError, ValueError, json.JSONDecodeError) as exc:
        LOG.error(json.dumps({"event": "targets_load_failed", "path": targets_path, "error": str(exc)}))
        return 1

    LOG.info(
        json.dumps(
            {
                "event": "probe_start",
                "targets": len(targets),
                "interval_s": interval,
                "timeout_s": timeout,
                "metrics_port": metrics_port,
            }
        )
    )

    metrics = Metrics()
    start_http_server(metrics_port, registry=metrics.registry)

    while True:
        loop_start = time.monotonic()
        try:
            run_once(targets, metrics, timeout=timeout)
        except Exception as exc:  # never crash the loop on a single iteration
            LOG.exception(json.dumps({"event": "probe_loop_error", "error": str(exc)}))
        elapsed = time.monotonic() - loop_start
        sleep_for = max(0.0, interval - elapsed)
        time.sleep(sleep_for)


if __name__ == "__main__":
    sys.exit(main())
