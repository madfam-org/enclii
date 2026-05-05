"""Deploy-pipeline freshness monitor.

Runs as a Kubernetes CronJob (every 5 min) in the `monitoring` namespace.
Closes the silent-failure gap exemplified by 2026-05-04: selva-office CI
failed on main at 02:29 → no image rebuilt → ArgoCD had nothing new → pods
stayed 8h-old, undetected for 3h.

For each `(repo, branch)` in /etc/deploy-pipeline-monitor/repos.json:
    1. Query GitHub for the latest commit on `branch`.
    2. Query GitHub for the latest 5 completed Actions runs on `branch`.
    3. Compute freshness + failure metrics.
    4. Push to Prometheus Pushgateway (single batch per run).

Per-repo failures are isolated — one repo's API hiccup must not block the
other 13. Sibling pattern: infra/cloudflared-probe/probe.py.
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
from prometheus_client import CollectorRegistry, Gauge, push_to_gateway

LOG = logging.getLogger("deploy-pipeline-monitor")

DEFAULT_REPOS_PATH = "/etc/deploy-pipeline-monitor/repos.json"
DEFAULT_PUSHGATEWAY_URL = "http://pushgateway.monitoring.svc.cluster.local:9091"
DEFAULT_PUSH_JOB = "deploy_pipeline_monitor"
DEFAULT_TIMEOUT_SECONDS = 10
DEFAULT_GITHUB_API = "https://api.github.com"

# CI status values we treat as "completed". Anything else (in_progress, queued)
# is excluded from the latest-run lookup so an in-flight rerun on a previously
# failing commit doesn't briefly mask the failure.
TERMINAL_CONCLUSIONS = ("success", "failure", "cancelled", "timed_out", "skipped")
ALERTING_CONCLUSIONS = ("success", "failure", "cancelled")


@dataclass(frozen=True)
class RepoSpec:
    repo: str  # owner/name
    branch: str = "main"

    @classmethod
    def from_dict(cls, raw: dict[str, Any]) -> "RepoSpec":
        if "repo" not in raw:
            raise ValueError(f"repo entry missing 'repo' key: {raw!r}")
        repo = str(raw["repo"])
        if "/" not in repo:
            raise ValueError(f"'repo' must be owner/name, got {repo!r}")
        return cls(repo=repo, branch=str(raw.get("branch", "main")))


def load_repos(path: str) -> list[RepoSpec]:
    """Load and validate the repo list from a ConfigMap-mounted JSON file."""
    text = Path(path).read_text(encoding="utf-8")
    data = json.loads(text)
    items = data["repos"] if isinstance(data, dict) and "repos" in data else data
    if not isinstance(items, list):
        raise ValueError(f"repos file must be a list or {{'repos': [...]}}, got {type(items).__name__}")
    return [RepoSpec.from_dict(r) for r in items]


@dataclass
class RepoSnapshot:
    """One repo's main-branch deploy-pipeline state at probe time."""

    repo: str
    branch: str
    head_commit_sha: str | None
    head_commit_pushed_at: float | None  # epoch seconds
    latest_run_conclusion: str | None  # success | failure | cancelled | ...
    latest_run_completed_at: float | None  # epoch seconds
    latest_run_head_sha: str | None
    failing_commit_pushed_at: float | None  # only set if latest run failed
    failure_streak: int  # consecutive non-success runs from newest backwards
    error: str | None  # populated if probe failed for this repo


def _parse_iso8601(ts: str | None) -> float | None:
    """GitHub returns ISO-8601 with `Z`. Convert to epoch seconds."""
    if not ts:
        return None
    try:
        from datetime import datetime, timezone

        normalized = ts.rstrip("Z")
        dt = datetime.fromisoformat(normalized).replace(tzinfo=timezone.utc)
        return dt.timestamp()
    except ValueError:
        return None


def _gh_get(session: requests.Session, url: str, timeout: float) -> dict[str, Any]:
    """GET a GitHub API URL, raise on non-2xx, return parsed JSON."""
    resp = session.get(url, timeout=timeout)
    resp.raise_for_status()
    return resp.json()


def fetch_repo_snapshot(
    spec: RepoSpec,
    session: requests.Session,
    timeout: float,
    api_base: str = DEFAULT_GITHUB_API,
) -> RepoSnapshot:
    """Query GitHub for one repo's main-branch state.

    Two API calls: branch HEAD + latest 5 workflow runs. We deliberately
    pull 5 (not 1) so we can compute `failure_streak` without a second
    paginated call.
    """
    branch_url = f"{api_base}/repos/{spec.repo}/branches/{spec.branch}"
    runs_url = (
        f"{api_base}/repos/{spec.repo}/actions/runs"
        f"?branch={spec.branch}&status=completed&per_page=5"
    )

    branch_data = _gh_get(session, branch_url, timeout)
    head_sha = branch_data.get("commit", {}).get("sha")
    head_committer = (
        branch_data.get("commit", {}).get("commit", {}).get("committer", {})
    )
    head_pushed_at = _parse_iso8601(head_committer.get("date"))

    runs_data = _gh_get(session, runs_url, timeout)
    runs = runs_data.get("workflow_runs", []) or []
    # Filter to terminal-state runs only; defensive even though we pass
    # status=completed (some edge cases return waiting + completed mixed).
    terminal = [r for r in runs if r.get("conclusion") in TERMINAL_CONCLUSIONS]

    if not terminal:
        return RepoSnapshot(
            repo=spec.repo,
            branch=spec.branch,
            head_commit_sha=head_sha,
            head_commit_pushed_at=head_pushed_at,
            latest_run_conclusion=None,
            latest_run_completed_at=None,
            latest_run_head_sha=None,
            failing_commit_pushed_at=None,
            failure_streak=0,
            error=None,
        )

    latest = terminal[0]
    latest_conclusion = latest.get("conclusion")
    latest_completed = _parse_iso8601(latest.get("updated_at") or latest.get("run_started_at"))
    latest_head_sha = latest.get("head_sha")

    # Failure streak: count consecutive non-success terminal runs from newest.
    streak = 0
    for run in terminal:
        if run.get("conclusion") == "success":
            break
        streak += 1

    # If the most recent run failed (or was cancelled), surface the timestamp
    # of the commit it ran against, so the alert can phrase "blocked for X
    # minutes" rather than "last run was X minutes ago".
    failing_commit_pushed_at: float | None = None
    if latest_conclusion in ("failure", "cancelled", "timed_out"):
        head_commit = latest.get("head_commit") or {}
        failing_commit_pushed_at = _parse_iso8601(head_commit.get("timestamp"))

    return RepoSnapshot(
        repo=spec.repo,
        branch=spec.branch,
        head_commit_sha=head_sha,
        head_commit_pushed_at=head_pushed_at,
        latest_run_conclusion=latest_conclusion,
        latest_run_completed_at=latest_completed,
        latest_run_head_sha=latest_head_sha,
        failing_commit_pushed_at=failing_commit_pushed_at,
        failure_streak=streak,
        error=None,
    )


class Metrics:
    """Bundle the prometheus_client metric handles for one CronJob run."""

    def __init__(self, registry: CollectorRegistry | None = None) -> None:
        self.registry = registry or CollectorRegistry()
        repo_labels = ("repo", "branch")
        self.commit_age = Gauge(
            "repo_main_latest_commit_age_seconds",
            "Seconds since the HEAD commit on the tracked branch was pushed.",
            repo_labels,
            registry=self.registry,
        )
        self.ci_age = Gauge(
            "repo_main_latest_ci_age_seconds",
            "Seconds since the latest completed CI run on the tracked branch finished.",
            repo_labels,
            registry=self.registry,
        )
        # status gauge — per (repo, branch, status), 1 for the current status, 0 for others.
        self.ci_status = Gauge(
            "repo_main_ci_status",
            "Latest CI run status. Value 1 indicates the active status; 0 otherwise.",
            ("repo", "branch", "status"),
            registry=self.registry,
        )
        self.failure_streak = Gauge(
            "repo_main_ci_failure_streak_count",
            "Consecutive non-success terminal runs on the tracked branch (newest first).",
            repo_labels,
            registry=self.registry,
        )
        self.blocked_seconds = Gauge(
            "repo_main_blocked_seconds",
            "If the latest run failed, seconds since the failing commit was pushed. 0 if last run succeeded.",
            repo_labels,
            registry=self.registry,
        )
        # Health gauge for the monitor itself — 1 if the snapshot succeeded,
        # 0 if we got an exception.
        self.probe_ok = Gauge(
            "repo_main_probe_ok",
            "1 if this run successfully fetched repo state from GitHub, else 0.",
            repo_labels,
            registry=self.registry,
        )
        self.last_run = Gauge(
            "deploy_pipeline_monitor_last_run_timestamp_seconds",
            "Unix timestamp of the last completed deploy-pipeline-monitor CronJob run.",
            registry=self.registry,
        )

    def record(self, snap: RepoSnapshot, now: float) -> None:
        labels = {"repo": snap.repo, "branch": snap.branch}

        if snap.error is not None:
            self.probe_ok.labels(**labels).set(0)
            return
        self.probe_ok.labels(**labels).set(1)

        if snap.head_commit_pushed_at is not None:
            self.commit_age.labels(**labels).set(max(0.0, now - snap.head_commit_pushed_at))
        if snap.latest_run_completed_at is not None:
            self.ci_age.labels(**labels).set(max(0.0, now - snap.latest_run_completed_at))

        for status in ALERTING_CONCLUSIONS:
            value = 1 if snap.latest_run_conclusion == status else 0
            self.ci_status.labels(**labels, status=status).set(value)

        self.failure_streak.labels(**labels).set(snap.failure_streak)

        if snap.latest_run_conclusion == "success" or snap.failing_commit_pushed_at is None:
            self.blocked_seconds.labels(**labels).set(0)
        else:
            self.blocked_seconds.labels(**labels).set(
                max(0.0, now - snap.failing_commit_pushed_at)
            )


def build_session(token: str | None) -> requests.Session:
    session = requests.Session()
    session.headers.update(
        {
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "enclii-deploy-pipeline-monitor/1.0",
        }
    )
    if token:
        session.headers["Authorization"] = f"Bearer {token}"
    return session


def run_once(
    repos: list[RepoSpec],
    metrics: Metrics,
    session: requests.Session,
    timeout: float,
    api_base: str = DEFAULT_GITHUB_API,
    now: float | None = None,
) -> list[RepoSnapshot]:
    """Probe every repo, isolating per-repo failures, and record metrics."""
    now = now if now is not None else time.time()
    snapshots: list[RepoSnapshot] = []
    for spec in repos:
        try:
            snap = fetch_repo_snapshot(spec, session, timeout=timeout, api_base=api_base)
        except Exception as exc:  # noqa: BLE001 — per-repo isolation is the point
            LOG.warning(
                json.dumps(
                    {
                        "event": "repo_probe_failed",
                        "repo": spec.repo,
                        "branch": spec.branch,
                        "error": f"{type(exc).__name__}: {exc}",
                    }
                )
            )
            snap = RepoSnapshot(
                repo=spec.repo,
                branch=spec.branch,
                head_commit_sha=None,
                head_commit_pushed_at=None,
                latest_run_conclusion=None,
                latest_run_completed_at=None,
                latest_run_head_sha=None,
                failing_commit_pushed_at=None,
                failure_streak=0,
                error=f"{type(exc).__name__}: {exc}",
            )
        metrics.record(snap, now=now)
        snapshots.append(snap)
        if snap.error is None:
            LOG.info(
                json.dumps(
                    {
                        "event": "repo_probed",
                        "repo": snap.repo,
                        "branch": snap.branch,
                        "head_sha": (snap.head_commit_sha or "")[:8],
                        "latest_run": snap.latest_run_conclusion,
                        "failure_streak": snap.failure_streak,
                        "blocked_seconds": (
                            None
                            if snap.failing_commit_pushed_at is None
                            else int(now - snap.failing_commit_pushed_at)
                        ),
                    }
                )
            )
    metrics.last_run.set(now)
    return snapshots


def main() -> int:
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(levelname)s %(message)s",
        stream=sys.stdout,
    )

    repos_path = os.environ.get("REPOS_PATH", DEFAULT_REPOS_PATH)
    pushgateway_url = os.environ.get("PUSHGATEWAY_URL", DEFAULT_PUSHGATEWAY_URL)
    push_job = os.environ.get("PUSH_JOB", DEFAULT_PUSH_JOB)
    timeout = float(os.environ.get("REQUEST_TIMEOUT_SECONDS", str(DEFAULT_TIMEOUT_SECONDS)))
    api_base = os.environ.get("GITHUB_API_URL", DEFAULT_GITHUB_API)
    token = os.environ.get("GITHUB_TOKEN")

    if not token:
        LOG.error(json.dumps({"event": "github_token_missing"}))
        return 2

    try:
        repos = load_repos(repos_path)
    except (FileNotFoundError, ValueError, json.JSONDecodeError) as exc:
        LOG.error(json.dumps({"event": "repos_load_failed", "path": repos_path, "error": str(exc)}))
        return 1

    LOG.info(
        json.dumps(
            {
                "event": "monitor_start",
                "repos": len(repos),
                "pushgateway_url": pushgateway_url,
                "push_job": push_job,
            }
        )
    )

    metrics = Metrics()
    session = build_session(token)
    snapshots = run_once(repos, metrics, session, timeout=timeout, api_base=api_base)

    try:
        push_to_gateway(pushgateway_url, job=push_job, registry=metrics.registry)
    except Exception as exc:  # noqa: BLE001 — log but don't crash the CronJob
        LOG.error(
            json.dumps(
                {
                    "event": "pushgateway_push_failed",
                    "error": f"{type(exc).__name__}: {exc}",
                    "url": pushgateway_url,
                }
            )
        )
        return 3

    failed = sum(1 for s in snapshots if s.error is not None)
    LOG.info(
        json.dumps(
            {
                "event": "monitor_done",
                "repos_probed": len(snapshots),
                "repos_failed": failed,
            }
        )
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
