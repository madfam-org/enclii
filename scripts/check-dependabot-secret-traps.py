#!/usr/bin/env python3
"""
check-dependabot-secret-traps.py — CI lint preventing workflows that Dependabot
can never run.

THE BUG THIS PREVENTS
=====================
On 2026-08-07 an org-wide sweep found five coforma-studio Dependabot PRs and an
unknown number of enclii ones that had been stuck for weeks. They did not fail a
test. They failed at ``Set up job`` — a job that never started, with no test
output and no obvious cause:

    Secret source: Dependabot
    ##[error]The template is not valid. .github/workflows/ci.yml
    (Line: 191, Col: 21): Unexpected value '', (Line: 192, Col: 21): ...

Cause: service containers carried

    services:
      postgres:
        image: postgres:15
        credentials:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

**Dependabot runs receive an empty secret store.** The expressions resolve to
``''``, and GitHub rejects the ENTIRE workflow template on an empty credentials
value. The job cannot start, so there is nothing to read and nothing to retry.

Why it survived so long: ``main`` stayed green the whole time, because pushes to
``main`` DO have the secrets. That asymmetry made it read as "flaky dependency
PRs" rather than a workflow defect — and crucially, **no rebase, recreate or
rerun could ever have fixed it.** The PRs were structurally unmergeable.

Fixed in coforma-studio#119 and enclii#363. This lint exists so the next
instance is caught at authoring time instead of by a PR that mysteriously never
starts.

WHAT IT CHECKS
==============
For every workflow that can be triggered by ``pull_request`` (the only trigger
Dependabot uses for its PRs), each job's ``services.*.credentials`` and
``container.credentials`` must not be populated from a ``secrets.`` expression.

A finding is reported as ARMED when the workflow is ``pull_request``-triggered,
and as LATENT otherwise — latent instances cannot break Dependabot today but
become armed the moment the trigger is widened.

HOW TO FIX A FINDING
====================
1. Public official images (postgres, redis, node, python…) need no auth at all
   on GitHub-hosted runners, which pull Docker Hub through GitHub's own
   arrangement. Delete the ``credentials`` block. This is what both fixes did.
2. Self-hosted/ARC runners egress from one shared IP where Docker Hub rate
   limits are real. There, keep the credentials and instead add the values to
   the **Dependabot secret store** (Settings → Secrets and variables →
   Dependabot), which is separate from Actions secrets.
3. Mirror the image into GHCR and authenticate with ``GITHUB_TOKEN``.

Never "fix" this by removing the ``pull_request`` trigger — that trades a
visible breakage for a silent loss of coverage.

USAGE
=====
    python3 scripts/check-dependabot-secret-traps.py .github/workflows/
    python3 scripts/check-dependabot-secret-traps.py --include-latent <roots...>

Exit 0 when clean, 1 when an armed trap is found (or any trap with
``--include-latent``).
"""
from __future__ import annotations

import argparse
import sys
from pathlib import Path
from typing import Iterator

try:
    import yaml
except ImportError:  # pragma: no cover - environment problem, not a finding
    print("error: PyYAML is required (pip install pyyaml)", file=sys.stderr)
    raise SystemExit(2)

SECRET_TOKEN = "secrets."


class Finding:
    def __init__(self, path: Path, job: str, where: str, armed: bool, detail: str):
        self.path, self.job, self.where, self.armed, self.detail = path, job, where, armed, detail

    def __str__(self) -> str:
        tag = "ARMED " if self.armed else "latent"
        return f"  [{tag}] {self.path}: job '{self.job}' → {self.where}\n           {self.detail}"


def workflow_files(roots: list[str]) -> Iterator[Path]:
    for root in roots:
        p = Path(root)
        if p.is_file():
            yield p
            continue
        for pattern in ("*.yml", "*.yaml"):
            yield from sorted(p.rglob(pattern))


def triggers(doc: dict) -> set[str]:
    """`on:` is parsed by PyYAML as the boolean True — YAML 1.1 treats `on` as a bool."""
    on = doc.get("on", doc.get(True))
    if isinstance(on, str):
        return {on}
    if isinstance(on, list):
        return set(on)
    if isinstance(on, dict):
        return set(on.keys())
    return set()


def uses_secret(credentials: object) -> bool:
    return SECRET_TOKEN in str(credentials)


def scan(path: Path) -> list[Finding]:
    try:
        doc = yaml.safe_load(path.read_text(encoding="utf-8"))
    except Exception:
        return []  # unparseable YAML is actionlint's job, not ours
    if not isinstance(doc, dict):
        return []

    armed = "pull_request" in triggers(doc)
    out: list[Finding] = []
    jobs = doc.get("jobs")
    if not isinstance(jobs, dict):
        return []

    for job_name, job in jobs.items():
        if not isinstance(job, dict):
            continue

        container = job.get("container")
        if isinstance(container, dict) and uses_secret(container.get("credentials", "")):
            out.append(Finding(path, str(job_name), "container.credentials", armed,
                               f"image {container.get('image', '?')} — resolves to '' on Dependabot runs"))

        services = job.get("services")
        if isinstance(services, dict):
            for svc_name, svc in services.items():
                if isinstance(svc, dict) and uses_secret(svc.get("credentials", "")):
                    out.append(Finding(path, str(job_name), f"services.{svc_name}.credentials", armed,
                                       f"image {svc.get('image', '?')} — resolves to '' on Dependabot runs"))
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("roots", nargs="*", default=[".github/workflows"],
                    help="workflow files or directories (default: .github/workflows)")
    ap.add_argument("--include-latent", action="store_true",
                    help="also fail on findings in workflows not triggered by pull_request")
    args = ap.parse_args()

    findings: list[Finding] = []
    scanned = 0
    for f in workflow_files(args.roots or [".github/workflows"]):
        scanned += 1
        findings.extend(scan(f))

    armed = [f for f in findings if f.armed]
    latent = [f for f in findings if not f.armed]

    # Read-proof: always state what was actually examined. A lint that scanned
    # nothing must never be mistaken for a lint that found nothing.
    print(f"dependabot-secret-traps: scanned {scanned} workflow file(s); "
          f"{len(armed)} armed, {len(latent)} latent")

    if armed:
        print("\nARMED — these workflows are pull_request-triggered, so every Dependabot PR\n"
              "that reaches them dies at 'Set up job' and CANNOT be fixed by rebase:")
        for f in armed:
            print(f)
    if latent and (args.include_latent or armed):
        print("\nLatent — not pull_request-triggered today, armed the moment that changes:")
        for f in latent:
            print(f)
    if armed or (latent and args.include_latent):
        print("\nSee the module docstring for the three accepted fixes.")
        return 1

    if not findings:
        print("OK — no service or job container takes its credentials from a secrets expression.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
