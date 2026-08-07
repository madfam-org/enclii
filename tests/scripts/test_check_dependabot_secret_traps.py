"""
Tests for scripts/check-dependabot-secret-traps.py.

Run with:
    pytest tests/scripts/test_check_dependabot_secret_traps.py -v

The load-bearing test here is ``test_catches_the_real_coforma_regression``: it
feeds the lint the exact workflow shape that kept five coforma-studio Dependabot
PRs permanently unmergeable, because a guard that has never been shown to fail
on the bug it exists for is not a guard.
"""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest

SCRIPT = Path(__file__).resolve().parents[2] / "scripts" / "check-dependabot-secret-traps.py"


def run(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run([sys.executable, str(SCRIPT), *args],
                          capture_output=True, text=True)


def write(tmp_path: Path, name: str, body: str) -> Path:
    d = tmp_path / "workflows"
    d.mkdir(exist_ok=True)
    p = d / name
    p.write_text(body, encoding="utf-8")
    return p


# The verbatim shape from coforma-studio ci.yml before #119 — the one that
# produced "The template is not valid ... Unexpected value ''" on every
# Dependabot PR while main stayed green.
COFORMA_PRE_FIX = """
name: CI
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
        credentials:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
        env:
          POSTGRES_PASSWORD: postgres
      redis:
        image: redis:7
        credentials:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
    steps:
      - uses: actions/checkout@v6
"""

COFORMA_FIXED = COFORMA_PRE_FIX.replace(
    """        credentials:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
""", "")


def test_catches_the_real_coforma_regression(tmp_path):
    """The exact pre-#119 shape must be reported ARMED and exit non-zero."""
    write(tmp_path, "ci.yml", COFORMA_PRE_FIX)
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 1, r.stdout
    assert "2 armed" in r.stdout
    assert "services.postgres.credentials" in r.stdout
    assert "services.redis.credentials" in r.stdout


def test_passes_once_the_credentials_are_removed(tmp_path):
    write(tmp_path, "ci.yml", COFORMA_FIXED)
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 0, r.stdout
    assert "0 armed" in r.stdout


def test_latent_when_not_pull_request_triggered(tmp_path):
    """Same trap, push-only: real but not currently reachable by Dependabot."""
    write(tmp_path, "ci.yml", COFORMA_PRE_FIX.replace(
        "  pull_request:\n    branches: [main]\n", ""))
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 0, r.stdout          # latent alone does not fail
    assert "2 latent" in r.stdout

    r2 = run("--include-latent", str(tmp_path / "workflows"))
    assert r2.returncode == 1, r2.stdout        # ...unless explicitly requested


def test_job_container_credentials_are_caught_too(tmp_path):
    write(tmp_path, "ci.yml", """
name: CI
on: [pull_request]
jobs:
  build:
    runs-on: ubuntu-latest
    container:
      image: private/toolchain:1
      credentials:
        username: ${{ secrets.REGISTRY_USER }}
        password: ${{ secrets.REGISTRY_TOKEN }}
    steps:
      - uses: actions/checkout@v6
""")
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 1, r.stdout
    assert "container.credentials" in r.stdout


def test_hardcoded_or_absent_credentials_are_not_flagged(tmp_path):
    """Only `secrets.` expressions collapse to '' on Dependabot runs."""
    write(tmp_path, "ci.yml", """
name: CI
on: [pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15
      other:
        image: ghcr.io/x/y:1
        credentials:
          username: ${{ github.actor }}
          password: ${{ github.token }}
    steps:
      - uses: actions/checkout@v6
""")
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 0, r.stdout


def test_reports_what_it_scanned(tmp_path):
    """Read-proof: a lint that scanned nothing must not read as a clean lint."""
    (tmp_path / "workflows").mkdir()
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 0
    assert "scanned 0 workflow file(s)" in r.stdout


def test_unparseable_yaml_is_ignored_not_fatal(tmp_path):
    write(tmp_path, "broken.yml", "name: [unclosed\n  : :\n")
    r = run(str(tmp_path / "workflows"))
    assert r.returncode == 0, r.stdout


def test_runs_against_this_repo_and_is_clean():
    """enclii's own workflows must stay free of the trap."""
    root = Path(__file__).resolve().parents[2]
    r = run(str(root / ".github" / "workflows"))
    assert r.returncode == 0, r.stdout
