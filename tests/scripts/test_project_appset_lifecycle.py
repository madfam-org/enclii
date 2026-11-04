"""Retiring a legacy project must not cascade, and must not touch the others.

infra/argocd/apps/project-appset.yaml generates one `<name>-services`
Application per infra/argocd/projects/<name>/config.json. Deleting a
config.json makes the ApplicationSet delete that Application, and the
Application carries `resources-finalizer.argocd.argoproj.io`, so Argo would
delete every workload it deployed. `"lifecycle": "retired"` in the config.json
instead renders that one Application with no automated sync and no resources
finalizer, through the ApplicationSet's `templatePatch`.

These checks are static. The behaviour was proven once against Argo CD
v3.2.5's own ApplicationSet code (`GenerateApplications` with the real Git
files generator) over every config.json: the retired project lost
`syncPolicy.automated` and its finalizer, and every other Application rendered
byte-identical. They keep the shape that proof depends on:

- the whole patch sits inside one `if` on `.lifecycle`, so it renders empty
  for every other project;
- the retired branch nulls exactly `metadata.finalizers` and
  `spec.syncPolicy.automated` (strategic merge: null deletes the key);
- the template still sets automated sync for everyone else, and no config.json
  carries a lifecycle value the patch does not know.

Procedure: infra/argocd/README.md, "Retiring a project".
"""

import json
import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
APPSET = ROOT / "infra/argocd/apps/project-appset.yaml"
PROJECTS = ROOT / "infra/argocd/projects"
KNOWN_LIFECYCLES = {"active", "retired"}
RETIRED = {"sim4d"}

IF_RETIRED = re.compile(
    r'^\{\{-\s*if\s+eq\s+\(default\s+"active"\s+\.lifecycle\)\s+"retired"\s*\}\}$'
)
END = re.compile(r"^\{\{-\s*end\s*\}\}$")


def appset():
    docs = [d for d in yaml.safe_load_all(APPSET.read_text()) if d]
    return next(d for d in docs if d["kind"] == "ApplicationSet")


def patch_lines():
    lines = appset()["spec"]["templatePatch"].strip().splitlines()
    assert lines, "templatePatch is empty"
    return lines


def configs():
    return {
        path.parent.name: json.loads(path.read_text())
        for path in sorted(PROJECTS.glob("*/config.json"))
    }


def test_patch_is_one_block_guarded_by_the_retired_lifecycle():
    lines = patch_lines()
    assert IF_RETIRED.match(lines[0].strip()), lines[0]
    assert END.match(lines[-1].strip()), lines[-1]
    inner = "\n".join(lines[1:-1])
    assert "{{" not in inner, "only the guard may be templated"


def test_retired_branch_drops_automated_sync_and_the_finalizer_only():
    body = yaml.safe_load("\n".join(patch_lines()[1:-1]))
    assert body == {
        "metadata": {"finalizers": None},
        "spec": {"syncPolicy": {"automated": None}},
    }


def test_template_still_auto_syncs_everything_else():
    spec = appset()["spec"]
    automated = spec["template"]["spec"]["syncPolicy"]["automated"]
    assert automated == {"prune": True, "selfHeal": True}
    # The finalizer comes from the controller (no preserveResourcesOnDeletion),
    # which is why the retired branch has to null it.
    assert "resources-finalizer.argocd.argoproj.io" in spec["template"]["metadata"][
        "finalizers"
    ]
    assert not (spec.get("syncPolicy") or {}).get("preserveResourcesOnDeletion")
    # goTemplate with missingkey=zero is what lets `.lifecycle` be absent.
    assert spec["goTemplate"] is True
    assert spec["goTemplateOptions"] == ["missingkey=zero"]


def test_every_lifecycle_value_is_known():
    for name, config in configs().items():
        lifecycle = config.get("lifecycle", "active")
        assert lifecycle in KNOWN_LIFECYCLES, (name, lifecycle)


def test_retired_projects_are_the_reviewed_set():
    retired = {
        name
        for name, config in configs().items()
        if config.get("lifecycle") == "retired"
    }
    # A new entry is a decision: add it here and to the README table together.
    assert retired == RETIRED
    readme = (ROOT / "infra/argocd/README.md").read_text()
    for name in retired:
        assert f"| {name} |" in readme, name
