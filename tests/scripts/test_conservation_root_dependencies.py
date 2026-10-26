"""Run the actual CI matrix script with an offline changed-file fixture."""
import fnmatch
import json
import os
from pathlib import Path, PurePosixPath
import shlex
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
WORKFLOW = REPO / ".github/workflows/ci.yml"
ROOT_INPUTS = ("package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", ".npmrc")
ROOT_CONSUMERS = {"switchyard-ui", "dispatch"}


def matrix_script():
    tail = WORKFLOW.read_text().split("- name: Build service matrix from services.json", 1)[1]
    lines = tail.split("run: |\n", 1)[1].splitlines()
    result = []
    for line in lines:
        if line.strip() and not line.startswith("          "):
            break
        result.append(line[10:])
    return "\n".join(result)


def context_copy_sources(dockerfile):
    """Ignore stage-to-stage copies; accept shell/JSON COPY and continuations."""
    joined = dockerfile.replace("\\\n", " ")
    for line in joined.splitlines():
        parts = line.strip().split(None, 1)
        if not parts or parts[0].upper() != "COPY":
            continue
        args = parts[1].strip()
        options = []
        while args.startswith("--"):
            option, args = args.split(None, 1)
            options.append(option)
        if any(option.startswith("--from=") for option in options):
            continue
        fields = json.loads(args) if args.startswith("[") else shlex.split(args)
        if len(fields) < 2:
            raise AssertionError("COPY has no source/destination pair")
        yield from (str(PurePosixPath(source)) for source in fields[:-1])


class RootDependencyConservationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        fixture_bin = self.root / "bin"
        fixture_bin.mkdir()
        git = fixture_bin / "git"
        git.write_text("#!/bin/sh\n"
                       'if [ "$1" != diff ] || [ "$2" != --name-only ]; then exit 71; fi\n'
                       'printf "%s\\n" "$FIXTURE_CHANGED_FILES"\n')
        git.chmod(0o755)
        self.env = {**os.environ,
                    "PATH": str(fixture_bin) + os.pathsep + os.environ["PATH"]}
        self.services = json.loads((REPO / "services.json").read_text())["services"]
        self.counter = 0

    def matrix(self, changes, requested="", force=False):
        script = matrix_script()
        script = script.replace("${{ github.event.inputs.services || '' }}", requested)
        script = script.replace("${{ github.event_name }}", "workflow_dispatch" if requested or force else "push")
        script = script.replace("${{ github.event.inputs.force_build || 'false' }}", "true" if force else "false")
        self.assertNotIn("${{", script)
        self.counter += 1
        output = self.root / ("output-" + str(self.counter))
        result = subprocess.run(
            ["/bin/bash", "-euo", "pipefail", "-c", script], cwd=REPO,
            env={**self.env, "GITHUB_OUTPUT": str(output),
                 "FIXTURE_CHANGED_FILES": "\n".join(changes)},
            text=True, capture_output=True, timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        values = dict(line.split("=", 1) for line in output.read_text().splitlines())
        rows = json.loads(values["matrix"])["include"]
        self.assertEqual(values["has_changes"], "true" if rows else "false")
        # Selection adds no bypass: downstream receives the registry's exact
        # scan/sign/pin inputs, including scan=true for every selected image.
        for row in rows:
            service = next(service for service in self.services if service["name"] == row["name"])
            expected = {key: service[key] for key in (
                "name", "path", "dockerfile", "image", "kustomization", "kustomize_image", "scan")}
            expected.update(target=service.get("target", ""), build_args=service.get("build_args", {}))
            self.assertEqual(row, expected)
            self.assertTrue(row["scan"])
        return {row["name"] for row in rows}

    def test_each_root_install_input_selects_exactly_its_two_images(self):
        for path in ROOT_INPUTS:
            with self.subTest(path=path):
                self.assertEqual(self.matrix([path]), ROOT_CONSUMERS)

    def test_declared_consumers_match_actual_docker_copy_inputs(self):
        for path in ROOT_INPUTS:
            consumers = set()
            for service in self.services:
                sources = context_copy_sources((REPO / service["dockerfile"]).read_text())
                if any(source == "." or fnmatch.fnmatchcase(path, source) for source in sources):
                    consumers.add(service["name"])
            with self.subTest(path=path):
                self.assertEqual(consumers, ROOT_CONSUMERS)
                self.assertEqual(self.matrix([path]), consumers)

    def test_mixed_workspace_status_docs_packet_selects_only_four_images(self):
        self.assertEqual(self.matrix([
            "package.json", "pnpm-lock.yaml", "apps/status/pnpm-lock.yaml",
            "apps/docs-site/package-lock.json",
        ]), ROOT_CONSUMERS | {"enclii-status", "docs-site"})

    def test_standalone_install_graphs_remain_isolated(self):
        for path, name in (
            ("apps/status/pnpm-lock.yaml", "enclii-status"),
            ("apps/docs-site/package-lock.json", "docs-site"),
            ("apps/landing/package.json", "landing-page"),
        ):
            with self.subTest(path=path):
                self.assertEqual(self.matrix([path]), {name})

    def test_existing_source_and_shared_dependency_selection_is_preserved(self):
        self.assertEqual(self.matrix(["packages/shared-lib/src/index.ts"]), ROOT_CONSUMERS)
        self.assertEqual(self.matrix(["apps/switchyard-api/internal/fixture.go"]), {"switchyard-api"})
        self.assertEqual(self.matrix(["apps/roundhouse/internal/fixture.go"]), {"roundhouse", "roundhouse-worker"})

    def test_root_file_names_are_exact_and_workflow_only_changes_do_not_build(self):
        self.assertEqual(self.matrix([
            "package.json.backup", "pnpm-lock.yaml.extra", ".npmrc.local",
            "docs-package.json", "pnpm-workspace.yaml.old", "servicesXjson",
            "services.json.backup", ".github/workflows/ci.yml",
            "tests/scripts/test_conservation_root_dependencies.py",
        ]), set())

    def test_explicit_requested_services_override_root_and_path_detection(self):
        self.assertEqual(self.matrix(
            [*ROOT_INPUTS, "apps/status/pnpm-lock.yaml"], requested="docs-site"), {"docs-site"})
        self.assertEqual(self.matrix(
            ["package.json"], requested=" docs-site , enclii-status "), {"docs-site", "enclii-status"})

    def test_large_changed_lists_do_not_sigpipe_away_any_required_build(self):
        # Over 112KB: early-exit grep can close its pipe while printf is still
        # writing. Exercise the complete workflow body, with matches both first
        # and last, rather than a standalone grep that misses the lost matrix.
        unrelated = [f"unrelated/{i:04d}/{'x' * 92}.txt" for i in range(1050)]
        self.assertGreater(len("\n".join(unrelated)), 112_000)
        all_services = {service["name"] for service in self.services}
        for path, expected in (
            (".npmrc", ROOT_CONSUMERS),
            ("apps/switchyard-ui/src/fixture.ts", {"switchyard-ui"}),
            ("packages/shared-lib/src/fixture.ts", ROOT_CONSUMERS),
            ("services.json", all_services),
        ):
            for position in ("first", "last"):
                with self.subTest(path=path, position=position):
                    changes = [path, *unrelated] if position == "first" else [*unrelated, path]
                    self.assertEqual(self.matrix(changes), expected)

        self.assertEqual(self.matrix(
            [".npmrc", *unrelated], requested="docs-site"), {"docs-site"})

    def test_force_and_registry_changes_keep_their_existing_all_service_precedence(self):
        all_services = {service["name"] for service in self.services}
        self.assertEqual(self.matrix(["package.json"], requested="docs-site", force=True), all_services)
        self.assertEqual(self.matrix(["services.json"], requested="docs-site"), all_services)


if __name__ == "__main__":
    unittest.main()
