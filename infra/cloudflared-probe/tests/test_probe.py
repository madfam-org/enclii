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
