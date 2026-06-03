#!/usr/bin/env python3
"""
Production-readiness ratchet F4 — Next.js transpilePackages coverage.

Fails when a Next.js app depends on local workspace packages but its
next.config.* file does not list those packages in transpilePackages.
The check follows direct and transitive workspace:* dependencies because
Next/Turbopack can otherwise stop at a package boundary and fail to resolve
nested workspace packages at build time.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path

EXEMPT_PREFIX = "TRANSPILE_PACKAGES_EXEMPT_"
DEP_FIELDS = ("dependencies", "devDependencies", "peerDependencies", "optionalDependencies")
CONFIG_NAMES = (
    "next.config.js",
    "next.config.mjs",
    "next.config.cjs",
    "next.config.ts",
)


def exemption_key(app_name: str) -> str:
    return EXEMPT_PREFIX + re.sub(r"[^A-Z0-9]", "_", app_name.upper())


def read_package(path: Path) -> dict:
    with path.open(encoding="utf-8") as fh:
        return json.load(fh)


def workspace_packages(root: Path) -> dict[str, tuple[Path, dict]]:
    packages: dict[str, tuple[Path, dict]] = {}
    for package_json in sorted(root.glob("packages/*/package.json")):
        try:
            doc = read_package(package_json)
        except (OSError, json.JSONDecodeError):
            continue
        name = doc.get("name")
        if isinstance(name, str) and name:
            packages[name] = (package_json.parent, doc)
    return packages


def dependency_names(doc: dict) -> set[str]:
    names: set[str] = set()
    for field in DEP_FIELDS:
        deps = doc.get(field)
        if isinstance(deps, dict):
            names.update(
                name
                for name, version in deps.items()
                if isinstance(version, str) and version.startswith("workspace:")
            )
    return names


def required_workspace_deps(app_doc: dict, packages: dict[str, tuple[Path, dict]]) -> set[str]:
    required: set[str] = set()
    queue = sorted(dependency_names(app_doc) & set(packages))
    while queue:
        name = queue.pop(0)
        if name in required:
            continue
        required.add(name)
        _, package_doc = packages[name]
        for dep in sorted(dependency_names(package_doc) & set(packages)):
            if dep not in required:
                queue.append(dep)
    return required


def next_apps(root: Path) -> list[tuple[Path, dict]]:
    apps: list[tuple[Path, dict]] = []
    for package_json in sorted(root.glob("apps/*/package.json")):
        try:
            doc = read_package(package_json)
        except (OSError, json.JSONDecodeError):
            continue
        deps = {}
        for field in DEP_FIELDS:
            value = doc.get(field)
            if isinstance(value, dict):
                deps.update(value)
        if "next" in deps:
            apps.append((package_json.parent, doc))
    return apps


def parse_transpile_packages(app_dir: Path) -> tuple[set[str], Path | None]:
    config_path = next((app_dir / name for name in CONFIG_NAMES if (app_dir / name).exists()), None)
    if config_path is None:
        return set(), None

    text = config_path.read_text(encoding="utf-8", errors="replace")
    match = re.search(r"transpilePackages\s*:\s*\[(.*?)\]", text, flags=re.DOTALL)
    if not match:
        return set(), config_path
    packages = {
        quoted[0] or quoted[1]
        for quoted in re.findall(r"'([^']+)'|\"([^\"]+)\"", match.group(1))
    }
    return packages, config_path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("root", nargs="?", default=".", help="Repo root, default cwd")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    exemptions = {
        key for key, value in os.environ.items() if key.startswith(EXEMPT_PREFIX) and value.strip()
    }
    packages = workspace_packages(root)

    failures: list[str] = []
    for app_dir, app_doc in next_apps(root):
        app_name = app_doc.get("name") or app_dir.name
        if exemption_key(str(app_name)) in exemptions:
            continue

        required = required_workspace_deps(app_doc, packages)
        if not required:
            continue

        configured, config_path = parse_transpile_packages(app_dir)
        missing = sorted(required - configured)
        if missing:
            location = config_path.relative_to(root) if config_path else app_dir.relative_to(root)
            failures.append(
                f"{location}: app {app_name!r} depends on workspace package(s) "
                f"{missing!r} but does not list them in transpilePackages. "
                f"Add them or set ``{exemption_key(str(app_name))}=<reason>``."
            )

    if failures:
        sys.stderr.write(
            "transpilePackages ratchet FAILED:\n\n"
            + "\n".join(f"  - {failure}" for failure in failures)
            + "\n\nNext.js apps that consume local dist-shaped workspace packages "
            "must list direct and transitive workspace dependencies in "
            "transpilePackages so Next/Turbopack can resolve them during builds.\n"
        )
        return 1

    print("OK: Next.js apps list required workspace packages in transpilePackages.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
