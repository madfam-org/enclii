#!/usr/bin/env python3
"""
ECOSYSTEM.md generator — renders per-repo self-contained docs that embed
the MADFAM ecosystem map + full enclii CLI reference so any repo can be
operated from its own ECOSYSTEM.md alone.

Usage:

    generator.py                               # render every repo into $MADFAM_LABSPACE/<repo>
    generator.py enclii janua                  # render named repos into $MADFAM_LABSPACE/<repo>
    generator.py --write PATH [PATH ...]       # render into the given checkouts
    generator.py --check PATH [PATH ...]       # diff each checkout's ECOSYSTEM.md, write nothing

    --projection PATH    product-registry projection (else $MADFAM_PRODUCT_PROJECTION,
                         else $MADFAM_LABSPACE/solarpunk-foundry/packages/core/src/
                         products/projection.public.json)
    --repo NAME          metadata key when a checkout's directory name differs

`--check` exit codes: 0 every file matches, 1 at least one file drifted,
2 UNDETERMINED (no metadata, no ECOSYSTEM.md, or no usable projection) —
fails CI exactly like 1.
"""
import argparse
import difflib
import json
import os
import sys
from pathlib import Path
from textwrap import dedent

# Package-relative import with standalone fallback.
try:
    from .metadata import REPOS_FULL
    from .registry import (
        ProjectionError,
        load_projection,
        md_table,
        render_platform_map,
        render_registry_entry,
        render_retired,
    )
except ImportError:
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from metadata import REPOS_FULL  # type: ignore
    from registry import (  # type: ignore
        ProjectionError,
        load_projection,
        md_table,
        render_platform_map,
        render_registry_entry,
        render_retired,
    )

LABSPACE = Path(os.environ.get("MADFAM_LABSPACE", "/Users/aldoruizluna/labspace"))

# ---------------------------------------------------------------------------
# Private metadata overlays
#
# This generator lives in a PUBLIC repo, and `ECOSYSTEM.md` is rendered into
# public and private repos alike. Some private repos carry curated material in
# their ECOSYSTEM.md that must never be published here: a sensitivity banner, a
# repo-boundary checkpoint, an operator "current production truth" baseline,
# real internal service domains and env-var names, repo-specific CLI examples.
# Before this overlay existed the only way to make such a repo re-renderable
# was to move that material into these metadata modules — i.e. to publish it.
# So each re-render dropped it instead, and it was re-added by hand downstream.
#
# An overlay is a JSON file, kept in the private repo it describes, mapping
# repo name to the same metadata keys these modules use. Point the generator at
# one (or several, os.pathsep-separated) with:
#
#     MADFAM_ECOSYSTEM_METADATA_OVERLAY=/path/to/private/ecosystem-metadata.json
#
# Data only — never code — so rendering never executes a private file. Repos
# with no overlay entry render exactly as before, byte for byte.
# ---------------------------------------------------------------------------

OVERLAY_ENV = "MADFAM_ECOSYSTEM_METADATA_OVERLAY"

#: Where a private repo keeps its own overlay. `--check`/`--write PATH` pick it
#: up automatically, so a scheduled drift check needs no per-repo wiring.
REPO_OVERLAY_RELPATH = Path("docs") / "ecosystem-metadata.json"


def _merge_repo_meta(base: dict, overlay: dict) -> dict:
    """Overlay one repo's metadata over its public base entry.

    Top-level keys are replaced; `production` is merged one level deep so an
    overlay can correct `services` without restating `namespace`.
    """
    merged = dict(base)
    for key, value in overlay.items():
        if key == "production" and isinstance(value, dict):
            merged["production"] = {**base.get("production", {}), **value}
        else:
            merged[key] = value
    return merged


def load_repos(overlay_paths: str | None = None) -> dict:
    """`REPOS_FULL`, with any private overlays applied."""
    repos = {repo: dict(meta) for repo, meta in REPOS_FULL.items()}
    raw = overlay_paths if overlay_paths is not None else os.environ.get(OVERLAY_ENV, "")
    for entry in raw.split(os.pathsep):
        path = entry.strip()
        if not path:
            continue
        overlay = json.loads(Path(path).read_text())
        for repo, meta in overlay.items():
            if not isinstance(meta, dict):
                raise SystemExit(f"{path}: overlay entry for {repo!r} must be an object")
            repos[repo] = _merge_repo_meta(repos.get(repo, {}), meta)
    return repos


def apply_boilerplate_overrides(repo: str, blocks: dict, overrides) -> dict:
    """Apply a repo's `boilerplate_overrides` to the shared blocks.

    Each override is `{"find": ..., "replace": ..., "why": ...}` and must match
    EXACTLY ONCE across the shared boilerplate. A repo that deliberately keeps
    its own version of a shared paragraph (a repo-specific CLI example, a local
    caveat) declares it here instead of hand-editing the rendered file. When the
    shared text later changes, the override stops matching and the render FAILS
    — loudly, at the moment the drift appears — rather than silently dropping
    the curated line the way a hand-edited copy did.
    """
    patched = dict(blocks)
    for index, override in enumerate(overrides or []):
        try:
            find = override["find"]
            replace = override["replace"]
        except (TypeError, KeyError) as error:
            raise SystemExit(
                f"{repo}: boilerplate_overrides[{index}] needs 'find' and 'replace' keys"
            ) from error
        total = sum(block.count(find) for block in patched.values())
        if total != 1:
            raise SystemExit(
                f"{repo}: boilerplate_overrides[{index}] matched {total} times, expected 1.\n"
                f"  why: {override.get('why', '(no reason recorded)')}\n"
                f"  find: {find[:120]!r}\n"
                "  The shared boilerplate has drifted. Re-read the current template block "
                "and update the override (or drop it if the shared text now says the same "
                "thing)."
            )
        for key, block in patched.items():
            if find in block:
                patched[key] = block.replace(find, replace)
    return patched


# ---------------------------------------------------------------------------
# Enclii-first legacy-raw banner
#
# `internal-devops/scripts/check-enclii-first-docs.py` scans every repo's root
# ECOSYSTEM.md. Any line matching its RAW_TOOL_PATTERNS fails the guard unless
# either (a) the surrounding ±4-line window contains an ALLOW_TERM, or (b) the
# document carries this marker anywhere in its text, which whitelists the file
# wholesale.
#
# Rendered ECOSYSTEM.md files DO contain such a line — the "Break-glass-only
# access" paragraph in ENCLII_CLI_REF names `kubectl`, `helm`, and
# `docker exec`. Today that line survives only on route (a): "break-glass" and
# "bootstrap" happen to sit inside the context window. That is incidental, not
# designed — an unrelated edit to the surrounding prose silently re-arms the
# guard fleet-wide.
#
# Every fleet ECOSYSTEM.md already carries this banner, applied by hand
# (madfam-org/forj#134 being the most recent). The generator never emitted it,
# so each re-render dropped it and someone re-added it downstream. Emitting it
# here makes generator output self-sufficient under route (b) and ends the
# hand-patch loop.
#
# The text below is byte-identical to the banner in all fleet ECOSYSTEM.md
# files as of 2026-08-27. The marker string must stay exactly in sync with
# LEGACY_RAW_MARKER in the checker.
# ---------------------------------------------------------------------------

LEGACY_RAW_MARKER = "MADFAM-ENCLII-FIRST-LEGACY-RAW v1"

LEGACY_RAW_BANNER = dedent(f"""
    > [!IMPORTANT]
    > {LEGACY_RAW_MARKER}: This document contains legacy raw infrastructure command examples.
    > Routine production operations must use Enclii web, API, or CLI. Treat raw
    > `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
    > access as platform bootstrap or documented break-glass only, and record any
    > missing Enclii adapter gap.
""").strip()


# ---------------------------------------------------------------------------
# Shared boilerplate — embedded verbatim in every ECOSYSTEM.md so each repo
# is truly self-contained.
#
# Nothing below types an estate fact. Which products exist, their repos, front
# doors and lifecycles come from the product-registry projection (registry.py);
# counts (services, ArgoCD apps, namespaces) are not rendered at all, because a
# typed count is stale the week after it is typed ("~40 services", "~28 apps"
# and "3 nodes" survived in 36 repos for five months). The topology is ROLES
# only: this text is copied into public repos.
# ---------------------------------------------------------------------------

ECOSYSTEM_MAP_HEAD = dedent("""
    ## MADFAM Ecosystem Map

    Everything below is embedded here so this document stands alone. The product
    tables are rendered from the public projection of the MADFAM product registry
    (`madfam-org/solarpunk-foundry` → `packages/core/src/products/projection.public.json`,
    generated from the registry in `madfam-org/internal-devops`). To change a row,
    change the registry and re-render — never hand-edit a rendered copy.

    Estate counts (services, ArgoCD applications, namespaces) are deliberately not
    typed here: they move weekly. The dated figures live in the private operations
    record, `madfam-org/internal-devops` (`infrastructure/topology.md`).

    ### Products in the registry
""").strip()

ECOSYSTEM_MAP_TAIL = dedent("""
    ### Cross-repo conventions

    - **Auth**: every authenticated service verifies Janua JWTs via JWKS at
      `https://auth.madfam.io/.well-known/jwks.json`. RS256 only — HS256 is
      banned on any path that verifies a Janua token (audit 2026-04-23 H3/H4);
      an app's own session cookie needs its own secret.
    - **Billing**: credit metering + entitlements flow through Dhanam. See
      `madfam-org/dhanam` for the meter/entitlement/invoice APIs.
    - **Inference**: every LLM call should route through Selva
      (`selva-office`) at `/v1` (OpenAI-compatible). Do not talk directly
      to OpenAI / Anthropic from service code.
    - **Agent SaaS tools**: end-user delegated tool calls (Slack, Gmail, etc.)
      route through Coupler (`madfam-org/coupler`, the Agent Tool Plane), not the
      Enclii Provider Hub.
      Operator infra actions stay on Enclii `providers.*` / `ops.*` (proxied as
      `madfam.ops.*` from Coupler for admin agents only).
    - **Third-party messages**: email/SMS/chat to people outside MADFAM go out
      through Angelia Courier (`madfam-org/angelia`). Carve-outs: Janua's
      customer-configured alert notifier and Selva agent tools.
    - **CORS**: explicit allowlist per service. Wildcards are banned
      (audit 2026-04-23 H2/H5/H6).
    - **Images**: `@sha256:`-pinned in every manifest; mutable tags such as
      `:latest` are a Kyverno policy violation.
    - **Onboarding**: `enclii onboard` (`POST /v1/admin/onboard` on
      switchyard-api) creates namespace, ArgoCD app, Cloudflare tunnel routes,
      Janua client, and NetworkPolicies in one shot. See
      `enclii/docs/guides/ONBOARDING_GUIDE.md`.

    ### Production topology

    Bare-metal k3s (v1.33+), 4 nodes, described by ROLE only. This file is
    generated and copied into public repos, so it never carries node hostnames,
    IP addresses or hardware SKUs (2026-07-16 exposure class 1). Node identity
    lives only in `madfam-org/internal-devops`.

    - control-plane node — control plane + primary workload
    - worker node — workloads + Longhorn second replica
    - two builder nodes (labelled `role=builder`, tainted
      `builder=true:NoSchedule`) — ARC runners only

    **Ingress**: Cloudflare Tunnel → cloudflared pods → K8s ClusterIP → container port.
    Zero exposed node ports. TLS terminated at Cloudflare edge.

    **Storage**: Longhorn CSI in 2-replica mode across the control-plane and
    worker nodes. Object storage: Cloudflare R2 (zero egress).

    **GitOps**: ArgoCD App-of-Apps with self-heal. Push to `main` → CI builds →
    GHCR → `kustomize edit set image` commits the digest →
    ArgoCD syncs → Switchyard tracks lifecycle events.

    **Operational access** (SSH, kubeconfigs, node identity, estate counts, cost
    ledger): private repo `madfam-org/internal-devops`. Not in any public repo.
""").strip()


def render_ecosystem_map(projection: dict) -> str:
    """The full map block: registry-sourced tables between fixed prose."""
    live = len(projection["products"])
    return "\n\n".join([
        ECOSYSTEM_MAP_HEAD,
        f"{live} customer-facing products, grouped by the registry's category and "
        "listed in registry order. `—` means the registry records no public front door yet.",
        render_platform_map(projection),
        "### Retired products — never present as live",
        render_retired(projection),
        ECOSYSTEM_MAP_TAIL,
    ])


ENCLII_CLI_REF = dedent("""
    ## Enclii CLI — DevOps Reference

    **Strong preference: use `enclii` over `kubectl`** for all operational
    tasks. The CLI routes through Switchyard API, which gives you audit
    logging, lifecycle event tracking, and service-scoped context. Escape
    to kubectl only for the gaps listed at the end of this section.

    ### Install

    GitHub Releases are the verified binary channel (Linux, macOS and Windows
    archives with checksums): `https://github.com/madfam-org/enclii/releases`.
    Homebrew, Scoop and `get.enclii.dev` are convenience targets, not yet
    monitored.

    ```bash
    # From source (any OS with Go)
    git clone https://github.com/madfam-org/enclii.git
    cd enclii && make build-cli && ./bin/enclii --version
    ```

    ### Auth

    ```bash
    enclii login                  # browser SSO (Janua)
    enclii whoami                 # verify active session
    enclii logout                 # clear local creds
    ```

    Global flags: `--api-endpoint` (or `ENCLII_API_ENDPOINT`, default
    `https://api.enclii.dev`) and `--api-token` (or `ENCLII_API_TOKEN`; legacy
    `ENCLII_TOKEN` is still read) for non-interactive use. Set
    `ENCLII_PROJECT=<project-slug>` (or pass `--project`) when a command has to
    resolve a service name.

    ### Day-to-day for {SERVICE}

    The commands below default to `{SERVICE}` — the primary service name for
    this repo as registered in Switchyard. For any other service in the
    ecosystem, swap the name. Environments are `dev`, `staging` and `prod`;
    most commands default to `dev`, so pass `--env prod` for production.

    ```bash
    # Status
    enclii ps --env prod

    # Logs
    enclii logs {SERVICE} --env prod -f                    # live tail
    enclii logs {SERVICE} --env prod --since 1h -n 200     # last hour

    # Deploy (reads service.yaml)
    enclii deploy --env staging --wait
    enclii deploy --env prod --canary 10% --change-ticket <url>

    # Rollback
    enclii rollback {SERVICE} --env prod                   # previous release
    enclii rollback {SERVICE} v42 --env prod

    # Releases + deployment history
    enclii releases {SERVICE} -n 20
    enclii deployments list

    # Secrets (routed through Lockbox → Vault → ESO → K8s)
    enclii secrets list --env prod
    enclii secrets set MY_KEY=value --secret --env prod

    # Chat-safe operator intake (values never pass through agent chat)
    enclii secrets intake submit <target> --reason "<audit reason>" --stdin
    enclii secrets intake status <intake-id>

    # Domains, tunnel routes, DNS
    enclii domains list --service {SERVICE}
    enclii domains add my.example.com --service {SERVICE}   # auto-provisions tunnel route + DNS

    # Scheduled jobs, routing, serverless
    enclii jobs list --project <project-slug>
    enclii junctions list --project <project-slug>
    enclii functions list

    # Observability
    enclii observe health --service <service-id>

    # Local dev environment
    enclii local up         # spin up dependent services (postgres, redis, …)
    enclii local logs
    enclii local down
    ```

    ### Full onboarding (only used when adding a brand-new service)

    ```bash
    # One-shot: namespace + ArgoCD app + tunnel routes + Janua client + netpol
    enclii onboard --repo madfam-org/<name> --db-name <db> --secrets-file .env
    ```

    ### Enclii-first production operations

    Enclii is the required control plane for routine production operations.
    Use the web UI, API, or CLI before reaching for raw infrastructure tools:

    - ArgoCD sync / diff / rollback — `enclii ops apps ...`
    - Pod logs, diagnosis, and safe restarts — `enclii ops pods ...`
    - Longhorn / PVC / PV inspection and repair planning — `enclii ops storage ...`
    - Kyverno violations and time-bound waivers — `enclii ops policy ...`
    - ExternalSecrets and Vault readiness — `enclii ops secrets ...`
    - ARC runner inspection and drain workflows — `enclii ops runners ...`
    - DNS, tunnels, SaaS hostnames, providers, and repo automation — `enclii providers ...`
    - Service lifecycle, domains, secrets, jobs, and observability — `enclii deploy`, `enclii rollback`, `enclii logs`, `enclii observe`, `enclii domains`, `enclii secrets`, `enclii jobs`

    ### Break-glass-only access

    Raw `kubectl`, `helm`, SSH, provider CLIs/APIs, `docker exec`, and direct
    container access are allowed only for platform bootstrap or documented
    break-glass emergencies when Enclii is unavailable or lacks an implemented
    adapter. Record the actor, reason, target service/environment, commands
    executed, result, and follow-up Enclii adapter gap or incident link.

    ### Cluster access

    kubeconfig + SSH keys live in `madfam-org/internal-devops` (private repo)
    for bootstrap and break-glass use only. Routine production operations must
    go through Enclii web, API, or CLI.

    ### Exit codes (scripting against the CLI)

    | Code | Meaning          |
    | ---- | ---------------- |
    | 0    | success          |
    | 10   | validation error |
    | 20   | build failed     |
    | 30   | deploy failed    |
    | 40   | timeout          |
    | 50   | auth error       |
""").strip()


PROVENANCE = dedent("""
    Rendered by `madfam-org/enclii/docs/templates/ecosystem/generator.py` from this
    repo's metadata entry (plus any private overlay kept in this repo) and the
    public product-registry projection. First generated 2026-04-23 for the "each
    repo stands alone" docs sweep. Do not hand-edit this file: change the metadata,
    overlay or registry and re-render. `generator.py --check <repo-path>` fails when
    this file differs from what the generator would write.
""").strip()


# ---------------------------------------------------------------------------
# Render
# ---------------------------------------------------------------------------

def render(repo: str, meta: dict, projection: dict | None = None) -> str:
    """Render one repo's ECOSYSTEM.md.

    `projection` is the parsed product-registry projection; when omitted it is
    loaded from the default location (see registry.py) and a missing file is
    an error, never a silent fallback.
    """
    if projection is None:
        projection = load_projection()
    services = meta.get("production", {}).get("services", [])
    ns = meta.get("production", {}).get("namespace", "(see enclii ps)")
    service_for_ops = meta.get("service_name_for_ops", repo)

    svc_table = "_(no deployed services — this repo is a library/tool.)_\n"
    if services:
        rows = [[f"`{name}`", str(domain), str(port) if port else "—"] for name, domain, port in services]
        svc_table = md_table(["Service", "Public domain", "Container port"], rows) + "\n"

    deps = meta.get("upstream_deps", [])
    consumers = meta.get("downstream_consumers", [])
    env_vars = meta.get("key_env", [])

    deps_md = "\n".join(f"- {d}" for d in deps) if deps else "_(none)_"
    consumers_md = "\n".join(f"- {c}" for c in consumers) if consumers else "_(none)_"
    # An entry that already carries its own code spans renders as written.
    env_md = (
        "\n".join(f"- {e}" if "`" in e else f"- `{e}`" for e in env_vars)
        if env_vars
        else "_(see repo README / .env.example)_"
    )

    blocks = apply_boilerplate_overrides(
        repo,
        {
            "map": render_ecosystem_map(projection),
            "cli": ENCLII_CLI_REF.replace("{SERVICE}", service_for_ops),
        },
        meta.get("boilerplate_overrides"),
    )
    ecosystem_map = blocks["map"]
    cli_ref = blocks["cli"]

    # Optional curated slots. Every one of them is empty by default, and an
    # empty slot contributes nothing to the output — a repo that declares none
    # renders byte-identically to a render from before they existed.
    header = [f"# {repo} — Ecosystem Context"]
    sensitivity_banner = (meta.get("sensitivity_banner") or "").strip()
    if sensitivity_banner:
        header.append(sensitivity_banner)
    header.append(LEGACY_RAW_BANNER)
    boundary_checkpoint = (meta.get("boundary_checkpoint") or "").strip()
    if boundary_checkpoint:
        header.append(boundary_checkpoint)
    header.append(f"> **{meta.get('tagline', '').strip()}**")
    header_md = "\n\n".join(header)

    cluster = "**Cluster**: bare-metal k3s (see topology section below)."
    production_truth = (meta.get("production_truth") or "").strip()
    if production_truth:
        cluster = f"{cluster}\n\n{production_truth}"

    provenance = PROVENANCE
    provenance_note = (meta.get("provenance_note") or "").strip()
    if provenance_note:
        provenance = f"{provenance}\n\n{provenance_note}"

    return f"""{header_md}

This file is self-contained: a Claude session on a fresh machine can operate
this service by reading only this one document. No external links are
load-bearing — the MADFAM ecosystem map and the full enclii CLI reference are
embedded below.

---

## 1. What this repo is

{meta.get("description", "").strip()}

**Pillar**: {meta.get("pillar", "—")}
**Type**: {meta.get("type", "—")}
**Status**: {meta.get("status", "—")}
{render_registry_entry(projection, repo)}

### Deployed services

{svc_table}
**Kubernetes namespace**: `{ns}`
{cluster}

### Upstream dependencies (this repo consumes)

{deps_md}

### Downstream consumers (this repo is consumed by)

{consumers_md}

### Key environment variables

{env_md}

---

{ecosystem_map}

---

{cli_ref}

---

## Document provenance

{provenance}
"""


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def _repo_name(path: Path, explicit: str | None) -> str:
    return explicit or path.resolve().name


def _repos_for_checkout(path: Path) -> dict:
    """Public metadata + env overlays + the checkout's own overlay, if any."""
    overlays = [os.environ.get(OVERLAY_ENV, "")]
    local = path / REPO_OVERLAY_RELPATH
    if local.is_file():
        overlays.append(str(local))
    return load_repos(os.pathsep.join(o for o in overlays if o))


def check_checkout(path: Path, projection: dict, repo: str | None = None) -> tuple[int, str]:
    """Render `path`'s ECOSYSTEM.md in memory and diff it against the file.

    Returns (status, report): 0 identical, 1 drifted, 2 undetermined.
    """
    name = _repo_name(path, repo)
    target = path / "ECOSYSTEM.md"
    repos = _repos_for_checkout(path)
    if name not in repos:
        return 2, f"UNDETERMINED {name}: no metadata entry (add it to a metadata_<pillar>.py module)"
    if not target.is_file():
        return 2, f"UNDETERMINED {name}: {target} does not exist"
    expected = render(name, repos[name], projection)
    actual = target.read_text(encoding="utf-8")
    if expected == actual:
        return 0, f"OK {name}: ECOSYSTEM.md matches the generator"
    diff = difflib.unified_diff(
        actual.splitlines(keepends=True),
        expected.splitlines(keepends=True),
        fromfile=f"{name}/ECOSYSTEM.md (checked in)",
        tofile=f"{name}/ECOSYSTEM.md (generator)",
    )
    return 1, f"DRIFT {name}: ECOSYSTEM.md differs from the generator output\n" + "".join(diff)


def write_checkout(path: Path, projection: dict, repo: str | None = None) -> str:
    name = _repo_name(path, repo)
    repos = _repos_for_checkout(path)
    if name not in repos:
        raise SystemExit(f"{name}: no metadata entry (add it to a metadata_<pillar>.py module)")
    out = path / "ECOSYSTEM.md"
    out.write_text(render(name, repos[name], projection), encoding="utf-8")
    return f"WROTE {out} ({out.stat().st_size:,} bytes)"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Render or drift-check ECOSYSTEM.md files.")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--check", nargs="+", metavar="PATH", help="diff each checkout; write nothing")
    mode.add_argument("--write", nargs="+", metavar="PATH", help="render into each checkout")
    parser.add_argument("--projection", help="path to projection.public.json")
    parser.add_argument("--repo", help="metadata key, when one PATH's directory name differs")
    parser.add_argument("names", nargs="*", help="repo names rendered into $MADFAM_LABSPACE/<name>")
    args = parser.parse_args(argv)

    paths = args.check or args.write
    if paths and args.names:
        parser.error("repo names and --check/--write paths are mutually exclusive")
    if args.repo and (not paths or len(paths) != 1):
        parser.error("--repo applies to exactly one --check/--write PATH")

    try:
        projection = load_projection(args.projection)
    except ProjectionError as error:
        print(f"UNDETERMINED: {error}", file=sys.stderr)
        return 2

    if args.check:
        worst = 0
        for raw in args.check:
            status, report = check_checkout(Path(raw), projection, args.repo)
            print(report, file=sys.stderr if status else sys.stdout)
            worst = max(worst, status)
        return worst

    if args.write:
        for raw in args.write:
            print(write_checkout(Path(raw), projection, args.repo))
        return 0

    repos = load_repos()
    for repo in args.names or list(repos.keys()):
        if repo not in repos:
            print(f"SKIP {repo} — no metadata defined")
            continue
        out = LABSPACE / repo / "ECOSYSTEM.md"
        out.write_text(render(repo, repos[repo], projection), encoding="utf-8")
        print(f"WROTE {out} ({out.stat().st_size:,} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
