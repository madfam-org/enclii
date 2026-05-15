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

## Expected failure mode

If CI cannot verify the signature for the exact digest, the digest commit job must fail closed and leave production unchanged.

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
