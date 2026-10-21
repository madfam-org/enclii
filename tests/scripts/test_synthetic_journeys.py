"""
Tests for the S-Gate 1 synthetic buyer: infra/k8s/production/synthetics, its
Argo Application, its pushgateway ingress, its intake target and the
`synthetic-journeys` rule group in prometheus.yaml.

Run with:
    pytest tests/scripts/test_synthetic_journeys.py -v

    # with the real rule evaluator, from the image production runs:
    ENCLII_PROMTOOL="docker run --rm --entrypoint /bin/promtool -v {dir}:/w:ro \\
        docker.io/prom/prometheus:v2.53.3 test rules /w/{name}" \\
        pytest tests/scripts/test_synthetic_journeys.py -v

WHY THIS EXISTS
===============
A synthetic check is only as good as the wiring nobody looks at. Each way
this one could silently stop working is pinned here:

  * every manifest is listed in the kustomization (`resources:` is an
    allowlist) and the Argo Application renders that path;
  * the CronJob runs the staging-full journey, once at a time, with no retry,
    non-root and read-only, with the identity env taken from the
    ExternalSecret whose keys are exactly the intake target's;
  * the runner can reach DNS, public 443 and the pushgateway, and the
    pushgateway admits it; Prometheus scrapes the pushgateway with
    honor_labels (the 2026-08-11 "never scraped" lesson);
  * the rules page (critical, category revenue-probe) with the product the
    runner pushes, NotRunning does not double-page a failure CheckoutFailing
    already covers, and the legacy synthetic-flow-probe rule no longer fires
    on the runner's series;
  * the image is pinned to a published digest. Until it is, the placeholder
    test below FAILS ON PURPOSE: that is the merge gate.

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

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
SYNTHETICS = REPO_ROOT / "infra" / "k8s" / "production" / "synthetics"
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
PROMETHEUS = MONITORING / "prometheus.yaml"
ALERTMANAGER = MONITORING / "alertmanager.yaml"
NETWORK_POLICIES = MONITORING / "network-policies.yaml"
PUSHGATEWAY_FILE = MONITORING / "deploy-pipeline-monitor.yaml"
ARGO_APP = REPO_ROOT / "infra" / "argocd" / "apps" / "synthetics.yaml"
ROOT_APP = REPO_ROOT / "infra" / "argocd" / "root-application.yaml"
REGISTRY = REPO_ROOT / "apps" / "switchyard-api" / "internal" / "secretsintake" / "registry.yaml"

RULES_KEY = "synthetic-journeys-rules.yml"
LEGACY_RULES_KEY = "synthetic-flow-probe-rules.yml"
JOURNEY = "dhanam.staging.essentials.checkout"
PRODUCT = "dhanam-staging-synthetic"
CRONJOB = "journeys-dhanam-staging-checkout"
INTAKE_TARGET = "synthetics/dhanam-staging-checkout"
CATEGORY = "revenue-probe"
ZERO_DIGEST = "sha256:" + "0" * 64
IDENTITY_ENV = {
    "DHANAM_STAGING_SYNTH_EMAIL": "dhanam_staging_synth_email",
    "DHANAM_STAGING_SYNTH_PASSWORD": "dhanam_staging_synth_password",  # pragma: allowlist secret (Vault property name, not a value)
}


def _docs(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]


def _one(path: Path, kind: str, name: str | None = None) -> dict:
    found = [d for d in _docs(path) if d.get("kind") == kind and (name is None or d["metadata"]["name"] == name)]
    assert len(found) == 1, f"expected one {kind} {name or ''} in {path}, found {len(found)}"
    return found[0]


def _kustomization() -> dict:
    return yaml.safe_load((SYNTHETICS / "kustomization.yaml").read_text(encoding="utf-8"))


def _cronjob() -> dict:
    return _one(SYNTHETICS / "cronjob.yaml", "CronJob", CRONJOB)


def _pod_spec() -> dict:
    return _cronjob()["spec"]["jobTemplate"]["spec"]["template"]["spec"]


def _container() -> dict:
    (container,) = _pod_spec()["containers"]
    return container


def _env() -> dict[str, dict]:
    return {e["name"]: e for e in _container()["env"]}


@lru_cache(maxsize=1)
def _rules_cm() -> dict:
    return _one(PROMETHEUS, "ConfigMap", "prometheus-rules")


def _rules() -> dict[str, dict]:
    groups = yaml.safe_load(_rules_cm()["data"][RULES_KEY])["groups"]
    (group,) = [g for g in groups if g.get("name") == "synthetic-journeys"]
    return {r["alert"]: r for r in group["rules"]}


# ---------------------------------------------------------------------------
# Rendering: allowlists and the Argo Application
# ---------------------------------------------------------------------------


def test_every_manifest_in_the_directory_is_listed_in_the_kustomization():
    listed = set(_kustomization()["resources"])
    on_disk = {p.name for p in SYNTHETICS.glob("*.yaml") if p.name != "kustomization.yaml"}
    assert on_disk == listed, f"unlisted: {sorted(on_disk - listed)}; missing: {sorted(listed - on_disk)}"
    assert _kustomization()["namespace"] == "synthetics"


@pytest.mark.skipif(not shutil.which("git"), reason="git not on PATH")
def test_no_listed_manifest_is_gitignored():
    """`*-secret.yaml` is ignored repo-wide: a manifest that matches is never
    committed, so CI renders a directory without it while a local run passes."""
    paths = [str(SYNTHETICS / r) for r in _kustomization()["resources"]]
    proc = subprocess.run(["git", "-C", str(REPO_ROOT), "check-ignore", *paths], capture_output=True, text=True)
    assert proc.returncode == 1 and not proc.stdout.strip(), f"gitignored: {proc.stdout.strip()}"


def test_the_argo_application_renders_the_directory():
    app = _one(ARGO_APP, "Application", "synthetics")
    src = app["spec"]["source"]
    assert src["repoURL"] == "https://github.com/madfam-org/enclii.git"
    assert src["targetRevision"] == "main"
    assert src["path"] == "infra/k8s/production/synthetics"
    assert "directory" not in src, "a directory.include would bypass the kustomization"
    assert app["spec"]["destination"]["namespace"] == "synthetics"
    assert app["spec"]["syncPolicy"]["automated"] == {"prune": True, "selfHeal": True}
    # The root app adopts every file in infra/argocd/apps; nothing filters it.
    root = _one(ROOT_APP, "Application", "enclii-infrastructure")["spec"]["source"]
    assert root["path"] == "infra/argocd/apps" and "directory" not in root
    assert not (REPO_ROOT / "infra" / "argocd" / "apps" / "kustomization.yaml").exists()


@pytest.mark.skipif(not shutil.which("kustomize"), reason="kustomize not on PATH; the structural tests above still ran")
def test_kustomize_renders_the_namespace_and_pins_the_image():
    out = subprocess.run(["kustomize", "build", str(SYNTHETICS)], capture_output=True, text=True, check=True).stdout
    docs = [d for d in yaml.safe_load_all(out) if d]
    kinds = sorted(d["kind"] for d in docs)
    # Two CronJobs: the staging buyer and the weekly production pass
    # (cronjob-production-weekly.yaml).
    assert kinds == sorted(["Namespace", "ResourceQuota", "NetworkPolicy", "NetworkPolicy", "ServiceAccount", "ExternalSecret", "CronJob", "CronJob"])
    for d in docs:
        if d["kind"] != "Namespace":
            assert d["metadata"]["namespace"] == "synthetics", d["metadata"]["name"]
    for cj in [d for d in docs if d["kind"] == "CronJob"]:
        image = cj["spec"]["jobTemplate"]["spec"]["template"]["spec"]["containers"][0]["image"]
        assert re.fullmatch(r"ghcr\.io/madfam-org/[a-z0-9/_-]+@sha256:[0-9a-f]{64}", image), image


def test_the_image_entry_is_a_ghcr_digest_pin():
    (img,) = _kustomization()["images"]
    assert img["name"] == _container()["image"] == "journeys"
    assert img["newName"].startswith("ghcr.io/madfam-org/"), "Kyverno restrict-image-registries"
    assert re.fullmatch(r"sha256:[0-9a-f]{64}", img["digest"])
    assert "tag" not in img and "newTag" not in img


def test_journeys_image_digest_is_published():
    """THE MERGE GATE. Fails while the digest is the all-zero placeholder.

    Build tools/journeys/Dockerfile (ops repo), push it to the newName in the
    kustomization, and pin the digest the registry returns.
    """
    (img,) = _kustomization()["images"]
    assert img["digest"] != ZERO_DIGEST, (
        "journeys image digest is still the placeholder: build and push the "
        "image, then pin its digest in infra/k8s/production/synthetics/kustomization.yaml"
    )


# ---------------------------------------------------------------------------
# The CronJob
# ---------------------------------------------------------------------------


def test_cronjob_schedule_and_job_shape():
    spec = _cronjob()["spec"]
    assert spec["schedule"] == "*/15 * * * *"
    assert spec["concurrencyPolicy"] == "Forbid"
    job = spec["jobTemplate"]["spec"]
    assert job["backoffLimit"] == 0, "a retry hides a failure from the streak and buys a second subscription"
    assert job["activeDeadlineSeconds"] < 15 * 60, "a hung run must end before the next one is due"
    assert _pod_spec()["restartPolicy"] == "Never"


def test_cronjob_runs_the_staging_full_journey_and_pushes_metrics():
    env = _env()
    assert env["JOURNEYS_MODE"]["value"] == "staging-full"
    assert re.fullmatch(env["JOURNEYS_FILTER"]["value"], JOURNEY)
    assert not re.fullmatch(env["JOURNEYS_FILTER"]["value"], JOURNEY + ".x")
    assert env["JOURNEYS_PUSHGATEWAY_URL"]["value"] == "http://pushgateway.monitoring.svc.cluster.local:9091"
    assert env["JOURNEYS_OUT"]["value"] == "/app/out"


def test_cronjob_takes_the_identity_only_from_the_external_secret():
    es = _one(SYNTHETICS / "journeys-identity.externalsecret.yaml", "ExternalSecret")
    secret = es["spec"]["target"]["name"]
    env = _env()
    for name in IDENTITY_ENV:
        ref = env[name]["valueFrom"]["secretKeyRef"]
        assert ref == {"name": secret, "key": name}, name
        assert "value" not in env[name]
    assert "envFrom" not in _container()


def test_cronjob_pod_is_non_root_read_only_and_tokenless():
    pod = _pod_spec()
    assert pod["automountServiceAccountToken"] is False
    assert pod["serviceAccountName"] == "journeys"
    psc = pod["securityContext"]
    assert psc["runAsNonRoot"] is True and psc["runAsUser"] == 1001
    assert psc["seccompProfile"] == {"type": "RuntimeDefault"}
    csc = _container()["securityContext"]
    assert csc["readOnlyRootFilesystem"] is True
    assert csc["allowPrivilegeEscalation"] is False
    assert csc["capabilities"]["drop"] == ["ALL"]
    mounts = {m["mountPath"] for m in _container()["volumeMounts"]}
    assert mounts == {"/tmp", "/app/out", "/dev/shm"}
    assert {"name": "ghcr-credentials"} in pod["imagePullSecrets"]


def _qty_cpu(v: str) -> float:
    return float(v[:-1]) / 1000 if str(v).endswith("m") else float(v)


def _qty_mem(v: str) -> int:
    units = {"Mi": 2**20, "Gi": 2**30}
    return int(float(v[:-2]) * units[v[-2:]])


def test_one_run_fits_the_namespace_quota():
    quota = _one(SYNTHETICS / "resource-quota.yaml", "ResourceQuota")["spec"]["hard"]
    res = _container()["resources"]
    assert _qty_cpu(res["requests"]["cpu"]) <= _qty_cpu(quota["requests.cpu"])
    assert _qty_mem(res["requests"]["memory"]) <= _qty_mem(quota["requests.memory"])
    assert _qty_cpu(res["limits"]["cpu"]) <= _qty_cpu(quota["limits.cpu"])
    assert _qty_mem(res["limits"]["memory"]) <= _qty_mem(quota["limits.memory"])
    spec = _cronjob()["spec"]
    assert spec["successfulJobsHistoryLimit"] + spec["failedJobsHistoryLimit"] + 1 <= int(quota["count/jobs.batch"])


# ---------------------------------------------------------------------------
# The identity: ExternalSecret <-> intake target
# ---------------------------------------------------------------------------


def test_external_secret_maps_exactly_the_intake_target_keys():
    target = yaml.safe_load(REGISTRY.read_text(encoding="utf-8"))["targets"][INTAKE_TARGET]
    es = _one(SYNTHETICS / "journeys-identity.externalsecret.yaml", "ExternalSecret")
    assert es["spec"]["secretStoreRef"] == {"name": "vault-store", "kind": "ClusterSecretStore"}
    assert es["metadata"]["name"] == target["external_secret"]
    assert target["namespace"] == _kustomization()["namespace"]
    mapping = {d["secretKey"]: (d["remoteRef"]["key"], d["remoteRef"]["property"]) for d in es["spec"]["data"]}
    assert mapping == {env: (target["vault_path"], prop) for env, prop in IDENTITY_ENV.items()}
    assert sorted(target["keys"]) == sorted(IDENTITY_ENV.values())
    assert "generate" not in target


def test_the_runner_holds_no_stripe_key():
    """Only Dhanam holds Stripe keys (2026-04-25 rule). Reset and cleanup go
    through Dhanam staging's synthetic-cancel endpoint, called as the identity."""
    target = yaml.safe_load(REGISTRY.read_text(encoding="utf-8"))["targets"][INTAKE_TARGET]
    es = _one(SYNTHETICS / "journeys-identity.externalsecret.yaml", "ExternalSecret")
    names = (
        list(target["keys"])
        + [d["secretKey"] for d in es["spec"]["data"]]
        + [d["remoteRef"]["property"] for d in es["spec"]["data"]]
        + list(_env())
    )
    assert not [n for n in names if "stripe" in n.lower()], names
    for path in SYNTHETICS.glob("*.yaml"):
        assert not re.search(r"\b(sk|rk)_(test|live)_[0-9A-Za-z]", path.read_text(encoding="utf-8")), path.name


# ---------------------------------------------------------------------------
# Network: egress here, ingress on the monitoring side, and the scrape
# ---------------------------------------------------------------------------


def _ports(rule: dict) -> set[tuple[str, int]]:
    return {(p.get("protocol", "TCP"), p["port"]) for p in rule.get("ports", [])}


def test_synthetics_namespace_is_default_deny_with_narrow_egress():
    pols = {d["metadata"]["name"]: d for d in _docs(SYNTHETICS / "network-policies.yaml") if d.get("kind") == "NetworkPolicy"}
    deny = pols["default-deny-all"]["spec"]
    assert deny["podSelector"] == {} and set(deny["policyTypes"]) == {"Ingress", "Egress"}
    eg = pols["journeys-egress"]["spec"]
    assert eg["podSelector"] == {"matchLabels": {"app.kubernetes.io/name": "journeys"}}
    assert eg["podSelector"]["matchLabels"].items() <= _cronjob()["spec"]["jobTemplate"]["spec"]["template"]["metadata"]["labels"].items()
    all_ports = set().union(*(_ports(r) for r in eg["egress"]))
    assert all_ports == {("UDP", 53), ("TCP", 53), ("TCP", 443), ("TCP", 9091)}
    (https,) = [r for r in eg["egress"] if ("TCP", 443) in _ports(r)]
    (block,) = https["to"]
    assert block["ipBlock"]["cidr"] == "0.0.0.0/0"
    assert {"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} <= set(block["ipBlock"]["except"])
    (push,) = [r for r in eg["egress"] if ("TCP", 9091) in _ports(r)]
    assert {
        "namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "monitoring"}},
        "podSelector": {"matchLabels": {"app": "pushgateway"}},
    } in push["to"]


def test_pushgateway_admits_the_journeys_on_its_container_port():
    pol = _one(NETWORK_POLICIES, "NetworkPolicy", "pushgateway-ingress-synthetics")["spec"]
    assert pol["podSelector"] == {"matchLabels": {"app": "pushgateway"}}
    (rule,) = pol["ingress"]
    assert rule["from"] == [
        {
            "namespaceSelector": {"matchLabels": {"kubernetes.io/metadata.name": "synthetics"}},
            "podSelector": {"matchLabels": {"app.kubernetes.io/name": "journeys"}},
        }
    ]
    assert _ports(rule) == {("TCP", 9091)}
    gw = _one(PUSHGATEWAY_FILE, "Deployment", "pushgateway")
    assert gw["spec"]["template"]["metadata"]["labels"]["app"] == "pushgateway"
    assert [p["containerPort"] for p in gw["spec"]["template"]["spec"]["containers"][0]["ports"]] == [9091]
    svc = _one(PUSHGATEWAY_FILE, "Service", "pushgateway")
    assert svc["metadata"]["namespace"] == "monitoring" and [p["port"] for p in svc["spec"]["ports"]] == [9091]


def test_prometheus_scrapes_the_pushgateway_with_honor_labels():
    """Pushed series keep job="journeys"; without honor_labels they would not."""
    cfg = yaml.safe_load(_one(PROMETHEUS, "ConfigMap", "prometheus-config")["data"]["prometheus.yml"])
    (job,) = [j for j in cfg["scrape_configs"] if j.get("job_name") == "pushgateway"]
    assert job["honor_labels"] is True
    keeps = [r for r in job["relabel_configs"] if r.get("action") == "keep"]
    assert any(r.get("regex") == "pushgateway" for r in keeps)
    # The monitoring Argo app must not exempt the scrape config from sync again.
    app = _one(REPO_ROOT / "infra" / "argocd" / "apps" / "monitoring.yaml", "Application", "monitoring")
    assert "ignoreDifferences" not in app["spec"]


# ---------------------------------------------------------------------------
# Rules and routing
# ---------------------------------------------------------------------------


def test_rules_page_critical_under_the_revenue_route():
    rules = _rules()
    assert set(rules) == {"SyntheticCheckoutFailing", "SyntheticJourneysNotRunning"}
    for name, rule in rules.items():
        assert rule["labels"]["severity"] == "critical", name
        assert rule["labels"]["category"] == CATEGORY, name
        assert "product" not in rule["labels"], f"{name}: product must come from the pushed series"
        assert 'job="journeys"' in rule["expr"], name
    cm = _one(ALERTMANAGER, "ConfigMap", "alertmanager-config")
    routes = yaml.safe_load(cm["data"]["alertmanager.yml"])["route"]["routes"]
    (route,) = [r for r in routes if f'category="{CATEGORY}"' in [str(m) for m in r.get("matchers") or []]]
    assert 'severity="critical"' in route["matchers"]
    assert "product" in route["group_by"]


def test_failing_needs_two_consecutive_failures():
    expr = _rules()["SyntheticCheckoutFailing"]["expr"]
    assert re.search(r"synthetic_journey_consecutive_failures\{[^}]*\}\s*>=\s*2\b", expr)


def test_not_running_is_45_minutes_and_names_the_runner_product():
    expr = _rules()["SyntheticJourneysNotRunning"]["expr"]
    assert expr.count("> 2700") == 3
    assert f'journey="{JOURNEY}"' in expr and f'product="{PRODUCT}"' in expr
    assert f'cronjob="{CRONJOB}"' in expr and 'namespace="synthetics"' in expr


def test_the_legacy_probe_rule_ignores_the_journeys_series():
    groups = yaml.safe_load(_rules_cm()["data"][LEGACY_RULES_KEY])["groups"]
    rules = {r["alert"]: r for g in groups for r in g["rules"]}
    assert 'job!="journeys"' in rules["SyntheticJourneyConsecutivelyFailing"]["expr"]


def test_annotations_use_only_labels_and_value():
    """No `query` template functions: a template that errors at evaluation has
    crashed this Prometheus before (#572)."""
    for name, rule in _rules().items():
        for key, text in rule["annotations"].items():
            for expr in re.findall(r"{{(.*?)}}", text, flags=re.S):
                assert re.fullmatch(r"\s*\$(labels\.[a-z_]+|value)\s*", expr), (name, key, expr)


# ---------------------------------------------------------------------------
# promtool: evaluate the real rules
# ---------------------------------------------------------------------------

S = f'job="journeys",journey="{JOURNEY}",product="{PRODUCT}",env="staging"'
KSM = f'kube_cronjob_created{{namespace="synthetics",cronjob="{CRONJOB}"}}'
FAILING_SUMMARY = f"{PRODUCT} synthetic checkout failed 2 runs in a row: {JOURNEY}"
NOT_RUNNING_SUMMARY = f"Synthetic journey {JOURNEY} has not passed or reported for 45 minutes"
ALERT_LABELS = f"""
                  severity: critical
                  category: revenue-probe
                  job: journeys
                  journey: {JOURNEY}
                  product: {PRODUCT}
                  env: staging"""

PROMTOOL_TEST = textwrap.dedent(
    f"""
    rule_files:
      - {RULES_KEY}
      - {LEGACY_RULES_KEY}
    evaluation_interval: 30s
    tests:
      # 1. Two failures in a row page as CheckoutFailing; NotRunning stays
      #    quiet while the failing runner keeps reporting (one page).
      - interval: 1m
        input_series:
          - series: 'synthetic_journey_consecutive_failures{{{S}}}'
            values: '0x10 1x15 2x15 3x25'
          - series: 'synthetic_journey_last_run_timestamp_seconds{{{S}}}'
            values: '0+60x65'
          - series: 'synthetic_journey_last_success_timestamp_seconds{{{S}}}'
            values: '60x65'
          - series: '{KSM}'
            values: '0x65'
        alert_rule_test:
          - eval_time: 20m
            alertname: SyntheticCheckoutFailing
            exp_alerts: []
          - eval_time: 30m
            alertname: SyntheticCheckoutFailing
            exp_alerts:
              - exp_labels:{ALERT_LABELS}
                exp_annotations:
                  summary: "{FAILING_SUMMARY}"
          - eval_time: 60m
            alertname: SyntheticJourneysNotRunning
            exp_alerts: []
          # The legacy probe rule must not page on the runner's series.
          - eval_time: 60m
            alertname: SyntheticJourneyConsecutivelyFailing
            exp_alerts: []

      # 2. One failure, then a pass: nothing pages.
      - interval: 1m
        input_series:
          - series: 'synthetic_journey_consecutive_failures{{{S}}}'
            values: '0x10 1x14 0x30'
          - series: 'synthetic_journey_last_run_timestamp_seconds{{{S}}}'
            values: '0+60x55'
          - series: 'synthetic_journey_last_success_timestamp_seconds{{{S}}}'
            values: '60x24 1500x30'
          - series: '{KSM}'
            values: '0x55'
        alert_rule_test:
          - eval_time: 22m
            alertname: SyntheticCheckoutFailing
            exp_alerts: []
          - eval_time: 50m
            alertname: SyntheticJourneysNotRunning
            exp_alerts: []

      # 3. The runner stops reporting after a pass at 10m: NotRunning once
      #    45 minutes pass with no run (plus `for`), not before.
      - interval: 1m
        input_series:
          - series: 'synthetic_journey_consecutive_failures{{{S}}}'
            values: '0x80'
          - series: 'synthetic_journey_last_run_timestamp_seconds{{{S}}}'
            values: '600x80'
          - series: 'synthetic_journey_last_success_timestamp_seconds{{{S}}}'
            values: '600x80'
          - series: '{KSM}'
            values: '0x80'
        alert_rule_test:
          - eval_time: 50m
            alertname: SyntheticJourneysNotRunning
            exp_alerts: []
          - eval_time: 65m
            alertname: SyntheticJourneysNotRunning
            exp_alerts:
              - exp_labels:{ALERT_LABELS}
                exp_annotations:
                  summary: "{NOT_RUNNING_SUMMARY}"

      # 4. Deployed but never reported (identity Secret missing, image not
      #    pullable): quiet for the first 45 minutes of the CronJob, then pages.
      - interval: 1m
        input_series:
          - series: '{KSM}'
            values: '0x80'
        alert_rule_test:
          - eval_time: 40m
            alertname: SyntheticJourneysNotRunning
            exp_alerts: []
          - eval_time: 55m
            alertname: SyntheticJourneysNotRunning
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: revenue-probe
                  job: journeys
                  journey: {JOURNEY}
                  product: {PRODUCT}
                  env: staging
                exp_annotations:
                  summary: "{NOT_RUNNING_SUMMARY}"

      # 5. No CronJob and no series: Argo's to report, not this rule's.
      - interval: 1m
        input_series:
          - series: 'up{{job="prometheus"}}'
            values: '1x80'
        alert_rule_test:
          - eval_time: 70m
            alertname: SyntheticJourneysNotRunning
            exp_alerts: []

      # 6. The legacy rule still pages for its own pusher.
      - interval: 1m
        input_series:
          - series: 'synthetic_journey_consecutive_failures{{job="synthetic-flow-probe",platform="selva",journey="admin-login"}}'
            values: '3x10'
        alert_rule_test:
          - eval_time: 5m
            alertname: SyntheticJourneyConsecutivelyFailing
            exp_alerts:
              - exp_labels:
                  severity: critical
                  category: synthetic-flow
                  job: synthetic-flow-probe
                  platform: selva
                  journey: admin-login
                exp_annotations:
                  summary: "Synthetic journey selva/admin-login is broken"
    """
).lstrip()

PROMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_PROMTOOL_TIMEOUT", "180"))


def _without_descriptions(key: str) -> str:
    """The loaded rules with only the multi-line `description` dropped (same
    trade as test_revenue_probe_rules.py): expr, for, labels and `summary` are
    compared exactly; the full text, descriptions included, goes through
    `promtool check rules` below."""
    doc = yaml.safe_load(_rules_cm()["data"][key])
    for group in doc["groups"]:
        for rule in group["rules"]:
            rule.get("annotations", {}).pop("description", None)
    return yaml.safe_dump(doc, sort_keys=False)


def _promtool(files: dict[str, str], name: str, verb: str) -> tuple[int, str]:
    template = os.environ.get("ENCLII_PROMTOOL")
    if not template and not shutil.which("promtool"):
        pytest.skip("promtool not on PATH and ENCLII_PROMTOOL not set; the structural tests above still ran")
    with tempfile.TemporaryDirectory() as tmp:
        for fname, body in files.items():
            (Path(tmp) / fname).write_text(body, encoding="utf-8")
        # The prometheus image runs as nobody; a 0700 tempdir is unreadable to
        # it through a bind mount.
        os.chmod(tmp, 0o755)
        for p in Path(tmp).iterdir():
            os.chmod(p, 0o644)
        if template:
            cmd: str | list[str] = template.replace("test rules", verb).format(dir=tmp, name=name)
            shell = True
        else:
            cmd = ["promtool", *verb.split(), str(Path(tmp) / name)]
            shell = False
        try:
            proc = subprocess.run(cmd, shell=shell, capture_output=True, text=True, timeout=PROMTOOL_TIMEOUT_SECONDS)
        except subprocess.TimeoutExpired:
            pytest.skip(f"promtool did not finish within {PROMTOOL_TIMEOUT_SECONDS}s")
    output = (proc.stdout + proc.stderr).strip()
    if proc.returncode != 0 and not any(w in output for w in ("FAILED", "SUCCESS", "error", "Checking")):
        pytest.skip(f"promtool could not run: {output[-500:]}")
    return proc.returncode, output


def test_promtool_checks_the_full_rule_text():
    body = _rules_cm()["data"][RULES_KEY]
    code, output = _promtool({RULES_KEY: body}, RULES_KEY, "check rules")
    assert code == 0, f"promtool rejected {RULES_KEY}:\n{output}"
    # 2 staging rules + 2 production-pass rules (synthetic-journeys-production).
    assert "SUCCESS: 4 rules found" in output, output


def test_promtool_evaluates_the_synthetic_journey_rules():
    files = {
        RULES_KEY: _without_descriptions(RULES_KEY),
        LEGACY_RULES_KEY: _without_descriptions(LEGACY_RULES_KEY),
        "synthetic-journeys-rules.test.yml": PROMTOOL_TEST,
    }
    code, output = _promtool(files, "synthetic-journeys-rules.test.yml", "test rules")
    assert code == 0, f"promtool rejected the rule test:\n{output}"
    assert "SUCCESS" in output, output
