"""Fixture tests for the stuck-runner watchdog's inline script.

Why this file exists
--------------------
On 2026-08-10 the watchdog was OOMKilled on every cycle for the whole duration
of an org-wide CI outage. It read every EphemeralRunner in the namespace with an
unbounded list, and an EphemeralRunner CR is ~5.8 KB because it embeds the
runner pod template. At 3,149 runners that is ~18 MB of JSON before kubectl's
deserialization overhead, against a 128Mi limit.

Both failure modes were *backlog-proportional*: the watchdog worked in normal
conditions and died in exactly the conditions it exists to detect. A test that
only exercises a handful of runners would have stayed green throughout, so the
scale cases below (7, 8, 9) are the ones that actually matter.

The tests run the real bash out of the real manifest against a stub kubectl, so
they cover the shipped artifact rather than a copy that can drift from it.
"""

from __future__ import annotations

import os
import shutil
import subprocess
import textwrap
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST = REPO_ROOT / "infra/k8s/production/arc/stuck-runner-watchdog.yaml"

# Must match MAX_SCAN in the manifest. Asserted in test_request_is_bounded.
EXPECTED_MAX_SCAN = 400

FAKE_KUBECTL = """\
#!/bin/bash
# Stub kubectl. Serves canned responses from $FIXTURE_DIR and records calls so
# tests can assert on what the watchdog actually asked the API for.
set -uo pipefail
args="$*"
case "$args" in
  *"get --raw"*ephemeralrunners*)
    for a in "$@"; do
      case "$a" in */ephemeralrunners*) echo "$a" >> "$FIXTURE_DIR/raw_urls.log" ;; esac
    done
    cat "$FIXTURE_DIR/er_page.json"; exit 0 ;;
  *"get pods"*)
    echo "get pods" >> "$FIXTURE_DIR/pod_calls.log"
    if [ -f "$FIXTURE_DIR/pods_fail" ]; then
      echo "the server was unable to return a response" >&2; exit 1
    fi
    cat "$FIXTURE_DIR/pods.txt"; exit 0 ;;
  *"delete ephemeralrunner"*)
    for a in "$@"; do
      case "$a" in madfam-*) echo "$a" >> "$FIXTURE_DIR/deletes.log" ;; esac
    done
    echo "deleted"; exit 0 ;;
  *exec*unner.Worker*)
    cat "$FIXTURE_DIR/worker_state.txt" 2>/dev/null || echo "active"; exit 0 ;;
  *logs*)
    cat "$FIXTURE_DIR/runner_log.txt" 2>/dev/null || true; exit 0 ;;
esac
echo "fake-kubectl: unhandled call: $args" >&2
exit 1
"""


def _script() -> str:
    """Pull the watchdog's bash out of the CronJob manifest."""
    docs = [d for d in yaml.safe_load_all(MANIFEST.read_text()) if d]
    cron = next(d for d in docs if d.get("kind") == "CronJob")
    container = cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]["containers"][0]
    return container["command"][-1]


def _resources() -> dict:
    docs = [d for d in yaml.safe_load_all(MANIFEST.read_text()) if d]
    cron = next(d for d in docs if d.get("kind") == "CronJob")
    return cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]["containers"][0]["resources"]


def _iso_ago(seconds: int) -> str:
    return (datetime.now(timezone.utc) - timedelta(seconds=seconds)).strftime(
        "%Y-%m-%dT%H:%M:%SZ"
    )


def _page(runners: list[tuple[str, int, str, str]], continue_token: str = "") -> dict:
    """Build an EphemeralRunner list page: (name, age_seconds, phase, run_id)."""
    items = []
    for name, age, phase, run_id in runners:
        status: dict[str, str] = {}
        if phase:
            status["phase"] = phase
        if run_id:
            status["workflowRunId"] = run_id
        items.append(
            {
                "metadata": {"name": name, "creationTimestamp": _iso_ago(age)},
                "status": status,
            }
        )
    meta = {"continue": continue_token} if continue_token else {}
    return {"metadata": meta, "items": items}


class Harness:
    def __init__(self, tmp_path: Path):
        self.dir = tmp_path
        self.fixtures = tmp_path / "fx"
        self.fixtures.mkdir()
        (self.fixtures / "deletes.log").touch()
        (self.fixtures / "pods.txt").write_text("")

        sa = tmp_path / "sa"
        sa.mkdir()
        (sa / "token").write_text("fake-token")
        (sa / "ca.crt").write_text("fake-ca")
        self.sa = sa

        bindir = tmp_path / "bin"
        bindir.mkdir()
        kubectl = bindir / "kubectl"
        kubectl.write_text(FAKE_KUBECTL)
        kubectl.chmod(0o755)
        self.bindir = bindir

        self.script = tmp_path / "watchdog.sh"
        self.script.write_text(_script())

    def set_page(self, runners, continue_token: str = ""):
        import json

        (self.fixtures / "er_page.json").write_text(
            json.dumps(_page(runners, continue_token))
        )

    def set_pods(self, mapping: dict[str, str]):
        (self.fixtures / "pods.txt").write_text(
            "".join(f"{k}={v}\n" for k, v in mapping.items())
        )

    def fail_pod_read(self):
        (self.fixtures / "pods_fail").touch()

    def set_worker_state(self, state: str):
        (self.fixtures / "worker_state.txt").write_text(state + "\n")

    def run(self) -> subprocess.CompletedProcess:
        env = dict(os.environ)
        env["PATH"] = f"{self.bindir}:{env['PATH']}"
        env["FIXTURE_DIR"] = str(self.fixtures)
        env["SERVICEACCOUNT_DIR"] = str(self.sa)
        env["KUBERNETES_SERVICE_HOST"] = "kubernetes.default.svc"
        env["KUBERNETES_SERVICE_PORT"] = "443"
        return subprocess.run(
            ["bash", str(self.script)],
            env=env,
            capture_output=True,
            text=True,
            timeout=180,
        )

    def deletions(self) -> list[str]:
        return (self.fixtures / "deletes.log").read_text().split()

    def raw_urls(self) -> list[str]:
        p = self.fixtures / "raw_urls.log"
        return p.read_text().split() if p.exists() else []

    def pod_call_count(self) -> int:
        p = self.fixtures / "pod_calls.log"
        return len(p.read_text().splitlines()) if p.exists() else 0


@pytest.fixture(autouse=True)
def _needs_tools():
    for tool in ("bash", "jq", "date"):
        if shutil.which(tool) is None:
            pytest.skip(f"{tool} not available")
    # The script parses RFC3339 timestamps with `date -d`, which is GNU-only.
    # CI (Ubuntu on ARC) and the runtime image (Debian) both have it; macOS
    # ships BSD date and would fail with a misleading "illegal option -- d"
    # rather than a real defect. Skip there instead of reporting a false red.
    probe = subprocess.run(
        ["date", "-d", "@0", "+%s"], capture_output=True, text=True
    )
    if probe.returncode != 0:
        pytest.skip("GNU date required (`date -d`); BSD date found")


@pytest.fixture
def h(tmp_path):
    return Harness(tmp_path)


def test_orphaned_runner_past_threshold_is_deleted(h):
    """Pod is gone and the CR is older than the 20-minute orphan threshold."""
    h.set_page([("madfam-a", 1800, "Running", "9001")])
    h.set_pods({})
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.deletions() == ["madfam-a"]


def test_fresh_runner_without_workflow_run_is_untouched(h):
    """No workflow run id means it is waiting for work, not stuck."""
    h.set_page([("madfam-fresh", 30, "", "")])
    h.set_pods({})
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.deletions() == []


def test_busy_runner_under_hard_ceiling_is_untouched(h):
    """A live pod with an active worker is a long job, not a zombie.

    This is the case that protects Playwright/e2e runs from being culled.
    """
    h.set_page([("madfam-busy", 1800, "Running", "9002")])
    h.set_pods({"madfam-busy": "Running"})
    h.set_worker_state("active")
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.deletions() == []


def test_live_pod_with_inactive_worker_is_deleted(h):
    """Pod says Running but no Runner.Worker process — the zombie case."""
    h.set_page([("madfam-zombie", 1800, "Running", "9003")])
    h.set_pods({"madfam-zombie": "Running"})
    h.set_worker_state("inactive")
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.deletions() == ["madfam-zombie"]


def test_backlog_warns_loudly_and_keeps_working(h):
    """A continue token means more runners than we will read in one cycle.

    The watchdog must NOT die here. Dying silently on a large backlog is the
    exact 2026-08-10 failure, and a backlog is when the signal matters most.
    """
    h.set_page([("madfam-a", 1800, "Running", "9004")], continue_token="TOKEN123")
    h.set_pods({})
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert "BACKLOG:" in result.stderr
    assert h.deletions() == ["madfam-a"]


def test_pod_read_failure_skips_cycle_without_deleting(h):
    """If pod state is unknown, deleting would be a guess. Do nothing instead."""
    h.set_page([("madfam-a", 1800, "Running", "9005")])
    h.fail_pod_read()
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.deletions() == []


def test_scales_to_a_full_page(h):
    """A full page must complete — the old per-runner pod read could not.

    At 3,149 runners the old code issued one `get pod` per runner at a 10s
    timeout, which cannot finish inside the */5 schedule regardless of memory.
    """
    runners = [(f"madfam-r{i}", 1800, "Running", f"90{i}") for i in range(EXPECTED_MAX_SCAN)]
    h.set_page(runners)
    h.set_pods({})
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert len(h.deletions()) == EXPECTED_MAX_SCAN


def test_request_is_bounded(h):
    """The API request carries an explicit limit.

    This is THE fix. Raising the memory limit alone would only move the cliff
    from 3,149 runners to some larger number; bounding the request removes it.
    """
    h.set_page([("madfam-a", 1800, "Running", "9006")])
    h.set_pods({})
    h.run()
    urls = h.raw_urls()
    assert len(urls) == 1, f"expected exactly one list call, got {urls}"
    assert f"limit={EXPECTED_MAX_SCAN}" in urls[0], urls[0]


def test_pod_reads_are_bulk_not_per_runner(h):
    """One pod list per cycle, no matter how many runners are in the page."""
    runners = [(f"madfam-r{i}", 1800, "Running", f"90{i}") for i in range(EXPECTED_MAX_SCAN)]
    h.set_page(runners)
    h.set_pods({})
    h.run()
    assert h.pod_call_count() == 1


def test_memory_limit_has_headroom_over_the_bounded_page():
    """Guard the limit against being trimmed back toward the OOM cliff."""
    res = _resources()
    limit = res["limits"]["memory"]
    assert limit.endswith("Mi"), limit
    assert int(limit[:-2]) >= 256, f"memory limit {limit} is too close to the 128Mi that OOMKilled"
