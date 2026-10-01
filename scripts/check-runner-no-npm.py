#!/usr/bin/env python3
"""
check-runner-no-npm.py — the published Node.js runner images carry no npm CLI.

WHY THIS EXISTS
===============
On 2026-09-30 the next 16.3.8 rollout (GHSA-vcvr-r3jv-pc5j, enclii #659/#660)
was blocked by the per-service Trivy HIGH/CRITICAL gate on the dispatch
(apps/admin-console) and switchyard-ui images. All three findings —
brace-expansion 5.0.9 (CVE-2026-102276, CVE-2026-102278) and undici 6.28.0
(CVE-2026-19534) — lived only inside npm's OWN vendored dependency tree at
/usr/local/lib/node_modules/npm/node_modules. No npm release bundled the fixed
versions, so no npm pin could clear the gate.

The runners never call npm: CMD is `node server.js` and the HEALTHCHECK uses
wget. So #660 made the runner stage of status, dispatch and switchyard-ui
delete npm and npx outright. This check keeps it that way: a refactor that
drops the `rm -rf` line, or a new CMD/HEALTHCHECK that shells out to npm/npx,
fails CI here instead of failing Trivy (or crashing at runtime) after merge.

WHAT IT CHECKS
==============
For every service in services.json, it resolves the stage that is actually
published (`target`, else the Dockerfile's final stage) and follows its
`FROM <stage>` chain back to an external base image. If that base is a
`node:` image, the chain must:

  1. contain a RUN that removes all of /usr/local/lib/node_modules/npm,
     /usr/local/bin/npm and /usr/local/bin/npx; and
  2. never invoke `npm` or `npx` (RUN, CMD, ENTRYPOINT, HEALTHCHECK) after
     that removal — the command would not exist at runtime.

Services whose published stage is not Node-based (Go/alpine, nginx) are
skipped and listed as such.

ADDING npm BACK TO A RUNTIME
============================
Prefer not to: run the tool in a build stage and COPY its output instead. If a
runtime genuinely needs npm, (a) delete the npm removal from that runner stage,
(b) add the service to NPM_ALLOWED below with the reason, and (c) expect the
Trivy gate to scan npm's vendored tree again — pin an npm in the `base` stage
whose bundled dependencies are clean (`npm view npm@<ver> dist.tarball`, then
inspect package/node_modules/) before merging.

USAGE
=====
    python3 scripts/check-runner-no-npm.py [--root DIR]

Exit codes:
  0 — every Node runner image deletes npm/npx and never calls them afterwards
  1 — at least one finding
  2 — could not read services.json or a Dockerfile
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path

#: Services allowed to keep npm in their published image, with the reason.
#: Empty on purpose — see "ADDING npm BACK TO A RUNTIME" above.
NPM_ALLOWED: dict[str, str] = {}

NPM_PATHS = (
    "/usr/local/lib/node_modules/npm",
    "/usr/local/bin/npm",
    "/usr/local/bin/npx",
)

#: `npm`/`npx` as a command word. Excludes paths (/usr/local/bin/npm),
#: dotfiles (.npmrc), pnpm, and identifiers such as npm_config_*.
NPM_INVOCATION = re.compile(r"(?<![\w./-])(npm|npx)(?![\w./-])")

#: An external base image that is Node.js: `node:22-alpine`,
#: `public.ecr.aws/docker/library/node:22-alpine`, `node@sha256:...`.
NODE_IMAGE = re.compile(r"(^|/)node[:@]")

EXECUTING = {"RUN", "CMD", "ENTRYPOINT", "HEALTHCHECK"}


@dataclass
class Instruction:
    keyword: str
    args: str
    line: int


@dataclass
class Stage:
    name: str | None
    base: str
    line: int
    instructions: list[Instruction] = field(default_factory=list)


def parse_instructions(text: str) -> list[Instruction]:
    """Join backslash continuations and drop comment lines, Docker-style."""
    out: list[Instruction] = []
    buf: list[str] = []
    start = 0
    for lineno, raw in enumerate(text.splitlines(), start=1):
        stripped = raw.strip()
        if stripped.startswith("#"):
            continue  # comments are removed even inside a continuation
        if not buf:
            if not stripped:
                continue
            start = lineno
        if stripped.endswith("\\"):
            buf.append(stripped[:-1])
            continue
        buf.append(stripped)
        joined = " ".join(part.strip() for part in buf).strip()
        buf = []
        if not joined:
            continue
        keyword, _, args = joined.partition(" ")
        out.append(Instruction(keyword.upper(), args.strip(), start))
    if buf:
        joined = " ".join(part.strip() for part in buf).strip()
        keyword, _, args = joined.partition(" ")
        out.append(Instruction(keyword.upper(), args.strip(), start))
    return out


def parse_stages(text: str) -> list[Stage]:
    stages: list[Stage] = []
    for ins in parse_instructions(text):
        if ins.keyword == "FROM":
            tokens = [t for t in ins.args.split() if not t.startswith("--")]
            base = tokens[0] if tokens else ""
            name = None
            if len(tokens) >= 3 and tokens[1].upper() == "AS":
                name = tokens[2]
            stages.append(Stage(name=name, base=base, line=ins.line))
        elif stages:
            stages[-1].instructions.append(ins)
    return stages


def stage_chain(stages: list[Stage], target: str | None) -> list[Stage]:
    """The published stage and the stages it inherits from, root first."""
    by_name = {s.name.lower(): s for s in stages if s.name}
    if target:
        current = by_name.get(target.lower())
        if current is None:
            raise ValueError(f"target stage {target!r} not found")
    else:
        current = stages[-1]
    chain = [current]
    seen = {id(current)}
    while current.base.lower() in by_name:
        current = by_name[current.base.lower()]
        if id(current) in seen:
            raise ValueError(f"stage cycle at {current.name!r}")
        seen.add(id(current))
        chain.append(current)
    return list(reversed(chain))


def removes_npm(args: str) -> bool:
    if not re.search(r"\brm\b", args):
        return False
    return all(re.search(re.escape(p) + r"(?=$|[\s;&|)])", args) for p in NPM_PATHS)


def check_dockerfile(service: str, dockerfile: Path, target: str | None) -> tuple[str, list[str]]:
    """Return (status, findings). status is 'ok', 'skipped' or 'fail'."""
    stages = parse_stages(dockerfile.read_text())
    if not stages:
        raise ValueError(f"{dockerfile}: no FROM instruction")
    chain = stage_chain(stages, target)
    if not NODE_IMAGE.search(chain[0].base):
        return "skipped", []

    flat = [ins for stage in chain for ins in stage.instructions]
    removal = None
    for idx, ins in enumerate(flat):
        if ins.keyword == "RUN" and removes_npm(ins.args):
            removal = idx

    findings: list[str] = []
    published = chain[-1].name or f"final stage (line {chain[-1].line})"
    if removal is None:
        findings.append(
            f"{service}: {dockerfile} stage {published} is Node-based but never deletes "
            f"npm/npx ({', '.join(NPM_PATHS)}); Trivy scans npm's vendored tree"
        )
    else:
        for ins in flat[removal + 1 :]:
            if ins.keyword in EXECUTING and NPM_INVOCATION.search(ins.args):
                findings.append(
                    f"{service}: {dockerfile}:{ins.line} {ins.keyword} calls npm/npx after "
                    "the runner deleted it"
                )
    return ("fail" if findings else "ok"), findings


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--root", default=".", help="repository root holding services.json")
    args = parser.parse_args(argv)
    root = Path(args.root)

    try:
        services = json.loads((root / "services.json").read_text())["services"]
    except (OSError, ValueError, KeyError) as exc:
        print(f"::error::cannot read services.json: {exc}", file=sys.stderr)
        return 2

    findings: list[str] = []
    checked: list[str] = []
    skipped: list[str] = []
    for svc in services:
        name = svc.get("name", "?")
        if name in NPM_ALLOWED:
            skipped.append(f"{name} (allowed: {NPM_ALLOWED[name]})")
            continue
        dockerfile = root / svc.get("dockerfile", "")
        try:
            status, found = check_dockerfile(name, dockerfile, svc.get("target") or None)
        except (OSError, ValueError) as exc:
            print(f"::error::{name}: {exc}", file=sys.stderr)
            return 2
        if status == "skipped":
            skipped.append(name)
        else:
            checked.append(name)
        findings.extend(found)

    for finding in findings:
        print(f"::error::{finding}")
    print(
        f"runner_npm_check={'FAIL' if findings else 'OK'} "
        f"node_runners={len(checked)} ({', '.join(checked) or 'none'}) "
        f"skipped={len(skipped)} ({', '.join(skipped) or 'none'})"
    )
    if not checked and not findings:
        # An empty scope is not a pass: services.json lost every Node runner,
        # or the parser stopped recognising them.
        print("::error::no Node.js runner image found in services.json", file=sys.stderr)
        return 1
    return 1 if findings else 0


if __name__ == "__main__":
    sys.exit(main())
