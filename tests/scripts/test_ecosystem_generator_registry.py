"""
Tests for the registry-sourced ECOSYSTEM.md generator and its `--check` mode.

Run with:
    pytest tests/scripts/test_ecosystem_generator_registry.py -v

What these pin (coherence audit 2026-09-23, ruling R39, findings E-002/E-011):

* The product tables come from the public product-registry projection, not
  from text typed into the generator. A product added to the projection shows
  up in every render; a missing projection is a hard failure, never a silent
  fallback to a built-in map.
* No estate count is typed into a render ("~40 services", "~28 apps",
  "3 nodes" survived in 36 repos for five months). The topology says 4 nodes,
  by ROLE only, and no render carries an IPv4 literal.
* PhyndCRM's role is the ruled line (R48).
* `generator.py --check <repo-path>` renders in memory and diffs: 0 match,
  1 drift, 2 undetermined.
* Tables are emitted in prettier's aligned form so repos that run
  `prettier --check` on Markdown never disagree with `--check`.

`fixtures/ecosystem-projection.public.json` is a frozen copy of
`solarpunk-foundry/packages/core/src/products/projection.public.json` (registry
v4, 2026-09-23). Tests that need a specific shape build their own document.
"""
from __future__ import annotations

import copy
import json
import re
import sys
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
GENERATOR_DIR = REPO_ROOT / "docs" / "templates" / "ecosystem"
FIXTURE = Path(__file__).parent / "fixtures" / "ecosystem-projection.public.json"

sys.path.insert(0, str(GENERATOR_DIR))

import generator  # noqa: E402
import registry  # noqa: E402
from metadata import REPOS_FULL  # noqa: E402

PROJECTION = registry.load_projection(FIXTURE)
IPV4 = re.compile(r"\b(?:\d{1,3}\.){3}\d{1,3}\b")


def _render(repo: str, projection: dict | None = None) -> str:
    return generator.render(repo, REPOS_FULL[repo], projection or PROJECTION)


def _projection(**changes) -> dict:
    document = copy.deepcopy(PROJECTION)
    document.update(changes)
    return document


# ---------------------------------------------------------------------------
# The map is registry-sourced
# ---------------------------------------------------------------------------


def test_every_projection_product_appears_in_the_map() -> None:
    rendered = _render("forj")
    for product in PROJECTION["products"]:
        assert f"| **{product['display_name']}**" in rendered, product["slug"]


def test_a_product_added_to_the_registry_shows_up_without_touching_the_generator() -> None:
    document = copy.deepcopy(PROJECTION)
    new = copy.deepcopy(document["products"][0])
    new.update(slug="brand-new", display_name="Brand New", lifecycle="beta")
    new["repo"] = {"name": "brand-new", "github_org": "madfam-org", "visibility": "public"}
    new["domains"] = {"primary": "brand.example", "hosts": [], "infra_hosts": []}
    new["site"] = {**new["site"], "banner_keyword": "SOMETHING NEW"}
    document["products"].append(new)

    rendered = _render("forj", document)
    assert "| **Brand New** | `madfam-org/brand-new` | brand.example | beta |" in re.sub(r" +", " ", rendered)
    assert "Something new" in rendered


def test_products_the_audit_found_missing_are_present() -> None:
    rendered = _render("enclii")
    for name in ("Nauta", "Kalya", "Symbiosis HCM", "Tlacuilo", "Telesia"):
        assert f"**{name}**" in rendered, name
    # Angelia is not customer-facing, so it is not a registry product; it is
    # named by the third-party messaging convention instead.
    assert "Angelia Courier" in rendered


def test_retired_products_render_only_as_tombstones() -> None:
    rendered = _render("forj")
    retired_section = rendered.split("### Retired products")[1].split("### Cross-repo conventions")[0]
    for entry in PROJECTION["retired"]:
        assert entry["display_name"] in retired_section
    products_section = rendered.split("### Products in the registry")[1].split("### Retired products")[0]
    for entry in PROJECTION["retired"]:
        assert f"**{entry['display_name']}**" not in products_section


def test_phynd_crm_role_is_the_ruled_line() -> None:
    rendered = _render("forj")
    row = next(line for line in rendered.splitlines() if line.startswith("| **PhyndCRM**"))
    assert "CRM — consent, campaigns, attribution" in row
    assert "deliverables portal" not in rendered


def test_registry_role_field_wins_over_the_generator_table() -> None:
    document = copy.deepcopy(PROJECTION)
    for product in document["products"]:
        if product["slug"] == "janua":
            product["site"]["role"] = "Identity, as the registry says it"
    assert "Identity, as the registry says it" in _render("forj", document)


def test_role_falls_back_to_the_registry_banner_keyword() -> None:
    product = next(p for p in PROJECTION["products"] if p["slug"] == "rondelio")
    assert "rondelio" not in registry.PLATFORM_ROLES
    assert registry.product_role(product) == product["site"]["banner_keyword"].capitalize()


def test_a_role_for_a_product_that_left_the_registry_fails_loudly() -> None:
    document = copy.deepcopy(PROJECTION)
    document["products"] = [p for p in document["products"] if p["slug"] != "phynd-crm"]
    with pytest.raises(registry.ProjectionError, match="phynd-crm"):
        registry.validate_projection(document)


def test_registry_entry_line_names_this_repos_products() -> None:
    assert "**Registry products**: Enclii (`enclii`, live, front door enclii.dev)" in _render("enclii")
    assert "Cotiza (`cotiza`, live, front door cotiza.studio)" in _render("digifab-quoting")
    assert "**Registry product**: none" in _render("madfam-crawler")


# ---------------------------------------------------------------------------
# No typed estate facts; roles-only topology
# ---------------------------------------------------------------------------


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_no_typed_estate_counts(repo: str) -> None:
    rendered = _render(repo)
    for stale in ("~40 services", "~28 apps", "~22 namespaces", "3 nodes", "3-node"):
        assert stale not in rendered, f"{repo}: {stale!r}"
    assert "4 nodes, described by ROLE only" in rendered


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_no_render_carries_an_ipv4_literal(repo: str) -> None:
    assert IPV4.findall(_render(repo)) == [], repo


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_no_retired_host_or_product_in_metadata(repo: str) -> None:
    rendered = _render(repo)
    for retired in ("forgesight.quest", "penny.onl", "sim4d.com", "phyne-crm", "PhyneCRM"):
        assert retired not in rendered, f"{repo}: {retired!r}"


# ---------------------------------------------------------------------------
# Projection loading
# ---------------------------------------------------------------------------


def test_missing_projection_is_an_error_not_a_fallback(tmp_path: Path) -> None:
    with pytest.raises(registry.ProjectionError, match="not found"):
        registry.load_projection(tmp_path / "absent.json")


def test_wrong_schema_is_rejected(tmp_path: Path) -> None:
    path = tmp_path / "p.json"
    path.write_text(json.dumps(_projection(schema="something-else/v9")))
    with pytest.raises(registry.ProjectionError, match="schema"):
        registry.load_projection(path)


def test_projection_path_resolution_order(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    monkeypatch.setenv("MADFAM_LABSPACE", str(tmp_path))
    monkeypatch.delenv(registry.PROJECTION_ENV, raising=False)
    labspace_copy = tmp_path / registry.PROJECTION_RELPATH
    assert registry.default_projection_paths()[0] == labspace_copy
    labspace_copy.parent.mkdir(parents=True)
    labspace_copy.write_text("{}")
    assert registry.resolve_projection_path() == labspace_copy
    monkeypatch.setenv(registry.PROJECTION_ENV, "/from/env.json")
    assert registry.resolve_projection_path() == Path("/from/env.json")
    assert registry.resolve_projection_path("/explicit.json") == Path("/explicit.json")


# ---------------------------------------------------------------------------
# prettier-stable tables
# ---------------------------------------------------------------------------


def test_md_table_is_padded_like_prettier() -> None:
    table = registry.md_table(["Code", "Meaning"], [["0", "success"], ["10", "validation error"]])
    assert table.splitlines() == [
        "| Code | Meaning          |",
        "| ---- | ---------------- |",
        "| 0    | success          |",
        "| 10   | validation error |",
    ]


def test_md_table_counts_wide_characters_twice() -> None:
    table = registry.md_table(["a"], [["日本"]])
    assert table.splitlines()[1] == "| ---- |"


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_metadata_uses_prettier_emphasis(repo: str) -> None:
    """prettier rewrites `*word*` to `_word_`; a render must already say `_word_`."""
    single_star = re.compile(r"(?<![*\w])\*(?![*\s])[^*\n]+?(?<![*\s])\*(?![*\w])")
    body = _render(repo).split("```")
    prose = re.sub(r"`[^`\n]*`", "", "".join(body[::2]))  # skip fenced code and code spans
    assert single_star.findall(prose) == [], repo


def test_env_entries_with_their_own_code_spans_render_as_written() -> None:
    meta = {**REPOS_FULL["forj"], "key_env": ["`A_VAR` — explained", "PLAIN_VAR — wrapped"]}
    rendered = generator.render("forj", meta, PROJECTION)
    assert "- `A_VAR` — explained" in rendered
    assert "- `PLAIN_VAR — wrapped`" in rendered


# ---------------------------------------------------------------------------
# --check / --write
# ---------------------------------------------------------------------------


def _main(*argv: str) -> int:
    return generator.main(["--projection", str(FIXTURE), *argv])


def test_write_then_check_is_clean(tmp_path: Path) -> None:
    checkout = tmp_path / "forj"
    checkout.mkdir()
    assert _main("--write", str(checkout)) == 0
    assert _main("--check", str(checkout)) == 0


def test_check_reports_drift_with_a_diff(tmp_path: Path, capsys: pytest.CaptureFixture) -> None:
    checkout = tmp_path / "forj"
    checkout.mkdir()
    _main("--write", str(checkout))
    doc = checkout / "ECOSYSTEM.md"
    doc.write_text(doc.read_text().replace("4 nodes", "3 nodes"))
    assert _main("--check", str(checkout)) == 1
    err = capsys.readouterr().err
    assert "DRIFT forj" in err
    assert "-Bare-metal k3s (v1.33+), 3 nodes" in err
    assert "+Bare-metal k3s (v1.33+), 4 nodes" in err


def test_check_without_the_file_is_undetermined(tmp_path: Path) -> None:
    checkout = tmp_path / "forj"
    checkout.mkdir()
    assert _main("--check", str(checkout)) == 2


def test_check_of_an_unknown_repo_is_undetermined(tmp_path: Path) -> None:
    checkout = tmp_path / "not-a-madfam-repo"
    checkout.mkdir()
    (checkout / "ECOSYSTEM.md").write_text("x")
    assert _main("--check", str(checkout)) == 2


def test_check_without_a_projection_is_undetermined(tmp_path: Path) -> None:
    checkout = tmp_path / "forj"
    checkout.mkdir()
    assert generator.main(["--projection", str(tmp_path / "absent.json"), "--check", str(checkout)]) == 2


def test_repo_flag_overrides_the_directory_name(tmp_path: Path) -> None:
    checkout = tmp_path / "clone-of-forj"
    checkout.mkdir()
    assert _main("--write", str(checkout), "--repo", "forj") == 0
    assert (checkout / "ECOSYSTEM.md").read_text().startswith("# forj — Ecosystem Context")
    assert _main("--check", str(checkout), "--repo", "forj") == 0


def test_check_picks_up_the_checkouts_own_overlay(tmp_path: Path) -> None:
    """A private repo's overlay lives at docs/ecosystem-metadata.json and is read
    automatically, so a scheduled --check needs no per-repo wiring."""
    checkout = tmp_path / "forj"
    (checkout / "docs").mkdir(parents=True)
    (checkout / "docs" / "ecosystem-metadata.json").write_text(
        json.dumps({"forj": {"production_truth": "OVERLAY-ONLY LINE"}})
    )
    _main("--write", str(checkout))
    assert "OVERLAY-ONLY LINE" in (checkout / "ECOSYSTEM.md").read_text()
    assert _main("--check", str(checkout)) == 0
    (checkout / "docs" / "ecosystem-metadata.json").unlink()
    assert _main("--check", str(checkout)) == 1


def test_names_and_paths_are_mutually_exclusive(tmp_path: Path) -> None:
    with pytest.raises(SystemExit):
        _main("forj", "--check", str(tmp_path))


def test_boilerplate_overrides_still_fail_loudly_when_the_shared_text_drifts() -> None:
    meta = {**REPOS_FULL["forj"], "boilerplate_overrides": [{"find": "text that is not there", "replace": "x"}]}
    with pytest.raises(SystemExit, match="matched 0 times"):
        generator.render("forj", meta, PROJECTION)


@pytest.mark.parametrize("repo", sorted(REPOS_FULL))
def test_every_render_carries_a_repo_boundary_marker(repo: str) -> None:
    """blueprint-harvester and tulana CI fail a changed ECOSYSTEM.md without one
    (scripts/boundary-checkpoint-check.sh); the shared text supplies it."""
    marker = re.compile(
        r"boundary checkpoint|repository boundary|public repository boundary|"
        r"repo-boundary contract|PUBLIC_REPO_BOUNDARY|repo-boundary-contract",
        re.I,
    )
    assert marker.search(_render(repo)), repo


def test_section_appendix_lands_after_key_env_and_before_the_map() -> None:
    meta = {**REPOS_FULL["forj"], "section_appendix": "### Auth: current status\n\nAPPENDIX-LINE"}
    rendered = generator.render("forj", meta, PROJECTION)
    assert rendered.index("### Key environment variables") < rendered.index("APPENDIX-LINE")
    assert rendered.index("APPENDIX-LINE") < rendered.index("## MADFAM Ecosystem Map")
    assert "APPENDIX-LINE" not in generator.render("forj", REPOS_FULL["forj"], PROJECTION)
