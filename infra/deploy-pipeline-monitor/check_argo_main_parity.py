"""ArgoCD main-branch commit parity monitor.

Runs as a Kubernetes CronJob in the `monitoring` namespace. It lists ArgoCD
Applications, extracts every GitHub `madfam-org/*` source pinned to `main`,
compares Argo's deployed revision to the current GitHub `main` commit SHA, and
pushes metrics to Pushgateway.

This deliberately measures version drift only. Argo `OutOfSync`/health drift is
handled by separate Argo and workload health alerts.
"""

from __future__ import annotations

import json
import logging
import os
import re
import time
from dataclasses import dataclass
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


@dataclass(frozen=True)
class AppSource:
    app: str
    source_index: int
    repo: str
    path: str
    deployed_revision: str


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

    def record(self, sources: list[AppSource], main_shas: dict[str, str], now: float) -> int:
        drift = 0
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
        self.checked_sources.set(len(sources))
        self.drift_sources.set(drift)
        self.probe_ok.set(1)
        self.last_run.set(now)
        return drift


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
        drift = metrics.record(sources, main_shas, now=now)
        LOG.info(
            json.dumps(
                {
                    "event": "parity_done",
                    "sources_checked": len(sources),
                    "repos_checked": len(main_shas),
                    "drift_sources": drift,
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
