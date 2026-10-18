"""
Tests for the PagerPathDrill rule (pager-path-drill-rules.yml in the
prometheus-rules ConfigMap, infra/k8s/production/monitoring/prometheus.yaml).

Run with:
    pytest tests/scripts/test_pager_path_drill.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_pager_path_drill.py -v

WHY THIS EXISTS
===============
Watchdog proves that rules reach Alertmanager. Nothing proved that a
critical page reaches a person except a real outage. PagerPathDrill fires on
a schedule (Mondays 16:00-16:05 UTC, plus a one-off window) as two series,
product drill-a and drill-b, labelled like a revenue probe so the
per-product revenue route groups them apart. What is pinned here:

  * the drill is critical and carries the revenue-probe category, and when
    alertmanager.yaml has the per-product revenue route the drill matches
    it; until then it still reaches the generic critical route;
  * the one-off window is exactly 300 seconds and its constants are epoch
    seconds, whatever the merger sets them to;
  * promtool: two alerts, one per product, inside the weekly window and the
    one-off window, and none outside them (the one-off constants are swapped
    for small values so the evaluation stays short).

If the evaluator cannot RUN the promtool test skips with the reason; it
never passes silently.
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
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
PROMETHEUS = MONITORING / "prometheus.yaml"
ALERTMANAGER = MONITORING / "alertmanager.yaml"

RULES_KEY = "pager-path-drill-rules.yml"
GROUP = "pager-path-drill"
ALERT = "PagerPathDrill"
# The label the per-product revenue route in alertmanager.yaml matches.
CATEGORY = "revenue-probe"
PRODUCTS = ("drill-a", "drill-b")
SUMMARY_PREFIX = "DRILL — pager path test, no action needed"
WINDOW_SECONDS = 300
ONE_OFF = re.compile(
    r"vector\(time\(\)\)\s*>=\s*(?P<start>\d+)\s+and\s+vector\(time\(\)\)\s*<\s*(?P<end>\d+)"
)
WEEKLY = "day_of_week() == 1 and hour() == 16 and minute() < 5"


def _docs(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]


def _configmap(path: Path, name: str) -> dict:
    for doc in _docs(path):
        if doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == name:
            return doc
    raise AssertionError(f"ConfigMap {name!r} not found in {path}")


@lru_cache(maxsize=1)
def _rules_text() -> str:
    return _configmap(PROMETHEUS, "prometheus-rules")["data"][RULES_KEY]


def _rule() -> dict:
    groups = [g for g in yaml.safe_load(_rules_text())["groups"] if g["name"] == GROUP]
    assert len(groups) == 1, f"expected one {GROUP!r} group"
    (rule,) = groups[0]["rules"]
    assert rule["alert"] == ALERT
    return rule


def _window() -> tuple[int, int]:
    found = ONE_OFF.findall(_rule()["expr"])
    assert len(found) == 1, f"expected one one-off window clause, got {found!r}"
    start, end = found[0]
    return int(start), int(end)


# ---------------------------------------------------------------------------
# Structural
# ---------------------------------------------------------------------------


def test_rules_file_is_loaded_by_prometheus():
    """Prometheus loads /etc/prometheus/rules/*.yml, the prometheus-rules keys."""
    cfg = yaml.safe_load(_configmap(PROMETHEUS, "prometheus-config")["data"]["prometheus.yml"])
    assert "/etc/prometheus/rules/*.yml" in cfg["rule_files"]
    assert RULES_KEY.endswith(".yml")


def test_drill_pages_and_says_it_is_a_drill():
    rule = _rule()
    assert rule["labels"]["severity"] == "critical"
    assert rule["labels"]["category"] == CATEGORY
    assert "product" not in rule["labels"], "the two products come from the expression"
    assert rule["annotations"]["summary"].startswith(SUMMARY_PREFIX)
    assert "for" not in rule, "the window is the duration; a `for` would eat into it"


def test_both_products_and_the_weekly_window_are_in_the_expression():
    expr = _rule()["expr"]
    for product in PRODUCTS:
        assert f'label_replace(vector(1), "product", "{product}", "", "")' in expr
    assert WEEKLY in expr
    assert "and on ()" in expr


def test_one_off_window_is_five_minutes_of_epoch_seconds():
    start, end = _window()
    assert end - start == WINDOW_SECONDS, (start, end)
    # Epoch seconds, not milliseconds and not a placeholder like 0.
    assert 1_700_000_000 <= start < 4_000_000_000, start


def test_one_off_window_is_marked_for_whoever_merges():
    assert "ONE-OFF DRILL WINDOW" in _rule()["expr"]


def test_annotations_use_only_labels_and_value():
    for key, text in _rule()["annotations"].items():
        for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
            assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)\s*", expr), (key, expr)


def _alertmanager_routes() -> list[dict]:
    cm = _configmap(ALERTMANAGER, "alertmanager-config")
    return yaml.safe_load(cm["data"]["alertmanager.yml"])["route"]["routes"]


def _matches(labels: dict, matcher: str) -> bool:
    m = re.fullmatch(r'\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!=|!~|=)\s*"(.*)"\s*', matcher)
    assert m, f"unparsed matcher {matcher!r}"
    name, op, value = m.groups()
    have = labels.get(name, "")
    if op == "=":
        return have == value
    if op == "!=":
        return have != value
    if op == "=~":
        return re.fullmatch(value, have) is not None
    return re.fullmatch(value, have) is None


def _first_route_for(labels: dict) -> dict:
    """First matching child route, the way Alertmanager walks them (no continue)."""
    for route in _alertmanager_routes():
        matchers = [str(m) for m in route.get("matchers") or []]
        matchers += [f'{k}="{v}"' for k, v in (route.get("match") or {}).items()]
        if all(_matches(labels, m) for m in matchers):
            return route
    raise AssertionError(f"no child route matches {labels!r}")


@pytest.mark.parametrize("product", PRODUCTS)
def test_drill_reaches_the_critical_receiver_per_product(product):
    """Each drill series pages through critical-receiver (Courier + email).

    With the per-product revenue route (category="revenue-probe") in
    alertmanager.yaml, it is the route the drill takes and it must group by
    product. Before that route exists the drill takes the generic critical
    route; it still pages, as one group.
    """
    labels = dict(_rule()["labels"], alertname=ALERT, product=product)
    route = _first_route_for(labels)
    assert route["receiver"] == "critical-receiver", route
    revenue_routes = [
        r for r in _alertmanager_routes()
        if f'category="{CATEGORY}"' in [str(m) for m in r.get("matchers") or []]
    ]
    if revenue_routes:
        assert route == revenue_routes[0], "the drill must take the per-product revenue route"
        assert "product" in route["group_by"]


# ---------------------------------------------------------------------------
# promtool: evaluate the real rule
# ---------------------------------------------------------------------------

# Swapped in for the one-off constants so the evaluation starts at 0 and stays
# short: the window becomes 01:00-01:05 on 1970-01-01 (a Thursday).
TEST_START, TEST_END = 3600, 3600 + WINDOW_SECONDS


def _alerts_for_both() -> str:
    out = []
    for product in PRODUCTS:
        out.append(
            textwrap.dedent(
                f"""\
                  - exp_labels:
                      severity: critical
                      category: {CATEGORY}
                      service: alertmanager
                      product: {product}
                    exp_annotations:
                      summary: "{SUMMARY_PREFIX} ({product})"
                """
            )
        )
    return textwrap.indent("".join(out), " " * 10)


def _promtool_test() -> str:
    both = _alerts_for_both()
    cases = [
        ("30m", False),       # Thursday 00:30, before the one-off window
        ("62m", True),        # inside the one-off window (01:00-01:05)
        ("66m", False),       # after it
        ("111h59m", False),   # Monday 1970-01-05 15:59 UTC
        ("112h", True),       # Monday 16:00
        ("112h4m", True),     # Monday 16:04
        ("112h5m", False),    # Monday 16:05, window closed
        ("136h2m", False),    # Tuesday 16:02
        ("280h2m", True),     # the next Monday 16:02
    ]
    tests = []
    for at, fires in cases:
        exp = ("\n" + both.rstrip()) if fires else " []"
        tests.append(
            f"      - eval_time: {at}\n"
            f"        alertname: {ALERT}\n"
            f"        exp_alerts:{exp}\n"
        )
    return (
        "rule_files:\n"
        f"  - {RULES_KEY}\n"
        "evaluation_interval: 1m\n"
        "tests:\n"
        "  - interval: 1m\n"
        "    input_series: []\n"
        "    alert_rule_test:\n" + "".join(tests)
    )


def _rules_for_promtool() -> str:
    start, end = _window()
    doc = yaml.safe_load(_rules_text())
    for group in doc["groups"]:
        for rule in group["rules"]:
            rule.get("annotations", {}).pop("description", None)
            expr = rule["expr"]
            expr = re.sub(rf"\b{start}\b", str(TEST_START), expr)
            expr = re.sub(rf"\b{end}\b", str(TEST_END), expr)
            rule["expr"] = expr
    return yaml.safe_dump(doc, sort_keys=False, allow_unicode=True)


PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def test_promtool_evaluates_the_drill():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip(
            "promtool not on PATH and ENCLII_PROMTOOL not set; the structural "
            "tests above still ran"
        )
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / RULES_KEY).write_text(_rules_for_promtool(), encoding="utf-8")
        name = "pager-path-drill.test.yml"
        (Path(tmp) / name).write_text(_promtool_test(), encoding="utf-8")
        # The prometheus image runs as nobody; a 0700 tempdir is unreadable
        # to it through a bind mount.
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
            proc = subprocess.run(
                cmd,
                shell=shell,
                capture_output=True,
                text=True,
                timeout=PROMTOOL_TIMEOUT_SECONDS,
            )
        except subprocess.TimeoutExpired:
            pytest.skip(f"promtool did not finish within {PROMTOOL_TIMEOUT_SECONDS}s")
    output = (proc.stdout + proc.stderr).strip()
    if proc.returncode != 0 and "FAILED" not in output and "SUCCESS" not in output:
        # The evaluator never ran (no daemon, image pull failure). Not a verdict.
        pytest.skip(f"promtool could not run: {output[-500:]}")
    assert proc.returncode == 0, f"promtool rejected the rule test:\n{output}"
    assert "SUCCESS" in output, output


# ---------------------------------------------------------------------------
# The page budget (infra-stability audit 2026-10-17, pager noise plan step 5)
# ---------------------------------------------------------------------------

BUDGET_KEY = "courier-page-budget-rules.yml"


def _budget_rules() -> dict[str, dict]:
    text = _configmap(PROMETHEUS, "prometheus-rules")["data"][BUDGET_KEY]
    (group,) = [g for g in yaml.safe_load(text)["groups"] if g["name"] == "courier-page-budget"]
    return {r["alert"]: r for r in group["rules"]}


def test_page_budget_is_a_warning_and_counts_only_sent_critical_pages():
    """A page about too many pages must not be one more page."""
    rule = _budget_rules()["CourierPageBudgetExceeded"]
    assert rule["labels"]["severity"] == "warning"
    expr = rule["expr"]
    assert "courier_alarm_pages_total" in expr
    assert 'severity="critical"' in expr and 'outcome="sent"' in expr
    # Resolved notices are not pages.
    assert 'phase=~"opened|reminder|reopened"' in expr
    assert "[24h]" in expr and expr.strip().endswith("> 5")


def test_page_budget_has_an_absence_twin():
    rule = _budget_rules()["CourierPageCounterMissing"]
    assert rule["labels"]["severity"] == "warning"
    assert rule["expr"].strip() == "absent(courier_alarm_pages_total)"


def test_prometheus_may_reach_the_counter_port_in_angelia():
    docs = _docs(MONITORING / "network-policies.yaml")
    (egress,) = [d for d in docs if d.get("kind") == "NetworkPolicy" and d["metadata"]["name"] == "prometheus-egress"]
    rules = [
        r for r in egress["spec"]["egress"]
        if any(
            (t.get("namespaceSelector") or {}).get("matchLabels", {}).get("kubernetes.io/metadata.name") == "angelia"
            for t in r.get("to") or []
        )
    ]
    assert len(rules) == 1
    assert rules[0]["ports"] == [{"protocol": "TCP", "port": 9464}]


BUDGET_TEST = textwrap.dedent(
    """
    rule_files:
      - courier-page-budget-rules.yml
    evaluation_interval: 30s
    tests:
      # 6 pages across two replicas: over budget. Resolved notices and failed
      # sends do not count.
      - interval: 1m
        input_series:
          - series: 'courier_alarm_pages_total{phase="opened",severity="critical",outcome="sent",pod="api-a"}'
            values: '0 1 2 3x60'
          - series: 'courier_alarm_pages_total{phase="reminder",severity="critical",outcome="sent",pod="api-b"}'
            values: '0 0 1 2 3x60'
          - series: 'courier_alarm_pages_total{phase="resolved",severity="critical",outcome="sent",pod="api-a"}'
            values: '0+1x63'
          - series: 'courier_alarm_pages_total{phase="opened",severity="critical",outcome="failed",pod="api-b"}'
            values: '0+1x63'
        alert_rule_test:
          - eval_time: 30m
            alertname: CourierPageBudgetExceeded
            exp_alerts:
              - exp_labels: {severity: warning, service: angelia, category: alerting-last-mile}
                exp_annotations: {summary: "The pager sent 6 critical pages in the last 24h (budget 5)"}
          - eval_time: 30m
            alertname: CourierPageCounterMissing
            exp_alerts: []
      # Exactly 5: within budget.
      - interval: 1m
        input_series:
          - series: 'courier_alarm_pages_total{phase="opened",severity="critical",outcome="sent",pod="api-a"}'
            values: '0 1 2 3 4 5x60'
        alert_rule_test:
          - eval_time: 30m
            alertname: CourierPageBudgetExceeded
            exp_alerts: []
      # No counter at all: the budget is unmeasured, and says so.
      - interval: 1m
        input_series:
          - series: 'up{job="other"}'
            values: '1x90'
        alert_rule_test:
          - eval_time: 70m
            alertname: CourierPageCounterMissing
            exp_alerts:
              - exp_labels: {severity: warning, service: angelia, category: alerting-last-mile}
                exp_annotations: {summary: "Courier's page counter is not being scraped, so the page budget is unmeasured"}
    """
).lstrip()


def test_promtool_evaluates_the_page_budget():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; the structural tests above still ran")
    doc = yaml.safe_load(_configmap(PROMETHEUS, "prometheus-rules")["data"][BUDGET_KEY])
    for group in doc["groups"]:
        for rule in group["rules"]:
            for key in ("description", "runbook_url"):
                rule.get("annotations", {}).pop(key, None)
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / BUDGET_KEY).write_text(yaml.safe_dump(doc, sort_keys=False), encoding="utf-8")
        name = "page-budget.test.yml"
        (Path(tmp) / name).write_text(BUDGET_TEST, encoding="utf-8")
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
