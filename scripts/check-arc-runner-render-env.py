#!/usr/bin/env python3
"""Cross-check the ARC runner image's render environment against the commons contract.

infra/docker/arc-runner/Dockerfile bakes the commons RENDER environment (G16):
libGL/X, Liberation fonts, and a pinned OpenSCAD snapshot. It does that so a
cartridge rendered on `madfam-runners-blue` renders the way it renders on the
yantra4d platform image. That promise is only worth something while the two
agree, and nothing kept them honest: the values were COPIED from yantra4d's
Dockerfile by hand, and the Dockerfile says so itself —

    CONTRACT SOURCE: this list is being lifted into hyperobjects-spec as
    `y4d_spec.render_environment` (...). That spec had not landed when this
    image was written, so the values here are copied from yantra4d's
    Dockerfile. The NEXT bump of this block must read them from the spec
    instead of re-copying, and drop this paragraph when it does.

The spec has since landed. This is that check.

The drift it catches is silent by construction. The image keeps building, the
smoke check keeps passing (it verifies the image against ITSELF — that OpenSCAD
runs and reports the version the same Dockerfile pinned), and the first symptom
is geometry that differs between CI and production. A version string agreeing
with itself proves nothing about whether it agrees with the commons.

WHAT IS COMPARED
----------------
  OPENSCAD_VERSION / OPENSCAD_SHA256 — against the ARG lines, EXACTLY. A
  version is a version, and a hash that is nearly right is wrong: the
  Dockerfile pipes the ARG straight into `sha256sum -c`, so a mismatch there
  is the difference between a verified download and a different binary.

  APT_PACKAGES (+ CI_EXTRA_APT_PACKAGES) — as a SUBSET, not as equal sets.
  This is where this check deliberately differs from yantra4d's
  scripts/qa/check_render_env.py, which compares platform-image sets both ways.
  This image is not the platform image: it is a CI runner that also carries
  Playwright deps, jq, python3-venv and a handful of X libraries the platform
  does not need. A package the image has and the contract does not is not
  drift — it is the runner being a runner. A package the CONTRACT requires and
  the image LACKS is drift, and it is the direction that breaks renders.

  Build-only packages (wget/curl/ca-certificates) are ignored on both sides,
  exactly as yantra4d's checker does: the contract describes what a render
  environment needs, and whether an image fetches with wget or curl is its own
  business, never drift.

THE t64 RENAME
--------------
Ubuntu 24.04 renamed several libraries for the 64-bit time_t transition, so the
image installs `libglib2.0-0t64` where the contract names `libglib2.0-0`. Those
are the same library; failing on that would be a false positive that teaches
people to ignore this check. A contract package is therefore also satisfied by
its `t64` spelling. The mapping is one narrow, named rule, not a fuzzy match.

WHEN THE SPEC IS OLDER THAN THE CONTRACT
----------------------------------------
`y4d_spec.render_environment` exists from hyperobjects-spec 9c2b341f on, and the
workflow pins the SHA the commons pin (db65cf1e). Should that pin ever move to a
spec without the module, an ImportError of THAT module is not a failure — the
check exits 0 saying "spec too old — check inactive" — so a pin change can never
make this guard red by accident. Any OTHER import error is still an error: a
spec that is present but broken must not read as a spec that is merely old.
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
DOCKERFILE = REPO / "infra" / "docker" / "arc-runner" / "Dockerfile"

# `ARG OPENSCAD_VERSION=<snapshot>` / `ARG OPENSCAD_SHA256=<64 hex>`
ARG_RE = re.compile(r"^\s*ARG\s+([A-Z0-9_]+)\s*=\s*(\S+)\s*$", re.MULTILINE)

# Packages that exist only to BUILD an image (fetch the OpenSCAD AppImage,
# verify it) and never at render time. Ignored on BOTH sides — the same set
# yantra4d's scripts/qa/check_render_env.py ignores.
BUILD_ONLY_PACKAGES = frozenset({"wget", "curl", "ca-certificates"})


def parse_args_block(text: str) -> dict:
    """Every top-level ``ARG NAME=value`` in the Dockerfile."""
    return {name: value for name, value in ARG_RE.findall(text)}


def parse_apt_packages(text: str) -> set:
    """The package names across EVERY ``apt-get install`` in the Dockerfile.

    Mirrors yantra4d's parser token for token — read from the `apt-get install`
    marker to the end of that shell command, keep the bare words, drop flags
    and stop at the `&&` that chains the next command on — with one difference:
    it loops over ALL occurrences instead of the first.

    That difference is load-bearing here. yantra4d's platform image installs its
    render packages in a single RUN; this Dockerfile deliberately uses two — the
    CI-deps layer, then the render layer, "so a package change here does not
    invalidate that layer and vice versa". A first-match parser would read only
    the CI-deps layer and report every render package as missing.

    Deliberately literal rather than a shell parser: a shell parser would be
    more general and less reviewable, and this file's job is to be obviously
    right about two known commands.
    """
    packages = set()
    marker = "apt-get install"
    start = 0
    while True:
        idx = text.find(marker, start)
        if idx == -1:
            return packages
        start = idx + len(marker)
        for raw in text[start:].split("\n"):
            line = raw.strip()
            # `\` continues the install; the first line without one ends it,
            # and so does an `&&` chaining the next command on.
            continues = line.endswith("\\")
            if continues:
                line = line[:-1].strip()
            stop = False
            for token in line.split():
                if token == "&&":
                    stop = True
                    break
                if token.startswith("-"):
                    continue
                packages.add(token)
            if stop or not continues:
                break


def satisfies(required: str, installed: set) -> bool:
    """Is a contract package present, allowing for the Ubuntu 24.04 t64 rename?

    `libglib2.0-0` and `libglib2.0-0t64` are the same library under the 64-bit
    time_t transition. Accepting the `t64` spelling is one narrow named rule;
    everything else must match by name.
    """
    return required in installed or f"{required}t64" in installed


def load_spec():
    """The spec's render environment, or None when the pin predates it.

    Returns (module, None) or (None, reason). Only a MISSING
    ``render_environment`` is a reason to stand down; anything else raises.
    """
    try:
        from y4d_spec import render_environment
    except ImportError as exc:
        name = getattr(exc, "name", "") or ""
        if name in ("y4d_spec", "y4d_spec.render_environment"):
            return None, str(exc)
        raise
    return render_environment, None


def compare(spec, dockerfile_text: str) -> list:
    """Every disagreement between the image and the contract, as readable lines."""
    problems = []

    # The runner is a CI machine: it needs the render packages AND the ones the
    # [geometry] extra's OCP kernel wants, which the contract lists separately.
    required = ((set(spec.APT_PACKAGES) | set(spec.CI_EXTRA_APT_PACKAGES))
                - BUILD_ONLY_PACKAGES)
    installed = parse_apt_packages(dockerfile_text) - BUILD_ONLY_PACKAGES

    missing = sorted(p for p in required if not satisfies(p, installed))
    if missing:
        problems.append(
            "apt packages the render contract requires but "
            "infra/docker/arc-runner/Dockerfile does not install: "
            + ", ".join(missing))

    args = parse_args_block(dockerfile_text)
    for arg in ("OPENSCAD_VERSION", "OPENSCAD_SHA256"):
        image_value = args.get(arg)
        spec_value = getattr(spec, arg)
        if image_value is None:
            problems.append(
                f"infra/docker/arc-runner/Dockerfile declares no ARG {arg}")
        elif image_value != spec_value:
            problems.append(
                f"{arg} differs — contract {spec_value!r}, "
                f"infra/docker/arc-runner/Dockerfile {image_value!r}")

    return problems


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(
        description="Check infra/docker/arc-runner/Dockerfile's render "
                    "environment against y4d_spec.render_environment.")
    ap.add_argument("--dockerfile", type=Path, default=DOCKERFILE,
                    help="Dockerfile to read "
                         "(default: infra/docker/arc-runner/Dockerfile)")
    args = ap.parse_args(argv)

    spec, inactive = load_spec()
    if spec is None:
        print(f"check-arc-runner-render-env: spec too old — check inactive "
              f"(no y4d_spec.render_environment: {inactive}). "
              f"It goes live with the next spec pin bump.")
        return 0

    if not args.dockerfile.is_file():
        print(f"check-arc-runner-render-env: FAIL — {args.dockerfile} not found")
        return 1

    problems = compare(spec, args.dockerfile.read_text(encoding="utf-8"))
    print(f"check-arc-runner-render-env: comparing {args.dockerfile} against "
          f"y4d_spec.render_environment (OpenSCAD {spec.OPENSCAD_VERSION}) — "
          f"mismatches={len(problems)}")
    for p in problems:
        print(f"  FAIL {p}")
    if problems:
        print("  The runner image and the commons contract must describe the "
              "same render environment, or a cartridge renders differently in "
              "CI than in production. Update whichever is wrong; if the image "
              "moved deliberately, bump hyperobjects-spec and re-pin it in "
              "the workflow.")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
