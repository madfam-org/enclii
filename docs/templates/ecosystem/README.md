# ECOSYSTEM.md generator

Produces the self-contained `ECOSYSTEM.md` file that every MADFAM repo ships
at its root. Each rendered file contains this repo's role in the ecosystem +
the product map from the product registry + the full enclii CLI DevOps
reference, so a Claude session on a fresh machine can operate any service by
reading only that repo's `ECOSYSTEM.md`.

## Usage

From `labspace/enclii` (or any MADFAM checkout where this lives):

```bash
# Render into checkouts (a private repo's own docs/ecosystem-metadata.json is picked up)
python3 docs/templates/ecosystem/generator.py --write ../janua ../tezca

# Drift check: render in memory, diff against each checkout's ECOSYSTEM.md, write nothing
python3 docs/templates/ecosystem/generator.py --check ../janua ../tezca
#   exit 0 = every file matches, 1 = drift (a unified diff is printed),
#   2 = UNDETERMINED (no metadata entry, no ECOSYSTEM.md, or no usable projection)

# Legacy form: render named repos into $MADFAM_LABSPACE/<repo>
MADFAM_LABSPACE=/path/to/labspace python3 docs/templates/ecosystem/generator.py enclii janua
```

`--repo NAME` sets the metadata key when a single checkout's directory name
differs from the repo name. `MADFAM_LABSPACE` defaults to
`/Users/aldoruizluna/labspace`; override it on other machines.

## Where the facts come from

| Fact | Source |
|---|---|
| Which products exist, display name, repo, front door, lifecycle, retirements | the public product-registry projection (below) |
| One-line cross-repo role of a product | `site.role`/`role` in the projection if present, else `PLATFORM_ROLES` in `registry.py`, else the registry's `site.banner_keyword` |
| Per-repo description, services, dependencies, env | `metadata_<pillar>.py` (+ private overlay) |
| Estate counts (services, ArgoCD apps, namespaces) | **not rendered** — they live, dated, in `internal-devops/infrastructure/topology.md` |
| Topology | roles only (control-plane, worker, two builders); node identity never |

The projection is read from, in order: `--projection PATH`, then
`MADFAM_PRODUCT_PROJECTION`, then
`$MADFAM_LABSPACE/solarpunk-foundry/packages/core/src/products/projection.public.json`,
then the `solarpunk-foundry` checkout next to this enclii checkout
(the public copy vendored in `madfam-org/solarpunk-foundry`, generated from the
private registry by `internal-devops/scripts/generate-product-projections.py`).
A missing or malformed projection is an error — there is no built-in fallback
map. When the registry changes, re-vendor the projection in the foundry, then
re-render; `--check` is how a stale render is found.

## Files

| File | Purpose |
|---|---|
| `generator.py` | Render logic, `--check`/`--write`, shared boilerplate (conventions, topology, enclii CLI reference) |
| `registry.py` | Projection loading/validation, product tables, `PLATFORM_ROLES`, prettier-aligned tables |
| `metadata.py` | Aggregator — unions the per-pillar dicts into `REPOS_FULL` |
| `metadata_platform.py` | Infrastructure + Identity/Auth (9 repos) |
| `metadata_business.py` | Financial/CRM/HCM + Learning (6 repos) |
| `metadata_fabrication.py` | Fabrication (9 repos) |
| `metadata_intelligence.py` | Intelligence/AI/Agents (5 repos) |
| `metadata_experience.py` | Brand/Experience + Games + Ecosystem blueprint (9 repos) |

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
| `section_appendix` | repo-specific subsections appended to the end of section 1 (after *Key environment variables*) |
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

- **Registry change** (product added, retired, renamed, new front door) →
  re-vendor the projection in `solarpunk-foundry`, then re-render every repo.

Re-renders are deterministic — safe to re-run without worrying about drift.
Tables are emitted in prettier's aligned form, so repos that run
`prettier --check` over Markdown accept a render unchanged.
