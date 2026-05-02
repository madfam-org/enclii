#!/usr/bin/env python3
"""
ECOSYSTEM.md generator — renders per-repo self-contained docs that embed
the MADFAM ecosystem map + full enclii CLI reference so any repo can be
operated from its own ECOSYSTEM.md alone.

Usage (from labspace root):

    python3 docs/templates/ecosystem/generator.py                 # regenerate everything
    python3 docs/templates/ecosystem/generator.py enclii janua    # regenerate specific repos
"""
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
    | **Selva** | `madfam-org/autoswarm-office` | LLM inference routing + agent orchestration |
    | **Karafiel** | `madfam-org/karafiel` | Operational compliance — CFDI, NOM-151, e.firma, SAT-adjacent. Owns legal-ops / contract templates |
    | **Tezca** | `madfam-org/tezca` | Mexican law oracle (informational only — feeds Karafiel) |
    | **Cotiza** | `madfam-org/digifab-quoting` | MADFAM's quoting engine (fabrication + services) |
    | **Forgesight** | `madfam-org/forgesight` | Digital fabrication industry intelligence (pricing/vendor feed to Cotiza) |
    | **Pravara MES** | `madfam-org/pravara-mes` | Fabrication-node routing and dispatch (physical jobs) |
    | **PhyneCRM** | `madfam-org/phyne-crm` | Client-facing deliverables portal (single pane of glass per engagement) |
    | **Fortuna** | `madfam-org/fortuna` | Problem intelligence / zeitgeist analysis |
    | **Avala** | `madfam-org/avala` | Learning verification platform |

    ### Cross-repo conventions

    - **Auth**: every authenticated service verifies Janua JWTs via JWKS at
      `https://auth.madfam.io/.well-known/jwks.json`. RS256 only — HS256 is
      fail-closed after the 2026-04-23 audit (H3/H4).
    - **Billing**: credit metering + entitlements flow through Dhanam. See
      `madfam-org/dhanam` for the meter/entitlement/invoice APIs.
    - **Inference**: every LLM call should route through Selva
      (`autoswarm-office`) at `/v1` (OpenAI-compatible). Do not talk directly
      to OpenAI / Anthropic from service code.
    - **CORS**: explicit allowlist per service. Wildcards are banned
      (audit 2026-04-23 H2/H5/H6).
    - **Images**: `@sha256:`-pinned in every manifest. Kyverno fail-closes on
      `:latest` or mutable tags.
    - **Onboarding**: `POST /v1/admin/onboard` on switchyard-api creates
      namespace, ArgoCD app, Cloudflare tunnel routes, Janua client, and
      NetworkPolicies in one shot. See `enclii/docs/guides/ONBOARDING_GUIDE.md`.

    ### Production topology

    Bare-metal k3s (v1.33+) on Hetzner, 3 nodes:

    - `foundry-cp` (Hetzner EX44, 14C/20T, 128 GB) — control-plane + primary workload
    - `foundry-worker-01` (Hetzner AX41-NVMe, Ryzen 5 3600, 64 GB) — worker + Longhorn 2nd replica
    - `foundry-builder-01` (Hetzner VPS, 2 vCPU, 4 GB, tainted `builder=true:NoSchedule`) — ARC runners only

    **Ingress**: Cloudflare Tunnel → 2× cloudflared pods → K8s ClusterIP → container port.
    Zero exposed node ports. TLS terminated at Cloudflare edge.

    **Storage**: Longhorn CSI v1.7+ in 2-replica mode across dedicated nodes.
    Object storage: Cloudflare R2 (zero egress).

    **GitOps**: ArgoCD App-of-Apps (~28 apps across ~22 namespaces) with self-heal.
    Push to `main` → CI builds → GHCR → `kustomize edit set image` commits digest →
    ArgoCD syncs → Switchyard tracks lifecycle events.

    **Operational access** (SSH, kubeconfigs, server IPs, cost ledger): private repo
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

    ### When to use kubectl (escape hatches)

    The enclii CLI routes through Switchyard. These operations don't yet have
    a CLI equivalent — kubectl is the right tool:

    - ArgoCD sync / patch — `kubectl patch application <app> -n argocd --type merge ...`
    - Kyverno PolicyExceptions + raw CRD management
    - Longhorn / PVC operations — `kubectl get volumes.longhorn.io -n longhorn-system`
    - Direct pod exec for debugging — `kubectl exec -n <ns> deploy/<svc> -- ...`
    - Raw port-forward — `kubectl port-forward -n <ns> svc/<svc> 8080:80`
    - Janua DB ops (no enclii equivalent)

    ### Cluster access

    kubeconfig + SSH keys live in `madfam-org/internal-devops` (private repo).
    On a fresh machine, pull that repo first to get `~/.kube/config-hetzner`.

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

    cli_ref = ENCLII_CLI_REF.replace("{SERVICE}", service_for_ops)

    return f"""# {repo} — Ecosystem Context

> **{meta.get("tagline", "").strip()}**

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
**Cluster**: bare-metal k3s on Hetzner (see topology section below).

### Upstream dependencies (this repo consumes)

{deps_md}

### Downstream consumers (this repo is consumed by)

{consumers_md}

### Key environment variables

{env_md}

---

{ECOSYSTEM_MAP}

---

{cli_ref}

---

## Document provenance

Generated 2026-04-23 as part of the "each repo stands alone" docs sweep. The
generator and per-repo metadata live at `madfam-org/enclii/docs/templates/ecosystem/`.
Re-render (don't hand-edit per-repo copies) when the ecosystem map or CLI
reference needs to update across the fleet.
"""


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    targets = sys.argv[1:] or list(REPOS_FULL.keys())
    for repo in targets:
        if repo not in REPOS_FULL:
            print(f"SKIP {repo} — no metadata defined")
            continue
        out = LABSPACE / repo / "ECOSYSTEM.md"
        out.write_text(render(repo, REPOS_FULL[repo]))
        print(f"WROTE {out} ({len(out.read_text()):,} bytes)")
