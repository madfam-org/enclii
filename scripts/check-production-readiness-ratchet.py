#!/usr/bin/env python3
"""Reusable production-readiness ratchet checks.

Usage:
  check-production-readiness-ratchet.py [--warn-only] /path/to/repo

This is intentionally focused on failure classes already seen in production:
tag-only images, unsafe probe defaults, placeholder secrets, and workspace
package export drift.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

SKIP_DIRS = {".git", "node_modules", "dist", "build", "coverage", ".next", "vendor"}
IMAGE_RE = re.compile(r"^\s*image:\s*['\"]?([^'\"\s#]+)", re.MULTILINE)
PROBE_RE = re.compile(r"^\s*(livenessProbe|readinessProbe|startupProbe):", re.MULTILINE)
TIMEOUT_RE = re.compile(r"^\s*timeoutSeconds:\s*(\d+)", re.MULTILINE)
PLACEHOLDER_RE = re.compile(r"\b(placeholder|your[_-]?key[_-]?here|changeme|xxx|example-secret|test-secret)\b", re.IGNORECASE)
SECRET_KIND_RE = re.compile(r"^kind:\s*Secret\s*$", re.MULTILINE)


def walk(root: Path, suffixes: set[str]) -> list[Path]:
    files: list[Path] = []
    for path in root.rglob("*"):
        if any(part in SKIP_DIRS for part in path.parts):
            continue
        if path.is_file() and path.suffix.lower() in suffixes:
            files.append(path)
    return files


def check_images(root: Path, errors: list[str]) -> None:
    for path in walk(root, {".yaml", ".yml"}):
        text = path.read_text(encoding="utf-8", errors="replace")
        for match in IMAGE_RE.finditer(text):
            image = match.group(1)
            if "infra/k8s" in str(path) and "@sha256:" not in image:
                errors.append(f"{path}: image is not digest-pinned: {image}")


def check_probes(root: Path, errors: list[str]) -> None:
    for path in walk(root, {".yaml", ".yml"}):
        text = path.read_text(encoding="utf-8", errors="replace")
        if not PROBE_RE.search(text):
            continue
        timeouts = [int(m.group(1)) for m in TIMEOUT_RE.finditer(text)]
        if not timeouts:
            errors.append(f"{path}: probe present without explicit timeoutSeconds")
        elif any(value < 3 for value in timeouts):
            errors.append(f"{path}: probe timeoutSeconds below 3")


def check_placeholder_secrets(root: Path, errors: list[str]) -> None:
    for path in walk(root, {".yaml", ".yml", ".env", ".example"}):
        text = path.read_text(encoding="utf-8", errors="replace")
        if SECRET_KIND_RE.search(text) and PLACEHOLDER_RE.search(text):
            errors.append(f"{path}: Kubernetes Secret contains placeholder-looking value")


def check_workspace_exports(root: Path, errors: list[str]) -> None:
    for path in walk(root, {".json"}):
        if path.name != "package.json":
            continue
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError:
            continue
        exports = data.get("exports")
        if not isinstance(exports, dict):
            continue
        for key, value in exports.items():
            if isinstance(value, dict) and "import" in value and not ({"require", "default"} & set(value)):
                errors.append(f"{path}: exports.{key} has import but no require/default")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--warn-only", action="store_true", help="print findings but exit 0")
    parser.add_argument("repo", nargs="?", default=".")
    args = parser.parse_args()

    root = Path(args.repo).resolve()
    errors: list[str] = []
    check_images(root, errors)
    check_probes(root, errors)
    check_placeholder_secrets(root, errors)
    check_workspace_exports(root, errors)
    if errors:
        heading = "Production-readiness ratchet findings:" if args.warn_only else "Production-readiness ratchet failed:"
        print(heading, file=sys.stderr)
        print("\n".join(errors), file=sys.stderr)
        return 0 if args.warn_only else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
