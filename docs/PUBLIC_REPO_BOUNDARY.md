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
