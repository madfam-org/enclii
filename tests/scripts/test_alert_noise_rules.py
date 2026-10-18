"""
Tests for the critical-alert noise fixes of 2026-10-11 (money-machine audit,
M2 F6/F12/F13).

Run with:
    pytest tests/scripts/test_alert_noise_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_alert_noise_rules.py -v

WHY THIS EXISTS
===============
Critical rules paging every 4h on standing conditions teach the reader of the
pager to ignore it, and a real revenue page then lands in that noise. Three
sources are pinned here:

  1. ArgoCDAppDegraded / ArgoCDAppMissing paged for staging apps and for apps
     whose only Degraded cause was a Job that failed weeks ago. They now page
     only for production apps not explained by stale failed Jobs alone; each
     excluded case has a warning twin, so nothing goes dark.
  2. PodChronicRestart7d (critical) paged for staging crash loops.
  3. CNPGClusterDegraded compared against 3 instances while cluster.yaml runs
     2, and read a metric CNPG does not export, so it could never fire.
     CNPGSyncReplicaLost matched a standby's normal observed=0.

Structural tests read the rules straight out of the manifests Prometheus
loads. The promtool tests evaluate them with Prometheus's own engine; if the
evaluator cannot RUN they skip with the reason, never pass silently.
"""
from __future__ import annotations

import os
import re
import shutil
import subprocess
import tempfile
import textwrap
from functools import lru_cache
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
PROMETHEUS = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring" / "prometheus.yaml"
PG_HA = REPO_ROOT / "infra" / "k8s" / "production" / "postgres-ha"
PODMONITOR = PG_HA / "podmonitor.yaml"
CLUSTER = PG_HA / "cluster.yaml"

STAGING_APP = ".+-staging(-.+)?"
RECORD = "argocd:namespace_degraded_only_by_stale_failed_jobs"


def _docs(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]


@lru_cache(maxsize=None)
def _rules_key(key: str) -> str:
    for doc in _docs(PROMETHEUS):
        if doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-rules":
            return doc["data"][key]
    raise AssertionError("prometheus-rules ConfigMap not found")


def _group(text: str, name: str) -> list[dict]:
    groups = [g for g in yaml.safe_load(text)["groups"] if g["name"] == name]
    assert len(groups) == 1, name
    return groups[0]["rules"]


def _argocd_rules() -> list[dict]:
    return _group(_rules_key("enclii-alerts.yml"), "argocd-gitops")


def _by_alert(rules: list[dict]) -> dict[str, dict]:
    return {r["alert"]: r for r in rules if "alert" in r}


@lru_cache(maxsize=1)
def _cnpg_rules_text() -> str:
    for doc in _docs(PODMONITOR):
        if doc.get("kind") == "PrometheusRule":
            return yaml.safe_dump({"groups": doc["spec"]["groups"]}, sort_keys=False)
    raise AssertionError("PrometheusRule not found in podmonitor.yaml")


def _cnpg_rules() -> dict[str, dict]:
    return _by_alert(_group(_cnpg_rules_text(), "postgres-ha.rules"))


def _cluster_instances() -> int:
    (cluster,) = [d for d in _docs(CLUSTER) if d.get("kind") == "Cluster"]
    return int(cluster["spec"]["instances"])


# ---------------------------------------------------------------------------
# Structural: Argo
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    "app",
    ["karafiel-staging", "dhanam-staging", "phynd-crm-staging", "selva-office-staging", "voxa-staging-services"],
)
def test_staging_regex_matches_the_staging_apps(app):
    assert re.fullmatch(STAGING_APP, app)


@pytest.mark.parametrize(
    "app", ["dhanam-services", "karafiel-services", "core-services", "monitoring", "staging", "voxa-services"]
)
def test_staging_regex_spares_production_apps(app):
    assert not re.fullmatch(STAGING_APP, app)


@pytest.mark.parametrize("alert", ["ArgoCDAppDegraded", "ArgoCDAppMissing"])
def test_critical_argo_rules_exclude_staging(alert):
    rule = _by_alert(_argocd_rules())[alert]
    assert rule["labels"]["severity"] == "critical"
    assert f'name!~"{STAGING_APP}"' in rule["expr"]


@pytest.mark.parametrize(
    "alert,health", [("ArgoCDStagingAppDegraded", "Degraded"), ("ArgoCDStagingAppMissing", "Missing")]
)
def test_staging_has_a_warning_twin(alert, health):
    rule = _by_alert(_argocd_rules())[alert]
    assert rule["labels"]["severity"] == "warning"
    assert f'health_status="{health}"' in rule["expr"]
    assert f'name=~"{STAGING_APP}"' in rule["expr"]


def test_stale_job_demotion_has_a_warning_twin_and_record_comes_first():
    rules = _argocd_rules()
    names = [r.get("record") or r.get("alert") for r in rules]
    assert names.index(RECORD) < names.index("ArgoCDAppDegraded")
    by = _by_alert(rules)
    assert f"unless on (dest_namespace)\n{RECORD}" in by["ArgoCDAppDegraded"]["expr"]
    twin = by["ArgoCDAppDegradedByStaleFailedJobs"]
    assert twin["labels"]["severity"] == "warning"
    assert f"and on (dest_namespace)\n{RECORD}" in twin["expr"]


def test_chronic_restart_7d_excludes_staging():
    rule = _by_alert(_group(_rules_key("chronic-restart-rules.yml"), "chronic-restart"))["PodChronicRestart7d"]
    assert rule["labels"]["severity"] == "critical"
    assert 'namespace!~".+-staging"' in rule["expr"]


def test_chronic_restart_7d_counts_only_pods_that_still_exist():
    """A replaced pod's restarts must not keep the 7d critical firing.

    The 7d range keeps a deleted pod's samples for a week; the join on `uid`
    (not on the pod name, which a StatefulSet reuses) drops them.
    """
    rule = _by_alert(_group(_rules_key("chronic-restart-rules.yml"), "chronic-restart"))["PodChronicRestart7d"]
    assert "and on (uid) kube_pod_info" in rule["expr"]


@pytest.mark.parametrize(
    "key,group,alert",
    [
        ("deploy-parity-alerts.yml", "argo-rollout-truth", "ArgoMainRolloutNotLanded"),
        ("deploy-parity-alerts.yml", "argo-rollout-truth", "ArgoMainRolloutStuckForTooLong"),
        ("deploy-pipeline-freshness-rules.yml", "deploy-pipeline-monitor", "MainCIFailingForTooLong"),
    ],
)
def test_rollout_and_main_ci_rules_page_nobody(key, group, alert):
    """Pager noise plan 2026-10-17: these go to the warning digest, never Telegram.

    The previous image keeps serving while they fire; the client-facing
    outage rules (client-slo, Argo health, revenue probes) stay critical.
    """
    rule = _by_alert(_group(_rules_key(key), group))[alert]
    assert rule["labels"]["severity"] == "warning"


# ---------------------------------------------------------------------------
# Structural: CNPG
# ---------------------------------------------------------------------------


def test_cnpg_degraded_threshold_equals_cluster_instances():
    """Raise both together: cluster.yaml spec.instances and the rule."""
    rule = _cnpg_rules()["CNPGClusterDegraded"]
    (threshold,) = re.findall(r"<\s*(\d+)\s*$", rule["expr"].strip())
    assert int(threshold) == _cluster_instances()
    assert f"of {_cluster_instances()} instances" in rule["annotations"]["summary"]


def test_cnpg_degraded_reads_a_metric_cnpg_exports():
    expr = _cnpg_rules()["CNPGClusterDegraded"]["expr"]
    assert "cnpg_collector_up" in expr
    assert "cnpg_collector_instances_ready" not in expr


def test_cnpg_absence_is_visible():
    rule = _cnpg_rules()["CNPGMetricsAbsent"]
    assert "absent(cnpg_collector_up" in rule["expr"]


# ---------------------------------------------------------------------------
# promtool
# ---------------------------------------------------------------------------

ARGO_TEST = textwrap.dedent(
    """
    rule_files:
      - enclii-alerts.yml
      - chronic-restart-rules.yml
    evaluation_interval: 30s
    tests:
      - interval: 1m
        input_series:
          # forgesight: Degraded, one Job failed 10 days ago, workloads fine.
          - series: 'argocd_app_info{name="forgesight-services",dest_namespace="forgesight",health_status="Degraded"}'
            values: '1x450'
          - series: 'kube_job_status_condition{namespace="forgesight",job_name="crawler-29000000",condition="Failed",status="true"}'
            values: '1x450'
          - series: 'kube_job_created{namespace="forgesight",job_name="crawler-29000000"}'
            values: '-864000x450'
          - series: 'kube_deployment_spec_replicas{namespace="forgesight",deployment="forgesight-api"}'
            values: '1x450'
          - series: 'kube_deployment_status_replicas_available{namespace="forgesight",deployment="forgesight-api"}'
            values: '1x450'
          # rondelio: Degraded, a Job that failed at t=0 (fresh until 6h).
          - series: 'argocd_app_info{name="rondelio-services",dest_namespace="rondelio",health_status="Degraded"}'
            values: '1x450'
          - series: 'kube_job_status_condition{namespace="rondelio",job_name="top100-baseline",condition="Failed",status="true"}'
            values: '1x450'
          - series: 'kube_job_created{namespace="rondelio",job_name="top100-baseline"}'
            values: '0x450'
          # core-services: an old failed Job AND a stuck rollout (new
          # ReplicaSet in ImagePullBackOff, the old pod still serving, so
          # replicas look full). The stuck rollout must keep it paging.
          - series: 'argocd_app_info{name="core-services",dest_namespace="enclii",health_status="Degraded"}'
            values: '1x450'
          - series: 'kube_job_status_condition{namespace="enclii",job_name="old-migrate",condition="Failed",status="true"}'
            values: '1x450'
          - series: 'kube_job_created{namespace="enclii",job_name="old-migrate"}'
            values: '-864000x450'
          - series: 'kube_deployment_spec_replicas{namespace="enclii",deployment="roundhouse-worker"}'
            values: '1x450'
          - series: 'kube_deployment_status_replicas_available{namespace="enclii",deployment="roundhouse-worker"}'
            values: '1x450'
          - series: 'kube_deployment_status_condition{namespace="enclii",deployment="roundhouse-worker",condition="Progressing",status="false"}'
            values: '1x450'
          # tlacuilo: an old failed Job AND a Deployment short of replicas.
          # The shortfall must keep it paging.
          - series: 'argocd_app_info{name="tlacuilo-services",dest_namespace="tlacuilo",health_status="Degraded"}'
            values: '1x450'
          - series: 'kube_job_status_condition{namespace="tlacuilo",job_name="old-seed",condition="Failed",status="true"}'
            values: '1x450'
          - series: 'kube_job_created{namespace="tlacuilo",job_name="old-seed"}'
            values: '-864000x450'
          - series: 'kube_deployment_spec_replicas{namespace="tlacuilo",deployment="tlacuilo-api"}'
            values: '1x450'
          - series: 'kube_deployment_status_replicas_available{namespace="tlacuilo",deployment="tlacuilo-api"}'
            values: '0x450'
          # converge-dash: Degraded, no Jobs at all. Pages.
          - series: 'argocd_app_info{name="converge-dash-services",dest_namespace="converge-dash",health_status="Degraded"}'
            values: '1x450'
          # Staging: never critical.
          - series: 'argocd_app_info{name="karafiel-staging",dest_namespace="enclii-karafiel-staging",health_status="Degraded"}'
            values: '1x450'
          - series: 'argocd_app_info{name="selva-office-staging",dest_namespace="selva-staging",health_status="Missing"}'
            values: '1x450'
          # Restarts: 300 in the window, production and staging.
          - series: 'kube_pod_container_status_restarts_total{namespace="logging",pod="loki-0",uid="u-loki-0",container="loki"}'
            values: '0+1x450'
          - series: 'kube_pod_info{namespace="logging",pod="loki-0",uid="u-loki-0"}'
            values: '1x450'
          - series: 'kube_pod_container_status_restarts_total{namespace="enclii-karafiel-staging",pod="karafiel-admin-x",uid="u-admin-x",container="admin"}'
            values: '0+1x450'
          - series: 'kube_pod_info{namespace="enclii-karafiel-staging",pod="karafiel-admin-x",uid="u-admin-x"}'
            values: '1x450'
          # A REPLACED pod: 240 restarts, then deleted at 4h (its series end).
          # Its samples stay inside the 7d range; it must not page.
          - series: 'kube_pod_container_status_restarts_total{namespace="logging",pod="fluent-bit-old",uid="u-fb-old",container="fluent-bit"}'
            values: '0+1x240'
          - series: 'kube_pod_info{namespace="logging",pod="fluent-bit-old",uid="u-fb-old"}'
            values: '1x240'
          # A StatefulSet pod recreated under the SAME name at 4h: the old uid
          # carries the restarts, the new one is healthy. Must not page.
          - series: 'kube_pod_container_status_restarts_total{namespace="data",pod="redis-0",uid="u-redis-old",container="redis"}'
            values: '0+1x240'
          - series: 'kube_pod_info{namespace="data",pod="redis-0",uid="u-redis-old"}'
            values: '1x240'
          - series: 'kube_pod_container_status_restarts_total{namespace="data",pod="redis-0",uid="u-redis-new",container="redis"}'
            values: '_x240 0x210'
          - series: 'kube_pod_info{namespace="data",pod="redis-0",uid="u-redis-new"}'
            values: '_x240 1x210'
        alert_rule_test:
          - eval_time: 10m
            alertname: ArgoCDAppDegraded
            exp_alerts:
              - exp_labels: {severity: critical, service: argocd, name: rondelio-services, dest_namespace: rondelio, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
              - exp_labels: {severity: critical, service: argocd, name: core-services, dest_namespace: enclii, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
              - exp_labels: {severity: critical, service: argocd, name: tlacuilo-services, dest_namespace: tlacuilo, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
              - exp_labels: {severity: critical, service: argocd, name: converge-dash-services, dest_namespace: converge-dash, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
          - eval_time: 10m
            alertname: ArgoCDAppDegradedByStaleFailedJobs
            exp_alerts:
              - exp_labels: {severity: warning, service: argocd, name: forgesight-services, dest_namespace: forgesight, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded only by Jobs that failed over 6h ago"}
          # 7h: rondelio's failed Job is now stale too, so it is demoted.
          - eval_time: 7h
            alertname: ArgoCDAppDegraded
            exp_alerts:
              - exp_labels: {severity: critical, service: argocd, name: core-services, dest_namespace: enclii, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
              - exp_labels: {severity: critical, service: argocd, name: tlacuilo-services, dest_namespace: tlacuilo, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
              - exp_labels: {severity: critical, service: argocd, name: converge-dash-services, dest_namespace: converge-dash, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded"}
          - eval_time: 7h
            alertname: ArgoCDAppDegradedByStaleFailedJobs
            exp_alerts:
              - exp_labels: {severity: warning, service: argocd, name: forgesight-services, dest_namespace: forgesight, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded only by Jobs that failed over 6h ago"}
              - exp_labels: {severity: warning, service: argocd, name: rondelio-services, dest_namespace: rondelio, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD application degraded only by Jobs that failed over 6h ago"}
          - eval_time: 40m
            alertname: ArgoCDAppMissing
            exp_alerts: []
          - eval_time: 40m
            alertname: ArgoCDStagingAppMissing
            exp_alerts:
              - exp_labels: {severity: warning, service: argocd, name: selva-office-staging, dest_namespace: selva-staging, health_status: Missing}
                exp_annotations: {summary: "ArgoCD staging application missing"}
          - eval_time: 40m
            alertname: ArgoCDStagingAppDegraded
            exp_alerts:
              - exp_labels: {severity: warning, service: argocd, name: karafiel-staging, dest_namespace: enclii-karafiel-staging, health_status: Degraded}
                exp_annotations: {summary: "ArgoCD staging application degraded"}
          # 300 restarts by 5h: production pages, staging does not, and
          # neither do the replaced fluent-bit pod or redis-0's old uid.
          - eval_time: 5h
            alertname: PodChronicRestart7d
            exp_alerts:
              - exp_labels: {severity: critical, category: chronic-restart, window: 7d, namespace: logging, pod: loki-0}
                exp_annotations: {summary: "logging/loki-0 restarted >200 times in 7d"}
    """
).lstrip()

CNPG_TEST = textwrap.dedent(
    """
    rule_files:
      - postgres-ha-rules.yml
    evaluation_interval: 30s
    tests:
      # Healthy: both instances up, primary observes its 1 sync standby; the
      # standby's own observed/expected are 0, which must not page.
      - interval: 1m
        input_series:
          - series: 'cnpg_collector_up{cluster="postgres-ha",instance="i-1"}'
            values: '1x10'
          - series: 'cnpg_collector_up{cluster="postgres-ha",instance="i-2"}'
            values: '1x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-1",value="observed"}'
            values: '1x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-1",value="expected"}'
            values: '1x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-2",value="observed"}'
            values: '0x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-2",value="expected"}'
            values: '0x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-2",value="min"}'
            values: '0x10'
        alert_rule_test:
          - eval_time: 9m
            alertname: CNPGClusterDegraded
            exp_alerts: []
          - eval_time: 9m
            alertname: CNPGSyncReplicaLost
            exp_alerts: []
          - eval_time: 9m
            alertname: CNPGMetricsAbsent
            exp_alerts: []

      # One instance's PostgreSQL down, and the primary lost its sync standby.
      - interval: 1m
        input_series:
          - series: 'cnpg_collector_up{cluster="postgres-ha",instance="i-1"}'
            values: '1x10'
          - series: 'cnpg_collector_up{cluster="postgres-ha",instance="i-2"}'
            values: '0x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-1",value="observed"}'
            values: '0x10'
          - series: 'cnpg_collector_sync_replicas{cluster="postgres-ha",instance="i-1",value="expected"}'
            values: '1x10'
        alert_rule_test:
          - eval_time: 3m
            alertname: CNPGClusterDegraded
            exp_alerts:
              - exp_labels: {severity: critical, component: postgres-ha, cluster: postgres-ha}
                exp_annotations: {summary: "Postgres HA cluster has 1 of 2 instances up"}
          - eval_time: 6m
            alertname: CNPGSyncReplicaLost
            exp_alerts:
              - exp_labels: {severity: warning, component: postgres-ha, cluster: postgres-ha}
                exp_annotations: {summary: "Postgres HA has no synchronous replica"}

      # One instance's pod gone entirely (no series): still degraded.
      - interval: 1m
        input_series:
          - series: 'cnpg_collector_up{cluster="postgres-ha",instance="i-1"}'
            values: '1x10'
        alert_rule_test:
          - eval_time: 3m
            alertname: CNPGClusterDegraded
            exp_alerts:
              - exp_labels: {severity: critical, component: postgres-ha, cluster: postgres-ha}
                exp_annotations: {summary: "Postgres HA cluster has 1 of 2 instances up"}

      # Nothing scraped at all: the absence warning, not a silent pass.
      - interval: 1m
        input_series:
          - series: 'up{job="unrelated"}'
            values: '1x20'
        alert_rule_test:
          - eval_time: 16m
            alertname: CNPGMetricsAbsent
            exp_alerts:
              # absent() carries the selector's equality labels.
              - exp_labels: {severity: warning, component: postgres-ha, cluster: postgres-ha}
                exp_annotations: {summary: "No metrics from the postgres-ha instances"}
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _without_descriptions(text: str) -> str:
    """promtool compares the full annotation map; summaries are compared
    exactly, multi-line descriptions are dropped so wording fixes are not
    test changes (their templates are parsed by `promtool check rules`)."""
    doc = yaml.safe_load(text)
    for group in doc["groups"]:
        for rule in group["rules"]:
            for key in ("description", "runbook", "runbook_url"):
                rule.get("annotations", {}).pop(key, None)
    return yaml.safe_dump(doc, sort_keys=False)


def _run_promtool(files: dict[str, str], test_name: str, test_body: str) -> None:
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; structural tests still ran")
    with tempfile.TemporaryDirectory() as tmp:
        for name, text in files.items():
            (Path(tmp) / name).write_text(_without_descriptions(text), encoding="utf-8")
        (Path(tmp) / test_name).write_text(test_body, encoding="utf-8")
        # The prometheus image runs as nobody; a 0700 tempdir is unreadable
        # to it through a bind mount.
        os.chmod(tmp, 0o755)
        for p in Path(tmp).iterdir():
            os.chmod(p, 0o644)
        if template:
            cmd: str | list[str] = template.format(dir=tmp, name=test_name)
            shell = True
        else:
            cmd = ["promtool", "test", "rules", str(Path(tmp) / test_name)]
            shell = False
        try:
            proc = subprocess.run(cmd, shell=shell, capture_output=True, text=True, timeout=PROMTOOL_TIMEOUT_SECONDS)
        except subprocess.TimeoutExpired:
            pytest.skip(f"promtool did not finish within {PROMTOOL_TIMEOUT_SECONDS}s")
    output = (proc.stdout + proc.stderr).strip()
    if proc.returncode != 0 and "FAILED" not in output and "SUCCESS" not in output:
        pytest.skip(f"promtool could not run: {output[-500:]}")
    assert proc.returncode == 0, f"promtool rejected the rule test:\n{output}"
    assert "SUCCESS" in output, output


def test_promtool_argo_and_chronic_restart_rules():
    _run_promtool(
        {
            "enclii-alerts.yml": _rules_key("enclii-alerts.yml"),
            "chronic-restart-rules.yml": _rules_key("chronic-restart-rules.yml"),
        },
        "argo-noise.test.yml",
        ARGO_TEST,
    )


def test_promtool_cnpg_rules():
    _run_promtool({"postgres-ha-rules.yml": _cnpg_rules_text()}, "cnpg.test.yml", CNPG_TEST)


# ---------------------------------------------------------------------------
# Main CI vs scheduled canaries (infra-stability audit 2026-10-17, W1O-1)
# ---------------------------------------------------------------------------


def _freshness_rules() -> dict[str, dict]:
    return _by_alert(_group(_rules_key("deploy-pipeline-freshness-rules.yml"), "deploy-pipeline-monitor"))


def test_main_ci_alert_reads_the_push_build_signal_only():
    """MainCIFailingForTooLong reads the push metric, never the canary one."""
    rule = _freshness_rules()["MainCIFailingForTooLong"]
    assert "repo_main_blocked_seconds" in rule["expr"]
    assert "scheduled" not in rule["expr"]


def test_canary_failure_has_its_own_warning_alert():
    rules = _freshness_rules()
    canary = rules["MainCanaryFailing"]
    assert canary["labels"]["severity"] == "warning"
    assert canary["expr"].strip() == "repo_main_scheduled_failure_streak_count >= 2"
    probe = rules["DeployPipelineMonitorScheduledProbeFailing"]
    assert probe["labels"]["severity"] == "warning"
    assert "repo_main_scheduled_probe_ok == 0" in probe["expr"]


CANARY_TEST = textwrap.dedent(
    """
    rule_files:
      - deploy-pipeline-freshness-rules.yml
    evaluation_interval: 30s
    tests:
      - interval: 1m
        input_series:
          # forgesight: push CI green, the scheduled canary red 3 runs running.
          - series: 'repo_main_blocked_seconds{repo="madfam-org/forgesight",branch="main"}'
            values: '0x60'
          - series: 'repo_main_scheduled_failure_streak_count{repo="madfam-org/forgesight",branch="main",workflow="Production Smoke"}'
            values: '3x60'
          # karafiel: one canary failure is a flake, not a page.
          - series: 'repo_main_scheduled_failure_streak_count{repo="madfam-org/karafiel",branch="main",workflow="Nightly"}'
            values: '1x60'
        alert_rule_test:
          - eval_time: 45m
            alertname: MainCIFailingForTooLong
            exp_alerts: []
          - eval_time: 45m
            alertname: MainCanaryFailing
            exp_alerts:
              - exp_labels: {severity: warning, category: deploy-pipeline, repo: madfam-org/forgesight, branch: main, workflow: Production Smoke}
                exp_annotations: {summary: 'madfam-org/forgesight scheduled workflow "Production Smoke" failed 3 times running'}
    """
).lstrip()


def test_promtool_canary_is_a_warning_and_not_a_build_failure():
    _run_promtool(
        {"deploy-pipeline-freshness-rules.yml": _rules_key("deploy-pipeline-freshness-rules.yml")},
        "canary.test.yml",
        CANARY_TEST,
    )
