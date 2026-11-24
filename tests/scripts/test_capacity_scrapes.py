"""
Pins the capacity-plane scrapes added for RFC 0049 Phase 1: Longhorn, the ARC
controller and Selva's provider credit grants, in the main Prometheus.

Run with:
    pytest tests/scripts/test_capacity_scrapes.py -v

WHY THIS EXISTS
===============
Each of these exporters already served /metrics on 2026-11-04 and nothing in
the main Prometheus read it, so volume sizes, runner counts and grant balances
existed only in three separate UIs. A scrape job that selects a port name the
target does not have, or a keep/drop regex that misses, yields an empty job
that looks configured. The checks below evaluate the relabel rules the way
Prometheus does (anchored regex over `;`-joined labels) against the port
names and metric names the exporters actually serve, and check that the
egress policy lets Prometheus reach each one.
"""
from __future__ import annotations

import re
from functools import lru_cache
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
PROMETHEUS = MONITORING / "prometheus.yaml"
NETPOL = MONITORING / "network-policies.yaml"
ARC_POLICIES = REPO_ROOT / "infra" / "k8s" / "policies" / "arc-default-deny.yaml"


@lru_cache(maxsize=1)
def _config() -> dict:
    for doc in yaml.safe_load_all(PROMETHEUS.read_text()):
        if isinstance(doc, dict) and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-config":
            return yaml.safe_load(doc["data"]["prometheus.yml"])
    raise AssertionError("prometheus-config ConfigMap not found")


def _job(name: str) -> dict:
    jobs = [j for j in _config()["scrape_configs"] if j["job_name"] == name]
    assert len(jobs) == 1, f"expected one {name} job, found {len(jobs)}"
    return jobs[0]


def _regex(rc: dict) -> str:
    value = rc.get("regex", "(.*)")
    if isinstance(value, bool):
        return "true" if value else "false"
    return str(value)


def _keeps(relabel_configs: list[dict], labels: dict[str, str]) -> bool:
    for rc in relabel_configs:
        action = rc.get("action", "replace")
        if action not in ("keep", "drop"):
            continue
        value = rc.get("separator", ";").join(labels.get(s, "") for s in rc["source_labels"])
        matched = re.fullmatch(_regex(rc), value) is not None
        if (action == "keep" and not matched) or (action == "drop" and matched):
            return False
    return True


def _metric_kept(job: dict, name: str) -> bool:
    return _keeps(job.get("metric_relabel_configs", []), {"__name__": name})


def _namespaces(job: dict) -> list[str]:
    return [ns for sd in job["kubernetes_sd_configs"] for ns in sd.get("namespaces", {}).get("names", [])]


# --- Longhorn -------------------------------------------------------------------


def test_longhorn_scrapes_each_manager_on_its_named_port():
    job = _job("longhorn")
    assert _namespaces(job) == ["longhorn-system"]
    target = {"__meta_kubernetes_service_name": "longhorn-backend", "__meta_kubernetes_endpoint_port_name": "manager"}
    assert _keeps(job["relabel_configs"], target)
    for svc, port in [("longhorn-frontend", "http"), ("longhorn-admission-webhook", "admission-webhook")]:
        assert not _keeps(
            job["relabel_configs"],
            {"__meta_kubernetes_service_name": svc, "__meta_kubernetes_endpoint_port_name": port},
        )


@pytest.mark.parametrize(
    "name,kept",
    [
        ("longhorn_volume_actual_size_bytes", True),
        ("longhorn_volume_robustness", True),
        ("longhorn_disk_capacity_bytes", True),
        ("longhorn_node_storage_usage_bytes", True),
        ("longhorn_rest_client_request_latency_seconds_bucket", False),
        ("longhorn_workqueue_queue_duration_seconds_bucket", False),
    ],
)
def test_longhorn_keeps_storage_series_and_drops_client_histograms(name, kept):
    assert _metric_kept(_job("longhorn"), name) is kept


# --- ARC controller -----------------------------------------------------------------


def test_arc_controller_scrapes_the_metrics_port_by_pod_discovery():
    job = _job("arc-controller")
    assert _namespaces(job) == ["arc-system"]
    controller = {
        "__meta_kubernetes_pod_label_app_kubernetes_io_name": "gha-rs-controller",
        "__meta_kubernetes_pod_container_port_name": "metrics",
    }
    assert _keeps(job["relabel_configs"], controller)
    listener = {
        "__meta_kubernetes_pod_label_app_kubernetes_io_name": "",
        "__meta_kubernetes_pod_container_port_name": "",
    }
    assert not _keeps(job["relabel_configs"], listener)


@pytest.mark.parametrize(
    "name,kept",
    [
        ("gha_controller_pending_ephemeral_runners", True),
        ("gha_controller_running_ephemeral_runners", True),
        ("gha_controller_failed_ephemeral_runners", True),
        ("gha_controller_running_listeners", True),
        ("controller_runtime_reconcile_errors_total", True),
        ("controller_runtime_reconcile_time_seconds_bucket", False),
        ("go_goroutines", False),
    ],
)
def test_arc_controller_keeps_runner_gauges_only(name, kept):
    assert _metric_kept(_job("arc-controller"), name) is kept


def test_arc_controller_metrics_port_is_admitted_from_monitoring():
    docs = [d for d in yaml.safe_load_all(ARC_POLICIES.read_text()) if isinstance(d, dict)]
    pol = next(d for d in docs if d["metadata"]["name"] == "arc-controller-metrics-ingress")
    rule = pol["spec"]["ingress"][0]
    assert rule["from"][0]["namespaceSelector"]["matchLabels"]["kubernetes.io/metadata.name"] == "monitoring"
    assert {"protocol": "TCP", "port": 8080} in rule["ports"]


# --- Selva grants -------------------------------------------------------------------


def test_selva_scrapes_nexus_api_http_port():
    job = _job("selva-nexus-api")
    assert _namespaces(job) == ["selva"]
    assert _keeps(
        job["relabel_configs"],
        {"__meta_kubernetes_service_name": "nexus-api", "__meta_kubernetes_endpoint_port_name": "http"},
    )
    assert not _keeps(
        job["relabel_configs"],
        {"__meta_kubernetes_service_name": "gateway", "__meta_kubernetes_endpoint_port_name": "http"},
    )


@pytest.mark.parametrize(
    "name,kept",
    [
        ("selva_provider_grant_remaining_usd", True),
        ("selva_provider_grant_spent_usd", True),
        ("selva_provider_grant_seconds_to_expiry", True),
        ("http_request_duration_seconds_bucket", False),
        ("python_gc_objects_collected_total", False),
    ],
)
def test_selva_keeps_selva_series_only(name, kept):
    assert _metric_kept(_job("selva-nexus-api"), name) is kept


# --- reachability ------------------------------------------------------------------


def _prometheus_egress() -> list[dict]:
    for doc in yaml.safe_load_all(NETPOL.read_text()):
        if isinstance(doc, dict) and doc.get("kind") == "NetworkPolicy" and doc["metadata"]["name"] == "prometheus-egress":
            return doc["spec"]["egress"]
    raise AssertionError("prometheus-egress not found")


def _egress_allows(namespace_labels: dict[str, str], port: int) -> bool:
    for rule in _prometheus_egress():
        ports = rule.get("ports")
        if ports and not any(p.get("port") == port for p in ports):
            continue
        for peer in rule.get("to", []) or [{}]:
            sel = peer.get("namespaceSelector")
            if sel is None:
                continue
            want = sel.get("matchLabels", {})
            if all(namespace_labels.get(k) == v for k, v in want.items()):
                return True
    return False


def test_prometheus_egress_reaches_all_three():
    assert _egress_allows({"kubernetes.io/metadata.name": "longhorn-system"}, 9500)
    assert _egress_allows({"kubernetes.io/metadata.name": "arc-system"}, 8080)
    # selva carries the application data-access label
    assert _egress_allows({"kubernetes.io/metadata.name": "selva", "enclii.dev/data-access": "true"}, 4300)
