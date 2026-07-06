# ARC Cosign Keyless Signing (sigstore) Runbook

> **Boundary checkpoint (2026-07-06, platform on-call):** Public-safe runbook — no
> secrets or production topology beyond this repo's own IaC. Private operational
> detail and sink live in `internal-devops` (2026-07-06 tulana audit roadmap).
> Policy: `docs/PUBLIC_REPO_BOUNDARY.md` (repo-boundary contract).

Date: 2026-07-06
Status: workflow-side fix shipped (tulana PR #24); runner-side egress ruled out in this repo — awaiting preflight evidence from the next deploy

## Symptom

`cosign sign` (keyless) fails on `madfam-runners-blue` ARC runner pods across
MADFAM deploy workflows with:

```
Fulcio OIDC: invalid character 'u' looking for beginning of value
```

Impact: image builds succeed but the workflow fails after push, so the GitOps
digest pin lags (tracked as P0 in
[tulana `docs/platform-ops-gaps-2026-06-22.md`](https://github.com/madfam-org/tulana/blob/main/docs/platform-ops-gaps-2026-06-22.md)).
The interim workaround was manual digest pinning.

## What the error actually means

The message is Go's `json.Unmarshal` failing on a body that starts with `u`.
Cosign received an HTTP response somewhere in the keyless flow (GitHub OIDC
token broker, `oauth2.sigstore.dev`, or `fulcio.sigstore.dev`) whose body was
**plain text, not JSON** — e.g. an error string such as Envoy's
`upstream connect error or disconnect/reset before headers ...` or a broker
`unauthorized`-style body. It is a diagnosability failure: the real error text
was swallowed by the JSON parser.

Two distinct fault classes can produce it:

1. **Workflow-side**: cosign's ambient OIDC fetch hitting a transient or
   permission-related error body from the Actions token broker or the
   sigstore edge (old cosign versions are worse at surfacing these).
2. **Runner-side**: something on the egress path (NetworkPolicy, proxy, DNS,
   TLS interception, stale CA bundle) making sigstore hosts unreachable or
   substituting a non-JSON body.

## Root-cause investigation (2026-07-06): runner-side egress ruled out in this repo

This repo contains **no egress filtering, proxying, or DNS interception that
could block or rewrite runner-pod traffic to sigstore**. Checked and ruled
out:

| Mechanism | Where it would live | Finding |
| --- | --- | --- |
| Runner NetworkPolicy (governing) | [`infra/k8s/policies/arc-default-deny.yaml`](../../infra/k8s/policies/arc-default-deny.yaml), synced by the ArgoCD `network-policies` app ([`infra/argocd/apps/network-policies.yaml`](../../infra/argocd/apps/network-policies.yaml)) | `runner-egress` selects all pods in `arc-runners` and allows egress to `0.0.0.0/0` on TCP 443/80/22/6443 plus DNS 53. Sigstore hosts (all HTTPS/443) are not filterable or filtered — NetworkPolicy is L3/L4 and this one is host-agnostic. |
| Legacy bootstrap NetworkPolicy | [`infra/k8s/base/arc/network-policies.yaml`](../../infra/k8s/base/arc/network-policies.yaml) (applied once by `infra/scripts/arc/install-arc.sh`) | Same broad 443 allow (`arc-runner-egress`). Additive with the above; cannot block. |
| Egress/HTTP proxy | Helm values [`infra/helm/arc/`](../../infra/helm/arc/), rendered manifests [`infra/k8s/production/arc/runner-blue/rendered.yaml`](../../infra/k8s/production/arc/runner-blue/rendered.yaml) | No `HTTP(S)_PROXY`/`NO_PROXY` env anywhere in the runner or dind containers; no proxy sidecar; no service mesh (CNI is k3s flannel `wireguard-native` — no L7 policy layer). |
| DNS policy / interception | Runner pod spec, CoreDNS config | No `dnsPolicy`/`dnsConfig` overrides, no `hostAliases`, no custom CoreDNS rewrite in this repo. |
| Provider firewall | [`infra/terraform/main.tf`](../../infra/terraform/main.tf) (`hcloud_firewall.k8s_nodes`) | All outbound traffic allowed. |
| Stale/injected CA bundle | [`infra/docker/arc-runner/Dockerfile`](../../infra/docker/arc-runner/Dockerfile) | Image installs fresh `ca-certificates` on every build; no custom CA is injected. |

Additionally, an L3/L4 block would surface as a **timeout or connection
reset** (curl hang, `context deadline exceeded`), not as a well-formed HTTP
response with a non-JSON body. The observed error therefore points at fault
class 1 (workflow-side / upstream error body), not at egress config in this
repo.

**Most likely root cause**: cosign's ambient OIDC fetch mishandling an error
body — addressed workflow-side by
[tulana PR #24](https://github.com/madfam-org/tulana/pull/24) (explicit token
mint via `core.getIDToken('sigstore')` passed with `--identity-token`, cosign
2.2.4 → 2.4.1, plus a sigstore connectivity preflight). No runner-side config
change is warranted; inventing an "allowlist" here would be dead config since
nothing filters by host.

## Fix

- **Workflow-side (shipped, tulana PR #24)** — adopt the same pattern in any
  workflow that signs on `madfam-runners-blue`:
  1. `permissions: id-token: write` on the signing job.
  2. Mint the token explicitly (`actions/github-script` →
     `core.getIDToken('sigstore')`) and pass it via
     `cosign sign --identity-token`. Failures then surface the Actions
     service's real error instead of a JSON parse error.
  3. Keep cosign current (≥ 2.4.x). Enclii's reusable
     [`build-publish.yml`](../../.github/workflows/build-publish.yml) already
     pins `sigstore/cosign-installer@v3.7.0`.
  4. Run a sigstore connectivity preflight before signing (see tulana
     `deploy-api.yml` / `deploy-web.yml`).
- **Runner-side (this repo)** — no config change required today. If a real
  egress block is ever confirmed (see verification below), the governing
  object to amend is the `runner-egress` NetworkPolicy in
  `infra/k8s/policies/arc-default-deny.yaml`; the ArgoCD `network-policies`
  app auto-syncs that directory (`selfHeal: true`), so a merge to `main`
  rolls it out without operator action.

## How to verify

The tulana PR #24 preflight output on the **next `main` deploy** is the
discriminating evidence:

| Preflight result | Signing result | Conclusion |
| --- | --- | --- |
| All four hosts ok | Sign succeeds | Root cause was workflow-side OIDC handling. Close the P0; retire the manual digest-pin workaround; then tighten/remove the tulana Kyverno signature PolicyException. |
| All four hosts ok | Sign still fails | Egress is fine; capture the new (now-verbose) cosign/token error and chase the OIDC/Fulcio exchange, not the network. |
| Any host UNREACHABLE | — | Real runner egress problem. Run the one-off probe below to localize it, then escalate to node-level networking (outside this repo's config — nothing in-repo filters egress). |

One-off runner probe from this repo (Enclii-first — no cluster access
needed): GitHub → Actions → **ARC Sigstore Egress Check**
([`.github/workflows/arc-sigstore-egress-check.yml`](../../.github/workflows/arc-sigstore-egress-check.yml))
→ *Run workflow*. It runs on `madfam-runners-blue` and:

1. Curls `oauth2.sigstore.dev`, `fulcio.sigstore.dev`, `rekor.sigstore.dev`,
   `tuf-repo-cdn.sigstore.dev`, printing HTTP status and the first bytes of
   each body (an intercepting proxy or error page names itself).
2. Prints the TLS issuer presented for the sigstore hosts (must be a public
   CA — an internal issuer means TLS interception, which keyless signing
   cannot tolerate).
3. Reproduces cosign's exact OIDC broker request
   (`${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=sigstore`) and prints the raw
   body **only when it is not valid JSON** — that body is the literal
   `invalid character ...` payload. The minted token itself is never printed.

## Rollout

Everything in this change is Git-driven:

- Runbook + probe workflow: merge to `main`; the `workflow_dispatch` probe
  becomes runnable once the workflow file exists on the default branch. No
  cluster changes.
- NetworkPolicy comment correction (`infra/k8s/policies/arc-default-deny.yaml`
  header): comment-only; the ArgoCD `network-policies` app (automated +
  `selfHeal`) reconciles it with no semantic diff. No operator apply step.

## Related

- [tulana `docs/platform-ops-gaps-2026-06-22.md`](https://github.com/madfam-org/tulana/blob/main/docs/platform-ops-gaps-2026-06-22.md) — P0 tracking entry
- [tulana PR #24](https://github.com/madfam-org/tulana/pull/24) — workflow-side fix + preflight
- [`docs/runbooks/SIGNED_GITOPS_DIGESTS.md`](./SIGNED_GITOPS_DIGESTS.md) — why unsigned digests must fail closed
- [`infra/docker/arc-runner/README.md`](../../infra/docker/arc-runner/README.md) — runner image bump policy
- Follow-up once signing is green: tighten/remove the tulana signature
  PolicyException so the Kyverno gate actually enforces (tracked in
  internal-devops 2026-07-06 tulana audit)
