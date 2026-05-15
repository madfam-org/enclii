# Signed GitOps Digest Runbook

Date: 2026-05-15

## Purpose

Production image digests in GitOps must represent the exact image artifact built and signed by CI. GitOps must not pin a digest resolved later from a tag or registry manifest, because that can advance production to an unsigned image that Kyverno will correctly reject.

## Source of truth

The signed source of truth is the digest artifact emitted by the image build job after the container image is built and signed.

The production digest commit job must:

1. Run only after the image build matrix succeeds.
2. Load the digest from the build artifact.
3. Refuse GHCR tag or registry fallback when the digest artifact is missing.
4. Verify the exact `ghcr.io/...@sha256:...` reference with cosign before editing any production kustomization.
5. Commit the production GitOps pin only after cosign verification succeeds.

The reusable `build-publish` workflow and the Enclii platform build callback follow the same rule. When Roundhouse reports a completed build, Switchyard API verifies the exact image digest with cosign before it writes any `kustomization.yaml` update through the GitHub Contents API. A missing `cosign` binary, an unsigned digest, or a digest signed by an untrusted identity must fail closed and leave GitOps unchanged.

## Expected failure mode

If CI or the Enclii platform cannot verify the signature for the exact digest, the digest commit path must fail closed and leave production unchanged.

This is intentional. A failed digest commit is less risky than an unsigned production image pin that blocks Argo CD sync and prevents Enclii from rolling out critical provider changes.

## Remediation steps

When `core-services` is blocked by Kyverno image signature verification:

1. Identify the rejected image digest from the Enclii Argo CD application status.
2. Verify that exact digest with cosign.
3. If cosign reports no signatures, do not bypass Kyverno.
4. Inspect the CI build run and confirm the digest that was signed by the build job.
5. Trigger or allow CI to produce a new signed digest artifact and commit that verified digest through the guarded digest commit job.
6. Sync the affected Argo CD application through Enclii after GitOps contains only signed digests.

## Enclii-first rule

Use Enclii operations for deployment sync and health checks. Direct cluster access is reserved for emergency diagnostics when Enclii cannot expose the required evidence.

## madfam-bot commit policy

`madfam-bot` may update production GitOps only as a release controller, not as an unbounded build callback.

Allowed behavior:

1. Verify the exact image digest with cosign.
2. Attach release evidence: source SHA, build run, image digest, signature identity, Enclii operation ID, and target environment.
3. Batch all service image updates for a promotion wave into one idempotent commit.
4. Leave production unchanged when any image is unsigned, missing provenance, or rejected by policy.

Disallowed behavior:

1. Direct-to-main digest churn after every build callback.
2. Repeated commits for the same service and target environment without a new verified release intent.
3. Any production pin to an unsigned image digest.
4. Treating a digest commit as proof that a service is deployed, healthy, or policy-admissible.

If `madfam-bot` continues producing unsigned or repetitive production digest commits after the guarded Switchyard API image is live, stop the writer path through Enclii first. If Enclii cannot identify the writer, rotate the affected GitHub token through the approved Selva/Enclii secret workflow and keep the old token disabled until the callback source is found.
