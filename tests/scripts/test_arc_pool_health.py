"""Fixture tests for the ARC pool-health detector's inline script.

Why this file exists
--------------------
The alert this detector replaces (ARCNoRunnersAvailable, in
infra/k8s/production/arc/monitoring.yaml) was dead four independent ways and
nobody knew, because a detector that never fires looks exactly like a system
that is never broken. It sat there for 82 days reading as coverage.

So the bar here is not "the script runs". It is: given the *precise* state the
cluster was in on 2026-08-10, does this thing actually emit an alert — and
given the state during the recovery, does it correctly stay quiet?

The tests replay the real numbers from the outage:
    max=20 current=20 pending=20 running=0, 3,149 runners all phase=Outdated
and from the recovery, where a legitimate scale-set replacement produced zero
pods with a large backlog for about five minutes.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MANIFEST = REPO_ROOT / "infra/k8s/production/arc/pool-health-alert.yaml"

FAKE_KUBECTL = """\
#!/bin/bash
# Stub kubectl. Pool state comes from $FIXTURE_DIR/ars.txt, EphemeralRunner
# pages from er_page.json, and the breach counter from state.txt.
set -uo pipefail
args="$*"
case "$args" in
  *"get autoscalingrunnerset"*)
    if [ -f "$FIXTURE_DIR/ars_fail" ]; then echo "not found" >&2; exit 1; fi
    cat "$FIXTURE_DIR/ars.txt"; exit 0 ;;
  *"get --raw"*ephemeralrunners*)
    cat "$FIXTURE_DIR/er_page.json"; exit 0 ;;
  *"get configmap"*)
    cat "$FIXTURE_DIR/state.txt" 2>/dev/null || exit 1
    exit 0 ;;
  *"create configmap"*)
    for a in "$@"; do
      case "$a" in --from-literal=consecutive_breaches=*)
        echo "${a#--from-literal=consecutive_breaches=}" > "$FIXTURE_DIR/state_written.txt" ;;
      esac
    done
    echo "apiVersion: v1"; exit 0 ;;
  *apply*) cat > /dev/null; exit 0 ;;
esac
echo "fake-kubectl: unhandled: $args" >&2
exit 1
"""

FAKE_CURL = """\
#!/bin/bash
# Stub curl. Records the posted alert payload and returns a canned HTTP code so
# tests can assert both what was sent and how a rejection is handled.
set -uo pipefail
payload=""
outfile=""
prev=""
for a in "$@"; do
  case "$prev" in
    -d) payload="$a" ;;
    -o) outfile="$a" ;;
  esac
  prev="$a"
done
[ -n "$payload" ] && printf '%s' "$payload" > "$FIXTURE_DIR/posted.json"
[ -n "$outfile" ] && echo "stub-response" > "$outfile"
printf '%s' "$(cat "$FIXTURE_DIR/http_code.txt" 2>/dev/null || echo 200)"
exit 0
"""


def _script() -> str:
    docs = [d for d in yaml.safe_load_all(MANIFEST.read_text()) if d]
    cron = next(d for d in docs if d.get("kind") == "CronJob")
    return cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]["containers"][0]["command"][-1]


def _env_defaults() -> dict[str, str]:
    docs = [d for d in yaml.safe_load_all(MANIFEST.read_text()) if d]
    cron = next(d for d in docs if d.get("kind") == "CronJob")
    container = cron["spec"]["jobTemplate"]["spec"]["template"]["spec"]["containers"][0]
    return {e["name"]: str(e["value"]) for e in container["env"]}


class Harness:
    def __init__(self, tmp_path: Path):
        self.fixtures = tmp_path / "fx"
        self.fixtures.mkdir()
        sa = tmp_path / "sa"
        sa.mkdir()
        (sa / "token").write_text("fake-token")
        (sa / "ca.crt").write_text("fake-ca")
        self.sa = sa

        bindir = tmp_path / "bin"
        bindir.mkdir()
        for name, body in (("kubectl", FAKE_KUBECTL), ("curl", FAKE_CURL)):
            p = bindir / name
            p.write_text(body)
            p.chmod(0o755)
        self.bindir = bindir

        self.script = tmp_path / "pool-health.sh"
        self.script.write_text(_script())

        self.set_pool(max_runners=20, running=5, pending=0, current=5)
        self.set_runners(total=5, outdated=0)
        (self.fixtures / "http_code.txt").write_text("200")

    def set_pool(self, max_runners: int, running: int, pending: int, current: int):
        (self.fixtures / "ars.txt").write_text(
            f"{max_runners}|{running}|{pending}|{current}"
        )

    def set_runners(self, total: int, outdated: int, spaced: bool = False):
        """Build an EphemeralRunner page.

        `spaced` emits `": "` separators instead of the API's compact encoding,
        to prove the counting is not coupled to JSON whitespace. A miscount
        reads as zero Outdated runners, i.e. an all-clear during an outage.
        """
        items = []
        for i in range(total):
            phase = "Outdated" if i < outdated else "Running"
            items.append(
                {
                    "kind": "EphemeralRunner",
                    "metadata": {"name": f"r{i}"},
                    "status": {"phase": phase},
                }
            )
        # The real API returns compact JSON; match it by default.
        separators = (", ", ": ") if spaced else (",", ":")
        (self.fixtures / "er_page.json").write_text(
            json.dumps({"kind": "EphemeralRunnerList", "metadata": {}, "items": items},
                       separators=separators)
        )

    def set_prev_streak(self, n: int):
        (self.fixtures / "state.txt").write_text(str(n))

    def set_http_code(self, code: str):
        (self.fixtures / "http_code.txt").write_text(code)

    def fail_pool_read(self):
        (self.fixtures / "ars_fail").touch()

    def run(self) -> subprocess.CompletedProcess:
        env = dict(os.environ)
        env["PATH"] = f"{self.bindir}:{env['PATH']}"
        env["FIXTURE_DIR"] = str(self.fixtures)
        env["SERVICEACCOUNT_DIR"] = str(self.sa)
        env.update(_env_defaults())
        return subprocess.run(
            ["sh", str(self.script)], env=env, capture_output=True, text=True, timeout=120
        )

    def posted(self) -> list[dict]:
        p = self.fixtures / "posted.json"
        return json.loads(p.read_text()) if p.exists() else []

    def alertnames(self) -> set[str]:
        return {a["labels"]["alertname"] for a in self.posted()}

    def written_streak(self) -> str:
        p = self.fixtures / "state_written.txt"
        return p.read_text().strip() if p.exists() else ""


@pytest.fixture(autouse=True)
def _needs_tools():
    for tool in ("sh", "date"):
        if shutil.which(tool) is None:
            pytest.skip(f"{tool} not available")


@pytest.fixture
def h(tmp_path):
    return Harness(tmp_path)


def test_healthy_pool_is_silent(h):
    """The common case must page nobody, or the alert gets muted by humans."""
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.posted() == []
    assert "ok: no alert conditions" in result.stdout


def test_the_actual_outage_fires(h):
    """Replay of 2026-08-10: max=20, pending=20, running=0, runners Outdated.

    This is the regression test for the whole incident. If this ever goes
    quiet, the detector has stopped detecting the thing it was built for.
    """
    h.set_pool(max_runners=20, running=0, pending=20, current=20)
    h.set_runners(total=500, outdated=500)
    h.set_prev_streak(5)
    result = h.run()
    assert result.returncode == 0, result.stderr
    names = h.alertnames()
    assert "ARCRunnersDeprecated" in names
    assert "ARCPoolServingNoJobs" in names
    assert "ARCEphemeralRunnerBacklog" in names


def test_deprecated_runners_fire_immediately(h):
    """phase=Outdated has no benign cause, so it does not wait for a streak."""
    h.set_pool(max_runners=20, running=0, pending=0, current=20)
    h.set_runners(total=10, outdated=3)
    h.set_prev_streak(0)
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert "ARCRunnersDeprecated" in h.alertnames()
    # Not yet saturated for long enough to claim the pool is stalled.
    assert "ARCPoolServingNoJobs" not in h.alertnames()


def test_saturation_waits_for_the_streak(h):
    """A single bad cycle must not page.

    During the 2026-08-10 recovery a legitimate scale-set replacement produced
    ~5 minutes of zero pods while ARC drained 1,826 stale runners. Alerting on
    one cycle would have fired on a successful repair.
    """
    h.set_pool(max_runners=20, running=0, pending=20, current=20)
    h.set_runners(total=10, outdated=0)
    h.set_prev_streak(0)
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.posted() == []
    assert h.written_streak() == "1"


def test_saturation_fires_once_sustained(h):
    """Third consecutive breaching cycle at */5 == 15 minutes."""
    h.set_pool(max_runners=20, running=0, pending=20, current=20)
    h.set_runners(total=10, outdated=0)
    h.set_prev_streak(2)
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert "ARCPoolServingNoJobs" in h.alertnames()
    assert h.written_streak() == "3"


def test_recovery_resets_the_streak(h):
    """Once runners are working again the counter must go back to zero."""
    h.set_pool(max_runners=20, running=7, pending=0, current=7)
    h.set_runners(total=7, outdated=0)
    h.set_prev_streak(9)
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert h.posted() == []
    assert h.written_streak() == "0"


def test_alert_carries_severity_and_runbook(h):
    """Routing is by severity; a page with no runbook wastes the responder."""
    h.set_pool(max_runners=20, running=0, pending=20, current=20)
    h.set_runners(total=10, outdated=10)
    h.set_prev_streak(5)
    h.run()
    for alert in h.posted():
        assert alert["labels"]["severity"] in {"critical", "warning"}
        assert alert["labels"]["service"] == "arc"
        assert alert["annotations"]["runbook_url"].startswith("https://")
        assert alert["annotations"]["description"]
        # Must outlive one cycle or the alert flaps against resolve_timeout=5m.
        assert alert["endsAt"].endswith("Z")


def test_alertmanager_rejection_is_a_failure(h):
    """A non-2xx must fail the job loudly.

    Reporting success while Alertmanager drops the payload is precisely how the
    previous alert managed to be dead for 82 days without anyone noticing.
    """
    h.set_pool(max_runners=20, running=0, pending=20, current=20)
    h.set_runners(total=10, outdated=10)
    h.set_prev_streak(5)
    h.set_http_code("403")
    result = h.run()
    assert result.returncode != 0
    assert "rejected" in result.stderr


def test_unreadable_pool_fails_loudly(h):
    """If pool state cannot be read, say so — do not report all-clear."""
    h.fail_pool_read()
    result = h.run()
    assert result.returncode != 0
    assert "could not read AutoscalingRunnerSet" in result.stderr


def test_counting_survives_json_whitespace(h):
    """Outdated runners must be counted regardless of encoder spacing.

    A whitespace-coupled pattern reads zero Outdated runners, which presents as
    an all-clear in the middle of the exact outage this exists to catch.
    """
    h.set_pool(max_runners=20, running=0, pending=0, current=20)
    h.set_runners(total=10, outdated=4, spaced=True)
    h.set_prev_streak(0)
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert "ARCRunnersDeprecated" in h.alertnames()


def test_list_wrapper_is_not_counted_as_a_runner(h):
    """`"kind":"EphemeralRunnerList"` must not inflate the runner total."""
    h.set_pool(max_runners=20, running=3, pending=0, current=3)
    h.set_runners(total=3, outdated=0)
    result = h.run()
    assert result.returncode == 0, result.stderr
    assert "total_er=3" in result.stdout, result.stdout


def test_thresholds_are_sane():
    """Guard the knobs that make this trustworthy rather than noisy."""
    env = _env_defaults()
    assert int(env["BREACH_THRESHOLD"]) >= 3, "must outlast a scale-set replacement"
    assert int(env["BACKLOG_CEILING"]) >= 100
    assert env["ALERTMANAGER_URL"].startswith("http://alertmanager")


def test_manifest_is_actually_deployed():
    """The ArgoCD app must name this file in its include allowlist.

    `directory.include` is an allowlist, not a filter: a manifest dropped in
    infra/k8s/production/arc that is not named there is simply never applied.
    monitoring.yaml has sat in that directory unapplied the whole time, which is
    one of the four reasons its alert never fired. Without this assertion the
    detector could be perfectly correct and still never run — the exact class of
    silent-no-op failure it was written to catch.
    """
    app = yaml.safe_load(
        (REPO_ROOT / "infra/argocd/apps/arc-watchdog.yaml").read_text().split("---", 1)[1]
    )
    include = app["spec"]["source"]["directory"]["include"]
    assert "pool-health-alert.yaml" in include, (
        f"pool-health-alert.yaml missing from arc-watchdog include: {include!r}"
    )
    assert "stuck-runner-watchdog.yaml" in include, include
    assert app["spec"]["source"]["path"] == "infra/k8s/production/arc"


def test_rbac_covers_every_resource_the_script_reads():
    """Least-privilege RBAC that omits a verb fails at runtime, not at deploy."""
    docs = [d for d in yaml.safe_load_all(MANIFEST.read_text()) if d]
    roles = {d["metadata"]["name"]: d for d in docs if d.get("kind") == "Role"}

    reader = roles["arc-pool-health-reader"]
    arc_rule = next(
        r for r in reader["rules"] if "actions.github.com" in r["apiGroups"]
    )
    assert {"autoscalingrunnersets", "ephemeralrunners"} <= set(arc_rule["resources"])
    assert {"get", "list"} <= set(arc_rule["verbs"])
    # Read-only: this detector must never be able to mutate the pool it watches.
    assert not {"delete", "update", "patch", "create"} & set(arc_rule["verbs"])

    state = roles["arc-pool-health-state"]
    verbs = {v for r in state["rules"] for v in r["verbs"]}
    assert {"get", "create"} <= verbs
    assert "update" in verbs or "patch" in verbs
