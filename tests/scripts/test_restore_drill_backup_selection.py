"""The logical restore drill must restore the newest DATED dump, never latest.sql.gz.

Why this file exists
--------------------
`postgres-backup` uploads every daily dump twice into the same R2 prefix:
first as `<YYYYMMDD_HHMMSS>.sql.gz`, then straight afterwards as the rolling
`latest.sql.gz`. The drill's download init container took "the newest object"
with `aws s3 ls | sort | tail -1`; every listing line starts with the upload
time, so the rolling copy always won. The `restore-drill` container derives the
dump's age from the stamp in the key name and rejects any name that is not
`<YYYYMMDD_HHMMSS>.sql.gz`, so a monthly run that picked `latest.sql.gz` failed
seconds after the download.

The tests run the real init-container bash out of the real manifest against a
stub `aws`, so they cover the shipped artifact and not a copy that can drift
from it. Nothing here talks to R2 or to a cluster.
"""

from __future__ import annotations

import os
import re
import shutil
import subprocess
import textwrap
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST = REPO_ROOT / "infra/k8s/production/backup/restore-drill-cronjob.yaml"

# Stub aws CLI. `s3 ls` serves $FIXTURE_DIR/listing.txt; `s3 cp` records the
# source URL it was asked to fetch and writes a few bytes to the destination.
FAKE_AWS = """\
#!/bin/bash
set -uo pipefail
case "$1 $2" in
  "s3 ls")
    cat "$FIXTURE_DIR/listing.txt"; exit 0 ;;
  "s3 cp")
    echo "$3" >> "$FIXTURE_DIR/cp.log"
    echo "dump" > "$4"; exit 0 ;;
esac
echo "fake-aws: unhandled call: $*" >&2
exit 1
"""

# `aws s3 ls` prints "<date> <time> <size right-aligned to 10> <key>", one
# line per object, keys in alphabetical order. The drill's `sort` therefore
# ordered by upload time, and latest.sql.gz (uploaded straight after the dated
# dump) was always the newest object.
ROLLING_COPY_NEWEST = textwrap.dedent(
    """\
    2026-10-05 01:03:31  419100000 20261005_010011.sql.gz
    2026-10-06 01:03:29  419200000 20261006_010009.sql.gz
    2026-10-07 01:03:34  419430400 20261007_010012.sql.gz
    2026-10-07 01:03:52  419430400 latest.sql.gz
    """
)

# A stray hand-uploaded object and a sub-prefix marker must not be picked
# either, even though the stray is the newest object in the listing.
STRAYS_NEWEST = textwrap.dedent(
    """\
                               PRE archive/
    2026-10-05 01:03:31  419100000 20261005_010011.sql.gz
    2026-10-06 01:03:29  419200000 20261006_010009.sql.gz
    2026-10-07 01:03:34  419430400 20261007_010012.sql.gz
    2026-10-07 01:03:52  419430400 latest.sql.gz
    2026-10-07 02:10:00     123456 manual-test.sql.gz
    """
)


def _pod_spec() -> dict:
    docs = [d for d in yaml.safe_load_all(MANIFEST.read_text()) if d]
    cron = next(
        d
        for d in docs
        if d.get("kind") == "CronJob" and d["metadata"]["name"] == "postgres-restore-drill"
    )
    return cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]


def _init_command() -> list[str]:
    init = next(c for c in _pod_spec()["initContainers"] if c["name"] == "download-backup")
    return init["command"]


def _run_init(tmp_path: Path, listing: str) -> tuple[subprocess.CompletedProcess, Path, Path]:
    """Run the init container's script; return (process, fixture dir, /shared dir)."""
    fixture = tmp_path / "fixture"
    shared = tmp_path / "shared"
    bindir = tmp_path / "bin"
    for d in (fixture, shared, bindir):
        d.mkdir()
    (fixture / "listing.txt").write_text(listing)
    aws = bindir / "aws"
    aws.write_text(FAKE_AWS)
    aws.chmod(0o755)

    command = _init_command()
    # The pod mounts its scratch volume at /shared; point it at a temp dir.
    script = command[-1].replace("/shared", str(shared))
    env = {
        "PATH": f"{bindir}{os.pathsep}{os.environ['PATH']}",
        "FIXTURE_DIR": str(fixture),
        "R2_ACCOUNT_ID": "test-account",
        "R2_BUCKET": "enclii-backups",
        "HOME": str(tmp_path),
    }
    # Same argv shape as the pod: bash -euo pipefail -c <script>.
    proc = subprocess.run(
        [shutil.which("bash") or command[0], *command[1:-1], script],
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )
    return proc, fixture, shared


def _name_gate() -> re.Pattern:
    """The key-name gate the restore-drill container applies to the download."""
    restore = next(c for c in _pod_spec()["containers"] if c["name"] == "restore-drill")
    match = re.search(r'\[\[ "\$\{STAMP\}" =~ (\S+) \]\]', restore["command"][-1])
    assert match, "restore-drill no longer gates on the backup key name; update this test"
    return re.compile(match.group(1))


@pytest.mark.parametrize(
    "listing, expected",
    [
        pytest.param(ROLLING_COPY_NEWEST, "20261007_010012.sql.gz", id="rolling-copy-is-newest-object"),
        pytest.param(STRAYS_NEWEST, "20261007_010012.sql.gz", id="strays-and-prefix-markers-ignored"),
    ],
)
def test_picks_newest_dated_dump(tmp_path: Path, listing: str, expected: str) -> None:
    proc, fixture, shared = _run_init(tmp_path, listing)

    assert proc.returncode == 0, proc.stdout + proc.stderr
    assert (fixture / "cp.log").read_text().splitlines() == [
        f"s3://enclii-backups/postgres/{expected}"
    ]
    assert (shared / "backup-name.txt").read_text().strip() == expected

    # The pick must pass the restore-drill container's own name gate, and the
    # rolling copy must not: that mismatch is what failed the drill.
    gate = _name_gate()
    assert gate.match(expected.split(".")[0])
    assert not gate.match("latest")


def test_only_rolling_copy_fails_with_a_message(tmp_path: Path) -> None:
    """No dated dump at all: fail on the explicit message, download nothing.

    Under `set -o pipefail` a filter that exits 1 on "no match" would abort the
    script before it reaches the FAIL line, leaving a bare non-zero exit.
    """
    proc, fixture, shared = _run_init(
        tmp_path, "2026-10-07 01:03:52  419430400 latest.sql.gz\n"
    )

    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "FAIL: No backups found" in proc.stdout
    assert not (fixture / "cp.log").exists()
    assert not (shared / "backup-name.txt").exists()
