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
- `python3-venv` — the base's system Python cannot create venvs without
  it (`ensurepip` lives in this package on Ubuntu). Workflows that run
  `python3 -m venv` against system Python hard-fail; apt-get fallbacks
  inside runner pods cannot sudo. Observed: rondelio's studio
  creation-suite smoke, deploy run 31035125262 (2026-08-05).

Result: every UI Tests / Playwright job across the org fails. Branch
protection becomes meaningless because every PR has red CI.

This image fixes that with the smallest possible diff against the
upstream base.

## The render environment (G16)

Since 2026-09-05 the image also carries everything a commons **render**
job needs — OpenSCAD plus the OpenGL/X/font stack it and CadQuery's OCP
kernel link against.

**Why it is baked in.** Commons render jobs used to `sudo apt-get install`
`libgl1 libglu1-mesa libxrender1 …` and `openscad` at the top of every run.
Those apt steps are bounded by a 300 s `timeout` (added after an unreachable
`git-core` PPA hung a yantra4d e2e shard for 28 minutes on 2026-09-02), and on
these pods they regularly hit that cap — so the job died before rendering a
single model. With the packages in the image, that step is a no-op: it can be
deleted from the workflows, or left in place, where apt finds everything
already installed and returns immediately.

**Why these exact versions.** The list mirrors yantra4d's platform image,
`apps/api/Dockerfile`, package for package, plus the OCP runtime libraries
that repo's CI installs separately. "It renders in CI" is only a meaningful
statement if CI renders with the same libraries and the same OpenSCAD binary
as production does. In particular the OpenSCAD **2026.02.13 snapshot** is
pinned by SHA-256 and installed from `files.openscad.org`, not from the Ubuntu
archive — the archive's `openscad` is older and silently renders *different
geometry* for Gridfinity extended syntax.

**Why 2026.02.13** (bumped 2026-09-05, rulings G31/G35 — with a correction).
The bump was made when yantra4d PR #125's backend job hit
`Assertion '(is_list($tags_shown) || ($tags_shown == "ALL"))'` in BOSL2's
`attachments.scad` on the first CI runner that ever had an `openscad` binary,
and the same file rendered on macOS 2026.02.13. **That reading was wrong**: the
platform's smoke test loaded BOSL2 with `use <BOSL2/std.scad>`, and `use` never
applies a used file's top-level assignments — BOSL2's `$tags_shown = "ALL"`
default among them — so every attachable primitive fails BOSL2's own assertion
on *any* OpenSCAD version. BOSL2 documents `include`-only; every commons
cartridge does `include <BOSL2/std.scad>`; the test was fixed to match
(yantra4d #126). Whether 2026.02.01 could render BOSL2 v2.0.753 was therefore
never actually tested and is not claimed here.

The 2026.02.13 pin **stays**, for the reason that matters: the runner image,
`yantra4d/apps/api/Dockerfile` (+ `Dockerfile.dev`) and the keystone's
`y4d_spec.render_environment` contract now all name one snapshot that this
image's build smoke has rendered BOSL2 with (`include <BOSL2/std.scad>` +
`cube([1,1,1], anchor=[-1,-1,-1]);` → a 1443-byte STL, byte-identical to
macOS). The smoke step *renders* rather than inspecting because the three
presence checks that preceded it all passed on an image that could not have
rendered a `use`-loaded BOSL2 either way — presence cannot see a
library/renderer mismatch. The contract source for the next bump is
`y4d_spec.render_environment`.

AppImages need FUSE, which containers do not have, so the AppImage is
extracted at build time (`--appimage-extract`) and `AppRun` is symlinked to
`/usr/local/bin/openscad`, exactly as the platform image does.

What the image provides:

| Component | Value |
|---|---|
| OpenSCAD | snapshot `2026.02.13`, sha256-verified, at `/usr/local/bin/openscad` |
| OpenGL / EGL | `libgl1`, `libglu1-mesa`, `libegl1` |
| X / Qt link deps | `libxrender1`, `libxcursor1`, `libxft2`, `libxinerama1`, `libxext6`, `libwayland-client0` |
| Text / fonts | `fonts-liberation`, `fontconfig` (`fc-cache -f` run at build), `libharfbuzz0b` |
| Headless display | `xvfb` |
| GLib | `libglib2.0-0t64` |

**Contract source — now enforced, not merely promised.** The list lives in
`hyperobjects-spec` as `y4d_spec.render_environment` (`APT_PACKAGES`,
`CI_EXTRA_APT_PACKAGES`, `OPENSCAD_VERSION`, `OPENSCAD_SHA256`) so the platform
image, this image and the render jobs read one definition. That contract has
landed, and since #520 a CI lane holds this `Dockerfile` to it:
[`arc-runner-render-env-drift.yml`](../../../.github/workflows/arc-runner-render-env-drift.yml)
installs the spec and fails a PR whose values here have drifted.

Order of operations, and it is not optional: **bump the spec first, re-pin it in
that workflow, then edit this block.**

### The drift check (#520)

`scripts/check-arc-runner-render-env.py` mirrors yantra4d's
`scripts/qa/check_render_env.py` — same `ARG` regex, same literal apt parser,
same build-only ignore set (`wget`, `curl`, `ca-certificates`) — with two
deliberate differences, both covered by
`tests/scripts/test_check_arc_runner_render_env.py`:

1. **It reads every `apt-get install`, not the first.** The platform image
   installs in one `RUN`; this `Dockerfile` deliberately uses two layers so a
   change in one does not invalidate the other. A first-match parser would read
   only the CI-deps layer and report every render package as missing.
2. **Packages are compared as a subset, not as equal sets.** This is a CI
   runner: it legitimately also carries Playwright deps, `jq`, `python3-venv`
   and extra X libraries. A package the image has and the contract does not is
   the runner being a runner. A package **the contract requires and the image
   lacks** is drift — and that is the direction that breaks renders.

`CI_EXTRA_APT_PACKAGES` counts as required here, because this machine runs the
`[geometry]` extra's OCP kernel. Ubuntu 24.04's 64-bit `time_t` rename is not
drift: `libglib2.0-0t64` satisfies the contract's `libglib2.0-0`, as one narrow
named rule rather than a fuzzy match.

**Why the build's own smoke check is not this check.** `arc-runner-image.yml`
verifies the built image **against itself** — that OpenSCAD runs and reports the
version the same `Dockerfile` pinned. A version string agreeing with itself
proves nothing about whether it agrees with the commons. This drift is silent by
construction: the image keeps building, the smoke check keeps passing, and the
first symptom is geometry that differs between CI and production — the one class
of bug the dual-engine parity lane cannot catch, because both sides of that
comparison run in the same image.

**It found real drift on the run that introduced it.** `libcomerr2` and
`libgpg-error0` are in the platform image and in the contract and were missing
here; pool renders had been running against a different library set than
production since G16. Both are in the image as of #520, and the pools were
repinned to the resulting build in #521.

The drift lane runs on GitHub-hosted `ubuntu-latest`, for the same
chicken-and-egg reason `arc-runner-image.yml` does: it is about the pool's own
image and must not depend on the pool being healthy.

**Size.** Measured on the 2026-09-05 build: **1971 MiB** total uncompressed,
against a **1470 MiB** upstream base — so the whole overlay (the pre-existing
CI packages plus this render environment) is **501 MiB**, with the extracted
OpenSCAD squashfs the largest single contributor (~84 MiB download, a few
hundred MiB on disk).

The repo has no image-size budget for this image. Instead the build prints
the total *and* the delta against the base to the job summary on every run —
the base reference is read from the `Dockerfile`'s own `ARG`s so it cannot
drift from the pin — which is what makes an unexpected jump visible in review.

The runner pods' 6 GiB memory limit is unaffected: this is disk/registry, not
RSS, and the pods pull by digest onto a warm node cache.

**Smoke check.** Every build of this image runs the final artifact through
`docker run` and asserts four things, each mapping to a failure mode that
does not announce itself:

1. `openscad --version` reports exactly `2026.02.13` — "openscad exists" is
   not enough, since the wrong version renders different geometry.
2. `fc-list` matches `liberation` — a missing font does not error, it renders
   boxes, and the part ships with unreadable labels.
3. `python3 -c "import ctypes; ctypes.CDLL('libGL.so.1')"` succeeds — this is
   precisely how OCP fails, at import, behind the misleading message
   "CadQuery is not installed".
4. **A BOSL2 part actually renders** (added with the G31 bump): the workflow
   clones BOSL2 at the platform's pinned `fcfce7c7`, mounts it read-only with
   `OPENSCADPATH`, renders the two-line file above, and requires exit 0 *and*
   a non-trivial STL. Checks 1–3 all passed on the 2026.02.01 image that could
   not render a single anchored BOSL2 primitive — version strings and library
   presence cannot see a semantic incompatibility, only a render can.

The `Dockerfile` asserts the same things at build time, but that layer runs as
`root`; the `docker run` check inherits the image's `USER runner`, which is
the only place a root-only squashfs tree would be caught.

## Image coordinates

- Registry: `ghcr.io/madfam-org/enclii/arc-runner`
- Tags published per build:
  - `:<git-sha>` — immutable, used for provenance and pre-merge testing
  - `:stable` — moved to point at the latest `main` build
- Base image (pinned in [`Dockerfile`](./Dockerfile)):
  `ghcr.io/actions/actions-runner:2.337.0` (upstream release published
  2026-08-27). The `Dockerfile` is the source of truth for this value; this
  line and the comment in `infra/helm/arc/values-runner-set.yaml` track it.

**What the pools run today.** Both `madfam-runners-blue` and
`madfam-runners-deploy` are pinned to
`…/arc-runner:stable@sha256:c35966ed277acedf16953c1e8075a2c9d92509be488b4de48b9487605da5a162`
(#521 — the image built from #520). The pin is the **manifest list** digest from
the build log's `pushing manifest for …` line, never the single-manifest digest
the run summary prints; the two differ and only the list digest is what a node
resolves. See
[`infra/k8s/production/arc/README.md`](../../k8s/production/arc/README.md) for
the four references that must move together.

## Rebuild cadence

Rebuild happens automatically when this directory changes, via
[`.github/workflows/arc-runner-image.yml`](../../../.github/workflows/arc-runner-image.yml).

In addition, schedule a deliberate bump in two cases:

1. **Quarterly cadence (every ~90 days).** Picks up Ubuntu base image
   security patches transparently — `apt-get update` in the build runs
   on the upstream cache state at build time.
2. **When `actions/runner` ships a new release.** Track
   <https://github.com/actions/runner/releases>. Bump the `BASE_TAG` **and
   `BASE_TAG_DATE`** build args in the `Dockerfile`, open a PR, and follow
   the verification procedure in
   [`internal-devops/runbooks/arc-runner-image-rebuild.md`](https://github.com/madfam-org/internal-devops/blob/main/runbooks/arc-runner-image-rebuild.md).

This cadence is **enforced in CI**, not merely documented. The `Image Age
Ratchet` job (`scripts/check-image-age.py`) fails the build when:

- a newer upstream release has been published for more than 30 days;
- `BASE_TAG_DATE` disagrees with the tag's actual publish date in the
  registry (a date field that drifts from reality is worse than none);
- upstream is unreachable *and* `BASE_TAG_DATE` is more than 55 days old —
  inside GitHub's ~60-day deprecation window.

Between a release and our bump the job warns rather than fails, which is the
30 days the cadence allows. `AGE_RATCHET_EXEMPT_ACTIONS_RUNNER=<reason>` in
the job's env downgrades the failure to a warning; use it only while a bump
PR is open, and delete it in that PR.

Remember that bumping the tag deploys nothing on its own: the pools run a
digest pin, so the overlay digest must be repinned after the image build runs
on `main` — in every reference of both rendered scale sets,
`infra/k8s/production/arc/runner-blue/rendered.yaml` and
`infra/k8s/production/arc/runner-deploy/rendered.yaml` (two references each:
the `init-dind-externals` init container and the `runner` container).

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

**Render packages are different**: they are not a free choice. They must match
the platform image (see [The render environment](#the-render-environment-g16)),
so add them upstream first — in `hyperobjects-spec`'s
`y4d_spec.render_environment`, which has landed and is now enforced — then
re-pin the spec in the drift workflow and mirror the change here. A render
package added here alone does not merely risk drift: the drift gate fails the
PR. An extra package here that production does not have means CI renders
something the platform cannot.

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

# Sanity-check the render environment (G16) — the same presence assertions the
# image build workflow gates on. Note this runs as `runner`, not root.
docker run --rm --entrypoint bash arc-runner:dev -c '
  xvfb-run -a openscad --version 2>&1 | grep 2026.02.13
  fc-list | grep -i liberation | head -3
  python3 -c "import ctypes; ctypes.CDLL(\"libGL.so.1\"); print(\"libGL OK\")"
'

# Render a real BOSL2 part (G31) — the check that would have caught the
# 2026.02.01 / BOSL2 v2.0.753 incompatibility. Expect exit 0 and ~1.4 KB.
mkdir -p /tmp/oscad-libs /tmp/oscad-smoke
git clone --filter=blob:none --no-checkout \
  https://github.com/BelfrySCAD/BOSL2 /tmp/oscad-libs/BOSL2
# Full sha: `git fetch <remote> <sha>` resolves its argument as a ref on the
# SERVER, which cannot expand an abbreviation ("couldn't find remote ref").
git -C /tmp/oscad-libs/BOSL2 fetch --depth 1 origin \
  fcfce7c763863d8e66d5f36a551d11129ec1a607
git -C /tmp/oscad-libs/BOSL2 checkout FETCH_HEAD
printf '%s\n' 'include <BOSL2/std.scad>' 'cube([1,1,1], anchor=[-1,-1,-1]);' \
  > /tmp/oscad-smoke/smoke.scad
chmod 777 /tmp/oscad-smoke
docker run --rm \
  -v /tmp/oscad-libs:/opt/oscad-libs:ro -v /tmp/oscad-smoke:/work \
  -e OPENSCADPATH=/opt/oscad-libs --entrypoint bash arc-runner:dev -c \
  'cd /work && xvfb-run -a openscad -o /work/smoke.stl smoke.scad'
wc -c /tmp/oscad-smoke/smoke.stl
```

## Operational links

- Helm values that consume the image:
  [`infra/helm/arc/values-runner-set.yaml`](../../helm/arc/values-runner-set.yaml)
- ArgoCD app reconciling the runner scale sets:
  [`infra/argocd/apps/arc-runners.yaml`](../../argocd/apps/arc-runners.yaml)
- Rebuild + rollback runbook:
  `internal-devops/runbooks/arc-runner-image-rebuild.md`
