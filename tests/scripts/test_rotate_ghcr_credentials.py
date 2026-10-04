"""Execute the legacy rotation script against stub commands, never a cluster."""
from __future__ import annotations

import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/rotate-ghcr-credentials.sh"
NAMESPACE_COUNT = 10

KUBECTL = r'''#!/bin/bash
set -eu
case "$1" in
  get)
    printf 'get %s\n' "$3" >> "$CALLS"
    exit 0 ;;
  create)
    ns=''
    for arg in "$@"; do case "$arg" in --namespace=*) ns="${arg#--namespace=}" ;; esac; done
    printf 'create %s\n' "$ns" >> "$CALLS"
    printf 'apiVersion: v1\ndata:\n  .dockerconfigjson: PRIVATE_SERIALIZED_SENTINEL\nmetadata:\n  namespace: %s\n' "$ns" ;;
  apply)
    payload="$(cat)"
    printf 'apply\n' >> "$CALLS"
    if [[ -n "${FAIL_NAMESPACE:-}" && "$payload" == *"namespace: $FAIL_NAMESPACE" ]]; then exit 1; fi
    printf 'secret updated\n' ;;
  *) exit 90 ;;
esac
'''
CRANE = r'''#!/bin/bash
printf 'crane\n' >> "$CALLS"
cat >/dev/null
exit 0
'''


class RotationScriptTests(unittest.TestCase):
    def run_script(self, *arguments, token="fixture-value", fail_namespace=""):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            for name, content in (("kubectl", KUBECTL), ("crane", CRANE)):
                command = folder / name
                command.write_text(content)
                command.chmod(0o755)
            calls = folder / "calls"
            env = dict(os.environ, PATH=f"{folder}:{os.environ['PATH']}",
                       CALLS=str(calls), GHCR_PAT=token, FAIL_NAMESPACE=fail_namespace)
            result = subprocess.run(["bash", str(SCRIPT), *arguments], env=env,
                                    capture_output=True, text=True, timeout=10)
            events = calls.read_text().splitlines() if calls.exists() else []
            return result, events

    def test_dry_run_is_metadata_only_without_credentials_or_login(self):
        result, events = self.run_script("--dry-run", token="")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(events), NAMESPACE_COUNT)
        self.assertTrue(all(event.startswith("get ") for event in events))
        self.assertNotIn("PRIVATE_SERIALIZED_SENTINEL", result.stdout + result.stderr)
        self.assertIn("Secret janua/ghcr-credentials", result.stdout)
        self.assertIn("No changes were applied", result.stdout)

    def test_success_continues_past_first_increment(self):
        result, events = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(events.count("apply"), NAMESPACE_COUNT)
        self.assertIn("10 updated, 0 failed", result.stdout)
        self.assertNotIn("PRIVATE_SERIALIZED_SENTINEL", result.stdout + result.stderr)
        self.assertNotIn("fixture-value", result.stdout + result.stderr)

    def test_failure_is_counted_and_remaining_namespaces_are_attempted(self):
        result, events = self.run_script(fail_namespace="enclii")
        self.assertEqual(result.returncode, 1)
        self.assertEqual(events.count("apply"), NAMESPACE_COUNT)
        self.assertIn("9 updated, 1 failed", result.stdout)
        self.assertNotIn("PRIVATE_SERIALIZED_SENTINEL", result.stdout + result.stderr)

    def test_unknown_argument_cannot_accidentally_apply(self):
        result, events = self.run_script("--dryrun")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(events, [])

    def test_checker_does_not_claim_validity_from_existence(self):
        text = (ROOT / "infra/k8s/production/ghcr-credential-check.yaml").read_text()
        self.assertNotIn("have valid $SECRET_NAME", text)
        self.assertIn("credential validity and package access were not checked", text)


if __name__ == "__main__":
    unittest.main()
