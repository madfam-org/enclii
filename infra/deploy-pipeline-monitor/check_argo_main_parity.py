"""ArgoCD main-branch commit parity monitor.

Runs as a Kubernetes CronJob in the `monitoring` namespace. It lists ArgoCD
Applications, extracts every GitHub `madfam-org/*` source pinned to `main`,
compares Argo's deployed revision to the current GitHub `main` commit SHA, and
pushes metrics to Pushgateway.

Two distinct questions, two metric families:

1. `argo_main_parity_source_ok` — does Argo's recorded revision equal GitHub
   `main`? This is *version* parity only.
2. `argo_main_rollout_ok` — did that revision actually roll out? Revision
   parity AND `status.sync.status == Synced` AND the last sync operation did
   not fail.

(2) exists because (1) cannot see a failed sync. `status.sync.revision` is the
revision Argo *targeted*, and Argo writes it even when the sync fails, so a
failed rollout still reports perfect parity. This was not hypothetical: on
2026-08-07 `avala-services` failed its PreSync migrate hook, retried 5 times,
gave up, and sat `OutOfSync` on 2-commit-old pods for four days while this
monitor reported `drift_sources: 0` every five minutes.

The original docstring delegated `OutOfSync` to "separate Argo and workload
health alerts". That delegation is unsound: a failed sync leaves the PREVIOUS
pods running and healthy, so Argo health stays `Healthy` and every workload
probe stays green. Nothing else was ever going to catch it. Hence (2) here.
"""

from __future__ import annotations

import json
import logging
import os
import re
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

import requests
from prometheus_client import CollectorRegistry, Gauge, push_to_gateway

LOG = logging.getLogger("argo-main-parity-monitor")

DEFAULT_ARGO_NAMESPACE = "argocd"
DEFAULT_GITHUB_API = "https://api.github.com"
DEFAULT_PUSHGATEWAY_URL = "http://pushgateway-headless.monitoring.svc.cluster.local:9091"
DEFAULT_PUSH_JOB = "argo_main_parity_monitor"
DEFAULT_TIMEOUT_SECONDS = 10

GITHUB_REPO_RE = re.compile(
    r"^(?:https://github.com/|git@github.com:)(?P<owner>[^/]+)/(?P<repo>[^/.]+)(?:\.git)?$"
)


FAILED_OPERATION_PHASES = frozenset({"Failed", "Error"})


@dataclass(frozen=True)
class AppSource:
    app: str
    source_index: int
    repo: str
    path: str
    deployed_revision: str
    # App-level rollout state, copied onto every source of that app.
    sync_status: str = ""
    operation_phase: str = ""
    operation_finished_at: str = ""

    @property
    def sync_failed(self) -> bool:
        return self.operation_phase in FAILED_OPERATION_PHASES

    def rollout_ok(self, expected_revision: str) -> bool:
        """True only when the target revision is genuinely serving.

        Revision parity alone is not enough: Argo records `sync.revision` even
        for a sync that failed, so a stuck app reports parity forever.
        """
        return (
            self.deployed_revision == expected_revision
            and self.sync_status == "Synced"
            and not self.sync_failed
        )


def _parse_k8s_timestamp(value: str) -> float | None:
    """Parse an RFC3339 timestamp to epoch seconds, tolerating a 'Z' suffix."""
    if not value:
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


def _k8s_api_base() -> str:
    host = os.environ.get("KUBERNETES_SERVICE_HOST")
    port = os.environ.get("KUBERNETES_SERVICE_PORT", "443")
    if not host:
        raise RuntimeError("KUBERNETES_SERVICE_HOST is not set")
    return f"https://{host}:{port}"


def _service_account_session() -> requests.Session:
    token_path = Path("/var/run/secrets/kubernetes.io/serviceaccount/token")
    ca_path = Path("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
    token = token_path.read_text(encoding="utf-8").strip()
    session = requests.Session()
    session.headers["Authorization"] = f"Bearer {token}"
    session.headers["Accept"] = "application/json"
    session.verify = str(ca_path)
    return session


def list_argo_applications(namespace: str, timeout: float) -> list[dict[str, Any]]:
    session = _service_account_session()
    url = f"{_k8s_api_base()}/apis/argoproj.io/v1alpha1/namespaces/{namespace}/applications"
    resp = session.get(url, timeout=timeout)
    resp.raise_for_status()
    payload = resp.json()
    return payload.get("items", [])


def _normalize_repo(repo_url: str) -> str | None:
    match = GITHUB_REPO_RE.match(repo_url)
    if not match:
        return None
    owner = match.group("owner")
    repo = match.group("repo")
    if owner != "madfam-org":
        return None
    return f"{owner}/{repo}"


def extract_main_sources(apps: list[dict[str, Any]]) -> list[AppSource]:
    sources: list[AppSource] = []
    for app in apps:
        name = app.get("metadata", {}).get("name", "")
        spec = app.get("spec", {})
        status = app.get("status", {})
        sync = status.get("sync", {})
        operation_state = status.get("operationState") or {}
        rollout = {
            "sync_status": str(sync.get("status", "") or ""),
            "operation_phase": str(operation_state.get("phase", "") or ""),
            "operation_finished_at": str(operation_state.get("finishedAt", "") or ""),
        }

        if isinstance(spec.get("sources"), list):
            revisions = sync.get("revisions") or []
            for idx, source in enumerate(spec["sources"]):
                if source.get("targetRevision") != "main":
                    continue
                repo = _normalize_repo(str(source.get("repoURL", "")))
                if repo is None:
                    continue
                deployed = revisions[idx] if idx < len(revisions) else sync.get("revision", "")
                sources.append(
                    AppSource(
                        app=name,
                        source_index=idx,
                        repo=repo,
                        path=str(source.get("path", "")),
                        deployed_revision=str(deployed or ""),
                        **rollout,
                    )
                )
            continue

        source = spec.get("source") or {}
        if source.get("targetRevision") != "main":
            continue
        repo = _normalize_repo(str(source.get("repoURL", "")))
        if repo is None:
            continue
        sources.append(
            AppSource(
                app=name,
                source_index=0,
                repo=repo,
                path=str(source.get("path", "")),
                deployed_revision=str(sync.get("revision", "") or ""),
                **rollout,
            )
        )
    return sources


def build_github_session(token: str | None) -> requests.Session:
    session = requests.Session()
    session.headers.update(
        {
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "enclii-argo-main-parity-monitor/1.0",
        }
    )
    if token:
        session.headers["Authorization"] = f"Bearer {token}"
    return session


def fetch_main_shas(
    repos: set[str],
    session: requests.Session,
    timeout: float,
    api_base: str,
) -> dict[str, str]:
    shas: dict[str, str] = {}
    for repo in sorted(repos):
        url = f"{api_base}/repos/{repo}/commits/main"
        resp = session.get(url, timeout=timeout)
        resp.raise_for_status()
        sha = resp.json().get("sha")
        if not sha:
            raise RuntimeError(f"GitHub returned no main SHA for {repo}")
        shas[repo] = str(sha)
    return shas


class Metrics:
    def __init__(self) -> None:
        self.registry = CollectorRegistry()
        source_labels = ("application", "source_index", "repo", "path")
        self.source_ok = Gauge(
            "argo_main_parity_source_ok",
            "1 when Argo deployed revision equals GitHub main SHA for this source, else 0.",
            source_labels,
            registry=self.registry,
        )
        self.checked_sources = Gauge(
            "argo_main_parity_checked_sources",
            "Number of Argo GitHub main sources checked in this run.",
            registry=self.registry,
        )
        self.drift_sources = Gauge(
            "argo_main_parity_drift_sources",
            "Number of Argo GitHub main sources whose deployed revision differs from GitHub main.",
            registry=self.registry,
        )
        # Rollout truth. Revision parity above cannot see a failed sync, because
        # Argo records sync.revision even when the sync fails.
        self.rollout_ok = Gauge(
            "argo_main_rollout_ok",
            "1 when the GitHub main revision is genuinely rolled out (revision parity AND Synced AND last sync operation not failed), else 0.",
            source_labels,
            registry=self.registry,
        )
        self.rollout_failed_sources = Gauge(
            "argo_main_rollout_failed_sources",
            "Number of Argo GitHub main sources where the main revision is NOT genuinely rolled out.",
            registry=self.registry,
        )
        self.rollout_stuck_seconds = Gauge(
            "argo_main_rollout_stuck_seconds",
            "Seconds since the last failed sync operation finished, per application; 0 when the last operation did not fail.",
            ("application", "repo"),
            registry=self.registry,
        )
        self.probe_ok = Gauge(
            "argo_main_parity_probe_ok",
            "1 when the parity monitor completed successfully, else 0.",
            registry=self.registry,
        )
        self.last_run = Gauge(
            "argo_main_parity_last_run_timestamp_seconds",
            "Unix timestamp of the last completed argo-main-parity monitor run.",
            registry=self.registry,
        )

    def record(
        self, sources: list[AppSource], main_shas: dict[str, str], now: float
    ) -> tuple[int, int]:
        drift = 0
        rollout_failed = 0
        for source in sources:
            expected = main_shas[source.repo]
            ok = int(source.deployed_revision == expected)
            drift += 0 if ok else 1
            self.source_ok.labels(
                application=source.app,
                source_index=str(source.source_index),
                repo=source.repo,
                path=source.path,
            ).set(ok)
            if not ok:
                LOG.warning(
                    json.dumps(
                        {
                            "event": "main_revision_drift",
                            "application": source.app,
                            "repo": source.repo,
                            "deployed_revision": source.deployed_revision,
                            "github_main": expected,
                        }
                    )
                )

            rolled_out = int(source.rollout_ok(expected))
            rollout_failed += 0 if rolled_out else 1
            self.rollout_ok.labels(
                application=source.app,
                source_index=str(source.source_index),
                repo=source.repo,
                path=source.path,
            ).set(rolled_out)

            stuck_seconds = 0.0
            if source.sync_failed:
                finished = _parse_k8s_timestamp(source.operation_finished_at)
                if finished is not None:
                    stuck_seconds = max(0.0, now - finished)
            self.rollout_stuck_seconds.labels(
                application=source.app, repo=source.repo
            ).set(stuck_seconds)

            # A source that has revision parity but did NOT roll out is the
            # exact blind spot this monitor used to have — log it distinctly so
            # it cannot be confused with ordinary version drift.
            if not rolled_out and ok:
                LOG.error(
                    json.dumps(
                        {
                            "event": "main_rollout_not_landed",
                            "application": source.app,
                            "repo": source.repo,
                            "revision": source.deployed_revision,
                            "sync_status": source.sync_status,
                            "operation_phase": source.operation_phase,
                            "stuck_seconds": round(stuck_seconds),
                        }
                    )
                )
        self.checked_sources.set(len(sources))
        self.drift_sources.set(drift)
        self.rollout_failed_sources.set(rollout_failed)
        self.probe_ok.set(1)
        self.last_run.set(now)
        return drift, rollout_failed


def main() -> int:
    logging.basicConfig(level=os.environ.get("LOG_LEVEL", "INFO"), format="%(asctime)s %(levelname)s %(message)s")
    timeout = float(os.environ.get("REQUEST_TIMEOUT_SECONDS", str(DEFAULT_TIMEOUT_SECONDS)))
    argo_namespace = os.environ.get("ARGOCD_NAMESPACE", DEFAULT_ARGO_NAMESPACE)
    pushgateway_url = os.environ.get("PUSHGATEWAY_URL", DEFAULT_PUSHGATEWAY_URL)
    push_job = os.environ.get("PUSH_JOB", DEFAULT_PUSH_JOB)
    github_api = os.environ.get("GITHUB_API_URL", DEFAULT_GITHUB_API)
    github_token = os.environ.get("GITHUB_TOKEN")
    now = time.time()

    metrics = Metrics()
    try:
        apps = list_argo_applications(argo_namespace, timeout=timeout)
        sources = extract_main_sources(apps)
        main_shas = fetch_main_shas(
            {source.repo for source in sources},
            session=build_github_session(github_token),
            timeout=timeout,
            api_base=github_api,
        )
        drift, rollout_failed = metrics.record(sources, main_shas, now=now)
        LOG.info(
            json.dumps(
                {
                    "event": "parity_done",
                    "sources_checked": len(sources),
                    "repos_checked": len(main_shas),
                    "drift_sources": drift,
                    "rollout_failed_sources": rollout_failed,
                }
            )
        )
    except Exception as exc:  # noqa: BLE001 - a failed monitor run must still push probe_ok=0
        metrics.probe_ok.set(0)
        metrics.last_run.set(now)
        LOG.exception("argo main parity monitor failed: %s", exc)
        push_to_gateway(pushgateway_url, job=push_job, registry=metrics.registry)
        return 1

    push_to_gateway(pushgateway_url, job=push_job, registry=metrics.registry)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
