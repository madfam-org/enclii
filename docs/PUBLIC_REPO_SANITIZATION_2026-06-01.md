# Enclii Public Repo Sanitization Contract

Date: 2026-06-01
Status: launch-blocking for infrastructure-linked Product/Offer GA evidence

## Position

Enclii public-repo sanitization has ecosystem-wide blast radius because the repo documents and operates deployment, infrastructure, secret, runner, and platform automation patterns.

## Current remediation posture

- `infra/k8s/base/secrets.dev.yaml` has been converted into a public-safe placeholder template.
- Scanner-valid dummy credential material in the identified archived infrastructure audit docs was normalized to non-credential-shaped placeholders.
- No repo-level pass is granted until current-tree scan, history scan, public artifact review, and owner approval are recorded in Tulana.

## Launch-blocking checks

Linked platforms/SKUs cannot rely on Enclii public-repo evidence until the repo has proof that:

- Secret manifests are template-only and scanner-safe.
- Archived audits do not expose exploit-ready topology, credentials, hostnames, tenant identifiers, or privileged operational procedures.
- Public deployment docs distinguish local/staging/production accurately.
- CI artifacts and releases do not expose operational evidence or secret material.
