"""
Behavioural guard: scripts/restore-addon.sh restores only the plan an operator
reviewed.

Run with:
    pytest tests/scripts/test_restore_addon_plan.py -v

WHAT THIS PINS (2026-10-26)
===========================
The addon restore drill is the proof that an addon backup restores. It creates
a namespace, copies a credential and builds a Cluster, so it follows the same
two-step contract as every other mutating Enclii operation: a dry run that
prints a fingerprint, then an apply bound to it with --expect-plan.

  * a dry run writes nothing and prints `PLAN-HASH <sha256>` on stdout;
  * an apply without --expect-plan, or with a fingerprint the live state no
    longer hashes to (a newer backup appeared), is refused with exit 10 before
    anything is created or a row is counted;
  * an apply with the reviewed fingerprint gets past the gate;
  * a source cluster with no backup stanza fails the DRY RUN (exit 20), not
    the apply;
  * the credential copy is rebuilt from name/type/data, so the source
    Cluster's ownerReference never reaches the scratch namespace (where the
    garbage collector would delete the copy mid-restore).

kubectl is a stub driven by a JSON state file. Every call it does not
recognise as a read is logged and fails, so "nothing was written" is a
property of the log, not an assumption.
"""
from __future__ import annotations

import json
import os
import re
import stat
import subprocess
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "restore-addon.sh"

NS = "project-fixture"
CLUSTER = "pg-kernel09-fixture"

STUB = r'''#!/usr/bin/env python3
import json, os, sys
state = json.load(open(os.environ["STUB_STATE"]))
args = sys.argv[1:]
with open(os.environ["STUB_LOG"], "a") as log:
    log.write(" ".join(args) + "\n")
def out(s):
    sys.stdout.write(s)
    sys.exit(0)
jsonpath = next((a.split("=", 1)[1] for a in args if a.startswith("jsonpath=")), None)
if "-o" in args:
    i = args.index("-o")
    if args[i + 1].startswith("jsonpath="):
        jsonpath = args[i + 1].split("=", 1)[1]
if args[:1] == ["get"] and args[1] == "cluster.postgresql.cnpg.io":
    c = state["cluster"]
    if c is None:
        sys.exit(1)
    if jsonpath is None:
        out("")
    out({"{.metadata.uid}": c["uid"],
         "{.spec.backup.barmanObjectStore.destinationPath}": c.get("dest", ""),
         "{.spec.backup.barmanObjectStore.endpointURL}": c.get("endpoint", ""),
         "{.spec.imageName}": c.get("image", "")}.get(jsonpath, ""))
if args[:1] == ["get"] and args[1] == "backup.postgresql.cnpg.io":
    if "--sort-by=.metadata.creationTimestamp" in args:
        out("".join(f"{b['name']} {state['cluster_name']}\n" for b in state["backups"]))
    name = args[2]
    b = next(b for b in state["backups"] if b["name"] == name)
    out({"{.status.startedAt}": b["started"], "{.metadata.uid}": b["uid"]}.get(jsonpath, ""))
sys.exit(1)
'''


def setup(tmp_path: Path, state: dict) -> dict:
    stub = tmp_path / "kubectl"
    stub.write_text(STUB)
    stub.chmod(stub.stat().st_mode | stat.S_IEXEC)
    (tmp_path / "state.json").write_text(json.dumps(state))
    env = {
        "PATH": os.environ["PATH"],
        "KUBECTL": str(stub),
        "STUB_STATE": str(tmp_path / "state.json"),
        "STUB_LOG": str(tmp_path / "calls.log"),
        "USER": "fixture-operator",
    }
    return env


def base_state() -> dict:
    return {
        "cluster_name": CLUSTER,
        "cluster": {
            "uid": "uid-cluster",
            "dest": f"s3://addon-backups-fixture/{NS}/{CLUSTER}",
            "endpoint": "https://fixture.r2.example",
            "image": "ghcr.io/cloudnative-pg/postgresql:18",
        },
        "backups": [{"name": f"{CLUSTER}-daily-20261026", "uid": "uid-b1", "started": "2026-10-26T04:00:05Z"}],
    }


def run(env: dict, *extra: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["bash", str(SCRIPT), "--namespace", NS, "--cluster", CLUSTER, *extra],
        env=env, capture_output=True, text=True, timeout=60,
    )


def calls(tmp_path: Path) -> list[str]:
    log = tmp_path / "calls.log"
    return log.read_text().splitlines() if log.exists() else []


def writes(tmp_path: Path) -> list[str]:
    return [c for c in calls(tmp_path) if not c.startswith("get ")]


def plan_hash(stdout: str) -> str:
    m = re.search(r"^PLAN-HASH ([0-9a-f]{64})$", stdout, re.M)
    assert m, f"dry run printed no PLAN-HASH line: {stdout!r}"
    return m.group(1)


def test_dry_run_prints_a_fingerprint_and_writes_nothing(tmp_path):
    env = setup(tmp_path, base_state())
    res = run(env, "--dry-run")
    assert res.returncode == 0, res.stderr
    plan_hash(res.stdout)
    assert "uid-b1" in res.stderr, "the reviewed plan names the selected backup"
    assert writes(tmp_path) == []
    # Deterministic: the same live state hashes the same.
    assert plan_hash(run(env, "--dry-run").stdout) == plan_hash(res.stdout)


def test_apply_without_expect_plan_is_refused_before_anything_runs(tmp_path):
    env = setup(tmp_path, base_state())
    res = run(env)
    assert res.returncode == 10, res.stderr
    assert "--expect-plan" in res.stderr
    assert writes(tmp_path) == []


def test_apply_with_a_stale_fingerprint_is_refused(tmp_path):
    state = base_state()
    env = setup(tmp_path, state)
    reviewed = plan_hash(run(env, "--dry-run").stdout)
    # A newer backup completes between the review and the apply.
    state["backups"].append({"name": f"{CLUSTER}-daily-20261027", "uid": "uid-b2", "started": "2026-10-27T04:00:05Z"})
    (tmp_path / "state.json").write_text(json.dumps(state))
    res = run(env, "--expect-plan", reviewed)
    assert res.returncode == 10, res.stderr
    assert "no longer matches" in res.stderr
    assert writes(tmp_path) == []


def test_apply_with_the_reviewed_fingerprint_passes_the_gate(tmp_path):
    env = setup(tmp_path, base_state())
    reviewed = plan_hash(run(env, "--dry-run").stdout)
    res = run(env, "--expect-plan", reviewed)
    # Past the gate the first write-adjacent step is counting the source over
    # `kubectl exec`; the stub refuses it, so the run stops there with 40.
    assert "plan matches the reviewed fingerprint" in res.stderr
    assert res.returncode == 40, res.stderr
    assert writes(tmp_path)[0].startswith("exec -n " + NS)


def test_a_cluster_without_backups_fails_the_dry_run(tmp_path):
    state = base_state()
    state["cluster"]["dest"] = ""
    env = setup(tmp_path, state)
    res = run(env, "--dry-run")
    assert res.returncode == 20, res.stderr
    assert "provisioned without backups" in res.stderr


def test_credential_copy_never_carries_the_source_owner_reference():
    text = SCRIPT.read_text()
    copy_block = text[text.index("if ! \"$KUBECTL\" get secret \"$BACKUP_CREDENTIALS_SECRET\" -n \"$SCRATCH_NS\""):]
    copy_block = copy_block[: copy_block.index("\nfi\n")]
    assert "-o go-template=" in copy_block, "rebuild the copy from name/type/data only"
    assert "-o yaml" not in copy_block, "a full-object copy carries ownerReferences and immutable"
