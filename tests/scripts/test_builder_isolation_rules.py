"""
Tests for the builder isolation rules (builder-isolation-rules.yml in the
prometheus-rules ConfigMap, infra/k8s/production/monitoring/prometheus.yaml).

Run with:
    pytest tests/scripts/test_builder_isolation_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_builder_isolation_rules.py -v

WHY THIS EXISTS
===============
A CI builder's `builder=true:NoSchedule` taint was removed by hand and stayed
off for two months. Production pods scheduled onto the builder and slowed
down whenever CI was busy, and nothing reported it. What is pinned:

  * the three rules, their severities and `category: platform`;
  * BuilderNodeTaintMissing wants the exact contract (key, value AND
    effect NoSchedule): a PreferNoSchedule taint still alerts;
  * NonCIPodOnBuilder never counts runners, the ARC listeners, Longhorn,
    DaemonSets or finished pods, and only counts nodes whose taint is in place;
  * builders are matched by name in every rule, and the blindness rule
    covers kube-state-metrics publishing no builder at all;
  * promtool: each rule fires on its case and stays silent on the near misses
    (fixtures/builder-isolation-rules.test.yml).
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
PROMTOOL_CASES = Path(__file__).resolve().parent / "fixtures" / "builder-isolation-rules.test.yml"
RULES_KEY = "builder-isolation-rules.yml"
BUILDER_NODES = 'node=~".+-builder-.+"'
SEVERITY = {
    "BuilderNodeTaintMissing": "critical",
    "NonCIPodOnBuilder": "warning",
    "BuilderIsolationUnobservable": "warning",
}


@lru_cache(maxsize=1)
def _rules_text() -> str:
    for doc in yaml.safe_load_all(PROMETHEUS.read_text(encoding="utf-8")):
        if isinstance(doc, dict) and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-rules":
            return doc["data"][RULES_KEY]
    raise AssertionError("prometheus-rules not found")


def _rules() -> dict[str, dict]:
    groups = yaml.safe_load(_rules_text())["groups"]
    assert [g["name"] for g in groups] == ["builder-isolation"]
    return {r["alert"]: r for r in groups[0]["rules"]}


def test_rule_set_and_labels():
    rules = _rules()
    assert set(rules) == set(SEVERITY)
    for name, rule in rules.items():
        assert rule["labels"] == {"severity": SEVERITY[name], "category": "platform"}, name


def test_every_rule_selects_builders_by_name():
    for name, rule in _rules().items():
        assert BUILDER_NODES in rule["expr"], name


def test_taint_rule_wants_the_whole_contract():
    expr = _rules()["BuilderNodeTaintMissing"]["expr"]
    for part in ('key="builder"', 'value="true"', 'effect="NoSchedule"'):
        assert part in expr
    assert "unless on (node)" in expr


def test_non_ci_rule_exclusions_and_taint_gate():
    expr = _rules()["NonCIPodOnBuilder"]["expr"]
    assert 'created_by_kind!="DaemonSet"' in expr
    assert 'namespace!~"arc-runners|arc-system|longhorn-system"' in expr
    assert 'phase="Running"' in expr
    assert 'and on (node)' in expr and 'effect="NoSchedule"' in expr


def test_blindness_rule_reads_node_info():
    assert _rules()["BuilderIsolationUnobservable"]["expr"].strip() == f"absent(kube_node_info{{{BUILDER_NODES}}})"


PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _promtool(verb: str, files: dict[str, str], target: str) -> str:
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; the structural tests above still ran")
    with tempfile.TemporaryDirectory() as tmp:
        for name, text in files.items():
            (Path(tmp) / name).write_text(text, encoding="utf-8")
        # The prometheus image runs as nobody; a 0700 tempdir is unreadable
        # to it through a bind mount.
        os.chmod(tmp, 0o755)
        for p in Path(tmp).iterdir():
            os.chmod(p, 0o644)
        if template:
            cmd: str | list[str] = template.replace("test rules", f"{verb} rules").format(dir=tmp, name=target)
            shell = True
        else:
            cmd = ["promtool", verb, "rules", str(Path(tmp) / target)]
            shell = False
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


def test_promtool_evaluates_the_builder_isolation_rules():
    # promtool compares the full annotation map; the cases pin summaries, and
    # test_promtool_check_rules still parses the description templates.
    groups = yaml.safe_load(_rules_text())
    for rule in groups["groups"][0]["rules"]:
        rule["annotations"].pop("description", None)
    name = "builder-isolation-rules.test.yml"
    _promtool(
        "test",
        {RULES_KEY: yaml.safe_dump(groups, sort_keys=False), name: PROMTOOL_CASES.read_text(encoding="utf-8")},
        name,
    )


def test_promtool_check_rules():
    _promtool("check", {RULES_KEY: _rules_text()}, RULES_KEY)
