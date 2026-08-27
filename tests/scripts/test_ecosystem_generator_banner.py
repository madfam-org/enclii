"""
Tests for docs/templates/ecosystem/generator.py — Enclii-first banner emission.

Run with:
    pytest tests/scripts/test_ecosystem_generator_banner.py -v

Read `test_banner_is_not_what_currently_saves_the_doc` first: it pins the
honest reason this banner matters. The rendered doc passes the Enclii-first
guard today WITHOUT the banner, purely because two ALLOW_TERMs happen to fall
inside the checker's ±4-line context window. The banner converts that
incidental pass into a declared one.

The checker itself lives in the private `internal-devops` repo, so it is not
importable here. Its contract is reproduced in `EnclíiFirstGuard` below and
pinned by `test_guard_reproduction_matches_documented_contract`. If the real
checker changes, that test is the thing that should be updated in lockstep.
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
GENERATOR_DIR = REPO_ROOT / "docs" / "templates" / "ecosystem"

sys.path.insert(0, str(GENERATOR_DIR))

from generator import (  # noqa: E402
    LEGACY_RAW_BANNER,
    LEGACY_RAW_MARKER,
    render,
)
from metadata import REPOS_FULL  # noqa: E402


# ---------------------------------------------------------------------------
# Reproduction of internal-devops/scripts/check-enclii-first-docs.py
#
# Only the parts that decide whether a root ECOSYSTEM.md passes. Kept
# deliberately verbatim (same regexes, same terms, same window arithmetic) so
# divergence from the real checker is visible in review.
# ---------------------------------------------------------------------------

GUARD_MARKER = "MADFAM-ENCLII-FIRST-LEGACY-RAW v1"

RAW_TOOL_PATTERNS = (
    re.compile(
        r"\bkubectl\s+(apply|create|delete|edit|exec|patch|rollout|scale|set|annotate|label)\b"
    ),
    re.compile(r"\bhelm\s+(install|upgrade|rollback|uninstall)\b"),
    re.compile(r"\bdocker\s+exec\b"),
    re.compile(r"\bssh\b.*\bkubectl\b"),
)

ALLOW_TERMS = (
    "break-glass",
    "break glass",
    "emergency",
    "bootstrap",
    "local",
    "local development",
    "test-only",
    "integration test",
    "kind",
    "historical incident",
    "no kubectl",
    "no `kubectl",
    "prohibited",
    "do not use",
    "don't use",
    "instead of",
    "replaces",
    "replaced",
    "not use raw",
    "not a routine",
    "not viable",
    "networkpolicy-isolated",
    "bootstrap-only",
    "compatibility",
)


def _context_allowed(lines: list[str], index: int) -> bool:
    start = max(0, index - 4)
    end = min(len(lines), index + 5)
    context = "\n".join(lines[start:end]).lower()
    return any(term in context for term in ALLOW_TERMS)


def guard_findings(text: str, *, honor_marker: bool = True) -> list[str]:
    """Return guard findings for a root-level doc. Empty list == passes."""
    if honor_marker and GUARD_MARKER in text:
        return []
    lines = text.splitlines()
    findings = []
    for index, line in enumerate(lines):
        if not any(pattern.search(line) for pattern in RAW_TOOL_PATTERNS):
            continue
        if _context_allowed(lines, index):
            continue
        findings.append(f"{index + 1}: {line.strip()}")
    return findings


# ---------------------------------------------------------------------------
# The honest limitation
# ---------------------------------------------------------------------------


def test_banner_is_not_what_currently_saves_the_doc() -> None:
    """Rendered output passes the guard even with the marker ignored.

    The "Break-glass-only access" paragraph is the only raw-tool match, and it
    is excused by route (a): "break-glass" and "bootstrap" sit inside the ±4
    line window. So the banner is NOT load-bearing for today's template text —
    it is load-bearing for every future edit to that prose, and for the
    hand-patch loop it ends (madfam-org/forj#134).

    Recorded as a test so nobody later "simplifies" the banner away believing
    the guard would catch the regression. It would not, until the surrounding
    wording drifts.
    """
    rendered = render("forj", REPOS_FULL["forj"])
    assert guard_findings(rendered, honor_marker=False) == []


def test_prose_drift_without_banner_would_fail_the_guard() -> None:
    """Why the banner exists: strip the ALLOW_TERMs and route (a) collapses.

    This simulates a plausible future edit that rewords the break-glass
    paragraph. Without the marker the guard fires; with it, the doc is safe.
    """
    rendered = render("forj", REPOS_FULL["forj"])
    drifted = rendered.replace("break-glass", "restricted").replace(
        "bootstrap", "initial setup"
    )

    assert guard_findings(drifted, honor_marker=False), (
        "expected the reworded break-glass paragraph to trip the guard; "
        "if this stops firing the simulation no longer models the risk"
    )
    # The banner still carries it, because route (b) is unconditional.
    assert guard_findings(drifted) == []


# ---------------------------------------------------------------------------
# What the generator must emit
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_every_repo_renders_with_the_marker(repo: str) -> None:
    assert LEGACY_RAW_MARKER in render(repo, REPOS_FULL[repo])


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_every_repo_renders_guard_clean(repo: str) -> None:
    findings = guard_findings(render(repo, REPOS_FULL[repo]))
    assert findings == [], f"{repo} would fail the Enclii-first guard:\n" + "\n".join(
        findings
    )


def test_marker_string_matches_the_checker_contract() -> None:
    """The generator's marker must equal the checker's LEGACY_RAW_MARKER."""
    assert LEGACY_RAW_MARKER == GUARD_MARKER


def test_banner_sits_immediately_after_the_h1() -> None:
    """Position is pinned to match the hand-applied banner in forj#134."""
    lines = render("forj", REPOS_FULL["forj"]).splitlines()
    assert lines[0] == "# forj — Ecosystem Context"
    assert lines[1] == ""
    assert lines[2] == "> [!IMPORTANT]"
    assert lines[3].startswith(f"> {LEGACY_RAW_MARKER}:")


def test_banner_is_a_well_formed_github_alert() -> None:
    banner_lines = LEGACY_RAW_BANNER.splitlines()
    assert banner_lines[0] == "> [!IMPORTANT]"
    assert all(line.startswith(">") for line in banner_lines)
    assert banner_lines[-1].endswith("missing Enclii adapter gap.")


def test_banner_matches_fleet_canonical_bytes() -> None:
    """Byte-for-byte equality with the banner already in every fleet repo.

    Sourced 2026-08-27 from madfam-org/forj main (and identical in janua,
    tezca, dhanam, karafiel, avala, fortuna, enclii, selva-office,
    digifab-quoting). Re-rendering must not perturb files that already carry it.
    """
    canonical = (
        "> [!IMPORTANT]\n"
        "> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw"
        " infrastructure command examples.\n"
        "> Routine production operations must use Enclii web, API, or CLI. Treat raw\n"
        "> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container\n"
        "> access as platform bootstrap or documented break-glass only, and record any\n"
        "> missing Enclii adapter gap."
    )
    assert LEGACY_RAW_BANNER == canonical


def test_banner_emitted_exactly_once() -> None:
    assert render("forj", REPOS_FULL["forj"]).count(LEGACY_RAW_MARKER) == 1


def test_render_is_deterministic() -> None:
    meta = REPOS_FULL["forj"]
    assert render("forj", meta) == render("forj", meta)


# ---------------------------------------------------------------------------
# The live fleet docs must stay clean
# ---------------------------------------------------------------------------


def test_this_repos_own_ecosystem_doc_carries_the_marker() -> None:
    """enclii's checked-in ECOSYSTEM.md is itself scanned by the guard."""
    doc = REPO_ROOT / "ECOSYSTEM.md"
    assert doc.is_file()
    assert LEGACY_RAW_MARKER in doc.read_text(encoding="utf-8")
