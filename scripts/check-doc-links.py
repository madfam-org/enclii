#!/usr/bin/env python3
"""Check local Markdown links in Enclii docs.

The checker treats root-relative docs links like /guides/foo as relative to the
repository docs directory, matching the published docs site convention.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlparse

ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
LINK_RE = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")
SKIP_DIRS = {".git", "node_modules", "dist", "build", "coverage", ".next", ".pytest_cache", ".serena", ".playwright-mcp", "out"}
SKIP_PREFIXES = {"docs/archive"}
EXTS = {".md", ".mdx", ".rst", ".txt"}
STANDARD_NAMES = {"README", "LICENSE", "SECURITY", "CONTRIBUTING", "CHANGELOG", "CODE_OF_CONDUCT"}


def iter_docs() -> list[Path]:
    files: list[Path] = []
    for path in ROOT.rglob("*"):
        if any(part in SKIP_DIRS for part in path.parts):
            continue
        rel = path.relative_to(ROOT).as_posix()
        if any(rel == prefix or rel.startswith(prefix + "/") for prefix in SKIP_PREFIXES):
            continue
        if not path.is_file():
            continue
        if path.suffix.lower() in EXTS or path.name in STANDARD_NAMES:
            files.append(path)
    return files


def candidates(source: Path, target: str) -> list[Path]:
    parsed = urlparse(target.strip().strip('"\''))
    if parsed.scheme or parsed.netloc or parsed.path == "":
        return []
    raw_path = unquote(parsed.path)
    if raw_path.startswith("#"):
        return []
    if raw_path.startswith("/"):
        base = DOCS / raw_path.lstrip("/")
    else:
        base = source.parent / raw_path
    result = [base]
    if base.suffix == "":
        result.extend([base.with_suffix(".md"), base / "index.md", base / "README.md"])
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--warn-only", action="store_true", help="print findings but exit 0")
    args = parser.parse_args()

    broken: list[str] = []
    for file_path in iter_docs():
        rel = file_path.relative_to(ROOT)
        for line_no, line in enumerate(file_path.read_text(encoding="utf-8", errors="replace").splitlines(), 1):
            for match in LINK_RE.finditer(line):
                target = match.group(1).split()[0]
                checks = candidates(file_path, target)
                if checks and not any(path.exists() for path in checks):
                    broken.append(f"{rel}:{line_no}: {match.group(1)}")
    if broken:
        print("Broken local documentation links:", file=sys.stderr)
        print("\n".join(broken), file=sys.stderr)
        return 0 if args.warn_only else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
