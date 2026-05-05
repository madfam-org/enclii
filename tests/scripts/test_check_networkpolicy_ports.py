"""
Tests for scripts/check-networkpolicy-ports.py.

Run with:
    pytest tests/scripts/test_check_networkpolicy_ports.py -v
"""
from __future__ import annotations

import importlib.util
import subprocess
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-networkpolicy-ports.py"


# Load the module under test by path (it has a hyphen in its name, so we
# can't `import` it directly).
spec = importlib.util.spec_from_file_location("check_np_ports", SCRIPT)
assert spec is not None and spec.loader is not None
check_np_ports = importlib.util.module_from_spec(spec)
# Register before exec so @dataclass can resolve the module via sys.modules.
sys.modules["check_np_ports"] = check_np_ports
spec.loader.exec_module(check_np_ports)


def write_manifest(dir_: Path, name: str, body: str) -> Path:
    p = dir_ / name
    p.write_text(body)
    return p


def run_script(*roots: Path) -> tuple[int, str, str]:
    """Run the script as a subprocess and return (exit_code, stdout, stderr)."""
    proc = subprocess.run(
        [sys.executable, str(SCRIPT), *[str(r) for r in roots]],
        capture_output=True,
        text=True,
    )
    return proc.returncode, proc.stdout, proc.stderr


# ---------------------------------------------------------------------------
# Manifest fixtures (inline strings — simpler than YAML files on disk)
# ---------------------------------------------------------------------------

DEPLOY_8000 = """\
apiVersion: apps/v1
kind: Deployment
metadata:
  name: karafiel-api
  namespace: karafiel
spec:
  template:
    metadata:
      labels:
        app: karafiel-api
    spec:
      containers:
        - name: api
          image: ghcr.io/madfam-org/karafiel/api:latest
          ports:
            - name: http
              containerPort: 8000
"""

DEPLOY_3050 = """\
apiVersion: apps/v1
kind: Deployment
metadata:
  name: karafiel-web
  namespace: karafiel
spec:
  template:
    metadata:
      labels:
        app: karafiel-web
    spec:
      containers:
        - name: web
          image: ghcr.io/madfam-org/karafiel/web:latest
          ports:
            - containerPort: 3050
"""

NP_NO_PORTS = """\
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-cloudflared-ingress
  namespace: karafiel
spec:
  podSelector:
    matchLabels:
      app: karafiel-api
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: cloudflared
"""

NP_WRONG_PORT_80 = """\
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-cloudflared-ingress
  namespace: karafiel
spec:
  podSelector:
    matchLabels:
      app: karafiel-api
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: cloudflared
      ports:
        - protocol: TCP
          port: 80
"""

NP_SUBSET_OK = """\
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-cloudflared-ingress
  namespace: karafiel
spec:
  podSelector:
    matchLabels:
      app: karafiel-api
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: cloudflared
      ports:
        - protocol: TCP
          port: 8000
        - protocol: TCP
          port: 3050
"""

NP_NO_MATCHING_DEPLOY = """\
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-external-thing
  namespace: karafiel
spec:
  podSelector:
    matchLabels:
      app: does-not-exist
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - protocol: TCP
          port: 9999
"""

NP_EGRESS_BAD = """\
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: karafiel-egress
  namespace: karafiel
spec:
  podSelector:
    matchLabels:
      app: karafiel-api
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: data
      ports:
        - protocol: TCP
          port: 6543  # postgres should be 5432; intentional drift
"""

NP_MATCH_EXPRESSIONS_OK = """\
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: status-egress
  namespace: karafiel
spec:
  podSelector:
    matchExpressions:
      - key: app
        operator: In
        values:
          - karafiel-api
          - karafiel-web
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - protocol: TCP
          port: 8000
"""


# ---------------------------------------------------------------------------
# Test cases
# ---------------------------------------------------------------------------


def test_passes_when_no_ports_field_present(tmp_path: Path) -> None:
    """Case 1: NetworkPolicy without ports + Deployment with containerPort 8000 → PASS."""
    write_manifest(tmp_path, "deploy.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "np.yaml", NP_NO_PORTS)

    code, stdout, _ = run_script(tmp_path)

    assert code == 0, stdout
    assert "0 failure" in stdout
    assert "OK" in stdout


def test_fails_on_port_mismatch_with_precise_message(tmp_path: Path) -> None:
    """Case 2: NP ports=[80] + Deployment containerPort=8000 → FAIL with precise message."""
    write_manifest(tmp_path, "deploy.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "np.yaml", NP_WRONG_PORT_80)

    code, stdout, stderr = run_script(tmp_path)

    assert code == 1
    assert "[FAIL]" in stdout
    assert "allow-cloudflared-ingress" in stdout
    assert "80" in stdout
    assert "8000" in stdout
    # Error message names the failure mode explicitly.
    assert "silently dropped at the CNI" in stdout
    assert "remove the `ports` restriction" in stdout
    assert "cloudflared is the trust boundary" in stdout
    assert "FAIL" in stderr


def test_passes_when_np_ports_subset_matches_pod_ports(tmp_path: Path) -> None:
    """Case 3: NP ports=[8000,3050] + pod containerPort=8000 → PASS (subset OK)."""
    write_manifest(tmp_path, "deploy.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "deploy_web.yaml", DEPLOY_3050)
    write_manifest(tmp_path, "np.yaml", NP_SUBSET_OK)

    code, stdout, _ = run_script(tmp_path)

    assert code == 0, stdout
    assert "0 failure" in stdout


def test_warns_but_does_not_fail_when_no_matching_deployment(tmp_path: Path) -> None:
    """Case 4: NP with no matching Deployment → WARN (don't fail; can't verify)."""
    write_manifest(tmp_path, "deploy.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "np.yaml", NP_NO_MATCHING_DEPLOY)

    code, stdout, _ = run_script(tmp_path)

    assert code == 0, stdout
    assert "[WARN]" in stdout
    assert "matches no in-scope" in stdout
    assert "1 warning" in stdout


def test_multiple_policies_and_deployments_mixed_results(tmp_path: Path) -> None:
    """Case 5: multiple NPs + multiple Deployments mixed → all individual results."""
    write_manifest(tmp_path, "deploy_api.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "deploy_web.yaml", DEPLOY_3050)
    # 3 policies: one passing (no ports), one failing (wrong port), one passing (subset).
    write_manifest(tmp_path, "np_ok.yaml", NP_NO_PORTS)
    write_manifest(tmp_path, "np_bad.yaml", NP_WRONG_PORT_80)
    write_manifest(tmp_path, "np_subset.yaml", NP_SUBSET_OK)

    code, stdout, _ = run_script(tmp_path)

    assert code == 1, stdout
    # Exactly one failure should be reported (the wrong-port one).
    assert stdout.count("[FAIL]") == 1
    assert "np_bad.yaml" in stdout
    assert "1 failure" in stdout


def test_egress_ports_are_not_checked(tmp_path: Path) -> None:
    """Egress rules describe destination ports (DNS=53, HTTPS=443, etc.),
    which intentionally do NOT intersect the source pod's containerPorts.
    Cross-checking would produce constant false positives across DNS/HTTPS
    egress rules. Verify the script ignores egress."""
    write_manifest(tmp_path, "deploy.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "np.yaml", NP_EGRESS_BAD)

    code, stdout, _ = run_script(tmp_path)

    assert code == 0, stdout
    assert "[FAIL]" not in stdout


def test_match_expressions_are_supported(tmp_path: Path) -> None:
    """matchExpressions (operator: In) should resolve like matchLabels."""
    write_manifest(tmp_path, "deploy_api.yaml", DEPLOY_8000)
    write_manifest(tmp_path, "deploy_web.yaml", DEPLOY_3050)
    write_manifest(tmp_path, "np.yaml", NP_MATCH_EXPRESSIONS_OK)

    code, stdout, _ = run_script(tmp_path)

    assert code == 0, stdout


def test_multiple_roots_are_searched(tmp_path: Path) -> None:
    """The script accepts multiple roots and merges results."""
    root_a = tmp_path / "a"
    root_b = tmp_path / "b"
    root_a.mkdir()
    root_b.mkdir()
    write_manifest(root_a, "deploy.yaml", DEPLOY_8000)
    write_manifest(root_b, "np.yaml", NP_WRONG_PORT_80)

    code, stdout, _ = run_script(root_a, root_b)

    assert code == 1, stdout
    assert "[FAIL]" in stdout


def test_returns_2_on_missing_root() -> None:
    """Missing root paths surface as exit code 2 (parse/usage error)."""
    code, _, stderr = run_script(Path("/this/path/definitely/does/not/exist"))
    assert code == 2
    assert "does not exist" in stderr


# ---------------------------------------------------------------------------
# Direct unit tests of the selector matcher (covers branches the e2e tests
# don't exercise efficiently).
# ---------------------------------------------------------------------------


def test_selector_matches_match_labels() -> None:
    assert check_np_ports.selector_matches({"matchLabels": {"app": "x"}}, {"app": "x"})
    assert not check_np_ports.selector_matches(
        {"matchLabels": {"app": "x"}}, {"app": "y"}
    )


def test_selector_matches_match_expressions_in_notin_exists() -> None:
    sel_in = {"matchExpressions": [{"key": "app", "operator": "In", "values": ["a", "b"]}]}
    assert check_np_ports.selector_matches(sel_in, {"app": "a"})
    assert not check_np_ports.selector_matches(sel_in, {"app": "c"})

    sel_notin = {
        "matchExpressions": [{"key": "app", "operator": "NotIn", "values": ["a"]}]
    }
    assert check_np_ports.selector_matches(sel_notin, {"app": "b"})
    assert not check_np_ports.selector_matches(sel_notin, {"app": "a"})

    sel_exists = {"matchExpressions": [{"key": "app", "operator": "Exists"}]}
    assert check_np_ports.selector_matches(sel_exists, {"app": "anything"})
    assert not check_np_ports.selector_matches(sel_exists, {"other": "x"})


def test_empty_selector_matches_everything() -> None:
    assert check_np_ports.selector_matches({}, {"app": "x"})
    assert check_np_ports.selector_matches({}, {})


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-v"]))
