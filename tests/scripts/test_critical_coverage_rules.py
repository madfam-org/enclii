"""
Tests for critical-coverage-rules.yml (alerting truth check, 2026-11-04).

Run with:
    pytest tests/scripts/test_critical_coverage_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_critical_coverage_rules.py -v

WHY THIS EXISTS
===============
A read-only check of both Prometheus instances and Alertmanager on 2026-11-04
found that node down, a full volume, a crash-looping platform pod, a failed
backup CronJob outside two namespaces, certificate expiry outside one probe
job, Prometheus retention below 7 days and ARC queue saturation had no live
rule. Several of the rules that did exist could not fire (the ARC ones read
metrics nothing exports). An alert that never fires looks exactly like a
system that never breaks, so every rule here is driven through its failure
state AND its healthy state with promtool. Structural tests always run; the
promtool tests skip with a reason when no evaluator is available.
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
PROMETHEUS = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring" / "prometheus.yaml"
KEY = "critical-coverage-rules.yml"


@lru_cache(maxsize=None)
def _rules_text() -> str:
    with PROMETHEUS.open("r", encoding="utf-8") as fh:
        for doc in yaml.safe_load_all(fh):
            if isinstance(doc, dict) and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-rules":
                return doc["data"][KEY]
    raise AssertionError("prometheus-rules ConfigMap not found")


def _rules() -> list[dict]:
    return [r for g in yaml.safe_load(_rules_text())["groups"] for r in g["rules"]]


def _severities(alert: str) -> set[str]:
    return {r["labels"]["severity"] for r in _rules() if r["alert"] == alert}


# ---------------------------------------------------------------------------
# Structural: each failure mode the operator expects to hear about has a rule
# ---------------------------------------------------------------------------

REQUIRED = {
    # failure mode: (alert, severities it must carry)
    "node down": ("NodeNotReady", {"critical"}),
    "monitoring blind on a node": ("NodeExporterMissing", {"warning"}),
    "object-state rules blind": ("KubeStateMetricsDown", {"critical"}),
    "volume full": ("PersistentVolumeFillingUp", {"warning", "critical"}),
    "platform crash loop": ("PlatformPodCrashLooping", {"critical"}),
    "backup CronJob failed": ("BackupCronJobFailed", {"critical"}),
    "public certificate expiry": ("ProbedTLSCertificateExpiring", {"warning", "critical"}),
    "cluster certificate expiry": ("KubeClientCertificateExpiration", {"warning", "critical"}),
    "retention below 7 days": ("PrometheusRetentionBelow7Days", {"warning"}),
    "ARC queue saturation": ("ARCPoolSaturated", {"warning", "critical"}),
    "ARC runners unschedulable": ("ARCRunnersCannotSchedule", {"warning"}),
}


@pytest.mark.parametrize("mode", sorted(REQUIRED))
def test_failure_mode_has_a_rule(mode):
    alert, severities = REQUIRED[mode]
    assert _severities(alert) == severities, mode


def test_every_rule_routes_and_explains_itself():
    for r in _rules():
        assert r["labels"]["severity"] in {"warning", "critical"}, r["alert"]
        assert r["labels"].get("service"), r["alert"]
        for key in ("summary", "description", "runbook_url"):
            assert r["annotations"].get(key), (r["alert"], key)


def test_paired_rules_can_inhibit_each_other():
    """Alertmanager inhibits warning by critical on equal (alertname, service)."""
    by_name: dict[str, list[dict]] = {}
    for r in _rules():
        by_name.setdefault(r["alert"], []).append(r)
    for name, rules in by_name.items():
        if len(rules) > 1:
            assert len({r["labels"]["service"] for r in rules}) == 1, name
            assert sorted(r["labels"]["severity"] for r in rules) == ["critical", "warning"], name


def test_arc_rules_require_a_fresh_heartbeat():
    """The pushgateway keeps the last pushed value forever; stale gauges must not page."""
    for r in _rules():
        if r["alert"].startswith("ARC"):
            assert "arc_pool_health_last_run_timestamp_seconds" in r["expr"], r["alert"]


def test_backup_rule_does_not_double_page_named_backups():
    expr = next(r["expr"] for r in _rules() if r["alert"] == "BackupCronJobFailed")
    for covered in ("k3s-datastore", "github-repos", "cloudflare-config", "argocd-secrets", "backup-verify", "pgbackrest-", "karafiel-"):
        assert covered in expr, covered


def test_retention_rule_reads_only_the_main_prometheus():
    """rules-eval keeps 24 h by design and must not trip a 7-day rule."""
    expr = next(r["expr"] for r in _rules() if r["alert"] == "PrometheusRetentionBelow7Days")
    assert 'job="prometheus"' in expr


# ---------------------------------------------------------------------------
# promtool: every rule fires on its failure state and stays quiet when healthy
# ---------------------------------------------------------------------------

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _without_descriptions(text: str) -> str:
    doc = yaml.safe_load(text)
    for group in doc["groups"]:
        for rule in group["rules"]:
            for key in ("description", "runbook_url"):
                rule.get("annotations", {}).pop(key, None)
    return yaml.safe_dump(doc, sort_keys=False)


def _run_promtool(test_body: str) -> None:
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; structural tests still ran")
    name = "critical-coverage.test.yml"
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / KEY).write_text(_without_descriptions(_rules_text()), encoding="utf-8")
        (Path(tmp) / name).write_text(test_body, encoding="utf-8")
        os.chmod(tmp, 0o755)
        for p in Path(tmp).iterdir():
            os.chmod(p, 0o644)
        if template:
            cmd: str | list[str] = template.format(dir=tmp, name=name)
            shell = True
        else:
            cmd = ["promtool", "test", "rules", str(Path(tmp) / name)]
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


def _test(series: str, evals: str) -> str:
    return textwrap.dedent(
        """
        rule_files:
          - critical-coverage-rules.yml
        evaluation_interval: 30s
        tests:
          - interval: 1m
            input_series:
        {series}
            alert_rule_test:
        {evals}
        """
    ).format(
        series=textwrap.indent(textwrap.dedent(series).strip("\n"), "      "),
        evals=textwrap.indent(textwrap.dedent(evals).strip("\n"), "      "),
    ).lstrip()


def test_promtool_node_rules():
    _run_promtool(_test(
        """
        - series: 'kube_node_status_condition{condition="Ready",status="true",node="down"}'
          values: '0x20'
        - series: 'kube_node_status_condition{condition="Ready",status="true",node="fine"}'
          values: '1x20'
        - series: 'kube_node_info{node="down"}'
          values: '1x20'
        - series: 'kube_node_info{node="fine"}'
          values: '1x20'
        - series: 'up{job="node-exporter",instance="fine:9100"}'
          values: '1x20'
        - series: 'up{job="kube-state-metrics",instance="ksm:8080"}'
          values: '1x20'
        """,
        """
        - eval_time: 3m
          alertname: NodeNotReady
          exp_alerts: []
        - eval_time: 7m
          alertname: NodeNotReady
          exp_alerts:
            - exp_labels: {severity: critical, service: kubernetes, category: node-health, node: down}
              exp_annotations: {summary: "Node down is not Ready"}
        - eval_time: 17m
          alertname: NodeExporterMissing
          exp_alerts:
            - exp_labels: {severity: warning, service: monitoring, category: node-health}
              exp_annotations: {summary: "1 node(s) have no reachable node-exporter"}
        - eval_time: 17m
          alertname: KubeStateMetricsDown
          exp_alerts: []
        """,
    ))


def test_promtool_kube_state_metrics_down():
    _run_promtool(_test(
        """
        - series: 'up{job="kube-state-metrics",instance="ksm:8080"}'
          values: '0x20'
        """,
        """
        - eval_time: 8m
          alertname: KubeStateMetricsDown
          exp_alerts: []
        - eval_time: 12m
          alertname: KubeStateMetricsDown
          exp_alerts:
            - exp_labels: {severity: critical, service: monitoring, category: alerting-pipeline}
              exp_annotations: {summary: "kube-state-metrics is down: object-state alerts are blind"}
        """,
    ))


def test_promtool_volume_rules():
    _run_promtool(_test(
        """
        # 90% full: warning only.
        - series: 'kubelet_volume_stats_used_bytes{job="kubernetes-nodes",namespace="data",persistentvolumeclaim="ninety"}'
          values: '90x30'
        - series: 'kubelet_volume_stats_capacity_bytes{job="kubernetes-nodes",namespace="data",persistentvolumeclaim="ninety"}'
          values: '100x30'
        # 97% full, reported by two jobs: one warning and one critical, not two of each.
        - series: 'kubelet_volume_stats_used_bytes{job="kubernetes-nodes",namespace="monitoring",persistentvolumeclaim="full"}'
          values: '97x30'
        - series: 'kubelet_volume_stats_used_bytes{job="kubernetes-apiservers",namespace="monitoring",persistentvolumeclaim="full"}'
          values: '97x30'
        - series: 'kubelet_volume_stats_capacity_bytes{job="kubernetes-nodes",namespace="monitoring",persistentvolumeclaim="full"}'
          values: '100x30'
        - series: 'kubelet_volume_stats_capacity_bytes{job="kubernetes-apiservers",namespace="monitoring",persistentvolumeclaim="full"}'
          values: '100x30'
        # Half full: silent.
        - series: 'kubelet_volume_stats_used_bytes{job="kubernetes-nodes",namespace="data",persistentvolumeclaim="half"}'
          values: '50x30'
        - series: 'kubelet_volume_stats_capacity_bytes{job="kubernetes-nodes",namespace="data",persistentvolumeclaim="half"}'
          values: '100x30'
        """,
        """
        - eval_time: 7m
          alertname: PersistentVolumeFillingUp
          exp_alerts:
            - exp_labels: {severity: critical, service: storage, category: disk-capacity, namespace: monitoring, persistentvolumeclaim: full}
              exp_annotations: {summary: "PVC monitoring/full is 97% full"}
        - eval_time: 17m
          alertname: PersistentVolumeFillingUp
          exp_alerts:
            - exp_labels: {severity: warning, service: storage, category: disk-capacity, namespace: data, persistentvolumeclaim: ninety}
              exp_annotations: {summary: "PVC data/ninety is 90% full"}
            - exp_labels: {severity: warning, service: storage, category: disk-capacity, namespace: monitoring, persistentvolumeclaim: full}
              exp_annotations: {summary: "PVC monitoring/full is 97% full"}
            - exp_labels: {severity: critical, service: storage, category: disk-capacity, namespace: monitoring, persistentvolumeclaim: full}
              exp_annotations: {summary: "PVC monitoring/full is 97% full"}
        """,
    ))


def test_promtool_platform_crash_loop():
    _run_promtool(_test(
        """
        - series: 'kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff",namespace="argocd",pod="argocd-repo-server-1",container="repo-server"}'
          values: '1x20'
        # A product namespace: PodCrashLooping's business, not a page.
        - series: 'kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff",namespace="tulana",pod="tulana-api-1",container="api"}'
          values: '1x20'
        # A Job pod in a platform namespace: its CronJob alerts own it.
        - series: 'kube_pod_container_status_waiting_reason{reason="CrashLoopBackOff",namespace="enclii",pod="status-recorder-1-abc",container="recorder"}'
          values: '1x20'
        - series: 'kube_pod_owner{namespace="enclii",pod="status-recorder-1-abc",owner_kind="Job",owner_name="status-recorder-1"}'
          values: '1x20'
        """,
        """
        - eval_time: 10m
          alertname: PlatformPodCrashLooping
          exp_alerts: []
        - eval_time: 16m
          alertname: PlatformPodCrashLooping
          exp_alerts:
            - exp_labels: {severity: critical, service: kubernetes, category: platform-health, namespace: argocd, pod: argocd-repo-server-1, container: repo-server}
              exp_annotations: {summary: "Platform pod argocd/argocd-repo-server-1 (repo-server) is crash-looping"}
        """,
    ))


def test_promtool_backup_cronjob_failed():
    # Timestamps are seconds since the test's t=0. Evaluated at 3h (10800 s).
    _run_promtool(_test(
        """
        # Failed last night: schedule newer than the newest success.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="symbiosis-hcm",cronjob="hcm-db-backup"}'
          values: '3600x200'
        - series: 'kube_cronjob_status_last_successful_time{namespace="symbiosis-hcm",cronjob="hcm-db-backup"}'
          values: '1800x200'
        # Never succeeded.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="selva",cronjob="postgres-backup"}'
          values: '3600x200'
        # Succeeded: silent.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="dhanam",cronjob="dhanam-documents-backup"}'
          values: '3600x200'
        - series: 'kube_cronjob_status_last_successful_time{namespace="dhanam",cronjob="dhanam-documents-backup"}'
          values: '3700x200'
        # Still running (a retry in progress): silent.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="tezca",cronjob="tezca-db-backup"}'
          values: '3600x200'
        - series: 'kube_cronjob_status_active{namespace="tezca",cronjob="tezca-db-backup"}'
          values: '1x200'
        # Scheduled 30 minutes ago and not done yet: inside the grace hour.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="longhorn-system",cronjob="weekly-s3-backup"}'
          values: '9000x200'
        # Suspended on purpose: silent.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="madlab",cronjob="old-backup"}'
          values: '3600x200'
        - series: 'kube_cronjob_spec_suspend{namespace="madlab",cronjob="old-backup"}'
          values: '1x200'
        # Already paged by name (BackupJobFailed, KarafielDocumentBackupUnproven): silent here.
        - series: 'kube_cronjob_status_last_schedule_time{namespace="data",cronjob="github-repos-backup"}'
          values: '3600x200'
        - series: 'kube_cronjob_info{namespace="data",cronjob="github-repos-backup"}'
          values: '1x200'
        - series: 'kube_cronjob_status_last_schedule_time{namespace="karafiel",cronjob="karafiel-documents-backup"}'
          values: '3600x200'
        - series: 'kube_cronjob_info{namespace="karafiel",cronjob="karafiel-documents-backup"}'
          values: '1x200'
        """,
        """
        - eval_time: 2h
          alertname: BackupCronJobFailed
          exp_alerts: []
        - eval_time: 3h
          alertname: BackupCronJobFailed
          exp_alerts:
            - exp_labels: {severity: critical, service: backups, category: data-safety, namespace: symbiosis-hcm, cronjob: hcm-db-backup}
              exp_annotations: {summary: "Backup CronJob symbiosis-hcm/hcm-db-backup: last run did not succeed"}
            - exp_labels: {severity: critical, service: backups, category: data-safety, namespace: selva, cronjob: postgres-backup}
              exp_annotations: {summary: "Backup CronJob selva/postgres-backup: last run did not succeed"}
        """,
    ))


def test_promtool_probed_certificate_expiry():
    # probe_ssl_earliest_cert_expiry is an absolute unix time; t=0 is the epoch here.
    _run_promtool(_test(
        """
        - series: 'probe_ssl_earliest_cert_expiry{job="blackbox-revenue",instance="https://ten-days.example"}'
          values: '864000x100'
        - series: 'probe_ssl_earliest_cert_expiry{job="blackbox-revenue",instance="https://three-days.example"}'
          values: '259200x100'
        # Covered by GeometryTLSCertExpiring: silent here.
        - series: 'probe_ssl_earliest_cert_expiry{job="blackbox-geometry",instance="https://geometry.example"}'
          values: '259200x100'
        # Sixty days: silent.
        - series: 'probe_ssl_earliest_cert_expiry{job="blackbox-revenue",instance="https://fine.example"}'
          values: '5184000x100'
        """,
        """
        - eval_time: 40m
          alertname: ProbedTLSCertificateExpiring
          exp_alerts:
            - exp_labels: {severity: critical, service: tls, category: certificate-expiry, job: blackbox-revenue, instance: "https://three-days.example"}
              exp_annotations: {summary: "TLS certificate for https://three-days.example expires in 2d 23h 20m 0s"}
        - eval_time: 65m
          alertname: ProbedTLSCertificateExpiring
          exp_alerts:
            - exp_labels: {severity: warning, service: tls, category: certificate-expiry, job: blackbox-revenue, instance: "https://ten-days.example"}
              exp_annotations: {summary: "TLS certificate for https://ten-days.example expires in 9d 22h 55m 0s"}
            - exp_labels: {severity: warning, service: tls, category: certificate-expiry, job: blackbox-revenue, instance: "https://three-days.example"}
              exp_annotations: {summary: "TLS certificate for https://three-days.example expires in 2d 22h 55m 0s"}
            - exp_labels: {severity: critical, service: tls, category: certificate-expiry, job: blackbox-revenue, instance: "https://three-days.example"}
              exp_annotations: {summary: "TLS certificate for https://three-days.example expires in 2d 22h 55m 0s"}
        """,
    ))


def test_promtool_kube_client_certificate_expiry():
    # Every request presents a certificate with 7-30 days left: warning, not critical.
    _run_promtool(_test(
        """
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="604800"}'
          values: '0x90'
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="2592000"}'
          values: '0+10x90'
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="7776000"}'
          values: '0+10x90'
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="+Inf"}'
          values: '0+10x90'
        - series: 'apiserver_client_certificate_expiration_seconds_count{job="kubernetes-apiservers",instance="api"}'
          values: '0+10x90'
        """,
        """
        - eval_time: 70m
          alertname: KubeClientCertificateExpiration
          exp_alerts:
            - exp_labels: {severity: warning, service: kubernetes, category: certificate-expiry, job: kubernetes-apiservers, instance: api}
              exp_annotations: {summary: "A client certificate used against the API server expires within 30 days"}
        """,
    ))


def test_promtool_kube_client_certificate_healthy():
    _run_promtool(_test(
        """
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="2592000"}'
          values: '0x90'
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="7776000"}'
          values: '0x90'
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="31104000"}'
          values: '0+10x90'
        - series: 'apiserver_client_certificate_expiration_seconds_bucket{job="kubernetes-apiservers",instance="api",le="+Inf"}'
          values: '0+10x90'
        - series: 'apiserver_client_certificate_expiration_seconds_count{job="kubernetes-apiservers",instance="api"}'
          values: '0+10x90'
        """,
        """
        - eval_time: 70m
          alertname: KubeClientCertificateExpiration
          exp_alerts: []
        """,
    ))


def test_promtool_retention_below_seven_days():
    _run_promtool(_test(
        """
        # Holds under 7 days and size retention keeps deleting: fires.
        - series: 'prometheus_tsdb_lowest_timestamp_seconds{job="prometheus",instance="capped"}'
          values: '0x90'
        - series: 'prometheus_tsdb_size_retentions_total{job="prometheus",instance="capped"}'
          values: '0+1x90'
        - series: 'prometheus_tsdb_time_retentions_total{job="prometheus",instance="capped"}'
          values: '0x90'
        # Holds under 7 days but deletes nothing (still filling up): silent.
        - series: 'prometheus_tsdb_lowest_timestamp_seconds{job="prometheus",instance="filling"}'
          values: '0x90'
        - series: 'prometheus_tsdb_size_retentions_total{job="prometheus",instance="filling"}'
          values: '0x90'
        - series: 'prometheus_tsdb_time_retentions_total{job="prometheus",instance="filling"}'
          values: '0x90'
        """,
        """
        - eval_time: 30m
          alertname: PrometheusRetentionBelow7Days
          exp_alerts: []
        - eval_time: 70m
          alertname: PrometheusRetentionBelow7Days
          exp_alerts:
            - exp_labels: {severity: warning, service: prometheus, category: alerting-pipeline, job: prometheus, instance: capped}
              exp_annotations: {summary: "Prometheus is deleting data while it holds only 1h 10m 0s"}
        """,
    ))


def test_promtool_arc_saturation():
    # arc_pool_health_last_run_timestamp_seconds tracks the clock ('0+60' per 1m
    # sample) for a live detector, and stops for a dead one.
    _run_promtool(_test(
        """
        - series: 'arc_pool_current_runners{scale_set="blue"}'
          values: '20x150'
        - series: 'arc_pool_max_runners{scale_set="blue"}'
          values: '20x150'
        - series: 'arc_pool_pending_runners{scale_set="blue"}'
          values: '3x150'
        - series: 'arc_pool_health_last_run_timestamp_seconds{scale_set="blue"}'
          values: '0+60x150'
        # Same numbers, but the detector died at t=0: stale gauges never page.
        - series: 'arc_pool_current_runners{scale_set="stale"}'
          values: '20x150'
        - series: 'arc_pool_max_runners{scale_set="stale"}'
          values: '20x150'
        - series: 'arc_pool_pending_runners{scale_set="stale"}'
          values: '3x150'
        - series: 'arc_pool_health_last_run_timestamp_seconds{scale_set="stale"}'
          values: '0x150'
        # Busy but not full: silent.
        - series: 'arc_pool_current_runners{scale_set="green"}'
          values: '1x150'
        - series: 'arc_pool_max_runners{scale_set="green"}'
          values: '2x150'
        - series: 'arc_pool_pending_runners{scale_set="green"}'
          values: '0x150'
        - series: 'arc_pool_health_last_run_timestamp_seconds{scale_set="green"}'
          values: '0+60x150'
        """,
        """
        - eval_time: 22m
          alertname: ARCRunnersCannotSchedule
          exp_alerts:
            - exp_labels: {severity: warning, service: arc, category: ci-capacity, scale_set: blue}
              exp_annotations: {summary: "3 ARC runner(s) on blue have been pending for 20 minutes"}
        - eval_time: 32m
          alertname: ARCPoolSaturated
          exp_alerts:
            - exp_labels: {severity: warning, service: arc, category: ci-capacity, scale_set: blue}
              exp_annotations: {summary: "ARC scale set blue has been at maxRunners for 30 minutes"}
        - eval_time: 122m
          alertname: ARCPoolSaturated
          exp_alerts:
            - exp_labels: {severity: warning, service: arc, category: ci-capacity, scale_set: blue}
              exp_annotations: {summary: "ARC scale set blue has been at maxRunners for 30 minutes"}
            - exp_labels: {severity: critical, service: arc, category: ci-capacity, scale_set: blue}
              exp_annotations: {summary: "ARC scale set blue has been at maxRunners for 2 hours"}
        """,
    ))
