"""The monthly pgBackRest restore drill must be able to pass, and must not pass by accident.

Why this file exists
--------------------
`pgbackrest-restore-drill` (namespace `data`) had never passed. Its 2026-10-01
run, the first after #627, failed about 20 minutes in. Two defects, each enough
to fail every run:

* No NetworkPolicy let the drill pod out. `default-deny-egress` selects every
  pod in `data`, so DNS and HTTPS to R2 were refused once kube-router
  programmed the pod, and every WAL segment is fetched by a NEW archive-get
  process (new DNS lookup, new connection).
* `pg_ctl -w` returned as soon as WAL redo began (with hot_standby=off the
  postmaster reports itself started at that point) and the next query was
  refused with "Hot standby mode is disabled", so the drill failed with
  "restored cluster is still in recovery".

Two ways it could have PASSED without proving anything are pinned as well:
PostgreSQL treats a failed restore_command as the end of the archive and
promotes early, so an archive-get ERROR must fail the drill; and the replay
recency limit must be shorter than the gap between the 00:00 UTC backup and
the scheduled run, or a replay that stopped at the base backup passes.

The behavioural tests run the real drill bash out of the real manifest against
stub pgbackrest / pg_ctl / pg_isready / psql binaries. The stub server refuses
every connection until pg_isready has reported it promoted, which is how a
hot_standby=off server behaves. The static tests pin the safety invariants: no
live Postgres volume, no archiving, no service-account token, and egress only
to DNS, 80 and 443. Nothing here talks to R2 or to a cluster.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
DRILL = REPO_ROOT / "infra/k8s/platform-infra/postgres-pgbackrest-restore-drill.yaml"
POSTGRES = REPO_ROOT / "infra/k8s/platform-infra/postgres.yaml"
POLICIES = REPO_ROOT / "infra/k8s/policies/data-network-policies.yaml"


def _docs(path: Path) -> list[dict]:
    return [d for d in yaml.safe_load_all(path.read_text()) if d]


def _cronjob() -> dict:
    return next(
        d
        for d in _docs(DRILL)
        if d["kind"] == "CronJob" and d["metadata"]["name"] == "pgbackrest-restore-drill"
    )


def _pod_template() -> dict:
    return _cronjob()["spec"]["jobTemplate"]["spec"]["template"]


def _pod_spec() -> dict:
    return _pod_template()["spec"]


def _drill_container() -> dict:
    return next(c for c in _pod_spec()["containers"] if c["name"] == "drill")


def _drill_script() -> str:
    return _drill_container()["command"][-1]


def _drill_env() -> dict[str, str]:
    return {e["name"]: e["value"] for e in _drill_container()["env"] if "value" in e}


# --------------------------------------------------------------------------
# Static: safety invariants of the manifest
# --------------------------------------------------------------------------


def test_restores_only_into_its_own_scratch_volume() -> None:
    """No volume of the live Postgres pod is mounted; the target is the drill's PVC."""
    live = next(d for d in _docs(POSTGRES) if d["kind"] == "Deployment" and d["metadata"]["name"] == "postgres")
    live_claims = {
        v["persistentVolumeClaim"]["claimName"]
        for v in live["spec"]["template"]["spec"]["volumes"]
        if "persistentVolumeClaim" in v
    }
    assert {"postgres-pvc", "pgbackrest-spool-pvc"} <= live_claims

    volumes = {v["name"]: v for v in _pod_spec()["volumes"]}
    for name, vol in volumes.items():
        assert "hostPath" not in vol, name
        claim = vol.get("persistentVolumeClaim", {}).get("claimName")
        assert claim not in live_claims, f"drill mounts the live volume {claim}"

    scratch = volumes["restore-target"]["persistentVolumeClaim"]["claimName"]
    assert scratch == "pgbackrest-restore-drill-pvc"
    assert any(d["kind"] == "PersistentVolumeClaim" and d["metadata"]["name"] == scratch for d in _docs(DRILL))

    mounts = {m["name"]: m["mountPath"] for m in _drill_container()["volumeMounts"]}
    assert mounts["restore-target"] == "/var/lib/restore-drill"
    script = _drill_script()
    assert "PGDATA_DIR=/var/lib/restore-drill/pgdata\n" in script
    assert '--pg1-path="${PGDATA_DIR}"' in script


def test_restored_cluster_never_archives_and_listens_on_no_tcp_address() -> None:
    script = _drill_script()
    assert "--archive-mode=off" in script
    assert "-c archive_mode=off" in script
    assert "-c listen_addresses=''" in script


def test_pod_gets_no_service_account_token() -> None:
    assert _pod_spec()["automountServiceAccountToken"] is False


def test_failure_reason_outlives_the_container_log() -> None:
    """The 2026-10-01 logs were gone before anyone read them."""
    spec = _pod_spec()
    for c in spec["initContainers"] + spec["containers"]:
        assert c.get("terminationMessagePolicy") == "FallbackToLogsOnError", c["name"]


def _selects(policy: dict, labels: dict[str, str]) -> bool:
    selector = policy["spec"].get("podSelector") or {}
    assert not selector.get("matchExpressions"), "extend _selects for matchExpressions"
    return all(labels.get(k) == v for k, v in (selector.get("matchLabels") or {}).items())


def test_egress_reaches_r2_and_nothing_toward_postgres() -> None:
    labels = _pod_template()["metadata"]["labels"]
    egress = [
        p
        for p in _docs(POLICIES)
        if p["kind"] == "NetworkPolicy"
        and p["metadata"]["namespace"] == "data"
        and "Egress" in p["spec"].get("policyTypes", [])
        and _selects(p, labels)
    ]
    # The namespace denies all egress by default; that is why the drill needs
    # an allow rule of its own.
    assert any(p["metadata"]["name"] == "default-deny-egress" and not p["spec"].get("egress") for p in egress)

    allowed: set[tuple[str, int]] = set()
    for p in egress:
        for rule in p["spec"].get("egress") or []:
            assert rule.get("ports"), f"{p['metadata']['name']}: an egress rule without ports allows every port"
            allowed |= {(port.get("protocol", "TCP"), port["port"]) for port in rule["ports"]}

    assert {("UDP", 53), ("TCP", 443), ("TCP", 80)} <= allowed
    assert ("TCP", 5432) not in allowed, "the drill must not be able to reach the live Postgres"
    assert ("TCP", 6443) not in allowed, "the drill has no business with the API server"


# --------------------------------------------------------------------------
# Behavioural: the drill bash, run against stubs
# --------------------------------------------------------------------------

STUB_PGBACKREST = """\
#!/bin/bash
case " $* " in
  *" version "*) echo "pgBackRest 2.59.1"; exit 0 ;;
  *" info "*) cat "$STATE/info.txt"; exit 0 ;;
  *" restore "*)
    for a in "$@"; do case "$a" in --pg1-path=*) P="${a#--pg1-path=}" ;; esac; done
    echo "$*" > "$STATE/restore.args"
    mkdir -p "$P/global" && touch "$P/global/pg_control"
    echo "P00   INFO: restore command end: completed successfully"
    exit 0 ;;
esac
echo "stub pgbackrest: unhandled: $*" >&2
exit 1
"""

# start: writes the fixture server log and marks the server running AND
# replaying. status: running or not. stop: not running.
STUB_PG_CTL = """\
#!/bin/bash
for a in "$@"; do last="$a"; done
case "$last" in
  start)
    while [ $# -gt 0 ]; do
      case "$1" in
        -l) log="$2"; shift 2 ;;
        -o) echo "$2" > "$STATE/pg_ctl.opts"; shift 2 ;;
        *) shift ;;
      esac
    done
    cat "$STATE/server.log" >> "$log"
    touch "$STATE/running" "$STATE/replaying"
    echo "server started"
    exit 0 ;;
  status) [ -f "$STATE/running" ] && exit 0; exit 3 ;;
  *) rm -f "$STATE/running"; exit 0 ;;
esac
"""

# Pops the next exit code from $STATE/isready (default 0 = accepting).
# 0 ends the replay (connections accepted from then on); 2 kills the server.
STUB_PG_ISREADY = """\
#!/bin/bash
echo call >> "$STATE/isready.calls"
code=0
if [ -s "$STATE/isready" ]; then
  code=$(head -n 1 "$STATE/isready")
  tail -n +2 "$STATE/isready" > "$STATE/isready.next"
  mv "$STATE/isready.next" "$STATE/isready"
fi
case "$code" in
  0) rm -f "$STATE/replaying" ;;
  2) rm -f "$STATE/running" ;;
esac
exit "$code"
"""

# A hot_standby=off server: every connection is refused until it promotes.
STUB_PSQL = """\
#!/bin/bash
if [ -f "$STATE/replaying" ]; then
  echo 'psql: error: connection to server on socket "/tmp/.s.PGSQL.5433" failed: FATAL:  the database system is not accepting connections' >&2
  echo 'DETAIL:  Hot standby mode is disabled.' >&2
  exit 2
fi
sql="${@: -1}"
case "$sql" in
  *pg_is_in_recovery*) echo f ;;
  *"FROM pg_database"*) cat "$STATE/databases.txt" ;;
  *"FROM pg_class"*) echo 120 ;;
  *) echo "stub psql: unhandled: $*" >&2; exit 1 ;;
esac
"""

INFO = """\
stanza: main
    status: ok
    cipher: aes-256-cbc

    db (current)
        wal archive min/max (15): 000000010000064E0000009A/000000010000066F000000F6

        full backup: 20261004-000007F
            timestamp start/stop: 2026-10-04 00:00:07+00 / 2026-10-04 00:04:03+00
            wal start/stop: 000000010000064E0000009B / 000000010000064E000000A2
"""


def _pg_ts(dt: datetime) -> str:
    """The format PostgreSQL logs a timestamptz in with timezone=UTC."""
    return dt.strftime("%Y-%m-%d %H:%M:%S.%f") + "+00"


def _server_log(last_tx: datetime, archive_error: bool = False, fatal: bool = False) -> str:
    begin = (
        "2026-10-07 04:20:01.000 P00   INFO: archive-get command begin 2.59.1: "
        "[000000010000066D000000CE, pg_wal/RECOVERYXLOG] --exec-id=1-abc --pg1-path=/x --stanza=main\n"
    )
    lines = ["2026-10-07 04:20:01.000 UTC [30] LOG:  starting archive recovery\n"]
    if fatal:
        lines += [
            begin,
            "2026-10-07 04:20:02.000 P00  ERROR: [049]: unable to get address for 'example.r2.cloudflarestorage.com': [-3] Temporary failure in name resolution\n",
            "2026-10-07 04:20:02.010 UTC [30] FATAL:  could not locate required checkpoint record\n",
        ]
        return "".join(lines)
    for seg in ("CE", "CF", "D0"):
        lines += [begin, f'2026-10-07 04:20:02.100 UTC [30] LOG:  restored log file "000000010000066D000000{seg}" from archive\n']
    if archive_error:
        lines += [
            begin,
            "2026-10-07 04:20:03.000 P00  ERROR: [049]: unable to get address for 'example.r2.cloudflarestorage.com': [-3] Temporary failure in name resolution\n",
        ]
    else:
        lines += [begin, "2026-10-07 04:20:03.000 P00   INFO: unable to find 000000010000066D000000D1 in the archive\n"]
    lines += [
        "2026-10-07 04:20:03.010 UTC [30] LOG:  redo done at 66D/D0000110 system usage: CPU: user: 0.10 s\n",
        f"2026-10-07 04:20:03.010 UTC [30] LOG:  last completed transaction was at log time {_pg_ts(last_tx)}\n",
        "2026-10-07 04:20:03.050 UTC [30] LOG:  selected new timeline ID: 2\n",
        "2026-10-07 04:20:03.100 UTC [30] LOG:  archive recovery complete\n",
        "2026-10-07 04:20:03.200 UTC [28] LOG:  database system is ready to accept connections\n",
    ]
    return "".join(lines)


def _gnu_date_dir(tmp_path: Path) -> Path | None:
    """A directory whose `date` is GNU date (the script uses `date -d`)."""
    if subprocess.run(["date", "--version"], capture_output=True).returncode == 0:
        return None
    gdate = shutil.which("gdate")
    if not gdate:
        pytest.skip("needs GNU date (the drill image's and CI's date); install coreutils for gdate")
    shim = tmp_path / "gnu"
    shim.mkdir()
    (shim / "date").symlink_to(gdate)
    return shim


def _run_drill(
    tmp_path: Path, server_log: str, isready: list[int]
) -> tuple[subprocess.CompletedProcess, Path]:
    state, bindir, target, scratch = (tmp_path / n for n in ("state", "bin", "target", "scratch"))
    for d in (state, bindir, target / "pgdata", scratch):
        d.mkdir(parents=True)
    for name, body in {
        "pgbackrest": STUB_PGBACKREST,
        "pg_ctl": STUB_PG_CTL,
        "pg_isready": STUB_PG_ISREADY,
        "psql": STUB_PSQL,
        "sleep": "#!/bin/bash\nexit 0\n",  # the replay poll waits 10 s per round
    }.items():
        (bindir / name).write_text(body)
        (bindir / name).chmod(0o755)
    (state / "info.txt").write_text(INFO)
    (state / "server.log").write_text(server_log)
    (state / "isready").write_text("".join(f"{c}\n" for c in isready))
    (state / "databases.txt").write_text("".join(f"db{i}\n" for i in range(6)))

    # Point the pod's paths at the temp dir; everything else runs as shipped.
    # "/tmp" goes first: on Linux the temp dir itself lives under /tmp.
    script = (
        _drill_script()
        .replace("/tmp", str(scratch))
        .replace("/opt/pgbackrest/bin/pgbackrest", str(bindir / "pgbackrest"))
        .replace("/var/lib/restore-drill", str(target))
    )
    path = [str(bindir)]
    gnu = _gnu_date_dir(tmp_path)
    if gnu:
        path.append(str(gnu))
    env = {
        **_drill_env(),
        "PATH": os.pathsep.join(path + [os.environ["PATH"]]),
        "STATE": str(state),
        "HOME": str(tmp_path),
    }
    command = _drill_container()["command"]
    proc = subprocess.run(
        [shutil.which("bash") or command[0], *command[1:-1], script],
        env=env,
        capture_output=True,
        text=True,
        timeout=60,
    )
    return proc, state


def _last_line(text: str) -> str:
    return text.rstrip("\n").splitlines()[-1]


def test_waits_for_promotion_then_passes(tmp_path: Path) -> None:
    """The 2026-10-01 defect: pg_ctl returns while the server still replays."""
    now = datetime.now(timezone.utc)
    proc, state = _run_drill(tmp_path, _server_log(last_tx=now - timedelta(seconds=40)), isready=[1, 1, 1, 0])

    out = proc.stdout + proc.stderr
    assert proc.returncode == 0, out
    assert "not accepting connections" not in out
    assert "RESTORE DRILL PASSED" in _last_line(proc.stdout)
    assert "wal_segments=3 " in proc.stdout
    assert len((state / "isready.calls").read_text().splitlines()) == 4
    assert "--archive-mode=off" in (state / "restore.args").read_text()
    opts = (state / "pg_ctl.opts").read_text()
    assert "archive_mode=off" in opts and "timezone=UTC" in opts


def test_archive_get_error_fails_although_postgres_promoted(tmp_path: Path) -> None:
    """PostgreSQL reads a failed restore_command as "end of archive" and promotes early."""
    now = datetime.now(timezone.utc)
    proc, _ = _run_drill(
        tmp_path, _server_log(last_tx=now - timedelta(seconds=40), archive_error=True), isready=[1, 0]
    )

    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "archive-get failed 1 time(s) during WAL replay" in _last_line(proc.stderr)
    assert "RESTORE DRILL PASSED" not in proc.stdout


def test_replay_that_stopped_at_the_base_backup_fails(tmp_path: Path) -> None:
    """A 00:00 UTC backup is ~4 h old at the 04:00 UTC run; that alone must not pass."""
    now = datetime.now(timezone.utc)
    proc, _ = _run_drill(tmp_path, _server_log(last_tx=now - timedelta(hours=4)), isready=[0])

    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "WAL chain incomplete" in _last_line(proc.stderr)


def test_server_that_dies_in_replay_fails_with_its_log_and_the_reason_last(tmp_path: Path) -> None:
    """The termination message (the log's tail) must end with the reason."""
    proc, _ = _run_drill(tmp_path, _server_log(last_tx=datetime.now(timezone.utc), fatal=True), isready=[1, 2])

    assert proc.returncode == 1, proc.stdout + proc.stderr
    assert "could not locate required checkpoint record" in proc.stderr
    assert "unable to get address" in proc.stderr
    assert "command begin" not in proc.stderr, "the long option dumps would crowd the termination message"
    assert "RESTORE DRILL FAILED: restored cluster stopped during WAL replay" in _last_line(proc.stderr)
