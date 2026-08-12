"""Unit tests for the ArgoCD main-branch rollout monitor.

The central case is a regression fixture built from the real
`avala-services` Application as it stood on 2026-08-07: its PreSync migrate
hook exceeded its deadline, Argo retried 5 times and gave up, and the app sat
`OutOfSync` on two-commit-old pods for four days. Throughout that window
`status.sync.revision` equalled GitHub `main`, so the pre-existing parity
check reported zero drift on every five-minute run.

A detector that has never been shown to fire is not a detector, so
`test_stuck_sync_is_caught_even_though_revision_matches` asserts both halves:
the old signal stays green (proving the fixture reproduces the blind spot) and
the new signal goes red.
"""

from __future__ import annotations

import check_argo_main_parity as mod
from check_argo_main_parity import AppSource, Metrics, extract_main_sources


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

AVALA_MAIN_SHA = "cac4b5ecfade51ecb836310e6d0f0e869b56a534"


def _app(
    name: str,
    *,
    repo: str = "https://github.com/madfam-org/avala.git",
    path: str = "infra/enclii",
    revision: str = AVALA_MAIN_SHA,
    sync_status: str = "Synced",
    phase: str = "Succeeded",
    finished_at: str = "2026-08-07T19:18:34Z",
) -> dict:
    return {
        "metadata": {"name": name},
        "spec": {"source": {"repoURL": repo, "path": path, "targetRevision": "main"}},
        "status": {
            "sync": {"status": sync_status, "revision": revision},
            "operationState": {"phase": phase, "finishedAt": finished_at},
        },
    }


def _gauge(metrics: Metrics, metric_name: str, **labels) -> float:
    value = metrics.registry.get_sample_value(metric_name, labels or None)
    assert value is not None, f"metric {metric_name}{labels or ''} was never set"
    return value


# ---------------------------------------------------------------------------
# Extraction
# ---------------------------------------------------------------------------


def test_extract_carries_rollout_state_onto_each_source() -> None:
    sources = extract_main_sources([_app("avala-services", sync_status="OutOfSync", phase="Failed")])
    assert len(sources) == 1
    source = sources[0]
    assert source.repo == "madfam-org/avala"
    assert source.sync_status == "OutOfSync"
    assert source.operation_phase == "Failed"
    assert source.sync_failed is True


def test_non_madfam_and_non_main_sources_are_ignored() -> None:
    assert extract_main_sources([_app("x", repo="https://github.com/other-org/thing.git")]) == []
    off_main = _app("y")
    off_main["spec"]["source"]["targetRevision"] = "release"
    assert extract_main_sources([off_main]) == []


# ---------------------------------------------------------------------------
# The regression this monitor exists to catch
# ---------------------------------------------------------------------------


def test_stuck_sync_is_caught_even_though_revision_matches() -> None:
    """avala-services, 2026-08-07: parity green, rollout dead."""
    sources = extract_main_sources(
        [_app("avala-services", sync_status="OutOfSync", phase="Failed")]
    )
    metrics = Metrics()
    # 4 days after the failed operation finished.
    now = mod._parse_k8s_timestamp("2026-08-11T19:18:34Z")
    drift, rollout_failed = metrics.record(sources, {"madfam-org/avala": AVALA_MAIN_SHA}, now=now)

    # The OLD signal is blind — this is the bug, asserted so it stays visible.
    assert drift == 0
    assert _gauge(
        metrics,
        "argo_main_parity_source_ok",
        application="avala-services",
        source_index="0",
        repo="madfam-org/avala",
        path="infra/enclii",
    ) == 1.0

    # The NEW signal fires.
    assert rollout_failed == 1
    assert _gauge(
        metrics,
        "argo_main_rollout_ok",
        application="avala-services",
        source_index="0",
        repo="madfam-org/avala",
        path="infra/enclii",
    ) == 0.0

    stuck = _gauge(
        metrics,
        "argo_main_rollout_stuck_seconds",
        application="avala-services",
        repo="madfam-org/avala",
    )
    assert stuck == 4 * 24 * 3600


def test_healthy_app_reports_rollout_ok() -> None:
    sources = extract_main_sources([_app("avala-services")])
    metrics = Metrics()
    drift, rollout_failed = metrics.record(
        sources, {"madfam-org/avala": AVALA_MAIN_SHA}, now=1.0
    )
    assert (drift, rollout_failed) == (0, 0)
    assert _gauge(
        metrics,
        "argo_main_rollout_ok",
        application="avala-services",
        source_index="0",
        repo="madfam-org/avala",
        path="infra/enclii",
    ) == 1.0
    assert _gauge(
        metrics,
        "argo_main_rollout_stuck_seconds",
        application="avala-services",
        repo="madfam-org/avala",
    ) == 0.0


def test_version_drift_still_counts_as_both_drift_and_rollout_failure() -> None:
    sources = extract_main_sources([_app("avala-services", revision="0" * 40)])
    metrics = Metrics()
    drift, rollout_failed = metrics.record(
        sources, {"madfam-org/avala": AVALA_MAIN_SHA}, now=1.0
    )
    assert drift == 1
    assert rollout_failed == 1


def test_in_flight_sync_is_not_reported_as_stuck() -> None:
    """A Running operation is mid-rollout, not a failure — no stuck seconds."""
    sources = extract_main_sources(
        [_app("avala-services", sync_status="OutOfSync", phase="Running", finished_at="")]
    )
    metrics = Metrics()
    _, rollout_failed = metrics.record(sources, {"madfam-org/avala": AVALA_MAIN_SHA}, now=10_000.0)
    assert rollout_failed == 1  # not yet landed
    assert _gauge(
        metrics,
        "argo_main_rollout_stuck_seconds",
        application="avala-services",
        repo="madfam-org/avala",
    ) == 0.0  # but not blamed on a failure


def test_missing_finished_at_does_not_crash_the_run() -> None:
    sources = extract_main_sources(
        [_app("avala-services", sync_status="OutOfSync", phase="Failed", finished_at="not-a-date")]
    )
    metrics = Metrics()
    _, rollout_failed = metrics.record(sources, {"madfam-org/avala": AVALA_MAIN_SHA}, now=10_000.0)
    assert rollout_failed == 1
    assert _gauge(
        metrics,
        "argo_main_rollout_stuck_seconds",
        application="avala-services",
        repo="madfam-org/avala",
    ) == 0.0


def test_multi_source_app_gets_rollout_state_on_every_source() -> None:
    app = {
        "metadata": {"name": "multi"},
        "spec": {
            "sources": [
                {
                    "repoURL": "https://github.com/madfam-org/avala.git",
                    "path": "a",
                    "targetRevision": "main",
                },
                {
                    "repoURL": "https://github.com/madfam-org/avala.git",
                    "path": "b",
                    "targetRevision": "main",
                },
            ]
        },
        "status": {
            "sync": {
                "status": "OutOfSync",
                "revisions": [AVALA_MAIN_SHA, AVALA_MAIN_SHA],
            },
            "operationState": {"phase": "Failed", "finishedAt": "2026-08-07T19:18:34Z"},
        },
    }
    sources = extract_main_sources([app])
    assert len(sources) == 2
    assert all(s.sync_failed for s in sources)
    assert all(not s.rollout_ok(AVALA_MAIN_SHA) for s in sources)


def test_rollout_ok_requires_all_three_conditions() -> None:
    base = dict(app="a", source_index=0, repo="madfam-org/avala", path="p")
    assert AppSource(
        **base, deployed_revision=AVALA_MAIN_SHA, sync_status="Synced", operation_phase="Succeeded"
    ).rollout_ok(AVALA_MAIN_SHA)
    # Synced but the last operation errored.
    assert not AppSource(
        **base, deployed_revision=AVALA_MAIN_SHA, sync_status="Synced", operation_phase="Error"
    ).rollout_ok(AVALA_MAIN_SHA)
    # Operation succeeded but the app has since drifted OutOfSync.
    assert not AppSource(
        **base, deployed_revision=AVALA_MAIN_SHA, sync_status="OutOfSync", operation_phase="Succeeded"
    ).rollout_ok(AVALA_MAIN_SHA)
    # Wrong revision.
    assert not AppSource(
        **base, deployed_revision="0" * 40, sync_status="Synced", operation_phase="Succeeded"
    ).rollout_ok(AVALA_MAIN_SHA)
