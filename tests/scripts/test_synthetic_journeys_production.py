"""
Tests for the weekly PRODUCTION journeys pass
(infra/k8s/production/synthetics/cronjob-production-weekly.yaml) and its
warning-only rules (group synthetic-journeys-production in
synthetic-journeys-rules.yml, monitoring/prometheus.yaml).

Run with:
    pytest tests/scripts/test_synthetic_journeys_production.py -v

WHY THIS EXISTS
===============
The staging synthetic buyer (cronjob.yaml) is allowed to sign in, open a
Stripe TEST checkout and type a test card, on staging only. The production
pass must never do any of that: the runner's `pre-session` mode is GET-only,
types nothing, signs in nowhere and stops before a checkout session exists.
That guarantee lives in the runner's code (internal-devops tools/journeys);
what this manifest controls is pinned here:

  * it runs `pre-session`, never `staging-full`, and mounts no Secret and no
    identity variable at all;
  * it runs weekly, alone (Forbid), never retries, and ends within 4h;
  * the geometry-fabrication products' journeys are filtered out (another
    workstream owns them);
  * it shares the staging buyer's sandbox (same pod labels, so the same
    default-deny egress policy) and both runs fit the namespace quota;
  * its rules are WARNINGS (decision 6 recommendation) and read only
    env="production" series, and the staging rules read only env="staging".
"""
from __future__ import annotations

import re
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
SYNTHETICS = REPO_ROOT / "infra" / "k8s" / "production" / "synthetics"
PROMETHEUS = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring" / "prometheus.yaml"
WEEKLY = SYNTHETICS / "cronjob-production-weekly.yaml"
STAGING = SYNTHETICS / "cronjob.yaml"
RULES_KEY = "synthetic-journeys-rules.yml"

# Journey ids the filter must drop (geometry fabrication) and keep (money engine).
DROPPED = [
    "cotiza.creator_pro.pricing-to-boundary",
    "entry.yantra4d-home",
    "forj.studio.pricing-to-boundary",
    "pravara-mes.growth.pricing-to-boundary",
    "demo.fashioncabi-studio",
    "demo.pliego-studio",
    "ladder.cotiza",
]
KEPT = [
    "dhanam.essentials.pricing-to-boundary",
    "karafiel.contador.pricing-to-boundary",
    "entry.dhanam-home",
    "ladder.selva",
    "entry.enclii-docs",
    "demo.tezca-search",
]


def _doc(path: Path, kind: str) -> dict:
    (doc,) = [d for d in yaml.safe_load_all(path.read_text(encoding="utf-8")) if isinstance(d, dict) and d.get("kind") == kind]
    return doc


def _weekly() -> dict:
    return _doc(WEEKLY, "CronJob")


def _pod(cron: dict) -> dict:
    return cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]


def _container(cron: dict) -> dict:
    (c,) = _pod(cron)["containers"]
    return c


def _env(cron: dict) -> dict[str, dict]:
    return {e["name"]: e for e in _container(cron)["env"]}


def test_listed_in_the_kustomization():
    kust = yaml.safe_load((SYNTHETICS / "kustomization.yaml").read_text(encoding="utf-8"))
    assert "cronjob-production-weekly.yaml" in kust["resources"]


def test_runs_pre_session_and_holds_no_secret():
    cron = _weekly()
    env = _env(cron)
    assert env["JOURNEYS_MODE"]["value"] == "pre-session"
    for e in env.values():
        assert "valueFrom" not in e, f"{e['name']}: the production pass reads no Secret"
    assert not any(k.startswith("DHANAM_STAGING_SYNTH") for k in env)
    c = _container(cron)
    assert "envFrom" not in c
    assert all("secret" not in v for v in _pod(cron)["volumes"])
    assert _pod(cron)["automountServiceAccountToken"] is False


def test_schedule_and_job_shape():
    spec = _weekly()["spec"]
    assert spec["schedule"] == "0 11 * * 1", "Monday 11:00 UTC, before the Monday report"
    assert spec["timeZone"] == "Etc/UTC"
    assert spec["concurrencyPolicy"] == "Forbid"
    job = spec["jobTemplate"]["spec"]
    assert job["backoffLimit"] == 0
    assert job["activeDeadlineSeconds"] <= 4 * 3600
    assert _pod(_weekly())["restartPolicy"] == "Never"


def test_filter_drops_geometry_journeys_and_keeps_the_money_engine():
    pattern = _env(_weekly())["JOURNEYS_FILTER"]["value"]
    # The runner is JavaScript; this pattern (negative lookahead) means the
    # same in Python's re.
    for jid in DROPPED:
        assert not re.search(pattern, jid), jid
    for jid in KEPT:
        assert re.search(pattern, jid), jid


def test_politeness_and_metrics():
    env = _env(_weekly())
    assert int(env["JOURNEYS_WORKERS"]["value"]) <= 2
    assert float(env["JOURNEYS_RPS"]["value"]) <= 1
    assert env["JOURNEYS_PUSHGATEWAY_URL"]["value"] == "http://pushgateway.monitoring.svc.cluster.local:9091"


def test_prints_the_summary_and_keeps_the_exit_code():
    c = _container(_weekly())
    assert c["command"] == ["/bin/sh", "-c"]
    (script,) = c["args"]
    assert "npx playwright test; rc=$?" in script
    assert 'cat "$JOURNEYS_OUT/summary.md"' in script
    assert script.rstrip().endswith("exit $rc")


def test_same_sandbox_as_the_staging_buyer():
    weekly, staging = _weekly(), _doc(STAGING, "CronJob")
    wl = weekly["spec"]["jobTemplate"]["spec"]["template"]["metadata"]["labels"]
    sl = staging["spec"]["jobTemplate"]["spec"]["template"]["metadata"]["labels"]
    assert wl["app.kubernetes.io/name"] == sl["app.kubernetes.io/name"] == "journeys", "egress policy selects this label"
    assert _container(weekly)["image"] == _container(staging)["image"] == "journeys"
    assert _container(weekly)["securityContext"] == _container(staging)["securityContext"]
    assert _pod(weekly)["securityContext"] == _pod(staging)["securityContext"]


def _cpu(v: str) -> float:
    return float(v[:-1]) / 1000 if str(v).endswith("m") else float(v)


def _mem(v: str) -> int:
    return int(float(v[:-2]) * {"Mi": 2**20, "Gi": 2**30}[v[-2:]])


def test_both_runs_fit_the_quota_at_once():
    hard = _doc(SYNTHETICS / "resource-quota.yaml", "ResourceQuota")["spec"]["hard"]
    crons = [_weekly(), _doc(STAGING, "CronJob")]
    res = [_container(c)["resources"] for c in crons]
    assert sum(_cpu(r["requests"]["cpu"]) for r in res) <= _cpu(hard["requests.cpu"])
    assert sum(_mem(r["requests"]["memory"]) for r in res) <= _mem(hard["requests.memory"])
    assert sum(_cpu(r["limits"]["cpu"]) for r in res) <= _cpu(hard["limits.cpu"])
    assert sum(_mem(r["limits"]["memory"]) for r in res) <= _mem(hard["limits.memory"])
    assert int(hard["pods"]) >= 2
    history = sum(c["spec"]["successfulJobsHistoryLimit"] + c["spec"]["failedJobsHistoryLimit"] + 1 for c in crons)
    assert history <= int(hard["count/jobs.batch"])


def _groups() -> dict[str, dict]:
    for d in yaml.safe_load_all(PROMETHEUS.read_text(encoding="utf-8")):
        if isinstance(d, dict) and d.get("kind") == "ConfigMap" and d["metadata"]["name"] == "prometheus-rules":
            return {g["name"]: g for g in yaml.safe_load(d["data"][RULES_KEY])["groups"]}
    raise AssertionError("prometheus-rules not found")


def test_production_rules_are_warnings_on_production_series_only():
    rules = {r["alert"]: r for r in _groups()["synthetic-journeys-production"]["rules"]}
    assert set(rules) == {"ProductionJourneysFailing", "ProductionJourneysNotRunning"}
    for name, r in rules.items():
        assert r["labels"]["severity"] == "warning", name
        assert r["labels"]["category"] != "revenue-probe", f"{name}: the revenue route pages"
        for sel in re.findall(r"synthetic_journey_[a-z_]+\{[^}]*\}", r["expr"]):
            assert 'env="production"' in sel, (name, sel)
    assert 'cronjob="journeys-production-weekly"' in rules["ProductionJourneysNotRunning"]["expr"]


def test_staging_rules_never_read_production_series():
    for r in _groups()["synthetic-journeys"]["rules"]:
        for sel in re.findall(r"synthetic_journey_[a-z_]+\{[^}]*\}", r["expr"]):
            assert 'env="staging"' in sel, (r["alert"], sel)
