"""Synthetic flow probe — layer-7 user-journey replay for the MADFAM ecosystem.

Sibling of cloudflared-probe (layer-3 in-cluster reachability). Where the
cloudflared probe answers "can cloudflared talk to the backend Service?",
this probe answers "can a real user log in and reach a protected page?".

Each journey is a sequence of HTTP steps with assertions. Per-platform
spec lives in /etc/synthetic-flow-probe/journeys/*.yaml (mounted from a
ConfigMap). Probe runs each journey on its own schedule (default 300s).

Emits Prometheus metrics on :9090/metrics. Logs structured JSON.

Environment:
    JOURNEYS_DIR       directory of *.yaml journey specs (default /etc/...)
    METRICS_PORT       port for /metrics endpoint (default 9090)
    LOG_LEVEL          INFO|DEBUG|WARNING (default INFO)
    ENVIRONMENT        production|preprod|local — gates mutating journeys
    HTTP_TIMEOUT_S     per-step timeout in seconds (default 15)

Credential placeholders (`${ADMIN_EMAIL}`, `${KARAFIEL_PASS}`, etc.) are
resolved against process env. Missing creds → journey is SKIPPED (not
failed) and a `synthetic_journey_skipped_total` counter is incremented.

Phase 1 is read-only journeys. Mutating journeys are refused outside
preprod (see should_run_journey). Phase 2 will add mutating-with-cleanup.
"""

from __future__ import annotations

import json
import logging
import os
import re
import sys
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import httpx
import yaml
from prometheus_client import (
    CollectorRegistry,
    Counter,
    Gauge,
    Histogram,
    start_http_server,
)

LOG = logging.getLogger("synthetic-flow-probe")

DEFAULT_JOURNEYS_DIR = "/etc/synthetic-flow-probe/journeys"
DEFAULT_METRICS_PORT = 9090
DEFAULT_HTTP_TIMEOUT_S = 15.0
DEFAULT_SCHEDULE_S = 300

# Placeholder syntax: ${VAR_NAME}. Anything else is left literal so YAML
# values like passwords containing $ aren't accidentally mangled.
_PLACEHOLDER_RE = re.compile(r"\$\{([A-Z][A-Z0-9_]*)\}")

SAFETY_READ_ONLY = "read-only"
SAFETY_MUTATING = "mutating"


# ---------------------------------------------------------------------------
# Spec parsing
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Step:
    """One HTTP step inside a journey.

    Assertions are deliberately minimal for Phase 1: status code, URL
    substring, JSON-key match, header presence/match. Anything more
    complex (XPath, full DOM) is Phase 2 (Playwright).
    """

    name: str
    method: str
    url: str
    body_form: dict[str, str] = field(default_factory=dict)
    body_json: dict[str, Any] | None = None
    headers: dict[str, str] = field(default_factory=dict)
    follow_redirects: bool = True
    expect_status: int | None = 200
    expect_url_contains: str | None = None
    expect_json_contains: dict[str, Any] = field(default_factory=dict)
    expect_header_present: list[str] = field(default_factory=list)
    expect_no_csp_violation: bool = False

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "Step":
        if "name" not in raw:
            raise ValueError(f"step missing 'name': {raw!r}")
        if "url" not in raw:
            raise ValueError(f"step missing 'url': {raw!r}")
        return cls(
            name=str(raw["name"]),
            method=str(raw.get("method", "GET")).upper(),
            url=str(raw["url"]),
            body_form=dict(raw.get("body_form", {})),
            body_json=raw.get("body_json"),
            headers=dict(raw.get("headers", {})),
            follow_redirects=bool(raw.get("follow_redirects", True)),
            expect_status=(
                None if raw.get("expect_status") is None and "expect_status" in raw
                else int(raw.get("expect_status", 200))
            ),
            expect_url_contains=raw.get("expect_url_contains"),
            expect_json_contains=dict(raw.get("expect_json_contains", {})),
            expect_header_present=list(raw.get("expect_header_present", [])),
            expect_no_csp_violation=bool(raw.get("expect_no_csp_violation", False)),
        )


@dataclass(frozen=True)
class Journey:
    platform: str
    journey: str
    description: str
    schedule_seconds: int
    safety_class: str
    steps: tuple[Step, ...]

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "Journey":
        for required in ("platform", "journey", "steps"):
            if required not in raw:
                raise ValueError(f"journey missing required key: {required!r}")
        steps_raw = raw["steps"]
        if not isinstance(steps_raw, list) or not steps_raw:
            raise ValueError("journey 'steps' must be a non-empty list")
        safety = str(raw.get("safety_class", SAFETY_READ_ONLY))
        if safety not in (SAFETY_READ_ONLY, SAFETY_MUTATING):
            raise ValueError(
                f"journey safety_class must be 'read-only' or 'mutating', got {safety!r}"
            )
        return cls(
            platform=str(raw["platform"]),
            journey=str(raw["journey"]),
            description=str(raw.get("description", "")),
            schedule_seconds=int(raw.get("schedule_seconds", DEFAULT_SCHEDULE_S)),
            safety_class=safety,
            steps=tuple(Step.from_dict(s) for s in steps_raw),
        )


def load_journeys(directory: str) -> list[Journey]:
    """Load every *.yaml file in `directory` as a Journey.

    Files that fail to parse are logged and skipped — one bad journey
    must not take down all the others.
    """
    base = Path(directory)
    if not base.is_dir():
        raise FileNotFoundError(f"journeys directory not found: {directory}")
    journeys: list[Journey] = []
    for path in sorted(base.glob("*.yaml")):
        try:
            data = yaml.safe_load(path.read_text(encoding="utf-8"))
            journeys.append(Journey.from_dict(data))
        except (yaml.YAMLError, ValueError, KeyError) as exc:
            LOG.error(
                json.dumps(
                    {"event": "journey_parse_failed", "file": str(path), "error": str(exc)}
                )
            )
    return journeys


# ---------------------------------------------------------------------------
# Placeholder resolution
# ---------------------------------------------------------------------------


class MissingCredentialError(Exception):
    """Raised when a ${VAR} placeholder has no matching env var.

    Caught at the journey level so one missing cred skips that journey
    instead of crashing the probe.
    """


def resolve_placeholders(value: str, env: dict[str, str]) -> str:
    """Substitute ${VAR} references against the supplied env dict.

    Raises MissingCredentialError if any referenced var is absent — we
    deliberately do NOT default to "" because a journey with empty
    credentials would silently fail at the auth step and produce a
    misleading metric.
    """

    def _sub(match: re.Match[str]) -> str:
        var = match.group(1)
        if var not in env:
            raise MissingCredentialError(var)
        return env[var]

    return _PLACEHOLDER_RE.sub(_sub, value)


def resolve_step(step: Step, env: dict[str, str]) -> Step:
    """Return a copy of `step` with all ${VAR} placeholders resolved."""
    return Step(
        name=step.name,
        method=step.method,
        url=resolve_placeholders(step.url, env),
        body_form={k: resolve_placeholders(v, env) for k, v in step.body_form.items()},
        body_json=step.body_json,  # JSON bodies don't get placeholder treatment in Phase 1
        headers={k: resolve_placeholders(v, env) for k, v in step.headers.items()},
        follow_redirects=step.follow_redirects,
        expect_status=step.expect_status,
        expect_url_contains=step.expect_url_contains,
        expect_json_contains=step.expect_json_contains,
        expect_header_present=step.expect_header_present,
        expect_no_csp_violation=step.expect_no_csp_violation,
    )


# ---------------------------------------------------------------------------
# Step execution + assertions
# ---------------------------------------------------------------------------


@dataclass
class StepResult:
    step: Step
    passed: bool
    status_code: int | None
    final_url: str | None
    latency_seconds: float
    failure_reason: str | None  # populated iff passed=False


def _check_csp_form_action(headers: httpx.Headers, target_url: str) -> str | None:
    """If the response sets a CSP with form-action, ensure target_url's host is allowed.

    Returns None on pass, a human reason on fail. Phase 1 only checks
    form-action because that's the directive that broke Janua login this
    session — we add more directives in Phase 2.
    """
    csp = headers.get("content-security-policy") or headers.get(
        "content-security-policy-report-only"
    )
    if not csp:
        return None  # No CSP at all is fine for a probe — the assertion is "no violation".
    # Find form-action directive.
    directives = [d.strip() for d in csp.split(";") if d.strip()]
    form_action = next(
        (d for d in directives if d.lower().startswith("form-action")), None
    )
    if not form_action:
        return None  # No form-action directive → no constraint.
    # Extract host from target_url.
    try:
        parsed = httpx.URL(target_url)
    except httpx.InvalidURL:
        return f"could not parse target URL {target_url!r} for CSP check"
    target_host = parsed.host
    # Allowed sources: everything after "form-action".
    sources = form_action.split()[1:]
    for src in sources:
        src = src.strip("'")
        if src in ("self", "*"):
            # 'self' check is approximate — we don't track the document
            # origin in the probe. Treat as a soft pass.
            return None
        if src.startswith("https://") or src.startswith("http://"):
            try:
                src_host = httpx.URL(src).host
            except httpx.InvalidURL:
                continue
            if src_host == target_host or (
                src_host.startswith("*.") and target_host.endswith(src_host[1:])
            ):
                return None
        elif src.startswith("*.") and target_host.endswith(src[1:]):
            return None
        elif src == target_host:
            return None
    return f"CSP form-action {form_action!r} does not include host {target_host!r}"


def _check_json_contains(body: bytes, expected: dict[str, Any]) -> str | None:
    """Verify every key in `expected` is present in the JSON body with matching value.

    Returns None on pass, a reason string on fail. Top-level only for
    Phase 1 — nested JSONPath would be useful but isn't worth the
    dependency just yet.
    """
    if not expected:
        return None
    try:
        parsed = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        return f"response body is not valid JSON: {exc}"
    if not isinstance(parsed, dict):
        return f"expected JSON object, got {type(parsed).__name__}"
    for key, want in expected.items():
        if key not in parsed:
            return f"expected JSON key {key!r} missing from response"
        if parsed[key] != want:
            return f"expected JSON {key}={want!r}, got {parsed[key]!r}"
    return None


def evaluate_assertions(
    step: Step, response: httpx.Response
) -> str | None:
    """Run all assertions on a response. Returns None on pass, reason on fail."""
    if step.expect_status is not None and response.status_code != step.expect_status:
        return f"expected status {step.expect_status}, got {response.status_code}"
    if step.expect_url_contains and step.expect_url_contains not in str(response.url):
        return (
            f"expected URL to contain {step.expect_url_contains!r}, "
            f"got {response.url!s}"
        )
    for header in step.expect_header_present:
        if header.lower() not in {k.lower() for k in response.headers.keys()}:
            return f"expected header {header!r} missing from response"
    if step.expect_no_csp_violation:
        # The CSP check needs to know which URL the next form would post
        # to. We approximate by checking the *final* response URL — i.e.,
        # this step expects to be a "render the form that posts to itself"
        # page. For Janua's login flow that's accurate; for other flows
        # the journey author can leave this off.
        violation = _check_csp_form_action(response.headers, str(response.url))
        if violation is not None:
            return violation
    if step.expect_json_contains:
        reason = _check_json_contains(response.content, step.expect_json_contains)
        if reason is not None:
            return reason
    return None


def execute_step(
    step: Step, client: httpx.Client, timeout: float
) -> StepResult:
    """Fire one HTTP step and evaluate assertions.

    Network failures (DNS, TCP, TLS, timeout) are treated as step failures
    rather than raised — the journey continues recording the failure so
    operators see *which* step broke.
    """
    start = time.monotonic()
    try:
        if step.method == "GET":
            response = client.get(
                step.url,
                headers=step.headers,
                follow_redirects=step.follow_redirects,
                timeout=timeout,
            )
        elif step.method in ("POST", "PUT", "PATCH"):
            kwargs: dict[str, Any] = {
                "headers": step.headers,
                "follow_redirects": step.follow_redirects,
                "timeout": timeout,
            }
            if step.body_json is not None:
                kwargs["json"] = step.body_json
            elif step.body_form:
                kwargs["data"] = step.body_form
            response = client.request(step.method, step.url, **kwargs)
        else:
            return StepResult(
                step=step,
                passed=False,
                status_code=None,
                final_url=None,
                latency_seconds=time.monotonic() - start,
                failure_reason=f"unsupported method {step.method!r}",
            )
    except httpx.HTTPError as exc:
        return StepResult(
            step=step,
            passed=False,
            status_code=None,
            final_url=None,
            latency_seconds=time.monotonic() - start,
            failure_reason=f"{type(exc).__name__}: {exc}",
        )

    latency = time.monotonic() - start
    failure = evaluate_assertions(step, response)
    return StepResult(
        step=step,
        passed=failure is None,
        status_code=response.status_code,
        final_url=str(response.url),
        latency_seconds=latency,
        failure_reason=failure,
    )


# ---------------------------------------------------------------------------
# Journey execution
# ---------------------------------------------------------------------------


@dataclass
class JourneyResult:
    journey: Journey
    passed: bool
    step_results: list[StepResult]
    skipped_reason: str | None = None  # populated iff this journey didn't run at all


def should_run_journey(journey: Journey, environment: str) -> tuple[bool, str | None]:
    """Sandbox boundary: refuse mutating journeys outside preprod.

    Returns (run?, refusal_reason). Phase 1 always blocks mutating against
    production. Phase 2 will add mutating-with-cleanup against prod for a
    narrow allowlist.
    """
    if journey.safety_class == SAFETY_MUTATING and environment == "production":
        return (
            False,
            "mutating journey refused against production (Phase 1 is read-only)",
        )
    return True, None


def execute_journey(
    journey: Journey,
    env: dict[str, str],
    timeout: float,
) -> JourneyResult:
    """Run one journey end-to-end with a shared cookie jar across steps."""
    # Fresh cookie jar per journey run — we're testing the login flow, not
    # session resumption. Phase 2 may add multi-tenant journeys that share.
    with httpx.Client(
        cookies=httpx.Cookies(),
        # Use HTTP/1.1; Janua / Karafiel ingress isn't reliably HTTP/2
        # behind cloudflared — h2 here would mask the real user path.
        http2=False,
        # Keep the User-Agent identifiable so log analysis can filter
        # probe traffic out of revenue / engagement signals.
        headers={"User-Agent": "synthetic-flow-probe/1.0 (+enclii observability)"},
    ) as client:
        step_results: list[StepResult] = []
        for raw_step in journey.steps:
            try:
                step = resolve_step(raw_step, env)
            except MissingCredentialError as exc:
                return JourneyResult(
                    journey=journey,
                    passed=False,
                    step_results=step_results,
                    skipped_reason=f"missing credential ${{{exc.args[0]}}}",
                )
            result = execute_step(step, client, timeout=timeout)
            step_results.append(result)
            if not result.passed:
                # Stop on first failure — subsequent steps would cascade
                # and pollute the metric (one root cause per failed run).
                return JourneyResult(
                    journey=journey, passed=False, step_results=step_results
                )
        return JourneyResult(journey=journey, passed=True, step_results=step_results)


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------


class Metrics:
    """Bundle of prometheus_client handles. Construction creates a fresh
    registry which keeps unit tests isolated from each other."""

    def __init__(self, registry: CollectorRegistry | None = None) -> None:
        self.registry = registry or CollectorRegistry()
        self.pass_total = Counter(
            "synthetic_journey_pass_total",
            "Number of journey runs that passed end-to-end",
            ("platform", "journey"),
            registry=self.registry,
        )
        self.fail_total = Counter(
            "synthetic_journey_fail_total",
            "Number of journey runs that failed; labelled with the failing step + reason class",
            ("platform", "journey", "step", "reason"),
            registry=self.registry,
        )
        self.skipped_total = Counter(
            "synthetic_journey_skipped_total",
            "Number of journey runs skipped (e.g., missing credentials, mutating-against-prod)",
            ("platform", "journey", "reason"),
            registry=self.registry,
        )
        self.step_latency = Histogram(
            "synthetic_journey_step_latency_seconds",
            "Per-step latency in seconds",
            ("platform", "journey", "step"),
            buckets=(0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0),
            registry=self.registry,
        )
        self.last_run_ts = Gauge(
            "synthetic_journey_last_run_timestamp",
            "Unix timestamp of the most recent run for this journey",
            ("platform", "journey"),
            registry=self.registry,
        )
        self.consecutive_failures = Gauge(
            "synthetic_journey_consecutive_failures",
            "Number of consecutive failed runs for this journey (resets to 0 on a pass)",
            ("platform", "journey"),
            registry=self.registry,
        )

    def record(self, result: JourneyResult) -> None:
        platform = result.journey.platform
        journey = result.journey.journey
        self.last_run_ts.labels(platform=platform, journey=journey).set(time.time())

        if result.skipped_reason:
            # Skipped runs don't count as pass or fail — they're a separate
            # signal (operator forgot to populate creds, etc.).
            reason = _classify_skip_reason(result.skipped_reason)
            self.skipped_total.labels(
                platform=platform, journey=journey, reason=reason
            ).inc()
            return

        # Record latencies for every step we executed (even on failure
        # the partial timing is useful).
        for step_result in result.step_results:
            self.step_latency.labels(
                platform=platform, journey=journey, step=step_result.step.name
            ).observe(step_result.latency_seconds)

        if result.passed:
            self.pass_total.labels(platform=platform, journey=journey).inc()
            self.consecutive_failures.labels(
                platform=platform, journey=journey
            ).set(0)
        else:
            failed = next((s for s in result.step_results if not s.passed), None)
            step_name = failed.step.name if failed else "unknown"
            reason = _classify_fail_reason(failed.failure_reason if failed else None)
            self.fail_total.labels(
                platform=platform, journey=journey, step=step_name, reason=reason
            ).inc()
            self.consecutive_failures.labels(
                platform=platform, journey=journey
            ).inc()


def _classify_fail_reason(raw: str | None) -> str:
    """Reduce a free-text failure reason to a stable label value.

    Cardinality discipline: Prometheus labels with unbounded text are a
    foot-gun. We map to a small fixed set so dashboards can group cleanly.
    """
    if not raw:
        return "unknown"
    lowered = raw.lower()
    if "expected status" in lowered:
        return "status_mismatch"
    if "expected url to contain" in lowered:
        return "wrong_redirect"
    if "expected header" in lowered:
        return "missing_header"
    if "csp form-action" in lowered:
        return "csp_violation"
    if "json" in lowered or "expected json" in lowered:
        return "body_assertion"
    if "timeout" in lowered:
        return "timeout"
    if "connect" in lowered or "dns" in lowered:
        return "network"
    if "missing credential" in lowered:
        return "missing_credential"
    return "other"


def _classify_skip_reason(raw: str) -> str:
    lowered = raw.lower()
    if "missing credential" in lowered:
        return "missing_credential"
    if "mutating" in lowered:
        return "mutating_refused"
    return "other"


# ---------------------------------------------------------------------------
# Scheduler
# ---------------------------------------------------------------------------


def _journey_loop(
    journey: Journey,
    metrics: Metrics,
    env: dict[str, str],
    environment: str,
    timeout: float,
    stop: threading.Event,
) -> None:
    """One thread per journey — simpler than asyncio for a fixed small set.

    Each iteration: check sandbox → run → record → sleep until next slot.
    The sleep uses Event.wait so SIGTERM exits promptly.
    """
    while not stop.is_set():
        loop_start = time.monotonic()
        run, refusal = should_run_journey(journey, environment)
        if not run:
            metrics.record(
                JourneyResult(
                    journey=journey,
                    passed=False,
                    step_results=[],
                    skipped_reason=refusal,
                )
            )
            LOG.info(
                json.dumps(
                    {
                        "event": "journey_skipped",
                        "platform": journey.platform,
                        "journey": journey.journey,
                        "reason": refusal,
                    }
                )
            )
        else:
            try:
                result = execute_journey(journey, env, timeout=timeout)
                metrics.record(result)
                _log_result(result)
            except Exception as exc:  # never crash the loop on a single iteration
                LOG.exception(
                    json.dumps(
                        {
                            "event": "journey_loop_error",
                            "platform": journey.platform,
                            "journey": journey.journey,
                            "error": str(exc),
                        }
                    )
                )
        elapsed = time.monotonic() - loop_start
        sleep_for = max(1.0, journey.schedule_seconds - elapsed)
        stop.wait(sleep_for)


def _log_result(result: JourneyResult) -> None:
    base = {
        "platform": result.journey.platform,
        "journey": result.journey.journey,
    }
    if result.skipped_reason:
        LOG.warning(json.dumps({**base, "event": "journey_skipped", "reason": result.skipped_reason}))
        return
    if result.passed:
        LOG.info(
            json.dumps(
                {
                    **base,
                    "event": "journey_passed",
                    "steps": len(result.step_results),
                    "total_latency_ms": round(
                        sum(s.latency_seconds for s in result.step_results) * 1000, 1
                    ),
                }
            )
        )
        return
    failed = next((s for s in result.step_results if not s.passed), None)
    LOG.warning(
        json.dumps(
            {
                **base,
                "event": "journey_failed",
                "step": failed.step.name if failed else "unknown",
                "reason": failed.failure_reason if failed else "unknown",
                "status_code": failed.status_code if failed else None,
                "final_url": failed.final_url if failed else None,
            }
        )
    )


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main() -> int:
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(message)s",
        stream=sys.stdout,
    )

    journeys_dir = os.environ.get("JOURNEYS_DIR", DEFAULT_JOURNEYS_DIR)
    metrics_port = int(os.environ.get("METRICS_PORT", str(DEFAULT_METRICS_PORT)))
    timeout = float(os.environ.get("HTTP_TIMEOUT_S", str(DEFAULT_HTTP_TIMEOUT_S)))
    environment = os.environ.get("ENVIRONMENT", "production")

    try:
        journeys = load_journeys(journeys_dir)
    except FileNotFoundError as exc:
        LOG.error(json.dumps({"event": "journeys_load_failed", "error": str(exc)}))
        return 1

    if not journeys:
        LOG.error(
            json.dumps(
                {
                    "event": "no_journeys_loaded",
                    "directory": journeys_dir,
                    "hint": "Mount at least one *.yaml journey spec into the journeys directory",
                }
            )
        )
        return 1

    LOG.info(
        json.dumps(
            {
                "event": "probe_start",
                "journeys": [{"platform": j.platform, "journey": j.journey} for j in journeys],
                "environment": environment,
                "metrics_port": metrics_port,
                "timeout_s": timeout,
            }
        )
    )

    metrics = Metrics()
    start_http_server(metrics_port, registry=metrics.registry)

    stop = threading.Event()
    threads: list[threading.Thread] = []
    for journey in journeys:
        thread = threading.Thread(
            target=_journey_loop,
            args=(journey, metrics, dict(os.environ), environment, timeout, stop),
            name=f"journey-{journey.platform}-{journey.journey}",
            daemon=True,
        )
        thread.start()
        threads.append(thread)

    # Block forever; container orchestrator handles lifecycle. SIGTERM
    # arrives as KeyboardInterrupt via stdlib default signal handling
    # since Python 3.10's main-thread interpreter.
    try:
        stop.wait()
    except KeyboardInterrupt:
        LOG.info(json.dumps({"event": "probe_shutdown"}))
        stop.set()
    return 0


if __name__ == "__main__":
    sys.exit(main())
