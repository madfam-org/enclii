#!/usr/bin/env python3
"""
Image Age Ratchet — fail CI when a deployed image digest is older than the
configured threshold (default: 30 days).

Why this exists
---------------
Today's selva-office case: CI failed for 3+ hours, no image rebuild happened,
and ArgoCD silently kept serving 8h-old pods. The kustomization digest never
moved, so nothing alerted. This check inverts the signal: we look at the
*age of the digest currently pinned in kustomization.yaml* and fail the build
if it has drifted past the threshold. That makes pipeline staleness a hard CI
failure instead of a silent runtime condition.

Behaviour
---------
- Walks `infra/k8s/**/kustomization.yaml`.
- For every `images:` entry that has a `digest:` (sha256:...), looks up the
  image manifest on GHCR and reads the `created` timestamp from the config
  blob.
- If `(now - created) > threshold_days`, fails with a precise message naming
  the image, digest, age, and creation date.
- Per-image exemptions are read from env vars of the form
  `AGE_RATCHET_EXEMPT_<IMAGE>=<reason>` (image name normalised to upper case
  with `-` -> `_`). Exemptions log a WARNING and do not fail the build —
  they're meant to be temporary while a dep upgrade lands.
- If no GHCR token is available, the check logs a WARNING and exits 0
  (graceful skip — better than blocking every fork PR on missing creds).

Speed
-----
On a typical PR (~6 pinned images) this finishes well under 5s, so operators
can run it locally too:

    python3 scripts/check-image-age.py
    python3 scripts/check-image-age.py --threshold-days 14   # tighter audit

Out of scope (Phase 2)
----------------------
- Auto-PR for stale images (Renovate-style).
- Multi-registry support (currently GHCR only).
- Per-image age policies (e.g. alpine bases that can stay older safely).
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import re
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable

import yaml

try:
    import requests
except ImportError:  # pragma: no cover - environment guard
    print("ERROR: this script requires `requests`. `pip install requests`.", file=sys.stderr)
    sys.exit(2)


GHCR_HOST = "ghcr.io"
DEFAULT_THRESHOLD_DAYS = 30
KUSTOMIZATION_GLOB = "infra/k8s/**/kustomization.yaml"
EXEMPT_PREFIX = "AGE_RATCHET_EXEMPT_"
MIN_PLAUSIBLE_CREATED = datetime(2000, 1, 1, tzinfo=timezone.utc)

# Accept both OCI and Docker manifest types — GHCR can return either depending
# on how the image was pushed.
MANIFEST_ACCEPT = ",".join([
    "application/vnd.oci.image.manifest.v1+json",
    "application/vnd.docker.distribution.manifest.v2+json",
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
])

logger = logging.getLogger("image-age-ratchet")


@dataclass(frozen=True)
class PinnedImage:
    """One image entry from a kustomization.yaml file."""

    source_file: Path
    name: str          # e.g. "switchyard-api" (the kustomize lookup name)
    new_name: str      # e.g. "ghcr.io/madfam-org/enclii/switchyard-api"
    digest: str        # e.g. "sha256:ee9e..."

    @property
    def repository(self) -> str:
        """The path portion of the registry URL, e.g. madfam-org/enclii/switchyard-api."""
        # Strip the registry host if present.
        if "/" not in self.new_name:
            return self.new_name
        host, _, repo = self.new_name.partition("/")
        if "." in host or ":" in host:
            return repo
        return self.new_name

    @property
    def exemption_key(self) -> str:
        # Use only the leaf name so the env var stays short and operator-friendly.
        leaf = self.new_name.rsplit("/", 1)[-1]
        return EXEMPT_PREFIX + re.sub(r"[^A-Z0-9]", "_", leaf.upper())


def parse_kustomization(path: Path) -> list[PinnedImage]:
    """Extract every `images:` entry that has a digest pin."""
    try:
        with path.open() as fh:
            doc = yaml.safe_load(fh) or {}
    except yaml.YAMLError as e:
        logger.error("failed to parse %s: %s", path, e)
        return []

    images = doc.get("images") or []
    result: list[PinnedImage] = []
    for entry in images:
        if not isinstance(entry, dict):
            continue
        digest = entry.get("digest")
        if not digest or not str(digest).startswith("sha256:"):
            continue
        name = entry.get("name")
        new_name = entry.get("newName") or name
        if not name or not new_name:
            continue
        result.append(PinnedImage(
            source_file=path,
            name=str(name),
            new_name=str(new_name),
            digest=str(digest),
        ))
    return result


def discover_pinned_images(repo_root: Path) -> list[PinnedImage]:
    found: list[PinnedImage] = []
    for kf in sorted(repo_root.glob(KUSTOMIZATION_GLOB)):
        found.extend(parse_kustomization(kf))
    return found


def _ghcr_token(repository: str, read_token: str | None) -> str | None:
    """Exchange a GitHub token (or anon) for a registry pull token.

    GHCR's `/token` endpoint accepts either Basic auth with a PAT
    (`username:<token>`) or anonymous requests for public images. We don't
    have the username in CI, but `Bearer <token>` against the GitHub API is
    *not* what GHCR's /token wants. The trick that works in GitHub Actions
    is: pass the GITHUB_TOKEN as the password with username=`x-access-token`.
    """
    auth = None
    if read_token:
        auth = ("x-access-token", read_token)
    try:
        resp = requests.get(
            f"https://{GHCR_HOST}/token",
            params={"scope": f"repository:{repository}:pull", "service": GHCR_HOST},
            auth=auth,
            timeout=10,
        )
    except requests.RequestException as e:
        logger.warning("token exchange failed for %s: %s", repository, e)
        return None
    if resp.status_code != 200:
        logger.warning("token exchange returned %s for %s: %s",
                       resp.status_code, repository, resp.text[:200])
        return None
    return resp.json().get("token")


def fetch_image_created(image: PinnedImage, read_token: str | None,
                        session: requests.Session | None = None) -> datetime | None:
    """Resolve the digest to a `created` timestamp from the config blob.

    Returns None on any registry error so the caller can decide whether to
    treat it as a soft skip (missing creds) or a hard failure.
    """
    sess = session or requests.Session()
    repo = image.repository
    bearer = _ghcr_token(repo, read_token)
    headers = {"Accept": MANIFEST_ACCEPT}
    if bearer:
        headers["Authorization"] = f"Bearer {bearer}"

    manifest_url = f"https://{GHCR_HOST}/v2/{repo}/manifests/{image.digest}"
    try:
        m = sess.get(manifest_url, headers=headers, timeout=10)
    except requests.RequestException as e:
        logger.warning("manifest fetch failed for %s@%s: %s", repo, image.digest, e)
        return None
    if m.status_code != 200:
        logger.warning("manifest %s@%s -> HTTP %s", repo, image.digest, m.status_code)
        return None

    body = m.json()
    # If we got a manifest list / index, pick the first manifest with a config
    # we can actually fetch. For our use case (single-arch service images)
    # this rarely fires.
    if body.get("manifests"):
        for sub in body["manifests"]:
            sub_digest = sub.get("digest")
            if not sub_digest:
                continue
            sub_image = PinnedImage(
                source_file=image.source_file,
                name=image.name,
                new_name=image.new_name,
                digest=sub_digest,
            )
            ts = fetch_image_created(sub_image, read_token, session=sess)
            if ts:
                return ts
        return None

    config = body.get("config") or {}
    config_digest = config.get("digest")
    if not config_digest:
        logger.warning("no config blob for %s@%s", repo, image.digest)
        return None

    blob_url = f"https://{GHCR_HOST}/v2/{repo}/blobs/{config_digest}"
    try:
        b = sess.get(blob_url, headers=headers, timeout=10)
    except requests.RequestException as e:
        logger.warning("config blob fetch failed for %s@%s: %s", repo, image.digest, e)
        return None
    if b.status_code != 200:
        logger.warning("config blob %s@%s -> HTTP %s", repo, image.digest, b.status_code)
        return None

    try:
        cfg = b.json()
    except json.JSONDecodeError:
        logger.warning("config blob for %s@%s is not JSON", repo, image.digest)
        return None

    created = cfg.get("created")
    if not created:
        return None
    return _parse_iso8601(created)


def _parse_iso8601(ts: str) -> datetime | None:
    # GHCR returns RFC3339 like "2026-04-15T18:22:43.987654321Z". `fromisoformat`
    # in 3.11+ handles the Z, but earlier versions don't, and nanoseconds blow
    # it up. Strip nanos past microseconds and normalise the trailing Z.
    s = ts.strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    # Trim sub-microsecond precision: "2026-04-15T18:22:43.123456789+00:00"
    m = re.match(r"^(.*\.\d{6})\d+([+-]\d{2}:\d{2})$", s)
    if m:
        s = m.group(1) + m.group(2)
    try:
        dt = datetime.fromisoformat(s)
    except ValueError:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt


def _read_exemptions(env: dict[str, str] | None = None) -> dict[str, str]:
    env = env if env is not None else dict(os.environ)
    return {k: v for k, v in env.items() if k.startswith(EXEMPT_PREFIX) and v}


def evaluate(images: Iterable[PinnedImage], threshold_days: int,
             now: datetime, fetcher, exemptions: dict[str, str]) -> tuple[list[str], list[str]]:
    """Return (failures, warnings). Failures are stale-and-not-exempt.

    `fetcher` is a callable PinnedImage -> Optional[datetime] so tests can
    stub it without HTTP.
    """
    failures: list[str] = []
    warnings: list[str] = []
    for img in images:
        if img.exemption_key in exemptions:
            warnings.append(
                f"EXEMPT {img.new_name}@{img.digest[:19]}... — reason: "
                f"{exemptions[img.exemption_key]}"
            )
            continue
        created = fetcher(img)
        if created is None:
            warnings.append(
                f"SKIP {img.new_name}@{img.digest[:19]}... — could not resolve "
                f"creation timestamp (registry auth or network issue)"
            )
            continue
        if created < MIN_PLAUSIBLE_CREATED:
            warnings.append(
                f"SKIP {img.new_name}@{img.digest[:19]}... — registry returned "
                f"implausible creation timestamp {created.date().isoformat()}"
            )
            continue
        age = now - created
        age_days = age.days
        if age_days > threshold_days:
            failures.append(
                f"{img.new_name}: digest {img.digest} is {age_days} days old "
                f"(created {created.date().isoformat()}). Rebuild + repin or "
                f"add an explicit `{img.exemption_key}=<reason>` env var to "
                f"acknowledge the staleness."
            )
        else:
            logger.info("ok %s age=%dd (threshold=%dd)",
                        img.new_name, age_days, threshold_days)
    return failures, warnings


def _resolve_repo_root(arg: str | None) -> Path:
    if arg:
        return Path(arg).resolve()
    # Default: assume the script lives at <repo>/scripts/check-image-age.py.
    return Path(__file__).resolve().parent.parent


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--threshold-days", type=int, default=DEFAULT_THRESHOLD_DAYS,
                        help=f"max digest age in days (default: {DEFAULT_THRESHOLD_DAYS})")
    parser.add_argument("--repo-root", type=str, default=None,
                        help="repo root (default: parent of this script)")
    parser.add_argument("-v", "--verbose", action="store_true",
                        help="log INFO-level details (per-image age)")
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=logging.INFO if args.verbose else logging.WARNING,
        format="%(levelname)s %(message)s",
    )

    repo_root = _resolve_repo_root(args.repo_root)
    images = discover_pinned_images(repo_root)
    if not images:
        print("No digest-pinned images found — nothing to check.")
        return 0

    token = os.getenv("GHCR_READ_TOKEN") or os.getenv("GITHUB_TOKEN")
    if not token:
        logger.warning(
            "no GHCR_READ_TOKEN or GITHUB_TOKEN in env — skipping age check. "
            "This is fine for fork PRs; ops should ensure CI has a token."
        )
        return 0

    exemptions = _read_exemptions()
    now = datetime.now(timezone.utc)

    def fetcher(img: PinnedImage):
        return fetch_image_created(img, token)

    failures, warnings = evaluate(images, args.threshold_days, now, fetcher, exemptions)

    for w in warnings:
        logger.warning("%s", w)

    if failures:
        print("\nImage Age Ratchet FAILED — the following digests exceed "
              f"{args.threshold_days} days:\n", file=sys.stderr)
        for f in failures:
            print(f"  - {f}", file=sys.stderr)
        print("\nRebuild + repin via the normal CI flow, or add an exemption "
              "env var in `.github/workflows/ci.yml` while a dep upgrade is in "
              "flight.", file=sys.stderr)
        return 1

    skipped = sum(1 for w in warnings if w.startswith("SKIP"))
    exempt = sum(1 for w in warnings if w.startswith("EXEMPT"))
    checked = len(images) - skipped - exempt
    suffix = []
    if skipped:
        suffix.append(f"{skipped} skipped (registry/auth)")
    if exempt:
        suffix.append(f"{exempt} exempt")
    extra = f" ({', '.join(suffix)})" if suffix else ""
    print(f"Image Age Ratchet OK — {checked}/{len(images)} digest(s) within "
          f"{args.threshold_days} days{extra}.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
