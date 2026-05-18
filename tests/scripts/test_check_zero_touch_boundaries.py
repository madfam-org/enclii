"""
Tests for scripts/check-zero-touch-boundaries.sh.

Run with:
    pytest tests/scripts/test_check_zero_touch_boundaries.py -v
"""
from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-zero-touch-boundaries.sh"


def write(path: Path, body: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)


def make_minimal_repo(root: Path) -> None:
    write(
        root / "infra/argocd/projects/enclii/config.json",
        json.dumps(
            {
                "name": "enclii",
                "repoURL": "https://github.com/madfam-org/enclii.git",
                "branch": "main",
                "manifestPath": "infra/k8s/production",
                "namespace": "enclii",
            }
        ),
    )
    write(
        root / "infra/k8s/production/janua-env-config.yaml",
        """
apiVersion: v1
kind: ConfigMap
data:
  CORS_ORIGINS: "https://app.enclii.dev,https://auth.madfam.io"
  CSRF_TRUSTED_ORIGINS: "https://app.enclii.dev"
""".lstrip(),
    )
    write(root / "infra/k8s/production/expected-tunnel-config.json", "[]\n")
    write(
        root / "apps/status/k8s/madfam/configmap.yaml",
        """
apiVersion: v1
kind: ConfigMap
metadata:
  name: status-madfam-config
data:
  services-config: "[]"
""".lstrip(),
    )
    write(
        root / "infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml",
        """
apiVersion: v1
kind: ConfigMap
data:
  repos.json: |
    {"repos": []}
""".lstrip(),
    )
    write(
        root / "apps/switchyard-ui/components/dashboard/framework-icon.tsx",
        """
const KNOWN_REPO_FRAMEWORKS: Record<string, FrameworkType> = {
};
""".lstrip(),
    )

    active_docs = [
        "README.md",
        "docs/guides/EXTERNAL_REPO_DEPLOY.md",
        "docs/guides/ZERO_TOUCH_CONTRACT.md",
        "docs/guides/ONBOARDING_GUIDE.md",
        "docs/cli/commands/onboard.md",
        "packages/cli/internal/cmd/onboard.go",
    ]
    for rel in active_docs:
        write(root / rel, "zero-touch boundary fixture\n")

    subprocess.run(["git", "init"], cwd=root, check=True, capture_output=True)
    subprocess.run(["git", "add", "."], cwd=root, check=True, capture_output=True)


def run_checker(repo_root: Path) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["ZERO_TOUCH_REPO_ROOT"] = str(repo_root)
    return subprocess.run(
        [str(SCRIPT)],
        cwd=REPO_ROOT,
        env=env,
        text=True,
        capture_output=True,
    )


def test_zero_touch_boundary_passes_minimal_compliant_repo(tmp_path: Path) -> None:
    make_minimal_repo(tmp_path)

    proc = run_checker(tmp_path)

    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert "Zero-touch boundary check passed" in proc.stdout


def test_zero_touch_boundary_rejects_new_argo_project_config(tmp_path: Path) -> None:
    make_minimal_repo(tmp_path)
    write(
        tmp_path / "infra/argocd/projects/new-client/config.json",
        json.dumps(
            {
                "name": "new-client",
                "repoURL": "https://github.com/madfam-org/new-client.git",
                "branch": "main",
                "manifestPath": "infra/k8s/production",
                "namespace": "new-client",
            }
        ),
    )

    proc = run_checker(tmp_path)

    assert proc.returncode == 1
    assert "New Argo project config is not allowed" in proc.stdout
    assert "new-client" in proc.stdout


def test_zero_touch_boundary_rejects_stale_auto_commit_wording(tmp_path: Path) -> None:
    make_minimal_repo(tmp_path)
    write(
        tmp_path / "docs/cli/commands/onboard.md",
        "Auto-commit config.json to infra/argocd/projects/<name>/ in the enclii repo\n",
    )

    proc = run_checker(tmp_path)

    assert proc.returncode == 1
    assert "Active onboarding docs/CLI still describe Enclii repo commits" in proc.stdout


def test_zero_touch_boundary_rejects_new_janua_product_origin(tmp_path: Path) -> None:
    make_minimal_repo(tmp_path)
    write(
        tmp_path / "infra/k8s/production/janua-env-config.yaml",
        """
apiVersion: v1
kind: ConfigMap
data:
  CORS_ORIGINS: "https://app.enclii.dev,https://app.new-product.example"
  CSRF_TRUSTED_ORIGINS: "https://app.enclii.dev"
""".lstrip(),
    )

    proc = run_checker(tmp_path)

    assert proc.returncode == 1
    assert "Unexpected Janua origin" in proc.stdout
    assert "https://app.new-product.example" in proc.stdout
