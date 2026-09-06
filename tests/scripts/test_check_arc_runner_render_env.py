"""
Tests for scripts/check-arc-runner-render-env.py — the ARC runner render-env
drift gate.

The gate compares infra/docker/arc-runner/Dockerfile against the commons
contract (`y4d_spec.render_environment`). These tests exercise the parsing and
the comparison against a SYNTHETIC contract, so they need neither network nor
the hyperobjects-spec package: the point under test is our parser and our
subset/t64 rules, not the spec's contents.

Run with:
    pytest tests/scripts/test_check_arc_runner_render_env.py -v
"""
from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-arc-runner-render-env.py"
REAL_DOCKERFILE = REPO_ROOT / "infra" / "docker" / "arc-runner" / "Dockerfile"

# Hyphen in filename means we can't `import` directly — load by path.
spec = importlib.util.spec_from_file_location("check_arc_runner_render_env", SCRIPT)
assert spec is not None and spec.loader is not None
mod = importlib.util.module_from_spec(spec)
sys.modules["check_arc_runner_render_env"] = mod
spec.loader.exec_module(mod)


class FakeSpec:
    """Stand-in for y4d_spec.render_environment."""

    def __init__(self, apt=(), ci_extra=(), version="2026.02.13", sha256="a" * 64):
        self.APT_PACKAGES = list(apt)
        self.CI_EXTRA_APT_PACKAGES = list(ci_extra)
        self.OPENSCAD_VERSION = version
        self.OPENSCAD_SHA256 = sha256


TWO_LAYER_DOCKERFILE = """\
FROM ghcr.io/actions/actions-runner:2.337.0
USER root
RUN apt-get update \\
 && apt-get install -y --no-install-recommends \\
      libatomic1 \\
      jq \\
 && rm -rf /var/lib/apt/lists/*
RUN apt-get update \\
 && apt-get install -y --no-install-recommends \\
      libgl1 \\
      libglib2.0-0t64 \\
      xvfb \\
 && fc-cache -f \\
 && rm -rf /var/lib/apt/lists/*
ARG OPENSCAD_VERSION=2026.02.13
ARG OPENSCAD_SHA256=%s
RUN curl -fsSL "https://example/OpenSCAD-${OPENSCAD_VERSION}.AppImage" -o /tmp/o
""" % ("a" * 64)


# ---------------------------------------------------------------------------
# parse_apt_packages
# ---------------------------------------------------------------------------

def test_reads_every_apt_install_not_just_the_first():
    """The Dockerfile splits CI deps and render deps across two RUN layers.

    A first-match parser (yantra4d's, which only ever sees one layer) would
    read `libatomic1 jq` and report every render package as missing. This is
    the single behavioural difference between the two parsers and the whole
    reason this one is not a copy.
    """
    got = mod.parse_apt_packages(TWO_LAYER_DOCKERFILE)
    assert got == {"libatomic1", "jq", "libgl1", "libglib2.0-0t64", "xvfb"}


def test_flags_and_chained_commands_are_not_packages():
    text = (
        "RUN apt-get update \\\n"
        " && apt-get install -y --no-install-recommends \\\n"
        "      libgl1 \\\n"
        " && rm -rf /var/lib/apt/lists/*\n"
    )
    assert mod.parse_apt_packages(text) == {"libgl1"}


def test_no_apt_install_at_all_reads_as_empty():
    assert mod.parse_apt_packages("FROM scratch\n") == set()


# ---------------------------------------------------------------------------
# parse_args_block
# ---------------------------------------------------------------------------

def test_reads_the_openscad_args():
    args = mod.parse_args_block(TWO_LAYER_DOCKERFILE)
    assert args["OPENSCAD_VERSION"] == "2026.02.13"
    assert args["OPENSCAD_SHA256"] == "a" * 64


# ---------------------------------------------------------------------------
# the t64 rule
# ---------------------------------------------------------------------------

def test_t64_spelling_satisfies_the_contract_name():
    """Ubuntu 24.04's 64-bit time_t rename is not drift."""
    assert mod.satisfies("libglib2.0-0", {"libglib2.0-0t64"})
    assert mod.satisfies("libglib2.0-0", {"libglib2.0-0"})


def test_t64_is_a_narrow_rule_not_a_fuzzy_match():
    """It must not turn any prefix match into a pass."""
    assert not mod.satisfies("libgl1", {"libgl1-mesa-dri"})
    assert not mod.satisfies("libcomerr2", {"libcomerr"})


# ---------------------------------------------------------------------------
# compare — the subset direction
# ---------------------------------------------------------------------------

def test_a_contract_package_the_image_lacks_is_drift():
    problems = mod.compare(
        FakeSpec(apt=["libgl1", "libcomerr2"]), TWO_LAYER_DOCKERFILE)
    assert len(problems) == 1
    assert "libcomerr2" in problems[0]
    assert "libgl1" not in problems[0]


def test_extra_packages_in_the_image_are_not_drift():
    """The runner is a CI machine and legitimately carries more than the
    platform image. Only the missing direction breaks renders."""
    assert mod.compare(FakeSpec(apt=["libgl1"]), TWO_LAYER_DOCKERFILE) == []


def test_ci_extra_packages_are_required_too():
    problems = mod.compare(
        FakeSpec(apt=["libgl1"], ci_extra=["libxrender1"]), TWO_LAYER_DOCKERFILE)
    assert len(problems) == 1 and "libxrender1" in problems[0]


def test_build_only_packages_are_ignored_on_both_sides():
    """Whether an image fetches with wget or curl is never drift."""
    assert mod.compare(
        FakeSpec(apt=["libgl1", "wget", "curl", "ca-certificates"]),
        TWO_LAYER_DOCKERFILE) == []


def test_t64_rename_alone_does_not_trip_the_gate():
    assert mod.compare(
        FakeSpec(apt=["libglib2.0-0"]), TWO_LAYER_DOCKERFILE) == []


# ---------------------------------------------------------------------------
# compare — the ARGs
# ---------------------------------------------------------------------------

def test_a_different_openscad_version_is_drift():
    problems = mod.compare(
        FakeSpec(apt=["libgl1"], version="2026.02.01"), TWO_LAYER_DOCKERFILE)
    assert len(problems) == 1
    assert "OPENSCAD_VERSION differs" in problems[0]


def test_a_nearly_right_sha256_is_wrong():
    """The Dockerfile pipes this ARG straight into `sha256sum -c`."""
    problems = mod.compare(
        FakeSpec(apt=["libgl1"], sha256="a" * 63 + "b"), TWO_LAYER_DOCKERFILE)
    assert len(problems) == 1 and "OPENSCAD_SHA256 differs" in problems[0]


def test_a_missing_arg_is_reported_rather_than_skipped():
    problems = mod.compare(FakeSpec(apt=["libgl1"]), "FROM scratch\n")
    assert any("declares no ARG OPENSCAD_VERSION" in p for p in problems)
    assert any("declares no ARG OPENSCAD_SHA256" in p for p in problems)


# ---------------------------------------------------------------------------
# the real Dockerfile
# ---------------------------------------------------------------------------

def test_the_real_dockerfile_parses_and_pins_both_args():
    """A read-proof: a parser that silently read nothing would pass every
    subset check above for the wrong reason."""
    text = REAL_DOCKERFILE.read_text(encoding="utf-8")
    packages = mod.parse_apt_packages(text)
    assert "libgl1" in packages, "render layer not parsed"
    assert "libatomic1" in packages, "CI-deps layer not parsed"
    args = mod.parse_args_block(text)
    assert args["OPENSCAD_VERSION"]
    assert len(args["OPENSCAD_SHA256"]) == 64


def test_the_gate_stands_down_on_a_spec_without_the_module(monkeypatch):
    """A spec pin bump must never make this guard red by accident."""
    monkeypatch.setitem(sys.modules, "y4d_spec", None)
    spec_mod, reason = mod.load_spec()
    assert spec_mod is None and reason
