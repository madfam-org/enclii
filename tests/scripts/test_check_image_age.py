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
# Extra: ISO8601 parser handles GHCR's nanosecond precision and Z suffix.
# Cheap, but it's the most likely fragility point in the registry path.
# ---------------------------------------------------------------------------

def test_parse_iso8601_handles_nanoseconds_and_z():
    dt = mod._parse_iso8601("2026-04-15T18:22:43.987654321Z")
    assert dt is not None
    assert dt.year == 2026 and dt.month == 4 and dt.day == 15
    assert dt.tzinfo is timezone.utc

    dt2 = mod._parse_iso8601("2026-04-15T18:22:43Z")
    assert dt2 is not None and dt2.tzinfo is timezone.utc

    assert mod._parse_iso8601("not-a-timestamp") is None
