"""fusionar-aprobado.yml: the label merge train merges, or says loudly why not.

On 2026-11-19 enclii#980 and #981 were labelled `aprobado-para-fusionar` by
the owner and approved by the workflow, but never merged: the workflow's only
merge identity was the `FUSIONADOR_TOKEN` secret, which does not exist, and the
missing secret was a `core.notice` on a green run. The owner merged both by
hand.

The contract these tests pin:

  * Approval semantics are unchanged: only accounts in APROBADORES count, the
    approval is a `fusionar/aprobado` status on the exact head, a new commit
    removes the label, and a merge is a squash bound to that head sha.
  * Merge identity: a token minted from the first-party GitHub App
    (`vars.FUSIONADOR_APP_ID`), else the legacy `FUSIONADOR_TOKEN`.
  * With no identity and an approved PR, the run fails, the head gets a red
    `fusionar/tren` status and the PR one «el tren no puede fusionar: falta …»
    comment per head (the */10 cron does not repeat either).
  * With no approved PR, a missing identity changes nothing (no red run).
  * A merge GitHub refuses is reported the same way instead of a warning.
  * The train's own `fusionar/tren` status never blocks a later merge.

The github-script body runs verbatim under node against an in-memory GitHub;
nothing here talks to GitHub.
"""

from __future__ import annotations

import json
import shutil
import subprocess
from pathlib import Path

import pytest
import yaml

REPO = Path(__file__).resolve().parents[2]
WORKFLOW = REPO / ".github" / "workflows" / "fusionar-aprobado.yml"
BOT = "github-actions[bot]"
HEAD = "a" * 40

HARNESS = r"""
const fs = require('fs');
const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const st = input.state;
const calls = [];
const failed = [];
const notices = [];
const rec = (name, fn) => async (p) => { calls.push({name, params: p}); return fn ? fn(p) : {data: {}}; };
const github = {
  paginate: async (fn, p) => (await fn(p)).data,
  rest: {
    issues: {
      removeLabel: rec('removeLabel'),
      createComment: rec('createComment'),
      listComments: rec('listComments', p => ({data: (st.comments || {})[p.issue_number] || []})),
    },
    repos: {
      createCommitStatus: rec('createCommitStatus', p => {
        st.statuses = st.statuses || {};
        (st.statuses[p.sha] = st.statuses[p.sha] || []).unshift({...p, creator: {login: 'github-actions[bot]'}});
        return {data: {}};
      }),
      listCommitStatusesForRef: rec('listStatuses', p => ({data: (st.statuses || {})[p.ref] || []})),
    },
    pulls: {
      list: rec('listPulls', () => ({data: st.prs.map(pr => ({number: pr.number, labels: pr.labels}))})),
      get: rec('getPull', p => ({data: st.prs.find(pr => pr.number === p.pull_number)})),
    },
    actions: {
      listWorkflowRunsForRepo: rec('listRuns', p => ({data: (st.runs || {})[p.head_sha] || []})),
    },
    checks: {
      listForRef: rec('listChecks', p => ({data: (st.checks || {})[p.ref] || []})),
    },
  },
};
const fusionador = {rest: {pulls: {merge: async (p) => {
  calls.push({name: 'merge', params: p});
  if (st.mergeError) { const e = new Error(st.mergeError.message); e.status = st.mergeError.status; throw e; }
  return {data: {sha: 'm'.repeat(40)}};
}}}};
const core = {
  info: () => {}, warning: () => {},
  notice: m => notices.push(m),
  setFailed: m => failed.push(m),
};
const fakeRequire = (name) => {
  if (name !== '@actions/github') throw new Error('unexpected require ' + name);
  return {getOctokit: (token) => { calls.push({name: 'getOctokit', params: {token}}); return fusionador; }};
};
const context = {
  repo: {owner: 'madfam-org', repo: 'enclii'},
  eventName: input.event.name,
  payload: input.event.payload || {},
};
Object.assign(process.env, input.env);
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
const fn = new AsyncFunction('github', 'context', 'core', 'require', input.script);
fn(github, context, core, fakeRequire)
  .then(() => process.stdout.write(JSON.stringify({calls, failed, notices})))
  .catch(e => { process.stderr.write(String(e && e.stack || e)); process.exit(2); });
"""


def _job() -> dict:
    return yaml.safe_load(WORKFLOW.read_text())["jobs"]["fusionar"]


def _script_step() -> dict:
    steps = [s for s in _job()["steps"] if str(s.get("uses", "")).startswith("actions/github-script@")]
    assert len(steps) == 1, "fusionar needs exactly one actions/github-script step"
    return steps[0]


@pytest.fixture(scope="module")
def node() -> str:
    path = shutil.which("node")
    if not path:
        pytest.skip("node is not on PATH (the ubuntu-24.04 runner ships it)")
    return path


def _run(node: str, tmp_path: Path, *, event: dict, state: dict, env: dict | None = None) -> dict:
    harness = tmp_path / "harness.js"
    harness.write_text(HARNESS)
    base_env = {
        "ETIQUETA": "aprobado-para-fusionar",
        "APROBADORES": "aldoruizluna",
        "FUSIONADOR_APP_TOKEN": "",
        "FUSIONADOR_TOKEN": "",
        "ESTE_WORKFLOW": "Fusionar lo aprobado",
    }
    base_env.update(env or {})
    payload = {"script": _script_step()["with"]["script"], "event": event, "state": state, "env": base_env}
    proc = subprocess.run([node, str(harness)], input=json.dumps(payload), capture_output=True, text=True, timeout=60)
    assert proc.returncode == 0, proc.stderr
    return json.loads(proc.stdout)


def _named(result: dict, name: str) -> list[dict]:
    return [c["params"] for c in result["calls"] if c["name"] == name]


def _pr(number: int = 980, *, labelled: bool = True, draft: bool = False, mergeable: bool | None = True) -> dict:
    labels = [{"name": "aprobado-para-fusionar"}] if labelled else []
    return {"number": number, "draft": draft, "head": {"sha": HEAD}, "labels": labels, "mergeable": mergeable}


def _approval() -> dict:
    return {"context": "fusionar/aprobado", "state": "success", "creator": {"login": BOT}, "description": "Aprobado"}


def _green_state(**extra) -> dict:
    state = {
        "prs": [_pr()],
        "statuses": {HEAD: [_approval()]},
        "runs": {HEAD: [{"name": "CI Pipeline", "status": "completed", "conclusion": "success"}]},
        "checks": {HEAD: []},
    }
    state.update(extra)
    return state


SCHEDULE = {"name": "schedule"}


def _labelled(sender: str = "aldoruizluna") -> dict:
    return {
        "name": "pull_request_target",
        "payload": {
            "action": "labeled",
            "label": {"name": "aprobado-para-fusionar"},
            "sender": {"login": sender},
            "pull_request": {"number": 980, "head": {"sha": HEAD}, "labels": [{"name": "aprobado-para-fusionar"}]},
        },
    }


# ---------------------------------------------------------------------------
# Structural
# ---------------------------------------------------------------------------


def test_app_token_is_minted_for_this_repo_with_contents_and_pull_requests_only() -> None:
    steps = _job()["steps"]
    app = [s for s in steps if str(s.get("uses", "")).startswith("actions/create-github-app-token@")]
    assert len(app) == 1
    app = app[0]
    assert app["id"] == "app"
    assert app["if"] == "vars.FUSIONADOR_APP_ID != ''"
    perms = {k: v for k, v in app["with"].items() if k.startswith("permission-")}
    assert perms == {"permission-contents": "write", "permission-pull-requests": "write"}
    # No owner/repositories: the action scopes the token to the current repo.
    assert "owner" not in app["with"] and "repositories" not in app["with"]
    assert steps.index(app) < steps.index(_script_step())


def test_script_prefers_the_app_token_over_the_legacy_pat() -> None:
    env = _script_step()["env"]
    assert env["FUSIONADOR_APP_TOKEN"] == "${{ steps.app.outputs.token }}"
    assert env["FUSIONADOR_TOKEN"] == "${{ secrets.FUSIONADOR_TOKEN }}"
    assert "process.env.FUSIONADOR_APP_TOKEN || process.env.FUSIONADOR_TOKEN" in _script_step()["with"]["script"]


def test_missing_identity_is_not_a_quiet_notice() -> None:
    assert "no se fusiona nada" not in _script_step()["with"]["script"]


# ---------------------------------------------------------------------------
# Behaviour
# ---------------------------------------------------------------------------


def test_owner_label_without_identity_records_approval_then_fails_loudly(node: str, tmp_path: Path) -> None:
    r = _run(node, tmp_path, event=_labelled(), state=_green_state(statuses={HEAD: []}))
    statuses = _named(r, "createCommitStatus")
    assert statuses[0]["context"] == "fusionar/aprobado" and statuses[0]["sha"] == HEAD
    assert statuses[0]["state"] == "success"
    assert [s["context"] for s in statuses[1:]] == ["fusionar/tren"]
    assert r["failed"]
    assert "El tren no puede fusionar: falta" in _named(r, "createComment")[0]["body"]
    assert not _named(r, "merge")


def test_approved_head_without_identity_goes_red_with_one_comment(node: str, tmp_path: Path) -> None:
    r = _run(node, tmp_path, event=SCHEDULE, state=_green_state())
    assert r["failed"] and "El tren no pudo fusionar" in r["failed"][0]
    tren = [s for s in _named(r, "createCommitStatus") if s["context"] == "fusionar/tren"]
    assert len(tren) == 1 and tren[0]["state"] == "failure" and tren[0]["sha"] == HEAD
    assert len(tren[0]["description"]) <= 140
    comments = _named(r, "createComment")
    assert len(comments) == 1 and comments[0]["issue_number"] == 980
    body = comments[0]["body"]
    assert "El tren no puede fusionar: falta" in body
    assert "FUSIONADOR_APP_ID" in body and "FUSIONADOR_APP_PRIVATE_KEY" in body and "FUSIONADOR_TOKEN" in body
    assert f"<!-- fusionar/tren:sin-identidad:{HEAD} -->" in body
    assert not _named(r, "getOctokit") and not _named(r, "merge")


def test_cron_does_not_repeat_the_comment_or_the_status(node: str, tmp_path: Path) -> None:
    first = _run(node, tmp_path, event=SCHEDULE, state=_green_state())
    status = [s for s in _named(first, "createCommitStatus") if s["context"] == "fusionar/tren"][0]
    comment = _named(first, "createComment")[0]
    again = _green_state(
        statuses={HEAD: [{"context": "fusionar/tren", "state": "failure", "description": status["description"]}, _approval()]},
        comments={980: [{"user": {"login": BOT}, "body": comment["body"]}]},
    )
    r = _run(node, tmp_path, event=SCHEDULE, state=again)
    assert r["failed"], "the run stays red while an approved PR cannot merge"
    assert not _named(r, "createCommitStatus")
    assert not _named(r, "createComment")


def test_no_approved_pr_means_no_red_run(node: str, tmp_path: Path) -> None:
    unlabelled = _run(node, tmp_path, event=SCHEDULE, state=_green_state(prs=[_pr(labelled=False)]))
    labelled_unapproved = _run(node, tmp_path, event=SCHEDULE, state=_green_state(statuses={HEAD: []}))
    draft = _run(node, tmp_path, event=SCHEDULE, state=_green_state(prs=[_pr(draft=True)]))
    for r in (unlabelled, labelled_unapproved, draft):
        assert not r["failed"]
        assert not _named(r, "createComment") and not _named(r, "createCommitStatus")


@pytest.mark.parametrize("env_key", ["FUSIONADOR_APP_TOKEN", "FUSIONADOR_TOKEN"])
def test_green_approved_head_merges_squash_bound_to_head(node: str, tmp_path: Path, env_key: str) -> None:
    r = _run(node, tmp_path, event=SCHEDULE, state=_green_state(), env={env_key: "tok"})
    assert _named(r, "getOctokit") == [{"token": "tok"}]
    assert _named(r, "merge") == [{"owner": "madfam-org", "repo": "enclii", "pull_number": 980, "sha": HEAD, "merge_method": "squash"}]
    assert not r["failed"] and not _named(r, "createComment")


def test_app_token_wins_over_the_pat(node: str, tmp_path: Path) -> None:
    r = _run(node, tmp_path, event=SCHEDULE, state=_green_state(), env={"FUSIONADOR_APP_TOKEN": "app", "FUSIONADOR_TOKEN": "pat"})
    assert _named(r, "getOctokit") == [{"token": "app"}]


def test_own_red_status_does_not_block_a_later_merge(node: str, tmp_path: Path) -> None:
    state = _green_state(statuses={HEAD: [{"context": "fusionar/tren", "state": "failure", "description": "x"}, _approval()]})
    r = _run(node, tmp_path, event=SCHEDULE, state=state, env={"FUSIONADOR_APP_TOKEN": "app"})
    assert len(_named(r, "merge")) == 1


def test_other_red_status_still_blocks(node: str, tmp_path: Path) -> None:
    state = _green_state(statuses={HEAD: [{"context": "ci/externo", "state": "failure"}, _approval()]})
    r = _run(node, tmp_path, event=SCHEDULE, state=state, env={"FUSIONADOR_APP_TOKEN": "app"})
    assert not _named(r, "merge") and not r["failed"]


def test_refused_merge_is_reported_loudly(node: str, tmp_path: Path) -> None:
    state = _green_state(mergeError={"status": 403, "message": "Resource not accessible by integration"})
    r = _run(node, tmp_path, event=SCHEDULE, state=state, env={"FUSIONADOR_APP_TOKEN": "app"})
    assert r["failed"] and "403" in r["failed"][0]
    tren = [s for s in _named(r, "createCommitStatus") if s["context"] == "fusionar/tren"]
    assert tren and tren[0]["state"] == "failure"
    body = _named(r, "createComment")[0]["body"]
    assert "403" in body and "Resource not accessible by integration" in body
    assert f"<!-- fusionar/tren:fusion-403:{HEAD} -->" in body


def test_non_owner_label_is_removed_and_nothing_else_happens(node: str, tmp_path: Path) -> None:
    r = _run(node, tmp_path, event=_labelled(sender="some-agent"), state=_green_state())
    assert _named(r, "removeLabel")
    assert not _named(r, "createCommitStatus") and not _named(r, "listPulls") and not r["failed"]


def test_new_commit_removes_the_label(node: str, tmp_path: Path) -> None:
    event = {
        "name": "pull_request_target",
        "payload": {"action": "synchronize", "pull_request": {"number": 980, "head": {"sha": HEAD}, "labels": [{"name": "aprobado-para-fusionar"}]}},
    }
    r = _run(node, tmp_path, event=event, state=_green_state())
    assert _named(r, "removeLabel") and not _named(r, "listPulls") and not r["failed"]
