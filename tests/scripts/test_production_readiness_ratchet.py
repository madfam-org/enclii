"""
Tests for production-readiness ratchet helpers.

Run with:
    pytest tests/scripts/test_production_readiness_ratchet.py -v
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
AGGREGATE_SCRIPT = REPO_ROOT / "scripts" / "check-production-readiness-ratchet.py"
IMAGE_PINNING_SCRIPT = REPO_ROOT / "scripts" / "ratchet" / "check-image-pinning.py"


def load_module(module_name: str, path: Path):
    spec = importlib.util.spec_from_file_location(module_name, path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[module_name] = module
    spec.loader.exec_module(module)
    return module


ratchet = load_module("check_production_readiness_ratchet", AGGREGATE_SCRIPT)


def write(path: Path, body: str) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)
    return path


def test_aggregate_image_check_accepts_kustomize_digest_coverage(tmp_path: Path) -> None:
    write(
        tmp_path / "infra/k8s/base/deployment.yaml",
        """
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-api
spec:
  template:
    spec:
      containers:
        - name: api
          image: switchyard-api
""".lstrip(),
    )
    write(
        tmp_path / "infra/k8s/production/kustomization.yaml",
        """
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
images:
  - name: switchyard-api
    newName: ghcr.io/madfam-org/enclii/switchyard-api
    digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
""".lstrip(),
    )

    errors: list[str] = []
    ratchet.check_images(tmp_path, errors, exemptions={})

    assert errors == []


def test_aggregate_image_check_honors_explicit_exemption(tmp_path: Path) -> None:
    write(
        tmp_path / "infra/k8s/production/synthetic-flow-probe.yaml",
        """
apiVersion: apps/v1
kind: Deployment
metadata:
  name: synthetic-flow-probe
spec:
  template:
    spec:
      containers:
        - name: probe
          image: ghcr.io/madfam-org/enclii/synthetic-flow-probe:placeholder
""".lstrip(),
    )

    errors: list[str] = []
    ratchet.check_images(
        tmp_path,
        errors,
        exemptions={"IMAGE_PIN_EXEMPT_SYNTHETIC_FLOW_PROBE": "unpublished"},
    )

    assert errors == []


def test_placeholder_secret_check_skips_marked_templates(tmp_path: Path) -> None:
    write(
        tmp_path / "infra/k8s/base/secrets.dev.yaml",
        """
---
# MADFAM-SECRET-TEMPLATE-ONLY v1
apiVersion: v1
kind: Secret
metadata:
  name: example-secret
stringData:
  fixture_value: "${GENERATE_ME}"
""".lstrip(),
    )

    errors: list[str] = []
    ratchet.check_placeholder_secrets(tmp_path, errors)

    assert errors == []


def test_placeholder_secret_check_ignores_comment_only_placeholders(tmp_path: Path) -> None:
    write(
        tmp_path / "infra/k8s/production/secret.yaml",
        """
---
apiVersion: v1
kind: Secret
metadata:
  name: real-secret
# password: change-me
stringData:
  password: real-value
""".lstrip(),
    )

    errors: list[str] = []
    ratchet.check_placeholder_secrets(tmp_path, errors)

    assert errors == []


def test_probe_check_requires_explicit_timeout(tmp_path: Path) -> None:
    write(
        tmp_path / "infra/k8s/base/api.yaml",
        """
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    spec:
      containers:
        - name: api
          image: api
          livenessProbe:
            httpGet:
              path: /health
              port: 8080
""".lstrip(),
    )

    errors: list[str] = []
    ratchet.check_probes(tmp_path, errors)

    assert len(errors) == 1
    assert "probe present without explicit timeoutSeconds" in errors[0]


def test_image_pinning_script_accepts_short_name_covered_by_kustomize_digest(
    tmp_path: Path,
) -> None:
    pytest.importorskip("yaml")
    image_pinning = load_module("check_image_pinning_ratchet", IMAGE_PINNING_SCRIPT)
    write(
        tmp_path / "base/deployment.yaml",
        """
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-api
spec:
  template:
    spec:
      containers:
        - name: api
          image: switchyard-api
""".lstrip(),
    )
    write(
        tmp_path / "production/kustomization.yaml",
        """
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
images:
  - name: switchyard-api
    newName: ghcr.io/madfam-org/enclii/switchyard-api
    digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
""".lstrip(),
    )

    failures = image_pinning.find_violations([tmp_path], exemptions={})

    assert failures == []


def test_image_pinning_script_honors_explicit_exemption(tmp_path: Path) -> None:
    pytest.importorskip("yaml")
    image_pinning = load_module("check_image_pinning_exempt", IMAGE_PINNING_SCRIPT)
    write(
        tmp_path / "production/probe.yaml",
        """
apiVersion: apps/v1
kind: Deployment
metadata:
  name: synthetic-flow-probe
spec:
  template:
    spec:
      containers:
        - name: probe
          image: ghcr.io/madfam-org/enclii/synthetic-flow-probe:placeholder
""".lstrip(),
    )

    failures = image_pinning.find_violations(
        [tmp_path],
        exemptions={"IMAGE_PIN_EXEMPT_SYNTHETIC_FLOW_PROBE": "unpublished"},
    )

    assert failures == []
