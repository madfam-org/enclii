"""
Guard: every pgBackRest binary that touches the shared repository runs the
same version.

Run with:
    pytest tests/scripts/test_pgbackrest_version_parity.py -v

WHY
===
After the 2026-09-18 eviction the postgres pod's init container installed
whatever PGDG shipped that day (archive-push 2.59.1) while the sidecar ran a
fixed image (backup/check/expire 2.55.1), against one repository. #628 and then
#785 (live 2026-10-18) pinned all of them to one version. The manifests say
"bump all three together"; this test makes that a CI failure instead of a
comment.

THE PINS (infra/k8s/platform-infra/)
====================================
- postgres.yaml, init container `pgbackrest-binary`: PGBACKREST_DEB_VERSION
  and PGBACKREST_EXPECTED_VERSION;
- postgres.yaml, the pgBackRest sidecar image tag (woblerr/pgbackrest:<v>-...);
- postgres-pgbackrest-restore-drill.yaml: PGBACKREST_DEB_VERSION and
  PGBACKREST_EXPECTED_VERSION, so a drill reads the repository with the
  version that writes it.
"""
from __future__ import annotations

import copy
import re
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
PLATFORM_INFRA = REPO_ROOT / "infra" / "k8s" / "platform-infra"
FILES = ("postgres.yaml", "postgres-pgbackrest-restore-drill.yaml")

SIDECAR_IMAGE = re.compile(r"(?:^|/)woblerr/pgbackrest:(\d+\.\d+(?:\.\d+)?)-")
DEB_VERSION = re.compile(r"^(\d+\.\d+(?:\.\d+)?)-")
EXPECTED_VERSION = re.compile(r"^pgBackRest (\d+\.\d+(?:\.\d+)?)$")

# Two deb pins, two expected-version checks, one sidecar image (2026-10-18).
MIN_PINS = 5


def containers(doc: dict):
    """Yield every container and init container in a Deployment or CronJob."""
    spec = doc.get("spec") or {}
    if doc.get("kind") == "CronJob":
        spec = ((spec.get("jobTemplate") or {}).get("spec") or {})
    pod = ((spec.get("template") or {}).get("spec") or {})
    for key in ("initContainers", "containers"):
        yield from pod.get(key) or []


def load_docs() -> dict[str, list[dict]]:
    return {
        name: [d for d in yaml.safe_load_all((PLATFORM_INFRA / name).read_text()) if isinstance(d, dict)]
        for name in FILES
    }


def find_pins(docs_by_file: dict[str, list[dict]]) -> list[tuple[str, str, str]]:
    """Return (file, where, version) for every pgBackRest version pin."""
    pins: list[tuple[str, str, str]] = []
    for name, docs in docs_by_file.items():
        for doc in docs:
            for c in containers(doc):
                where = f"{doc['metadata']['name']}/{c['name']}"
                m = SIDECAR_IMAGE.search(c.get("image") or "")
                if m:
                    pins.append((name, f"{where} image", m.group(1)))
                for env in c.get("env") or []:
                    value = str(env.get("value") or "")
                    if env.get("name") == "PGBACKREST_DEB_VERSION":
                        m = DEB_VERSION.match(value)
                        pins.append((name, f"{where} PGBACKREST_DEB_VERSION", m.group(1) if m else value))
                    elif env.get("name") == "PGBACKREST_EXPECTED_VERSION":
                        m = EXPECTED_VERSION.match(value)
                        pins.append((name, f"{where} PGBACKREST_EXPECTED_VERSION", m.group(1) if m else value))
    return pins


def find_problems(pins: list[tuple[str, str, str]]) -> list[str]:
    problems: list[str] = []
    if len(pins) < MIN_PINS:
        problems.append(f"found {len(pins)} pgBackRest pins, expected at least {MIN_PINS}: {pins}")
    versions = {v for _, _, v in pins}
    if len(versions) > 1:
        detail = "; ".join(f"{f} {w} = {v}" for f, w, v in pins)
        problems.append(f"pgBackRest versions differ ({sorted(versions)}): {detail}")
    return problems


def test_every_pin_names_one_version():
    assert find_problems(find_pins(load_docs())) == []


def test_each_file_carries_its_pins():
    files = {f for f, _, _ in find_pins(load_docs())}
    assert files == set(FILES)


def test_catches_a_sidecar_bumped_alone():
    docs = copy.deepcopy(load_docs())
    for doc in docs["postgres.yaml"]:
        for c in containers(doc):
            if SIDECAR_IMAGE.search(c.get("image") or ""):
                c["image"] = "docker.io/woblerr/pgbackrest:2.99.0-alpine"
    assert any("versions differ" in p for p in find_problems(find_pins(docs)))


def test_catches_a_drill_left_behind():
    docs = copy.deepcopy(load_docs())
    for doc in docs["postgres-pgbackrest-restore-drill.yaml"]:
        for c in containers(doc):
            for env in c.get("env") or []:
                if env.get("name") == "PGBACKREST_DEB_VERSION":
                    env["value"] = "2.55.1-1.pgdg12+1"
    assert any("versions differ" in p for p in find_problems(find_pins(docs)))
