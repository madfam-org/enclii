"""Unit tests for the deploy-pipeline freshness monitor.

GitHub API calls are stubbed via a tiny FakeSession so the suite runs
offline. We deliberately do not exercise pushgateway here — the metric
side-effects on `Metrics.registry` are inspected directly.
"""

from __future__ import annotations

import json
from typing import Any

import pytest

import check_main_ci as mod
from check_main_ci import (
    Metrics,
    RepoSnapshot,
    RepoSpec,
    fetch_repo_snapshot,
    load_repos,
    run_once,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


class _FakeResponse:
    def __init__(self, payload: Any, status_code: int = 200) -> None:
        self._payload = payload
        self.status_code = status_code

    def json(self) -> Any:
        return self._payload

    def raise_for_status(self) -> None:
        if self.status_code >= 400:
            import requests

            raise requests.HTTPError(f"{self.status_code}")


class _FakeSession:
    """Minimal `requests.Session` stand-in keyed on URL substring."""

    def __init__(self, routes: dict[str, _FakeResponse]) -> None:
        self.routes = routes
        self.calls: list[str] = []

    def get(self, url: str, timeout: float | None = None) -> _FakeResponse:  # noqa: ARG002
        self.calls.append(url)
        for needle, response in self.routes.items():
            if needle in url:
                return response
        raise AssertionError(f"unmocked URL: {url}")


def _gauge_sample(registry, metric_name: str, label_match: dict[str, str]) -> float | None:
    for family in registry.collect():
        if family.name != metric_name:
            continue
        for sample in family.samples:
            if all(sample.labels.get(k) == v for k, v in label_match.items()):
                return sample.value
    return None


# ---------------------------------------------------------------------------
# load_repos
# ---------------------------------------------------------------------------


def test_load_repos_accepts_wrapped_object_and_defaults_branch_to_main(tmp_path):
    path = tmp_path / "repos.json"
    path.write_text(json.dumps({"repos": [{"repo": "owner/a"}, {"repo": "owner/b", "branch": "release"}]}))
    repos = load_repos(str(path))
    assert repos == [RepoSpec("owner/a", "main"), RepoSpec("owner/b", "release")]


def test_load_repos_rejects_malformed_repo_string(tmp_path):
    path = tmp_path / "repos.json"
    path.write_text(json.dumps([{"repo": "just-a-name"}]))
    with pytest.raises(ValueError):
        load_repos(str(path))


# ---------------------------------------------------------------------------
# fetch_repo_snapshot — success path
# ---------------------------------------------------------------------------


def test_fetch_snapshot_success_clears_blocked_and_streak():
    """Latest run = success → blocked_seconds metric stays 0, streak = 0."""
    branch_payload = {
        "commit": {
            "sha": "abc1234567",
            "commit": {"committer": {"date": "2026-05-04T08:00:00Z"}},
        }
    }
    runs_payload = {
        "workflow_runs": [
            {
                "conclusion": "success",
                "head_sha": "abc1234567",
                "updated_at": "2026-05-04T08:05:30Z",
                "head_commit": {"timestamp": "2026-05-04T08:00:00Z"},
            },
            {
                "conclusion": "success",
                "head_sha": "9990000000",
                "updated_at": "2026-05-03T22:00:00Z",
                "head_commit": {"timestamp": "2026-05-03T21:55:00Z"},
            },
        ]
    }
    session = _FakeSession({"branches/main": _FakeResponse(branch_payload), "actions/runs": _FakeResponse(runs_payload)})

    snap = fetch_repo_snapshot(RepoSpec("madfam-org/karafiel"), session, timeout=1.0)

    assert snap.error is None
    assert snap.head_commit_sha == "abc1234567"
    assert snap.latest_run_conclusion == "success"
    assert snap.failure_streak == 0
    assert snap.failing_commit_pushed_at is None
    # Two API calls — branch HEAD + runs list.
    assert len(session.calls) == 2


# ---------------------------------------------------------------------------
# fetch_repo_snapshot — failure path with streak
# ---------------------------------------------------------------------------


def test_fetch_snapshot_failure_streak_counts_consecutive_non_success():
    """Reproduces the 2026-05-04 selva-office shape: latest = failure, prior = failure, then success."""
    branch_payload = {
        "commit": {
            "sha": "deadbeef00",
            "commit": {"committer": {"date": "2026-05-04T02:29:00Z"}},
        }
    }
    runs_payload = {
        "workflow_runs": [
            {
                "conclusion": "failure",
                "head_sha": "deadbeef00",
                "updated_at": "2026-05-04T02:34:00Z",
                "head_commit": {"timestamp": "2026-05-04T02:29:00Z"},
            },
            {
                "conclusion": "failure",
                "head_sha": "deadbeef00",
                "updated_at": "2026-05-04T02:31:00Z",
                "head_commit": {"timestamp": "2026-05-04T02:29:00Z"},
            },
            {
                "conclusion": "success",
                "head_sha": "feedface00",
                "updated_at": "2026-05-04T01:00:00Z",
                "head_commit": {"timestamp": "2026-05-04T00:55:00Z"},
            },
        ]
    }
    session = _FakeSession({"branches/main": _FakeResponse(branch_payload), "actions/runs": _FakeResponse(runs_payload)})

    snap = fetch_repo_snapshot(RepoSpec("madfam-org/selva-office"), session, timeout=1.0)

    assert snap.latest_run_conclusion == "failure"
    assert snap.failure_streak == 2
    assert snap.failing_commit_pushed_at is not None


# ---------------------------------------------------------------------------
# fetch_repo_snapshot — empty runs (new branch, never built)
# ---------------------------------------------------------------------------


def test_fetch_snapshot_handles_empty_runs():
    branch_payload = {
        "commit": {"sha": "newrepo000", "commit": {"committer": {"date": "2026-05-04T09:00:00Z"}}}
    }
    session = _FakeSession(
        {"branches/main": _FakeResponse(branch_payload), "actions/runs": _FakeResponse({"workflow_runs": []})}
    )

    snap = fetch_repo_snapshot(RepoSpec("madfam-org/empty"), session, timeout=1.0)

    assert snap.latest_run_conclusion is None
    assert snap.failure_streak == 0
    assert snap.failing_commit_pushed_at is None
    assert snap.head_commit_sha == "newrepo000"


# ---------------------------------------------------------------------------
# Metrics — blocked_seconds is 0 on success, > 0 on failure
# ---------------------------------------------------------------------------


def test_metrics_blocked_seconds_zero_on_success():
    m = Metrics()
    snap = RepoSnapshot(
        repo="madfam-org/karafiel",
        branch="main",
        head_commit_sha="abc",
        head_commit_pushed_at=1_000_000.0,
        latest_run_conclusion="success",
        latest_run_completed_at=1_000_300.0,
        latest_run_head_sha="abc",
        failing_commit_pushed_at=None,
        failure_streak=0,
        error=None,
    )
    m.record(snap, now=1_000_500.0)
    val = _gauge_sample(
        m.registry,
        "repo_main_blocked_seconds",
        {"repo": "madfam-org/karafiel", "branch": "main"},
    )
    assert val == 0.0


def test_metrics_blocked_seconds_reflects_failing_commit_age_and_status_one_hot():
    """The whole point: failure on main 30+ min old → blocked_seconds > 1800."""
    m = Metrics()
    failing_pushed_at = 1_000_000.0
    now = failing_pushed_at + 2400  # 40 min later
    snap = RepoSnapshot(
        repo="madfam-org/selva-office",
        branch="main",
        head_commit_sha="deadbeef00",
        head_commit_pushed_at=failing_pushed_at,
        latest_run_conclusion="failure",
        latest_run_completed_at=failing_pushed_at + 300,
        latest_run_head_sha="deadbeef00",
        failing_commit_pushed_at=failing_pushed_at,
        failure_streak=3,
        error=None,
    )
    m.record(snap, now=now)

    blocked = _gauge_sample(
        m.registry,
        "repo_main_blocked_seconds",
        {"repo": "madfam-org/selva-office", "branch": "main"},
    )
    assert blocked == 2400.0

    fail = _gauge_sample(
        m.registry,
        "repo_main_ci_status",
        {"repo": "madfam-org/selva-office", "branch": "main", "status": "failure"},
    )
    succ = _gauge_sample(
        m.registry,
        "repo_main_ci_status",
        {"repo": "madfam-org/selva-office", "branch": "main", "status": "success"},
    )
    assert fail == 1.0
    assert succ == 0.0

    streak = _gauge_sample(
        m.registry,
        "repo_main_ci_failure_streak_count",
        {"repo": "madfam-org/selva-office", "branch": "main"},
    )
    assert streak == 3.0


# ---------------------------------------------------------------------------
# run_once — per-repo failure isolation
# ---------------------------------------------------------------------------


def test_run_once_isolates_per_repo_failures(monkeypatch):
    """One repo throwing must not stop the others; probe_ok records the failure."""
    calls: list[str] = []

    def fake_fetch(spec, session, timeout, api_base=mod.DEFAULT_GITHUB_API):  # noqa: ARG001
        calls.append(spec.repo)
        if spec.repo == "madfam-org/broken":
            raise RuntimeError("boom")
        return RepoSnapshot(
            repo=spec.repo,
            branch=spec.branch,
            head_commit_sha="x",
            head_commit_pushed_at=1_000_000.0,
            latest_run_conclusion="success",
            latest_run_completed_at=1_000_100.0,
            latest_run_head_sha="x",
            failing_commit_pushed_at=None,
            failure_streak=0,
            error=None,
        )

    monkeypatch.setattr(mod, "fetch_repo_snapshot", fake_fetch)

    repos = [RepoSpec("madfam-org/a"), RepoSpec("madfam-org/broken"), RepoSpec("madfam-org/c")]
    metrics = Metrics()
    snapshots = run_once(repos, metrics, session=object(), timeout=1.0, now=1_000_500.0)  # type: ignore[arg-type]

    assert calls == ["madfam-org/a", "madfam-org/broken", "madfam-org/c"]
    ok_a = _gauge_sample(metrics.registry, "repo_main_probe_ok", {"repo": "madfam-org/a", "branch": "main"})
    ok_b = _gauge_sample(metrics.registry, "repo_main_probe_ok", {"repo": "madfam-org/broken", "branch": "main"})
    ok_c = _gauge_sample(metrics.registry, "repo_main_probe_ok", {"repo": "madfam-org/c", "branch": "main"})
    assert ok_a == 1.0
    assert ok_b == 0.0
    assert ok_c == 1.0
    broken = next(s for s in snapshots if s.repo == "madfam-org/broken")
    assert broken.error and "RuntimeError" in broken.error
