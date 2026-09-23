"""
Tests for scripts/check-argocd-ignore-differences.py.

Run with:
    pytest tests/scripts/test_check_argocd_ignore_differences.py -v

The load-bearing test is ``test_catches_the_real_appset_regression``: it feeds
the lint the exact ApplicationSet rule that silently dropped every added
ExternalSecret ``spec.data`` entry across ~35 generated apps, because a guard
that has never been shown to fail on the bug it exists for is not a guard.
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SCRIPT = REPO / "scripts" / "check-argocd-ignore-differences.py"


def run(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run([sys.executable, str(SCRIPT), *args], capture_output=True, text=True)


def write(tmp_path: Path, body: str, name: str = "app.yaml") -> Path:
    p = tmp_path / name
    p.write_text(body, encoding="utf-8")
    return p


APPSET_PRE_FIX = """
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata:
  name: project-applications
spec:
  template:
    metadata:
      name: '{{ .name }}-services'
      annotations:
        argocd.argoproj.io/compare-options: IgnoreExtraneous=true
    spec:
      syncPolicy:
        syncOptions:
          - RespectIgnoreDifferences=true
          - ServerSideApply=true
      ignoreDifferences:
        - group: external-secrets.io
          kind: ExternalSecret
          jsonPointers:
            - /metadata/annotations/force-sync
          jqPathExpressions:
            - .spec.data[]?.remoteRef.conversionStrategy
            - .spec.data[]?.remoteRef.decodingStrategy
            - .spec.data[]?.remoteRef.metadataPolicy
"""


def app(ignore: str, sync_options: str = "- RespectIgnoreDifferences=true", compare: str = "IgnoreExtraneous=true") -> str:
    return f"""
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: demo
  annotations:
    argocd.argoproj.io/compare-options: {compare}
spec:
  syncPolicy:
    syncOptions:
      {sync_options}
  ignoreDifferences:
{ignore}
"""


def test_catches_the_real_appset_regression(tmp_path):
    r = run(str(write(tmp_path, APPSET_PRE_FIX)))
    assert r.returncode == 1, r.stdout
    assert "ExternalSecret" in r.stdout
    assert ".spec.data[]?.remoteRef.conversionStrategy" in r.stdout


def test_fixed_appset_passes(tmp_path):
    fixed = APPSET_PRE_FIX.split("          jqPathExpressions:")[0]
    r = run(str(write(tmp_path, fixed)))
    assert r.returncode == 0, r.stdout


def test_builtin_kind_list_paths_are_safe(tmp_path):
    body = app("""  - group: apps
    kind: StatefulSet
    jqPathExpressions:
      - .spec.volumeClaimTemplates[]?.status
  - group: admissionregistration.k8s.io
    kind: ValidatingWebhookConfiguration
    jsonPointers:
      - /webhooks/0/clientConfig/caBundle""")
    r = run(str(write(tmp_path, body)))
    assert r.returncode == 0, r.stdout


def test_crd_json_pointer_index_is_caught(tmp_path):
    body = app("""  - group: kyverno.io
    kind: ClusterPolicy
    jsonPointers:
      - /spec/rules/0/verifyImages""")
    r = run(str(write(tmp_path, body)))
    assert r.returncode == 1
    assert "/spec/rules/0/verifyImages" in r.stdout


def test_wildcard_kind_is_treated_as_crd(tmp_path):
    body = app("""  - group: "*"
    kind: "*"
    jqPathExpressions:
      - .spec.items[0].name""")
    assert run(str(write(tmp_path, body))).returncode == 1


def test_crd_map_paths_are_safe(tmp_path):
    body = app("""  - group: kyverno.io
    kind: ClusterPolicy
    jqPathExpressions:
      - .status
      - .spec.admission
      - '.data["services-config"]'
    jsonPointers:
      - /metadata/annotations/force-sync""")
    assert run(str(write(tmp_path, body))).returncode == 0


def test_without_respect_ignore_differences_list_rule_is_not_an_error(tmp_path):
    body = app("""  - group: actions.github.com
    kind: AutoscalingListener
    jqPathExpressions:
      - .spec.items[]?.x""", sync_options="- CreateNamespace=true")
    assert run(str(write(tmp_path, body))).returncode == 0


def test_server_side_diff_in_compare_options_is_caught(tmp_path):
    body = app("""  - group: apps
    kind: Deployment
    jsonPointers:
      - /spec/replicas""", compare="IgnoreExtraneous=true,ServerSideDiff=true")
    r = run(str(write(tmp_path, body)))
    assert r.returncode == 1
    assert "ServerSideDiff" in r.stdout


def test_server_side_diff_in_sync_options_is_caught(tmp_path):
    body = app("""  - group: apps
    kind: Deployment
    jsonPointers:
      - /spec/replicas""", sync_options="- ServerSideDiff=true")
    r = run(str(write(tmp_path, body)))
    assert r.returncode == 1
    assert "syncOptions" in r.stdout


def test_empty_root_fails_loudly(tmp_path):
    r = run(str(tmp_path))
    assert r.returncode == 1
    assert "no Application" in r.stdout


def test_repo_infra_argocd_is_clean():
    r = run(str(REPO / "infra" / "argocd"))
    assert r.returncode == 0, r.stdout
