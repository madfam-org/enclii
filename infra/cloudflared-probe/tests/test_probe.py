"""Unit tests for the cloudflared probe target-evaluation logic.

Network calls are stubbed via a fake `requests.get` so the suite runs offline.
"""

from __future__ import annotations

import json
from typing import Any

import pytest
import requests

import probe as probe_mod
from probe import Metrics, ProbeResult, Target, load_targets, probe_target, run_once


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


class _FakeResponse:
    def __init__(self, status_code: int) -> None:
        self.status_code = status_code


def _target(**overrides: Any) -> Target:
    base: dict[str, Any] = {
        "name": "karafiel-api",
        "url": "http://karafiel-api.karafiel.svc.cluster.local:8000/health",
        "expected_status": 200,
        "namespace": "karafiel",
        "service": "karafiel-api",
        "port": 8000,
    }
    base.update(overrides)
    return Target.from_dict(base)


# ---------------------------------------------------------------------------
# load_targets
# ---------------------------------------------------------------------------


def test_load_targets_accepts_bare_list(tmp_path):
    path = tmp_path / "targets.json"
    path.write_text(
        json.dumps(
            [
                {
                    "name": "a",
                    "url": "http://a.svc.cluster.local/health",
                    "namespace": "ns-a",
                    "service": "a",
                    "port": 80,
                }
            ]
        )
    )
    targets = load_targets(str(path))
    assert len(targets) == 1
    assert targets[0].name == "a"
    assert targets[0].expected_status == 200  # default


def test_load_targets_accepts_wrapped_object(tmp_path):
    path = tmp_path / "targets.json"
    path.write_text(
        json.dumps(
            {
                "targets": [
                    {
                        "name": "b",
                        "url": "http://b.svc/health",
                        "namespace": "ns-b",
                        "service": "b",
                        "port": 8080,
                        "expected_status": 204,
                    }
                ]
            }
        )
    )
    targets = load_targets(str(path))
    assert len(targets) == 1
    assert targets[0].expected_status == 204
    assert targets[0].port == 8080


def test_load_targets_rejects_missing_keys(tmp_path):
    path = tmp_path / "targets.json"
    # missing 'service'
    path.write_text(json.dumps([{"name": "x", "url": "http://x", "namespace": "n", "port": 80}]))
    with pytest.raises(ValueError):
        load_targets(str(path))


# ---------------------------------------------------------------------------
# probe_target
# ---------------------------------------------------------------------------


def test_probe_target_reachable_on_expected_status(monkeypatch):
    monkeypatch.setattr(probe_mod.requests, "get", lambda *a, **kw: _FakeResponse(200))
    result = probe_target(_target(), timeout=1.0)
    assert result.reachable is True
    assert result.status_code == 200
    assert result.error is None


def test_probe_target_blocked_on_status_mismatch(monkeypatch):
    monkeypatch.setattr(probe_mod.requests, "get", lambda *a, **kw: _FakeResponse(502))
    result = probe_target(_target(), timeout=1.0)
    assert result.reachable is False
    assert result.status_code == 502
    assert "502" in (result.error or "")


def test_probe_target_blocked_on_connection_error(monkeypatch):
    def raise_conn(*_a, **_kw):
        raise requests.ConnectionError("connection refused")

    monkeypatch.setattr(probe_mod.requests, "get", raise_conn)
    result = probe_target(_target(), timeout=1.0)
    assert result.reachable is False
    assert result.status_code is None
    assert "ConnectionError" in (result.error or "")


def test_probe_target_blocked_on_timeout(monkeypatch):
    def raise_timeout(*_a, **_kw):
        raise requests.Timeout("timed out")

    monkeypatch.setattr(probe_mod.requests, "get", raise_timeout)
    result = probe_target(_target(), timeout=0.5)
    assert result.reachable is False
    assert result.status_code is None
    assert "Timeout" in (result.error or "")


def test_probe_target_respects_custom_expected_status(monkeypatch):
    monkeypatch.setattr(probe_mod.requests, "get", lambda *a, **kw: _FakeResponse(204))
    result = probe_target(_target(expected_status=204), timeout=1.0)
    assert result.reachable is True


def test_probe_target_disables_proxy(monkeypatch):
    captured: dict[str, Any] = {}

    def capture(*args, **kwargs):
        captured.update(kwargs)
        return _FakeResponse(200)

    monkeypatch.setattr(probe_mod.requests, "get", capture)
    probe_target(_target(), timeout=1.0)
    # Probe must NOT route through HTTP_PROXY / HTTPS_PROXY — the whole point
    # is to test the in-cluster path that cloudflared would actually take.
    assert captured.get("proxies") == {"http": "", "https": ""}


# ---------------------------------------------------------------------------
# Metrics + run_once integration
# ---------------------------------------------------------------------------


def _gauge_value(metric_family_iter, metric_name: str, label_match: dict[str, str]) -> float | None:
    """Walk the prometheus_client registry output and pluck a single sample."""
    for family in metric_family_iter:
        if family.name != metric_name:
            continue
        for sample in family.samples:
            if all(sample.labels.get(k) == v for k, v in label_match.items()):
                return sample.value
    return None


def test_metrics_record_sets_gauges_on_reachable():
    m = Metrics()
    result = ProbeResult(
        target=_target(),
        reachable=True,
        status_code=200,
        latency_seconds=0.042,
        error=None,
    )
    m.record(result)
    samples = list(m.registry.collect())
    val = _gauge_value(
        samples,
        "cloudflared_probe_reachable",
        {"namespace": "karafiel", "service": "karafiel-api", "port": "8000", "name": "karafiel-api"},
    )
    assert val == 1.0
    agg = _gauge_value(
        samples,
        "cloudflared_probe_reachable_total",
        {"namespace": "karafiel", "service": "karafiel-api", "port": "8000"},
    )
    assert agg == 1.0


def test_metrics_record_sets_gauges_on_blocked():
    m = Metrics()
    result = ProbeResult(
        target=_target(),
        reachable=False,
        status_code=None,
        latency_seconds=5.0,
        error="ConnectionError: connection refused",
    )
    m.record(result)
    samples = list(m.registry.collect())
    agg = _gauge_value(
        samples,
        "cloudflared_probe_reachable_total",
        {"namespace": "karafiel", "service": "karafiel-api", "port": "8000"},
    )
    assert agg == 0.0


def test_run_once_visits_every_target(monkeypatch):
    visited: list[str] = []

    def fake_get(url, **_kw):
        visited.append(url)
        return _FakeResponse(200)

    monkeypatch.setattr(probe_mod.requests, "get", fake_get)
    targets = [
        _target(name="a", url="http://a.svc/health", service="a"),
        _target(name="b", url="http://b.svc/health", service="b"),
        _target(name="c", url="http://c.svc/health", service="c"),
    ]
    m = Metrics()
    results = run_once(targets, m, timeout=1.0)
    assert [r.target.name for r in results] == ["a", "b", "c"]
    assert visited == ["http://a.svc/health", "http://b.svc/health", "http://c.svc/health"]
    assert all(r.reachable for r in results)


# ---------------------------------------------------------------------------
# Service port cross-check (the 2026-08-06 Fault 2 control)
#
# On 2026-08-06 the probe dialed dhanam-api:3000 and janua-api:8080 — ports
# those Services do not publish (both publish 80). The dial timed out, and the
# probe reported `probe_blocked` with the hint "check NetworkPolicy / CNI".
# These tests pin the corrected behaviour: a Service that exists but does not
# publish the dialed port must be reported as a MISCONFIGURATION, and a check
# that could not run must never be reported as a clean bill of health.
# ---------------------------------------------------------------------------


class _FakeJSONResponse:
    def __init__(self, status_code: int, body: dict | None = None) -> None:
        self.status_code = status_code
        self._body = body or {}

    def json(self):
        return self._body


class _FakeSession:
    """Stands in for `requests` inside ServicePortLookup."""

    def __init__(self, response=None, exc: Exception | None = None) -> None:
        self.response = response
        self.exc = exc
        self.calls: list[str] = []

    def get(self, url, **_kw):
        self.calls.append(url)
        if self.exc is not None:
            raise self.exc
        return self.response


def _svc_body(*ports: int) -> dict:
    return {"spec": {"ports": [{"port": p, "targetPort": p} for p in ports]}}


def _lookup(session, tmp_path, **kw):
    token = tmp_path / "token"
    token.write_text("fake-token")
    return probe_mod.ServicePortLookup(
        api_host="10.43.0.1",
        api_port="443",
        token_path=str(token),
        ca_path=str(tmp_path / "absent-ca.crt"),
        session=session,
        **kw,
    )


def test_evaluate_port_flags_the_exact_fault_2_config():
    """dhanam-api dialed on 3000 while the Service publishes only 80."""
    target = _target(
        name="dhanam-api",
        url="http://dhanam-api.dhanam.svc.cluster.local:3000/health",
        namespace="dhanam",
        service="dhanam-api",
        port=3000,
    )
    verdict = probe_mod.evaluate_port(
        target,
        probe_mod.ServicePortCheck(
            checked=True, misconfigured=False, published_ports=(80,)
        ),
    )
    assert verdict.checked is True
    assert verdict.misconfigured is True
    assert verdict.reason == probe_mod.REASON_PORT_NOT_PUBLISHED
    assert verdict.published_ports == (80,)


def test_evaluate_port_accepts_a_published_port():
    target = _target(
        name="dhanam-api",
        url="http://dhanam-api.dhanam.svc.cluster.local:80/health",
        namespace="dhanam",
        service="dhanam-api",
        port=80,
    )
    verdict = probe_mod.evaluate_port(
        target,
        probe_mod.ServicePortCheck(
            checked=True, misconfigured=False, published_ports=(80, 3000)
        ),
    )
    assert verdict.misconfigured is False


def test_evaluate_port_passes_through_unchecked_verdicts():
    """An unverifiable target must stay unverified — never 'fine'."""
    unchecked = probe_mod.ServicePortCheck(
        checked=False, misconfigured=False, error="kube API returned 403"
    )
    verdict = probe_mod.evaluate_port(_target(), unchecked)
    assert verdict.checked is False
    assert verdict.error == "kube API returned 403"


def test_lookup_reads_published_ports(tmp_path):
    session = _FakeSession(_FakeJSONResponse(200, _svc_body(80, 4200)))
    lookup = _lookup(session, tmp_path)
    verdict = lookup.published_ports("enclii", "switchyard-api")
    assert verdict.checked is True
    assert verdict.published_ports == (80, 4200)
    assert session.calls == [
        "https://10.43.0.1:443/api/v1/namespaces/enclii/services/switchyard-api"
    ]


def test_lookup_reports_missing_service(tmp_path):
    lookup = _lookup(_FakeSession(_FakeJSONResponse(404)), tmp_path)
    verdict = lookup.published_ports("dhanam", "gone")
    assert verdict.checked is True
    assert verdict.misconfigured is True
    assert verdict.reason == probe_mod.REASON_SERVICE_MISSING


def test_lookup_treats_rbac_denial_as_unchecked(tmp_path):
    lookup = _lookup(_FakeSession(_FakeJSONResponse(403)), tmp_path)
    verdict = lookup.published_ports("dhanam", "dhanam-api")
    assert verdict.checked is False
    assert "403" in (verdict.error or "")


def test_lookup_treats_transport_error_as_unchecked(tmp_path):
    lookup = _lookup(_FakeSession(exc=requests.ConnectionError("no route")), tmp_path)
    verdict = lookup.published_ports("dhanam", "dhanam-api")
    assert verdict.checked is False
    assert "ConnectionError" in (verdict.error or "")


def test_lookup_without_token_is_unchecked(tmp_path):
    lookup = probe_mod.ServicePortLookup(
        api_host="10.43.0.1",
        token_path=str(tmp_path / "nope"),
        ca_path=str(tmp_path / "nope-ca"),
        session=_FakeSession(_FakeJSONResponse(200, _svc_body(80))),
    )
    verdict = lookup.published_ports("dhanam", "dhanam-api")
    assert verdict.checked is False
    assert "ServiceAccount token" in (verdict.error or "")


def test_lookup_caches_within_ttl(tmp_path):
    session = _FakeSession(_FakeJSONResponse(200, _svc_body(80)))
    lookup = _lookup(session, tmp_path, cache_ttl=300)
    target = _target(namespace="dhanam", service="dhanam-api", port=80)
    lookup.check(target, now=1000.0)
    lookup.check(target, now=1100.0)
    assert len(session.calls) == 1
    lookup.check(target, now=1400.0)
    assert len(session.calls) == 2


def test_run_once_reports_misconfigured_not_blocked(monkeypatch, tmp_path, caplog):
    """The regression: a timeout on an unpublished port is a config fault."""

    def _timeout(*_a, **_kw):
        raise requests.ConnectTimeout("timed out")

    monkeypatch.setattr(probe_mod.requests, "get", _timeout)
    target = _target(
        name="dhanam-api",
        url="http://dhanam-api.dhanam.svc.cluster.local:3000/health",
        namespace="dhanam",
        service="dhanam-api",
        port=3000,
    )
    lookup = _lookup(_FakeSession(_FakeJSONResponse(200, _svc_body(80))), tmp_path)
    m = Metrics()

    with caplog.at_level("WARNING"):
        results = run_once([target], m, timeout=1.0, service_lookup=lookup)

    assert results[0].reachable is False
    assert results[0].service_check.misconfigured is True

    events = [json.loads(r.message)["event"] for r in caplog.records]
    assert "probe_misconfigured" in events
    # The wrong diagnosis must not be emitted alongside the right one.
    assert "probe_blocked" not in events

    samples = list(m.registry.collect())
    assert (
        _gauge_value(
            samples,
            "cloudflared_probe_target_misconfigured",
            {
                "namespace": "dhanam",
                "service": "dhanam-api",
                "port": "3000",
                "reason": probe_mod.REASON_PORT_NOT_PUBLISHED,
            },
        )
        == 1.0
    )


def test_run_once_still_reports_blocked_when_port_is_published(
    monkeypatch, tmp_path, caplog
):
    """A real NetworkPolicy drop keeps its original diagnosis."""

    def _timeout(*_a, **_kw):
        raise requests.ConnectTimeout("timed out")

    monkeypatch.setattr(probe_mod.requests, "get", _timeout)
    target = _target(
        name="karafiel-api",
        url="http://karafiel-api.karafiel.svc.cluster.local:8000/health",
        namespace="karafiel",
        service="karafiel-api",
        port=8000,
    )
    lookup = _lookup(_FakeSession(_FakeJSONResponse(200, _svc_body(8000))), tmp_path)
    m = Metrics()

    with caplog.at_level("WARNING"):
        run_once([target], m, timeout=1.0, service_lookup=lookup)

    payloads = [json.loads(r.message) for r in caplog.records]
    blocked = [p for p in payloads if p["event"] == "probe_blocked"]
    assert len(blocked) == 1
    assert blocked[0]["service_port_check"] == "ok"
    assert "NetworkPolicy" in blocked[0]["hint"]


def test_run_once_marks_check_unavailable_when_lookup_absent(monkeypatch):
    """No lookup configured → the blind spot must be visible in metrics."""
    monkeypatch.setattr(probe_mod.requests, "get", lambda *_a, **_kw: _FakeResponse(200))
    m = Metrics()
    run_once([_target()], m, timeout=1.0, service_lookup=None)
    samples = list(m.registry.collect())
    assert _gauge_value(samples, "cloudflared_probe_service_check_unavailable", {}) == 1.0


def test_run_once_clears_check_unavailable_when_lookup_works(monkeypatch, tmp_path):
    monkeypatch.setattr(probe_mod.requests, "get", lambda *_a, **_kw: _FakeResponse(200))
    lookup = _lookup(_FakeSession(_FakeJSONResponse(200, _svc_body(8000))), tmp_path)
    m = Metrics()
    run_once([_target()], m, timeout=1.0, service_lookup=lookup)
    samples = list(m.registry.collect())
    assert _gauge_value(samples, "cloudflared_probe_service_check_unavailable", {}) == 0.0


def test_run_once_survives_a_lookup_that_raises(monkeypatch):
    """The cross-check must never take reachability probing down with it."""
    monkeypatch.setattr(probe_mod.requests, "get", lambda *_a, **_kw: _FakeResponse(200))

    class _Exploding:
        def check(self, _target):
            raise RuntimeError("boom")

    m = Metrics()
    results = run_once([_target()], m, timeout=1.0, service_lookup=_Exploding())
    assert results[0].reachable is True
    assert results[0].service_check.checked is False
