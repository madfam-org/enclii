#!/usr/bin/env python3
"""Check the real, checksum-bound Trivy IMAGE parser without scanning an image.

The publisher's actual argv is evaluated as closed AST data, never executed as
Python. Both parser calls append --help. No inherited auth, HOME, proxy or Trivy
configuration reaches the child. CI supplies the fixed official native archive.
"""
from __future__ import annotations

import argparse
import ast
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys
import tarfile
import tempfile

import yaml

VERSION = "0.76.1"
OFFICIAL_ARCHIVES = {
    ("Linux", "x86_64"): "c7cbe3137fb8a48a17574bede3dd6beb16742d1e9772929940fc90082c39d9c0",  # pragma: allowlist secret - official public Linux archive SHA256
    ("Darwin", "x86_64"): "7aa762eacd582052d053b62dcf972a1ab058abe39a57ccae234ed8894945b6b7",  # pragma: allowlist secret - official public Mac archive SHA256
}
RELATIONSHIPS = "unknown,root,workspace,direct,indirect"
MAX_HELP = 128 * 1024


class Refused(Exception):
    pass


def require(condition, phase):
    if not condition:
        raise Refused(phase)


def sha_file(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def archive_binary_binding(archive, binary):
    expected = OFFICIAL_ARCHIVES.get((platform.system(), platform.machine()))
    require(expected is not None, "unsupported-native-platform")
    require(sha_file(archive) == expected, "official-archive-checksum")
    with tarfile.open(archive, "r:gz") as bundle:
        member = bundle.getmember("trivy")
        require(member.isfile(), "official-archive-member")
        with bundle.extractfile(member) as stream:
            require(hashlib.file_digest(stream, "sha256").hexdigest() == sha_file(binary),
                    "native-binary-matches-official-archive")
    return expected


def source_command(workflow, private):
    document = yaml.safe_load(workflow.read_text())
    steps = document["jobs"]["pin"]["steps"]
    matches = [s["run"] for s in steps if s.get("name") == "Prepare fixed pre-pin image scanner"]
    require(len(matches) == 1, "single-fixed-scanner-step")
    run = matches[0]
    driver = run.split("<<'PY_SCAN'\n", 1)[1].split("\nPY_SCAN", 1)[0]
    tree = ast.parse(driver)
    relation_values = [n.value for n in tree.body if isinstance(n, ast.Assign)
                       and any(isinstance(t, ast.Name) and t.id == "RELATIONSHIPS" for t in n.targets)]
    require(len(relation_values) == 1, "single-static-relationship-list")
    relationships = ast.literal_eval(relation_values[0])
    require(isinstance(relationships, tuple) and relationships == tuple(RELATIONSHIPS.split(",")),
            "fixed-complete-relationship-list")
    require(sum(isinstance(n, ast.Name) and n.id == "RELATIONSHIPS" and isinstance(n.ctx, ast.Store)
                for n in ast.walk(tree)) == 1, "no-relationship-list-reassignment")
    functions = [n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "scan"]
    require(len(functions) == 1, "single-scan-function")
    function = functions[0]
    assignments = [n for n in ast.walk(function) if isinstance(n, ast.Assign)
                   and any(isinstance(t, ast.Name) and t.id == "command" for t in n.targets)]
    require(len(assignments) == 1 and len(assignments[0].targets) == 1
            and isinstance(assignments[0].value, ast.List), "single-data-only-image-command")
    parents = {child: node for node in ast.walk(function) for child in ast.iter_child_nodes(node)}
    uses = [n for n in ast.walk(function) if isinstance(n, ast.Name) and n.id == "command"]
    loads = 0
    for node in uses:
        parent = parents[node]
        if isinstance(node.ctx, ast.Store):
            require(parent is assignments[0], "no-command-reassignment")
        else:
            loads += 1
            require(isinstance(parent, ast.Call) and isinstance(parent.func, ast.Name)
                    and parent.func.id == "capture" and parent.args[0] is node,
                    "command-used-only-by-fixed-capture")
    require(loads == 1, "single-command-capture")
    reference = "ghcr.io/madfam-org/fixture/api@sha256:" + "0" * 64

    def evaluate(node):
        if isinstance(node, ast.Constant) and isinstance(node.value, str):
            return node.value
        if isinstance(node, ast.Name) and node.id in ["ROOT", "ref"]:
            return private if node.id == "ROOT" else reference
        if isinstance(node, ast.Name) and node.id == "RELATIONSHIPS":
            return relationships
        if (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
                and isinstance(node.func.value, ast.Constant) and node.func.value.value == ","
                and node.func.attr == "join" and len(node.args) == 1 and not node.keywords):
            values = evaluate(node.args[0])
            require(values == relationships, "only-static-relationship-join")
            return ",".join(values)
        if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Div):
            left, right = evaluate(node.left), evaluate(node.right)
            require(isinstance(left, Path) and isinstance(right, str)
                    and right not in ["", ".", ".."] and "/" not in right and "\\" not in right,
                    "closed-private-path-expression")
            return left / right
        if (isinstance(node, ast.Call) and isinstance(node.func, ast.Name)
                and node.func.id == "str" and len(node.args) == 1 and not node.keywords):
            return str(evaluate(node.args[0]))
        raise Refused("non-data-command-expression")

    command = [evaluate(n) for n in assignments[0].value.elts]
    require(all(isinstance(value, str) for value in command)
            and command[:2] == [str(private / "trivy"), "image"]
            and command[-1] == reference, "fixed-image-command-shape")
    require(command[command.index("--pkg-relationships") + 1] == RELATIONSHIPS,
            "all-installed-package-relationships-explicit")
    return command, hashlib.sha256(driver.encode()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--trivy", type=Path, required=True)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--workflow", type=Path,
                        default=Path(__file__).resolve().parents[2] / ".github/workflows/build-publish.yml")
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    result = {"schema": "verified-trivy-image-parser-v1", "version": VERSION,
              "image_or_database_scan_performed": False, "auth_supplied": False}
    try:
        binary = args.trivy.resolve(strict=True)
        result["official_archive_sha256"] = archive_binary_binding(args.archive, binary)
        result["binary_sha256"] = sha_file(binary)
        with tempfile.TemporaryDirectory(prefix="enclii-trivy-image-parser-") as directory:
            private = Path(directory)
            for name in ["config", "cache", "docker"]:
                (private / name).mkdir(mode=0o700)
            (private / "config.yaml").write_text("{}\n")
            (private / "config.yaml").chmod(0o600)
            (private / "empty.ignore").touch(mode=0o600)
            command, driver_sha = source_command(args.workflow, private)
            command[0] = str(binary)
            clean = {"PATH": os.defpath, "XDG_CONFIG_HOME": str(private / "config"),
                     "XDG_CACHE_HOME": str(private / "cache"), "DOCKER_CONFIG": str(private / "docker")}

            def run(argv):
                outcome = subprocess.run(argv, cwd=private, env=clean, capture_output=True,
                                         text=True, timeout=30)
                require(len(outcome.stdout.encode()) <= MAX_HELP
                        and len(outcome.stderr.encode()) <= MAX_HELP, "bounded-cli-help")
                return outcome

            version = run([str(binary), "--version"])
            require(version.returncode == 0 and re.search(r"^Version: 0\.76\.1$", version.stdout, re.M),
                    "official-native-version")
            negative = run([*command[:-1], "--include-dev-deps=true", command[-1], "--help"])
            require(negative.returncode != 0 and "unknown flag" in negative.stderr
                    and "include-dev-deps" in negative.stderr, "unsupported-image-flag-negative")
            accepted = run([*command, "--help"])
            require(accepted.returncode == 0, "all-actual-image-arguments-parse")
            require("--pkg-relationships" in accepted.stdout
                    and "(default [" + RELATIONSHIPS + "])" in accepted.stdout,
                    "real-image-relationship-default")
            require(not any((private / "cache").iterdir())
                    and not any((private / "docker").iterdir()), "no-image-database-or-auth-materialized")
            flags = [value.split("=", 1)[0] for value in command[2:] if value.startswith("--")]
            result.update(result="PASS", workflow_sha256=sha_file(args.workflow),
                          inline_driver_sha256=driver_sha, parsed_image_flags=flags,
                          installed_package_relationships=RELATIONSHIPS.split(","),
                          unsupported_image_flag_refused=True, no_cache_or_auth_materialized=True,
                          raw_help_stdout_sha256=hashlib.sha256(accepted.stdout.encode()).hexdigest())
    except Exception as error:
        result.update(result="FAIL", phase=str(error) if isinstance(error, Refused) else "invalid-source-or-cli")
    if args.report:
        args.report.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps({k: result[k] for k in ["schema", "version", "result", "image_or_database_scan_performed"]}))
    return 0 if result["result"] == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
