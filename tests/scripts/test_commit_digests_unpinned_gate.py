"""ci.yml `commit-digests`: a built service that is not pinned fails the job loudly.

Before 2026-10-17 (infra-stability audit E4-F5) a scan-enabled service whose
Trivy result was missing or not "pass" was skipped with a plain
`echo "WARNING: ..."`, and the job stayed green when 1 of N services pinned.
switchyard-api has no ArgoCD app and no push-triggered deploy, so its digest
pin IS its deploy: a silent skip left a merged control-plane change
undeployed while main CI reported done (enclii#379/#380, and the 2026-10-11
bundled-cosign scan finding).

The contract these tests pin:

  * RFC 0017 Tier B partial success is kept: a sibling's skip never blocks a
    service that built, scanned and verified. Its pin is committed and pushed.
  * Each skipped service is recorded with its reason, raised as an `::error::`
    annotation, and listed in a job-summary table.
  * The job then exits non-zero ("Fail when a built service was not pinned"),
    after the other pins are already on main.
  * The `pinned` output names only services whose pin is on main, and
    report-lifecycle reports `digest_committed` only for those.

Both steps run verbatim as the workflow carries them, with one textual change:
`/tmp/` is pointed at the test's own temp dir. `cosign`, `kustomize` and
`sleep` are shims; `git` pushes to a throwaway bare origin. Nothing here talks
to GitHub or a registry.

The pin step uses `declare -A`, so it needs bash >= 4 (the ubuntu runner has
bash 5). With an older bash the tests skip with that reason; set
ENCLII_BASH4 to a bash 4+ interpreter to run them anyway.
"""

from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

REPO = Path(__file__).resolve().parents[2]
CI = REPO / ".github" / "workflows" / "ci.yml"
REPOSITORY = "madfam-org/enclii"
FAIL_STEP = "Fail when a built service was not pinned"


def _job() -> dict:
    return yaml.safe_load(CI.read_text())["jobs"]["commit-digests"]


def _steps() -> tuple[dict, dict]:
    steps = _job()["steps"]
    pin = [s for s in steps if s.get("id") == "pin"]
    fail = [s for s in steps if s.get("name") == FAIL_STEP]
    assert len(pin) == 1, "commit-digests needs exactly one step with id: pin"
    assert len(fail) == 1, f"commit-digests needs exactly one step named {FAIL_STEP!r}"
    return pin[0], fail[0]


def _bash() -> str:
    candidate = os.environ.get("ENCLII_BASH4") or shutil.which("bash") or ""
    if not candidate:
        pytest.skip("no bash on PATH")
    proc = subprocess.run(
        [candidate, "-c", 'echo "${BASH_VERSINFO[0]}"'], capture_output=True, text=True
    )
    major = proc.stdout.strip()
    if not os.environ.get("ENCLII_BASH4") and (not major.isdigit() or int(major) < 4):
        pytest.skip(f"{candidate} is bash {major or '?'}; the pin step needs bash >= 4 (declare -A)")
    return candidate


# ---------------------------------------------------------------------------
# Structural
# ---------------------------------------------------------------------------


def test_scripts_take_inputs_from_env_not_expressions():
    """The scripts must run verbatim here, so no ${{ }} inside them."""
    pin, fail = _steps()
    for step in (pin, fail):
        assert "${{" not in step["run"], f"{step.get('name')}: move ${{{{ }}}} inputs to env"
    assert set(pin["env"]) >= {"MATRIX", "REPOSITORY"}
    assert set(fail["env"]) >= {"MATRIX", "PINNED", "PIN_OUTCOME"}


def test_fail_step_runs_after_the_pin_step_even_when_it_fails():
    steps = _job()["steps"]
    names = [s.get("id") or s.get("name") for s in steps]
    assert names.index(FAIL_STEP) == names.index("pin") + 1
    _, fail = _steps()
    assert fail["if"] == "always()"
    assert "steps.pin.outputs.pinned" in fail["env"]["PINNED"]
    assert "steps.pin.outcome" in fail["env"]["PIN_OUTCOME"]


def test_no_skip_is_a_plain_echo_warning():
    """Every skip goes through unpinned(), which records and annotates it."""
    pin, _ = _steps()
    script = pin["run"]
    assert 'echo "WARNING' not in script
    lines = [line.strip() for line in script.splitlines()]
    skips = [i for i, line in enumerate(lines) if line == "continue"]
    assert len(skips) == 4, "missing artifact, scan missing, scan failed, unverifiable signature"
    for i in skips:
        assert lines[i - 1].startswith('unpinned "$NAME" '), f"a skip without unpinned(): {lines[i - 1]!r}"


def test_job_exports_pinned_and_lifecycle_reports_only_pinned():
    doc = yaml.safe_load(CI.read_text())
    assert doc["jobs"]["commit-digests"]["outputs"]["pinned"] == "${{ steps.pin.outputs.pinned }}"
    lifecycle = doc["jobs"]["report-lifecycle"]
    cond = " ".join(lifecycle["if"].split())
    assert "needs.commit-digests.result == 'success'" in cond
    assert "needs.commit-digests.result == 'failure' && needs.commit-digests.outputs.pinned != ''" in cond
    run = lifecycle["steps"][-1]["run"]
    assert "PINNED='${{ needs.commit-digests.outputs.pinned }}'" in run
    # digest_committed sits after the pinned check, image_pushed before it.
    assert run.index('case " $PINNED " in') < run.index("event_type\": \"digest_committed")
    assert run.index("event_type\": \"image_pushed") < run.index('case " $PINNED " in')


# ---------------------------------------------------------------------------
# Behaviour: run the two steps verbatim
# ---------------------------------------------------------------------------

SERVICES = {
    # name: (scan enabled, kustomization dir)
    "switchyard-api": (True, "infra/k8s/production/switchyard-api"),
    "switchyard-ui": (True, "infra/k8s/production/switchyard-ui"),
    "roundhouse": (False, "infra/k8s/production/roundhouse"),
}


def _digest(name: str) -> str:
    return "sha256:" + hashlib.sha256(name.encode()).hexdigest()


def _git(cwd: Path, *args: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", *args],
        cwd=cwd,
        check=True,
        capture_output=True,
        text=True,
        env={**os.environ, "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_SYSTEM": os.devnull},
    )


def _shims(tmp: Path) -> Path:
    bin_dir = tmp / "shims"
    bin_dir.mkdir()
    (bin_dir / "sleep").write_text("#!/bin/sh\nexit 0\n")
    # cosign verify ... <ref>: reject refs listed in COSIGN_REJECT.
    (bin_dir / "cosign").write_text(
        "#!/bin/sh\n"
        'for last; do :; done\n'
        'for bad in $COSIGN_REJECT; do\n'
        '  case "$last" in *"$bad"*) echo "no matching signatures" >&2; exit 1;; esac\n'
        "done\n"
        'echo "[]"\n'
    )
    # kustomize edit set image NAME=REF: record it in ./kustomization.yaml.
    (bin_dir / "kustomize").write_text(
        "#!/bin/sh\n"
        '[ "$1 $2 $3" = "edit set image" ] || { echo "unexpected kustomize $*" >&2; exit 2; }\n'
        'echo "# pinned $4" >> kustomization.yaml\n'
    )
    for f in bin_dir.iterdir():
        f.chmod(0o755)
    return bin_dir


def _setup(tmp: Path, *, artifacts, scans) -> tuple[Path, Path, dict]:
    """A work clone of a bare origin, the /tmp inputs, and the step env."""
    origin = tmp / "origin.git"
    _git(tmp, "init", "--bare", "-b", "main", str(origin))
    work = tmp / "work"
    _git(tmp, "clone", str(origin), str(work))
    _git(work, "config", "user.email", "t@example.invalid")
    _git(work, "config", "user.name", "t")
    for _, kdir in SERVICES.values():
        (work / kdir).mkdir(parents=True)
        (work / kdir / "kustomization.yaml").write_text("resources: []\n")
    _git(work, "add", "-A")
    _git(work, "commit", "-m", "base")
    _git(work, "push", "origin", "main")

    t = tmp / "t"
    (t / "digests").mkdir(parents=True)
    (t / "scan-results").mkdir()
    for name in artifacts:
        scan, kdir = SERVICES[name]
        (t / "digests" / f"digest-{name}.txt").write_text(
            f"{name}|{name}|{kdir}|ghcr.io/{REPOSITORY}/{name}|{_digest(name)}\n"
        )
    for name, result in scans.items():
        (t / "scan-results" / f"scan-result-{name}.txt").write_text(result + "\n")

    matrix = {"include": [{"name": n, "scan": s} for n, (s, _) in SERVICES.items()]}
    gh = tmp / "gh"
    gh.mkdir()
    env = {
        **os.environ,
        "PATH": f"{_shims(tmp)}{os.pathsep}{os.environ['PATH']}",
        "GIT_CONFIG_GLOBAL": os.devnull,
        "GIT_CONFIG_SYSTEM": os.devnull,
        "MATRIX": json.dumps(matrix),
        "REPOSITORY": REPOSITORY,
        "GITHUB_ENV": str(gh / "env"),
        "GITHUB_OUTPUT": str(gh / "output"),
        "GITHUB_STEP_SUMMARY": str(gh / "summary"),
        "COSIGN_REJECT": "",
    }
    for k in ("env", "output", "summary"):
        (gh / k).write_text("")
    return work, t, env


def _run(script: str, tmp_inputs: Path, cwd: Path, env: dict) -> subprocess.CompletedProcess:
    script = script.replace("/tmp/", f"{tmp_inputs}/")
    path = cwd.parent / f"step-{hashlib.sha256(script.encode()).hexdigest()[:12]}.sh"
    path.write_text(script)
    # No `shell:` is set, so the runner uses its default: `bash -e {0}`.
    return subprocess.run(
        [_bash(), "-e", str(path)], cwd=cwd, env=env, capture_output=True, text=True, timeout=60
    )


def _run_both(tmp_path, *, artifacts, scans, reject=""):
    pin, fail = _steps()
    work, t, env = _setup(tmp_path, artifacts=artifacts, scans=scans)
    env["COSIGN_REJECT"] = reject
    first = _run(pin["run"], t, work, env)
    outputs = dict(
        line.split("=", 1) for line in Path(env["GITHUB_OUTPUT"]).read_text().splitlines() if "=" in line
    )
    env2 = {**env, "PINNED": outputs.get("pinned", ""), "PIN_OUTCOME": "success" if first.returncode == 0 else "failure"}
    second = _run(fail["run"], t, work, env2)
    summary = Path(env["GITHUB_STEP_SUMMARY"]).read_text()
    pinned_on_origin = {
        name
        for name, (_, kdir) in SERVICES.items()
        if f"/{name}@{_digest(name)}" in _git(tmp_path / "origin.git", "show", f"main:{kdir}/kustomization.yaml").stdout
    }
    return first, second, outputs, summary, pinned_on_origin


def test_all_built_services_pin_and_the_job_stays_green(tmp_path):
    first, second, outputs, summary, on_origin = _run_both(
        tmp_path,
        artifacts=list(SERVICES),
        scans={"switchyard-api": "pass", "switchyard-ui": "pass"},
    )
    assert first.returncode == 0, first.stdout + first.stderr
    assert second.returncode == 0, second.stdout + second.stderr
    assert on_origin == set(SERVICES)
    assert sorted(outputs["pinned"].split()) == sorted(SERVICES)
    assert "Built but NOT pinned: 0" in summary
    assert "::error" not in first.stdout + second.stdout


def test_failed_scan_skips_only_that_service_and_fails_the_job_loudly(tmp_path):
    first, second, outputs, summary, on_origin = _run_both(
        tmp_path,
        artifacts=list(SERVICES),
        scans={"switchyard-api": "fail", "switchyard-ui": "pass"},
    )
    # Tier B: the siblings' pins were committed and pushed.
    assert first.returncode == 0, first.stdout + first.stderr
    assert on_origin == {"switchyard-ui", "roundhouse"}
    assert outputs["pinned"].split() == ["switchyard-ui", "roundhouse"]
    # Loud: an annotation, a summary row with the reason, a red job.
    assert re.search(r"::error title=Built but NOT pinned: switchyard-api::Trivy scan did not pass \(result: fail\)", first.stdout)
    assert "| `switchyard-api` | Trivy scan did not pass (result: fail)" in summary
    assert "Built but NOT pinned: 1" in summary
    assert second.returncode == 1
    assert "::error title=1 built service(s) NOT pinned::switchyard-api" in second.stdout


def test_missing_scan_result_is_a_skip_with_its_own_reason(tmp_path):
    first, second, _, summary, on_origin = _run_both(
        tmp_path,
        artifacts=list(SERVICES),
        scans={"switchyard-ui": "pass"},
    )
    assert first.returncode == 0
    assert "switchyard-api" not in on_origin
    assert "| `switchyard-api` | Trivy scan result missing" in summary
    assert second.returncode == 1


def test_unverifiable_signature_and_missing_artifact_are_skips_too(tmp_path):
    first, second, outputs, summary, on_origin = _run_both(
        tmp_path,
        artifacts=["switchyard-api", "switchyard-ui"],  # roundhouse: no artifact
        scans={"switchyard-api": "pass", "switchyard-ui": "pass"},
        reject=_digest("switchyard-ui"),
    )
    assert first.returncode == 0, first.stdout + first.stderr
    assert on_origin == {"switchyard-api"}
    assert outputs["pinned"] == "switchyard-api"
    assert "| `switchyard-ui` | refusing to pin an unsigned or unverifiable digest" in summary
    assert "| `roundhouse` | no digest artifact" in summary
    assert "Built but NOT pinned: 2" in summary
    assert second.returncode == 1


def test_nothing_pinnable_fails_the_pin_step_and_the_gate(tmp_path):
    first, second, outputs, summary, on_origin = _run_both(
        tmp_path,
        artifacts=["switchyard-api", "switchyard-ui"],
        scans={"switchyard-api": "fail", "switchyard-ui": "fail"},
    )
    assert first.returncode == 1
    assert "::error::No digests resolved despite 3 services built" in first.stdout
    assert on_origin == set()
    assert "pinned" not in outputs
    assert "Pin step: failure" in summary
    assert second.returncode == 1


def test_scan_disabled_service_needs_no_scan_result(tmp_path):
    first, second, outputs, _, on_origin = _run_both(
        tmp_path,
        artifacts=list(SERVICES),
        scans={"switchyard-api": "pass", "switchyard-ui": "pass"},  # none for roundhouse
    )
    assert "roundhouse" in on_origin
    assert "roundhouse" in outputs["pinned"].split()
    assert second.returncode == 0
