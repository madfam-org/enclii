# MADFAM ARC Runner Image

A thin overlay on `ghcr.io/actions/actions-runner` that adds the system
packages our CI jobs need but that the upstream image omits.

## Why this exists

The `madfam-runners-blue` ARC pool runs the stock GitHub Actions runner
image, which is intentionally lean. It does **not** include:

- `libatomic1` — required by pnpm and Node native addons; missing it
  causes `__atomic_*` symbol-load failures the moment any JS toolchain
  starts.
- `libasound2t64`, `libnss3`, `libxkbcommon0`, `libgbm1` — required by
  Chromium, which Playwright launches headless. Playwright downloads
  the browser binary itself, but it cannot supply OS shared libraries.
- `xz-utils` — required before any per-job Node install exists because
  `actions/setup-node` unpacks upstream Node Linux archives. Without it,
  jobs can reach `pnpm` with `node: not found` even though setup-node ran.

Result: every UI Tests / Playwright job across the org fails. Branch
protection becomes meaningless because every PR has red CI.

This image fixes that with the smallest possible diff against the
upstream base.

## Image coordinates

- Registry: `ghcr.io/madfam-org/enclii/arc-runner`
- Tags published per build:
  - `:<git-sha>` — immutable, used for provenance and pre-merge testing
  - `:stable` — moved to point at the latest `main` build
- Base image (pinned in [`Dockerfile`](./Dockerfile)):
  `ghcr.io/actions/actions-runner:2.334.0`

## Rebuild cadence

Rebuild happens automatically when this directory changes, via
[`.github/workflows/arc-runner-image.yml`](../../../.github/workflows/arc-runner-image.yml).

In addition, schedule a deliberate bump in two cases:

1. **Quarterly cadence (every ~90 days).** Picks up Ubuntu base image
   security patches transparently — `apt-get update` in the build runs
   on the upstream cache state at build time.
2. **When `actions/runner` ships a new release.** Track
   <https://github.com/actions/runner/releases>. Bump the `BASE_TAG`
   build arg in the `Dockerfile`, open a PR, and follow the verification
   procedure in
   [`internal-devops/runbooks/arc-runner-image-rebuild.md`](https://github.com/madfam-org/internal-devops/blob/main/runbooks/arc-runner-image-rebuild.md).

## Base-image bump policy

- **Never** use `:latest` for the base image. ARC controller behavior
  is sensitive to the exact runner agent version and our Kyverno
  `disallow-latest-tags` policy will reject it anyway.
- Pin to a `MAJOR.MINOR.PATCH` tag from the official
  `ghcr.io/actions/actions-runner` repo. The runner agent's
  Major/Minor must be compatible with the gha-runner-scale-set
  controller version we deploy (`values-controller.yaml` pins this
  separately).
- Never lag the upstream `actions/runner` agent by more than 60 days
  in production — GitHub deprecates older agents at the registration
  layer.

## Adding a package

Be conservative. Each added package is surface area for CVEs and
build-time minutes. Justify additions in the `Dockerfile` comment block
next to the package list, with a concrete failure mode it fixes.

If you find yourself adding language toolchains (Node, Go, Python
runtimes), reach for the per-job `setup-*` actions instead — those are
versioned per workflow and don't lock the whole org to one toolchain
version.

## Verifying the image locally

```bash
# From repo root.
docker build \
  -t arc-runner:dev \
  infra/docker/arc-runner

# Sanity-check the libraries it claims to add.
docker run --rm --entrypoint bash arc-runner:dev -c '
  ldconfig -p | grep -E "libatomic|libasound|libnss3|libxkbcommon|libgbm"
  jq --version
  pip3 --version
  xz --version
'
```

## Operational links

- Helm values that consume the image:
  [`infra/helm/arc/values-runner-set.yaml`](../../helm/arc/values-runner-set.yaml)
- ArgoCD app reconciling the runner scale sets:
  [`infra/argocd/apps/arc-runners.yaml`](../../argocd/apps/arc-runners.yaml)
- Rebuild + rollback runbook:
  `internal-devops/runbooks/arc-runner-image-rebuild.md`
