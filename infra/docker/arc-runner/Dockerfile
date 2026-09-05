# MADFAM ARC Runner Image
#
# Thin overlay on the official GitHub Actions runner image.
# Adds the system packages our CI jobs require — primarily Playwright
# browser dependencies and pnpm's libatomic dependency — that are
# present in github-hosted ubuntu-latest but missing from the upstream
# self-hosted runner image.
#
# Since 2026-09-05 it also bakes the commons RENDER environment (G16):
# libGL/X, Liberation fonts, and the OpenSCAD 2026.02.01 snapshot,
# mirroring yantra4d's platform image so cartridges render in CI the way
# they render in production instead of apt-installing per job.
#
# Failure mode this fixes: every UI Tests + Playwright job across the
# ecosystem fails on `madfam-runners-blue` because libatomic1 (pnpm /
# Node native modules) and libasound2t64 + libnss3 + libxkbcommon0 +
# libgbm1 (Chromium for Playwright) are not in the stock runner image.
#
# Rebuild cadence and base-image bump policy: see README.md.

# Pinned to a specific runner version, never :latest.
# Bump deliberately — see internal-devops/runbooks/arc-runner-image-rebuild.md.
#
# This pin is load-bearing for org-wide CI availability, not just for
# package freshness. GitHub rejects runner agents that lag the latest
# release by ~60 days *at the registration layer*: the agent starts,
# fails to register, and ARC respawns it forever. The pool reports
# runners as CURRENT while RUNNING stays 0 and every job sits queued.
# 2.334.0 (2026-04-21) went past that window and stalled the whole
# ecosystem on 2026-08-10 — 3,149 EphemeralRunners, all "Outdated /
# Runner is deprecated", zero jobs served. Honour the 30-day bump
# cadence in the runbook; the deprecation is silent until it isn't.
#
# 2026-09-04 bump: 2.336.0 (published 2026-07-20) → 2.337.0 (published
# 2026-08-27), the newest upstream release at the time of writing. 2.336.0
# was 46 days old and the runbook's next-bump date was 2026-09-19, i.e.
# inside the window that ends in a registration outage.
#
# BUMPING THIS LINE DOES NOT DEPLOY ANYTHING. The pools run a digest pin
# (Kyverno `require-image-digest`), so the new agent only reaches runners
# once the overlay digest produced by .github/workflows/arc-runner-image.yml
# on merge is repinned in BOTH image references of BOTH rendered scale sets,
# infra/k8s/production/arc/runner-blue/rendered.yaml and
# infra/k8s/production/arc/runner-deploy/rendered.yaml. That repin is a
# second PR, by design — the digest does not exist until this one merges.
#
# BASE_TAG_DATE is the upstream publish date of BASE_TAG (the `created`
# timestamp on the base image's linux/amd64 config blob). It is not
# decoration: `scripts/check-image-age.py` reads it, and it is the ONLY
# thing standing between this pin and a silent 111-day drift when the
# registry is unreachable from CI. Update it in the same edit as BASE_TAG —
# the ratchet fails the build if the two disagree.
ARG BASE_IMAGE=ghcr.io/actions/actions-runner
ARG BASE_TAG=2.337.0
ARG BASE_TAG_DATE=2026-08-27
FROM ${BASE_IMAGE}:${BASE_TAG}
ARG BASE_IMAGE
ARG BASE_TAG
ARG BASE_TAG_DATE

# Switch to root for apt operations; the upstream image switches back to
# the unprivileged `runner` user via its ENTRYPOINT.
USER root

# Minimum package set, justified per package:
#   libatomic1        - pnpm + Node native addons crash on missing __atomic_*
#   libasound2t64     - Playwright Chromium audio (Ubuntu 24.04 t64 transition)
#   libasound2-data   - companion data package for libasound2t64
#   libnss3           - Playwright Chromium TLS / cert verification
#   libxkbcommon0     - Playwright Chromium keyboard input
#   libgbm1           - Playwright Chromium GPU buffer manager (headless)
#   jq                - ubiquitous in our CI scripts (status checks, JSON munging)
#   python3-pip       - some workflows use python tooling (uv, pre-commit hooks)
#   python3-venv      - `python3 -m venv` (ensurepip) fails without it on the
#                       Ubuntu base; present in github-hosted ubuntu-latest.
#                       Concrete failure: rondelio's studio creation-suite
#                       smoke hard-failed creating .venv-studio-smoke on
#                       fresh blue pods (deploy run 31035125262, 2026-08-05)
#                       and the workflow's apt-get fallback cannot sudo here.
#   xz-utils          - actions/setup-node extracts upstream Node tar.xz payloads
#   ca-certificates   - explicit so cert bundle is fresh post-base-image bump
#
# Intentionally NOT installed (kept minimal):
#   - chromium / firefox themselves (Playwright manages browsers via its own download)
#   - docker / buildkit (DinD is disabled in our runner config)
#   - language toolchains (Node, Go, etc. — installed per-job via setup-* actions;
#     keep xz-utils here because setup-node needs it before Node exists)
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      libatomic1 \
      libasound2t64 \
      libasound2-data \
      libnss3 \
      libxkbcommon0 \
      libgbm1 \
      jq \
      python3-pip \
      python3-venv \
      xz-utils \
      ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Sanity-check the additions at build time so a bad base-image bump
# fails the GHA build instead of breaking every PR in the org.
RUN ldconfig -p | grep -q libatomic.so.1 \
 && ldconfig -p | grep -q libasound.so.2 \
 && ldconfig -p | grep -q libnss3.so \
 && ldconfig -p | grep -q libxkbcommon.so.0 \
 && ldconfig -p | grep -q libgbm.so.1 \
 && command -v jq >/dev/null \
 && command -v pip3 >/dev/null \
 && command -v xz >/dev/null \
 && python3 -m venv /tmp/venv-sanity \
 && /tmp/venv-sanity/bin/python -m pip --version >/dev/null \
 && rm -rf /tmp/venv-sanity

# ---------------------------------------------------------------------------
# Render environment (G16) — libGL/X, fonts, and OpenSCAD.
#
# Why this is baked in rather than apt-installed per job: commons render jobs
# (yantra4d cartridges, hyperobject exports) used to `sudo apt-get install`
# libGL + openscad at the top of every run. On these pods that step regularly
# hit the workflows' own 300 s apt cap — a bounded timeout added after an
# unreachable git-core PPA hung an e2e shard for 28 minutes on 2026-09-02 —
# and the whole job died before a single model rendered. Baking the packages
# turns that step into a no-op.
#
# The list below MIRRORS yantra4d apps/api/Dockerfile (the platform image the
# renders actually run on in production) plus the OCP runtime libraries its
# CI installs separately. "Renders in CI" only means something if CI renders
# with the same libraries and the same OpenSCAD as the platform, so this pair
# must move together.
#
# CONTRACT SOURCE: this list is being lifted into hyperobjects-spec as
# `y4d_spec.render_environment` (`APT_PACKAGES`, `OPENSCAD_VERSION`,
# `OPENSCAD_SHA256`) by lane L-G31. That spec had not landed when this image
# was written, so the values here are copied from yantra4d's Dockerfile. The
# NEXT bump of this block must read them from the spec instead of re-copying,
# and drop this paragraph when it does.
#
# Per package:
#   libgl1            - OCP (CadQuery's kernel) dlopens libGL.so.1 at import;
#                       without it cq_runner subprocesses die with
#                       "CadQuery is not installed (libGL.so.1: cannot open
#                       shared object file)". Same for OpenSCAD's renderer.
#   libglu1-mesa      - GLU, pulled in by the same OpenGL stack
#   libglib2.0-0t64   - GLib, linked by the OpenSCAD AppRun payload
#                       (t64 name on the Ubuntu 24.04 base)
#   libharfbuzz0b     - text shaping for OpenSCAD's text() primitive
#   libegl1           - EGL entry points the AppImage payload links against
#   libwayland-client0- ditto (Qt platform plugins resolve it at load)
#   libxrender1       - X render extension; X libs below are needed even
#   libxcursor1         headless because Qt/OCC link them unconditionally,
#   libxft2             which is why the render jobs' apt step listed them
#   libxinerama1
#   libxext6
#   fonts-liberation  - the metric-compatible font family cartridges label
#                       parts with; a missing font silently renders boxes
#   fontconfig        - fc-cache/fc-list, so installed fonts are findable
#   xvfb              - virtual framebuffer for OpenSCAD runs that still want
#                       a display; mirrors the platform image
#
# Kept in its own RUN layer, after the CI-deps layer above, so a package
# change here does not invalidate that layer and vice versa.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      libgl1 \
      libglu1-mesa \
      libglib2.0-0t64 \
      libharfbuzz0b \
      libegl1 \
      libwayland-client0 \
      libxrender1 \
      libxcursor1 \
      libxft2 \
      libxinerama1 \
      libxext6 \
      fonts-liberation \
      fontconfig \
      xvfb \
 && fc-cache -f \
 && rm -rf /var/lib/apt/lists/*

# OpenSCAD snapshot — MUST match yantra4d apps/api/Dockerfile exactly (same
# version, same hash, same extraction), so a cartridge that renders here
# renders identically on the platform. The 2026+ snapshot is required for
# Gridfinity extended syntax; the Ubuntu archive's openscad is older and
# would silently render different geometry.
#
# AppImages need FUSE, which containers do not have, so the image is
# extracted at build time and the AppRun binary linked onto PATH — the same
# trick the platform image uses.
#
# The `chmod -R a+rX` is not decoration. Extraction runs as root here but the
# pool executes jobs as the unprivileged `runner` user, and a squashfs payload
# with a root-only directory bit turns into "openscad: Permission denied" on
# every render job while the build itself stays green. yantra4d's image does
# not need this because it runs OpenSCAD as the same user that extracted it.
#
# To bump: download the AppImage, `sha256sum` it, update both ARGs here AND
# in yantra4d (or, once L-G31 lands, in hyperobjects-spec, which both read).
ARG OPENSCAD_VERSION=2026.02.01
ARG OPENSCAD_SHA256=dad3a8d19e63c543129eabef314676ac9348ffcab727544f4283b2ae9000277c
# curl, not wget: the upstream base ships curl (and jq/unzip) but NOT wget,
# and adding a package just to fetch one file is surface area we do not need.
# `--fail` matters — without it curl writes the 404 body to the output file
# and the sha256 check becomes the only thing catching a moved artifact.
RUN curl -fsSL --retry 3 --retry-delay 5 \
      "https://files.openscad.org/snapshots/OpenSCAD-${OPENSCAD_VERSION}-x86_64.AppImage" \
      -o /tmp/openscad.AppImage \
 && echo "${OPENSCAD_SHA256}  /tmp/openscad.AppImage" | sha256sum -c - \
 && chmod +x /tmp/openscad.AppImage \
 && cd /opt && /tmp/openscad.AppImage --appimage-extract \
 && rm /tmp/openscad.AppImage \
 && ln -sf /opt/squashfs-root/AppRun /usr/local/bin/openscad \
 && chmod -R a+rX /opt/squashfs-root

# Sanity-check the render environment the same way the CI-deps block above is
# checked: a bad base bump or a moved upstream artifact fails THIS build
# rather than every commons render job on the pool.
#
# `openscad --version` writes to stderr, hence the 2>&1. It is run under
# xvfb-run because OpenSCAD is a Qt application: on a build host with no
# DISPLAY, some builds abort before printing anything. xvfb-run costs one
# second here and removes a whole class of "works on my machine".
RUN ldconfig -p | grep -q libGL.so.1 \
 && ldconfig -p | grep -q libGLU.so.1 \
 && fc-list | grep -qi liberation \
 && python3 -c "import ctypes; ctypes.CDLL('libGL.so.1')" \
 && xvfb-run -a openscad --version 2>&1 | grep -q "${OPENSCAD_VERSION}"

# Drop back to the runner user the upstream image expects.
USER runner

# Image metadata (consumed by GHCR + provenance tooling).
LABEL org.opencontainers.image.source="https://github.com/madfam-org/enclii"
LABEL org.opencontainers.image.description="MADFAM hardened ARC runner image — adds Playwright + pnpm system deps and the commons render environment (libGL/X, Liberation fonts, OpenSCAD 2026.02.01) to the upstream actions/actions-runner base."
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
LABEL org.opencontainers.image.title="enclii arc-runner"
LABEL enclii.dev/component="arc-runner"
LABEL enclii.dev/base-image="${BASE_IMAGE}:${BASE_TAG}"
LABEL enclii.dev/base-image-published="${BASE_TAG_DATE}"
