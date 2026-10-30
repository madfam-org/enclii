#!/usr/bin/env python3
"""Run the real hook against isolated staged fixtures and controlled validators."""

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


HOOK = Path(__file__).resolve().parents[1] / "hooks" / "pre-commit"


class PreCommitValidationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        # Isolate PATH so a developer's installed golangci-lint cannot affect skips.
        for name in ("git", "grep", "head", "cut", "wc", "tr", "sed", "bash"):
            self.bin.joinpath(name).symlink_to(shutil.which(name))
        self.env = {**os.environ, "PATH": str(self.bin), "CALLS": str(self.root / "calls")}
        self.git("init", "-q")
        self.stub("gofmt", "exit 0")
        self.stub("npm", '''
case "$2" in
  typecheck) status=${TYPECHECK_STATUS:-0} ;;
  lint) status=${LINT_STATUS:-0} ;;
  *) exit 99 ;;
esac
printf '%s\\n' "$2" >> "$CALLS"
i=0
while [ "$i" -lt "${OUTPUT_LINES:-1}" ]; do
  printf 'diagnostic line %s: validation result\\n' "$i"
  i=$((i + 1))
done
exit "$status"
''')
        self.stub("golangci-lint", '''
printf 'go-lint\\n' >> "$CALLS"
i=0
while [ "$i" -lt "${OUTPUT_LINES:-1}" ]; do
  printf 'diagnostic line %s: validation result\\n' "$i"
  i=$((i + 1))
done
exit "${GO_STATUS:-0}"
''')

    def stub(self, name, body):
        script = self.bin / name
        script.write_text("#!/bin/bash\n" + body + "\n")
        script.chmod(0o755)

    def git(self, *args):
        subprocess.run([str(self.bin / "git"), *args], cwd=self.root,
                       env=self.env, check=True, capture_output=True)

    def stage(self, path, content):
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)
        self.git("add", path)

    def stage_ui(self):
        self.stage("apps/switchyard-ui/package.json", "{}\n")
        self.stage("apps/switchyard-ui/fixture.ts", "export const value = 1;\n")

    def stage_go(self):
        self.stage("apps/switchyard-api/go.mod", "module example.invalid/fixture\n")
        self.stage("apps/switchyard-api/fixture.go", "package fixture\n")

    def run_hook(self, **values):
        result = subprocess.run(["/bin/bash", str(HOOK)], cwd=self.root,
                                env={**self.env, **values}, text=True,
                                capture_output=True, timeout=20)
        self.output = result.stdout + result.stderr
        call_file = self.root / "calls"
        self.calls = call_file.read_text().splitlines() if call_file.exists() else []
        return result.returncode

    def test_typecheck_nonzero_without_error_keyword_blocks(self):
        self.stage_ui()
        self.assertNotEqual(self.run_hook(TYPECHECK_STATUS="7"), 0)
        self.assertEqual(self.calls, ["typecheck"])
        self.assertIn("TypeScript check failed", self.output)
        self.assertNotIn("typecheck passed", self.output)

    def test_lint_nonzero_without_error_keyword_blocks_once(self):
        self.stage_ui()
        self.assertNotEqual(self.run_hook(LINT_STATUS="8"), 0)
        self.assertEqual(self.calls, ["typecheck", "lint"])
        self.assertIn("ESLint check failed", self.output)
        self.assertNotIn("switchyard-ui lint passed", self.output)

    def test_success_runs_each_validator_once(self):
        self.stage_ui()
        self.stage_go()
        self.assertEqual(self.run_hook(), 0, self.output)
        self.assertEqual(self.calls, ["typecheck", "lint", "go-lint"])
        self.assertIn("All pre-commit validations passed", self.output)

    def test_verbose_success_does_not_fail_from_sigpipe(self):
        self.stage_ui()
        self.stage_go()
        self.assertEqual(self.run_hook(OUTPUT_LINES="10000"), 0, self.output)
        self.assertEqual(self.calls, ["typecheck", "lint", "go-lint"])
        self.assertEqual(self.output.count("diagnostic line"), 60)

    def test_verbose_failure_remains_failure(self):
        self.stage_ui()
        self.assertNotEqual(self.run_hook(OUTPUT_LINES="10000", LINT_STATUS="2"), 0)
        self.assertEqual(self.calls, ["typecheck", "lint"])
        self.assertEqual(self.output.count("diagnostic line"), 40)

    def test_go_nonzero_blocks(self):
        self.stage_go()
        self.assertNotEqual(self.run_hook(GO_STATUS="3"), 0)
        self.assertEqual(self.calls, ["go-lint"])
        self.assertNotIn("Go lint passed", self.output)

    def test_missing_go_linter_reports_skip_without_pass_claim(self):
        self.stage_go()
        (self.bin / "golangci-lint").unlink()
        self.assertEqual(self.run_hook(), 0, self.output)
        self.assertEqual(self.calls, [])
        self.assertIn("skipping Go lint", self.output)
        self.assertIn("Go lint was skipped", self.output)
        self.assertNotIn("Go lint passed", self.output)
        self.assertNotIn("All pre-commit validations passed", self.output)


if __name__ == "__main__":
    unittest.main()
