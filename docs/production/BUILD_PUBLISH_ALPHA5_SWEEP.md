# build-publish.yml `v1.0.0-alpha.5` — tag + caller sweep

Collateral for the release that follows the registry-digest-handoff PR. **Nothing here
has been executed.** No tag was created and no caller was bumped by the authoring
session; the orchestrator runs the sweep after that PR merges.

## Why a new tag at all

Callers pin the reusable workflow by TAG, not by branch or SHA:

- `@main` would be a standing unreviewed secret transfer, because callers use
  `secrets: inherit`.
- `@<sha>` fails the pin job's own cosign identity regexp, which accepts only
  `refs/(heads/main|tags/v[0-9].*)`.

So a fix to `build-publish.yml` reaches callers only when a new tag is cut and the
callers are bumped to it. Until both happen, every pinned caller keeps running the
old workflow.

## Delta from `v1.0.0-alpha.4`

`v1.0.0-alpha.4` is one commit behind `main` on this file. The delta is therefore
exactly two changes:

1. **PR #453** (`50848cb1`) — already on main, not yet in any tag: disables the
   docker toolkit's diagnostic build-record upload, retries the digest upload once,
   and moves the deploy-state-guard onto the ARC pool it guards (it was dying at
   0 steps on `ubuntu-latest` and inverting the alarm).
2. **This PR** — registry-derived digests + guard hardening.

Verify before tagging:

```
git log --oneline v1.0.0-alpha.4..main -- .github/workflows/build-publish.yml
```

Expect exactly those two commits and nothing else.

## Tag message (verbatim)

Create as an ANNOTATED tag on the merge commit, matching how alpha.1-alpha.4 were cut:

```
git tag -a v1.0.0-alpha.5 -F <this-message> <merge-sha>
git push origin v1.0.0-alpha.5
```

---

```
v1.0.0-alpha.5

For build-publish.yml consumers, the delta from v1.0.0-alpha.4 is two commits:
PR #453 and the registry-digest-handoff PR. Together they take GitHub billing
off the deploy hot path and close the gap that let a stale host pass the guard.

QUOTA IMMUNITY. The workflow no longer hands image digests from the build matrix
to the pin job through an Actions artifact. madfam-org is on the FREE plan: a
fixed 500 MB pool shared between Actions artifacts AND GHCR packages, hard-stop
when full, no overage. Every deploy pushes another image version into that same
pool, so the pool refills itself — it blocked twice in 24h on 2026-08-27, and a
purge bought only ~3h before it re-blocked org-wide. Each time, images built,
pushed and signed GREEN while the digest upload failed, the pin never happened,
main went green, and every caller's live host kept serving its previous build.

The pin job now derives each digest directly from GHCR with
`docker buildx imagetools inspect <prefix>/<svc>:<commit-sha>`. The registry
already knows every digest it accepted, so the artifact hop — and with it the
billing dependency — is gone entirely. Nothing else consumed those artifacts.
The `:<commit-sha>` tag is the only tag this workflow pushes (no `:latest`,
which Kyverno rejects), it is unique per commit, and no branch-moving tag exists
anywhere in the push, so no reference can drift to an unrelated build between
build and pin. Derived digests are still cosign-verified against the madfam-org
workflow identity before being pinned.

GUARD HARDENING. The deploy-state-guard asserted only that the pin JOB was
green. But that job reports success when all of its meaningful steps skip behind
"no digests resolved" — so build=success plus pin=success plus zero pins passed
the guard with nothing pinned and the host stale. The pin job now emits
pins_written and pin_commit, and the guard requires pins_written > 0 once a
build has run. The full truth table is documented in the workflow and was
executed exhaustively across all 48 combinations.

Also in #453: the diagnostic docker build-record upload is disabled (it competed
for the same capped pool while nothing consumed it), and the guard runs on the
same ARC pool as the jobs it guards — on ubuntu-latest it was dying before its
first step whenever GitHub-hosted minutes were unavailable and reporting "THE
LIVE HOST IS STILL SERVING THE PREVIOUS BUILD" on deploys that were fine.

ADR-010: load-bearing paths must not depend on GitHub billing.
```

---

## Callers to bump

Seven repos pin this workflow. All are on `v1.0.0-alpha.2` except crea-frontend,
which is on `v1.0.0-alpha.4`. Every one of them is exposed to the quota class
today.

| repo | call site | current pin |
| --- | --- | --- |
| acervo | `.github/workflows/build-deploy.yml:21` | `v1.0.0-alpha.2` |
| crea-frontend | `.github/workflows/build-deploy.yml:35` | `v1.0.0-alpha.4` |
| crea-map | `.github/workflows/build-deploy.yml:29` | `v1.0.0-alpha.2` |
| kalya | `.github/workflows/build-deploy.yml:29` | `v1.0.0-alpha.2` |
| lexidrop | `.github/workflows/build-deploy.yml:29` | `v1.0.0-alpha.2` |
| marca | `.github/workflows/build-deploy.yml:21` | `v1.0.0-alpha.2` |
| nauta | `.github/workflows/build-deploy.yml:74` | `v1.0.0-alpha.2` |

Line numbers were read at the time of writing; re-grep rather than trusting them:

```
grep -rn 'build-publish\.yml@' .github/workflows/
```

Note `nauta/.github/workflows/build-deploy.yml` also mentions the tag in comments
(lines 26/36) explaining why callers pin a tag rather than `@main` or a SHA. Those
are prose, not call sites — bump the `uses:` line only, and leave the reasoning
intact.

### Sweep order

Bump one caller first, let it run a real deploy, and confirm the pin landed before
touching the other six. The registry-derivation path has never executed in CI; a
staged sweep means a surprise costs one repo rather than seven.

Per caller, confirm on the first run after the bump:

1. the pin job's `Derive digests from GHCR` step resolved a digest per service;
2. a `ci: pin image digests from <sha>` commit exists on the caller's `main`;
3. the digest in its `kustomization.yaml` actually moved;
4. `Guard against silent stale deploy` is green.

Check 4 is corroborating only — 2 and 3 are load-bearing. That distinction is the
whole point of the guard-hardening half of this release: a green guard was never
by itself evidence of a deploy.
