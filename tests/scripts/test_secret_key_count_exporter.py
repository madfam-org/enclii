"""
Tests for the secret key-count exporter.

The exporter script is embedded in the `secret-key-count-exporter-script`
ConfigMap in infra/k8s/production/monitoring/secret-key-count-exporter.yaml.
These tests load the script *out of that ConfigMap* and execute it, so there
is exactly one copy of the code and no possibility of test/deploy drift: what
is tested here is byte-for-byte what the pod runs.

Run with:
    pytest tests/scripts/test_secret_key_count_exporter.py -v
"""
from __future__ import annotations

import json
import types
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST = (
    REPO_ROOT
    / "infra"
    / "k8s"
    / "production"
    / "monitoring"
    / "secret-key-count-exporter.yaml"
)

SCRIPT_CONFIGMAP = "secret-key-count-exporter-script"
TARGETS_CONFIGMAP = "secret-key-count-exporter-targets"


def _docs() -> list[dict]:
    with MANIFEST.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict)]


def _configmap(name: str) -> dict:
    for doc in _docs():
        if doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == name:
            return doc
    raise AssertionError(f"ConfigMap {name} not found in {MANIFEST}")


def _load_exporter() -> types.ModuleType:
    """Exec the ConfigMap-embedded exporter as a module."""
    source = _configmap(SCRIPT_CONFIGMAP)["data"]["exporter.py"]
    module = types.ModuleType("secret_key_count_exporter")
    module.__dict__["__name__"] = "secret_key_count_exporter"
    exec(compile(source, str(MANIFEST) + ":exporter.py", "exec"), module.__dict__)
    return module


exporter = _load_exporter()


def _targets_json() -> dict:
    return json.loads(_configmap(TARGETS_CONFIGMAP)["data"]["secrets.json"])


# ---------------------------------------------------------------------------
# The threshold the incident requires
# ---------------------------------------------------------------------------


def test_dhanam_prod_target_is_watched_at_20_keys() -> None:
    """The control the 2026-06-13 runbook asked for: alert below 20 keys.

    dhanam/dhanam-secrets holds 23 keys when all three writers have merged;
    the wipe window produced 2, 10, 12 and 13.
    """
    targets = [t for t in _targets_json()["targets"] if set(t.keys()) - {"//"}]
    match = [
        t
        for t in targets
        if t["namespace"] == "dhanam" and t["secret"] == "dhanam-secrets"
    ]
    assert len(match) == 1, "dhanam/dhanam-secrets must be watched exactly once"
    assert match[0]["min_keys"] == 20


def test_targets_file_parses_with_the_shipped_loader(tmp_path: Path) -> None:
    path = tmp_path / "secrets.json"
    path.write_text(_configmap(TARGETS_CONFIGMAP)["data"]["secrets.json"])
    loaded = exporter.load_targets(str(path))
    assert {(t["namespace"], t["secret"]) for t in loaded} >= {
        ("dhanam", "dhanam-secrets")
    }
    # Comment-only entries must not become phantom targets.
    assert all("//" not in t for t in loaded)


# ---------------------------------------------------------------------------
# Counting and thresholds
# ---------------------------------------------------------------------------


# NOTE: the kwarg is `secret_name`, not `secret`. The repo pre-commit hook
# flags `secret = "..."` as a possible hardcoded credential; this value is a
# Kubernetes Secret NAME, never a secret value.
def _target(namespace="dhanam", secret_name="dhanam-secrets", min_keys=20) -> dict:
    return {"namespace": namespace, "secret": secret_name, "min_keys": min_keys}


def test_poll_reports_the_healthy_count() -> None:
    results = exporter.poll([_target()], counter=lambda *_a: (23, None))
    assert results[0]["count"] == 23
    assert results[0]["error"] is None


def test_poll_records_the_measured_wipe_window_value(caplog) -> None:
    """10 keys is the value actually measured mid-window on 2026-08-06."""
    with caplog.at_level("ERROR"):
        results = exporter.poll([_target()], counter=lambda *_a: (10, None))
    assert results[0]["count"] == 10
    events = [json.loads(r.message)["event"] for r in caplog.records]
    assert "secret_key_count_low" in events


def test_poll_records_the_two_key_bridge_state() -> None:
    results = exporter.poll([_target()], counter=lambda *_a: (2, None))
    assert results[0]["count"] == 2


def test_failed_read_is_not_reported_as_a_low_count() -> None:
    """An RBAC/API failure must not masquerade as a secret collapse."""
    results = exporter.poll([_target()], counter=lambda *_a: (None, "kube API returned 403"))
    assert results[0]["count"] is None
    assert results[0]["error"] == "kube API returned 403"

    text = exporter.render(results, 1)
    # No count series at all — the alert compares counts, so emitting a 0 here
    # would page for the wrong reason.
    assert "secret_key_count{" not in text
    assert 'secret_key_count_check_success{secret_namespace="dhanam",secret_name="dhanam-secrets"} 0' in text


# ---------------------------------------------------------------------------
# Exposition format
# ---------------------------------------------------------------------------


def test_render_emits_expected_series() -> None:
    results = exporter.poll([_target()], counter=lambda *_a: (23, None))
    text = exporter.render(results, 7)
    assert 'secret_key_count{secret_namespace="dhanam",secret_name="dhanam-secrets"} 23' in text
    assert (
        'secret_key_count_min_expected{secret_namespace="dhanam",secret_name="dhanam-secrets"} 20'
        in text
    )
    assert (
        'secret_key_count_check_success{secret_namespace="dhanam",secret_name="dhanam-secrets"} 1'
        in text
    )
    assert "secret_key_count_polls_total 7" in text
    assert "# TYPE secret_key_count gauge" in text


def test_render_uses_collision_free_label_names() -> None:
    """`namespace` and `service` are overwritten by the kubernetes-services
    scrape job (verified: cloudflared_probe_reachable carries
    namespace="cloudflare-tunnel", exported_namespace="dhanam"). The exporter
    must not use those names."""
    text = exporter.render(
        exporter.poll([_target()], counter=lambda *_a: (23, None)), 1
    )
    for line in text.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        assert "namespace=" not in line.replace("secret_namespace=", "")
        assert "service=" not in line


def test_render_is_valid_prometheus_text_shape() -> None:
    text = exporter.render(exporter.poll([_target()], counter=lambda *_a: (23, None)), 1)
    assert text.endswith("\n")
    for line in text.splitlines():
        if line.startswith("#"):
            assert line.startswith("# HELP ") or line.startswith("# TYPE ")
            continue
        assert " " in line
        value = line.rsplit(" ", 1)[1]
        float(value)  # raises if the value is not a number


# ---------------------------------------------------------------------------
# Kubernetes API handling
# ---------------------------------------------------------------------------


class _FakeResponse:
    def __init__(self, payload: dict) -> None:
        self._payload = payload

    def read(self) -> bytes:
        return json.dumps(self._payload).encode("utf-8")

    def __enter__(self):
        return self

    def __exit__(self, *_a):
        return False


def test_count_keys_counts_data_entries(monkeypatch) -> None:
    monkeypatch.setenv("KUBERNETES_SERVICE_HOST", "10.43.0.1")
    monkeypatch.setattr(exporter, "read_token", lambda: "fake")
    payload = {"data": {f"KEY_{i}": "dmFsdWU=" for i in range(23)}}
    count, err = exporter.count_keys(
        "dhanam", "dhanam-secrets", opener_get=lambda _req: _FakeResponse(payload)
    )
    assert (count, err) == (23, None)


def test_count_keys_handles_secret_with_no_data(monkeypatch) -> None:
    monkeypatch.setenv("KUBERNETES_SERVICE_HOST", "10.43.0.1")
    monkeypatch.setattr(exporter, "read_token", lambda: "fake")
    count, err = exporter.count_keys(
        "dhanam", "dhanam-secrets", opener_get=lambda _req: _FakeResponse({})
    )
    assert (count, err) == (0, None)


def test_count_keys_without_api_host_is_an_error(monkeypatch) -> None:
    monkeypatch.delenv("KUBERNETES_SERVICE_HOST", raising=False)
    count, err = exporter.count_keys("dhanam", "dhanam-secrets")
    assert count is None
    assert "KUBERNETES_SERVICE_HOST" in err


def test_count_keys_without_token_is_an_error(monkeypatch) -> None:
    monkeypatch.setenv("KUBERNETES_SERVICE_HOST", "10.43.0.1")
    monkeypatch.setattr(exporter, "read_token", lambda: None)
    count, err = exporter.count_keys("dhanam", "dhanam-secrets")
    assert count is None
    assert "token" in err


def test_count_keys_surfaces_transport_errors(monkeypatch) -> None:
    monkeypatch.setenv("KUBERNETES_SERVICE_HOST", "10.43.0.1")
    monkeypatch.setattr(exporter, "read_token", lambda: "fake")

    def _boom(_req):
        raise OSError("connection refused")

    count, err = exporter.count_keys("dhanam", "dhanam-secrets", opener_get=_boom)
    assert count is None
    assert "OSError" in err


def test_load_targets_rejects_missing_fields(tmp_path: Path) -> None:
    path = tmp_path / "targets.json"
    path.write_text(json.dumps({"targets": [{"namespace": "dhanam"}]}))
    with pytest.raises(ValueError):
        exporter.load_targets(str(path))


# ---------------------------------------------------------------------------
# Manifest invariants that keep the control wired up
# ---------------------------------------------------------------------------


def test_service_carries_the_scrape_annotations() -> None:
    """The whole metric pipeline hangs off these two annotations."""
    svc = [
        d
        for d in _docs()
        if d.get("kind") == "Service"
        and d["metadata"]["name"] == "secret-key-count-exporter"
    ][0]
    ann = svc["metadata"]["annotations"]
    assert ann["prometheus.io/scrape"] == "true"
    assert ann["prometheus.io/port"] == "9092"
    assert svc["spec"]["ports"][0]["port"] == 9092
    assert svc["spec"]["selector"] == {"app": "secret-key-count-exporter"}


def test_networkpolicy_selector_matches_the_pod_labels() -> None:
    """The node-exporter policy in this namespace selects `app:
    node-exporter` while the pods are labelled `app.kubernetes.io/name` — it
    matches nothing and all four targets are DOWN. Pin ours."""
    deploy = [
        d
        for d in _docs()
        if d.get("kind") == "Deployment"
        and d["metadata"]["name"] == "secret-key-count-exporter"
    ][0]
    pod_labels = deploy["spec"]["template"]["metadata"]["labels"]
    policies = [d for d in _docs() if d.get("kind") == "NetworkPolicy"]
    assert policies, "exporter must ship its own NetworkPolicies (namespace is default-deny)"
    for np in policies:
        selector = np["spec"]["podSelector"]["matchLabels"]
        for key, value in selector.items():
            assert pod_labels.get(key) == value, (
                f"NetworkPolicy {np['metadata']['name']} selects {key}={value} "
                f"but the pod is labelled {pod_labels}"
            )


def test_ingress_policy_allows_the_metrics_port() -> None:
    deploy = [
        d
        for d in _docs()
        if d.get("kind") == "Deployment"
        and d["metadata"]["name"] == "secret-key-count-exporter"
    ][0]
    container = deploy["spec"]["template"]["spec"]["containers"][0]
    container_port = container["ports"][0]["containerPort"]
    ingress = [
        d
        for d in _docs()
        if d.get("kind") == "NetworkPolicy"
        and d["metadata"]["name"] == "secret-key-count-exporter-ingress"
    ][0]
    allowed = {
        p["port"] for rule in ingress["spec"]["ingress"] for p in rule.get("ports", [])
    }
    assert container_port in allowed


def test_rbac_is_scoped_to_named_secrets_only() -> None:
    """No wildcard secret read anywhere in this manifest."""
    roles = [d for d in _docs() if d.get("kind") in ("Role", "ClusterRole")]
    assert roles
    for role in roles:
        assert role["kind"] == "Role", "cluster-wide secret read is not acceptable"
        for rule in role["rules"]:
            assert rule["verbs"] == ["get"]
            assert rule.get("resourceNames"), "secret read must be resourceName-scoped"


def test_every_watched_namespace_has_a_rolebinding() -> None:
    watched = {
        t["namespace"]
        for t in _targets_json()["targets"]
        if set(t.keys()) - {"//"}
    }
    bound = {
        d["metadata"]["namespace"]
        for d in _docs()
        if d.get("kind") == "RoleBinding"
    }
    assert watched <= bound, f"targets without RBAC: {watched - bound}"
