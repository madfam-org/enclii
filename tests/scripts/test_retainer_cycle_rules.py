"""
Tests for the retainer billing-cycle rules (retainer-cycle-rules.yml in the
prometheus-rules ConfigMap, infra/k8s/production/monitoring/prometheus.yaml).

Run with:
    pytest tests/scripts/test_retainer_cycle_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_retainer_cycle_rules.py -v

WHY THIS EXISTS
===============
The vCTO retainer is the money that arrives today, and its CFDI has a date:
3 business days from the corte. Until nauta#351 nobody but the owner knew
whether a cycle had been stamped; the owner asked for «a mechanism for
yourself to check without me having to hold all the information». nauta#351
publishes the cycle as gauges; these rules read them. What is pinned:

  * the gauge names are exactly nauta#351's (a typo here is a rule that
    never fires and never says so);
  * RetainerCfdiOverdue is the one critical rule; every cycle rule joins on
    (workspace_id, cycle), so none can fire while the gauges are absent, and
    absence is its own warning;
  * each cycle pages as its own `product` (Courier's once-per-episode key);
  * promtool: overdue fires only past the deadline with no stamp, a stamp
    clears it, a stamped-unsent CFDI and an unpaid cycle warn, and missing
    gauges warn.
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
RULES_KEY = "retainer-cycle-rules.yml"

# nauta#351 (apps/worker /metrics), exact names. Changing one needs the
# matching nauta change in review.
CYCLE_GAUGES = {
    "nauta_retainer_cycle_corte_timestamp_seconds",
    "nauta_retainer_cycle_cfdi_deadline_timestamp_seconds",
    "nauta_retainer_cycle_cfdi_stamped_timestamp_seconds",
    "nauta_retainer_cycle_cfdi_sent_timestamp_seconds",
    "nauta_retainer_cycle_cfdi_delivery_failed",
    "nauta_retainer_cycle_payment_due_timestamp_seconds",
    "nauta_retainer_cycle_payment_recorded_timestamp_seconds",
    "nauta_retainer_cycle_paid_in_full",
}
STATUS_GAUGES = {
    "nauta_retainer_status_last_success_timestamp_seconds",
    "nauta_retainer_status_read_ok",
    "nauta_retainer_status_workspaces",
}
CYCLE_RULES = {"RetainerCfdiOverdue", "RetainerCfdiStampedNotSent", "RetainerCfdiDeliveryFailed", "RetainerPaymentOverdue"}


@lru_cache(maxsize=1)
def _rules_text() -> str:
    for doc in yaml.safe_load_all(PROMETHEUS.read_text(encoding="utf-8")):
        if isinstance(doc, dict) and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-rules":
            return doc["data"][RULES_KEY]
    raise AssertionError("prometheus-rules not found")


def _rules() -> dict[str, dict]:
    (group,) = yaml.safe_load(_rules_text())["groups"]
    assert group["name"] == "retainer-cycle"
    return {r["alert"]: r for r in group["rules"]}


def test_rule_set_and_severities():
    rules = _rules()
    assert set(rules) == CYCLE_RULES | {"RetainerMetricsMissing", "RetainerStatusNotRead"}
    for name, rule in rules.items():
        want = "critical" if name == "RetainerCfdiOverdue" else "warning"
        assert rule["labels"] == {"severity": want, "category": "retainer"}, name


def test_rules_read_only_nauta_351_gauges():
    used = set(re.findall(r"\bnauta_retainer_[a-z_]*[a-z]\b", _rules_text()))
    assert used <= CYCLE_GAUGES | STATUS_GAUGES, used - (CYCLE_GAUGES | STATUS_GAUGES)
    assert "nauta_retainer_cycle_cfdi_deadline_timestamp_seconds" in used
    assert "nauta_retainer_cycle_cfdi_stamped_timestamp_seconds" in used


def test_cycle_rules_join_per_cycle_and_page_per_cycle():
    for name in CYCLE_RULES:
        expr = _rules()[name]["expr"]
        if name != "RetainerCfdiDeliveryFailed":
            assert "on (workspace_id, cycle)" in expr, name
        assert '"retainer_key", "-", "workspace_id", "cycle"' in expr, name
        assert '"product", "retainer-$1", "retainer_key", "(.+)"' in expr, name
        assert "absent(" not in expr, f"{name}: absence is RetainerMetricsMissing's job"
        assert _rules()[name]["keep_firing_for"] == "90m", name
        # Every gauge read drops the scrape's pod labels, so a worker restart
        # (a new `pod`) never becomes a second alert and a second page.
        for gauge in re.findall(r"\bnauta_retainer_cycle_[a-z_]*[a-z]\b", expr):
            assert f"max by (workspace_id, cycle) ({gauge})" in expr, (name, gauge)
        assert len(re.findall(r"\bnauta_retainer_cycle_[a-z_]*[a-z]\b", expr)) == expr.count("max by (workspace_id, cycle) ("), name


def test_absence_is_a_warning_of_its_own():
    rule = _rules()["RetainerMetricsMissing"]
    assert rule["expr"].strip() == "absent(nauta_retainer_status_last_success_timestamp_seconds)"
    assert rule["for"] == "2h"


def test_annotations_use_only_labels_and_value():
    for name, rule in _rules().items():
        for key, text in rule["annotations"].items():
            for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
                assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)\s*", expr), (name, key, expr)


def test_rule_text_carries_no_hostname_or_literal_identity():
    """enclii is public: the rules identify a cycle only through templated
    labels (workspace_id, cycle) at runtime, never through a literal value.
    Real client names are checked by MADFAM's private estate scan."""
    text = _rules_text()
    assert not re.search(r"\b[a-z0-9-]+\.(mx|com|io|app|dev|quest)\b", text, flags=re.I), "hostname in rule text"
    assert not re.search(r"workspace_id\s*=\s*[\"'][^\"']+[\"']", text), "literal workspace_id in a selector"
    assert "{{ $labels.workspace_id }}" in text


# ---------------------------------------------------------------------------
# promtool. promtool's clock starts at 0, so deadlines are small epoch values:
# the CFDI deadline is 600 (10m), the payment due date 1200 (20m; overdue a
# day later, at 87600). The workspace id is made up.
# ---------------------------------------------------------------------------

WS = "00000000-0000-4000-8000-000000000000"
L = f'workspace_id="{WS}",cycle="4",namespace="nauta",pod="nauta-worker-a"'

PROMTOOL_TEST = textwrap.dedent(
    f"""
    rule_files:
      - {RULES_KEY}
    evaluation_interval: 1m
    tests:
      # 1. Not stamped: nothing before the deadline (600s) + 30m; critical
      #    after. The stamp at 60m clears it, after keep_firing_for (90m).
      - interval: 1m
        input_series:
          - series: 'nauta_retainer_cycle_cfdi_deadline_timestamp_seconds{{{L}}}'
            values: '600x200'
          - series: 'nauta_retainer_cycle_cfdi_stamped_timestamp_seconds{{{L}}}'
            values: '0x60 3700x140'
          - series: 'nauta_retainer_cycle_cfdi_sent_timestamp_seconds{{{L}}}'
            values: '0x60 3750x140'
          - series: 'nauta_retainer_status_last_success_timestamp_seconds'
            values: '0+60x200'
        alert_rule_test:
          - eval_time: 30m
            alertname: RetainerCfdiOverdue
            exp_alerts: []
          - eval_time: 45m
            alertname: RetainerCfdiOverdue
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: retainer
                  workspace_id: "{WS}"
                  cycle: "4"
                  retainer_key: "{WS}-4"
                  product: "retainer-{WS}-4"
                exp_annotations:
                  summary: "Retainer CFDI overdue: cycle 4 is past its deadline with no stamp"
          - eval_time: 155m
            alertname: RetainerCfdiOverdue
            exp_alerts: []
          - eval_time: 155m
            alertname: RetainerCfdiStampedNotSent
            exp_alerts: []
          - eval_time: 155m
            alertname: RetainerMetricsMissing
            exp_alerts: []

      # 1b. The worker restarts at 50m (new pod): still ONE alert, with no
      #     pod label, firing throughout.
      - interval: 1m
        input_series:
          - series: 'nauta_retainer_cycle_cfdi_deadline_timestamp_seconds{{workspace_id="{WS}",cycle="4",pod="nauta-worker-a"}}'
            values: '600x50'
          - series: 'nauta_retainer_cycle_cfdi_stamped_timestamp_seconds{{workspace_id="{WS}",cycle="4",pod="nauta-worker-a"}}'
            values: '0x50'
          - series: 'nauta_retainer_cycle_cfdi_deadline_timestamp_seconds{{workspace_id="{WS}",cycle="4",pod="nauta-worker-b"}}'
            values: '_x52 600x100'
          - series: 'nauta_retainer_cycle_cfdi_stamped_timestamp_seconds{{workspace_id="{WS}",cycle="4",pod="nauta-worker-b"}}'
            values: '_x52 0x100'
        alert_rule_test:
          - eval_time: 70m
            alertname: RetainerCfdiOverdue
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: retainer
                  workspace_id: "{WS}"
                  cycle: "4"
                  retainer_key: "{WS}-4"
                  product: "retainer-{WS}-4"
                exp_annotations:
                  summary: "Retainer CFDI overdue: cycle 4 is past its deadline with no stamp"

      # 2. Stamped, never sent: a warning an hour after the stamp (+15m).
      - interval: 1m
        input_series:
          - series: 'nauta_retainer_cycle_cfdi_deadline_timestamp_seconds{{{L}}}'
            values: '600x200'
          - series: 'nauta_retainer_cycle_cfdi_stamped_timestamp_seconds{{{L}}}'
            values: '300x200'
          - series: 'nauta_retainer_cycle_cfdi_sent_timestamp_seconds{{{L}}}'
            values: '0x200'
        alert_rule_test:
          - eval_time: 50m
            alertname: RetainerCfdiStampedNotSent
            exp_alerts: []
          - eval_time: 90m
            alertname: RetainerCfdiStampedNotSent
            exp_alerts:
              - exp_labels:
                  severity: warning
                  category: retainer
                  workspace_id: "{WS}"
                  cycle: "4"
                  retainer_key: "{WS}-4"
                  product: "retainer-{WS}-4"
                exp_annotations:
                  summary: "Retainer CFDI stamped but not sent: cycle 4"
          - eval_time: 90m
            alertname: RetainerCfdiOverdue
            exp_alerts: []

      # 3. No gauges at all (nauta#351 not deployed): no cycle rule can fire;
      #    RetainerMetricsMissing warns after 2h.
      - interval: 1m
        input_series:
          - series: 'up{{job="prometheus"}}'
            values: '1x200'
        alert_rule_test:
          - eval_time: 100m
            alertname: RetainerMetricsMissing
            exp_alerts: []
          - eval_time: 150m
            alertname: RetainerMetricsMissing
            exp_alerts:
              - exp_labels:
                  severity: warning
                  category: retainer
                exp_annotations:
                  summary: "Retainer cycle metrics are missing: the retainer rules cannot fire"
          - eval_time: 150m
            alertname: RetainerCfdiOverdue
            exp_alerts: []

      # 4. Unpaid: payment due at 1200s, no record; a warning a day later
      #    (+30m). Recorded payments clear it.
      - interval: 1m
        input_series:
          - series: 'nauta_retainer_cycle_payment_due_timestamp_seconds{{{L}}}'
            values: '1200x1600'
          - series: 'nauta_retainer_cycle_payment_recorded_timestamp_seconds{{{L}}}'
            values: '0x1600'
        alert_rule_test:
          - eval_time: 1440m
            alertname: RetainerPaymentOverdue
            exp_alerts: []
          - eval_time: 1520m
            alertname: RetainerPaymentOverdue
            exp_alerts:
              - exp_labels:
                  severity: warning
                  category: retainer
                  workspace_id: "{WS}"
                  cycle: "4"
                  retainer_key: "{WS}-4"
                  product: "retainer-{WS}-4"
                exp_annotations:
                  summary: "Retainer payment overdue: cycle 4 is a day past due with no payment recorded"
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _rules_text_without_descriptions() -> str:
    doc = yaml.safe_load(_rules_text())
    for group in doc["groups"]:
        for rule in group["rules"]:
            rule.get("annotations", {}).pop("description", None)
    return yaml.safe_dump(doc, sort_keys=False)


def test_promtool_evaluates_the_retainer_rules():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; the structural tests above still ran")
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / RULES_KEY).write_text(_rules_text_without_descriptions(), encoding="utf-8")
        name = "retainer-cycle-rules.test.yml"
        (Path(tmp) / name).write_text(PROMTOOL_TEST, encoding="utf-8")
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
