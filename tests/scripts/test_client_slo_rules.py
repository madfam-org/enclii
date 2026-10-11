"""
Tests for the `client-slo` rule group in the `prometheus-rules` ConfigMap
(infra/k8s/production/monitoring/prometheus.yaml, key client-slo-rules.yml).

Run with:
    pytest tests/scripts/test_client_slo_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_client_slo_rules.py -v

WHY THIS EXISTS
===============
The group's five rules share one enumerated client-namespace selector. A
namespace missing from it is silently uncovered: on 2026-10-05 the production
`voxa` namespace had kube-state-metrics series but was absent from
`ClientDeploymentUnavailable` (the group's only critical rule), so a voxa
Deployment below its desired replicas paged nobody.

Two layers, the same split as tests/scripts/test_check_alertmanager_config.py:

  1. STRUCTURAL (always runs). Reads the rules straight out of the ConfigMap,
     so what is tested is byte-for-byte what Prometheus loads. Pins that the
     five selectors are identical, that `voxa` and the seven revenue
     namespaces added 2026-10-11 are covered, and that `voxa-staging` and the
     `enclii-*-staging` namespaces are not.

  2. promtool (runs when `promtool` is on PATH or ENCLII_PROMTOOL is set).
     Evaluates the real rule with Prometheus's own engine: a voxa Deployment
     with available < desired for 5m fires, the same condition in
     voxa-staging does not. If the evaluator cannot RUN (no docker, registry
     unreachable) the test is skipped with the reason, never passed silently.
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
MANIFEST = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring" / "prometheus.yaml"
RULES_CONFIGMAP = "prometheus-rules"
RULES_KEY = "client-slo-rules.yml"

EXPECTED_RULES = {
    "ClientServiceErrorRate",
    "ClientServiceLatencyP95",
    "ClientPodRestartRate",
    "ClientDeploymentUnavailable",
    "TenantResourceQuotaNearLimit",
}

NAMESPACE_SELECTOR = re.compile(r'namespace=~"([^"]+)"')
# The staging exclusion every selector carries next to the inclusion. PromQL
# accepts two matchers on one label and a series must satisfy both.
NAMESPACE_EXCLUSION = re.compile(r'namespace!~"([^"]+)"')
STAGING_EXCLUSION = ".+-staging"

# The seven revenue namespaces that were uncovered until 2026-10-11
# (money-machine audit M2 F5). Names are the Argo applications' resource
# namespaces, read from `enclii ops apps status --json`.
REVENUE_NAMESPACES_ADDED_2026_10_11 = [
    "digifab-quoting",
    "yantra4d",
    "kalya",
    "madfam-site",
    "avala",
    "factlas",
    "routecraft",
]


@lru_cache(maxsize=1)
def _rules_file_text() -> str:
    with MANIFEST.open("r", encoding="utf-8") as fh:
        for doc in yaml.safe_load_all(fh):
            if (
                isinstance(doc, dict)
                and doc.get("kind") == "ConfigMap"
                and (doc.get("metadata") or {}).get("name") == RULES_CONFIGMAP
            ):
                return doc["data"][RULES_KEY]
    raise AssertionError(f"ConfigMap {RULES_CONFIGMAP!r} not found in {MANIFEST}")


def _client_slo_rules() -> dict[str, dict]:
    groups = yaml.safe_load(_rules_file_text())["groups"]
    group = [g for g in groups if g.get("name") == "client-slo"]
    assert len(group) == 1, "expected exactly one `client-slo` group"
    return {r["alert"]: r for r in group[0]["rules"] if "alert" in r}


def _selectors(expr: str) -> list[str]:
    return NAMESPACE_SELECTOR.findall(expr)


def _exclusions(expr: str) -> list[str]:
    return NAMESPACE_EXCLUSION.findall(expr)


def _covered(selector: str, namespace: str, exclusion: str | None = None) -> bool:
    # PromQL `=~` and `!~` are fully anchored (RE2, implicit ^...$), which
    # fullmatch reproduces for these plain alternations.
    if re.fullmatch(selector, namespace) is None:
        return False
    return exclusion is None or re.fullmatch(exclusion, namespace) is None


def _deployment_unavailable_matchers() -> tuple[str, str]:
    expr = _client_slo_rules()["ClientDeploymentUnavailable"]["expr"]
    (selector,) = set(_selectors(expr))
    (exclusion,) = set(_exclusions(expr))
    return selector, exclusion


# ---------------------------------------------------------------------------
# Structural
# ---------------------------------------------------------------------------


def test_group_has_the_expected_rules():
    assert set(_client_slo_rules()) == EXPECTED_RULES


def test_every_rule_uses_one_identical_namespace_selector():
    """A namespace is either fully covered by the group or not at all."""
    seen: set[str] = set()
    for name, rule in _client_slo_rules().items():
        sels = _selectors(rule["expr"])
        assert sels, f"{name} has no namespace=~ selector"
        seen.update(sels)
    assert len(seen) == 1, (
        "client-slo rules disagree on the client-namespace selector; add a new "
        f"namespace to every rule in the group. Found: {sorted(seen)}"
    )


def test_every_selector_carries_the_staging_exclusion():
    """`enclii-.*` matches `enclii-<app>-staging`; each inclusion needs its pair."""
    for name, rule in _client_slo_rules().items():
        sels, excls = _selectors(rule["expr"]), _exclusions(rule["expr"])
        assert len(excls) == len(sels), (
            f"{name}: {len(sels)} namespace=~ selectors but {len(excls)} "
            f'namespace!~"{STAGING_EXCLUSION}" exclusions'
        )
        assert set(excls) == {STAGING_EXCLUSION}, f"{name}: {excls}"


def test_client_deployment_unavailable_pages():
    rule = _client_slo_rules()["ClientDeploymentUnavailable"]
    assert rule["labels"]["severity"] == "critical"
    assert rule["for"] == "5m"
    assert len(_selectors(rule["expr"])) == 2, (
        "both sides of the comparison must carry the selector"
    )


@pytest.mark.parametrize(
    "namespace",
    ["voxa", "nauta", "janua", "selva", "pravara-mes", "enclii-x", "project-x"]
    + REVENUE_NAMESPACES_ADDED_2026_10_11,
)
def test_production_client_namespaces_are_covered(namespace):
    selector, exclusion = _deployment_unavailable_matchers()
    assert _covered(selector, namespace, exclusion)


@pytest.mark.parametrize(
    "namespace",
    [
        "voxa-staging",
        "voxa-preview",
        "xvoxa",
        # `enclii-.*` matched these two until the exclusion was added.
        "enclii-dhanam-staging",
        "enclii-karafiel-staging",
        "project-x-staging",
        "kalya-staging",
        "digifab-quoting-staging",
    ],
)
def test_staging_and_lookalikes_are_not_covered(namespace):
    """Staging can serve an old build for weeks; it must not page."""
    selector, exclusion = _deployment_unavailable_matchers()
    assert not _covered(selector, namespace, exclusion)


# ---------------------------------------------------------------------------
# promtool: evaluate the real rule
# ---------------------------------------------------------------------------

PROMTOOL_TEST = textwrap.dedent(
    """
    rule_files:
      - client-slo-rules.yml
    evaluation_interval: 30s
    tests:
      - interval: 1m
        input_series:
          # voxa production: web has 1 of 2 replicas available for 10m.
          - series: 'kube_deployment_spec_replicas{namespace="voxa",deployment="voxa-web"}'
            values: '2x10'
          - series: 'kube_deployment_status_replicas_available{namespace="voxa",deployment="voxa-web"}'
            values: '1x10'
          # voxa production: api is healthy.
          - series: 'kube_deployment_spec_replicas{namespace="voxa",deployment="voxa-api"}'
            values: '2x10'
          - series: 'kube_deployment_status_replicas_available{namespace="voxa",deployment="voxa-api"}'
            values: '2x10'
          # voxa-staging: the same shortfall, which must NOT fire.
          - series: 'kube_deployment_spec_replicas{namespace="voxa-staging",deployment="voxa-web"}'
            values: '2x10'
          - series: 'kube_deployment_status_replicas_available{namespace="voxa-staging",deployment="voxa-web"}'
            values: '1x10'
          # Cotiza production (added 2026-10-11): web has 0 of 1 available.
          - series: 'kube_deployment_spec_replicas{namespace="digifab-quoting",deployment="digifab-quoting-web"}'
            values: '1x10'
          - series: 'kube_deployment_status_replicas_available{namespace="digifab-quoting",deployment="digifab-quoting-web"}'
            values: '0x10'
          # A staging namespace that `enclii-.*` matches: must NOT fire.
          - series: 'kube_deployment_spec_replicas{namespace="enclii-karafiel-staging",deployment="karafiel-admin"}'
            values: '1x10'
          - series: 'kube_deployment_status_replicas_available{namespace="enclii-karafiel-staging",deployment="karafiel-admin"}'
            values: '0x10'
        alert_rule_test:
          # Pending, not firing, before `for: 5m` elapses.
          - eval_time: 4m
            alertname: ClientDeploymentUnavailable
            exp_alerts: []
          # Exactly two alerts: digifab-quoting and voxa/voxa-web. Neither
          # voxa-staging nor enclii-karafiel-staging fires.
          - eval_time: 6m
            alertname: ClientDeploymentUnavailable
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: client-slo
                  namespace: digifab-quoting
                  deployment: digifab-quoting-web
                exp_annotations:
                  summary: "Deployment digifab-quoting-web unavailable in digifab-quoting"
                  description: "Available replicas (0) < desired for 5m."
              - exp_labels:
                  severity: critical
                  category: client-slo
                  namespace: voxa
                  deployment: voxa-web
                exp_annotations:
                  summary: "Deployment voxa-web unavailable in voxa"
                  description: "Available replicas (1) < desired for 5m."
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def test_promtool_production_fires_and_staging_does_not():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip(
            "promtool not on PATH and ENCLII_PROMTOOL not set; the structural "
            "tests above still ran"
        )
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / RULES_KEY).write_text(_rules_file_text(), encoding="utf-8")
        name = "client-slo-rules.test.yml"
        (Path(tmp) / name).write_text(PROMTOOL_TEST, encoding="utf-8")
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
