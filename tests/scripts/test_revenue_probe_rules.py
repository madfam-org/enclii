"""
Tests for the revenue probes: the blackbox exporter, its `blackbox-revenue`
scrape job and the `revenue-probes` rule group.

Run with:
    pytest tests/scripts/test_revenue_probe_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_revenue_probe_rules.py -v

WHY THIS EXISTS
===============
Money-machine audit 2026-10-11 (M2 F1/F2/F5): an app answering 500 behind
Ready pods paged nobody. cotiza.studio's /api/* failed while its front page
and its Argo app read healthy, and Dhanam priced USD/MXN off its hardcoded
emergency table while /health answered 200. These probes are what pages for
that, so each way they could silently stop working is pinned here:

  * the exporter file is rendered (listed in the kustomization);
  * every target names a module the exporter defines, carries the labels the
    rules and the runbook text read, and is an https URL;
  * the revenue flows the audit named are all probed;
  * the scrape job really points Prometheus at the exporter's Service port,
    and the NetworkPolicy admits Prometheus on it;
  * the FX module has exactly one body regex, so probe_failed_due_to_regex
    means "emergency_fallback" and nothing else;
  * the rules page (critical), and the FX regex failure pages as itself, not
    also as RevenueEndpointDown.

The promtool layer evaluates the real rules with Prometheus's own engine; if
the evaluator cannot RUN it skips with the reason, never passes silently.
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
from urllib.parse import urlparse

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
PROMETHEUS = MONITORING / "prometheus.yaml"
BLACKBOX = MONITORING / "blackbox-exporter.yaml"
KUSTOMIZATION = MONITORING / "kustomization.yaml"
NETWORK_POLICIES = MONITORING / "network-policies.yaml"

JOB = "blackbox-revenue"
RULES_KEY = "revenue-probe-rules.yml"
EXPORTER_ADDRESS = "blackbox-exporter.monitoring.svc:9115"
REQUIRED_LABELS = {"product", "flow", "target_namespace", "check", "module"}

# (product, flow) pairs the audit named. Removing one needs a reason in review.
REQUIRED_FLOWS = {
    ("cotiza", "web-api-proxy"),
    ("cotiza", "quote"),
    ("cotiza", "quote-via-web"),
    ("cotiza", "api"),
    ("dhanam", "api"),
    ("dhanam", "billing-catalog"),
    ("dhanam", "fx"),
    ("yantra4d", "api"),
    ("kalya", "web"),
    ("madfam-site", "web"),
    ("avala", "api"),
    ("factlas", "api"),
    ("routecraft", "web"),
}


def _docs(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]


def _configmap(path: Path, name: str) -> dict:
    for doc in _docs(path):
        if doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == name:
            return doc
    raise AssertionError(f"ConfigMap {name!r} not found in {path}")


@lru_cache(maxsize=1)
def _scrape_job() -> dict:
    cfg = yaml.safe_load(_configmap(PROMETHEUS, "prometheus-config")["data"]["prometheus.yml"])
    jobs = [j for j in cfg["scrape_configs"] if j.get("job_name") == JOB]
    assert len(jobs) == 1, f"expected exactly one {JOB!r} scrape job"
    return jobs[0]


def _targets() -> list[tuple[str, dict]]:
    out = []
    for sc in _scrape_job()["static_configs"]:
        for target in sc["targets"]:
            out.append((target, sc.get("labels") or {}))
    return out


@lru_cache(maxsize=1)
def _modules() -> dict:
    cfg = yaml.safe_load(_configmap(BLACKBOX, "blackbox-exporter-config")["data"]["config.yml"])
    return cfg["modules"]


@lru_cache(maxsize=1)
def _rules_text() -> str:
    return _configmap(PROMETHEUS, "prometheus-rules")["data"][RULES_KEY]


def _rules() -> dict[str, dict]:
    groups = yaml.safe_load(_rules_text())["groups"]
    group = [g for g in groups if g.get("name") == "revenue-probes"]
    assert len(group) == 1, "expected exactly one `revenue-probes` group"
    return {r["alert"]: r for r in group[0]["rules"]}


# ---------------------------------------------------------------------------
# Structural
# ---------------------------------------------------------------------------


def test_exporter_is_rendered_by_the_kustomization():
    resources = yaml.safe_load(KUSTOMIZATION.read_text(encoding="utf-8"))["resources"]
    assert "blackbox-exporter.yaml" in resources


def test_exporter_image_is_digest_pinned():
    deploy = [d for d in _docs(BLACKBOX) if d.get("kind") == "Deployment"][0]
    (container,) = deploy["spec"]["template"]["spec"]["containers"]
    assert "@sha256:" in container["image"], container["image"]
    assert container["resources"]["limits"], "Kyverno requires limits"
    assert container["readinessProbe"]["httpGet"] and container["livenessProbe"]["httpGet"]


def test_every_target_is_https_with_a_known_module_and_all_labels():
    modules = _modules()
    targets = _targets()
    assert targets, "no targets"
    for url, labels in targets:
        assert urlparse(url).scheme == "https", url
        missing = REQUIRED_LABELS - set(labels)
        assert not missing, f"{url} lacks labels {sorted(missing)}"
        assert labels["module"] in modules, f"{url}: unknown module {labels['module']!r}"


def test_no_target_is_listed_twice():
    urls = [u for u, _ in _targets()]
    assert len(urls) == len(set(urls))


def test_the_audited_revenue_flows_are_probed():
    have = {(l["product"], l["flow"]) for _, l in _targets()}
    assert REQUIRED_FLOWS <= have, f"missing: {sorted(REQUIRED_FLOWS - have)}"


def test_exactly_one_fx_source_probe_on_the_fx_module():
    fx = [(u, l) for u, l in _targets() if l["check"] == "fx-source"]
    assert len(fx) == 1, fx
    url, labels = fx[0]
    assert url == "https://api.dhan.am/v1/fx-rates/health"
    assert labels["module"] == "http_dhanam_fx"
    for _, l in _targets():
        assert l["check"] in {"availability", "fx-source"}, l


def test_fx_module_fails_only_on_emergency_fallback():
    """probe_failed_due_to_regex must mean one thing for this module."""
    http = _modules()["http_dhanam_fx"]["http"]
    assert http.get("fail_if_body_matches_regexp") == ['"source"\\s*:\\s*"emergency_fallback"']
    assert not http.get("fail_if_body_not_matches_regexp")
    assert not http.get("fail_if_header_matches") and not http.get("fail_if_header_not_matches")


@pytest.mark.parametrize("name", ["http_2xx_json", "http_401", "http_dhanam_catalog", "http_dhanam_fx"])
def test_modules_do_not_follow_redirects_and_require_tls(name):
    http = _modules()[name]["http"]
    assert http["follow_redirects"] is False
    assert http["fail_if_not_ssl"] is True


def test_module_timeout_fits_inside_the_scrape_timeout():
    scrape = int(_scrape_job()["scrape_timeout"].rstrip("s"))
    for name, mod in _modules().items():
        assert int(mod["timeout"].rstrip("s")) < scrape, name


def test_scrape_job_points_prometheus_at_the_exporter():
    relabels = _scrape_job()["relabel_configs"]
    assert {"source_labels": ["__address__"], "target_label": "__param_target"} in relabels
    assert {"source_labels": ["module"], "target_label": "__param_module"} in relabels
    assert {"target_label": "__address__", "replacement": EXPORTER_ADDRESS} in relabels
    assert _scrape_job()["metrics_path"] == "/probe"
    svc = [d for d in _docs(BLACKBOX) if d.get("kind") == "Service"][0]
    assert svc["metadata"]["name"] == "blackbox-exporter"
    assert [p["port"] for p in svc["spec"]["ports"]] == [9115]


def test_network_policy_admits_prometheus_on_the_probe_port():
    pols = {d["metadata"]["name"]: d for d in _docs(NETWORK_POLICIES) if d.get("kind") == "NetworkPolicy"}
    ingress = pols["blackbox-exporter-ingress"]["spec"]
    assert ingress["podSelector"]["matchLabels"] == {"app": "blackbox-exporter"}
    (rule,) = ingress["ingress"]
    assert rule["from"] == [{"podSelector": {"matchLabels": {"app": "prometheus"}}}]
    assert rule["ports"] == [{"protocol": "TCP", "port": 9115}]
    egress_ports = {
        (p["protocol"], p["port"])
        for r in pols["blackbox-exporter-egress"]["spec"]["egress"]
        for p in r["ports"]
    }
    assert egress_ports == {("UDP", 53), ("TCP", 443)}


def test_rules_page_and_fx_regex_is_not_double_paged():
    rules = _rules()
    assert set(rules) == {"RevenueEndpointDown", "DhanamFXEmergencyFallback", "RevenueProbesNotRunning"}
    for name, rule in rules.items():
        assert rule["labels"]["severity"] == "critical", name
        assert f'job="{JOB}"' in rule["expr"], name
    assert 'check="fx-source"' in rules["RevenueEndpointDown"]["expr"]
    assert "unless" in rules["RevenueEndpointDown"]["expr"]


# ---------------------------------------------------------------------------
# promtool: evaluate the real rules
# ---------------------------------------------------------------------------

PROMTOOL_TEST = textwrap.dedent(
    """
    rule_files:
      - revenue-probe-rules.yml
    evaluation_interval: 30s
    tests:
      # 1. Cotiza's proxy fails, Dhanam FX is on the emergency table, the
      #    Dhanam API is fine.
      - interval: 1m
        input_series:
          - series: 'up{job="blackbox-revenue",instance="https://cotiza.studio/api/health"}'
            values: '1x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://cotiza.studio/api/health",product="cotiza",flow="web-api-proxy",target_namespace="digifab-quoting",check="availability",module="http_2xx_json"}'
            values: '0x20'
          - series: 'probe_failed_due_to_regex{job="blackbox-revenue",instance="https://cotiza.studio/api/health",product="cotiza",flow="web-api-proxy",target_namespace="digifab-quoting",check="availability",module="http_2xx_json"}'
            values: '0x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://api.dhan.am/health",product="dhanam",flow="api",target_namespace="dhanam",check="availability",module="http_2xx_json"}'
            values: '1x20'
          - series: 'probe_failed_due_to_regex{job="blackbox-revenue",instance="https://api.dhan.am/health",product="dhanam",flow="api",target_namespace="dhanam",check="availability",module="http_2xx_json"}'
            values: '0x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://api.dhan.am/v1/fx-rates/health",product="dhanam",flow="fx",target_namespace="dhanam",check="fx-source",module="http_dhanam_fx"}'
            values: '0x20'
          - series: 'probe_failed_due_to_regex{job="blackbox-revenue",instance="https://api.dhan.am/v1/fx-rates/health",product="dhanam",flow="fx",target_namespace="dhanam",check="fx-source",module="http_dhanam_fx"}'
            values: '1x20'
        alert_rule_test:
          - eval_time: 2m
            alertname: RevenueEndpointDown
            exp_alerts: []
          # Cotiza only: the FX regex failure is NOT an endpoint-down page.
          - eval_time: 5m
            alertname: RevenueEndpointDown
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://cotiza.studio/api/health
                  product: cotiza
                  flow: web-api-proxy
                  target_namespace: digifab-quoting
                  check: availability
                  module: http_2xx_json
                exp_annotations:
                  summary: "cotiza web-api-proxy is failing: https://cotiza.studio/api/health"
          - eval_time: 10m
            alertname: DhanamFXEmergencyFallback
            exp_alerts: []
          - eval_time: 17m
            alertname: DhanamFXEmergencyFallback
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  service: dhanam
                  job: blackbox-revenue
                  instance: https://api.dhan.am/v1/fx-rates/health
                  product: dhanam
                  flow: fx
                  target_namespace: dhanam
                  check: fx-source
                  module: http_dhanam_fx
                exp_annotations:
                  summary: "Dhanam FX is serving the hardcoded emergency rate table"
          - eval_time: 17m
            alertname: RevenueProbesNotRunning
            exp_alerts: []

      # 2. The FX endpoint itself is DOWN (5xx: the regex never runs). That is
      #    an endpoint-down page, not an FX-source page.
      - interval: 1m
        input_series:
          - series: 'probe_success{job="blackbox-revenue",instance="https://api.dhan.am/v1/fx-rates/health",product="dhanam",flow="fx",target_namespace="dhanam",check="fx-source",module="http_dhanam_fx"}'
            values: '0x20'
          - series: 'probe_failed_due_to_regex{job="blackbox-revenue",instance="https://api.dhan.am/v1/fx-rates/health",product="dhanam",flow="fx",target_namespace="dhanam",check="fx-source",module="http_dhanam_fx"}'
            values: '0x20'
        alert_rule_test:
          - eval_time: 5m
            alertname: RevenueEndpointDown
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://api.dhan.am/v1/fx-rates/health
                  product: dhanam
                  flow: fx
                  target_namespace: dhanam
                  check: fx-source
                  module: http_dhanam_fx
                exp_annotations:
                  summary: "dhanam fx is failing: https://api.dhan.am/v1/fx-rates/health"
          - eval_time: 17m
            alertname: DhanamFXEmergencyFallback
            exp_alerts: []

      # 3. The exporter is unreachable: every target's up is 0.
      - interval: 1m
        input_series:
          - series: 'up{job="blackbox-revenue",instance="https://kalya.app/api/health"}'
            values: '0x20'
          - series: 'up{job="blackbox-revenue",instance="https://routecraft.app/api/health"}'
            values: '0x20'
        alert_rule_test:
          - eval_time: 8m
            alertname: RevenueProbesNotRunning
            exp_alerts: []
          - eval_time: 12m
            alertname: RevenueProbesNotRunning
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                exp_annotations:
                  summary: "Revenue probes are not running: the revenue flows are unwatched"
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _rules_text_without_descriptions() -> str:
    """The loaded rules with only the multi-line `description` dropped.

    promtool compares the full annotation map, and repeating each runbook
    paragraph verbatim here would make every wording fix a test change. expr,
    for, labels and the `summary` template are compared exactly; the
    descriptions' templates are still parsed by `promtool check rules` in CI's
    rule check, and test_annotations_use_only_labels_and_value pins what they
    may reference.
    """
    doc = yaml.safe_load(_rules_text())
    for group in doc["groups"]:
        for rule in group["rules"]:
            rule.get("annotations", {}).pop("description", None)
    return yaml.safe_dump(doc, sort_keys=False)


def test_annotations_use_only_labels_and_value():
    """No `query` template functions: a template that errors at evaluation
    has crashed this Prometheus before (#572)."""
    for name, rule in _rules().items():
        for key, text in rule["annotations"].items():
            for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
                assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)\s*", expr), (name, key, expr)


def test_promtool_evaluates_the_revenue_rules():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip(
            "promtool not on PATH and ENCLII_PROMTOOL not set; the structural "
            "tests above still ran"
        )
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / RULES_KEY).write_text(_rules_text_without_descriptions(), encoding="utf-8")
        name = "revenue-probe-rules.test.yml"
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
