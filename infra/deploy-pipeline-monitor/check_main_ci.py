"""Deploy-pipeline freshness monitor.

Runs as a Kubernetes CronJob (every 5 min) in the `monitoring` namespace.
Closes the silent-failure gap exemplified by 2026-05-04: selva-office CI
failed on main at 02:29 → no image rebuilt → ArgoCD had nothing new → pods
stayed 8h-old, undetected for 3h.

For each `(repo, branch)` in /etc/deploy-pipeline-monitor/repos.json:
    1. Query GitHub for the latest commit on `branch`.
    2. Query GitHub for the latest 5 completed PUSH-event Actions runs on
       `branch`: the repo's CI for what was merged.
    3. Query GitHub for the latest completed SCHEDULE-event runs on `branch`:
       the repo's scheduled canaries, tracked per workflow.
    4. Compute freshness + failure metrics.
    5. Push to Prometheus Pushgateway (single batch per run).

WHY PUSH AND SCHEDULE ARE SEPARATE (infra-stability audit 2026-10-17, W1O-1).
Until then step 2 read the latest completed run of ANY event. A repo whose
push CI was green but whose scheduled production canary was red (forgesight
`Production Smoke`, rondelio `Production Canaries`) read as "main CI failing"
and paged MainCIFailingForTooLong, the alert for merged-but-not-built code.
A cancelled merge-bot run (`workflow_run`) and Dependabot jobs (`dynamic`) did
the same. The push signal now counts only `event=push`, and a red canary is
its own signal (`repo_main_scheduled_*`, alert MainCanaryFailing): a
production check failing is worth knowing, but it is not a build failure, and
naming it as one sends the reader to the wrong place.

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

# The GitHub `event` of a run that counts as the repo's CI for what was merged.
PUSH_EVENT = "push"
# The `event` of a scheduled canary run.
SCHEDULE_EVENT = "schedule"
# How many completed scheduled runs to read per repo. Runs of every scheduled
# workflow share this page, newest first, so it must cover a few runs of each:
# 30 is three runs each for ten workflows, and costs one API call either way.
SCHEDULED_RUNS_PER_PAGE = 30
# A canary run that FAILED. `cancelled` and `skipped` are neither a pass nor a
# failure for a canary (a superseded or condition-skipped run says nothing
# about production), so they are passed over: they neither count towards nor
# break a failure streak.
CANARY_FAILING_CONCLUSIONS = ("failure", "timed_out")


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

    Two API calls: branch HEAD + latest 5 PUSH-event workflow runs. We
    deliberately pull 5 (not 1) so we can compute `failure_streak` without a
    second paginated call. `event=push` keeps scheduled canaries, merge-bot
    `workflow_run` runs and Dependabot `dynamic` runs out of the build signal;
    canaries are read separately by `fetch_scheduled_states`.
    """
    branch_url = f"{api_base}/repos/{spec.repo}/branches/{spec.branch}"
    runs_url = (
        f"{api_base}/repos/{spec.repo}/actions/runs"
        f"?branch={spec.branch}&status=completed&event={PUSH_EVENT}&per_page=5"
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


@dataclass(frozen=True)
class ScheduledWorkflowState:
    """One scheduled workflow's (a canary's) latest state on the tracked branch."""

    workflow: str
    latest_conclusion: str | None  # newest completed run's conclusion
    latest_completed_at: float | None  # epoch seconds
    failure_streak: int  # consecutive failed runs from newest, cancelled/skipped passed over


def _workflow_name(run: dict[str, Any]) -> str:
    """The workflow a run belongs to, as a human reads it in the Actions tab.

    `name` is the workflow's name; the workflow file path is the fallback
    because two workflows may share a name and a path cannot. Never empty, so
    a metric label is never blank.
    """
    name = run.get("name")
    if isinstance(name, str) and name.strip():
        return name.strip()
    path = run.get("path")
    if isinstance(path, str) and path.strip():
        return path.strip()
    return f"workflow-{run.get('workflow_id', 'unknown')}"


def summarise_scheduled_runs(runs: list[dict[str, Any]]) -> list[ScheduledWorkflowState]:
    """PURE. Newest-first completed scheduled runs -> one state per workflow.

    The streak counts consecutive FAILED runs (`CANARY_FAILING_CONCLUSIONS`)
    from the newest backwards and stops at the first success. `cancelled` and
    `skipped` runs are passed over: they are not evidence either way. Sorted by
    workflow name so the metric order is stable between runs.
    """
    by_workflow: dict[str, list[dict[str, Any]]] = {}
    for run in runs:
        if run.get("conclusion") not in TERMINAL_CONCLUSIONS:
            continue
        by_workflow.setdefault(_workflow_name(run), []).append(run)

    states: list[ScheduledWorkflowState] = []
    for workflow in sorted(by_workflow):
        wf_runs = by_workflow[workflow]
        latest = wf_runs[0]
        streak = 0
        for run in wf_runs:
            conclusion = run.get("conclusion")
            if conclusion == "success":
                break
            if conclusion in CANARY_FAILING_CONCLUSIONS:
                streak += 1
        states.append(
            ScheduledWorkflowState(
                workflow=workflow,
                latest_conclusion=latest.get("conclusion"),
                latest_completed_at=_parse_iso8601(
                    latest.get("updated_at") or latest.get("run_started_at")
                ),
                failure_streak=streak,
            )
        )
    return states


def fetch_scheduled_states(
    spec: RepoSpec,
    session: requests.Session,
    timeout: float,
    api_base: str = DEFAULT_GITHUB_API,
) -> list[ScheduledWorkflowState]:
    """Query GitHub for the repo's scheduled (canary) runs on the tracked branch.

    One API call. A repo with no scheduled workflow returns [] and emits no
    canary series, which is the truth: there is nothing scheduled to fail.
    """
    runs_url = (
        f"{api_base}/repos/{spec.repo}/actions/runs"
        f"?branch={spec.branch}&status=completed&event={SCHEDULE_EVENT}"
        f"&per_page={SCHEDULED_RUNS_PER_PAGE}"
    )
    runs_data = _gh_get(session, runs_url, timeout)
    return summarise_scheduled_runs(runs_data.get("workflow_runs", []) or [])


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
        # Scheduled canaries, per workflow. Separate from the push metrics above
        # on purpose: a red canary is a production signal, not a build failure.
        canary_labels = ("repo", "branch", "workflow")
        self.scheduled_failure_streak = Gauge(
            "repo_main_scheduled_failure_streak_count",
            "Consecutive failed runs of one scheduled workflow on the tracked branch (newest first; cancelled/skipped passed over).",
            canary_labels,
            registry=self.registry,
        )
        self.scheduled_latest_failed = Gauge(
            "repo_main_scheduled_latest_failed",
            "1 if the newest completed run of this scheduled workflow failed or timed out, else 0.",
            canary_labels,
            registry=self.registry,
        )
        self.scheduled_probe_ok = Gauge(
            "repo_main_scheduled_probe_ok",
            "1 if this run fetched the repo's scheduled runs from GitHub, else 0.",
            repo_labels,
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

    def record_scheduled(
        self,
        spec: RepoSpec,
        states: list[ScheduledWorkflowState] | None,
    ) -> None:
        """Record one repo's canary states. `None` means the fetch failed."""
        labels = {"repo": spec.repo, "branch": spec.branch}
        if states is None:
            self.scheduled_probe_ok.labels(**labels).set(0)
            return
        self.scheduled_probe_ok.labels(**labels).set(1)
        for state in states:
            wf = {**labels, "workflow": state.workflow}
            self.scheduled_failure_streak.labels(**wf).set(state.failure_streak)
            self.scheduled_latest_failed.labels(**wf).set(
                1 if state.latest_conclusion in CANARY_FAILING_CONCLUSIONS else 0
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

        # Canaries are read after, and isolated from, the push snapshot: a
        # failed schedule fetch must not blank the build signal, and vice versa.
        try:
            scheduled: list[ScheduledWorkflowState] | None = fetch_scheduled_states(
                spec, session, timeout=timeout, api_base=api_base
            )
        except Exception as exc:  # noqa: BLE001 — per-repo isolation is the point
            LOG.warning(
                json.dumps(
                    {
                        "event": "repo_scheduled_probe_failed",
                        "repo": spec.repo,
                        "branch": spec.branch,
                        "error": f"{type(exc).__name__}: {exc}",
                    }
                )
            )
            scheduled = None
        metrics.record_scheduled(spec, scheduled)
        if scheduled:
            LOG.info(
                json.dumps(
                    {
                        "event": "repo_scheduled_probed",
                        "repo": spec.repo,
                        "branch": spec.branch,
                        "workflows": {
                            st.workflow: {
                                "latest": st.latest_conclusion,
                                "failure_streak": st.failure_streak,
                            }
                            for st in scheduled
                        },
                    }
                )
            )
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
