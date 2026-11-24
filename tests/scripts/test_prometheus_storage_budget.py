"""
Pins the main Prometheus's history budget (RFC 0049 Phase 1, decision D6).

Run with:
    pytest tests/scripts/test_prometheus_storage_budget.py -v

    # also check the rendered prometheus.yml with the parser production runs:
    ENCLII_PROMTOOL_CHECK_CONFIG="docker run --rm --entrypoint /bin/promtool \\
        -v {dir}:/w:ro docker.io/prom/prometheus:v2.53.3 \\
        check config --syntax-only /w/{name}" \\
        pytest tests/scripts/test_prometheus_storage_budget.py -v

WHY THIS EXISTS
===============
Until 2026-11-04 this Prometheus ran with retention.time=7d and
retention.size=8GB, and the size cap always won: the oldest sample was about
56 hours old, so every [7d] rule and every 7-day panel silently read about 2.3
days. Two causes, both pinned here:

  1. The same exporters were scraped more than once. node-exporter's pods and
     Service both carry the prometheus.io annotations, so `kubernetes-pods`,
     `kubernetes-services` and `node-exporter` all stored every node_* series
     (3x storage, and 3x on any `sum by (instance)` without a job selector).
     Prometheus itself, redis-exporter and postgres-exporter were stored twice.
  2. A handful of histogram bucket families nobody reads were a large share
     of every sample, including a full copy of the API server's registry from
     the control-plane node's kubelet endpoint (k3s runs both in one process).

The target is 7-9 days inside a 16GB cap on a volume that can hold the cap.
The tests read the manifest Prometheus is deployed from, evaluate the relabel
rules the way Prometheus does (anchored RE2-compatible regexes over
`;`-joined source labels), and check that no drop removes a metric a rule or a
git-provisioned dashboard reads.
"""
from __future__ import annotations

import json
import os
import re
import shlex
import subprocess
import tempfile
from functools import lru_cache
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
PROMETHEUS = MONITORING / "prometheus.yaml"
DASHBOARDS = MONITORING / "dashboards"

# Exporters that have a dedicated job. Each must be scraped by that job only.
DEDICATED_SERVICES = {
    ("monitoring", "node-exporter"): "node-exporter",
    ("monitoring", "prometheus"): "prometheus",
    ("data", "redis-exporter"): "redis",
    ("data", "postgres-exporter"): "postgres",
}

# Annotated Services that have NO dedicated job and must stay in
# `kubernetes-services` (secret-key-count-exporter's alert selects that job).
GENERIC_SERVICES = [
    ("monitoring", "secret-key-count-exporter"),
    ("avala", "avala-api"),
    ("kube-system", "kube-dns"),
]


@lru_cache(maxsize=1)
def _docs() -> list[dict]:
    return [d for d in yaml.safe_load_all(PROMETHEUS.read_text()) if isinstance(d, dict)]


def _doc(kind: str, name: str) -> dict:
    return next(d for d in _docs() if d.get("kind") == kind and d["metadata"]["name"] == name)


@lru_cache(maxsize=1)
def _config() -> dict:
    return yaml.safe_load(_doc("ConfigMap", "prometheus-config")["data"]["prometheus.yml"])


def _job(name: str) -> dict:
    return next(j for j in _config()["scrape_configs"] if j["job_name"] == name)


def _args() -> list[str]:
    deploy = _doc("Deployment", "prometheus")
    container = next(c for c in deploy["spec"]["template"]["spec"]["containers"] if c["name"] == "prometheus")
    return container["args"]


def _flag(name: str) -> str:
    prefix = f"--{name}="
    values = [a[len(prefix):] for a in _args() if a.startswith(prefix)]
    assert len(values) == 1, f"expected exactly one {prefix} flag, got {values}"
    return values[0]


_UNITS = {"B": 1, "KB": 2**10, "MB": 2**20, "GB": 2**30, "TB": 2**40}


def _bytes(value: str) -> int:
    """Prometheus size flags use power-of-two units (1GB = 2^30 bytes)."""
    m = re.fullmatch(r"(\d+)(B|KB|MB|GB|TB)", value)
    assert m, f"unparseable size {value!r}"
    return int(m.group(1)) * _UNITS[m.group(2)]


def _days(value: str) -> float:
    m = re.fullmatch(r"(\d+)([dhw])", value)
    assert m, f"unparseable duration {value!r}"
    n, unit = int(m.group(1)), m.group(2)
    return {"d": n, "h": n / 24, "w": n * 7}[unit]


def _regex(rc: dict) -> str:
    """YAML reads an unquoted `regex: true` as a boolean; Prometheus reads it
    as the string "true"."""
    value = rc.get("regex", "(.*)")
    if isinstance(value, bool):
        return "true" if value else "false"
    return str(value)


def _keeps(relabel_configs: list[dict], labels: dict[str, str]) -> bool:
    """Evaluate only the keep/drop steps, in order, the way Prometheus does:
    source label values joined with `;` (missing labels are empty), regex
    fully anchored."""
    for rc in relabel_configs:
        action = rc.get("action", "replace")
        if action not in ("keep", "drop"):
            continue
        value = rc.get("separator", ";").join(labels.get(s, "") for s in rc["source_labels"])
        matched = re.fullmatch(_regex(rc), value) is not None
        if action == "keep" and not matched:
            return False
        if action == "drop" and matched:
            return False
    return True


# --- retention and volume ----------------------------------------------------


def test_history_target_is_seven_to_nine_days():
    days = _days(_flag("storage.tsdb.retention.time"))
    assert 7 <= days <= 9, f"retention.time {days}d is outside the 7-9 day target (RFC 0049 D6)"


def test_size_cap_is_sixteen_gb():
    assert _flag("storage.tsdb.retention.size") == "16GB"


def test_volume_holds_the_cap_with_headroom():
    """The cap counts blocks, WAL and head chunks, but compaction writes a new
    block before deleting its sources, and ext4 reserves blocks the non-root
    Prometheus user cannot use. A quarter of headroom over the cap."""
    pvc = _doc("PersistentVolumeClaim", "prometheus-data")
    request = pvc["spec"]["resources"]["requests"]["storage"]
    m = re.fullmatch(r"(\d+)Gi", request)
    assert m, f"unexpected PVC size format {request!r}"
    volume = int(m.group(1)) * 2**30
    cap = _bytes(_flag("storage.tsdb.retention.size"))
    assert volume >= cap * 1.25, f"PVC {request} leaves under 25% headroom over the {cap / 2**30:.0f}GiB cap"


# --- scrape every exporter once ------------------------------------------------


def test_node_exporter_pods_are_not_scraped_by_the_pod_job():
    pod = {
        "__meta_kubernetes_namespace": "monitoring",
        "__meta_kubernetes_pod_annotation_prometheus_io_scrape": "true",
        "__meta_kubernetes_pod_label_app_kubernetes_io_name": "node-exporter",
    }
    assert not _keeps(_job("kubernetes-pods")["relabel_configs"], pod)


def test_other_annotated_pods_are_still_scraped():
    for ns, name in [("cv", "cv"), ("cloudflare-tunnel", "cloudflared"), ("data", "redis-ha")]:
        pod = {
            "__meta_kubernetes_namespace": ns,
            "__meta_kubernetes_pod_annotation_prometheus_io_scrape": "true",
            "__meta_kubernetes_pod_label_app_kubernetes_io_name": name,
        }
        assert _keeps(_job("kubernetes-pods")["relabel_configs"], pod), f"{ns}/{name} pod lost its scrape"


@pytest.mark.parametrize("ns,svc", sorted(DEDICATED_SERVICES))
def test_services_with_a_dedicated_job_are_not_scraped_twice(ns, svc):
    labels = {
        "__meta_kubernetes_namespace": ns,
        "__meta_kubernetes_service_name": svc,
        "__meta_kubernetes_service_annotation_prometheus_io_scrape": "true",
    }
    assert not _keeps(_job("kubernetes-services")["relabel_configs"], labels)
    # and the dedicated job exists to take it
    _job(DEDICATED_SERVICES[(ns, svc)])


@pytest.mark.parametrize("ns,svc", GENERIC_SERVICES)
def test_generic_annotated_services_are_still_scraped(ns, svc):
    labels = {
        "__meta_kubernetes_namespace": ns,
        "__meta_kubernetes_service_name": svc,
        "__meta_kubernetes_service_annotation_prometheus_io_scrape": "true",
    }
    assert _keeps(_job("kubernetes-services")["relabel_configs"], labels)


# --- bucket drops never remove something that is read -----------------------------


def _metric_drops() -> list[tuple[str, list[str], str]]:
    out = []
    for job in _config()["scrape_configs"]:
        for rc in job.get("metric_relabel_configs", []):
            if rc.get("action") == "drop":
                out.append((job["job_name"], rc["source_labels"], _regex(rc)))
    return out


_NAME = re.compile(r"\b([a-zA-Z_:][a-zA-Z0-9_:]*)\b")


def _rule_exprs() -> list[str]:
    rules = _doc("ConfigMap", "prometheus-rules")["data"]
    exprs = []
    for text in rules.values():
        for group in (yaml.safe_load(text) or {}).get("groups", []):
            for rule in group.get("rules", []):
                exprs.append(str(rule.get("expr", "")))
    return exprs


def _dashboard_exprs() -> list[str]:
    exprs = []

    def walk(node):
        if isinstance(node, dict):
            for k, v in node.items():
                if k == "expr" and isinstance(v, str):
                    exprs.append(v)
                else:
                    walk(v)
        elif isinstance(node, list):
            for v in node:
                walk(v)

    for path in sorted(DASHBOARDS.glob("*.json")):
        walk(json.loads(path.read_text()))
    return exprs


def _referenced_names() -> set[str]:
    names = set()
    for expr in _rule_exprs() + _dashboard_exprs():
        names.update(m.group(1) for m in _NAME.finditer(expr) if "_" in m.group(1))
    return names


def test_there_are_metric_drops_to_check():
    assert _metric_drops(), "no metric_relabel_configs drops found; the budget depends on them"


def test_name_only_drops_remove_nothing_a_rule_or_dashboard_reads():
    referenced = _referenced_names()
    for job, sources, regex in _metric_drops():
        if sources != ["__name__"]:
            continue
        hit = sorted(n for n in referenced if re.fullmatch(regex, n))
        assert not hit, f"job {job} drops {hit}, which a rule or dashboard reads"


def test_bucket_drops_keep_sum_and_count():
    for job, sources, regex in _metric_drops():
        if sources != ["__name__"]:
            continue
        for base in ("kyverno_policy_execution_duration_seconds", "apiserver_request_sli_duration_seconds"):
            for suffix in ("_sum", "_count"):
                assert not re.fullmatch(regex, base + suffix), f"job {job} drops {base + suffix}"


def test_kubelet_drop_only_touches_the_control_plane_and_keeps_kubelet_series():
    drops = [(s, r) for j, s, r in _metric_drops() if j == "kubernetes-nodes"]
    assert drops, "kubernetes-nodes lost its control-plane duplicate drop"
    for sources, regex in drops:
        assert sources[0] == "node_role_kubernetes_io_control_plane"
        # a worker node's kubelet never matches
        assert not re.fullmatch(regex, ";apiserver_client_certificate_expiration_seconds_bucket")
        # kubelet-owned series on the control plane stay
        for name in ("kubelet_running_pods", "kubelet_volume_stats_used_bytes", "container_cpu_usage_seconds_total"):
            assert not re.fullmatch(regex, "true;" + name)
        # the duplicated registry goes
        assert re.fullmatch(regex, "true;apiserver_request_duration_seconds_bucket")
        assert re.fullmatch(regex, "true;etcd_request_duration_seconds_bucket")
    # ...and the API server job, which keeps the one copy, does not drop the
    # latency histogram the duplicate removal relies on.
    for job, sources, regex in _metric_drops():
        if job == "kubernetes-apiservers":
            assert not re.fullmatch(regex, "apiserver_request_duration_seconds_bucket")


# --- optional: the real config parser --------------------------------------------


def test_promtool_accepts_the_config():
    template = os.environ.get("ENCLII_PROMTOOL_CHECK_CONFIG")
    if not template:
        pytest.skip("ENCLII_PROMTOOL_CHECK_CONFIG not set; structural tests still ran")
    text = _doc("ConfigMap", "prometheus-config")["data"]["prometheus.yml"]
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / "prometheus.yml"
        path.write_text(text, encoding="utf-8")
        os.chmod(tmp, 0o755)
        os.chmod(path, 0o644)
        cmd = template.format(dir=tmp, name=path.name)
        try:
            proc = subprocess.run(shlex.split(cmd), capture_output=True, text=True, timeout=180)
        except (OSError, subprocess.TimeoutExpired) as exc:
            pytest.skip(f"promtool could not run: {exc}")
        output = proc.stdout + proc.stderr
        if proc.returncode != 0 and ("Unable to find image" in output or "pull access denied" in output):
            pytest.skip(f"promtool image unavailable: {output[-300:]}")
        assert proc.returncode == 0, output
