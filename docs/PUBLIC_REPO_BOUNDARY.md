# Enclii Public Repository Boundary

`enclii` is the public DevOps platform repository. It should document product and
platform contracts, public-safe architecture, and operational workflows that do not
contain private infrastructure, secrets, or sensitive runbook detail.

## Canonical lane assignment

- `internal-devops`: private ops playbooks, raw topology, credential retrieval, cost and incident internals.
- `solarpunk-foundry`: public ecosystem-wide mapping and shared contract references.
- `enclii`: public-safe platform implementation and service-oriented operational guides.
- `tulana`: private service implementation and pricing/competitor intelligence workflows.

## What belongs in this repo

- architecture and design principles that can be discussed publicly
- `enclii` service specs, APIs, and CLI/API documentation
- public-safe runbook templates and flow outlines
- local dogfooding commands that use placeholders only

## Placeholder convention

Node identity is private. This repo names nodes by ROLE and, where a command
needs a token in the host position, uses a `<TOKEN>` placeholder that cannot be
mistaken for a live value:

```text
<CONTROL_PLANE_NODE>   <CONTROL_PLANE_IP>
<WORKER_NODE>          <WORKER_NODE_IP>
<BUILDER_NODE>         <BUILDER_NODE_IP>
```

Resolve them from `internal-devops/infrastructure/nodes.md`. The same rule
applies outside prose, in every file type:

| Surface | Rule |
|---|---|
| Docs | a role ("the control-plane node") or a `<TOKEN>` placeholder |
| Manifests | select by LABEL (`role=builder`) or a standard node-role label — never `kubernetes.io/hostname` |
| Scripts | `${NODE_HOST:?}`-style REQUIRED variables, so a missing value fails loudly instead of seeding a wrong entity |
| Tests | fixture names (`node-a`, `node-b`) |
| Hardware | a class ("dedicated bare-metal", "cloud compute instance") — never a provider SKU |

If the guard blocks a legitimate non-secret example, prefer a placeholder over
a realistic-looking value.

## What belongs elsewhere

- server IPs, hostnames, kubeconfigs, raw access runbooks
- secrets, tokens, signing keys, or private credential paths
- provider billing and procurement detail
- sensitive incident timelines or customer-sensitive data

Use [`internal-devops/docs/repo-boundary-contract.md`](https://github.com/madfam-org/internal-devops/blob/main/docs/repo-boundary-contract.md) as the governing policy.

## Boundary checkpoint for public docs/status edits

For `README`, `ROADMAP`, `AI_CONTEXT`, changelog, and public status surfaces:

- add a checkpoint block in each edit context:
  - date
  - change summary (public-safe)
  - what was redacted/redirected to private-only detail
  - reviewer/owner
- include at least one pointer to this policy:
  - local boundary doc (`enclii/docs/PUBLIC_REPO_BOUNDARY.md`)
  - canonical policy (`https://github.com/madfam-org/internal-devops/blob/main/docs/repo-boundary-contract.md`)

Do not move private operational topology, account detail, or incident evidence into this public repo. Use pointer-only references to the private source.

CI enforces this on changed high-risk doc surfaces with `scripts/boundary-checkpoint-check.sh` from `.github/workflows/public-hygiene.yml`. The PR template carries the same checklist for reviewer visibility.

## Automation — coverage as of 2026-09-04

State this plainly, because the guard has been read as broader than it is.
`scripts/public-hygiene-check.sh` now scans **every tracked text file** (2,626
of them), not the ~doc-only `find` it used before — `.yml`, `.sh`, `.ts`,
`.json` and `.npmrc` were previously never read at all. Its self-test is
`scripts/tests/test-public-hygiene.sh`, run from the same workflow.

| Class | State |
|---|---|
| Stripe / GitHub / AWS key shapes, PEM private-key markers | covered, whole tree |
| npm registry `_auth` with a concrete value | covered, whole tree (placeholder forms `${VAR}`, `%s`, `YOUR_`, `<REDACTED>` excluded) |
| Unresolved secret placeholders (the `CHANGE`/`REPLACE_WITH_` marker forms) | covered, whole tree |
| Server hardware SKUs | covered, whole tree, **through the private pattern file** (same arrangement as node hostnames below): the guard carries no SKU needle of its own. Skipped (`classes_skipped=1`) when that file is unreadable |
| Public IPv4 literals | covered over the OPS file set (docs, manifests, workflows, scripts, env samples; not application source or tests). Octet-range checked; private/loopback/link-local/TEST-NET and documented public resolvers excluded. Measured 0 findings, 0 false positives |
| **Committed `kind: Secret` values** | covered over every tracked `.yaml`/`.yml`, by `scripts/check-committed-secret-values.py` (a YAML parser, not a grep — added after the 2026-09-06 verdaccio finding, where a live bcrypt hash under `stringData:` passed every token-shape grep above). A core/v1 Secret fails when a `data`/`stringData` value survives the placeholder filter **and** looks like credential material: a known shape (bcrypt/crypt, PEM, Stripe/GitHub/AWS/Vault/Slack/Google/npm/GitLab/SendGrid, JWT), a 32+ char hex digest, or an opaque high-entropy run. `data:` is base64-decoded first. `ExternalSecret`, `SealedSecret`, `SecretStore`/`ClusterSecretStore`, and name-only Secret shells pass. There is **no allowlist, by design** — see the limits below. Skipped (`classes_skipped=1`) when PyYAML is absent |
| **Node hostnames** | **not covered by this file, by design.** The needles are the exact strings that must not appear here, so shipping them would publish the answer key, and hashing them buys obfuscation while implying secrecy. They are read from a private file via `MADFAM_HYGIENE_PATTERNS`; when it is unreadable the run prints `node-identity class SKIPPED` and `classes_skipped=1`. The enforcing run is `internal-devops/scripts/check-public-repo-node-identity.py` |
| **Cloudflare tunnel identifiers** | **not covered.** A bare UUID pattern is dominated here by RFC-4122 example ids in CLI docs and by test fixtures; narrowing it to tunnel-context lines returns live findings that belong to the tunnel-identifier lane. Recorded as an open gap rather than assumed fixed |

### What the committed-Secret rule does not catch

Stated plainly, because the target is **zero committed Secret values** and a
guard that implies more coverage than it has is worse than none.

The rule judges the **shape of the value**, never the name of its key and never
a registry of blessed files. That choice is deliberate. Measured over this tree
on 2026-09-07, committed Secrets hold 56 non-empty values, and most are not
secrets at all — `type: helm`, `enableOCI: "true"`, `BILLING_MX_VAT_RATE:
"0.16"`, `url: ghcr.io`, `database: enclii_dev`, a verbatim Prometheus scrape
config. Failing on "any non-placeholder value" would report ~20 non-findings,
and the only way back to green would be an allowlist — the exact mechanism that
lets the next real secret through, since an entry is cheap to add and never
re-reviewed. So there is no allowlist, and the cost is paid here instead:

- **A short, low-entropy secret is not caught.** `password: hunter2` is
  indistinguishable by shape from `type: helm`. Key-name heuristics were
  considered and rejected — `htpasswd` is not an obvious credential key name,
  and a key allowlist is an allowlist.
- **Only `.yaml`/`.yml` is parsed.** A Secret embedded in a Helm template, a
  `.json` manifest, or a heredoc inside a shell script is not read.
- **Unparseable YAML is skipped, not failed.** Templated manifests are not
  valid YAML and are not applied as-is; the run says which files it skipped.
- **A genuinely random-looking non-secret would be a false positive.** None
  exist in this tree today. If one appears, the fix is to move it out of a
  Secret — not to register an exception.

**Consequence:** passing CI is not evidence that a change is boundary-clean —
and a green run with `classes_skipped=1` is not evidence that node identity was
checked. Human review against the "does not belong here" list above is still
the control that matters.

**Treat repository history as public exposure unless proven otherwise.**
Deleting a line from `HEAD` does not remove it from git history, and flipping a
repo private contains an exposure without cleaning it. Node identities are not
credentials, so nothing here is rotation-blocked; for anything that is, rotate
first and independently of any flip.
