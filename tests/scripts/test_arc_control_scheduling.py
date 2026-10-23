"""ARC capacity repairs must preserve active jobs and execution isolation."""

from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]


def documents(path):
    return [d for d in yaml.safe_load_all((ROOT / path).read_text()) if d]


def test_controller_values_and_render_preserve_jobs_and_builder_headroom():
    values = documents("infra/helm/arc/values-controller.yaml")[0]
    deployment = next(
        d
        for d in documents("infra/k8s/production/arc/controller/rendered.yaml")
        if d["kind"] == "Deployment"
    )
    template = deployment["spec"]["template"]["spec"]
    assert values["flags"]["updateStrategy"] == "eventual"
    args = template["containers"][0]["args"]
    assert "--update-strategy=eventual" in args
    assert "--update-strategy=immediate" not in args
    assert "--enable-leader-election" in args
    assert deployment["spec"]["replicas"] == values["replicaCount"] == 2
    assert values["podDisruptionBudget"]["minAvailable"] == 1
    expected = {
        "requiredDuringSchedulingIgnoredDuringExecution": {
            "nodeSelectorTerms": [
                {
                    "matchExpressions": [
                        {"key": "role", "operator": "NotIn", "values": ["builder"]}
                    ]
                }
            ]
        }
    }
    assert (
        template["affinity"]["nodeAffinity"]
        == values["affinity"]["nodeAffinity"]
        == expected
    )
    assert (
        template["affinity"]["podAntiAffinity"] == values["affinity"]["podAntiAffinity"]
    )


def test_recovery_validation_keeps_checks_on_existing_deploy_pool():
    workflow = documents(".github/workflows/arc-self-deploy.yml")[0]
    validate = workflow["jobs"]["validate"]
    assert validate["runs-on"] == "madfam-runners-deploy"
    steps = {step["name"] for step in validate["steps"]}
    assert {
        "Validate Helm values",
        "Validate Kubernetes manifests",
        "Lint scripts",
    } <= steps
    assert (
        "github.event_name == 'workflow_dispatch'" in workflow["jobs"]["deploy"]["if"]
    )
    scanner = documents(".github/workflows/pre-commit-secret-hooks.yml")[0]["jobs"][
        "secret-hooks"
    ]
    assert scanner["runs-on"] == "ubuntu-24.04"
    script = "\n".join(step.get("run", "") for step in scanner["steps"])
    assert "pre-commit run detect-secrets --all-files" in script
    assert "pre-commit run gitleaks --all-files" in script
    assert "pre-commit run detect-secrets --files" in script
    assert 'git add "$probe"' in script
    assert "if pre-commit run gitleaks > gl.out" in script
    for colour in ("blue", "deploy"):
        scale_set = next(
            d
            for d in documents(
                f"infra/k8s/production/arc/runner-{colour}/rendered.yaml"
            )
            if d["kind"] == "AutoscalingRunnerSet"
        )
        assert scale_set["spec"]["template"]["spec"]["nodeSelector"] == {
            "role": "builder"
        }
