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
    cloudflared_probe_target_misconfigured{namespace, service, port, reason}
                                                                  (gauge: 1|0)
    cloudflared_probe_service_check_unavailable                   (gauge: 1|0)

TARGET MISCONFIGURATION vs. BLOCKED TRAFFIC
-------------------------------------------
On 2026-08-06 this probe dialed `dhanam-api:3000` and `janua-api:8080` —
container ports those Services do NOT publish (both publish 80). Dialing a
Service port that does not exist times out exactly like a NetworkPolicy drop,
so the probe logged `probe_blocked` with the hint "check NetworkPolicy / CNI".
That was a wrong diagnosis: it masked the real one and made a genuine outage
indistinguishable from standing noise.

The Service definitions live in each product repo, so no static check in the
enclii repo can compare them. Instead the probe reads the live Service from
the Kubernetes API and compares its published `spec.ports[].port` against the
port it is about to dial. A target whose Service exists but does not publish
the dialed port is reported as `probe_misconfigured` (distinct log event and
its own gauge), never as `probe_blocked`.

If the API cannot be reached or RBAC denies it, the probe does NOT guess: it
keeps the old behaviour and raises `cloudflared_probe_service_check_unavailable`
so the blind spot is itself visible. Requires `get`/`list` on `services`
(see the ClusterRole in infra/k8s/production/cloudflared-probe.yaml).

Logs structured JSON. Run interval: PROBE_INTERVAL_SECONDS (default 60).
"""

from __future__ import annotations

import json
import logging
import os
import sys
import time
from dataclasses import dataclass, field
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

# In-cluster ServiceAccount credential locations (kubelet-projected).
SA_DIR = "/var/run/secrets/kubernetes.io/serviceaccount"
DEFAULT_SA_TOKEN_PATH = f"{SA_DIR}/token"
DEFAULT_SA_CA_PATH = f"{SA_DIR}/ca.crt"

# Services change rarely; re-reading every loop would add 5 API calls/minute
# for no benefit. 300s keeps a mid-incident `kubectl edit svc` visible within
# five minutes while costing ~1 call/target/5min.
DEFAULT_SERVICE_CACHE_TTL_SECONDS = 300
DEFAULT_KUBE_API_TIMEOUT_SECONDS = 5

# Reasons emitted on cloudflared_probe_target_misconfigured.
REASON_PORT_NOT_PUBLISHED = "port_not_published"
REASON_SERVICE_MISSING = "service_missing"


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


@dataclass(frozen=True)
class ServicePortCheck:
    """Verdict on whether a target dials a port its Service actually publishes.

    `checked` is False when the Kubernetes API could not answer (no
    credentials, RBAC denial, network error). In that case `misconfigured` is
    meaningless and callers must fall back to the port-agnostic behaviour —
    an unverifiable target must never be reported as verified-good.
    """

    checked: bool
    misconfigured: bool
    reason: str | None = None
    published_ports: tuple[int, ...] = ()
    error: str | None = None


NOT_CHECKED = ServicePortCheck(checked=False, misconfigured=False)


class ServicePortLookup:
    """Reads Service `spec.ports[].port` from the Kubernetes API, with a TTL cache.

    Deliberately read-only and deliberately failure-tolerant: every error path
    returns a `checked=False` verdict rather than raising, because losing the
    Service cross-check must never take down reachability probing.
    """

    def __init__(
        self,
        *,
        api_host: str | None = None,
        api_port: str | None = None,
        token_path: str = DEFAULT_SA_TOKEN_PATH,
        ca_path: str = DEFAULT_SA_CA_PATH,
        timeout: float = DEFAULT_KUBE_API_TIMEOUT_SECONDS,
        cache_ttl: float = DEFAULT_SERVICE_CACHE_TTL_SECONDS,
        session: Any = None,
    ) -> None:
        self.api_host = api_host or os.environ.get("KUBERNETES_SERVICE_HOST", "")
        self.api_port = api_port or os.environ.get("KUBERNETES_SERVICE_PORT", "443")
        self.token_path = token_path
        self.ca_path = ca_path
        self.timeout = timeout
        self.cache_ttl = cache_ttl
        self.session = session or requests
        self._cache: dict[tuple[str, str], tuple[float, ServicePortCheck]] = {}

    # -- credentials ------------------------------------------------------

    def _token(self) -> str | None:
        try:
            return Path(self.token_path).read_text(encoding="utf-8").strip()
        except OSError:
            return None

    def _verify(self) -> Any:
        # Fall back to the system trust store only if the projected CA is
        # absent; never disable verification.
        return self.ca_path if Path(self.ca_path).exists() else True

    # -- lookup -----------------------------------------------------------

    def published_ports(self, namespace: str, service: str) -> ServicePortCheck:
        """Return the Service's published ports, or a checked=False verdict."""
        if not self.api_host:
            return ServicePortCheck(
                checked=False, misconfigured=False, error="KUBERNETES_SERVICE_HOST unset"
            )

        token = self._token()
        if token is None:
            return ServicePortCheck(
                checked=False,
                misconfigured=False,
                error=f"no ServiceAccount token at {self.token_path}",
            )

        url = (
            f"https://{self.api_host}:{self.api_port}"
            f"/api/v1/namespaces/{namespace}/services/{service}"
        )
        try:
            resp = self.session.get(
                url,
                headers={"Authorization": f"Bearer {token}", "Accept": "application/json"},
                timeout=self.timeout,
                verify=self._verify(),
                proxies={"http": "", "https": ""},
            )
        except requests.RequestException as exc:
            return ServicePortCheck(
                checked=False, misconfigured=False, error=f"{type(exc).__name__}: {exc}"
            )

        if resp.status_code == 404:
            return ServicePortCheck(
                checked=True,
                misconfigured=True,
                reason=REASON_SERVICE_MISSING,
                published_ports=(),
            )
        if resp.status_code != 200:
            # 401/403 → RBAC not granted; anything else → API trouble. Either
            # way we cannot conclude anything about the port.
            return ServicePortCheck(
                checked=False,
                misconfigured=False,
                error=f"kube API returned {resp.status_code}",
            )

        try:
            body = resp.json()
            ports = tuple(
                int(p["port"])
                for p in (body.get("spec") or {}).get("ports") or []
                if isinstance(p, dict) and "port" in p
            )
        except (ValueError, TypeError, KeyError) as exc:
            return ServicePortCheck(
                checked=False, misconfigured=False, error=f"malformed Service body: {exc}"
            )

        return ServicePortCheck(checked=True, misconfigured=False, published_ports=ports)

    def check(self, target: "Target", now: float | None = None) -> ServicePortCheck:
        """Cached verdict for one target, including the port comparison."""
        now = time.monotonic() if now is None else now
        key = (target.namespace, target.service)
        cached = self._cache.get(key)
        if cached is not None and now - cached[0] < self.cache_ttl:
            base = cached[1]
        else:
            base = self.published_ports(target.namespace, target.service)
            self._cache[key] = (now, base)

        return evaluate_port(target, base)


def evaluate_port(target: "Target", lookup: ServicePortCheck) -> ServicePortCheck:
    """Compare a target's dialed port against its Service's published ports.

    Pure function — the entire Fault-2 decision lives here so it is unit
    testable without a cluster.
    """
    if not lookup.checked:
        return lookup
    if lookup.reason == REASON_SERVICE_MISSING:
        return lookup
    if target.port in lookup.published_ports:
        return ServicePortCheck(
            checked=True, misconfigured=False, published_ports=lookup.published_ports
        )
    return ServicePortCheck(
        checked=True,
        misconfigured=True,
        reason=REASON_PORT_NOT_PUBLISHED,
        published_ports=lookup.published_ports,
    )


@dataclass
class ProbeResult:
    target: Target
    reachable: bool
    status_code: int | None
    latency_seconds: float
    error: str | None
    service_check: ServicePortCheck = field(default=NOT_CHECKED)


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
        # Fault-2 signal: the target dials a port its Service does not
        # publish (or the Service is gone). Independent of reachability —
        # it is set even while the probe is still green, so the config error
        # is visible before it masquerades as an outage.
        self.misconfigured = Gauge(
            "cloudflared_probe_target_misconfigured",
            "1 if the target's Service does not publish the dialed port "
            "(reason=port_not_published) or does not exist (reason=service_missing)",
            ("namespace", "service", "port", "reason"),
            registry=self.registry,
        )
        # 1 when the Service cross-check could not run at all (no token, RBAC
        # denied, API unreachable). Without this, "no misconfiguration found"
        # would be indistinguishable from "never looked".
        self.service_check_unavailable = Gauge(
            "cloudflared_probe_service_check_unavailable",
            "1 if the Kubernetes Service port cross-check could not be performed",
            registry=self.registry,
        )

    def record(self, result: ProbeResult) -> None:
        t = result.target
        common = {"namespace": t.namespace, "service": t.service, "port": str(t.port)}
        self.reachable.labels(**common, name=t.name).set(1 if result.reachable else 0)
        self.reachable_agg.labels(**common).set(1 if result.reachable else 0)
        self.latency.labels(**common).observe(result.latency_seconds)

        check = result.service_check
        for reason in (REASON_PORT_NOT_PUBLISHED, REASON_SERVICE_MISSING):
            hit = check.checked and check.misconfigured and check.reason == reason
            self.misconfigured.labels(**common, reason=reason).set(1 if hit else 0)

        if not result.reachable:
            if check.checked and check.misconfigured:
                # Do NOT file this under transport errors: it is a config
                # fault, and conflating the two is what produced the wrong
                # diagnosis in the first place.
                error_class = check.reason or "misconfigured"
            elif result.status_code is not None:
                error_class = "status_mismatch"
            else:
                error_class = (result.error or "unknown").split(":", 1)[0]
            self.errors.labels(**common, error_class=error_class).inc()


def run_once(
    targets: list[Target],
    metrics: Metrics,
    timeout: float,
    service_lookup: ServicePortLookup | None = None,
) -> list[ProbeResult]:
    """Probe every target sequentially, record metrics, log warnings.

    Sequential is fine: target list is small (~5–20), interval is 60s,
    individual probes timeout in 5s. Worst case 100s on a fully-blocked list,
    well within the 60s scrape interval if the next iteration overlaps —
    Prometheus tolerates stale samples.

    When `service_lookup` is supplied, each target's dialed port is compared
    against its Service's published ports (TTL-cached) so a failure can be
    reported as `probe_misconfigured` rather than `probe_blocked`.
    """
    results: list[ProbeResult] = []
    any_check_unavailable = False
    for target in targets:
        result = probe_target(target, timeout=timeout)
        if service_lookup is not None:
            try:
                result.service_check = service_lookup.check(target)
            except Exception as exc:  # never let the cross-check break probing
                result.service_check = ServicePortCheck(
                    checked=False,
                    misconfigured=False,
                    error=f"{type(exc).__name__}: {exc}",
                )
            if not result.service_check.checked:
                any_check_unavailable = True
        else:
            any_check_unavailable = True

        metrics.record(result)

        check = result.service_check
        if check.checked and check.misconfigured:
            # Emitted whether or not the probe itself failed: a target that
            # dials an unpublished port is broken even while something else
            # keeps it green.
            LOG.error(
                json.dumps(
                    {
                        "event": "probe_misconfigured",
                        "target": target.name,
                        "url": target.url,
                        "namespace": target.namespace,
                        "service": target.service,
                        "dialed_port": target.port,
                        "published_ports": list(check.published_ports),
                        "reason": check.reason,
                        "hint": (
                            "This is NOT a NetworkPolicy problem. Service "
                            f"{target.namespace}/{target.service} does not publish "
                            f"port {target.port}"
                            + (
                                f" (it publishes {list(check.published_ports)})"
                                if check.published_ports
                                else " (Service not found)"
                            )
                            + ". Fix the target in the cloudflared-probe-targets "
                            "ConfigMap, or publish the port on the Service in its "
                            "own repo. Dialing a Service port that does not exist "
                            "times out exactly like a CNI drop."
                        ),
                    }
                )
            )

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
        elif check.checked and check.misconfigured:
            # Already reported above as probe_misconfigured. Emitting
            # probe_blocked too would re-assert the wrong diagnosis.
            pass
        else:
            if check.checked:
                hint = (
                    "cloudflared CANNOT reach this Service either. The Service "
                    f"does publish port {target.port} "
                    f"(published: {list(check.published_ports)}), so this is a "
                    "reachability fault: check NetworkPolicy / CNI / endpoints."
                )
            else:
                hint = (
                    "cloudflared CANNOT reach this Service either — check "
                    "NetworkPolicy / CNI. NOTE: the Service port cross-check "
                    "did not run ("
                    + (check.error or "disabled")
                    + "), so a target dialing an unpublished Service port would "
                    "look identical to this. Verify with: kubectl get svc "
                    f"{target.service} -n {target.namespace} -o jsonpath='{{.spec.ports}}'"
                )
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
                        "service_port_check": "ok" if check.checked else "unavailable",
                        "hint": hint,
                    }
                )
            )
        results.append(result)
    metrics.service_check_unavailable.set(1 if any_check_unavailable else 0)
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
    # Opt-out exists so the probe still runs in environments without the
    # ClusterRole (local dev, a cluster where RBAC has not been applied yet).
    service_port_check = os.environ.get("SERVICE_PORT_CHECK", "true").lower() not in (
        "false",
        "0",
        "no",
    )
    service_cache_ttl = float(
        os.environ.get("SERVICE_CACHE_TTL_SECONDS", str(DEFAULT_SERVICE_CACHE_TTL_SECONDS))
    )

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
                "service_port_check": service_port_check,
            }
        )
    )

    metrics = Metrics()
    start_http_server(metrics_port, registry=metrics.registry)

    service_lookup = (
        ServicePortLookup(timeout=timeout, cache_ttl=service_cache_ttl)
        if service_port_check
        else None
    )

    while True:
        loop_start = time.monotonic()
        try:
            run_once(targets, metrics, timeout=timeout, service_lookup=service_lookup)
        except Exception as exc:  # never crash the loop on a single iteration
            LOG.exception(json.dumps({"event": "probe_loop_error", "error": str(exc)}))
        elapsed = time.monotonic() - loop_start
        sleep_for = max(0.0, interval - elapsed)
        time.sleep(sleep_for)


if __name__ == "__main__":
    sys.exit(main())
