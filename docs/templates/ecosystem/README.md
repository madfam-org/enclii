# ECOSYSTEM.md generator

Produces the self-contained `ECOSYSTEM.md` file that every MADFAM repo ships
at its root. Each rendered file contains this repo's role in the ecosystem +
the 12-platform map + the full enclii CLI DevOps reference, so a Claude
session on a fresh machine can operate any service by reading only that
repo's `ECOSYSTEM.md`.

## Usage

From `labspace/enclii` (or any MADFAM checkout where this lives):

```bash
# Regenerate ECOSYSTEM.md for every repo
MADFAM_LABSPACE=/path/to/labspace python3 docs/templates/ecosystem/generator.py

# Regenerate specific repos
python3 docs/templates/ecosystem/generator.py enclii janua tezca
```

`MADFAM_LABSPACE` defaults to `/Users/aldoruizluna/labspace`; override it on
other machines.

## Files

| File | Purpose |
|---|---|
| `generator.py` | Render logic + shared boilerplate (ecosystem map + enclii CLI reference) |
| `metadata.py` | Aggregator — unions the per-pillar dicts into `REPOS_FULL` |
| `metadata_platform.py` | Infrastructure + Identity/Auth (9 repos) |
| `metadata_business.py` | Financial/CRM/HCM + Learning (6 repos) |
| `metadata_fabrication.py` | Fabrication (10 repos) |
| `metadata_intelligence.py` | Intelligence/AI/Agents (6 repos) |
| `metadata_experience.py` | Brand/Experience + Games + Ecosystem blueprint (8 repos) |

Split into per-pillar modules so each stays under enclii's 800-line
pre-commit guard.

## When to re-render

- **Template change** (ecosystem map, CLI reference) → edit `generator.py`,
  re-render every repo, open one PR per repo.
- **Add a new repo** → add its entry to the appropriate `metadata_<pillar>.py`,
  re-render only that repo.
- **Correct a repo's metadata** → edit that repo's entry, re-render only it.

Re-renders are deterministic — safe to re-run without worrying about drift.
