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

## Optional per-repo slots

Every slot below is optional and empty by default: a repo that declares none
renders byte-for-byte as it did before the slots existed.

| Key | Renders |
|---|---|
| `sensitivity_banner` | a blockquote above the enclii-first banner, for repos whose data is sensitive |
| `boundary_checkpoint` | a section between the banners and the tagline (some repos' CI requires this marker in `ECOSYSTEM.md`) |
| `production_truth` | a block after the namespace/cluster lines, for a dated operator baseline |
| `provenance_note` | a paragraph appended to *Document provenance* |
| `boilerplate_overrides` | exact-once substitutions applied to the shared ecosystem map + CLI reference |

`boilerplate_overrides` is a list of `{"find": ..., "replace": ..., "why": ...}`.
Each `find` must match **exactly once** across the shared boilerplate or the
render fails with the reason recorded in `why`. That is the point: a repo that
deliberately keeps its own version of a shared paragraph declares it here, and
when the shared text later changes the render breaks loudly instead of silently
dropping the curated line — which is what hand-edited copies did.

## Private metadata overlays

This generator and its metadata are **public**. Some private repos carry
material in their `ECOSYSTEM.md` that must not be published here: real internal
service domains, env-var names, operator production-truth baselines, boundary
checkpoints. Before overlays there were only two options — publish it, or lose
it on every re-render (and it was lost, then re-added downstream by hand).

An overlay is a JSON file kept **in the private repo it describes**, mapping
repo name to the same metadata keys these modules use:

```bash
MADFAM_ECOSYSTEM_METADATA_OVERLAY=/path/to/private-repo/docs/ecosystem-metadata.json \
  python3 docs/templates/ecosystem/generator.py tulana
```

Several files may be given, separated by the platform path separator. Top-level
keys replace the public base entry; `production` merges one level deep. Overlays
are **data, never code** — rendering never executes a private file. Repos with no
overlay entry are unaffected.

## When to re-render

- **Template change** (ecosystem map, CLI reference) → edit `generator.py`,
  re-render every repo, open one PR per repo.
- **Add a new repo** → add its entry to the appropriate `metadata_<pillar>.py`,
  re-render only that repo.
- **Correct a repo's metadata** → edit that repo's entry, re-render only it.

Re-renders are deterministic — safe to re-run without worrying about drift.
