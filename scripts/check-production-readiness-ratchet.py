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
import os
import re
import sys
from pathlib import Path

SKIP_DIRS = {".git", "node_modules", "dist", "build", "coverage", ".next", "vendor"}
IMAGE_RE = re.compile(r"^\s*image:\s*['\"]?([^'\"\s#]+)", re.MULTILINE)
KUSTOMIZE_IMAGES_RE = re.compile(r"(?ms)^images:\s*\n(.*?)(?=^[A-Za-z0-9_-]+:|\Z)")
KUSTOMIZE_DIGEST_RE = re.compile(r"^\s*digest:\s*(sha256:[0-9a-f]{64})\s*$", re.MULTILINE)
KUSTOMIZE_NAME_RE = re.compile(r"^\s*(?:-\s*)?name:\s*['\"]?([^'\"\s#]+)", re.MULTILINE)
PROBE_RE = re.compile(r"^\s*(livenessProbe|readinessProbe|startupProbe):", re.MULTILINE)
TIMEOUT_RE = re.compile(r"^\s*timeoutSeconds:\s*(\d+)", re.MULTILINE)
PLACEHOLDER_RE = re.compile(
    r"(\bplaceholder\b|\byour[_-]?key[_-]?here\b|\bchange[_-]?me\b|\bchangeme\b|"
    r"\bxxx\b|\bexample[-_a-z0-9]*\b|\btest-secret\b|\$\{[A-Z0-9_]+\}|"
    r"__CHANGE_ME|__GENERATE)",
    re.IGNORECASE,
)
SECRET_KIND_RE = re.compile(r"^kind:\s*Secret\s*$", re.MULTILINE)
SECRET_TEMPLATE_MARKER = "MADFAM-SECRET-TEMPLATE-ONLY v1"
IMAGE_EXEMPT_PREFIX = "IMAGE_PIN_EXEMPT_"


def walk(root: Path, suffixes: set[str]) -> list[Path]:
    files: list[Path] = []
    for path in root.rglob("*"):
        if any(part in SKIP_DIRS for part in path.parts):
            continue
        if path.is_file() and path.suffix.lower() in suffixes:
            files.append(path)
    return files


def kustomize_digest_names(root: Path) -> set[str]:
    names: set[str] = set()
    for path in walk(root, {".yaml", ".yml"}):
        if path.name != "kustomization.yaml":
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        for section in KUSTOMIZE_IMAGES_RE.findall(text):
            for entry in re.split(r"(?m)^-\s+", section):
                if not KUSTOMIZE_DIGEST_RE.search(entry):
                    continue
                name = KUSTOMIZE_NAME_RE.search(entry)
                if name:
                    names.add(name.group(1))
    return names


def image_exemption_key(image_ref: str) -> str:
    leaf = image_ref.split("@", 1)[0].split(":", 1)[0].rsplit("/", 1)[-1]
    sanitized = re.sub(r"[^A-Z0-9]", "_", leaf.upper())
    return IMAGE_EXEMPT_PREFIX + sanitized


def read_image_exemptions(env: dict[str, str] | None = None) -> dict[str, str]:
    src = env if env is not None else os.environ
    return {
        key: value
        for key, value in src.items()
        if key.startswith(IMAGE_EXEMPT_PREFIX) and value.strip()
    }


def check_images(root: Path, errors: list[str], exemptions: dict[str, str]) -> None:
    kustomize_pinned = kustomize_digest_names(root)
    for path in walk(root, {".yaml", ".yml"}):
        text = path.read_text(encoding="utf-8", errors="replace")
        for match in IMAGE_RE.finditer(text):
            image = match.group(1)
            if "infra/k8s" in str(path) and "@sha256:" not in image:
                if image in kustomize_pinned:
                    continue
                if image == "IMAGE" and "components/deployment-template" in str(path):
                    continue
                if image_exemption_key(image) in exemptions:
                    continue
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
        for doc in re.split(r"^---\s*$", text, flags=re.MULTILINE):
            if not SECRET_KIND_RE.search(doc):
                continue
            if SECRET_TEMPLATE_MARKER in doc or SECRET_TEMPLATE_MARKER in text:
                continue
            uncommented = "\n".join(
                line.partition("#")[0] for line in doc.splitlines()
            )
            if PLACEHOLDER_RE.search(uncommented):
                errors.append(f"{path}: Kubernetes Secret contains placeholder-looking value")
                break


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
    check_images(root, errors, read_image_exemptions())
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
