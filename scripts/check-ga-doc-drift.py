#!/usr/bin/env python3
"""Warn when GA task state drifts outside canonical Enclii GA docs.

Canonical GA status/task state lives in docs/production/: REMAINING_OPS_GA.md
(task queue), GA_READINESS_SCORECARD.md (dashboard), and
COMMERCIAL_GA_TRACKER.md (program state). Every other doc must point there
instead of restating GA status, percentages, or dates.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PATTERN = re.compile(r"\b(single source of truth|canonical execution queue|open ops tasks|total open ops tasks|stability ga|commercial ga)\b", re.IGNORECASE)
SKIP = {
    ".git", "node_modules", "dist", "build", "coverage", ".next",
    ".pytest_cache", ".serena", ".playwright-mcp", "out",
    "docs/archive",
    # Dated historical session records — same rationale as docs/archive.
    "claudedocs",
    # GA program home: the canonical trackers plus the plan/runbook/proof docs
    # they link to, which necessarily name the milestones they define. The
    # guard targets GA state leaking *outside* the program home.
    "docs/production",
}

findings: list[str] = []
for path in ROOT.rglob("*.md"):
    rel = path.relative_to(ROOT).as_posix()
    if any(rel == s or rel.startswith(s + "/") for s in SKIP):
        continue
    text = path.read_text(encoding="utf-8", errors="replace")
    for line_no, line in enumerate(text.splitlines(), 1):
        if PATTERN.search(line):
            findings.append(f"{rel}:{line_no}: {line.strip()}")

if findings:
    print("Potential GA tracker drift outside canonical docs:", file=sys.stderr)
    print("\n".join(findings), file=sys.stderr)
    print("\nKeep task state in REMAINING_OPS_GA.md and dashboard state in GA_READINESS_SCORECARD.md.", file=sys.stderr)
    sys.exit(1)
