"""
Tests for the per-project CloudNativePG health alerts (stabilization wave
2026-10-26).

Run with:
    pytest tests/scripts/test_cnpg_project_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_cnpg_project_rules.py -v

WHY THIS EXISTS
===============
On 2026-10-24 two CNPG incidents in a project namespace paged nobody, because
the only CNPG health rules watched the platform `postgres-ha` cluster:

  1. every crea-sim cluster sat in «Instance Status Extraction Error» while
     PostgreSQL answered and cnpg_collector_up stayed 1;
  2. one instance kept a broken volume mount after a node fault.

The phase exists only on the Cluster object, so kube-state-metrics publishes
it (kube-state-metrics.yaml, --custom-resource-state-config) and the
`enclii-addon-db.cnpg-health` group in addon-db-rules.yaml reads it. Pinned
here:

  - every kube_cnpg_cluster_* series a rule reads is one the kube-state-metrics
    config actually defines, and kube-state-metrics may list Clusters;
  - hibernated clusters (0 instances on purpose) are excluded by CNPG's own
    confirmation, not by the annotation alone, so a cluster annotated for
    hibernation that CNPG never acted on still pages;
  - `CNPGInstanceStatusUnreachable` keeps its name and its `namespace` and
    `cluster` labels: Selva's self-healing intake (selva-office RFC 0047)
    keys on them;
  - promtool evaluates the real rules: incident 1 fires, a phase change does
    not reset the clock, hibernated and healthy clusters stay quiet.

If the evaluator cannot RUN, the promtool cases skip with the reason, never
pass silently; the structural tests still gate.
"""
from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import textwrap
from functools import lru_cache
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
ADDON_RULES = MONITORING / "addon-db-rules.yaml"
KSM = MONITORING / "kube-state-metrics.yaml"
KUSTOMIZATION = MONITORING / "kustomization.yaml"

GROUP = "enclii-addon-db.cnpg-health"
PROJECT_SELECTOR = 'namespace=~"project-.*"'
HEALTHY = "Cluster in healthy state"
SELVA_ALERT = "CNPGInstanceStatusUnreachable"
CRS_FLAG = "--custom-resource-state-config="


def _docs(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]


@lru_cache(maxsize=None)
def _rule_doc() -> dict:
    docs = [d for d in _docs(ADDON_RULES) if d.get("kind") == "PrometheusRule"]
    assert len(docs) == 1
    return docs[0]


def _group() -> dict:
    groups = [g for g in _rule_doc()["spec"]["groups"] if g["name"] == GROUP]
    assert len(groups) == 1, GROUP
    return groups[0]


def _rules() -> dict[str, dict]:
    return {r["alert"]: r for r in _group()["rules"] if "alert" in r}


@lru_cache(maxsize=None)
def _ksm_docs() -> tuple[dict, ...]:
    return tuple(_docs(KSM))


def _ksm_args() -> list[str]:
    deploy = [d for d in _ksm_docs() if d["kind"] == "Deployment"]
    assert len(deploy) == 1
    containers = deploy[0]["spec"]["template"]["spec"]["containers"]
    return containers[0]["args"]


@lru_cache(maxsize=None)
def _crs_config() -> dict:
    flags = [a for a in _ksm_args() if a.startswith(CRS_FLAG)]
    assert len(flags) == 1, "exactly one inline --custom-resource-state-config"
    return yaml.safe_load(flags[0][len(CRS_FLAG):])


def _cnpg_resource() -> dict:
    resources = [r for r in _crs_config()["spec"]["resources"]
                 if r["groupVersionKind"]["group"] == "postgresql.cnpg.io"]
    assert len(resources) == 1
    return resources[0]


def _defined_series() -> set[str]:
    res = _cnpg_resource()
    return {f'{res["metricNamePrefix"]}_{m["name"]}' for m in res["metrics"]}


# ---------------------------------------------------------------------------
# kube-state-metrics publishes what the rules read
# ---------------------------------------------------------------------------

def test_crs_config_is_the_documented_schema():
    cfg = _crs_config()
    assert cfg["kind"] == "CustomResourceStateMetrics"
    gvk = _cnpg_resource()["groupVersionKind"]
    assert gvk == {"group": "postgresql.cnpg.io", "version": "v1", "kind": "Cluster"}


def test_every_series_carries_namespace_and_cluster():
    labels = _cnpg_resource()["labelsFromPath"]
    assert labels["namespace"] == ["metadata", "namespace"]
    assert labels["cluster"] == ["metadata", "name"]


def test_info_metric_reads_phase_and_cnpg_hibernation_confirmation():
    info = {m["name"]: m for m in _cnpg_resource()["metrics"]}["info"]
    assert info["each"]["type"] == "Info"
    paths = info["each"]["info"]["labelsFromPath"]
    assert paths["phase"] == ["status", "phase"]
    # CNPG's condition, not the annotation: the annotation is the request,
    # the condition is the operator confirming it acted.
    assert paths["hibernated"] == ["status", "conditions", "[type=cnpg.io/hibernation]", "status"]
    assert paths["hibernation"] == ["metadata", "annotations", "cnpg.io/hibernation"]


def test_ready_instances_gauge_reads_zero_when_unset():
    gauges = {m["name"]: m["each"]["gauge"] for m in _cnpg_resource()["metrics"] if m["each"]["type"] == "Gauge"}
    assert gauges["spec_instances"]["path"] == ["spec", "instances"]
    assert gauges["status_ready_instances"]["path"] == ["status", "readyInstances"]
    assert gauges["status_ready_instances"]["nilIsZero"] is True


def test_rules_read_only_series_the_config_defines():
    defined = _defined_series()
    exprs = " ".join(r["expr"] for r in _group()["rules"])
    import re

    used = set(re.findall(r"\bkube_cnpg_cluster_[a-z_]+", exprs))
    assert used, "the group reads no kube_cnpg_cluster_* series"
    assert used <= defined, f"rules read series the config does not publish: {sorted(used - defined)}"


def test_ksm_may_list_clusters_and_discover_crds():
    role = [d for d in _ksm_docs() if d["kind"] == "ClusterRole"]
    assert len(role) == 1
    rules = role[0]["rules"]

    def grants(group: str, resource: str) -> set[str]:
        out: set[str] = set()
        for r in rules:
            if group in r.get("apiGroups", []) and resource in r.get("resources", []):
                out |= set(r["verbs"])
        return out

    assert {"list", "watch"} <= grants("postgresql.cnpg.io", "clusters")
    assert {"list", "watch"} <= grants("apiextensions.k8s.io", "customresourcedefinitions")
    # Read-only, and still no Secrets.
    for r in rules:
        assert set(r["verbs"]) <= {"list", "watch"}, r
        assert "secrets" not in r.get("resources", []), r


def test_both_files_are_rendered_by_kustomize():
    resources = yaml.safe_load(KUSTOMIZATION.read_text(encoding="utf-8"))["resources"]
    assert "addon-db-rules.yaml" in resources
    assert "kube-state-metrics.yaml" in resources


# ---------------------------------------------------------------------------
# rule shape
# ---------------------------------------------------------------------------

def test_selva_contract_alert_exists_with_its_labels():
    rule = _rules()[SELVA_ALERT]
    assert rule["labels"]["severity"] == "critical"
    assert "by (namespace, cluster)" in rule["expr"]
    assert f'phase!~"{HEALTHY}|"' in rule["expr"]


@pytest.mark.parametrize("alert", ["CNPGInstanceStatusUnreachable", "CNPGReadyInstancesBelowSpec", "CNPGInstancePostgresDown"])
def test_project_rules_are_scoped_and_page(alert):
    rule = _rules()[alert]
    assert PROJECT_SELECTOR in rule["expr"]
    assert rule["labels"]["severity"] == "critical"
    assert rule["labels"]["component"] == "managed-db-addon"


@pytest.mark.parametrize("alert", ["CNPGInstanceStatusUnreachable", "CNPGReadyInstancesBelowSpec"])
def test_hibernation_excluded_by_cnpg_confirmation_not_annotation(alert):
    expr = _rules()[alert]["expr"]
    assert 'hibernated' in expr and '"True"' in expr
    assert "hibernation=" not in expr and "hibernation!=" not in expr


def test_state_metrics_absence_is_visible():
    rule = _rules()["CNPGClusterStateMetricsAbsent"]
    assert rule["expr"].strip() == "absent(kube_cnpg_cluster_info)"
    assert rule["labels"]["severity"] == "warning"


def test_platform_cluster_rules_untouched():
    # postgres-ha keeps its own rules; this group must not double-page it.
    for rule in _group()["rules"]:
        assert "postgres-ha" not in rule["expr"]


# ---------------------------------------------------------------------------
# promtool
# ---------------------------------------------------------------------------

HEALTH_TEST = textwrap.dedent(
    """
    rule_files:
      - addon-db-rules.yml
    evaluation_interval: 1m
    tests:
      # Incident 1: the operator cannot read instance status. PostgreSQL itself
      # is up (cnpg_collector_up 1) and the instance is Ready, so only the
      # phase says anything. A healthy cluster and a hibernated one beside it
      # stay quiet; a cluster annotated for hibernation that CNPG never
      # confirmed still pages.
      - interval: 1m
        input_series:
          - series: 'kube_cnpg_cluster_info{namespace="project-aa11",cluster="pg-k1",phase="Instance Status Extraction Error: HTTP communication issue"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_spec_instances{namespace="project-aa11",cluster="pg-k1"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_status_ready_instances{namespace="project-aa11",cluster="pg-k1"}'
            values: '1x20'
          - series: 'cnpg_collector_up{namespace="project-aa11",cluster="pg-k1",pod="pg-k1-1",instance="10.0.0.1:9187"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_info{namespace="project-aa11",cluster="pg-k2",phase="Cluster in healthy state"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_spec_instances{namespace="project-aa11",cluster="pg-k2"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_status_ready_instances{namespace="project-aa11",cluster="pg-k2"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_info{namespace="project-aa11",cluster="pg-old",phase="Cluster in healthy state",hibernation="on",hibernated="True"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_spec_instances{namespace="project-aa11",cluster="pg-old"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_status_ready_instances{namespace="project-aa11",cluster="pg-old"}'
            values: '0x20'
          - series: 'kube_cnpg_cluster_info{namespace="project-aa11",cluster="pg-stuck",phase="Instance Status Extraction Error: HTTP communication issue",hibernation="on"}'
            values: '1x20'
          # Outside project-*: not this group's business.
          - series: 'kube_cnpg_cluster_info{namespace="data",cluster="postgres-ha",phase="Waiting for the instances to become active"}'
            values: '1x20'
        alert_rule_test:
          - eval_time: 9m
            alertname: CNPGInstanceStatusUnreachable
            exp_alerts: []
          - eval_time: 11m
            alertname: CNPGInstanceStatusUnreachable
            exp_alerts:
              - exp_labels: {severity: critical, component: managed-db-addon, namespace: project-aa11, cluster: pg-k1}
                exp_annotations:
                  phase: "Instance Status Extraction Error: HTTP communication issue"
                  summary: "CNPG reports cluster project-aa11/pg-k1 unhealthy: Instance Status Extraction Error: HTTP communication issue"
              - exp_labels: {severity: critical, component: managed-db-addon, namespace: project-aa11, cluster: pg-stuck}
                exp_annotations:
                  phase: "Instance Status Extraction Error: HTTP communication issue"
                  summary: "CNPG reports cluster project-aa11/pg-stuck unhealthy: Instance Status Extraction Error: HTTP communication issue"
          - eval_time: 11m
            alertname: CNPGReadyInstancesBelowSpec
            exp_alerts: []
          - eval_time: 11m
            alertname: CNPGInstancePostgresDown
            exp_alerts: []
          - eval_time: 11m
            alertname: CNPGClusterStateMetricsAbsent
            exp_alerts: []

      # The phase moves between two unhealthy values: one alert, and the 10m
      # clock is not reset by the change at 6m. It clears once healthy
      # (kube-state-metrics drops the old phase's series; Prometheus marks it
      # stale on the next scrape, written `stale` here).
      - interval: 1m
        input_series:
          - series: 'kube_cnpg_cluster_info{namespace="project-bb22",cluster="pg-p",phase="Instance Status Extraction Error: HTTP communication issue"}'
            values: '1x5 stale'
          - series: 'kube_cnpg_cluster_info{namespace="project-bb22",cluster="pg-p",phase="Waiting for the instances to become active"}'
            values: '_x5 1x9 stale'
          - series: 'kube_cnpg_cluster_info{namespace="project-bb22",cluster="pg-p",phase="Cluster in healthy state"}'
            values: '_x15 1x10'
        alert_rule_test:
          - eval_time: 11m
            alertname: CNPGInstanceStatusUnreachable
            exp_alerts:
              - exp_labels: {severity: critical, component: managed-db-addon, namespace: project-bb22, cluster: pg-p}
                exp_annotations:
                  phase: "Waiting for the instances to become active"
                  summary: "CNPG reports cluster project-bb22/pg-p unhealthy: Waiting for the instances to become active"
          - eval_time: 16m
            alertname: CNPGInstanceStatusUnreachable
            exp_alerts: []

      # Incident 2: after a node fault the instance's PostgreSQL stops
      # answering (I/O error on its data volume) and the pod is no longer
      # Ready. The phase still reads healthy. The hibernated cluster's 0 ready
      # instances must not page.
      - interval: 1m
        input_series:
          - series: 'kube_cnpg_cluster_info{namespace="project-cc33",cluster="pg-k1",phase="Cluster in healthy state"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_spec_instances{namespace="project-cc33",cluster="pg-k1"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_status_ready_instances{namespace="project-cc33",cluster="pg-k1"}'
            values: '1 1 0x18'
          - series: 'cnpg_collector_up{namespace="project-cc33",cluster="pg-k1",pod="pg-k1-1",instance="10.0.0.2:9187",job="project-cc33/pg-k1"}'
            values: '1 1 0x18'
          - series: 'kube_cnpg_cluster_info{namespace="project-cc33",cluster="pg-old",phase="Cluster in healthy state",hibernation="on",hibernated="True"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_spec_instances{namespace="project-cc33",cluster="pg-old"}'
            values: '1x20'
          - series: 'kube_cnpg_cluster_status_ready_instances{namespace="project-cc33",cluster="pg-old"}'
            values: '0x20'
          # The platform cluster's collector is postgres-ha's rules' business.
          - series: 'cnpg_collector_up{namespace="data",cluster="postgres-ha",pod="postgres-ha-1"}'
            values: '0x20'
        alert_rule_test:
          - eval_time: 8m
            alertname: CNPGInstancePostgresDown
            exp_alerts:
              - exp_labels: {severity: critical, component: managed-db-addon, namespace: project-cc33, cluster: pg-k1, pod: pg-k1-1}
                exp_annotations:
                  summary: "PostgreSQL in project-cc33/pg-k1-1 is not answering its own instance manager"
          - eval_time: 11m
            alertname: CNPGReadyInstancesBelowSpec
            exp_alerts: []
          - eval_time: 13m
            alertname: CNPGReadyInstancesBelowSpec
            exp_alerts:
              - exp_labels: {severity: critical, component: managed-db-addon, namespace: project-cc33, cluster: pg-k1}
                exp_annotations:
                  summary: "Managed database project-cc33/pg-k1 has 1 instance(s) not ready"
          - eval_time: 13m
            alertname: CNPGInstanceStatusUnreachable
            exp_alerts: []

      # kube-state-metrics publishes no Cluster state: the absence warning,
      # not a silent pass.
      - interval: 1m
        input_series:
          - series: 'up{job="kube-state-metrics"}'
            values: '1x40'
        alert_rule_test:
          - eval_time: 31m
            alertname: CNPGClusterStateMetricsAbsent
            exp_alerts:
              - exp_labels: {severity: warning, component: managed-db-addon}
                exp_annotations:
                  summary: "kube-state-metrics publishes no CloudNativePG Cluster state"
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _rules_file_text() -> str:
    """The PrometheusRule's spec as a promtool rule file, the cnpg-health group
    only, multi-line descriptions dropped (promtool compares the full
    annotation map; `promtool check rules` still parses their templates in
    test_promtool_check_rules)."""
    group = yaml.safe_load(yaml.safe_dump(_group()))
    for rule in group["rules"]:
        rule.get("annotations", {}).pop("description", None)
    return yaml.safe_dump({"groups": [group]}, sort_keys=False)


def _promtool(args_for: "callable", files: dict[str, str]) -> str:
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; structural tests still ran")
    with tempfile.TemporaryDirectory() as tmp:
        for name, text in files.items():
            (Path(tmp) / name).write_text(text, encoding="utf-8")
        # The prometheus image runs as nobody; a 0700 tempdir is unreadable
        # to it through a bind mount.
        os.chmod(tmp, 0o755)
        for p in Path(tmp).iterdir():
            os.chmod(p, 0o644)
        cmd, shell = args_for(template, tmp)
        try:
            proc = subprocess.run(cmd, shell=shell, capture_output=True, text=True, timeout=PROMTOOL_TIMEOUT_SECONDS)
        except subprocess.TimeoutExpired:
            pytest.skip(f"promtool did not finish within {PROMTOOL_TIMEOUT_SECONDS}s")
    output = (proc.stdout + proc.stderr).strip()
    if proc.returncode != 0 and "FAILED" not in output and "SUCCESS" not in output:
        pytest.skip(f"promtool could not run: {output[-500:]}")
    assert proc.returncode == 0, f"promtool rejected the rules:\n{output}"
    assert "SUCCESS" in output, output
    return output


def test_promtool_cnpg_health_rules():
    def args_for(template, tmp):
        if template:
            return template.format(dir=tmp, name="cnpg-health.test.yml"), True
        return ["promtool", "test", "rules", str(Path(tmp) / "cnpg-health.test.yml")], False

    _promtool(args_for, {"addon-db-rules.yml": _rules_file_text(), "cnpg-health.test.yml": HEALTH_TEST})


def test_promtool_check_rules_whole_file():
    """Every group in the PrometheusRule, descriptions included, parses."""
    full = yaml.safe_dump({"groups": _rule_doc()["spec"]["groups"]}, sort_keys=False)

    def args_for(template, tmp):
        if template:
            # The CI template is `... test rules /w/{name}`; swap the verb.
            return template.replace("test rules", "check rules").format(dir=tmp, name="addon-db-rules.yml"), True
        return ["promtool", "check", "rules", str(Path(tmp) / "addon-db-rules.yml")], False

    _promtool(args_for, {"addon-db-rules.yml": full})
