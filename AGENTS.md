# Enclii Agent Operating Guide

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


<!-- MADFAM-AGENTS-CANONICAL v1 -->

This is the canonical instruction file for Claude, Codex, and any other LLM
agent working in this repository. `CLAUDE.md` is kept only as a compatibility
redirect and should not become the source of truth again.

## Required operating doctrine

- Read this file before making repo changes.
- Prefer existing repo conventions, scripts, and docs over introducing new
  patterns.
- Preserve user work and never revert unrelated changes.
- Treat production operations as Enclii-first: use Enclii web, API, or CLI for
  provisioning, deployment, observability, domains, secrets, provider
  operations, scaling, rollback, and remediation.
- Use direct `kubectl`, `helm`, SSH, provider CLIs/APIs, `docker exec`, or
  direct container access only for platform bootstrap or documented break-glass
  emergencies when Enclii is unavailable or lacks an implemented adapter.
- Record any missing Enclii adapter gap instead of normalizing raw production
  access in docs or runbooks.

## Repo entrypoints

- `README.md`
- `ECOSYSTEM.md`
- `docs/`
- `infra/`
- `.github/workflows/`

## LLM context files

- `llms.txt` is the compact context index.
- `llms-full.txt` is the durable full-context map and operating contract.
- `AGENTS.md` is canonical for agent instructions.
- `CLAUDE.md` redirects here for Claude compatibility.

## Maintenance

Regenerate or repair these files with
`internal-devops/scripts/sync-agent-docs.py` from the labspace ecosystem.

## Cursor IDE

- Open `labspace/madfam-platform.code-workspace` for platform work (not full `labspace/` root).
- Entrypoints: `apps/switchyard-api/`, `apps/switchyard-ui/`, `packages/cli/`, `infra/k8s/`.
- Default agent model: **Auto**; use **Opus 4.7** / **GPT-5.3 Codex** + Max Mode only for large infra/API spans.
- `.cursorignore` excludes `tests/golden/` and rendered manifests from indexing.
- Playbook: `internal-devops/ecosystem/cursor-usage-playbook.md`.
