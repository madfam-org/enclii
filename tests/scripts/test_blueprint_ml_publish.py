"""Execute release gate scripts against synthetic Git/GitHub responses."""
import json
import os
from pathlib import Path
import subprocess

import pytest
import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = yaml.safe_load((ROOT / '.github/workflows/blueprint-api-publish.yml').read_text())
SHA = 'a' * 40


def script(name):
    return next(step['run'] for job in WORKFLOW['jobs'].values()
                for step in job['steps'] if step.get('name') == name)


def execute(body, tmp_path, **environment):
    env = {**os.environ, 'SERVICE': 'ml', 'PATH': f"{tmp_path}:{os.environ['PATH']}", **environment}
    return subprocess.run(['bash', '-c', body], cwd=tmp_path, env=env,
                          capture_output=True, text=True, timeout=5)


def executable(tmp_path, name, body):
    p = tmp_path / name
    p.write_text('#!/bin/bash\n' + body)
    p.chmod(0o755)


@pytest.mark.parametrize('service', ['api', 'ml', 'compliance'])
@pytest.mark.parametrize('sha,ack,reason,valid', [
    (SHA, 'production-kustomization', 'Synthetic release validation', True),
    ('main', 'production-kustomization', 'Synthetic release validation', False),
    (SHA, 'no', 'Synthetic release validation', False),
    (SHA, 'production-kustomization', 'short', False),
])
def test_explicit_inputs(service, sha, ack, reason, valid, tmp_path):
    result = execute(script('Validate inputs'), tmp_path, SERVICE=service, SOURCE_SHA=sha, DEPLOY_ACK=ack, REASON=reason)
    assert (result.returncode == 0) is valid


@pytest.mark.parametrize('service', ['api', 'ml', 'compliance'])
@pytest.mark.parametrize('status,conclusion,actual,valid', [
    ('completed', 'success', SHA, True),
    ('in_progress', '', SHA, False),
    ('completed', 'failure', SHA, False),
    ('completed', 'success', 'b' * 40, False),
])
def test_source_ci_gate(service, status, conclusion, actual, valid, tmp_path):
    executable(tmp_path, 'git', 'echo "$ACTUAL_SHA"\n')
    executable(tmp_path, 'gh', 'echo "$RUN_RESPONSE"\n')
    # Keep the gate's output inside the test directory.
    body = script('Require current main and successful exact-source CI').replace('/tmp/source-ci.json', './source-ci.json')
    result = execute(body, tmp_path, SERVICE=service, SOURCE_SHA=SHA, ACTUAL_SHA=actual,
                     HARVESTER_REPO='fixture/product',
                     RUN_RESPONSE=json.dumps([{'status': status, 'conclusion': conclusion}]))
    assert (result.returncode == 0) is valid


@pytest.mark.parametrize('service', ['api', 'ml', 'compliance'])
def test_main_advance_stops_before_mutating_gitops(service, tmp_path):
    executable(tmp_path, 'git', '''case "$*" in
      'fetch origin main') exit 0;;
      'rev-parse HEAD') echo a;;
      'rev-parse origin/main') echo b;;
      *) touch unexpected-mutation; exit 99;;
    esac
    ''')
    result = execute(script('Commit & push digest pin'), tmp_path, SERVICE=service, REASON='Synthetic release validation')
    assert result.returncode != 0
    assert not (tmp_path / 'unexpected-mutation').exists()


def test_api_rejects_mutable_main(tmp_path):
    result = execute(script('Validate inputs'), tmp_path, SERVICE='api', SOURCE_SHA='main',
                     DEPLOY_ACK='production-kustomization', REASON='Synthetic release validation')
    assert result.returncode != 0


def test_unknown_service_is_rejected(tmp_path):
    result = execute(script('Validate inputs'), tmp_path, SERVICE='arbitrary', SOURCE_SHA=SHA,
                     DEPLOY_ACK='production-kustomization', REASON='Synthetic release validation')
    assert result.returncode != 0


def test_all_services_checkout_main_and_run_source_gate():
    steps = WORKFLOW['jobs']['build-service']['steps']
    checkout = next(step for step in steps if step.get('uses', '').startswith('actions/checkout@'))
    assert checkout['with']['ref'] == 'main'
    gate = next(step for step in steps if step.get('name') == 'Require current main and successful exact-source CI')
    assert 'if' not in gate
