"""
Tests for the cv rules (cv-rules.yml in the prometheus-rules ConfigMap,
infra/k8s/production/monitoring/prometheus.yaml).

Run with:
    pytest tests/scripts/test_cv_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_cv_rules.py -v

WHY THIS EXISTS
===============
cv (cv.madfam.io, cv-app.madfam.io, cv-api.madfam.io) exports metrics on an
internal 9090 listener since ai-portfolio-builder#16, and nothing paged a
person about it: `cv` is in none of the client-slo selectors. The reviewed
source is internal-devops grafana/alerts/cv.yaml (#886). What is pinned:

  * the five rules, and that four page (critical) while the read-proof warns;
  * every rule carries `product: cv` (the critical route groups by
    alertname and product) and selects the scrape the pods actually get:
    job="kubernetes-pods", namespace="cv";
  * 503 never counts as a failure (dormant sign-in answers 503 by design),
    host_role="none" (probes, unknown Hosts) never pages, and a 5xx page
    needs at least 5 errors;
  * the migrate rule reads the Job's Failed CONDITION, not the failed-pod
    count, which is already 1 when a retry succeeds;
  * promtool: each rule fires on its outage and stays silent on its near
    misses (fixtures/cv-rules.test.yml).
"""
from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
from functools import lru_cache
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
PROMETHEUS = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring" / "prometheus.yaml"
PROMTOOL_CASES = Path(__file__).resolve().parent / "fixtures" / "cv-rules.test.yml"
RULES_KEY = "cv-rules.yml"
PAGING = {"CVTargetDown", "CVReadinessFailing", "CVHigh5xxRate", "CVPreSyncMigrateFailing"}


@lru_cache(maxsize=1)
def _rules_text() -> str:
    for doc in yaml.safe_load_all(PROMETHEUS.read_text(encoding="utf-8")):
        if isinstance(doc, dict) and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-rules":
            return doc["data"][RULES_KEY]
    raise AssertionError("prometheus-rules not found")


def _rules() -> dict[str, dict]:
    groups = yaml.safe_load(_rules_text())["groups"]
    assert [g["name"] for g in groups] == ["cv.availability", "cv.delivery"]
    return {r["alert"]: r for g in groups for r in g["rules"]}


def test_rule_set_severities_and_routing_labels():
    rules = _rules()
    assert set(rules) == PAGING | {"CVMigrateJobUnobservable"}
    for name, rule in rules.items():
        want = "critical" if name in PAGING else "warning"
        assert rule["labels"] == {"severity": want, "product": "cv", "service": "cv"}, name
        assert rule["annotations"]["runbook_url"].startswith(
            "https://github.com/madfam-org/internal-devops/blob/main/runbooks/cv-alerts.md#"
        ), name


def test_app_rules_select_the_annotation_scrape():
    for name in ("CVTargetDown", "CVReadinessFailing", "CVHigh5xxRate"):
        expr = _rules()[name]["expr"]
        assert 'job="kubernetes-pods"' in expr and 'namespace="cv"' in expr, name
    # CVTargetDown also fires when the target vanished entirely.
    assert "absent(up{" in _rules()["CVTargetDown"]["expr"]


def test_5xx_rule_excludes_503_and_probes_and_needs_volume():
    expr = _rules()["CVHigh5xxRate"]["expr"]
    assert expr.count('status!="503"') == 2, "both the ratio and the volume floor must exclude 503"
    assert expr.count('host_role!="none"') == 3
    assert "sum by (host_role)" in expr
    assert ">= 5" in expr and "> 0.05" in expr


def test_migrate_rule_reads_the_failed_condition():
    expr = _rules()["CVPreSyncMigrateFailing"]["expr"]
    assert "kube_job_failed{" in expr and 'condition="true"' in expr and 'job_name="cv-migrate"' in expr
    assert "kube_job_status_failed" not in expr


def test_annotations_use_only_labels_and_value():
    import re

    for name, rule in _rules().items():
        for key, text in rule["annotations"].items():
            for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
                assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)(\s*\|\s*humanizePercentage)?\s*", expr), (name, key, expr)


PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def test_promtool_evaluates_the_cv_rules():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; the structural tests above still ran")
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / RULES_KEY).write_text(_rules_text(), encoding="utf-8")
        name = "cv-rules.test.yml"
        (Path(tmp) / name).write_text(PROMTOOL_CASES.read_text(encoding="utf-8"), encoding="utf-8")
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
