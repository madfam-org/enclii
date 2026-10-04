"""Exercise recovery workflow scripts locally; kubectl is a fixture-only stub."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
WORKFLOW = REPO / '.github/workflows/gitops-argo-sync.yml'


def run_block(path, marker):
    tail = path.read_text().split(marker, 1)[1]
    lines = tail.split('run: |\n', 1)[1].splitlines()
    result = []
    for line in lines:
        if line.strip() and not line.startswith('          '):
            break
        result.append(line[10:])
    return '\n'.join(result)


class GitOpsSyncTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.server = self.root / 'application.json'
        self.app = {'apiVersion': 'argoproj.io/v1alpha1', 'kind': 'Application',
                    'metadata': {'name': 'core-services', 'namespace': 'argocd', 'resourceVersion': '10', 'annotations': {'keep': 'yes'}},
                    'spec': {'source': {'targetRevision': 'main'}},
                    'status': {'health': {'status': 'Degraded'}, 'sync': {'status': 'Synced'}}}
        self.save()
        self.env = {**os.environ, 'PATH': str(self.bin) + os.pathsep + os.environ['PATH'],
                    'APP': 'core-services', 'REVISION': 'a' * 40, 'REASON': 'reviewed fixture recovery',
                    'REQUEST_ID': 'fixture-request', 'SERVER': str(self.server),
                    'CALLS': str(self.root / 'calls'), 'TMPDIR': str(self.root),
                    'DEPLOY_ACK': 'production-kustomization'}
        self.stub('sleep', '#!/bin/sh\nexit 0\n')
        self.stub('kubectl', '#!' + sys.executable + '\n' + '''
import json, os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ['CALLS'], 'a') as f: f.write(json.dumps(args) + '\\n')
p = Path(os.environ['SERVER'])
a = json.loads(p.read_text())
if args[0] == 'get':
    print(json.dumps(a))
elif args[0] == 'replace':
    incoming = json.loads(Path(args[2]).read_text())
    if os.environ.get('RACE') == '1':
        a['metadata']['resourceVersion'] = '11'
        a['operation'] = {'initiatedBy': {'automated': True}, 'sync': {'revision': 'old', 'resources': [{'kind': 'ConfigMap', 'name': 'fixture'}]}}
        p.write_text(json.dumps(a))
    if incoming['metadata']['resourceVersion'] != a['metadata']['resourceVersion']:
        sys.exit('Conflict: resource version changed')
    p.write_text(json.dumps(incoming))
elif args[:2] == ['rollout', 'status']:
    sys.exit(int(os.environ.get('ROLLOUT_STATUS', '0')))
else:
    sys.exit('unexpected kubectl command')
''')

    def stub(self, name, script):
        p = self.bin / name
        p.write_text(script)
        p.chmod(0o755)

    def save(self):
        self.server.write_text(json.dumps(self.app))

    def run_step(self, name, **env):
        script = run_block(WORKFLOW, '- name: ' + name)
        if name != 'Validate inputs':
            # Execute the exact mutation/wait body after credential setup;
            # never create or read a real kubeconfig in a local test.
            script = 'set -euo pipefail\numask ' + script.split('umask ', 1)[1]
        return subprocess.run(['/bin/bash', '-c', script], env={**self.env, **env},
                              text=True, capture_output=True, timeout=20)

    def calls(self):
        p = self.root / 'calls'
        return [json.loads(line) for line in p.read_text().splitlines()] if p.exists() else []

    def finished(self, phase='Succeeded', ours=True, revision=None):
        self.app['status']['operationState'] = {
            'phase': phase, 'finishedAt': '2026-01-01T00:00:00Z',
            'operation': {'info': [{'name': 'enclii-request-id', 'value': 'fixture-request' if ours else 'other'}]},
            'syncResult': {'revision': revision or self.env['REVISION']}}
        self.save()

    def test_exact_revision_ack_and_reason_required(self):
        self.assertEqual(self.run_step('Validate inputs').returncode, 0)
        for env in ({'REVISION': 'main'}, {'DEPLOY_ACK': 'no'}, {'REASON': 'short'}):
            self.assertNotEqual(self.run_step('Validate inputs', **env).returncode, 0)

    def test_full_operation_cas_and_safe_audit_encoding(self):
        result = self.run_step('Sync Argo Application', REASON='fixture "quote"\nand newline')
        self.assertEqual(result.returncode, 0, result.stderr)
        app = json.loads(self.server.read_text())
        self.assertEqual(app['metadata']['resourceVersion'], '10')
        self.assertEqual(app['metadata']['annotations']['keep'], 'yes')
        self.assertEqual(app['spec'], self.app['spec'])
        self.assertEqual(app['status'], self.app['status'])
        self.assertEqual(app['operation']['sync'], {
            'prune': False, 'revision': 'a' * 40, 'syncStrategy': {'apply': {'force': False}},
            'resources': [
                {'group': 'apps', 'kind': 'Deployment', 'name': 'switchyard-api', 'namespace': 'enclii'},
                {'group': 'apps', 'kind': 'Deployment', 'name': 'docs-site', 'namespace': 'enclii'},
            ],
        })
        self.assertEqual(app['operation']['initiatedBy'], {'username': 'github-actions-enclii-gitops-sync'})
        self.assertEqual([c[0] for c in self.calls()], ['get', 'replace'])

    def test_other_application_keeps_its_existing_unrestricted_sync_scope(self):
        result = self.run_step('Sync Argo Application', APP='fixture-app', REVISION='')
        self.assertEqual(result.returncode, 0, result.stderr)
        sync = json.loads(self.server.read_text())['operation']['sync']
        self.assertNotIn('resources', sync)
        self.assertNotIn('revision', sync)
        self.assertFalse(sync['prune'])

    def test_concurrent_operation_is_preserved_without_retry(self):
        self.assertNotEqual(self.run_step('Sync Argo Application', RACE='1').returncode, 0)
        app = json.loads(self.server.read_text())
        self.assertEqual(app['metadata']['resourceVersion'], '11')
        self.assertTrue(app['operation']['initiatedBy']['automated'])
        self.assertEqual(app['operation']['sync']['revision'], 'old')
        self.assertNotIn('enclii.dev/last-ops-request-id', app['metadata']['annotations'])
        self.assertEqual([c[0] for c in self.calls()], ['get', 'replace'])

    def test_active_operation_and_missing_version_refuse_mutation(self):
        for version, operation in [('10', {'sync': {'revision': 'old'}}), ('', None)]:
            self.app['metadata']['resourceVersion'] = version
            self.app['operation'] = operation
            self.save()
            self.assertNotEqual(self.run_step('Sync Argo Application').returncode, 0)
        self.assertTrue(all(c[0] == 'get' for c in self.calls()))

    def test_our_completed_operation_requires_target_rollout(self):
        self.finished()
        result = self.run_step('Wait for sync completion')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(['rollout', 'status', 'deployment/switchyard-api', '-n', 'enclii', '--timeout=300s'], self.calls())
        self.assertNotEqual(self.run_step('Wait for sync completion', ROLLOUT_STATUS='1').returncode, 0)

    def test_old_succeeded_operation_and_synced_app_do_not_pass(self):
        self.finished(ours=False)
        result = self.run_step('Wait for sync completion')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('Timed out', result.stdout)
        self.assertTrue(all(c[0] == 'get' for c in self.calls()))

    def test_failed_and_wrong_revision_operations_fail(self):
        for phase, revision in [('Failed', None), ('Error', None), ('Succeeded', 'b' * 40)]:
            self.finished(phase=phase, revision=revision)
            self.assertNotEqual(self.run_step('Wait for sync completion').returncode, 0)
        self.assertTrue(all(c[0] == 'get' for c in self.calls()))

    def test_workflow_only_main_change_detects_no_image_builds(self):
        script = run_block(REPO / '.github/workflows/ci.yml', '- name: Build service matrix from services.json')
        script = re.sub(r'\$\{\{ github.event.inputs.services \|\| \'\' \}\}', '', script)
        script = script.replace('${{ github.event_name }}', 'push').replace("${{ github.event.inputs.force_build || 'false' }}", 'false')
        self.stub('git', '#!/bin/sh\nprintf "%s\\n" .github/workflows/gitops-argo-sync.yml tests/scripts/test_gitops_argo_sync.py\n')
        output = self.root / 'output'
        result = subprocess.run(['/bin/bash', '-c', script], cwd=REPO,
                                env={**self.env, 'GITHUB_OUTPUT': str(output)}, text=True, capture_output=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('has_changes=false', output.read_text())
        self.assertIn('matrix={"include":[]}', output.read_text())


if __name__ == '__main__':
    unittest.main()
