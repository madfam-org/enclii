"""Unit tests for the synthetic flow probe.

Network calls are stubbed via httpx.MockTransport so the suite runs offline.
We deliberately do NOT mock at the requests-library boundary like cloudflared-probe
does (it uses requests, we use httpx) — httpx ships with a first-class MockTransport
that lets us assert on the full request/response cycle.
"""

from __future__ import annotations

import json
from typing import Callable

import httpx
import pytest
import yaml

import probe as probe_mod
from probe import (
    Journey,
    Metrics,
    SAFETY_MUTATING,
    SAFETY_READ_ONLY,
    Step,
    _check_csp_form_action,
    _classify_fail_reason,
    evaluate_assertions,
    execute_journey,
    execute_step,
    load_journeys,
    resolve_placeholders,
    resolve_step,
    should_run_journey,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _step(**overrides) -> Step:
    base = {
        "name": "step1",
        "method": "GET",
        "url": "https://example.test/login",
        "expect_status": 200,
    }
    base.update(overrides)
    return Step.from_dict(base)


def _client(handler: Callable[[httpx.Request], httpx.Response]) -> httpx.Client:
    transport = httpx.MockTransport(handler)
    return httpx.Client(transport=transport, follow_redirects=True)


def _gauge_value(samples, metric_name, label_match):
    for family in samples:
        if family.name != metric_name:
            continue
        for sample in family.samples:
            if all(sample.labels.get(k) == v for k, v in label_match.items()):
                return sample.value
    return None


def _counter_total(samples, metric_name, label_match):
    """Sum every '_total' sample matching the labels — Counter exposes both
    `metric_name_total` and `metric_name_created` so we filter on suffix."""
    total = 0.0
    for family in samples:
        if family.name != metric_name:
            continue
        for sample in family.samples:
            if not sample.name.endswith("_total"):
                continue
            if all(sample.labels.get(k) == v for k, v in label_match.items()):
                total += sample.value
    return total


# ---------------------------------------------------------------------------
# YAML parsing
# ---------------------------------------------------------------------------


def test_load_journeys_parses_three_phase1_yamls(tmp_path):
    """Drop the real Phase 1 journey files into a tmp dir and verify they parse."""
    yaml_text = """
platform: selva
journey: admin-login
description: smoke
schedule_seconds: 300
safety_class: read-only
steps:
  - name: open-login-page
    method: GET
    url: https://app.selva.town/login
    expect_status: 200
"""
    (tmp_path / "selva.yaml").write_text(yaml_text)
    (tmp_path / "karafiel.yaml").write_text(yaml_text.replace("selva", "karafiel"))
    (tmp_path / "dhanam.yaml").write_text(yaml_text.replace("selva", "dhanam"))

    journeys = load_journeys(str(tmp_path))
    assert len(journeys) == 3
    platforms = sorted(j.platform for j in journeys)
    assert platforms == ["dhanam", "karafiel", "selva"]


def test_load_journeys_skips_malformed_files(tmp_path, caplog):
    """One bad YAML file must not block the others."""
    (tmp_path / "good.yaml").write_text(
        "platform: p\njourney: j\nsteps:\n  - {name: s, url: 'http://x'}\n"
    )
    (tmp_path / "bad.yaml").write_text("not: [valid")  # malformed YAML
    journeys = load_journeys(str(tmp_path))
    assert len(journeys) == 1
    assert journeys[0].platform == "p"


def test_journey_rejects_invalid_safety_class():
    with pytest.raises(ValueError, match="safety_class"):
        Journey.from_dict(
            {
                "platform": "p",
                "journey": "j",
                "safety_class": "yolo",
                "steps": [{"name": "s", "url": "http://x"}],
            }
        )


# ---------------------------------------------------------------------------
# Placeholder resolution
# ---------------------------------------------------------------------------


def test_resolve_placeholders_substitutes_env_vars():
    out = resolve_placeholders("user=${ADMIN_EMAIL}&p=${ADMIN_PASSWORD}", {
        "ADMIN_EMAIL": "admin@madfam.io",
        "ADMIN_PASSWORD": "s3cret",
    })
    assert out == "user=admin@madfam.io&p=s3cret"


def test_resolve_placeholders_raises_on_missing_var():
    with pytest.raises(probe_mod.MissingCredentialError) as exc:
        resolve_placeholders("${MISSING_VAR}", {})
    assert exc.value.args[0] == "MISSING_VAR"


def test_resolve_step_resolves_form_body_and_url():
    step = _step(
        method="POST",
        url="https://${HOST}/login",
        body_form={"email": "${ADMIN_EMAIL}", "password": "${ADMIN_PASSWORD}"},
    )
    resolved = resolve_step(
        step,
        {"HOST": "auth.madfam.io", "ADMIN_EMAIL": "a@b.co", "ADMIN_PASSWORD": "p"},
    )
    assert resolved.url == "https://auth.madfam.io/login"
    assert resolved.body_form == {"email": "a@b.co", "password": "p"}


# ---------------------------------------------------------------------------
# Assertions
# ---------------------------------------------------------------------------


def test_evaluate_assertions_passes_on_status_match():
    step = _step(expect_status=200)
    response = httpx.Response(200, request=httpx.Request("GET", "https://x"))
    assert evaluate_assertions(step, response) is None


def test_evaluate_assertions_fails_on_status_mismatch():
    step = _step(expect_status=200)
    response = httpx.Response(502, request=httpx.Request("GET", "https://x"))
    reason = evaluate_assertions(step, response)
    assert reason is not None
    assert "502" in reason


def test_evaluate_assertions_fails_on_url_substring_miss():
    step = Step.from_dict(
        {
            "name": "s",
            "url": "https://app.selva.town/sso",
            "expect_status": 200,
            "expect_url_contains": "auth.madfam.io",
        }
    )
    response = httpx.Response(
        200,
        request=httpx.Request("GET", "https://app.selva.town/oops"),
    )
    reason = evaluate_assertions(step, response)
    assert reason is not None
    assert "auth.madfam.io" in reason


def test_evaluate_assertions_passes_on_json_contains():
    step = Step.from_dict(
        {
            "name": "s",
            "url": "https://x/me",
            "expect_status": 200,
            "expect_json_contains": {"email": "admin@madfam.io"},
        }
    )
    response = httpx.Response(
        200,
        content=json.dumps({"email": "admin@madfam.io", "tenant": "madfam"}).encode(),
        request=httpx.Request("GET", "https://x/me"),
    )
    assert evaluate_assertions(step, response) is None


def test_evaluate_assertions_fails_on_json_value_mismatch():
    step = Step.from_dict(
        {
            "name": "s",
            "url": "https://x/me",
            "expect_status": 200,
            "expect_json_contains": {"email": "admin@madfam.io"},
        }
    )
    response = httpx.Response(
        200,
        content=json.dumps({"email": "someone-else@example.com"}).encode(),
        request=httpx.Request("GET", "https://x/me"),
    )
    reason = evaluate_assertions(step, response)
    assert reason is not None


# ---------------------------------------------------------------------------
# CSP form-action — the "would have caught Janua login" assertion
# ---------------------------------------------------------------------------


def test_csp_form_action_pass_when_directive_includes_target_host():
    headers = httpx.Headers(
        {"content-security-policy": "default-src 'self'; form-action https://auth.madfam.io"}
    )
    assert (
        _check_csp_form_action(headers, "https://auth.madfam.io/login") is None
    )


def test_csp_form_action_fail_when_directive_excludes_target_host():
    """This is the literal regression-guard for the 2026-05-04 Janua incident:
    the CSP form-action was set to 'self' only, which broke posting to Janua.
    With this assertion in place the journey would have failed within 5
    minutes instead of the hours the real incident ran for."""
    headers = httpx.Headers(
        {"content-security-policy": "default-src 'self'; form-action 'none'"}
    )
    reason = _check_csp_form_action(
        headers, "https://auth.madfam.io/login"
    )
    assert reason is not None
    assert "form-action" in reason
    assert "auth.madfam.io" in reason


def test_csp_form_action_pass_with_wildcard_subdomain():
    headers = httpx.Headers(
        {"content-security-policy": "form-action *.madfam.io"}
    )
    assert _check_csp_form_action(headers, "https://auth.madfam.io/x") is None


def test_csp_form_action_pass_when_no_csp_header():
    headers = httpx.Headers({})
    assert _check_csp_form_action(headers, "https://x/y") is None


# ---------------------------------------------------------------------------
# Step execution via MockTransport
# ---------------------------------------------------------------------------


def test_execute_step_records_status_and_latency():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"ok": True})

    with _client(handler) as client:
        result = execute_step(_step(), client, timeout=5.0)
    assert result.passed is True
    assert result.status_code == 200
    assert result.latency_seconds >= 0


def test_execute_step_handles_network_error():
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused")

    with _client(handler) as client:
        result = execute_step(_step(), client, timeout=5.0)
    assert result.passed is False
    assert result.status_code is None
    assert "ConnectError" in (result.failure_reason or "")


# ---------------------------------------------------------------------------
# Sandbox boundary
# ---------------------------------------------------------------------------


def test_should_run_journey_blocks_mutating_against_production():
    j = Journey(
        platform="x",
        journey="y",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_MUTATING,
        steps=(_step(),),
    )
    run, reason = should_run_journey(j, "production")
    assert run is False
    assert reason is not None
    assert "mutating" in reason


def test_should_run_journey_allows_read_only_against_production():
    j = Journey(
        platform="x",
        journey="y",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(_step(),),
    )
    run, reason = should_run_journey(j, "production")
    assert run is True
    assert reason is None


def test_should_run_journey_allows_mutating_against_preprod():
    j = Journey(
        platform="x",
        journey="y",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_MUTATING,
        steps=(_step(),),
    )
    run, _ = should_run_journey(j, "preprod")
    assert run is True


# ---------------------------------------------------------------------------
# End-to-end journey replay
# ---------------------------------------------------------------------------


def test_execute_journey_passes_when_all_steps_succeed(monkeypatch):
    journey = Journey(
        platform="selva",
        journey="admin-login",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(
            _step(name="step-a", url="https://app.selva.town/a"),
            _step(name="step-b", url="https://app.selva.town/b"),
        ),
    )

    def handler(request):
        return httpx.Response(200)

    # Patch httpx.Client used inside execute_journey.
    real_client = httpx.Client

    def fake_client(*args, **kwargs):
        kwargs.pop("http2", None)
        return real_client(transport=httpx.MockTransport(handler), **{
            k: v for k, v in kwargs.items() if k != "transport"
        })

    monkeypatch.setattr(probe_mod.httpx, "Client", fake_client)

    result = execute_journey(journey, env={}, timeout=5.0)
    assert result.passed is True
    assert len(result.step_results) == 2


def test_execute_journey_stops_at_first_failure(monkeypatch):
    journey = Journey(
        platform="selva",
        journey="admin-login",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(
            _step(name="step-a", url="https://app.selva.town/a"),
            _step(name="step-b", url="https://app.selva.town/b"),
            _step(name="step-c", url="https://app.selva.town/c"),
        ),
    )

    call_count = {"n": 0}

    def handler(request):
        call_count["n"] += 1
        # First call passes, second fails with 502, third should never happen.
        if call_count["n"] == 1:
            return httpx.Response(200)
        return httpx.Response(502)

    real_client = httpx.Client

    def fake_client(*args, **kwargs):
        kwargs.pop("http2", None)
        return real_client(transport=httpx.MockTransport(handler), **{
            k: v for k, v in kwargs.items() if k != "transport"
        })

    monkeypatch.setattr(probe_mod.httpx, "Client", fake_client)

    result = execute_journey(journey, env={}, timeout=5.0)
    assert result.passed is False
    assert len(result.step_results) == 2  # stopped after step-b's failure
    assert result.step_results[0].passed is True
    assert result.step_results[1].passed is False
    assert call_count["n"] == 2  # step-c was never executed


def test_execute_journey_skipped_on_missing_credential(monkeypatch):
    journey = Journey(
        platform="selva",
        journey="admin-login",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(
            _step(name="needs-cred", url="https://x/y", method="POST"),
        ),
    )
    # Inject a placeholder into the step's body_form by reconstructing it:
    journey = Journey(
        platform=journey.platform,
        journey=journey.journey,
        description=journey.description,
        schedule_seconds=journey.schedule_seconds,
        safety_class=journey.safety_class,
        steps=(
            Step.from_dict(
                {
                    "name": "needs-cred",
                    "method": "POST",
                    "url": "https://x/y",
                    "body_form": {"password": "${ADMIN_PASSWORD}"},
                }
            ),
        ),
    )

    def handler(request):
        raise AssertionError("step should have been skipped before HTTP call")

    real_client = httpx.Client

    def fake_client(*args, **kwargs):
        kwargs.pop("http2", None)
        return real_client(transport=httpx.MockTransport(handler), **{
            k: v for k, v in kwargs.items() if k != "transport"
        })

    monkeypatch.setattr(probe_mod.httpx, "Client", fake_client)

    result = execute_journey(journey, env={}, timeout=5.0)  # no ADMIN_PASSWORD
    assert result.passed is False
    assert result.skipped_reason is not None
    assert "ADMIN_PASSWORD" in result.skipped_reason


# ---------------------------------------------------------------------------
# Metrics emission
# ---------------------------------------------------------------------------


def test_metrics_record_passing_journey_increments_pass_total():
    metrics = Metrics()
    journey = Journey(
        platform="selva",
        journey="admin-login",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(_step(),),
    )
    from probe import JourneyResult, StepResult

    metrics.record(
        JourneyResult(
            journey=journey,
            passed=True,
            step_results=[
                StepResult(
                    step=_step(),
                    passed=True,
                    status_code=200,
                    final_url="https://x",
                    latency_seconds=0.123,
                    failure_reason=None,
                )
            ],
        )
    )
    samples = list(metrics.registry.collect())
    val = _counter_total(
        samples,
        "synthetic_journey_pass",
        {"platform": "selva", "journey": "admin-login"},
    )
    assert val == 1.0
    consec = _gauge_value(
        samples,
        "synthetic_journey_consecutive_failures",
        {"platform": "selva", "journey": "admin-login"},
    )
    assert consec == 0.0


def test_metrics_record_failing_journey_increments_fail_with_step_and_reason():
    metrics = Metrics()
    journey = Journey(
        platform="selva",
        journey="admin-login",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(_step(name="post-credentials"),),
    )
    from probe import JourneyResult, StepResult

    metrics.record(
        JourneyResult(
            journey=journey,
            passed=False,
            step_results=[
                StepResult(
                    step=_step(name="post-credentials"),
                    passed=False,
                    status_code=502,
                    final_url=None,
                    latency_seconds=2.0,
                    failure_reason="expected status 200, got 502",
                )
            ],
        )
    )
    samples = list(metrics.registry.collect())
    val = _counter_total(
        samples,
        "synthetic_journey_fail",
        {
            "platform": "selva",
            "journey": "admin-login",
            "step": "post-credentials",
            "reason": "status_mismatch",
        },
    )
    assert val == 1.0
    consec = _gauge_value(
        samples,
        "synthetic_journey_consecutive_failures",
        {"platform": "selva", "journey": "admin-login"},
    )
    assert consec == 1.0


def test_metrics_consecutive_failures_resets_on_pass():
    """The Slack alert key is `consecutive_failures >= 3`. This test guards
    the reset path so a recovery doesn't leave a stale-high value firing."""
    metrics = Metrics()
    journey = Journey(
        platform="x",
        journey="y",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(_step(),),
    )
    from probe import JourneyResult, StepResult

    fail = JourneyResult(
        journey=journey,
        passed=False,
        step_results=[
            StepResult(
                step=_step(),
                passed=False,
                status_code=500,
                final_url=None,
                latency_seconds=0.1,
                failure_reason="expected status 200, got 500",
            )
        ],
    )
    passing = JourneyResult(
        journey=journey,
        passed=True,
        step_results=[
            StepResult(
                step=_step(),
                passed=True,
                status_code=200,
                final_url="https://x",
                latency_seconds=0.1,
                failure_reason=None,
            )
        ],
    )

    metrics.record(fail)
    metrics.record(fail)
    samples = list(metrics.registry.collect())
    assert _gauge_value(
        samples,
        "synthetic_journey_consecutive_failures",
        {"platform": "x", "journey": "y"},
    ) == 2.0

    metrics.record(passing)
    samples = list(metrics.registry.collect())
    assert _gauge_value(
        samples,
        "synthetic_journey_consecutive_failures",
        {"platform": "x", "journey": "y"},
    ) == 0.0


def test_metrics_record_skipped_journey_does_not_count_as_fail():
    metrics = Metrics()
    journey = Journey(
        platform="x",
        journey="y",
        description="",
        schedule_seconds=300,
        safety_class=SAFETY_READ_ONLY,
        steps=(_step(),),
    )
    from probe import JourneyResult

    metrics.record(
        JourneyResult(
            journey=journey,
            passed=False,
            step_results=[],
            skipped_reason="missing credential ${ADMIN_PASSWORD}",
        )
    )
    samples = list(metrics.registry.collect())
    skipped = _counter_total(
        samples,
        "synthetic_journey_skipped",
        {"platform": "x", "journey": "y", "reason": "missing_credential"},
    )
    assert skipped == 1.0
    fail = _counter_total(
        samples,
        "synthetic_journey_fail",
        {"platform": "x", "journey": "y"},
    )
    assert fail == 0.0


# ---------------------------------------------------------------------------
# Reason classification — keeps label cardinality bounded
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "raw,expected",
    [
        ("expected status 200, got 502", "status_mismatch"),
        ("expected URL to contain auth.madfam.io, got https://wrong/x", "wrong_redirect"),
        ("CSP form-action 'self' does not include host auth.madfam.io", "csp_violation"),
        ("expected JSON key email missing", "body_assertion"),
        ("ConnectTimeout: timed out", "timeout"),
        ("ConnectError: connection refused", "network"),
        ("missing credential ADMIN_PASSWORD", "missing_credential"),
        (None, "unknown"),
        ("something nobody anticipated", "other"),
    ],
)
def test_classify_fail_reason_buckets_correctly(raw, expected):
    assert _classify_fail_reason(raw) == expected


# ---------------------------------------------------------------------------
# Real journey YAMLs ship-shape — guards against typos in the manifests
# ---------------------------------------------------------------------------


def test_phase1_journey_yamls_load_with_real_manifest_content(tmp_path):
    """Lift the real Phase 1 YAML files into a tmp dir and ensure they parse
    cleanly. Catches typos in the shipped manifests before the probe pod
    starts crashlooping in prod."""
    from pathlib import Path

    journeys_dir = Path(__file__).resolve().parent.parent / "journeys"
    if not journeys_dir.exists():
        pytest.skip("journey manifests not present in this checkout")

    journeys = load_journeys(str(journeys_dir))
    platforms = sorted(j.platform for j in journeys)
    # Phase 1 ships exactly three journeys — guard against accidental drops.
    assert platforms == ["dhanam", "karafiel", "selva"]
    for j in journeys:
        # Every Phase 1 journey is read-only by policy.
        assert j.safety_class == SAFETY_READ_ONLY
        # All journeys hit Janua at some point — that's the shared dependency
        # that gives the triple-fail diagnostic signal.
        urls = [s.url for s in j.steps]
        assert any("auth.madfam.io" in u or "${" in u for u in urls), (
            f"journey {j.platform}/{j.journey} does not exercise the Janua path"
        )
