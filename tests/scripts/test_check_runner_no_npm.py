"""
Tests for scripts/check-runner-no-npm.py.

Run with:
    pytest tests/scripts/test_check_runner_no_npm.py -v

The check keeps the 2026-09-30 fix (enclii #660) in place: the published
Node.js runner images delete npm/npx, because Trivy failed them on CVEs that
lived only inside npm's vendored dependency tree. These tests drive the script
against throwaway trees via --root, plus the real repository.
"""
from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-runner-no-npm.py"

NODE_BASE = "FROM public.ecr.aws/docker/library/node:22-alpine AS base\n" \
    "RUN npm install -g npm@11.19.1 && npm cache clean --force\n"

REMOVAL = (
    "RUN apk add --no-cache wget \\\n"
    "  && rm -rf /root/.npm \\\n"
    "  && rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm /usr/local/bin/npx\n"
)

GOOD_RUNNER = (
    NODE_BASE
    + "FROM base AS runner\n"
    + "# npm is deleted below; this comment mentions npm start and must not count\n"
    + REMOVAL
    + "USER nextjs\n"
    + 'HEALTHCHECK CMD wget -qO- http://localhost:3000/health || exit 1\n'
    + 'CMD ["node", "server.js"]\n'
)


def make_tree(tmp_path: Path, dockerfiles: dict[str, str], services: list[dict]) -> Path:
    for rel, body in dockerfiles.items():
        path = tmp_path / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body)
    (tmp_path / "services.json").write_text(json.dumps({"services": services}))
    return tmp_path


def run(root: Path) -> tuple[int, str]:
    proc = subprocess.run(
        [sys.executable, str(SCRIPT), "--root", str(root)],
        capture_output=True,
        text=True,
    )
    return proc.returncode, proc.stdout + proc.stderr


def svc(name: str, dockerfile: str, target: str = "") -> dict:
    entry = {"name": name, "dockerfile": dockerfile}
    if target:
        entry["target"] = target
    return entry


def test_real_repository_passes():
    code, out = run(REPO_ROOT)
    assert code == 0, out
    # The three runners #660 changed are the ones checked, not skipped.
    for name in ("switchyard-ui", "dispatch", "enclii-status"):
        assert name in out.split("skipped=")[0], out


def test_runner_that_keeps_npm_fails(tmp_path):
    body = NODE_BASE + "FROM base AS runner\n" + 'CMD ["node", "server.js"]\n'
    root = make_tree(tmp_path, {"apps/ui/Dockerfile": body}, [svc("ui", "apps/ui/Dockerfile")])
    code, out = run(root)
    assert code == 1
    assert "never deletes npm/npx" in out


def test_partial_removal_fails(tmp_path):
    body = (
        NODE_BASE
        + "FROM base AS runner\n"
        + "RUN rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm\n"
        + 'CMD ["node", "server.js"]\n'
    )
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("ui", "Dockerfile")])
    code, out = run(root)
    assert code == 1, out  # npx left behind still carries npm's tree


def test_good_runner_passes_and_comments_are_ignored(tmp_path):
    root = make_tree(tmp_path, {"Dockerfile": GOOD_RUNNER}, [svc("ui", "Dockerfile")])
    code, out = run(root)
    assert code == 0, out
    assert "runner_npm_check=OK" in out


def test_cmd_calling_npm_after_removal_fails(tmp_path):
    body = GOOD_RUNNER.replace('CMD ["node", "server.js"]', 'CMD ["npm", "start"]')
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("ui", "Dockerfile")])
    code, out = run(root)
    assert code == 1
    assert "CMD calls npm/npx" in out


def test_healthcheck_calling_npx_after_removal_fails(tmp_path):
    body = GOOD_RUNNER.replace(
        "HEALTHCHECK CMD wget -qO- http://localhost:3000/health || exit 1",
        "HEALTHCHECK CMD npx --yes wait-on http://localhost:3000/health",
    )
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("ui", "Dockerfile")])
    code, out = run(root)
    assert code == 1
    assert "HEALTHCHECK calls npm/npx" in out


def test_pnpm_npmrc_and_npm_config_are_not_npm(tmp_path):
    body = GOOD_RUNNER.replace(
        "USER nextjs",
        "RUN rm -f .npmrc && echo $npm_config_cache && pnpm --version\nUSER nextjs",
    )
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("ui", "Dockerfile")])
    code, out = run(root)
    assert code == 0, out


def test_removal_inherited_from_an_intermediate_stage(tmp_path):
    body = (
        NODE_BASE
        + "FROM base AS slim\n"
        + REMOVAL
        + "FROM slim AS runner\n"
        + 'CMD ["node", "server.js"]\n'
    )
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("ui", "Dockerfile")])
    code, out = run(root)
    assert code == 0, out


def test_non_node_final_stage_is_skipped(tmp_path):
    body = NODE_BASE + "RUN npm run build\nFROM nginxinc/nginx-unprivileged:alpine\n" \
        'CMD ["nginx", "-g", "daemon off;"]\n'
    root = make_tree(
        tmp_path,
        {"web/Dockerfile": body, "ui/Dockerfile": GOOD_RUNNER},
        [svc("web", "web/Dockerfile"), svc("ui", "ui/Dockerfile")],
    )
    code, out = run(root)
    assert code == 0, out
    assert "skipped=1 (web)" in out


def test_target_stage_is_the_one_checked(tmp_path):
    # The final stage is a clean nginx image, but services.json publishes the
    # `runner` target, which still carries npm.
    body = (
        NODE_BASE
        + "FROM base AS runner\n"
        + 'CMD ["node", "server.js"]\n'
        + "FROM nginx:alpine AS static\n"
    )
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("ui", "Dockerfile", target="runner")])
    code, out = run(root)
    assert code == 1
    assert "never deletes npm/npx" in out


def test_empty_scope_is_not_a_pass(tmp_path):
    body = "FROM golang:1.26 AS build\nFROM alpine:3.24\n"
    root = make_tree(tmp_path, {"Dockerfile": body}, [svc("api", "Dockerfile")])
    code, out = run(root)
    assert code == 1
    assert "no Node.js runner image" in out


def test_missing_dockerfile_is_an_error(tmp_path):
    root = make_tree(tmp_path, {}, [svc("ui", "nope/Dockerfile")])
    code, _ = run(root)
    assert code == 2
