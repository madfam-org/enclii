"""
Tests for scripts/check-platform-infra-copies.sh.

Run with:
    pytest tests/scripts/test_check_platform_infra_copies.py -v

The check asserts that an ArgoCD-synced platform-infra manifest stays
byte-identical to its canonical source (see the script header for the
2026-09-17 redis OOMKill incident it guards against). These tests drive it
against throwaway trees via PLATFORM_INFRA_COPIES_REPO_ROOT.
"""
from __future__ import annotations

import os
import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-platform-infra-copies.sh"

# The registered pair the script ships with.
SYNCED = "infra/k8s/platform-infra/redis.yaml"
CANONICAL = "infra/k8s/production/data/redis.yaml"

# A representative redis manifest carrying the memory cap whose loss caused the
# incident. Byte-equality is all the check cares about, so the content only has
# to be stable, not a full Deployment.
REDIS_YAML = """\
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: data
spec:
  template:
    spec:
      containers:
        - name: redis
          command:
            - /bin/sh
            - -c
            - |
              redis-server \\
                --appendonly yes \\
                --maxmemory 200mb \\
                --maxmemory-policy allkeys-lru \\
                --requirepass "$(REDIS_PASSWORD)"
"""


def without_maxmemory(yaml: str) -> str:
    """Drop the --maxmemory line — the exact drift of the 2026-09-17 incident."""
    return "".join(
        line for line in yaml.splitlines(keepends=True) if "--maxmemory 200mb" not in line
    )


def write(path: Path, body: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)


def run_checker(repo_root: Path) -> subprocess.CompletedProcess[str]:
    env = dict(os.environ)
    env["PLATFORM_INFRA_COPIES_REPO_ROOT"] = str(repo_root)
    return subprocess.run(
        ["bash", str(SCRIPT)],
        cwd=REPO_ROOT,
        env=env,
        capture_output=True,
        text=True,
    )


def test_passes_when_the_copy_matches_canonical(tmp_path: Path) -> None:
    write(tmp_path / SYNCED, REDIS_YAML)
    write(tmp_path / CANONICAL, REDIS_YAML)
    result = run_checker(tmp_path)
    assert result.returncode == 0, result.stderr
    assert "in sync" in result.stdout


def test_fails_when_the_synced_copy_drifts(tmp_path: Path) -> None:
    drifted = without_maxmemory(REDIS_YAML)
    assert "--maxmemory 200mb" in REDIS_YAML and "--maxmemory 200mb" not in drifted
    write(tmp_path / SYNCED, drifted)  # ArgoCD-synced copy lost the cap
    write(tmp_path / CANONICAL, REDIS_YAML)  # canonical still has it
    result = run_checker(tmp_path)
    assert result.returncode == 1
    assert "DRIFT" in result.stderr
    assert "maxmemory" in result.stderr  # the diff names the dropped line


def test_fails_when_a_registered_file_is_missing(tmp_path: Path) -> None:
    write(tmp_path / CANONICAL, REDIS_YAML)  # synced copy absent entirely
    result = run_checker(tmp_path)
    assert result.returncode == 1
    assert "missing" in result.stderr
