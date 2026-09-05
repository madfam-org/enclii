#!/usr/bin/env python3
"""
ECOSYSTEM.md generator — renders per-repo self-contained docs that embed
the MADFAM ecosystem map + full enclii CLI reference so any repo can be
operated from its own ECOSYSTEM.md alone.

Usage (from labspace root):

    python3 docs/templates/ecosystem/generator.py                 # regenerate everything
    python3 docs/templates/ecosystem/generator.py enclii janua    # regenerate specific repos
"""
import json
import os
import sys
from pathlib import Path
from textwrap import dedent

# Package-relative import with standalone fallback.
try:
    from .metadata import REPOS_FULL
except ImportError:
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    from metadata import REPOS_FULL  # type: ignore

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
# ---------------------------------------------------------------------------

ECOSYSTEM_MAP = dedent("""
    ## MADFAM Ecosystem Map

    MADFAM runs ~40 services on sovereign bare-metal infrastructure. Everything
    below is embedded here so this document stands alone.

    ### The platforms every repo should know about

    | Platform | Repo | Role |
    |---|---|---|
    | **Enclii** | `madfam-org/enclii` | PaaS control plane — all deploys go through this |
    | **Janua** | `madfam-org/janua` | OIDC/OAuth 2.0 provider — RS256 JWKS at `auth.madfam.io/.well-known/jwks.json` |
    | **Dhanam** | `madfam-org/dhanam` | Billing + payment gateways (Stripe, Mercado Pago, SPEI, etc.) |
    | **Selva** | `madfam-org/selva-office` | LLM inference routing + agent orchestration |
    | **Karafiel** | `madfam-org/karafiel` | Operational compliance — CFDI, NOM-151, e.firma, SAT-adjacent. Owns legal-ops / contract templates |
    | **Tezca** | `madfam-org/tezca` | Mexican law oracle (informational only — feeds Karafiel) |
    | **Cotiza** | `madfam-org/digifab-quoting` | MADFAM's quoting engine (fabrication + services) |
    | **Forgesight** | `madfam-org/forgesight` | Digital fabrication industry intelligence (pricing/vendor feed to Cotiza) |
    | **Pravara MES** | `madfam-org/pravara-mes` | Fabrication-node routing and dispatch (physical jobs) |
    | **PhyndCRM** | `madfam-org/phynd-crm` | Client-facing deliverables portal (single pane of glass per engagement) |
    | **Fortuna** | `madfam-org/fortuna` | Problem intelligence / zeitgeist analysis |
    | **Avala** | `madfam-org/avala` | Learning verification platform |

    ### Cross-repo conventions

    - **Auth**: every authenticated service verifies Janua JWTs via JWKS at
      `https://auth.madfam.io/.well-known/jwks.json`. RS256 only — HS256 is
      fail-closed after the 2026-04-23 audit (H3/H4).
    - **Billing**: credit metering + entitlements flow through Dhanam. See
      `madfam-org/dhanam` for the meter/entitlement/invoice APIs.
    - **Inference**: every LLM call should route through Selva
      (`selva-office`) at `/v1` (OpenAI-compatible). Do not talk directly
      to OpenAI / Anthropic from service code.
    - **CORS**: explicit allowlist per service. Wildcards are banned
      (audit 2026-04-23 H2/H5/H6).
    - **Images**: `@sha256:`-pinned in every manifest. Kyverno fail-closes on
      `:latest` or mutable tags.
    - **Onboarding**: `POST /v1/admin/onboard` on switchyard-api creates
      namespace, ArgoCD app, Cloudflare tunnel routes, Janua client, and
      NetworkPolicies in one shot. See `enclii/docs/guides/ONBOARDING_GUIDE.md`.

    ### Production topology

    Bare-metal k3s (v1.33+), 3 nodes. Roles only — this generator emits node
    ROLES and never node hostnames, IPs or hardware SKUs, because every repo it
    writes into is public and `ECOSYSTEM.md` is copied verbatim across all of
    them (2026-07-16 exposure class 1). Node identity lives only in
    `madfam-org/internal-devops`.

    - control-plane node (dedicated bare-metal) — control-plane + primary workload
    - worker node (dedicated bare-metal) — worker + Longhorn 2nd replica
    - builder node (cloud compute instance, labelled `role=builder`, tainted
      `builder=true:NoSchedule`) — ARC runners only

    **Ingress**: Cloudflare Tunnel → 2× cloudflared pods → K8s ClusterIP → container port.
    Zero exposed node ports. TLS terminated at Cloudflare edge.

    **Storage**: Longhorn CSI v1.7+ in 2-replica mode across dedicated nodes.
    Object storage: Cloudflare R2 (zero egress).

    **GitOps**: ArgoCD App-of-Apps (~28 apps across ~22 namespaces) with self-heal.
    Push to `main` → CI builds → GHCR → `kustomize edit set image` commits digest →
    ArgoCD syncs → Switchyard tracks lifecycle events.

    **Operational access** (SSH, kubeconfigs, node hostnames, server IPs, hardware
    SKUs, cost ledger): private repo
    `madfam-org/internal-devops`. Not in any public repo.
""").strip()


ENCLII_CLI_REF = dedent("""
    ## Enclii CLI — DevOps Reference

    **Strong preference: use `enclii` over `kubectl`** for all operational
    tasks. The CLI routes through Switchyard API, which gives you audit
    logging, lifecycle event tracking, and service-scoped context. Escape
    to kubectl only for the gaps listed at the end of this section.

    ### Install

    ```bash
    # macOS
    brew install enclii/tap/enclii

    # Linux / from source (any OS with Go 1.22+)
    git clone https://github.com/madfam-org/enclii.git
    cd enclii && make install-cli

    # Build only (no install)
    make build-cli && ./bin/enclii version
    ```

    ### Auth

    ```bash
    enclii login                  # browser SSO (Janua)
    enclii whoami                 # verify active session
    enclii logout                 # clear local creds
    ```

    Env vars: `ENCLII_API_URL` (default `https://api.enclii.dev`),
    `ENCLII_TOKEN` (alternative to interactive login),
    `ENCLII_PROJECT`, `ENCLII_ENV`.

    ### Day-to-day for {SERVICE}

    The commands below default to `{SERVICE}` — the primary service name for
    this repo as registered in Switchyard. For any other service in the
    ecosystem, swap the name.

    ```bash
    # Status + where the pods are running
    enclii ps --wide
    enclii ps {SERVICE} --env production

    # Logs (tail, filter, history)
    enclii logs {SERVICE} -f                          # live tail
    enclii logs {SERVICE} --since 1h --level error    # last hour, errors only
    enclii logs {SERVICE} --env staging -f

    # Deploy (preview, staging, production)
    enclii deploy --env preview                       # from current branch
    enclii deploy --env staging
    enclii deploy --env production --strategy canary --canary-percent 10

    # Rollback
    enclii rollback {SERVICE}                         # previous release
    enclii rollback {SERVICE} --to-revision 5

    # Releases + history
    enclii releases {SERVICE}                          # list builds
    enclii releases {SERVICE} --latest --output json

    # Secrets (routed through Lockbox -> Vault -> ESO -> K8s)
    enclii secrets list {SERVICE}
    enclii secrets set MY_KEY=value --service {SERVICE} --secret
    enclii secrets rm MY_KEY --service {SERVICE}

    # Domains, tunnel routes, DNS
    enclii domains list {SERVICE}
    enclii domains add {SERVICE} my.example.com       # auto-provisions tunnel route + DNS

    # Scheduled jobs (cron + one-off)
    enclii jobs list
    enclii jobs run <job-name>                         # trigger one-off

    # Routing (ingress + TLS)
    enclii junctions list {SERVICE}

    # Serverless (scale-to-zero functions)
    enclii functions list

    # Local dev environment
    enclii local up         # spin up dependent services (postgres, redis, ...)
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

    | Code | Meaning |
    |---|---|
    | 0  | success |
    | 10 | validation error |
    | 20 | build failed |
    | 30 | deploy failed |
    | 40 | timeout |
    | 50 | auth error |
""").strip()


PROVENANCE = dedent("""
    Generated 2026-04-23 as part of the "each repo stands alone" docs sweep. The
    generator and per-repo metadata live at `madfam-org/enclii/docs/templates/ecosystem/`.
    Re-render (don't hand-edit per-repo copies) when the ecosystem map or CLI
    reference needs to update across the fleet.
""").strip()


# ---------------------------------------------------------------------------
# Render
# ---------------------------------------------------------------------------

def render(repo: str, meta: dict) -> str:
    services = meta.get("production", {}).get("services", [])
    ns = meta.get("production", {}).get("namespace", "(see enclii ps)")
    service_for_ops = meta.get("service_name_for_ops", repo)

    svc_table = "_(no deployed services — this repo is a library/tool.)_\n"
    if services:
        rows = []
        for name, domain, port in services:
            port_s = str(port) if port else "—"
            rows.append(f"| `{name}` | {domain} | {port_s} |")
        svc_table = (
            "| Service | Public domain | Container port |\n"
            "|---|---|---|\n" + "\n".join(rows) + "\n"
        )

    deps = meta.get("upstream_deps", [])
    consumers = meta.get("downstream_consumers", [])
    env_vars = meta.get("key_env", [])

    deps_md = "\n".join(f"- {d}" for d in deps) if deps else "_(none)_"
    consumers_md = "\n".join(f"- {c}" for c in consumers) if consumers else "_(none)_"
    env_md = "\n".join(f"- `{e}`" for e in env_vars) if env_vars else "_(see repo README / .env.example)_"

    blocks = apply_boilerplate_overrides(
        repo,
        {
            "map": ECOSYSTEM_MAP,
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

    cluster = "**Cluster**: bare-metal k3s on Hetzner (see topology section below)."
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

if __name__ == "__main__":
    repos = load_repos()
    targets = sys.argv[1:] or list(repos.keys())
    for repo in targets:
        if repo not in repos:
            print(f"SKIP {repo} — no metadata defined")
            continue
        out = LABSPACE / repo / "ECOSYSTEM.md"
        out.write_text(render(repo, repos[repo]))
        print(f"WROTE {out} ({len(out.read_text()):,} bytes)")
