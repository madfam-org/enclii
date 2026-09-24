"""Product-registry input for the ECOSYSTEM.md generator.

Every fact in the rendered "MADFAM Ecosystem Map" that describes WHICH products
exist — display name, repo, public front door, lifecycle, retirement — comes
from the public projection of the product registry, never from text typed into
this generator. The registry itself (`internal-devops/ecosystem/registry/
products.yaml`) is private; its allow-listed public projection is vendored in
the public foundry, so this public generator never reads a private repo:

    madfam-org/solarpunk-foundry
      packages/core/src/products/projection.public.json
      (schema `madfam-product-projection/v1`)

Resolution order for the projection path: the explicit argument (`--projection`
on the command line), then `MADFAM_PRODUCT_PROJECTION`, then
`$MADFAM_LABSPACE/solarpunk-foundry/...`, then the solarpunk-foundry checkout
next to this enclii checkout. A missing or malformed
projection is a hard failure — there is deliberately no built-in fallback map,
because a fallback is exactly how the stale 12-platform table outlived the
registry by five months.

What is NOT in the projection and therefore still lives here:

* `PLATFORM_ROLES` — the one-line cross-repo contract for the products other
  repos integrate with (e.g. "RS256 JWKS at ..."). The registry has no role
  field today. A product without an entry falls back to the registry's own
  `site.banner_keyword`, else `—`. If the registry grows a public
  `role` (top level or under `site`), it wins over this table automatically.
  Every key here must name a live product in the projection; the render fails
  otherwise, so a retired product cannot keep a role line.
"""
from __future__ import annotations

import json
import os
import unicodedata
from pathlib import Path

PROJECTION_ENV = "MADFAM_PRODUCT_PROJECTION"
PROJECTION_SCHEMA = "madfam-product-projection/v1"
PROJECTION_RELPATH = Path("solarpunk-foundry/packages/core/src/products/projection.public.json")

# Cross-repo contract per product slug. Roles only, no counts, no hosts that the
# registry already carries (the front door column comes from the registry).
PLATFORM_ROLES: dict[str, str] = {
    "enclii": "PaaS control plane — every deploy goes through it",
    "janua": "OIDC/OAuth 2.0 identity provider — RS256 JWKS at `auth.madfam.io/.well-known/jwks.json`",
    "selva": "LLM inference gateway (OpenAI-compatible `/v1`) + agent orchestration",
    "forgesight": "Digital-fabrication industry intelligence (pricing/vendor feed to Cotiza)",
    "dhanam": "Billing, entitlements and payment gateways (Stripe, Mercado Pago, SPEI)",
    "fortuna": "Problem intelligence / zeitgeist analysis",
    "karafiel": "Operational compliance — CFDI, NOM-151, e.firma; owns legal-ops templates",
    "tezca": "Mexican law oracle (informational only — feeds Karafiel)",
    "avala": "Learning and competency verification",
    "cotiza": "Quoting engine (fabrication + services)",
    "pravara-mes": "Fabrication routing and dispatch (physical jobs)",
    # Ruling R48 (2026-09-23 coherence audit).
    "phynd-crm": "CRM — consent, campaigns, attribution",
    "ceq": "Generative asset pipeline — ComfyUI wrapper behind `/v1/render`",
    "kalya": "Booking and scheduling",
    "symbiosis": "Human capital management — Mexican payroll",
    "nauta": "Fractional CTO practice — staff cockpit and per-client ERP workspaces",
    "tlacuilo": "Document intelligence (OCR)",
}


class ProjectionError(Exception):
    """The projection could not be used. The CLI maps this to exit 2 (UNDETERMINED)."""


def default_projection_paths() -> list[Path]:
    """`$MADFAM_LABSPACE/solarpunk-foundry/...`, then the foundry checkout that
    sits next to the enclii checkout this file lives in. The second covers
    callers that point MADFAM_LABSPACE at a scratch output directory (tulana's
    scripts/render-ecosystem-md.sh does)."""
    labspace = Path(os.environ.get("MADFAM_LABSPACE", "/Users/aldoruizluna/labspace"))
    sibling = Path(__file__).resolve().parents[4] / PROJECTION_RELPATH
    return [labspace / PROJECTION_RELPATH] + ([sibling] if sibling != labspace / PROJECTION_RELPATH else [])


def resolve_projection_path(explicit: str | os.PathLike | None = None) -> Path:
    if explicit:
        return Path(explicit)
    env = os.environ.get(PROJECTION_ENV, "").strip()
    if env:
        return Path(env)
    candidates = default_projection_paths()
    return next((c for c in candidates if c.is_file()), candidates[0])


def load_projection(path: str | os.PathLike | None = None) -> dict:
    """Read and validate the public projection. Fails loudly, never falls back."""
    resolved = resolve_projection_path(path)
    try:
        document = json.loads(resolved.read_text(encoding="utf-8"))
    except FileNotFoundError:
        raise ProjectionError(
            f"product projection not found at {resolved}.\n"
            f"  Pass --projection PATH, set {PROJECTION_ENV}, or check out "
            "madfam-org/solarpunk-foundry next to this repo (MADFAM_LABSPACE)."
        ) from None
    except json.JSONDecodeError as error:
        raise ProjectionError(f"product projection at {resolved} is not valid JSON: {error}") from None
    validate_projection(document, source=str(resolved))
    return document


def validate_projection(document: dict, *, source: str = "projection") -> None:
    if not isinstance(document, dict) or document.get("schema") != PROJECTION_SCHEMA:
        found = document.get("schema") if isinstance(document, dict) else type(document).__name__
        raise ProjectionError(f"{source}: expected schema {PROJECTION_SCHEMA!r}, found {found!r}")
    products = document.get("products")
    if not isinstance(products, list) or not products:
        raise ProjectionError(f"{source}: `products` must be a non-empty list")
    slugs = set()
    for product in products:
        for key in ("slug", "display_name", "lifecycle", "domains", "site"):
            if key not in product:
                raise ProjectionError(f"{source}: product {product.get('slug', '?')!r} lacks {key!r}")
        slugs.add(product["slug"])
    unknown = sorted(set(PLATFORM_ROLES) - slugs)
    if unknown:
        raise ProjectionError(
            f"{source}: PLATFORM_ROLES names products the registry does not list: {unknown}. "
            "A product left the registry (retired or renamed) — drop or rename its role line."
        )


def product_role(product: dict) -> str:
    site = product.get("site") or {}
    explicit = product.get("role") or site.get("role")
    if explicit:
        return str(explicit).strip()
    if product["slug"] in PLATFORM_ROLES:
        return PLATFORM_ROLES[product["slug"]]
    keyword = (site.get("banner_keyword") or "").strip()
    # The category is already the table's heading, so it is not repeated here.
    return keyword.capitalize() if keyword else "—"


def _display_width(text: str) -> int:
    """Terminal width as prettier's `getStringWidth` counts it (wide/fullwidth = 2)."""
    width = 0
    for char in text:
        if unicodedata.combining(char):
            continue
        width += 2 if unicodedata.east_asian_width(char) in ("W", "F") else 1
    return width


def md_table(header: list[str], rows: list[list[str]]) -> str:
    """A Markdown table in prettier's aligned form.

    Several fleet repos run `prettier --check` over Markdown (digifab-quoting,
    primavera3d), and prettier re-pads every table it sees. Emitting the padded
    form here keeps a render byte-identical after prettier, so `--check` and a
    repo's formatter can never disagree about the same file.
    """
    table = [header] + rows
    widths = [max(3, *(_display_width(row[i]) for row in table)) for i in range(len(header))]

    def line(cells: list[str]) -> str:
        padded = [cell + " " * (widths[i] - _display_width(cell)) for i, cell in enumerate(cells)]
        return "| " + " | ".join(padded) + " |"

    delimiter = "| " + " | ".join("-" * width for width in widths) + " |"
    return "\n".join([line(header), delimiter] + [line(row) for row in rows])


def _repo_cell(product: dict) -> str:
    repo = product.get("repo") or {}
    if not repo.get("name"):
        return "—"
    return f"`{repo.get('github_org', 'madfam-org')}/{repo['name']}`"


def _front_door(product: dict) -> str:
    primary = (product.get("domains") or {}).get("primary")
    return primary or "—"


def products_for_repo(projection: dict, repo: str) -> list[dict]:
    return [p for p in projection["products"] if (p.get("repo") or {}).get("name") == repo]


def render_registry_entry(projection: dict, repo: str) -> str:
    """One line for section 1: which registry product(s) this repo ships."""
    products = products_for_repo(projection, repo)
    if not products:
        return "**Registry product**: none — this repo is not a customer-facing product in the registry."
    parts = []
    for product in products:
        door = _front_door(product)
        door_s = f", front door {door}" if door != "—" else ""
        parts.append(f"{product['display_name']} (`{product['slug']}`, {product['lifecycle']}{door_s})")
    label = "Registry product" if len(parts) == 1 else "Registry products"
    return f"**{label}**: " + "; ".join(parts) + "."


def render_platform_map(projection: dict) -> str:
    """The product table, grouped by registry category, in registry order."""
    groups: dict[str, list[dict]] = {}
    for product in projection["products"]:
        category = (product.get("site") or {}).get("category") or "Other"
        groups.setdefault(category, []).append(product)

    sections = []
    for category, products in groups.items():
        rows = [
            [
                f"**{product['display_name']}**",
                _repo_cell(product),
                _front_door(product),
                product["lifecycle"],
                product_role(product),
            ]
            for product in products
        ]
        table = md_table(["Product", "Repo", "Front door", "Lifecycle", "Role"], rows)
        sections.append(f"#### {category}\n\n{table}")
    return "\n\n".join(sections)


def render_retired(projection: dict) -> str:
    retired = projection.get("retired") or []
    if not retired:
        return "_(the registry records no retired products)_"
    names = {p["slug"]: p["display_name"] for p in projection["products"]}
    rows = []
    for entry in retired:
        successor = entry.get("successor_slug")
        successor_s = names.get(successor, successor) if successor else "—"
        rows.append([entry["display_name"], entry["retired_on"], successor_s, entry.get("redirect_to") or "none"])
    return md_table(["Product", "Retired on", "Successor", "Redirect"], rows)
