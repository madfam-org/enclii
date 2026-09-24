# Reusable workflows

> Published from `madfam-org/enclii` (public repo). Callable from any
> MADFAM service repo — private or public — without extra permissions.

## `build-publish.yml` — build, sign, push, and pin digest

Replaces the hand-rolled "enclii-build" pattern that each service repo
has historically copy-pasted. The reusable captures every fix we've
landed over time:

- pinned cosign installer + release (broken @v3 head drift)
- Docker Hub login BEFORE setup-buildx, `continue-on-error` when creds
  unavailable (org-secret visibility edge cases + self-hosted runners)
- no `:latest` tags (Kyverno `disallow-latest-tag`)
- cosign keyless sign after push (Kyverno `verify-image-signatures`)
- `kustomize edit set image … @sha256:<digest>` pin commit back to main
  (Kyverno `require-image-digest`)
- ARC runner routing via `vars.ARC_BOOTSTRAP_COMPLETE`
- while-read loop over JSON services (bash doesn't word-split)
- single-line `GITHUB_OUTPUT` (no multi-line heredoc)

### How to adopt

Replace your repo's `.github/workflows/enclii-build.yml` (or similar)
with a ~20-line caller:

```yaml
name: Build & Deploy

on:
  push:
    branches: [main]
    paths:
      - 'apps/**'
      - 'services/**'
      - 'packages/**'
      - 'infra/k8s/production/**'
      - '.github/workflows/**'
  workflow_dispatch:
    inputs:
      services:
        description: Comma-separated service names (empty = change-detect)
        required: false
        default: ''

jobs:
  build-publish:
    # Pin a release by its commit SHA, with the tag as a comment. See
    # "Pinning the reusable workflow" below for how to find the SHA.
    uses: madfam-org/enclii/.github/workflows/build-publish.yml@<40-hex commit SHA>  # vX.Y.Z
    with:
      image_prefix: ghcr.io/madfam-org/<repo-slug>
      kustomization_path: infra/k8s/production
      services: |
        [
          {"name":"api",       "dockerfile":"apps/api/Dockerfile",       "paths":"apps/api packages"},
          {"name":"web",       "dockerfile":"apps/web/Dockerfile",       "paths":"apps/web packages"},
          {"name":"admin",     "dockerfile":"apps/admin/Dockerfile",     "paths":"apps/admin"}
        ]
      # Per-service `context` is optional (defaults to "."). Set it when
      # the Dockerfile's COPY paths are relative to a subdirectory
      # rather than the repo root, e.g.:
      #   {"name":"api","context":"backend","dockerfile":"backend/Dockerfile","paths":"backend"}
    # Named secrets: pass only what this repo needs (see "Secrets" below).
    # `secrets: inherit` also still works.
    secrets:
      NPM_MADFAM_TOKEN: ${{ secrets.NPM_MADFAM_TOKEN }}
      ENCLII_CALLBACK_TOKEN: ${{ secrets.ENCLII_CALLBACK_TOKEN }}
    permissions:
      contents: write
      packages: write
      id-token: write
```

### Required repo setup

1. **Dockerfiles**: use `FROM public.ecr.aws/docker/library/<image>`
   for Docker Hub images (avoid the anon rate limit on self-hosted
   runners). Don't build with `:latest` as the default tag.
2. **ARC runner routing**: set repo variable
   `ARC_BOOTSTRAP_COMPLETE=true` if you want self-hosted runners
   (recommended once the `madfam-runners-blue` pool is healthy).
3. **Kustomization**: `infra/k8s/production/kustomization.yaml`
   must have an `images:` section listing each service's image name;
   the workflow edits these entries with the pushed digest.
4. **Branch protection**: if `main` requires PR review, provide an
   `ENCLII_COMMIT_TOKEN` secret (PAT with contents:write) so the
   digest-pin commit can push past the protection.

### Secrets

`build-publish.yml` declares every secret it reads under
`on.workflow_call.secrets`, all `required: false`. A caller can either:

- keep `secrets: inherit`, which passes every secret the caller repo can
  see, declared or not; or
- pass named secrets (`secrets: { NAME: ${{ secrets.NAME }} }`), which
  hands the reusable workflow only what you list. Passing a name that is
  not in the table below is a caller-side error: GitHub rejects a secret the
  called workflow does not declare.

Anything you do not pass is empty inside the workflow, and each use falls
back as listed. Pass the ones whose fallback is wrong for your repo.

| Secret | Purpose | When not passed |
|---|---|---|
| `NPM_MADFAM_TOKEN` | npm.madfam.io auth, mounted as the BuildKit secret `npmrc` | Empty token; any `pnpm install` of `@janua`/`@madfam`/`@forj`/`@cotiza` packages fails |
| `ENCLII_CALLBACK_TOKEN` | Lifecycle-callback bearer (matches the server's `ENCLII_ARGOCD_WEBHOOK_SECRET`) | Warning, no Release registered: a new service stays at 0 replicas |
| `ENCLII_DEPLOY_TOKEN` | Legacy name for the callback bearer | Unused when `ENCLII_CALLBACK_TOKEN` is set |
| `ENCLII_API_URL` | Callback endpoint | `https://api.enclii.dev` |
| `ENCLII_COMMIT_TOKEN` | Token the digest-pin commit pushes with | `github.token`; pass it if `main` is protected against that token |
| `GHCR_PAT` | GHCR auth override for push, digest read and verify | `github.token` |
| `DOCKER_USERNAME` / `DOCKER_TOKEN` | Docker Hub login | Login skipped (soft-fail); use `public.ecr.aws` mirrors |

### Pinning the reusable workflow

Pin a release, not `@main`. With `secrets: inherit`, whatever code sits at
the ref receives every secret your repo holds, so a moving ref is a standing,
unreviewed hand-off of those secrets. Two forms work:

- **Commit SHA plus the tag in a comment** (GitHub's recommended form:
  "Using the commit SHA is the safest option for stability and security"):

  ```yaml
  uses: madfam-org/enclii/.github/workflows/build-publish.yml@<sha>  # vX.Y.Z
  ```

  Only commits that contain the SHA-aware signer check can be pinned this
  way: the first release tag cut after madfam-org/enclii#620, and anything
  later. Older commits still reject their own SHA identity at the pin step
  (see below).
- **Tag**: `@vX.Y.Z`. Still accepted. A tag can be moved by anyone with
  write access to enclii, which a SHA cannot.

Find a release's commit SHA. The `^{}` suffix peels an annotated tag to the
commit it points at, and that commit is the SHA to pin:

```bash
git ls-remote https://github.com/madfam-org/enclii.git 'refs/tags/vX.Y.Z^{}'
# <40-hex sha>	refs/tags/vX.Y.Z^{}
# No output? It is a lightweight tag. Drop the ^{}:
git ls-remote https://github.com/madfam-org/enclii.git 'refs/tags/vX.Y.Z'
```

Dependabot (`package-ecosystem: github-actions`) understands the
`@<sha>  # vX.Y.Z` form and bumps both together.

#### How a SHA pin is verified

Keyless signing records the build job's OIDC `job_workflow_ref` as the
certificate identity: `https://github.com/madfam-org/enclii/.github/workflows/build-publish.yml@<ref>`,
where `<ref>` is exactly what the caller wrote after `@`. Before pinning a
digest, the pin job runs `cosign verify` against an identity regexp:

- A tag or `main` caller gets the long-standing regexp: any `madfam-org`
  workflow at `@refs/heads/main` or `@refs/tags/v…`. This is unchanged.
- A SHA caller first has the SHA checked. It must be reachable from
  `madfam-org/enclii` `main` (the GitHub compare API reports
  `main...<sha>` as `identical` or `behind`) or be the commit of a `v*`
  tag. If it is, the regexp additionally accepts exactly one identity:
  `build-publish.yml@<that sha>`. If it is not, the pin step fails with
  `NOT reachable from madfam-org/enclii main`.

#### What that check does not protect against (imposter commits)

`uses: madfam-org/enclii/...@<sha>` resolves a commit from **any fork in
the repository network**, not only from `madfam-org/enclii`, and the code
that runs is the code at that SHA. The reachability check lives in that
code. An attacker who chooses the SHA also chooses the check, and can
delete it. So the check does exactly one job: it stops an **honest** caller
from pinning an unmerged PR head or a fork commit by mistake. It is not a
security boundary against a malicious SHA.

The controls that do hold are elsewhere:

- **Who can edit caller workflows.** A malicious `uses:` line has to be
  merged into a caller repo. Review changes to `.github/workflows/` there.
- **Kyverno admission.** The cluster admits `ghcr.io/madfam-org/*` images
  only with a keyless signature from the GitHub Actions OIDC issuer (see
  `infra/k8s/base/kyverno/policies/image-policies.yaml`). That policy does
  not look at the workflow ref, so a SHA-pinned signature is admitted
  exactly as a tag-pinned one was before. Pushing to `ghcr.io/madfam-org`
  still requires that org's package write access.

When you pin a SHA, check it before merging:
`git ls-remote https://github.com/madfam-org/enclii.git 'refs/tags/vX.Y.Z^{}'`
must print the same SHA as your `uses:` line.

### Migration playbook

For each repo:

1. Replace `.github/workflows/enclii-build.yml` with the caller above.
2. Verify `Dockerfile` FROM lines use `public.ecr.aws/docker/library/…`
   for Docker Hub images.
3. Confirm `infra/k8s/production/kustomization.yaml` has the `images:`
   stanza.
4. Open a PR — the reusable workflow will run, validate the pipeline
   end-to-end, and (on merge) commit the first digest pin.

### Debugging

- Matrix resolves to `[]` on workflow_dispatch with no changes: the
  `services` input in the caller must include `paths` for each service,
  OR you pass an explicit list via dispatch input.
- A `paths` entry is a directory (prefix match: anything under it) or a
  file (exact match): `"src package.json .npmrc"` rebuilds on a change
  under `src/` and on a change to either file. Before 2026-09-06 only the
  prefix form matched, so a dependency-only merge (lockfile + manifest)
  produced a green run with **no image and no pin** — read the run: if
  `Build <service>` is `skipped` and no `ci: pin image digests` commit
  follows on main, nothing shipped. Pin `@v1.0.0-alpha.8` or later.
- 429 on base-image pull: flip the Dockerfile's `FROM` to
  `public.ecr.aws/docker/library/…`.
- Cosign fails to sign: verify the OIDC token is being issued
  (`id-token: write` permission on the caller job).
- `Refusing to pin unsigned or unverifiable digest` on a SHA-pinned
  caller: the pinned commit predates the SHA-aware check (#620). Pin a newer
  release's SHA. The step log prints the accepted identity regexp.
- `NOT reachable from madfam-org/enclii main`: the SHA is not on `main`
  and is not a release tag's commit. Re-derive it with `git ls-remote`
  (above).
