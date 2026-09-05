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
Two ratchets share this script, because they are the same failure at two
altitudes: something stopped being rebuilt and nothing said so.

(A) Deployed-digest age — the original check:
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
- If no GHCR token is available, the check logs a WARNING and skips the
  digest half (graceful skip — better than blocking every fork PR on
  missing creds).

(B) ARC runner base-tag age — the org-wide-CI-outage class:
- Reads `ARG BASE_TAG` / `ARG BASE_TAG_DATE` from
  `infra/docker/arc-runner/Dockerfile`.
- Fails when a NEWER upstream `actions/actions-runner` release has been
  published for more than the threshold, when `BASE_TAG_DATE` disagrees with
  what the registry says, or — when upstream is unreachable — when the
  stated date is past the deprecation floor. See the section above
  `parse_base_tag` for why each rule is shaped the way it is.
- Runs anonymously (GHCR issues pull tokens for public repos), so it still
  covers fork PRs. Skip it with `--skip-base-tag`.

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
    # Deliberately do NOT exit here. This module is imported by
    # tests/scripts/test_check_image_age.py, and a module-level sys.exit()
    # aborts pytest *collection* for the whole directory rather than failing a
    # single test. `requests` is only needed on the registry path, so we defer
    # the hard failure to main(), which is the only caller that reaches the
    # network. The pure-logic helpers (parse_kustomization, evaluate,
    # _parse_iso8601) stay importable and testable without it.
    requests = None  # type: ignore[assignment]

REQUESTS_MISSING_MSG = (
    "ERROR: this script requires `requests`. `pip install requests`."
)


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
    #
    # FOLLOW-UP, BEFORE ANYONE FLIPS `provenance: true` in
    # .github/workflows/build-publish.yml (it is `false` today, at the
    # `Build and push` step): that flip makes buildx push an OCI INDEX per
    # service — the linux/amd64 image PLUS an attestation manifest whose
    # platform is `unknown/unknown` and whose config blob carries
    # `created: 0001-01-01T00:00:00Z`. This loop takes the first sub-manifest
    # that yields a timestamp. buildx orders the image first today, so it
    # would keep working — but nothing in the spec guarantees that ordering,
    # and if it ever flips, this returns a year-1 timestamp, MIN_PLAUSIBLE_CREATED
    # downgrades it to a SKIP, and the digest ratchet silently stops covering
    # every service. Losing a guard quietly is worse than failing loudly.
    #
    # The fix is small and is already written twice below: select
    # linux/amd64 explicitly, the way fetch_tag_created does. Do that in the
    # same PR as the provenance flip, not after it. The rest of the chain
    # tolerates the flip as far as the record can be read statically — the
    # pin step derives `.Manifest.Digest` (the index digest), cosign signs
    # and verifies that same digest, and Kyverno's require-image-digest only
    # asks that a digest be present — but none of that has been exercised
    # live, and the free-plan 500 MB GHCR/artifact pool that hard-blocked the
    # org for 6h+ on 2026-08-27 gains another manifest per service per deploy.
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


# ---------------------------------------------------------------------------
# ARC runner base-tag ratchet
# ---------------------------------------------------------------------------
# Why this lives in the same script as the digest ratchet
# ------------------------------------------------------
# Same failure shape, one class up. The digest ratchet catches "a service
# image we build stopped being rebuilt". This catches "the base image we
# build ON stopped being bumped" — and that one takes the whole org's CI
# down rather than one service.
#
# On 2026-08-10 the ARC pool's pinned `actions/actions-runner` base was 111
# days old across two skipped releases. GitHub rejects runner agents that lag
# the newest release by ~60 days AT THE REGISTRATION LAYER: the agent starts,
# fails to register, ARC respawns it forever. 3,149 EphemeralRunners, all
# phase=Outdated, zero jobs served, every private repo QUEUED — detected by a
# human noticing a slow PR. The post-mortem's own action item was "automate
# the cadence so this is prevented, not merely detected". This is that.
#
# What it asserts, in order of preference
# ---------------------------------------
# 1. `BASE_TAG_DATE` in the Dockerfile matches what the registry says the
#    pinned tag was actually published on. A date field nobody verifies
#    becomes a date field somebody edits to silence CI.
# 2. We are not more than `--base-tag-threshold-days` (30, the runbook's
#    cadence) behind a NEWER upstream release. The clock starts when
#    upstream publishes, not when we notice.
# 3. If the registry is unreachable (fork PR, GHCR outage, egress policy),
#    fall back to the stated `BASE_TAG_DATE` alone: warn past the 30-day
#    cadence, fail past BASE_TAG_DEPRECATION_DAYS. The fallback is
#    deliberately weaker than the network check, because "no new upstream
#    release exists" and "we cannot see upstream" are different facts and
#    only the second one is our problem to route around — but it still has
#    a hard floor, because an unfixable-red CI job is a smaller outage than
#    an org-wide registration failure.

ARC_DOCKERFILE = "infra/docker/arc-runner/Dockerfile"
UPSTREAM_RUNNER_REPO = "actions/actions-runner"
BASE_TAG_THRESHOLD_DAYS = 30
# GitHub's deprecation window is ~60 days. Fail the offline fallback before
# it, not on it.
BASE_TAG_DEPRECATION_DAYS = 55
SEMVER_TAG_RE = re.compile(r"^\d+\.\d+\.\d+$")
BASE_TAG_RE = re.compile(r"^ARG\s+BASE_TAG=(\S+)\s*$", re.M)
BASE_TAG_DATE_RE = re.compile(r"^ARG\s+BASE_TAG_DATE=(\S+)\s*$", re.M)
BASE_IMAGE_RE = re.compile(r"^ARG\s+BASE_IMAGE=(\S+)\s*$", re.M)


@dataclass(frozen=True)
class BaseTagPin:
    """The `ARG BASE_TAG=` / `ARG BASE_TAG_DATE=` pair from a Dockerfile."""

    source_file: Path
    image: str            # e.g. "ghcr.io/actions/actions-runner"
    tag: str              # e.g. "2.337.0"
    tag_date_raw: str | None
    tag_date: datetime | None

    @property
    def repository(self) -> str:
        host, _, repo = self.image.partition("/")
        return repo if ("." in host or ":" in host) else self.image

    @property
    def exemption_key(self) -> str:
        leaf = self.image.rsplit("/", 1)[-1]
        return EXEMPT_PREFIX + re.sub(r"[^A-Z0-9]", "_", leaf.upper())


def parse_base_tag(path: Path) -> BaseTagPin | None:
    """Read the base pin out of a Dockerfile. None if the file has no BASE_TAG."""
    try:
        text = path.read_text()
    except OSError as e:
        logger.warning("could not read %s: %s", path, e)
        return None

    m = BASE_TAG_RE.search(text)
    if not m:
        return None
    image_m = BASE_IMAGE_RE.search(text)
    date_m = BASE_TAG_DATE_RE.search(text)
    raw = date_m.group(1) if date_m else None
    parsed = _parse_iso8601(raw + "T00:00:00Z") if raw and re.fullmatch(r"\d{4}-\d{2}-\d{2}", raw) else None
    return BaseTagPin(
        source_file=path,
        image=image_m.group(1) if image_m else UPSTREAM_RUNNER_REPO,
        tag=m.group(1),
        tag_date_raw=raw,
        tag_date=parsed,
    )


def _registry_get(session, url: str, token: str | None, accept: str | None = None):
    headers = {}
    if accept:
        headers["Accept"] = accept
    if token:
        headers["Authorization"] = f"Bearer {token}"
    try:
        return session.get(url, headers=headers, timeout=10)
    except requests.RequestException as e:
        logger.warning("registry GET failed for %s: %s", url, e)
        return None


def list_upstream_tags(repository: str, read_token: str | None,
                       session=None) -> list[str] | None:
    """Every MAJOR.MINOR.PATCH tag in a public registry repository.

    `n=1000` in one shot rather than following Link pagination: the upstream
    runner repo carries well under a hundred semver tags, and a partial page
    would silently understate what is available — which fails OPEN, the one
    direction this check must not fail. If the page is ever full, that is a
    signal to add pagination, so it is logged.
    """
    if requests is None:
        return None
    sess = session or requests.Session()
    token = _ghcr_token(repository, read_token)
    resp = _registry_get(sess, f"https://{GHCR_HOST}/v2/{repository}/tags/list?n=1000", token)
    if resp is None or resp.status_code != 200:
        logger.warning("tag list for %s -> HTTP %s", repository,
                       getattr(resp, "status_code", "n/a"))
        return None
    try:
        tags = resp.json().get("tags") or []
    except json.JSONDecodeError:
        logger.warning("tag list for %s is not JSON", repository)
        return None
    if len(tags) >= 1000:
        logger.warning("tag list for %s hit the page limit; add pagination", repository)
    return [t for t in tags if SEMVER_TAG_RE.match(t)]


def newest_semver(tags: Iterable[str]) -> str | None:
    best = None
    for t in tags:
        if not SEMVER_TAG_RE.match(t):
            continue
        key = tuple(int(x) for x in t.split("."))
        if best is None or key > best[0]:
            best = (key, t)
    return best[1] if best else None


def fetch_tag_created(repository: str, ref: str, read_token: str | None,
                      session=None) -> datetime | None:
    """`created` from the linux/amd64 image config behind a tag or digest.

    Picks the platform explicitly instead of "first manifest that resolves":
    a modern multi-arch push also carries attestation manifests, which report
    `platform: unknown/unknown` and a config blob with no useful `created`.
    Reading one of those would date the image to the epoch and turn a stale
    pin into a pass.
    """
    if requests is None:
        return None
    sess = session or requests.Session()
    token = _ghcr_token(repository, read_token)
    resp = _registry_get(sess, f"https://{GHCR_HOST}/v2/{repository}/manifests/{ref}",
                         token, accept=MANIFEST_ACCEPT)
    if resp is None or resp.status_code != 200:
        logger.warning("manifest %s:%s -> HTTP %s", repository, ref,
                       getattr(resp, "status_code", "n/a"))
        return None
    try:
        body = resp.json()
    except json.JSONDecodeError:
        logger.warning("manifest %s:%s is not JSON", repository, ref)
        return None

    if body.get("manifests"):
        chosen = None
        for sub in body["manifests"]:
            plat = sub.get("platform") or {}
            if plat.get("os") == "linux" and plat.get("architecture") == "amd64":
                chosen = sub.get("digest")
                break
        if not chosen:
            logger.warning("no linux/amd64 manifest for %s:%s", repository, ref)
            return None
        return fetch_tag_created(repository, chosen, read_token, session=sess)

    config_digest = (body.get("config") or {}).get("digest")
    if not config_digest:
        logger.warning("no config blob for %s:%s", repository, ref)
        return None
    blob = _registry_get(sess, f"https://{GHCR_HOST}/v2/{repository}/blobs/{config_digest}",
                         token)
    if blob is None or blob.status_code != 200:
        logger.warning("config blob %s:%s -> HTTP %s", repository, ref,
                       getattr(blob, "status_code", "n/a"))
        return None
    try:
        created = blob.json().get("created")
    except json.JSONDecodeError:
        logger.warning("config blob for %s:%s is not JSON", repository, ref)
        return None
    return _parse_iso8601(created) if created else None


def evaluate_base_tag(pin: BaseTagPin, now: datetime, threshold_days: int,
                      upstream: tuple[str | None, datetime | None, datetime | None],
                      exemptions: dict[str, str]) -> tuple[list[str], list[str]]:
    """(failures, warnings) for one base-image pin.

    `upstream` is (newest_tag, newest_created, pinned_created); any element
    may be None when the registry could not answer.
    """
    failures: list[str] = []
    warnings: list[str] = []
    where = f"{pin.source_file.name} (`ARG BASE_TAG`)"

    if pin.exemption_key in exemptions:
        warnings.append(
            f"EXEMPT {pin.image}:{pin.tag} — reason: {exemptions[pin.exemption_key]}"
        )
        return failures, warnings

    if pin.tag_date is None:
        failures.append(
            f"{pin.image}:{pin.tag}: {where} has no parseable `ARG "
            f"BASE_TAG_DATE=YYYY-MM-DD` (found: {pin.tag_date_raw!r}). That "
            f"field is the offline half of this ratchet — add it with the "
            f"upstream publish date of {pin.tag}."
        )
        return failures, warnings

    newest_tag, newest_created, pinned_created = upstream

    if pinned_created is not None:
        skew = abs((pinned_created.date() - pin.tag_date.date()).days)
        if skew > 1:
            failures.append(
                f"{pin.image}:{pin.tag}: BASE_TAG_DATE says "
                f"{pin.tag_date.date().isoformat()} but the registry says the "
                f"tag was published {pinned_created.date().isoformat()}. Fix "
                f"the date in {where} — a date field that drifts from reality "
                f"is worse than no date field."
            )

    if newest_tag is None or newest_created is None:
        # Offline fallback: the stated date is all we have.
        age_days = (now - pin.tag_date).days
        if age_days > BASE_TAG_DEPRECATION_DAYS:
            failures.append(
                f"{pin.image}:{pin.tag}: pinned {age_days} days ago "
                f"({pin.tag_date.date().isoformat()}) and the upstream tag "
                f"list is unreachable. That is inside GitHub's ~60-day "
                f"runner-agent deprecation window; bump {where} and repin the "
                f"overlay digest in infra/k8s/production/arc/runner-blue/rendered.yaml."
            )
        elif age_days > threshold_days:
            warnings.append(
                f"CADENCE {pin.image}:{pin.tag} — pinned {age_days} days ago and "
                f"upstream is unreachable, so freshness could not be confirmed. "
                f"The runbook cadence is {threshold_days} days; this becomes a "
                f"hard failure at {BASE_TAG_DEPRECATION_DAYS}."
            )
        else:
            logger.info("ok base tag %s:%s pinned_age=%dd (upstream unreachable)",
                        pin.image, pin.tag, age_days)
        return failures, warnings

    if newest_tag == pin.tag:
        logger.info("ok base tag %s:%s is the newest upstream release",
                    pin.image, pin.tag)
        return failures, warnings

    behind_days = (now - newest_created).days
    if behind_days > threshold_days:
        failures.append(
            f"{pin.image}: pinned at {pin.tag} while {newest_tag} has been "
            f"published for {behind_days} days (since "
            f"{newest_created.date().isoformat()}). The cadence is "
            f"{threshold_days} days and GitHub deprecates lagging runner "
            f"agents at the registration layer at ~60. Bump BASE_TAG and "
            f"BASE_TAG_DATE in {where}, then repin the overlay digest in "
            f"infra/k8s/production/arc/runner-blue/rendered.yaml (both "
            f"occurrences) once the image build has run on main."
        )
    else:
        warnings.append(
            f"BEHIND {pin.image}: {newest_tag} is available (published "
            f"{newest_created.date().isoformat()}, {behind_days}d ago); pinned "
            f"at {pin.tag}. Hard failure at {threshold_days}d."
        )
    return failures, warnings


def check_base_tag(repo_root: Path, now: datetime, threshold_days: int,
                   read_token: str | None,
                   exemptions: dict[str, str]) -> tuple[list[str], list[str]]:
    """Run the base-tag ratchet, resolving upstream state where possible."""
    dockerfile = repo_root / ARC_DOCKERFILE
    if not dockerfile.is_file():
        logger.warning("SKIP base-tag ratchet — %s not found under %s",
                       ARC_DOCKERFILE, repo_root)
        return [], []
    pin = parse_base_tag(dockerfile)
    if pin is None:
        logger.warning("SKIP base-tag ratchet — no `ARG BASE_TAG=` in %s", dockerfile)
        return [], []

    newest_tag: str | None = None
    newest_created: datetime | None = None
    pinned_created: datetime | None = None
    # GHCR issues anonymous pull tokens for public repositories, so this half
    # of the check still runs on fork PRs where no GITHUB_TOKEN is available.
    tags = list_upstream_tags(pin.repository, read_token)
    if tags:
        newest_tag = newest_semver(tags)
        if newest_tag:
            newest_created = fetch_tag_created(pin.repository, newest_tag, read_token)
        pinned_created = fetch_tag_created(pin.repository, pin.tag, read_token)
        if newest_created is not None and newest_created < MIN_PLAUSIBLE_CREATED:
            newest_created = None
        if pinned_created is not None and pinned_created < MIN_PLAUSIBLE_CREATED:
            pinned_created = None
        if newest_created is None:
            newest_tag = None

    return evaluate_base_tag(pin, now, threshold_days,
                             (newest_tag, newest_created, pinned_created), exemptions)


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
    parser.add_argument("--base-tag-threshold-days", type=int,
                        default=BASE_TAG_THRESHOLD_DAYS,
                        help=("max days behind the newest upstream "
                              f"actions/actions-runner release (default: "
                              f"{BASE_TAG_THRESHOLD_DAYS})"))
    parser.add_argument("--skip-base-tag", action="store_true",
                        help="skip the ARC runner base-tag ratchet")
    parser.add_argument("-v", "--verbose", action="store_true",
                        help="log INFO-level details (per-image age)")
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=logging.INFO if args.verbose else logging.WARNING,
        format="%(levelname)s %(message)s",
    )

    repo_root = _resolve_repo_root(args.repo_root)
    exemptions = _read_exemptions()
    now = datetime.now(timezone.utc)
    token = os.getenv("GHCR_READ_TOKEN") or os.getenv("GITHUB_TOKEN")

    # (B) Base-tag ratchet first: it needs no repo token, it is the check
    # whose failure mode is an org-wide CI outage rather than one stale
    # service, and it must not be skipped by the token guard below.
    base_failures: list[str] = []
    base_warnings: list[str] = []
    if not args.skip_base_tag:
        base_failures, base_warnings = check_base_tag(
            repo_root, now, args.base_tag_threshold_days, token, exemptions)
        for w in base_warnings:
            logger.warning("%s", w)
        if base_failures:
            print("\nARC Base Tag Ratchet FAILED:\n", file=sys.stderr)
            for f in base_failures:
                print(f"  - {f}", file=sys.stderr)
            print("\nSee infra/docker/arc-runner/README.md for the bump policy. "
                  "Bumping the tag alone does not deploy it: the overlay digest "
                  "in infra/k8s/production/arc/runner-blue/rendered.yaml must be "
                  "repinned after the image build runs on main.", file=sys.stderr)
        else:
            print("ARC Base Tag Ratchet OK — runner base pin is within "
                  f"{args.base_tag_threshold_days} days of upstream.")

    images = discover_pinned_images(repo_root)
    if not images:
        print("No digest-pinned images found — nothing to check.")
        return 1 if base_failures else 0

    if not token:
        logger.warning(
            "no GHCR_READ_TOKEN or GITHUB_TOKEN in env — skipping digest age "
            "check. This is fine for fork PRs; ops should ensure CI has a token."
        )
        return 1 if base_failures else 0

    # Only the registry path needs `requests`. Everything above this line
    # (discovery, graceful token skip) works without it, so this is the first
    # point where a missing dependency is genuinely fatal.
    if requests is None:
        print(REQUESTS_MISSING_MSG, file=sys.stderr)
        return 2

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

    if base_failures:
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
