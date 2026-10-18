"""
Tests for scripts/money-engine-status.py, the one-command money engine read.

Run with:
    pytest tests/scripts/test_money_engine_status.py -v

No network: every fetch is a stub. What is pinned:

  * it reads exactly the probe targets and modules Prometheus uses (so the
    outside read and the pager can never judge different URLs or rules);
  * its judge applies the blackbox rules this estate uses (status, body
    regexes both ways, header regexes, redirects as failures);
  * a broken money-path step breaks its path and sets exit code 1, and a
    warning-only target never changes the exit code;
  * the prober-log parser counts the blackbox-exporter's "Probe failed" lines.
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "money-engine-status.py"


def _load():
    spec = importlib.util.spec_from_file_location("money_engine_status", SCRIPT)
    mod = importlib.util.module_from_spec(spec)
    sys.modules["money_engine_status"] = mod
    spec.loader.exec_module(mod)
    return mod


mes = _load()


def _prometheus_targets() -> list[tuple[str, dict]]:
    path = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring" / "prometheus.yaml"
    for doc in yaml.safe_load_all(path.read_text(encoding="utf-8")):
        if isinstance(doc, dict) and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "prometheus-config":
            cfg = yaml.safe_load(doc["data"]["prometheus.yml"])
            (job,) = [j for j in cfg["scrape_configs"] if j["job_name"] == "blackbox-revenue"]
            return [(u, sc.get("labels") or {}) for sc in job["static_configs"] for u in sc["targets"]]
    raise AssertionError("prometheus-config not found")


def test_reads_the_same_targets_as_prometheus():
    assert [(t.url, t.labels) for t in mes.load_targets()] == _prometheus_targets()


def test_every_target_module_is_defined():
    modules = mes.load_modules()
    for t in mes.load_targets():
        assert t.labels["module"] in modules, t.url


def test_money_paths_are_read_from_the_label():
    t = mes.Target("https://x", {"money_path": ",retainer,dhanam-self-serve,"})
    assert t.paths == ["retainer", "dhanam-self-serve"]
    assert mes.Target("https://x", {}).paths == []
    assert mes.Target("https://x", {"paging": "warning"}).warning_only


MODULE_2XX = {"http": {"fail_if_body_not_matches_regexp": ['"status"\\s*:']}}
MODULE_401 = {"http": {"valid_status_codes": [401]}}
MODULE_CORS = {
    "http": {
        "valid_status_codes": [401],
        "fail_if_header_not_matches": [{"header": "Access-Control-Allow-Origin", "regexp": "^https://app\\.dhan\\.am$"}],
    }
}
MODULE_NOT = {"http": {"fail_if_body_matches_regexp": ['"source"\\s*:\\s*"emergency_fallback"']}}


@pytest.mark.parametrize(
    "module,status,body,headers,ok",
    [
        (MODULE_2XX, 200, '{"status":"ok"}', [], True),
        (MODULE_2XX, 200, "<html>", [], False),
        (MODULE_2XX, 503, '{"status":"down"}', [], False),
        (MODULE_2XX, 307, "", [], False),  # a redirect is a failure, never followed
        (MODULE_401, 401, "", [], True),
        (MODULE_401, 200, "", [], False),
        (MODULE_CORS, 401, "", [("access-control-allow-origin", "https://app.dhan.am")], True),
        (MODULE_CORS, 401, "", [], False),
        (MODULE_CORS, 401, "", [("Access-Control-Allow-Origin", "*")], False),
        (MODULE_NOT, 200, '{"source":"banxico"}', [], True),
        (MODULE_NOT, 200, '{"source":"emergency_fallback"}', [], False),
    ],
)
def test_judge_applies_the_blackbox_rules(module, status, body, headers, ok):
    assert (mes.judge(module, status, body, headers) == []) is ok


def _stub(answers: dict[str, tuple[int, str, list]]):
    def fetcher(url, module):
        return answers.get(url, (200, '{"status":"ok"}', []))

    return fetcher


def test_a_failing_step_breaks_its_path():
    targets = [
        mes.Target("https://a/health", {"product": "a", "flow": "api", "module": "http_2xx_json", "money_path": ",p1,p2,"}),
        mes.Target("https://b/health", {"product": "b", "flow": "api", "module": "http_2xx_json", "money_path": ",p2,"}),
        mes.Target("https://c/pricing", {"product": "c", "flow": "pricing-page", "module": "http_2xx_json", "paging": "warning"}),
    ]
    modules = {"http_2xx_json": MODULE_2XX}
    fetcher = _stub({"https://b/health": (503, "", [])})
    results = [mes.probe(t, modules, fetcher) for t in targets]
    paths = mes.money_paths(results)
    assert {k: all(s.ok for s in v) for k, v in paths.items()} == {"p1": True, "p2": False}
    screen = mes.render(results, None, "")
    assert "BROKEN  p2" in screen and "OK      p1" in screen
    assert "failing=1" in screen


def test_exit_code_ignores_warning_only_targets(monkeypatch, capsys):
    targets = [
        mes.Target("https://a/health", {"product": "a", "flow": "api", "module": "m", "money_path": ",p1,"}),
        mes.Target("https://c/pricing", {"product": "c", "flow": "pricing-page", "module": "m", "paging": "warning"}),
    ]
    modules = {"m": MODULE_2XX}
    real_probe = mes.probe
    monkeypatch.setattr(mes, "load_targets", lambda: targets)
    monkeypatch.setattr(mes, "load_modules", lambda: modules)

    monkeypatch.setattr(mes, "probe", lambda t, m: real_probe(t, m, _stub({"https://c/pricing": (500, "", [])})))
    assert mes.main([]) == 0
    assert "digest (warning): 0/1 pass" in capsys.readouterr().out

    monkeypatch.setattr(mes, "probe", lambda t, m: real_probe(t, m, _stub({"https://a/health": (500, "", [])})))
    assert mes.main([]) == 1


def test_no_answer_is_a_failure():
    t = mes.Target("https://a/health", {"product": "a", "flow": "api", "module": "m"})
    r = mes.probe(t, {"m": MODULE_2XX}, lambda url, module: (0, "", []))
    assert not r.ok and r.reasons == ["no answer (timeout, DNS or TLS)"]


PROBER_LOG = "\n".join(
    [
        'time=2026-10-17T22:26:50.497Z level=INFO source=main.go:98 msg="Starting blackbox_exporter" version="(version=0.28.0)"',
        'time=2026-10-18T04:37:53.696Z level=ERROR source=http.go:605 msg="Invalid HTTP response status code, wanted 2xx" module=http_2xx_json target=https://api.dhan.am/health status_code=503',
        'time=2026-10-18T04:37:53.696Z level=ERROR source=handler.go:130 msg="Probe failed" module=http_2xx_json target=https://api.dhan.am/health duration_seconds=0.14',
        'time=2026-10-18T04:38:53.752Z level=ERROR source=handler.go:130 msg="Probe failed" module=http_2xx_json target=https://api.dhan.am/health duration_seconds=0.20',
        'time=2026-10-18T12:49:23.285Z level=ERROR source=handler.go:130 msg="Probe failed" module=http_2xx_json target=https://api.yantra4d.com/api/health duration_seconds=0.25',
    ]
)


def test_prober_log_counts_probe_failures_per_target():
    assert mes.parse_prober_log(PROBER_LOG) == {
        "https://api.dhan.am/health": {"failures": 2, "first": "2026-10-18T04:37:53.696Z", "last": "2026-10-18T04:38:53.752Z"},
        "https://api.yantra4d.com/api/health": {"failures": 1, "first": "2026-10-18T12:49:23.285Z", "last": "2026-10-18T12:49:23.285Z"},
    }


def test_cluster_read_without_the_cli_says_so(monkeypatch):
    monkeypatch.setattr(mes.shutil, "which", lambda name: None)
    assert mes.read_cluster_prober(100) == (None, "enclii CLI not on PATH")
