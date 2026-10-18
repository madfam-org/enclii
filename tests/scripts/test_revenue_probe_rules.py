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
    also as RevenueEndpointDown;
  * a list endpoint that answers a bare JSON array is judged by the array
    module, not by the status-field one it can never satisfy (Cotiza's guest
    quote list failed every probe that way from 2026-10-12);
  * each product pages as its own group: every rule carries the label the
    Alertmanager revenue route matches, and every target the `product` label
    that route groups by.

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
ALERTMANAGER = MONITORING / "alertmanager.yaml"

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
    # Added after the audit: Forgesight's API.
    ("forgesight", "api"),
    # Janua, judged by its readiness REPORT (R107/R109); see below.
    ("janua", "api"),
    # Crea Tu Mundo's ERP workspace host and MAP (infra-stability 2026-10-17,
    # E3 F3): the only vCTO client's hosts were watched by replica counts only.
    ("ctm-erp", "workspace"),
    ("ctm-map", "web"),
    # Money paths (money-engine wave 2, 2026-10-18): the pages and the
    # sign-in rail a buyer goes through, not only the APIs behind them.
    ("dhanam", "pricing-page"),
    ("dhanam", "signup-page"),
    ("janua", "oidc-discovery"),
    ("janua", "jwks"),
    ("janua", "cors-dhanam"),
    ("kalya", "booking-page"),
    ("selva", "pricing-page"),
    ("karafiel", "pricing-page"),
    ("dhanam", "fx-degraded"),
    ("karafiel", "cfdi-broker"),
}

# The money paths that take money today, and the steps each must contain.
# A path is a page (MoneyPathDown); removing a step needs a reason in review.
MONEY_PATHS = {
    "retainer": {("nauta", "web"), ("karafiel", "api"), ("janua", "api"), ("janua", "jwks"), ("janua", "oidc-discovery")},
    "dhanam-self-serve": {
        ("dhanam", "pricing-page"),
        ("dhanam", "signup-page"),
        ("dhanam", "api"),
        ("dhanam", "billing-catalog"),
        ("janua", "api"),
        ("janua", "oidc-discovery"),
        ("janua", "jwks"),
        ("janua", "cors-dhanam"),
    },
    "sales-booking": {("kalya", "web"), ("kalya", "booking-page"), ("madfam-site", "web")},
}

# The label the Alertmanager revenue route matches on (alertmanager.yaml).
CATEGORY = "revenue-probe"


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


def _group_rules(name: str) -> dict[str, dict]:
    groups = yaml.safe_load(_rules_text())["groups"]
    group = [g for g in groups if g.get("name") == name]
    assert len(group) == 1, f"expected exactly one `{name}` group"
    return {r["alert"]: r for r in group[0]["rules"]}


def _rules() -> dict[str, dict]:
    return _group_rules("revenue-probes")


def _money_rules() -> dict[str, dict]:
    return _group_rules("money-paths")


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


# Bodies the two JSON modules must tell apart. Go's regexp and Python's re
# agree on these patterns: `^` without (?m) anchors at the start of the body,
# and blackbox_exporter matches with a search, not a full match.
JSON_ARRAY_BODIES = ["[]", '[{"id":"q_1","status":"draft"}]', ' \n[\n  {"id": "q_1"}\n]']
NOT_ARRAY_BODIES = [
    '{"status":"ok"}',
    '{"error":"internal"}',
    "<!DOCTYPE html><html><body>[]</body></html>",
    "",
]


@pytest.mark.parametrize("body", JSON_ARRAY_BODIES)
def test_array_module_accepts_a_json_array(body):
    (pattern,) = _modules()["http_2xx_json_array"]["http"]["fail_if_body_not_matches_regexp"]
    assert re.search(pattern, body), body


@pytest.mark.parametrize("body", NOT_ARRAY_BODIES)
def test_array_module_rejects_objects_html_and_empty_bodies(body):
    (pattern,) = _modules()["http_2xx_json_array"]["http"]["fail_if_body_not_matches_regexp"]
    assert not re.search(pattern, body), body


def test_array_module_checks_only_the_body_shape():
    http = _modules()["http_2xx_json_array"]["http"]
    assert not http.get("valid_status_codes"), "2xx, like http_2xx_json"
    assert not http.get("fail_if_body_matches_regexp")
    assert http["headers"] == _modules()["http_2xx_json"]["http"]["headers"]
    assert http["preferred_ip_protocol"] == "ip4"
    assert http["method"] == "GET"


def test_status_module_cannot_pass_an_empty_list():
    """Why the array module exists: `[]` never satisfies http_2xx_json."""
    (pattern,) = _modules()["http_2xx_json"]["http"]["fail_if_body_not_matches_regexp"]
    assert not re.search(pattern, "[]")


def test_cotiza_guest_quote_list_is_judged_as_an_array():
    """It answers 200 `[]`; on http_2xx_json it paged falsely from 2026-10-12."""
    by_url = dict(_targets())
    labels = by_url["https://cotiza.studio/api/v1/guest/quotes"]
    assert labels["module"] == "http_2xx_json_array"
    assert (labels["product"], labels["flow"]) == ("cotiza", "web-api-proxy")
    # The only array target today; a second one belongs in this test with a
    # curl of its body in review.
    assert [u for u, l in _targets() if l["module"] == "http_2xx_json_array"] == [
        "https://cotiza.studio/api/v1/guest/quotes"
    ]


# ---------------------------------------------------------------------------
# Janua: the readiness report decides, not the status code
# ---------------------------------------------------------------------------
# Janua's readiness answers 200 while Redis or the database is down and
# reports the state in its body (internal-devops R107 and R109, 2026-10-04:
# readiness reports, it does not gate). A status-code check, or /health, which
# carries no Redis or database field, stays green through both outages. The
# module must fail on every body below that reports one of them down.

JANUA_READY_URL = "https://auth.madfam.io/api/v1/health/ready"

# The live answer, 2026-10-17T20:32Z (timestamp shortened).
JANUA_READY_OK = (
    '{"status":"ready","timestamp":"2026-10-17T20:32:18","database":{"healthy":true,'
    '"status":"healthy"},"redis":"healthy","redis_circuit":{"state":"closed",'
    '"last_failure_time":null,"fallback_calls":0,"strict_failures":0,'
    '"client_initialized":true},"degraded":[]}'
)
JANUA_READY_DOWN = {
    "redis-down": JANUA_READY_OK.replace('"status":"ready"', '"status":"degraded"')
    .replace('"redis":"healthy"', '"redis":"unhealthy"')
    .replace('"degraded":[]', '"degraded":["redis"]'),
    "database-down": JANUA_READY_OK.replace('"status":"ready"', '"status":"degraded"')
    .replace('"healthy":true,"status":"healthy"', '"healthy":false,"status":"unhealthy"')
    .replace('"degraded":[]', '"degraded":["database"]'),
    "both-down": JANUA_READY_OK.replace('"status":"ready"', '"status":"degraded"')
    .replace('"degraded":[]', '"degraded":["database","redis"]'),
    "breaker-open": JANUA_READY_OK.replace('"state":"closed"', '"state":"open"'),
    "breaker-half-open": JANUA_READY_OK.replace('"state":"closed"', '"state":"half_open"'),
    "database-flag-only": JANUA_READY_OK.replace('"healthy":true', '"healthy":false'),
    # /health's own answer: 200, and no readiness fields at all.
    "health-endpoint-body": '{"status":"healthy","version":"0.1.0","environment":"production"}',
    "empty": "",
}


def _blackbox_body_passes(module: str, body: str) -> bool:
    """blackbox_exporter's body rules: every not-match regex must match, and
    no match regex may. Go's regexp and Python's re agree on these patterns."""
    http = _modules()[module]["http"]
    must = http.get("fail_if_body_not_matches_regexp") or []
    must_not = http.get("fail_if_body_matches_regexp") or []
    return all(re.search(p, body) for p in must) and not any(re.search(p, body) for p in must_not)


def test_janua_is_probed_on_its_readiness_report():
    by_url = dict(_targets())
    labels = by_url[JANUA_READY_URL]
    assert labels["module"] == "http_janua_ready"
    assert (labels["product"], labels["flow"], labels["target_namespace"]) == ("janua", "api", "janua")
    # Janua's API health is judged ONLY by the readiness report; the other
    # Janua targets are the sign-in rail (discovery, keys, the browser's CORS
    # call) and the public pricing page, never a second "is the API up" probe
    # that cannot see Redis or the database.
    janua = {l["flow"]: u for u, l in _targets() if l["product"] == "janua"}
    assert janua["api"] == JANUA_READY_URL
    assert set(janua) == {"api", "oidc-discovery", "jwks", "cors-dhanam", "pricing-page"}, janua


def test_janua_module_passes_the_healthy_report():
    assert _blackbox_body_passes("http_janua_ready", JANUA_READY_OK)


@pytest.mark.parametrize("case", sorted(JANUA_READY_DOWN))
def test_janua_module_fails_when_redis_or_the_database_is_reported_down(case):
    assert not _blackbox_body_passes("http_janua_ready", JANUA_READY_DOWN[case]), case


def test_janua_module_keeps_the_common_conventions():
    http = _modules()["http_janua_ready"]["http"]
    assert not http.get("valid_status_codes"), "2xx: readiness answers 200 while degraded"
    assert http["headers"] == _modules()["http_2xx_json"]["http"]["headers"]
    assert http["preferred_ip_protocol"] == "ip4"
    assert http["method"] == "GET"


def test_json_401_module_differs_from_http_401_only_by_accept():
    """Nauta answers a request with no Accept header as a document navigation
    (307 to /login); the exporter sends no Accept, so a workspace host on plain
    http_401 fails every probe while healthy. Asking for JSON gets the 401."""
    plain = _modules()["http_401"]["http"]
    json_ = _modules()["http_401_json"]["http"]
    assert json_["valid_status_codes"] == [401]
    assert json_["headers"] == {**plain["headers"], "Accept": "application/json"}
    assert {k: v for k, v in json_.items() if k != "headers"} == {
        k: v for k, v in plain.items() if k != "headers"
    }


def test_client_workspace_host_is_probed_with_the_json_401_module():
    by_url = dict(_targets())
    labels = by_url["https://erp.creatumundo.mx/"]
    assert labels["module"] == "http_401_json"
    assert (labels["product"], labels["flow"], labels["target_namespace"]) == (
        "ctm-erp",
        "workspace",
        "nauta",
    )
    # The only target on it today; a second one belongs here, with a curl of
    # its no-Accept and JSON answers in review.
    assert [u for u, l in _targets() if l["module"] == "http_401_json"] == ["https://erp.creatumundo.mx/"]


def test_every_module_is_checked_below():
    """A new module must be added to the parametrized checks, not skip them."""
    assert set(_modules()) == set(MODULES)


MODULES = [
    "http_2xx_json",
    "http_2xx_json_array",
    "http_401",
    "http_dhanam_catalog",
    "http_dhanam_fx",
    "http_janua_ready",
    "http_401_json",
    "http_page_2xx",
    "http_pricing_page",
    "http_oidc_discovery",
    "http_jwks",
    "http_janua_cors_dhanam",
    "http_dhanam_fx_degraded",
    "http_karafiel_broker",
    # Used by the blackbox-geometry job; its targets and body check are
    # pinned in tests/scripts/test_geometry_probe_rules.py.
    "http_200_html",
]


@pytest.mark.parametrize("name", MODULES)
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
    assert set(rules) == {
        "RevenueEndpointDown",
        "RevenueEndpointDegraded",
        "DhanamFXEmergencyFallback",
        "RevenueProbesNotRunning",
    }
    severities = {name: rule["labels"]["severity"] for name, rule in rules.items()}
    assert severities == {
        "RevenueEndpointDown": "critical",
        "RevenueEndpointDegraded": "warning",
        "DhanamFXEmergencyFallback": "critical",
        "RevenueProbesNotRunning": "critical",
    }
    for name, rule in rules.items():
        assert f'job="{JOB}"' in rule["expr"], name
    assert 'check="fx-source"' in rules["RevenueEndpointDown"]["expr"]
    assert "unless" in rules["RevenueEndpointDown"]["expr"]
    # A money-path step pages once, as its path (MoneyPathDown), and a
    # warning target never pages: RevenueEndpointDown must exclude both.
    assert 'paging!="warning"' in rules["RevenueEndpointDown"]["expr"]
    assert 'money_path=""' in rules["RevenueEndpointDown"]["expr"]
    assert 'paging="warning"' in rules["RevenueEndpointDegraded"]["expr"]


def _alertmanager_revenue_route() -> dict:
    cm = _configmap(ALERTMANAGER, "alertmanager-config")
    routes = yaml.safe_load(cm["data"]["alertmanager.yml"])["route"]["routes"]
    matched = [
        r for r in routes
        if f'category="{CATEGORY}"' in [str(m) for m in r.get("matchers") or []]
    ]
    assert len(matched) == 1, f'expected one category="{CATEGORY}" route in alertmanager.yaml'
    return matched[0]


def test_every_product_pages_as_its_own_group():
    """The per-product Alertmanager route works only if both labels arrive.

    `category` is set on each rule; `product` is a static target label that
    Prometheus keeps on probe_success / probe_failed_due_to_regex and the
    rules keep on the alert (RevenueEndpointDown's `unless on (instance)`
    keeps the left-hand labels). Drop either and the alerts fall through to
    the generic critical route, one group per alertname again.
    """
    route = _alertmanager_revenue_route()
    assert "product" in route["group_by"] and "alertname" in route["group_by"]
    for name, rule in _rules().items():
        assert rule["labels"].get("category") == CATEGORY, name
        assert "product" not in rule["labels"], (
            f"{name}: a static product label would merge every product into one group"
        )
        assert not re.search(r"\b(sum|avg|max|min|count|topk|bottomk)\b", rule["expr"]), (
            f"{name}: an aggregation would drop the product label"
        )
    for url, labels in _targets():
        assert re.fullmatch(r"[a-z0-9][a-z0-9-]*", labels["product"]), url
    # One product, one target_namespace: product is a page, not a flow.
    ns = {}
    for _, labels in _targets():
        ns.setdefault(labels["product"], set()).add(labels["target_namespace"])
    assert all(len(v) == 1 for v in ns.values()), ns


# ---------------------------------------------------------------------------
# Money paths (money-engine wave 2, 2026-10-18)
# ---------------------------------------------------------------------------
# A money path is the chain of public steps a payment goes through TODAY. A
# step failing pages once for its path (MoneyPathDown); the step itself is a
# warning (MoneyPathStepFailing). Targets of SKUs that cannot be bought
# self-serve today carry `paging: warning` (decision 6 of the journeys plan,
# as recommended; unruled when this was written) and never page.


def _money_path_members() -> dict[str, set[tuple[str, str]]]:
    out: dict[str, set[tuple[str, str]]] = {}
    for _, labels in _targets():
        for path in [p for p in labels.get("money_path", "").split(",") if p]:
            out.setdefault(path, set()).add((labels["product"], labels["flow"]))
    return out


def test_money_path_labels_are_comma_wrapped_names():
    for url, labels in _targets():
        mp = labels.get("money_path")
        if mp is None:
            continue
        assert re.fullmatch(r",([a-z0-9][a-z0-9-]*,)+", mp), (url, mp)


def test_money_paths_have_exactly_their_steps():
    assert _money_path_members() == MONEY_PATHS


def test_money_path_rule_counts_every_path_and_names_it():
    expr = _money_rules()["MoneyPathDown"]["expr"]
    for path in MONEY_PATHS:
        assert f'money_path=~".*,{path},.*"' in expr, path
        # The page is keyed by `product` (Alertmanager's revenue route groups
        # by it; Courier's once-per-episode key reads it), so the path's name
        # must be written into both labels.
        assert f'"money_path", "{path}", "", ""' in expr, path
        assert f'"product", "{path}", "", ""' in expr, path
    named = set(re.findall(r'money_path=~"\.\*,([a-z0-9-]+),\.\*"', expr))
    assert named == set(MONEY_PATHS), named


def test_money_path_names_do_not_collide_with_products():
    products = {l["product"] for _, l in _targets()}
    assert not products & set(MONEY_PATHS)


def test_money_path_rules():
    rules = _money_rules()
    assert set(rules) == {"MoneyPathDown", "MoneyPathStepFailing"}
    assert rules["MoneyPathDown"]["labels"] == {"severity": "critical", "category": CATEGORY}
    assert rules["MoneyPathStepFailing"]["labels"] == {"severity": "warning", "category": CATEGORY}
    assert rules["MoneyPathDown"]["for"] == "3m"
    assert 'money_path!=""' in rules["MoneyPathStepFailing"]["expr"]
    for name, rule in rules.items():
        for key, text in rule["annotations"].items():
            for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
                assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)\s*", expr), (name, key, expr)


def test_warning_targets_are_not_money_path_steps():
    for url, labels in _targets():
        if "paging" in labels:
            assert labels["paging"] == "warning", (url, labels["paging"])
            assert "money_path" not in labels, f"{url}: a warning target cannot be a step of a paging path"


def test_every_target_either_pages_or_warns_on_purpose():
    """Every NEW target is placed explicitly: on a money path, or warning."""
    for url, labels in _targets():
        if (labels["product"], labels["flow"]) in {
            ("dhanam", "pricing-page"),
            ("dhanam", "signup-page"),
            ("janua", "oidc-discovery"),
            ("janua", "jwks"),
            ("janua", "cors-dhanam"),
            ("kalya", "booking-page"),
            ("selva", "pricing-page"),
            ("karafiel", "pricing-page"),
            ("avala", "pricing-page"),
            ("tezca", "pricing-page"),
            ("routecraft", "pricing-page"),
            ("janua", "pricing-page"),
            ("enclii", "pricing-landing"),
            ("dhanam", "fx-degraded"),
            ("karafiel", "cfdi-broker"),
        }:
            assert ("money_path" in labels) != ("paging" in labels), url


# Bodies recorded from the live endpoints on 2026-10-18 (17:20–17:31Z),
# shortened, and the failures each module exists to catch.
PAGE_OK = '<!DOCTYPE html><html lang="es"><head><title>Dhanam</title></head><body><div id="__next"></div></body></html>'
PRICING_OK = '<!DOCTYPE html><html lang="es"><body><h3>Developer</h3><p>$1,159 MXN / mes</p><p>IVA incluido</p></body></html>'
OIDC_OK = (
    '{"issuer":"https://auth.madfam.io","authorization_endpoint":"https://auth.madfam.io/api/v1/oauth/authorize",'
    '"jwks_uri":"https://auth.madfam.io/.well-known/jwks.json"}'
)
JWKS_OK = '{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"janua-primary-key","n":"x","e":"AQAB"}]}'
FX_OK = (
    '{"service":"fx-rates","status":"healthy","testRate":{"from":"USD","to":"MXN","rate":18.493,'
    '"source":"banxico","degraded":false,"asOf":null}}'
)
KARAFIEL_OK = (
    '{"status": "healthy", "service": "karafiel-api", "services": {"database": "connected", '
    '"redis": "connected"}, "timestamp": "2026-10-18T17:31:29.453643+00:00"}'
)
CATALOG_OK = (
    '{"products":[{"slug":"dhanam","tiers":[{"slug":"free","grossPrices":{}},{"slug":"essentials",'
    '"grossPrices":{"MXN":{"monthly":15000,"yearly":143700,"ivaRate":0.16}}}]}],'
    '"updatedAt":"2026-10-18T17:20:36.633Z"}'
)

MODULE_CASES = {
    "http_page_2xx": ([PAGE_OK, PRICING_OK], ['{"error":"not found"}', "", "Service Unavailable"]),
    "http_pricing_page": (
        [PRICING_OK],
        [PAGE_OK, '<html><body>Precios</body></html>', '<html><body>IVA</body></html>', '{"products":[]}'],
    ),
    "http_oidc_discovery": (
        [OIDC_OK],
        [
            OIDC_OK.replace("https://auth.madfam.io", "http://localhost:8000"),
            '{"issuer":"https://auth.madfam.io"}',
            "<html></html>",
        ],
    ),
    "http_jwks": ([JWKS_OK], ['{"keys":[]}', '{"keys": [ ]}', "", "<html></html>"]),
    "http_dhanam_fx_degraded": (
        [FX_OK, FX_OK.replace('"source":"banxico"', '"source":"database"')],
        [FX_OK.replace('"degraded":false', '"degraded":true')],
    ),
    "http_karafiel_broker": (
        [KARAFIEL_OK],
        [
            KARAFIEL_OK.replace('"redis": "connected"', '"redis": "error"'),
            KARAFIEL_OK.replace('"database": "connected"', '"database": "error"'),
            '{"status": "alive"}',
        ],
    ),
    "http_dhanam_catalog": (
        [CATALOG_OK],
        [
            '{"products":[],"updatedAt":"x"}',
            CATALOG_OK.replace('"MXN":{"monthly":15000', '"MXN":{"monthly":0'),
            '{"products":[{"slug":"dhanam","tiers":[{"slug":"free","grossPrices":{}}]}]}',
        ],
    ),
}


@pytest.mark.parametrize("module", sorted(MODULE_CASES))
def test_new_modules_pass_the_live_answer_and_fail_the_broken_ones(module):
    good, bad = MODULE_CASES[module]
    for body in good:
        assert _blackbox_body_passes(module, body), (module, body[:80])
    for body in bad:
        assert not _blackbox_body_passes(module, body), (module, body[:80])


def test_fx_degraded_module_has_one_regex_and_fx_source_keeps_its_own():
    http = _modules()["http_dhanam_fx_degraded"]["http"]
    assert http.get("fail_if_body_matches_regexp") == ['"degraded"\\s*:\\s*true']
    assert not http.get("fail_if_body_not_matches_regexp")
    by_url = dict(_targets())
    assert by_url["https://api.dhan.am/v1/fx-rates/health?probe=fx-degraded"]["paging"] == "warning"


def test_janua_cors_probe_sends_dhanams_origin_and_requires_it_back():
    http = _modules()["http_janua_cors_dhanam"]["http"]
    assert http["valid_status_codes"] == [401]
    assert http["headers"]["Origin"] == "https://app.dhan.am"
    (match,) = http["fail_if_header_not_matches"]
    assert match["header"].lower() == "access-control-allow-origin"
    assert re.search(match["regexp"], "https://app.dhan.am")
    assert not re.search(match["regexp"], "*")
    assert not re.search(match["regexp"], "https://app.dhan.am.evil.example")
    assert not match.get("allow_missing"), "a missing header must fail"


def test_pricing_modules_never_pin_a_price_value():
    """Prices belong to Tulana and Dhanam; a probe that pins one pages on
    every legitimate price change."""
    for name in ("http_page_2xx", "http_pricing_page"):
        for pattern in _modules()[name]["http"].get("fail_if_body_not_matches_regexp", []):
            assert not re.search(r"\d", pattern), (name, pattern)


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

      # 4. Two products fail together: two alerts, each carrying its own
      #    `product`, which is what the Alertmanager revenue route groups by.
      #    The guest-quote list on the array module is one of them.
      - interval: 1m
        input_series:
          - series: 'probe_success{job="blackbox-revenue",instance="https://cotiza.studio/api/v1/guest/quotes",product="cotiza",flow="web-api-proxy",target_namespace="digifab-quoting",check="availability",module="http_2xx_json_array"}'
            values: '0x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://api.forgesight.app/health",product="forgesight",flow="api",target_namespace="forgesight",check="availability",module="http_2xx_json"}'
            values: '0x20'
        alert_rule_test:
          - eval_time: 5m
            alertname: RevenueEndpointDown
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://cotiza.studio/api/v1/guest/quotes
                  product: cotiza
                  flow: web-api-proxy
                  target_namespace: digifab-quoting
                  check: availability
                  module: http_2xx_json_array
                exp_annotations:
                  summary: "cotiza web-api-proxy is failing: https://cotiza.studio/api/v1/guest/quotes"
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://api.forgesight.app/health
                  product: forgesight
                  flow: api
                  target_namespace: forgesight
                  check: availability
                  module: http_2xx_json
                exp_annotations:
                  summary: "forgesight api is failing: https://api.forgesight.app/health"

      # 5. Money paths. Janua's readiness (a step of two paths) and
      #    dhan.am/pricing fail; a warning-only pricing page fails; Nauta is
      #    fine. One page per broken PATH, none per step; the steps and the
      #    warning page go to the digest.
      - interval: 1m
        input_series:
          - series: 'probe_success{job="blackbox-revenue",instance="https://auth.madfam.io/api/v1/health/ready",product="janua",flow="api",target_namespace="janua",check="availability",module="http_janua_ready",money_path=",retainer,dhanam-self-serve,"}'
            values: '0x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://dhan.am/pricing",product="dhanam",flow="pricing-page",target_namespace="dhanam",check="availability",module="http_page_2xx",money_path=",dhanam-self-serve,"}'
            values: '0x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://app.nauta.quest/health",product="nauta",flow="web",target_namespace="nauta",check="availability",module="http_2xx_json",money_path=",retainer,"}'
            values: '1x20'
          - series: 'probe_success{job="blackbox-revenue",instance="https://selva.town/pricing",product="selva",flow="pricing-page",target_namespace="selva",check="availability",module="http_pricing_page",paging="warning"}'
            values: '0x20'
        alert_rule_test:
          - eval_time: 2m
            alertname: MoneyPathDown
            exp_alerts: []
          - eval_time: 5m
            alertname: MoneyPathDown
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  money_path: retainer
                  product: retainer
                exp_annotations:
                  summary: "Money path retainer is broken: 1 step(s) failing"
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  money_path: dhanam-self-serve
                  product: dhanam-self-serve
                exp_annotations:
                  summary: "Money path dhanam-self-serve is broken: 2 step(s) failing"
          # The steps never page as RevenueEndpointDown, and neither does the
          # warning target.
          - eval_time: 15m
            alertname: RevenueEndpointDown
            exp_alerts: []
          - eval_time: 5m
            alertname: MoneyPathStepFailing
            exp_alerts:
              - exp_labels:
                  severity: warning
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://auth.madfam.io/api/v1/health/ready
                  product: janua
                  flow: api
                  target_namespace: janua
                  check: availability
                  module: http_janua_ready
                  money_path: ",retainer,dhanam-self-serve,"
                exp_annotations:
                  summary: "Money path step failing (,retainer,dhanam-self-serve,): janua api https://auth.madfam.io/api/v1/health/ready"
              - exp_labels:
                  severity: warning
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://dhan.am/pricing
                  product: dhanam
                  flow: pricing-page
                  target_namespace: dhanam
                  check: availability
                  module: http_page_2xx
                  money_path: ",dhanam-self-serve,"
                exp_annotations:
                  summary: "Money path step failing (,dhanam-self-serve,): dhanam pricing-page https://dhan.am/pricing"
          - eval_time: 8m
            alertname: RevenueEndpointDegraded
            exp_alerts: []
          - eval_time: 12m
            alertname: RevenueEndpointDegraded
            exp_alerts:
              - exp_labels:
                  severity: warning
                  category: revenue-probe
                  job: blackbox-revenue
                  instance: https://selva.town/pricing
                  product: selva
                  flow: pricing-page
                  target_namespace: selva
                  check: availability
                  module: http_pricing_page
                  paging: warning
                exp_annotations:
                  summary: "selva pricing-page is failing (warning only): https://selva.town/pricing"
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
