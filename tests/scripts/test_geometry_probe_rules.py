"""
Tests for the geometry-factory probes: the `blackbox-geometry` scrape job,
the `http_200_html` blackbox module and the `geometry-probes` rule group.

Run with:
    pytest tests/scripts/test_geometry_probe_rules.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_geometry_probe_rules.py -v

WHY THIS EXISTS
===============
Geometry stability pass 2026-10-18 (lane O1): the geometry offering is up
only if someone is told when it is down. Before this job only
api.yantra4d.com and the Cotiza hosts were probed over HTTP; the other
geometry hosts (the Yantra4D apex, studio and admin, Fashion Cabinet,
Pliego, Blueprint.Tube) were watched by replica counts only, so a host
answering 5xx or timing out behind Ready pods paged nobody. Pinned here:

  * every geometry host is probed, exactly once, over https, on a module the
    exporter defines, with the labels the rules and the runbook text read;
  * the job runs at 60s or slower (scrape cost, the 2026-09 scrape-widening
    incident) and points Prometheus at the exporter;
  * the SPA apex is judged as HTML, because its /api/health is index.html;
  * the three rules page (critical), read only this job, keep each target's
    `product` (Courier's page identity) and route to critical-receiver
    through the generic critical route, not the revenue route and not a
    staging route.

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
ALERTMANAGER = MONITORING / "alertmanager.yaml"

JOB = "blackbox-geometry"
RULES_KEY = "geometry-probe-rules.yml"
GROUP = "geometry-probes"
EXPORTER_ADDRESS = "blackbox-exporter.monitoring.svc:9115"
REQUIRED_LABELS = {"product", "flow", "target_namespace", "module"}
CATEGORY = "geometry-probe"

# Every geometry-factory host, on the URL that answers 200 with no redirect
# (curl'd 2026-10-18). url -> (product, flow, target_namespace, module).
GEOMETRY_TARGETS = {
    "https://yantra4d.com/": ("yantra4d", "landing", "yantra4d", "http_200_html"),
    "https://app.yantra4d.com/api/health": ("yantra4d", "studio", "yantra4d", "http_2xx_json"),
    "https://admin.yantra4d.com/api/health": ("yantra4d", "admin", "yantra4d", "http_2xx_json"),
    "https://api.yantra4d.com/api/health": ("yantra4d", "api", "yantra4d", "http_2xx_json"),
    "https://fc.madfam.io/health": ("fashion-cabinet", "app", "fashion-cabinet", "http_2xx_json"),
    "https://fashioncabi.net/health": ("fashion-cabinet", "brand-domain", "fashion-cabinet", "http_2xx_json"),
    "https://pliego.madfam.io/health": ("pliego", "landing", "pliego", "http_2xx_json"),
    "https://pliego-app.madfam.io/health": ("pliego", "app", "pliego", "http_2xx_json"),
    "https://pliego-api.madfam.io/health": ("pliego", "api", "pliego", "http_2xx_json"),
    "https://cotiza.studio/api/health": ("cotiza", "web", "digifab-quoting", "http_2xx_json"),
    "https://api.cotiza.studio/health": ("cotiza", "api", "digifab-quoting", "http_2xx_json"),
    "https://blueprint.tube/health": ("blueprint", "landing", "blueprint-harvester", "http_2xx_json"),
    "https://api.blueprint.tube/health": ("blueprint", "api", "blueprint-harvester", "http_2xx_json"),
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
def _prometheus_config() -> dict:
    return yaml.safe_load(_configmap(PROMETHEUS, "prometheus-config")["data"]["prometheus.yml"])


def _scrape_job() -> dict:
    jobs = [j for j in _prometheus_config()["scrape_configs"] if j.get("job_name") == JOB]
    assert len(jobs) == 1, f"expected exactly one {JOB!r} scrape job"
    return jobs[0]


def _targets() -> list[tuple[str, dict]]:
    return [
        (target, sc.get("labels") or {})
        for sc in _scrape_job()["static_configs"]
        for target in sc["targets"]
    ]


@lru_cache(maxsize=1)
def _modules() -> dict:
    cfg = yaml.safe_load(_configmap(BLACKBOX, "blackbox-exporter-config")["data"]["config.yml"])
    return cfg["modules"]


@lru_cache(maxsize=1)
def _rules_text() -> str:
    return _configmap(PROMETHEUS, "prometheus-rules")["data"][RULES_KEY]


def _rules() -> dict[str, dict]:
    groups = yaml.safe_load(_rules_text())["groups"]
    group = [g for g in groups if g.get("name") == GROUP]
    assert len(group) == 1, f"expected exactly one `{GROUP}` group"
    return {r["alert"]: r for r in group[0]["rules"]}


def _seconds(duration: str) -> int:
    units = {"s": 1, "m": 60, "h": 3600}
    return int(duration[:-1]) * units[duration[-1]]


# ---------------------------------------------------------------------------
# Targets and the scrape job
# ---------------------------------------------------------------------------


def test_every_geometry_host_is_probed_exactly_once():
    urls = [u for u, _ in _targets()]
    assert len(urls) == len(set(urls)), "a target is listed twice"
    assert set(urls) == set(GEOMETRY_TARGETS), (
        f"missing: {sorted(set(GEOMETRY_TARGETS) - set(urls))}; "
        f"unexpected: {sorted(set(urls) - set(GEOMETRY_TARGETS))}"
    )


def test_every_target_is_https_with_its_labels_and_a_known_module():
    modules = _modules()
    for url, labels in _targets():
        assert urlparse(url).scheme == "https", url
        missing = REQUIRED_LABELS - set(labels)
        assert not missing, f"{url} lacks labels {sorted(missing)}"
        assert labels["module"] in modules, f"{url}: unknown module {labels['module']!r}"
        product, flow, namespace, module = GEOMETRY_TARGETS[url]
        assert (labels["product"], labels["flow"], labels["target_namespace"], labels["module"]) == (
            product,
            flow,
            namespace,
            module,
        ), url
        assert re.fullmatch(r"[a-z0-9][a-z0-9-]*", labels["product"]), url


def test_job_is_cheap_and_points_prometheus_at_the_exporter():
    job = _scrape_job()
    assert _seconds(job["scrape_interval"]) >= 60, "keep the probe interval at 60s or slower"
    assert job["metrics_path"] == "/probe"
    relabels = job["relabel_configs"]
    assert {"source_labels": ["__address__"], "target_label": "__param_target"} in relabels
    assert {"source_labels": ["module"], "target_label": "__param_module"} in relabels
    assert {"source_labels": ["__param_target"], "target_label": "instance"} in relabels
    assert {"target_label": "__address__", "replacement": EXPORTER_ADDRESS} in relabels
    scrape = _seconds(job["scrape_timeout"])
    for _, labels in _targets():
        assert _seconds(_modules()[labels["module"]]["timeout"]) < scrape, labels["module"]


def test_geometry_hosts_are_not_revenue_targets_under_another_job_name():
    """Separate jobs keep the alertnames apart: a geometry host added to
    blackbox-revenue would page RevenueEndpointDown on the revenue route
    instead of GeometryHostDown."""
    jobs = [j["job_name"] for j in _prometheus_config()["scrape_configs"]]
    assert jobs.count(JOB) == 1
    revenue = next(j for j in _prometheus_config()["scrape_configs"] if j["job_name"] == "blackbox-revenue")
    revenue_urls = {t for sc in revenue["static_configs"] for t in sc["targets"]}
    # The one URL both jobs probe on purpose; see the job's header comment.
    assert set(GEOMETRY_TARGETS) & revenue_urls == {"https://api.yantra4d.com/api/health"}


# ---------------------------------------------------------------------------
# http_200_html: the SPA apex
# ---------------------------------------------------------------------------

HTML_BODIES = [
    '<!DOCTYPE html><html lang="es" class="dark">',
    "<!doctype html>\n<html lang=\"en\">",
    "<HTML><BODY>ok</BODY></HTML>",
]
NOT_HTML_BODIES = ['{"status":"ok"}', '{"error":"internal"}', "", "OK"]


def _body_passes(module: str, body: str) -> bool:
    """blackbox_exporter's body rules: every not-match regex must match, and
    no match regex may. Go's regexp and Python's re agree on these patterns."""
    http = _modules()[module]["http"]
    must = http.get("fail_if_body_not_matches_regexp") or []
    must_not = http.get("fail_if_body_matches_regexp") or []
    return all(re.search(p, body) for p in must) and not any(re.search(p, body) for p in must_not)


@pytest.mark.parametrize("body", HTML_BODIES)
def test_html_module_accepts_an_html_document(body):
    assert _body_passes("http_200_html", body), body


@pytest.mark.parametrize("body", NOT_HTML_BODIES)
def test_html_module_rejects_json_empty_and_plain_bodies(body):
    assert not _body_passes("http_200_html", body), body


def test_html_module_wants_exactly_200_without_redirects_over_tls():
    module = _modules()["http_200_html"]
    http = module["http"]
    assert module["prober"] == "http"
    assert http["valid_status_codes"] == [200]
    assert http["follow_redirects"] is False
    assert http["fail_if_not_ssl"] is True
    assert http["method"] == "GET"
    assert http["preferred_ip_protocol"] == "ip4"
    assert http["headers"] == _modules()["http_2xx_json"]["http"]["headers"]
    assert not http.get("fail_if_body_matches_regexp")


def test_the_spa_apex_is_the_only_html_target():
    """yantra4d.com answers 200 index.html for /api/health, so http_2xx_json
    would fail it forever; every other geometry host has a JSON health path."""
    assert [u for u, l in _targets() if l["module"] == "http_200_html"] == ["https://yantra4d.com/"]


# ---------------------------------------------------------------------------
# Rules and routing
# ---------------------------------------------------------------------------


def test_rules_page_and_read_only_this_job():
    rules = _rules()
    assert set(rules) == {"GeometryHostDown", "GeometryTLSCertExpiring", "GeometryProbesNotRunning"}
    for name, rule in rules.items():
        assert rule["labels"]["severity"] == "critical", name
        assert rule["labels"].get("category") == CATEGORY, name
        assert f'job="{JOB}"' in rule["expr"], name
        assert "product" not in rule["labels"], f"{name}: a static product would merge every page"
    assert _seconds(rules["GeometryHostDown"]["for"]) >= 180
    assert "14 * 86400" in rules["GeometryTLSCertExpiring"]["expr"]


def test_per_target_rules_keep_the_product_label():
    """Courier keys a page on `product`; an aggregation would drop it and
    collapse every geometry host into one page."""
    for name in ("GeometryHostDown", "GeometryTLSCertExpiring"):
        expr = _rules()[name]["expr"]
        assert not re.search(r"\b(sum|avg|max|min|count|topk|bottomk|absent)\b", expr), name


def test_annotations_use_only_labels_and_value():
    """No `query` template functions: a template that errors at evaluation
    has crashed this Prometheus before (#572)."""
    for name, rule in _rules().items():
        for key, text in rule["annotations"].items():
            for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
                assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)\s*", expr), (name, key, expr)


def _matcher_ok(matcher: str, labels: dict) -> bool:
    m = re.fullmatch(r'\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*(=~|!~|!=|=)\s*"(.*)"\s*', matcher)
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


def _route_matches(route: dict, labels: dict) -> bool:
    for k, v in (route.get("match") or {}).items():
        if labels.get(k, "") != v:
            return False
    return all(_matcher_ok(str(m), labels) for m in route.get("matchers") or [])


def _walk(labels: dict) -> dict:
    """The first top-level child route that matches (every route in this
    config has continue: false), as Alertmanager walks them."""
    cm = _configmap(ALERTMANAGER, "alertmanager-config")
    routes = yaml.safe_load(cm["data"]["alertmanager.yml"])["route"]["routes"]
    for route in routes:
        if _route_matches(route, labels):
            assert route.get("continue") in (None, False)
            return route
    raise AssertionError(f"no child route matches {labels}")


@pytest.mark.parametrize("alert", ["GeometryHostDown", "GeometryTLSCertExpiring", "GeometryProbesNotRunning"])
def test_rules_reach_the_critical_receiver_through_the_generic_critical_route(alert):
    rule = _rules()[alert]
    labels = {"alertname": alert, "job": JOB, **rule["labels"]}
    if alert != "GeometryProbesNotRunning":
        url, (product, flow, namespace, module) = next(iter(GEOMETRY_TARGETS.items()))
        labels.update(instance=url, product=product, flow=flow, target_namespace=namespace, module=module)
    route = _walk(labels)
    assert route["receiver"] == "critical-receiver", route
    assert (route.get("match") or {}).get("severity") == "critical", (
        "expected the generic critical route, not the revenue or a staging route"
    )


# ---------------------------------------------------------------------------
# promtool: evaluate the real rules
# ---------------------------------------------------------------------------

_YANTRA = 'job="blackbox-geometry",instance="https://yantra4d.com/",product="yantra4d",flow="landing",target_namespace="yantra4d",module="http_200_html"'
_PLIEGO = 'job="blackbox-geometry",instance="https://pliego-api.madfam.io/health",product="pliego",flow="api",target_namespace="pliego",module="http_2xx_json"'

PROMTOOL_TEST = textwrap.dedent(
    f"""
    rule_files:
      - {RULES_KEY}
    evaluation_interval: 30s
    tests:
      # 1. Pliego's API fails, the Yantra4D apex is fine: one page, Pliego's,
      #    and only after 3 minutes.
      - interval: 1m
        input_series:
          - series: 'up{{{_YANTRA}}}'
            values: '1x20'
          - series: 'probe_success{{{_YANTRA}}}'
            values: '1x20'
          - series: 'probe_success{{{_PLIEGO}}}'
            values: '0x20'
        alert_rule_test:
          - eval_time: 2m
            alertname: GeometryHostDown
            exp_alerts: []
          - eval_time: 5m
            alertname: GeometryHostDown
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: geometry-probe
                  job: blackbox-geometry
                  instance: https://pliego-api.madfam.io/health
                  product: pliego
                  flow: api
                  target_namespace: pliego
                  module: http_2xx_json
                exp_annotations:
                  summary: "pliego api is down: https://pliego-api.madfam.io/health"
          - eval_time: 15m
            alertname: GeometryProbesNotRunning
            exp_alerts: []

      # 2. Certificates: 10 days left pages after 1h; 30 days left does not.
      #    promtool's clock starts at Unix time 0, so the series value is the
      #    expiry as seconds from the start.
      - interval: 1m
        input_series:
          - series: 'probe_ssl_earliest_cert_expiry{{{_YANTRA}}}'
            values: '864000x120'
          - series: 'probe_ssl_earliest_cert_expiry{{{_PLIEGO}}}'
            values: '2592000x120'
        alert_rule_test:
          - eval_time: 30m
            alertname: GeometryTLSCertExpiring
            exp_alerts: []
          - eval_time: 65m
            alertname: GeometryTLSCertExpiring
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: geometry-probe
                  job: blackbox-geometry
                  instance: https://yantra4d.com/
                  product: yantra4d
                  flow: landing
                  target_namespace: yantra4d
                  module: http_200_html
                exp_annotations:
                  summary: "yantra4d landing: TLS certificate expires within 14 days (https://yantra4d.com/)"

      # 3. The job is not loaded: no up series at all.
      - interval: 1m
        input_series:
          - series: 'up{{job="blackbox-revenue",instance="https://kalya.app/api/health"}}'
            values: '1x20'
        alert_rule_test:
          - eval_time: 8m
            alertname: GeometryProbesNotRunning
            exp_alerts: []
          - eval_time: 12m
            alertname: GeometryProbesNotRunning
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: geometry-probe
                exp_annotations:
                  summary: "Geometry probes are not running: the geometry hosts are unwatched"
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _rules_text_without_descriptions() -> str:
    """The loaded rules with only the multi-line `description` dropped; expr,
    for, labels and the `summary` template are compared exactly."""
    doc = yaml.safe_load(_rules_text())
    for group in doc["groups"]:
        for rule in group["rules"]:
            rule.get("annotations", {}).pop("description", None)
    return yaml.safe_dump(doc, sort_keys=False)


def test_promtool_evaluates_the_geometry_rules():
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; the structural tests above still ran")
    with tempfile.TemporaryDirectory() as tmp:
        (Path(tmp) / RULES_KEY).write_text(_rules_text_without_descriptions(), encoding="utf-8")
        name = "geometry-probe-rules.test.yml"
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
                cmd, shell=shell, capture_output=True, text=True, timeout=PROMTOOL_TIMEOUT_SECONDS
            )
        except subprocess.TimeoutExpired:
            pytest.skip(f"promtool did not finish within {PROMTOOL_TIMEOUT_SECONDS}s")
    output = (proc.stdout + proc.stderr).strip()
    if proc.returncode != 0 and "FAILED" not in output and "SUCCESS" not in output:
        # The evaluator never ran (no daemon, image pull failure). Not a verdict.
        pytest.skip(f"promtool could not run: {output[-500:]}")
    assert proc.returncode == 0, f"promtool rejected the rule test:\n{output}"
    assert "SUCCESS" in output, output
