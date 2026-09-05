"""
Tests for scripts/check-image-age.py — the Image Age Ratchet.

Run with:
    pytest tests/scripts/test_check_image_age.py -v
"""
from __future__ import annotations

import importlib.util
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-image-age.py"

# Hyphen in filename means we can't `import` directly — load by path.
spec = importlib.util.spec_from_file_location("check_image_age", SCRIPT)
assert spec is not None and spec.loader is not None
mod = importlib.util.module_from_spec(spec)
sys.modules["check_image_age"] = mod
spec.loader.exec_module(mod)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

KUSTOMIZATION_BODY = """\
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: enclii
resources:
- ../base
images:
- digest: sha256:c9590267a422a4fe3ad534710c0bd556dbea96260df9c38594eb3405d6613065
  name: docs-site
  newName: ghcr.io/madfam-org/enclii/docs-site
- digest: sha256:ee9e4277f09092fc9ae2877c830faca556229f9e1d99ee0aba7f70d222ff87d9
  name: switchyard-api
  newName: ghcr.io/madfam-org/enclii/switchyard-api
- name: untagged-image
  newName: ghcr.io/madfam-org/enclii/untagged
"""


def _write_kustomization(tmp_path: Path, body: str = KUSTOMIZATION_BODY) -> Path:
    f = tmp_path / "kustomization.yaml"
    f.write_text(body)
    return f


def _img(name: str, digest: str = "sha256:" + "a" * 64) -> mod.PinnedImage:
    return mod.PinnedImage(
        source_file=Path("/tmp/k.yaml"),
        name=name,
        new_name=f"ghcr.io/madfam-org/enclii/{name}",
        digest=digest,
    )


# ---------------------------------------------------------------------------
# 1. parse_kustomization extracts digest entries correctly
# ---------------------------------------------------------------------------

def test_parse_kustomization_extracts_digest_entries(tmp_path):
    kf = _write_kustomization(tmp_path)
    pinned = mod.parse_kustomization(kf)

    # Two entries have digest:, one is unpinned (untagged-image) and is skipped.
    assert len(pinned) == 2
    names = {p.name for p in pinned}
    assert names == {"docs-site", "switchyard-api"}

    docs_site = next(p for p in pinned if p.name == "docs-site")
    assert docs_site.new_name == "ghcr.io/madfam-org/enclii/docs-site"
    assert docs_site.digest.startswith("sha256:")
    assert docs_site.repository == "madfam-org/enclii/docs-site"
    assert docs_site.exemption_key == "AGE_RATCHET_EXEMPT_DOCS_SITE"


# ---------------------------------------------------------------------------
# 2. Mock GHCR response → image age computed correctly
# ---------------------------------------------------------------------------

def test_age_computed_from_creation_timestamp():
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    img = _img("switchyard-api")

    # Pretend the registry says this image was created 10 days ago.
    fetcher = lambda _i: now - timedelta(days=10)

    failures, warnings = mod.evaluate(
        images=[img], threshold_days=30, now=now,
        fetcher=fetcher, exemptions={},
    )
    assert failures == []
    # No warnings either — the image is healthy.
    assert warnings == []


# ---------------------------------------------------------------------------
# 3. 30-day-old digest passes; 31-day-old fails with expected message
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("age_days, should_fail", [
    (0, False),
    (29, False),
    (30, False),   # exactly at threshold — not older than threshold
    (31, True),
    (90, True),
])
def test_threshold_boundary(age_days, should_fail):
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    img = _img("switchyard-api")
    fetcher = lambda _i: now - timedelta(days=age_days)

    failures, _ = mod.evaluate(
        images=[img], threshold_days=30, now=now,
        fetcher=fetcher, exemptions={},
    )
    if should_fail:
        assert len(failures) == 1
        msg = failures[0]
        assert "switchyard-api" in msg
        assert f"is {age_days} days old" in msg
        assert "Rebuild + repin" in msg
        assert "AGE_RATCHET_EXEMPT_SWITCHYARD_API" in msg
    else:
        assert failures == []


# ---------------------------------------------------------------------------
# 4. Exemption env var bypasses with logged WARNING
# ---------------------------------------------------------------------------

def test_exemption_bypasses_failure_with_warning():
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    img = _img("switchyard-api")
    fetcher = lambda _i: now - timedelta(days=120)  # very stale

    exemptions = {
        "AGE_RATCHET_EXEMPT_SWITCHYARD_API":
            "blocked on @janua/react-sdk v3 release, ETA 2026-05-15",
    }
    failures, warnings = mod.evaluate(
        images=[img], threshold_days=30, now=now,
        fetcher=fetcher, exemptions=exemptions,
    )
    assert failures == []
    assert len(warnings) == 1
    assert "EXEMPT" in warnings[0]
    assert "switchyard-api" in warnings[0]
    assert "blocked on @janua/react-sdk" in warnings[0]


# ---------------------------------------------------------------------------
# 5. Multiple stale images: error message lists all
# ---------------------------------------------------------------------------

def test_multiple_stale_images_all_listed():
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    images = [_img("docs-site"), _img("switchyard-api"), _img("waybill")]
    ages = {"docs-site": 45, "switchyard-api": 90, "waybill": 5}

    def fetcher(img):
        return now - timedelta(days=ages[img.name])

    failures, _ = mod.evaluate(
        images=images, threshold_days=30, now=now,
        fetcher=fetcher, exemptions={},
    )
    # docs-site (45d) and switchyard-api (90d) are stale; waybill (5d) is fine.
    assert len(failures) == 2
    joined = "\n".join(failures)
    assert "docs-site" in joined and "45 days old" in joined
    assert "switchyard-api" in joined and "90 days old" in joined
    assert "waybill" not in joined


# ---------------------------------------------------------------------------
# 6. Token missing: graceful skip with WARNING (don't fail the build)
# ---------------------------------------------------------------------------

def test_missing_token_skips_gracefully(tmp_path, monkeypatch, capsys):
    # Build a fake repo layout and point the script at it.
    k_dir = tmp_path / "infra" / "k8s" / "production"
    k_dir.mkdir(parents=True)
    _write_kustomization(k_dir)

    monkeypatch.delenv("GHCR_READ_TOKEN", raising=False)
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    # Strip any inherited exemption vars from the host shell so they don't
    # bleed into the test environment.
    for k in list(__import__("os").environ):
        if k.startswith(mod.EXEMPT_PREFIX):
            monkeypatch.delenv(k, raising=False)

    rc = mod.main(["--repo-root", str(tmp_path)])
    assert rc == 0  # graceful skip, not a hard fail


# ---------------------------------------------------------------------------
# 7. Fetcher returning None (registry error) is a soft skip, not a fail
#    (companion to #6 — covers the per-image error path, not the missing-creds
#    short-circuit. Together they cover both "build never made it" cases.)
# ---------------------------------------------------------------------------

def test_fetcher_none_skips_with_warning_not_failure():
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    img = _img("switchyard-api")
    fetcher = lambda _i: None  # registry unreachable / 404 / auth issue

    failures, warnings = mod.evaluate(
        images=[img], threshold_days=30, now=now,
        fetcher=fetcher, exemptions={},
    )
    assert failures == []
    assert any("SKIP" in w and "switchyard-api" in w for w in warnings)


# ---------------------------------------------------------------------------
# 8. Sentinel timestamps from registry metadata are soft skips, not failures.
# ---------------------------------------------------------------------------

def test_implausible_created_timestamp_skips_with_warning_not_failure():
    now = datetime(2026, 5, 4, 12, 0, 0, tzinfo=timezone.utc)
    img = _img("switchyard-api")
    fetcher = lambda _i: datetime(1, 1, 1, tzinfo=timezone.utc)

    failures, warnings = mod.evaluate(
        images=[img], threshold_days=30, now=now,
        fetcher=fetcher, exemptions={},
    )
    assert failures == []
    assert any("SKIP" in w and "implausible creation timestamp" in w for w in warnings)


# ---------------------------------------------------------------------------
# Extra: ISO8601 parser handles GHCR's nanosecond precision and Z suffix.
# Cheap, but it's the most likely fragility point in the registry path.
# ---------------------------------------------------------------------------

def test_module_imports_without_requests_installed(tmp_path):
    """Importing this script must never call sys.exit().

    Regression guard for the collection break: the `requests` ImportError
    handler used to `sys.exit(2)` at module scope, which aborted pytest
    *collection* for all of tests/scripts/ (an INTERNALERROR, not a test
    failure) on any machine without `requests`. Importing is a read-only act;
    only main() may exit.
    """
    # The module is already imported at the top of this file — reaching here
    # at all proves import didn't exit. Re-exec it to pin the behaviour even
    # when `requests` IS installed locally.
    spec2 = importlib.util.spec_from_file_location("check_image_age_reimport", SCRIPT)
    assert spec2 is not None and spec2.loader is not None
    m2 = importlib.util.module_from_spec(spec2)
    sys.modules["check_image_age_reimport"] = m2
    spec2.loader.exec_module(m2)  # must not raise SystemExit
    assert hasattr(m2, "main")


def test_missing_requests_fails_loudly_in_main(tmp_path, monkeypatch, capsys):
    """With a token present, a missing `requests` is fatal — rc 2, not a
    silent pass. Pairs with the test above: import is safe, main() is strict.
    """
    k_dir = tmp_path / "infra" / "k8s" / "production"
    k_dir.mkdir(parents=True)
    _write_kustomization(k_dir)

    monkeypatch.setattr(mod, "requests", None)
    monkeypatch.setenv("GITHUB_TOKEN", "dummy-token")

    rc = mod.main(["--repo-root", str(tmp_path)])
    assert rc == 2
    assert "requires `requests`" in capsys.readouterr().err


def test_parse_iso8601_handles_nanoseconds_and_z():
    dt = mod._parse_iso8601("2026-04-15T18:22:43.987654321Z")
    assert dt is not None
    assert dt.year == 2026 and dt.month == 4 and dt.day == 15
    assert dt.tzinfo is timezone.utc

    dt2 = mod._parse_iso8601("2026-04-15T18:22:43Z")
    assert dt2 is not None and dt2.tzinfo is timezone.utc

    assert mod._parse_iso8601("not-a-timestamp") is None


# ---------------------------------------------------------------------------
# ARC runner base-tag ratchet
# ---------------------------------------------------------------------------
# The digest ratchet's failure is one stale service. This one's failure is the
# 2026-08-10 org-wide CI outage: an `actions/actions-runner` base 111 days old,
# rejected at GitHub's registration layer, 3,149 EphemeralRunners in
# phase=Outdated, zero jobs served. So the bar for these tests is the same as
# for the pool-health detector's: given the state the repo was actually in,
# does the check fail — and given a healthy pin, does it stay quiet.

DOCKERFILE_BODY = """\
# comment
ARG BASE_IMAGE=ghcr.io/actions/actions-runner
ARG BASE_TAG=2.337.0
ARG BASE_TAG_DATE=2026-08-27
FROM ${BASE_IMAGE}:${BASE_TAG}
USER root
"""

NOW = datetime(2026, 9, 4, tzinfo=timezone.utc)


def _write_dockerfile(root: Path, body: str = DOCKERFILE_BODY) -> Path:
    d = root / "infra" / "docker" / "arc-runner"
    d.mkdir(parents=True, exist_ok=True)
    f = d / "Dockerfile"
    f.write_text(body)
    return f


def _pin(tag: str = "2.337.0", date_raw: str | None = "2026-08-27") -> mod.BaseTagPin:
    parsed = mod._parse_iso8601(date_raw + "T00:00:00Z") if date_raw else None
    return mod.BaseTagPin(
        source_file=Path("/tmp/Dockerfile"),
        image="ghcr.io/actions/actions-runner",
        tag=tag,
        tag_date_raw=date_raw,
        tag_date=parsed,
    )


def _day(s: str) -> datetime:
    return datetime.fromisoformat(s).replace(tzinfo=timezone.utc)


def test_parse_base_tag_reads_image_tag_and_date(tmp_path):
    pin = mod.parse_base_tag(_write_dockerfile(tmp_path))
    assert pin is not None
    assert pin.image == "ghcr.io/actions/actions-runner"
    assert pin.repository == "actions/actions-runner"
    assert pin.tag == "2.337.0"
    assert pin.tag_date == _day("2026-08-27")
    assert pin.exemption_key == "AGE_RATCHET_EXEMPT_ACTIONS_RUNNER"


def test_parse_base_tag_without_date_field(tmp_path):
    body = DOCKERFILE_BODY.replace("ARG BASE_TAG_DATE=2026-08-27\n", "")
    pin = mod.parse_base_tag(_write_dockerfile(tmp_path, body))
    assert pin is not None and pin.tag == "2.337.0"
    assert pin.tag_date is None and pin.tag_date_raw is None


def test_missing_base_tag_date_is_a_hard_failure():
    """The offline half of the ratchet cannot be silently deleted."""
    failures, _ = mod.evaluate_base_tag(
        _pin(date_raw=None), NOW, 30, (None, None, None), {})
    assert len(failures) == 1
    assert "BASE_TAG_DATE" in failures[0]


def test_pin_on_newest_upstream_release_is_silent():
    failures, warnings = mod.evaluate_base_tag(
        _pin(), NOW, 30,
        ("2.337.0", _day("2026-08-27"), _day("2026-08-27")), {})
    assert failures == [] and warnings == []


def test_newer_release_inside_the_cadence_only_warns():
    """8 days behind is the normal state between a release and our bump."""
    failures, warnings = mod.evaluate_base_tag(
        _pin(tag="2.336.0", date_raw="2026-07-20"), NOW, 30,
        ("2.337.0", _day("2026-08-27"), _day("2026-07-20")), {})
    assert failures == []
    assert any(w.startswith("BEHIND") and "2.337.0" in w for w in warnings)


def test_newer_release_past_the_cadence_fails():
    """The 2026-08-10 shape: two releases skipped, well past 30 days."""
    failures, _ = mod.evaluate_base_tag(
        _pin(tag="2.334.0", date_raw="2026-04-21"), NOW, 30,
        ("2.336.0", _day("2026-07-20"), _day("2026-04-21")), {})
    assert len(failures) == 1
    assert "2.336.0" in failures[0] and "46 days" in failures[0]
    # The fix is two edits, and naming only the first is how the pool ends up
    # running an image nobody deployed.
    assert "rendered.yaml" in failures[0]


def test_stated_date_disagreeing_with_the_registry_fails():
    """A date field nobody verifies is a date field somebody edits."""
    failures, _ = mod.evaluate_base_tag(
        _pin(tag="2.337.0", date_raw="2026-09-01"), NOW, 30,
        ("2.337.0", _day("2026-08-27"), _day("2026-08-27")), {})
    assert len(failures) == 1
    assert "2026-09-01" in failures[0] and "2026-08-27" in failures[0]


def test_one_day_of_publish_skew_is_tolerated():
    """Registries stamp UTC build time; a date field is a date."""
    failures, warnings = mod.evaluate_base_tag(
        _pin(tag="2.337.0", date_raw="2026-08-26"), NOW, 30,
        ("2.337.0", _day("2026-08-27"), _day("2026-08-27")), {})
    assert failures == [] and warnings == []


def test_offline_fallback_warns_past_the_cadence_but_does_not_fail():
    """Unreachable upstream is not evidence of staleness — only of blindness."""
    failures, warnings = mod.evaluate_base_tag(
        _pin(tag="2.336.0", date_raw="2026-07-20"), NOW, 30,
        (None, None, None), {})
    assert failures == []
    assert any(w.startswith("CADENCE") for w in warnings)


def test_offline_fallback_fails_inside_the_deprecation_window():
    failures, _ = mod.evaluate_base_tag(
        _pin(tag="2.334.0", date_raw="2026-04-21"), NOW, 30,
        (None, None, None), {})
    assert len(failures) == 1
    assert "deprecation window" in failures[0]


def test_exemption_downgrades_a_failure_to_a_warning():
    failures, warnings = mod.evaluate_base_tag(
        _pin(tag="2.334.0", date_raw="2026-04-21"), NOW, 30,
        ("2.336.0", _day("2026-07-20"), _day("2026-04-21")),
        {"AGE_RATCHET_EXEMPT_ACTIONS_RUNNER": "bump PR in flight"})
    assert failures == []
    assert warnings and warnings[0].startswith("EXEMPT")


def test_newest_semver_sorts_numerically_not_lexically():
    """`2.9.0 > 2.10.0` under string comparison — that is how a bump gets
    skipped by a check that thinks it is watching."""
    assert mod.newest_semver(["2.9.0", "2.10.0", "2.10.1"]) == "2.10.1"
    assert mod.newest_semver(["2.336.0", "2.337.0", "latest", "ubuntu-22.04"]) == "2.337.0"
    assert mod.newest_semver([]) is None
    assert mod.newest_semver(["latest"]) is None


def test_check_base_tag_skips_when_dockerfile_is_absent(tmp_path):
    assert mod.check_base_tag(tmp_path, NOW, 30, None, {}) == ([], [])


def test_check_base_tag_falls_back_when_the_registry_is_unreachable(tmp_path, monkeypatch):
    _write_dockerfile(tmp_path)
    monkeypatch.setattr(mod, "list_upstream_tags", lambda *a, **k: None)
    monkeypatch.setattr(mod, "fetch_tag_created", lambda *a, **k: None)
    failures, warnings = mod.check_base_tag(tmp_path, NOW, 30, None, {})
    assert failures == [] and warnings == []  # 8 days old, inside the cadence


def test_implausible_registry_timestamp_does_not_pass_a_stale_pin(tmp_path, monkeypatch):
    """An attestation manifest's config blob dates to the epoch. Reading one
    must degrade to the offline path, never certify freshness."""
    body = DOCKERFILE_BODY.replace("2.337.0", "2.334.0").replace(
        "2026-08-27", "2026-04-21")
    _write_dockerfile(tmp_path, body)
    monkeypatch.setattr(mod, "list_upstream_tags", lambda *a, **k: ["2.334.0", "2.336.0"])
    monkeypatch.setattr(mod, "fetch_tag_created",
                        lambda *a, **k: datetime(1970, 1, 1, tzinfo=timezone.utc))
    failures, _ = mod.check_base_tag(tmp_path, NOW, 30, None, {})
    assert len(failures) == 1
    assert "deprecation window" in failures[0]


def test_main_fails_on_base_tag_even_with_no_pinned_digests(tmp_path, monkeypatch):
    monkeypatch.setattr(mod, "check_base_tag",
                        lambda *a, **k: (["stale base tag"], []))
    assert mod.main(["--repo-root", str(tmp_path)]) == 1


def test_repo_dockerfile_carries_a_parseable_pin_and_date():
    """Guard the real file: both fields present, date parseable, tag semver."""
    pin = mod.parse_base_tag(REPO_ROOT / mod.ARC_DOCKERFILE)
    assert pin is not None, "the ARC runner Dockerfile lost its ARG BASE_TAG"
    assert mod.SEMVER_TAG_RE.match(pin.tag), f"BASE_TAG {pin.tag!r} is not MAJOR.MINOR.PATCH"
    assert pin.tag_date is not None, (
        "ARG BASE_TAG_DATE is missing or unparseable — the ratchet's offline "
        "half depends on it"
    )
