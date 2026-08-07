"""
Tests for scripts/check-probe-targets.py.

Run with:
    pytest tests/scripts/test_check_probe_targets.py -v

Read `test_would_not_have_caught_fault_2_alone` first: it pins the honest
limitation of this check, so nobody later mistakes it for the control that
catches an unpublished Service port.
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-probe-targets.py"


def run_script(*roots: Path) -> tuple[int, str, str]:
    proc = subprocess.run(
        [sys.executable, str(SCRIPT), *[str(r) for r in roots]],
        capture_output=True,
        text=True,
    )
    return proc.returncode, proc.stdout, proc.stderr


def configmap(targets: list[dict], name: str = "cloudflared-probe-targets") -> str:
    payload = json.dumps({"targets": targets}, indent=2)
    indented = "\n".join("    " + line for line in payload.splitlines())
    return (
        "apiVersion: v1\n"
        "kind: ConfigMap\n"
        "metadata:\n"
        f"  name: {name}\n"
        "  namespace: cloudflare-tunnel\n"
        "data:\n"
        "  targets.json: |\n" + indented + "\n"
    )


def write(dir_: Path, name: str, body: str) -> Path:
    p = dir_ / name
    p.write_text(body)
    return p


GOOD_TARGET = {
    "name": "dhanam-api",
    "url": "http://dhanam-api.dhanam.svc.cluster.local:80/health",
    "expected_status": 200,
    "namespace": "dhanam",
    "service": "dhanam-api",
    "port": 80,
}


# ---------------------------------------------------------------------------
# The honest limitation
# ---------------------------------------------------------------------------


def test_would_not_have_caught_fault_2_alone(tmp_path: Path) -> None:
    """The broken 2026-08-06 entry was internally consistent.

    `dhanam-api` dialed :3000 and declared `"port": 3000` — self-consistent,
    and wrong, because the Service publishes only 80. Nothing in this repo
    knows that. This static check therefore PASSES on the exact fault, and
    that is by design: the control that catches it is the runtime Service
    lookup in infra/cloudflared-probe/probe.py. Recorded as a test so the
    boundary cannot be forgotten.
    """
    broken = dict(
        GOOD_TARGET,
        url="http://dhanam-api.dhanam.svc.cluster.local:3000/health",
        port=3000,
    )
    write(tmp_path, "cm.yaml", configmap([broken]))
    code, out, _ = run_script(tmp_path)
    assert code == 0, out


# ---------------------------------------------------------------------------
# What it does catch
# ---------------------------------------------------------------------------


def test_passes_on_the_current_shape(tmp_path: Path) -> None:
    write(tmp_path, "cm.yaml", configmap([GOOD_TARGET]))
    code, out, _ = run_script(tmp_path)
    assert code == 0, out
    assert "1 probe-target ConfigMap" in out


def test_catches_url_port_diverging_from_declared_port(tmp_path: Path) -> None:
    bad = dict(GOOD_TARGET, url="http://dhanam-api.dhanam.svc.cluster.local:3000/health")
    write(tmp_path, "cm.yaml", configmap([bad]))
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "dials port 3000" in out
    assert '"port": 80' in out


def test_catches_host_not_matching_service_and_namespace(tmp_path: Path) -> None:
    bad = dict(GOOD_TARGET, url="http://dhanam-api.janua.svc.cluster.local:80/health")
    write(tmp_path, "cm.yaml", configmap([bad]))
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "dhanam-api.dhanam.svc.cluster.local" in out


def test_implicit_port_80_is_accepted(tmp_path: Path) -> None:
    ok = dict(GOOD_TARGET, url="http://dhanam-api.dhanam.svc.cluster.local/health")
    write(tmp_path, "cm.yaml", configmap([ok]))
    code, out, _ = run_script(tmp_path)
    assert code == 0, out


def test_implicit_port_80_conflicting_with_declared_port_fails(tmp_path: Path) -> None:
    bad = dict(
        GOOD_TARGET, url="http://dhanam-api.dhanam.svc.cluster.local/health", port=3000
    )
    write(tmp_path, "cm.yaml", configmap([bad]))
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "dials port 80" in out


def test_catches_missing_fields(tmp_path: Path) -> None:
    bad = {k: v for k, v in GOOD_TARGET.items() if k != "service"}
    write(tmp_path, "cm.yaml", configmap([bad]))
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "missing required field" in out


def test_catches_duplicate_target_names(tmp_path: Path) -> None:
    write(tmp_path, "cm.yaml", configmap([GOOD_TARGET, dict(GOOD_TARGET)]))
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "duplicate target name" in out


def test_catches_invalid_json(tmp_path: Path) -> None:
    body = (
        "apiVersion: v1\n"
        "kind: ConfigMap\n"
        "metadata:\n"
        "  name: cloudflared-probe-targets\n"
        "data:\n"
        "  targets.json: |\n"
        '    {"targets": [ {"name": "x",, } ] }\n'
    )
    write(tmp_path, "cm.yaml", body)
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "not valid JSON" in out


def test_comment_only_entries_are_skipped(tmp_path: Path) -> None:
    write(tmp_path, "cm.yaml", configmap([{"//": "a note"}, GOOD_TARGET]))
    code, out, _ = run_script(tmp_path)
    assert code == 0, out


def test_bare_root_path_warns_but_passes(tmp_path: Path) -> None:
    ok = dict(GOOD_TARGET, url="http://dhanam-api.dhanam.svc.cluster.local:80/")
    write(tmp_path, "cm.yaml", configmap([ok]))
    code, out, _ = run_script(tmp_path)
    assert code == 0
    assert "WARN" in out


def test_non_probe_configmaps_are_ignored(tmp_path: Path) -> None:
    write(
        tmp_path,
        "other.yaml",
        "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: unrelated\ndata:\n  a: b\n",
    )
    code, out, _ = run_script(tmp_path)
    assert code == 0
    assert "checked 0 probe-target ConfigMap" in out


def test_missing_root_exits_2(tmp_path: Path) -> None:
    code, _, err = run_script(tmp_path / "nope")
    assert code == 2
    assert "does not exist" in err


# ---------------------------------------------------------------------------
# The live repository must stay clean
# ---------------------------------------------------------------------------


def test_repository_passes() -> None:
    code, out, err = run_script(REPO_ROOT / "infra" / "k8s")
    assert code == 0, f"infra/k8s probe targets are inconsistent:\n{out}\n{err}"


def test_live_configmap_is_actually_scanned() -> None:
    """Guards against the check silently scanning nothing (the
    `listed=65 exposed=0` failure mode)."""
    code, out, _ = run_script(REPO_ROOT / "infra" / "k8s")
    assert code == 0
    assert "checked 0 probe-target ConfigMap" not in out
