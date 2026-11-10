"""
Guard: every Dockerfile that builds cosign from source raises the same
dependency floors, and no floor sits below the version that fixes its advisory.

Run with:
    pytest tests/scripts/test_bundled_cosign_floors.py -v

WHY
===
Upstream cosign pins several dependencies below their fixed versions, so
apps/switchyard-api/Dockerfile and apps/roundhouse/Dockerfile clone the cosign
release and `go get` a floor for each one before building. The roundhouse copy
said it mirrored switchyard-api's, yet it drifted twice: by 2026-11-09 it was
missing the x/net, klauspost/compress and go-jose floors and had older x/text
and x/mod. Separately, enclii#942 raised klauspost/compress in the Go modules
(GO-2026-6865) while the bundled cosign kept v1.18.7, because the repo's own
go.mod does not reach a binary built from another module. This test turns
"keep the two in sync" from a comment into a CI failure.

It reads the Dockerfiles only; it does not build cosign. Proving a floor set
resolves and compiles stays a `go get` + `go mod tidy` + `go build` run on the
cosign release (recorded in the Dockerfile comments), and the image scans run
on the push to main.
"""
from __future__ import annotations

import re
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
DOCKERFILES = (
    "apps/switchyard-api/Dockerfile",
    "apps/roundhouse/Dockerfile",
)

# Lowest version that fixes the advisory each floor exists for. A floor may be
# higher (a newer module can require it), never lower.
FIXED_MINIMUMS = {
    "golang.org/x/text": ("v0.39.0", "CVE-2026-56852"),
    "google.golang.org/grpc": ("v1.83.2", "CVE-2026-84445"),
    "golang.org/x/mod": ("v0.40.0", "CVE-2026-56864, CVE-2026-56865"),
    "golang.org/x/net": ("v0.62.0", "CVE-2026-78661"),
    "github.com/klauspost/compress": ("v1.20.2", "GO-2026-6865"),
    "github.com/go-jose/go-jose/v4": ("v4.1.6", "CVE-2026-116587"),
}

COSIGN_VERSION = re.compile(r"^ENV COSIGN_VERSION=(\S+)$", re.M)
GO_GET = re.compile(r"\bgo get\s+((?:\S+@\S+\s*)+?)\s*&&")
SEMVER = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")


def cosign_block(text: str) -> str:
    """Return the RUN instruction that clones and builds cosign."""
    runs = [
        run
        for run in re.findall(r"^RUN .*?(?<!\\)$", text, re.M | re.S)
        if "github.com/sigstore/cosign" in run
    ]
    assert len(runs) == 1, f"expected one cosign build RUN, found {len(runs)}"
    return runs[0]


def parse_floors(text: str) -> dict[str, str]:
    """Return {module: version} from the cosign build's `go get` line."""
    block = cosign_block(text)
    gets = GO_GET.findall(block)
    assert len(gets) == 1, f"expected one `go get` in the cosign build, found {len(gets)}"
    floors: dict[str, str] = {}
    for spec in gets[0].split():
        module, _, version = spec.partition("@")
        assert module not in floors, f"{module} is listed twice"
        floors[module] = version
    return floors


def version_key(version: str) -> tuple[int, int, int]:
    match = SEMVER.match(version)
    assert match, f"floor {version!r} is not a plain release version"
    return tuple(int(part) for part in match.groups())


def read(path: str) -> str:
    return (REPO_ROOT / path).read_text()


@pytest.fixture(scope="module")
def floors_by_file() -> dict[str, dict[str, str]]:
    return {path: parse_floors(read(path)) for path in DOCKERFILES}


def test_every_cosign_building_dockerfile_is_covered():
    building = sorted(
        str(path.relative_to(REPO_ROOT))
        for path in REPO_ROOT.glob("apps/*/Dockerfile*")
        if "github.com/sigstore/cosign" in path.read_text()
    )
    assert building == sorted(DOCKERFILES)


def test_all_dockerfiles_build_the_same_cosign_release():
    versions = {path: COSIGN_VERSION.findall(read(path)) for path in DOCKERFILES}
    assert all(len(found) == 1 for found in versions.values()), versions
    assert len({found[0] for found in versions.values()}) == 1, versions


def test_floor_sets_are_identical(floors_by_file):
    reference_path, reference = next(iter(floors_by_file.items()))
    for path, floors in floors_by_file.items():
        assert floors == reference, f"{path} drifted from {reference_path}"


def test_every_floor_meets_its_fixed_version(floors_by_file):
    for path, floors in floors_by_file.items():
        assert set(floors) == set(FIXED_MINIMUMS), (
            f"{path}: add or remove the advisory for each floor in FIXED_MINIMUMS"
        )
        for module, version in floors.items():
            minimum, advisory = FIXED_MINIMUMS[module]
            assert version_key(version) >= version_key(minimum), (
                f"{path}: {module} {version} is below {minimum} ({advisory})"
            )


SAMPLE = """\
FROM golang AS builder
ENV COSIGN_VERSION=v3.1.3
RUN git clone --depth 1 -b "${COSIGN_VERSION}" https://github.com/sigstore/cosign /tmp/cosign-src && \\
    cd /tmp/cosign-src && \\
    go get golang.org/x/net@v0.62.0 github.com/klauspost/compress@v1.20.2 && \\
    go mod tidy && \\
    CGO_ENABLED=0 go build -o /usr/local/bin/cosign ./cmd/cosign
RUN go install "github.com/anchore/syft/cmd/syft@v1.46.0"
"""


def test_parser_reads_only_the_cosign_build():
    assert parse_floors(SAMPLE) == {
        "golang.org/x/net": "v0.62.0",
        "github.com/klauspost/compress": "v1.20.2",
    }


def test_parser_rejects_a_dockerfile_without_a_cosign_build():
    with pytest.raises(AssertionError, match="expected one cosign build RUN"):
        parse_floors('FROM golang\nRUN go install "github.com/anchore/syft/cmd/syft@v1.46.0"\n')


def test_parser_rejects_a_duplicated_floor():
    doubled = SAMPLE.replace(
        "github.com/klauspost/compress@v1.20.2",
        "github.com/klauspost/compress@v1.20.2 github.com/klauspost/compress@v1.18.7",
    )
    with pytest.raises(AssertionError, match="listed twice"):
        parse_floors(doubled)


def test_version_key_orders_numerically_and_rejects_pseudo_versions():
    assert version_key("v1.20.2") > version_key("v1.18.7")
    assert version_key("v0.62.0") > version_key("v0.9.9")
    with pytest.raises(AssertionError):
        version_key("v0.0.0-20230304212654-82a0ddb27589")
