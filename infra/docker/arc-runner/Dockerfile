# MADFAM ARC Runner Image
#
# Thin overlay on the official GitHub Actions runner image.
# Adds the system packages our CI jobs require — primarily Playwright
# browser dependencies and pnpm's libatomic dependency — that are
# present in github-hosted ubuntu-latest but missing from the upstream
# self-hosted runner image.
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
ARG BASE_IMAGE=ghcr.io/actions/actions-runner
ARG BASE_TAG=2.336.0
FROM ${BASE_IMAGE}:${BASE_TAG}
ARG BASE_IMAGE
ARG BASE_TAG

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

# Drop back to the runner user the upstream image expects.
USER runner

# Image metadata (consumed by GHCR + provenance tooling).
LABEL org.opencontainers.image.source="https://github.com/madfam-org/enclii"
LABEL org.opencontainers.image.description="MADFAM hardened ARC runner image — adds Playwright + pnpm system deps to the upstream actions/actions-runner base."
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
LABEL org.opencontainers.image.title="enclii arc-runner"
LABEL enclii.dev/component="arc-runner"
LABEL enclii.dev/base-image="${BASE_IMAGE}:${BASE_TAG}"
