"""The digest-pin push retry in the self-pinning workflows (#665).

weighbridge.yml and deploy-pipeline-monitor.yml commit their own image digest
to main after a build. Several such workflows fire on one push and race, so
the "Commit digest pin" step retries a rejected push up to five times: fetch,
reset onto the fresh origin/main, re-apply OUR digest, and exit 0 when fresh
main already pins it.

These tests run that step's script verbatim, as the workflow carries it,
against a throwaway bare origin. Nothing here talks to GitHub.
"""

from __future__ import annotations

import os
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

REPO = Path(__file__).resolve().parents[2]
WORKFLOWS = REPO / ".github" / "workflows"

OLD_DIGEST = "sha256:" + "0" * 64
OUR_DIGEST = "sha256:" + "a" * 64

CASES = [
    pytest.param(
        "weighbridge.yml",
        "ghcr.io/madfam-org/enclii/weighbridge",
        "infra/k8s/production/monitoring/weighbridge.yaml",
        id="weighbridge",
    ),
    pytest.param(
        "deploy-pipeline-monitor.yml",
        "ghcr.io/madfam-org/enclii/deploy-pipeline-monitor",
        "infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml",
        id="deploy-pipeline-monitor",
    ),
]


def _pin_step(workflow: str) -> tuple[str, dict[str, str]]:
    """Return the pin_commit step's script and the workflow-level env."""
    doc = yaml.safe_load((WORKFLOWS / workflow).read_text())
    env = {k: str(v) for k, v in (doc.get("env") or {}).items()}
    for job in doc["jobs"].values():
        for step in job.get("steps", []):
            if step.get("id") == "pin_commit":
                return step["run"], env
    raise AssertionError(f"{workflow}: no step with id pin_commit")


def _git(cwd: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["git", *args], cwd=cwd, check=check, capture_output=True, text=True,
        env={**os.environ, "GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_SYSTEM": os.devnull},
    )


def _manifest(image: str, digest: str) -> str:
    return f"containers:\n  - image: {image}@{digest}\n"


def _shims(tmp: Path) -> Path:
    """A PATH prefix with a no-op `sleep` (the retry backs off 2-10 s) and,
    where the system sed is BSD, a `sed` that accepts the GNU `-E -i <script>`
    form the workflow uses on its Linux runner."""
    bin_dir = tmp / "shims"
    bin_dir.mkdir()
    (bin_dir / "sleep").write_text("#!/bin/sh\nexit 0\n")
    gnu = subprocess.run(["sed", "--version"], capture_output=True).returncode == 0
    if not gnu:
        real = shutil.which("sed")
        (bin_dir / "sed").write_text(
            "#!/bin/sh\n"
            'if [ "$1" = "-E" ] && [ "$2" = "-i" ]; then shift 2; exec '
            + real + ' -E -i "" "$@"; fi\n'
            "exec " + real + ' "$@"\n'
        )
    for shim in bin_dir.iterdir():
        shim.chmod(0o755)
    return bin_dir


class Origin:
    """A bare origin with main, and a runner checkout cloned from it."""

    def __init__(self, tmp: Path, image: str, manifest: str):
        self.tmp, self.image, self.manifest = tmp, image, manifest
        self.bare = tmp / "origin.git"
        _git(tmp, "init", "--bare", "-b", "main", str(self.bare))
        seed = tmp / "seed"
        _git(tmp, "clone", str(self.bare), str(seed))
        self._identity(seed)
        (seed / manifest).parent.mkdir(parents=True)
        (seed / manifest).write_text(_manifest(image, OLD_DIGEST))
        (seed / "other.txt").write_text("base\n")
        _git(seed, "add", "-A")
        _git(seed, "commit", "-m", "base")
        _git(seed, "push", "origin", "HEAD:main")
        self.seed = seed
        self.runner = tmp / "runner"
        _git(tmp, "clone", str(self.bare), str(self.runner))

    @staticmethod
    def _identity(repo: Path) -> None:
        _git(repo, "config", "user.name", "test")
        _git(repo, "config", "user.email", "test@example.test")

    def land_on_main(self, path: str, content: str, message: str) -> None:
        """Another workflow's commit lands on main after the runner cloned."""
        (self.seed / path).write_text(content)
        _git(self.seed, "add", path)
        _git(self.seed, "commit", "-m", message)
        _git(self.seed, "push", "origin", "HEAD:main")

    def reject_every_push(self) -> None:
        hook = self.bare / "hooks" / "pre-receive"
        hook.write_text("#!/bin/sh\necho 'rejected by test hook' >&2\nexit 1\n")
        hook.chmod(0o755)

    def main_file(self, path: str) -> str:
        return _git(self.tmp, "--git-dir", str(self.bare), "show", f"main:{path}").stdout

    def main_log(self) -> list[str]:
        out = _git(self.tmp, "--git-dir", str(self.bare), "log", "--format=%s", "main").stdout
        return out.splitlines()

    def run_step(self, script: str, env: dict[str, str]) -> subprocess.CompletedProcess:
        shims = _shims(self.tmp)
        full_env = {
            **os.environ,
            **env,
            "DIGEST": OUR_DIGEST,
            "PATH": f"{shims}{os.pathsep}{os.environ['PATH']}",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_SYSTEM": os.devnull,
        }
        return subprocess.run(["bash", "-c", script], cwd=self.runner, env=full_env,
                              capture_output=True, text=True, timeout=120)


@pytest.mark.parametrize("workflow,image,manifest", CASES)
def test_pin_step_targets_its_own_image_and_manifest(workflow, image, manifest):
    script, env = _pin_step(workflow)
    rendered = script.replace("${IMAGE}", env.get("IMAGE", "")).replace("${MANIFEST}", env.get("MANIFEST", ""))
    assert image in rendered
    assert manifest in rendered
    assert "for attempt in 1 2 3 4 5" in script
    assert "git reset --hard origin/main" in script


@pytest.mark.parametrize("workflow,image,manifest", CASES)
def test_push_without_contention_pins_our_digest(tmp_path, workflow, image, manifest):
    script, env = _pin_step(workflow)
    origin = Origin(tmp_path, image, manifest)

    result = origin.run_step(script, env)

    assert result.returncode == 0, result.stdout + result.stderr
    assert origin.main_file(manifest) == _manifest(image, OUR_DIGEST)


@pytest.mark.parametrize("workflow,image,manifest", CASES)
def test_rejected_push_resets_onto_fresh_main_and_reapplies(tmp_path, workflow, image, manifest):
    script, env = _pin_step(workflow)
    origin = Origin(tmp_path, image, manifest)
    origin.land_on_main("other.txt", "another pin\n", "build: pin another image")

    result = origin.run_step(script, env)

    assert result.returncode == 0, result.stdout + result.stderr
    assert "push attempt 1 rejected" in result.stdout
    # Our digest landed on top of the other workflow's commit; nothing was lost.
    assert origin.main_file(manifest) == _manifest(image, OUR_DIGEST)
    assert origin.main_file("other.txt") == "another pin\n"
    assert origin.main_log()[1] == "build: pin another image"


@pytest.mark.parametrize("workflow,image,manifest", CASES)
def test_fresh_main_that_already_pins_our_digest_exits_clean(tmp_path, workflow, image, manifest):
    script, env = _pin_step(workflow)
    origin = Origin(tmp_path, image, manifest)
    origin.land_on_main(manifest, _manifest(image, OUR_DIGEST), "build: pinned by a concurrent run")
    before = origin.main_log()

    result = origin.run_step(script, env)

    assert result.returncode == 0, result.stdout + result.stderr
    assert "Fresh main already pins this digest; nothing to push." in result.stdout
    assert origin.main_log() == before


@pytest.mark.parametrize("workflow,image,manifest", CASES)
def test_persistent_rejection_fails_after_five_attempts(tmp_path, workflow, image, manifest):
    script, env = _pin_step(workflow)
    origin = Origin(tmp_path, image, manifest)
    origin.reject_every_push()

    result = origin.run_step(script, env)

    assert result.returncode == 1, result.stdout + result.stderr
    assert "push attempt 5 rejected" in result.stdout
    assert "after 5 attempts" in result.stdout
    assert origin.main_file(manifest) == _manifest(image, OLD_DIGEST)
