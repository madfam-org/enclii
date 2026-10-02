""":stable is promoted only after smoke and signing (#664).

arc-runner-image.yml pushes every build by its sha tag and moves `:stable`
in a final step, after both smoke checks and the cosign signature. These
tests pin that order and those conditions, so a later edit cannot move the
promotion ahead of verification or let a pull request reach it.
"""

from __future__ import annotations

from pathlib import Path

import yaml

WORKFLOW = Path(__file__).resolve().parents[2] / ".github" / "workflows" / "arc-runner-image.yml"


def _steps() -> list[dict]:
    doc = yaml.safe_load(WORKFLOW.read_text())
    return doc["jobs"]["build"]["steps"]


def _index(steps: list[dict], name: str) -> int:
    for i, step in enumerate(steps):
        if step.get("name") == name:
            return i
    raise AssertionError(f"no step named {name!r} in {WORKFLOW.name}")


def test_build_step_tags_only_the_sha():
    build = next(s for s in _steps() if s.get("id") == "build")
    tags = str(build["with"]["tags"])
    assert "steps.meta.outputs.tag_sha" in tags
    assert ":stable" not in tags


def test_promotion_runs_after_both_smoke_checks_and_the_signature():
    steps = _steps()
    promote = _index(steps, "Promote :stable")
    for prerequisite in (
        "Smoke-check the render environment",
        "Verify runner agent, GitHub CLI and Chromium",
        "Sign image (keyless)",
    ):
        assert _index(steps, prerequisite) < promote, f"{prerequisite} must run before Promote :stable"
    assert steps[promote].get("id") == "promote"


def test_promotion_only_when_stable_publishing_and_pushing():
    promote = next(s for s in _steps() if s.get("id") == "promote")
    condition = str(promote.get("if", ""))
    assert "steps.meta.outputs.publish_stable == 'true'" in condition
    assert "steps.meta.outputs.push_image == 'true'" in condition


def test_pull_requests_never_push_or_publish_stable():
    meta = next(s for s in _steps() if s.get("id") == "meta")
    script = meta["run"]
    assert 'if [ "${GITHUB_EVENT_NAME}" = "pull_request" ]; then\n  echo "push_image=false"' in script
    # publish_stable=true is written in exactly two branches, both gated on main.
    assert script.count('echo "publish_stable=true"') == 2
    assert script.count('"${GITHUB_REF}" = "refs/heads/main"') == 2
    assert 'echo "publish_stable=false"' in script


def test_promotion_refuses_without_a_usable_digest():
    promote = next(s for s in _steps() if s.get("id") == "promote")
    assert promote["env"]["DIGEST"] == "${{ steps.build.outputs.digest }}"
    assert "^sha256:[0-9a-f]{64}$" in promote["run"]
    assert "refusing to move :stable" in promote["run"]
