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
    uses: madfam-org/enclii/.github/workflows/build-publish.yml@main
    with:
      image_prefix: ghcr.io/madfam-org/<repo-slug>
      kustomization_path: infra/k8s/production
      services: |
        [
          {"name":"api",       "dockerfile":"apps/api/Dockerfile",       "paths":"apps/api packages"},
          {"name":"web",       "dockerfile":"apps/web/Dockerfile",       "paths":"apps/web packages"},
          {"name":"admin",     "dockerfile":"apps/admin/Dockerfile",     "paths":"apps/admin"}
        ]
    secrets: inherit
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

All optional; workflow soft-fails when absent.

| Secret | Purpose |
|---|---|
| `DOCKER_USERNAME` / `DOCKER_TOKEN` | Docker Hub login (usually org-level) |
| `ENCLII_API_URL` / `ENCLII_DEPLOY_TOKEN` | Enclii lifecycle callback |
| `ENCLII_COMMIT_TOKEN` | PAT to push past branch protection |

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
- 429 on base-image pull: flip the Dockerfile's `FROM` to
  `public.ecr.aws/docker/library/…`.
- Cosign fails to sign: verify the OIDC token is being issued
  (`id-token: write` permission on the caller job).
