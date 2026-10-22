"""
pgbouncer auth_query (2026-10-21): the chain from Vault to a pooled login.

Run with:
    pytest tests/scripts/test_pgbouncer_auth_query.py -v -rs

THE CHAIN THIS PINS
===================
  intake target data/pgbouncer-auth
    -> secret/enclii:pgbouncer_auth_password
    -> ExternalSecret data/pgbouncer-auth (in platform-infra/kustomization.yaml)
       writing Secret pgbouncer-auth key pgbouncer_auth_password
    -> the pgbouncer Deployment's build-userlist init container, which writes
       `"pgbouncer_auth" "<password>"` first and then the hand-kept
       pgbouncer-userlist lines into a memory emptyDir
    -> mounted at /etc/pgbouncer/userlist.txt (pgbouncer.ini auth_file)
    -> pgbouncer.ini auth_user/auth_dbname/auth_query -> pgbouncer.get_auth(text)
    -> created by scripts/sql/pgbouncer-auth-query.sql, removed by the rollback
    -> switchyard-api's drift checker (PgbouncerAuthFunction, the Secret name and
       key, the direct-connect-roles ConfigMap key) and its RBAC.

  The admin console user (2026-10-22) has a parallel, optional chain:
  intake target data/pgbouncer-admin -> secret/enclii:pgbouncer_admin_password
    -> ExternalSecret data/pgbouncer-admin -> Secret pgbouncer-admin, mounted
       optional -> the init container's second line -> admin_users/stats_users.

Three layers:
  1. Static: every copy of a name agrees (no cluster, no database).
  2. Behaviour of the shell that ships: the init container script run with sh,
     and scripts/pgbouncer-auth-query.sh run against stub ssh/sudo/kubectl. The
     password is asserted never to reach stdout, stderr or any argv.
  3. End to end, when PostgreSQL and pgbouncer binaries are present: initdb,
     the real migration, the manifest's own pgbouncer.ini, and pooled logins.
     CI job "pgbouncer auth_query" installs them; locally the case skips with
     the reason (see docs/testing/SKIPPED_TESTS.md).
"""
from __future__ import annotations

import base64
import os
import re
import shutil
import socket
import subprocess
import textwrap
import time
from pathlib import Path

import pytest
import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
PLATFORM_INFRA = REPO_ROOT / "infra" / "k8s" / "platform-infra"
PGBOUNCER_YAML = PLATFORM_INFRA / "pgbouncer.yaml"
EXTERNAL_SECRET = PLATFORM_INFRA / "pgbouncer-auth.externalsecret.yaml"
ADMIN_EXTERNAL_SECRET = PLATFORM_INFRA / "pgbouncer-admin.externalsecret.yaml"
KUSTOMIZATION = PLATFORM_INFRA / "kustomization.yaml"
MIGRATE_SQL = REPO_ROOT / "scripts" / "sql" / "pgbouncer-auth-query.sql"
ROLLBACK_SQL = REPO_ROOT / "scripts" / "sql" / "pgbouncer-auth-query-rollback.sql"
WRAPPER = REPO_ROOT / "scripts" / "pgbouncer-auth-query.sh"
REGISTRY = REPO_ROOT / "apps" / "switchyard-api" / "internal" / "secretsintake" / "registry.yaml"
GO_AUTH = REPO_ROOT / "apps" / "switchyard-api" / "internal" / "provisioning" / "pgbouncer_auth_query.go"
RBAC = REPO_ROOT / "infra" / "k8s" / "cross-namespace-rbac" / "rbac.yaml"

FUNCTION = "pgbouncer.get_auth(text)"
SECRET_NAME = "pgbouncer-auth"  # pragma: allowlist secret
SECRET_KEY = "pgbouncer_auth_password"  # pragma: allowlist secret
ADMIN_SECRET_NAME = "pgbouncer-admin"  # pragma: allowlist secret
ADMIN_SECRET_KEY = "pgbouncer_admin_password"  # pragma: allowlist secret
# internal-devops audits/2026-10-17-infra-stability/lanes/W1-P.md, W1P-5.
W1P_DIRECT_ROLES = {
    "app_admin", "janua_migrator", "phyne", "posthog",
    "selva_migrator", "telesia", "tlacuilo", "voxa_app",
}
# A recognisable fake password in the shape intake --generate mints.
FAKE_PW = "TESTONLYpgbouncerAuthNotARealPassword_0123"  # pragma: allowlist secret
FAKE_ADMIN_PW = "TESTONLYpgbouncerAdminNotARealPassword_456"  # pragma: allowlist secret


def _docs(path: Path) -> list[dict]:
    return [d for d in yaml.safe_load_all(path.read_text()) if d]


def _pgbouncer():
    docs = _docs(PGBOUNCER_YAML)
    cm = next(d for d in docs if d["kind"] == "ConfigMap")
    deploy = next(d for d in docs if d["kind"] == "Deployment")
    return cm, deploy


def _ini_settings(ini: str) -> dict[str, str]:
    out, section = {}, ""
    for raw in ini.splitlines():
        line = raw.strip()
        if not line or line[0] in ";#":
            continue
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1].strip().lower()
            continue
        if section == "pgbouncer" and "=" in line:
            k, v = line.split("=", 1)
            out[k.strip().lower()] = v.strip()
    return out


def _init_script() -> str:
    _, deploy = _pgbouncer()
    init = next(c for c in deploy["spec"]["template"]["spec"]["initContainers"] if c["name"] == "build-userlist")
    assert init["command"][:2] == ["/bin/sh", "-c"]
    return init["command"][2]


# --------------------------------------------------------------------------
# 1. Static
# --------------------------------------------------------------------------


def test_ini_turns_on_auth_query_through_the_lookup_function():
    cm, _ = _pgbouncer()
    s = _ini_settings(cm["data"]["pgbouncer.ini"])
    assert s["auth_type"] == "md5", "md5 uses SCRAM automatically for SCRAM verifiers; plain cannot check one"
    assert s["auth_file"] == "/etc/pgbouncer/userlist.txt"
    assert s["auth_user"] == "pgbouncer_auth"
    assert s["auth_dbname"] == "postgres"
    assert s["auth_query"] == "SELECT usename, passwd FROM pgbouncer.get_auth($1)"


def test_migration_and_rollback_agree_on_the_function_and_role():
    sql = MIGRATE_SQL.read_text()
    assert "CREATE OR REPLACE FUNCTION pgbouncer.get_auth(p_usename text)" in sql
    assert "SECURITY DEFINER" in sql
    assert "SET search_path = pg_catalog, pg_temp" in sql
    assert f"REVOKE ALL ON FUNCTION {FUNCTION} FROM PUBLIC;" in sql
    assert f"GRANT EXECUTE ON FUNCTION {FUNCTION} TO pgbouncer_auth;" in sql
    assert "REVOKE ALL ON SCHEMA pgbouncer FROM PUBLIC;" in sql
    assert "NOT s.usesuper" in sql, "get_auth must never return a superuser"
    assert "PASSWORD :'auth_password'" in sql, "the password arrives as a quoted psql variable"
    rollback = ROLLBACK_SQL.read_text()
    assert f"DROP FUNCTION IF EXISTS {FUNCTION};" in rollback
    assert "DROP ROLE pgbouncer_auth;" in rollback


def test_go_detector_names_match():
    go = GO_AUTH.read_text()
    assert f'PgbouncerAuthFunction = "{FUNCTION}"' in go
    assert f'pgbouncerAuthSecretName   = "{SECRET_NAME}"' in go
    assert f'pgbouncerAuthSecretKey    = "{SECRET_KEY}"' in go
    assert 'pgbouncerDirectConnectKey = "direct-connect-roles"' in go


def test_external_secret_delivers_the_key_the_pod_reads():
    (es,) = _docs(EXTERNAL_SECRET)
    assert es["kind"] == "ExternalSecret"
    assert es["metadata"] == {**es["metadata"], "name": SECRET_NAME, "namespace": "data"}
    assert es["spec"]["secretStoreRef"] == {"name": "vault-store", "kind": "ClusterSecretStore"}
    assert es["spec"]["target"]["name"] == SECRET_NAME
    assert es["spec"]["target"]["creationPolicy"] == "Owner"
    assert es["spec"]["target"]["deletionPolicy"] == "Retain"
    (item,) = es["spec"]["data"]
    assert item["secretKey"] == SECRET_KEY
    assert item["remoteRef"]["key"] == "secret/enclii"
    assert item["remoteRef"]["property"] == SECRET_KEY
    assert "pgbouncer-auth.externalsecret.yaml" in yaml.safe_load(KUSTOMIZATION.read_text())["resources"]


def test_intake_target_matches_the_external_secret():
    target = yaml.safe_load(REGISTRY.read_text())["targets"]["data/pgbouncer-auth"]
    assert target["vault_path"] == "secret/enclii"
    assert target["namespace"] == "data"
    assert target["external_secret"] == SECRET_NAME
    assert target["keys"] == [SECRET_KEY]


def test_admin_console_user_has_its_own_vault_chain():
    """pgbouncer_admin is in admin_users and stats_users; the console has no
    Postgres backend, so only a userlist line lets it log in (2026-10-22)."""
    cm, _ = _pgbouncer()
    s = _ini_settings(cm["data"]["pgbouncer.ini"])
    assert s["admin_users"] == "pgbouncer_admin"
    assert s["stats_users"] == "pgbouncer_admin"
    (es,) = _docs(ADMIN_EXTERNAL_SECRET)
    assert es["kind"] == "ExternalSecret"
    assert es["metadata"] == {**es["metadata"], "name": ADMIN_SECRET_NAME, "namespace": "data"}
    assert es["spec"]["secretStoreRef"] == {"name": "vault-store", "kind": "ClusterSecretStore"}
    assert es["spec"]["target"]["name"] == ADMIN_SECRET_NAME
    assert es["spec"]["target"]["creationPolicy"] == "Owner"
    assert es["spec"]["target"]["deletionPolicy"] == "Retain"
    (item,) = es["spec"]["data"]
    assert item["secretKey"] == ADMIN_SECRET_KEY
    assert item["remoteRef"]["key"] == "secret/enclii"
    assert item["remoteRef"]["property"] == ADMIN_SECRET_KEY
    assert "pgbouncer-admin.externalsecret.yaml" in yaml.safe_load(KUSTOMIZATION.read_text())["resources"]
    target = yaml.safe_load(REGISTRY.read_text())["targets"]["data/pgbouncer-admin"]
    assert target["vault_path"] == "secret/enclii"
    assert target["namespace"] == "data"
    assert target["external_secret"] == ADMIN_SECRET_NAME
    assert target["keys"] == [ADMIN_SECRET_KEY]


def test_deployment_wires_the_generated_userlist():
    _, deploy = _pgbouncer()
    spec = deploy["spec"]["template"]["spec"]
    volumes = {v["name"]: v for v in spec["volumes"]}
    assert volumes["pgbouncer-auth"]["secret"]["secretName"] == SECRET_NAME
    assert not volumes["pgbouncer-auth"]["secret"].get("optional"), (
        "required on purpose: without it new pods wait and the old ones keep serving"
    )
    assert volumes["pgbouncer-admin"]["secret"]["secretName"] == ADMIN_SECRET_NAME
    assert volumes["pgbouncer-admin"]["secret"]["optional"] is True, (
        "optional on purpose: the pooler must start before intake writes the admin password"
    )
    assert volumes["pgbouncer-generated"]["emptyDir"]["medium"] == "Memory"
    init = next(c for c in spec["initContainers"] if c["name"] == "build-userlist")
    mounts = {m["name"]: m["mountPath"] for m in init["volumeMounts"]}
    assert mounts == {
        "pgbouncer-userlist": "/etc/pgbouncer-userlist-source",
        "pgbouncer-auth": "/etc/pgbouncer-auth",
        "pgbouncer-admin": "/etc/pgbouncer-admin",
        "pgbouncer-generated": "/etc/pgbouncer-generated",
    }
    assert f"/etc/pgbouncer-auth/{SECRET_KEY}" in init["command"][2]
    assert f"/etc/pgbouncer-admin/{ADMIN_SECRET_KEY}" in init["command"][2]
    main = next(c for c in spec["containers"] if c["name"] == "pgbouncer")
    userlist_mount = next(m for m in main["volumeMounts"] if m["mountPath"] == "/etc/pgbouncer/userlist.txt")
    assert userlist_mount["name"] == "pgbouncer-generated"
    assert userlist_mount["subPath"] == "userlist.txt"
    assert init["image"] == main["image"], "same pinned image: no extra pull, the shell is already there"


def test_direct_connect_roles_declare_the_w1p_eight():
    cm, _ = _pgbouncer()
    declared = {
        line.split("#", 1)[0].strip()
        for line in cm["data"]["direct-connect-roles"].splitlines()
    } - {""}
    assert W1P_DIRECT_ROLES <= declared
    assert "ceq" not in declared, "ceq does use the pooler (W1P-5): it must stay counted"


def test_switchyard_can_read_only_the_auth_secret():
    role = next(
        d for d in _docs(RBAC)
        if d["kind"] == "Role" and d["metadata"]["name"] == "switchyard-pgbouncer-manager"
    )
    rules = [r for r in role["rules"] if SECRET_NAME in r.get("resourceNames", [])]
    assert len(rules) == 1
    assert rules[0]["resources"] == ["secrets"]
    assert rules[0]["verbs"] == ["get"]


# --------------------------------------------------------------------------
# 2a. The init container script
# --------------------------------------------------------------------------


def _run_init(tmp_path: Path, password: str | None, source: str | None, admin: str | None = None):
    # admin None: the optional pgbouncer-admin Secret does not exist, so its
    # volume is an empty directory (what kubelet mounts for optional: true).
    src_dir, auth_dir, admin_dir, out_dir = (tmp_path / n for n in ("src", "auth", "admin", "out"))
    for d in (src_dir, auth_dir, admin_dir, out_dir):
        d.mkdir(parents=True)
    if source is not None:
        (src_dir / "userlist.txt").write_text(source)
    if password is not None:
        (auth_dir / SECRET_KEY).write_text(password)
    if admin is not None:
        (admin_dir / ADMIN_SECRET_KEY).write_text(admin)
    script = (
        _init_script()
        .replace("/etc/pgbouncer-userlist-source", str(src_dir))
        .replace("/etc/pgbouncer-auth", str(auth_dir))
        .replace("/etc/pgbouncer-admin", str(admin_dir))
        .replace("/etc/pgbouncer-generated", str(out_dir))
    )
    proc = subprocess.run(["sh", "-c", script], capture_output=True, text=True, timeout=30)
    out = out_dir / "userlist.txt"
    return proc, (out.read_text() if out.exists() else None), out


def test_init_script_puts_the_auth_line_first_and_keeps_the_rest(tmp_path):
    source = '"janua" "j"\n"pgbouncer_auth" "stale"\n"pgbouncer_admin" "a"'  # no trailing newline
    proc, text, out = _run_init(tmp_path, FAKE_PW, source)
    assert proc.returncode == 0, proc.stderr
    assert text.splitlines() == [f'"pgbouncer_auth" "{FAKE_PW}"', '"janua" "j"', '"pgbouncer_admin" "a"']
    assert FAKE_PW not in proc.stdout + proc.stderr
    assert "2 line(s) from pgbouncer-userlist" in proc.stdout
    assert oct(out.stat().st_mode & 0o777) == "0o600"


@pytest.mark.parametrize(
    "password, why",
    [
        ("short_but_valid_chars", "shorter than 32"),
        (FAKE_PW[:-1] + '"', "outside base64url"),
        (FAKE_PW + " x", "outside base64url"),
        ("", "shorter than 32"),
    ],
)
def test_init_script_refuses_a_password_that_could_break_the_userlist(tmp_path, password, why):
    proc, text, _ = _run_init(tmp_path, password, '"janua" "j"\n')
    assert proc.returncode != 0
    assert why in proc.stderr
    assert text is None, "nothing is written when the password is refused"


def test_init_script_fails_without_the_secret_file(tmp_path):
    proc, text, _ = _run_init(tmp_path, None, '"janua" "j"\n')
    assert proc.returncode != 0
    assert text is None


def test_init_script_without_the_admin_secret_says_the_console_is_closed(tmp_path):
    proc, text, _ = _run_init(tmp_path, FAKE_PW, '"janua" "j"\n')
    assert proc.returncode == 0, proc.stderr
    assert text.splitlines() == [f'"pgbouncer_auth" "{FAKE_PW}"', '"janua" "j"']
    assert "1 line(s) from pgbouncer-userlist" in proc.stdout
    assert "admin console closed" in proc.stdout


def test_init_script_puts_the_admin_line_second_and_drops_a_hand_kept_one(tmp_path):
    source = '"janua" "j"\n"pgbouncer_admin" "stale"\n"pgbouncer_auth" "stale"\n"ceq" "c"\n'
    proc, text, out = _run_init(tmp_path, FAKE_PW, source, admin=FAKE_ADMIN_PW)
    assert proc.returncode == 0, proc.stderr
    assert text.splitlines() == [
        f'"pgbouncer_auth" "{FAKE_PW}"',
        f'"pgbouncer_admin" "{FAKE_ADMIN_PW}"',
        '"janua" "j"',
        '"ceq" "c"',
    ]
    assert FAKE_PW not in proc.stdout + proc.stderr
    assert FAKE_ADMIN_PW not in proc.stdout + proc.stderr
    assert "pgbouncer_auth + pgbouncer_admin + 2 line(s) from pgbouncer-userlist" in proc.stdout
    assert oct(out.stat().st_mode & 0o777) == "0o600"


@pytest.mark.parametrize(
    "admin, why",
    [
        ("short_but_valid_chars", "pgbouncer_admin password is shorter than 32"),
        (FAKE_ADMIN_PW[:-1] + '"', "pgbouncer_admin password has characters outside base64url"),
        ("", "pgbouncer_admin password is shorter than 32"),
    ],
)
def test_init_script_refuses_an_admin_password_that_could_break_the_userlist(tmp_path, admin, why):
    proc, text, _ = _run_init(tmp_path, FAKE_PW, '"janua" "j"\n', admin=admin)
    assert proc.returncode != 0
    assert why in proc.stderr
    assert text is None, "nothing is written when the admin password is refused"


# --------------------------------------------------------------------------
# 2b. scripts/pgbouncer-auth-query.sh against stub ssh / sudo / kubectl
# --------------------------------------------------------------------------

STUB_SSH = """#!/usr/bin/env bash
# Stub ssh: "$1" is the host; the rest is the remote command (bash -s).
host="$1"; shift
printf '%s\\n' "$host" >> "$STUB_LOG.hosts"
exec "$@"
"""

STUB_SUDO = """#!/usr/bin/env bash
[ "$1" = "-n" ] || { echo "sudo without -n" >&2; exit 9; }
shift
exec "$@"
"""

STUB_KUBECTL = """#!/usr/bin/env bash
# Stub kubectl. Records argv; answers from files; psql calls save their stdin.
printf '%s\\n' "$*" >> "$STUB_LOG"
args="$*"
case "$args" in
  *"get secret pgbouncer-auth -o jsonpath={.data.pgbouncer_auth_password}"*)
    printf '%s' "$STUB_SECRET_B64" ;;
  *"get secret pgbouncer-auth -o name"*) echo "secret/pgbouncer-auth" ;;
  *"get secret pgbouncer-admin -o name"*)
    [ "${STUB_ADMIN_LINE:-absent}" = present ] || exit 1
    echo "secret/pgbouncer-admin" ;;
  *"get externalsecret"*) echo "EXTERNALSECRET READY REASON"; echo "pgbouncer-auth True SecretSynced" ;;
  *"get configmap pgbouncer-config"*) cat "$STUB_INI" ;;
  *"get pods -l app=pgbouncer"*) printf 'pgbouncer-a\\npgbouncer-b\\n' ;;
  *"exec -i deploy/postgres"*) cat >> "$STUB_PSQL_STDIN"; echo "psql ok" ;;
  *"exec -i pgbouncer-"*) cat > /dev/null; echo "pooled SCRAM login ok as pgbouncer_auth_probe" ;;
  *"exec pgbouncer-"*"echo present"*) echo "${STUB_ADMIN_LINE:-absent}" ;;
  *"exec pgbouncer-"*) echo '"pgbouncer_auth"' ;;
  *"logs pgbouncer-"*) : ;;
  *) echo "unexpected kubectl call: $args" >&2; exit 7 ;;
esac
"""


class _Env(dict):
    """The subprocess environment. Its repr is short so a failing assertion
    never prints the caller's environment (which may hold real tokens)."""

    def __repr__(self) -> str:
        return "<subprocess env>"


@pytest.fixture()
def stub_env(tmp_path):
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    for name, body in (("ssh", STUB_SSH), ("sudo", STUB_SUDO), ("kubectl", STUB_KUBECTL)):
        p = bin_dir / name
        p.write_text(body)
        p.chmod(0o755)
    ini = tmp_path / "ini"
    ini.write_text("[pgbouncer]\nauth_type = md5\nauth_query = SELECT usename, passwd FROM pgbouncer.get_auth($1)\n")
    env = _Env({
        **os.environ,
        "PATH": f"{bin_dir}:{os.environ['PATH']}",
        "PGB_AUTH_SSH": str(bin_dir / "ssh"),
        "STUB_LOG": str(tmp_path / "kubectl.log"),
        "STUB_PSQL_STDIN": str(tmp_path / "psql.stdin"),
        "STUB_INI": str(ini),
        "STUB_SECRET_B64": base64.b64encode(FAKE_PW.encode()).decode(),
    })
    return env, tmp_path


def _wrapper(env, *args):
    return subprocess.run(["bash", str(WRAPPER), *args], env=env, capture_output=True, text=True, timeout=60)


def _calls(tmp_path) -> str:
    log = tmp_path / "kubectl.log"
    return log.read_text() if log.exists() else ""


def test_migrate_feeds_the_password_on_stdin_only(stub_env):
    env, tmp = stub_env
    proc = _wrapper(env, "migrate")
    assert proc.returncode == 0, proc.stderr
    stdin = (tmp / "psql.stdin").read_text()
    assert stdin.startswith(f"\\set auth_password {FAKE_PW}\n")
    assert MIGRATE_SQL.read_text() in stdin
    assert FAKE_PW not in proc.stdout + proc.stderr
    assert FAKE_PW not in _calls(tmp), "the password must never be a kubectl argument"
    assert (tmp / "kubectl.log.hosts").read_text().strip() == "ssh.madfam.io"


@pytest.mark.parametrize(
    "value, why",
    [("", "has no pgbouncer_auth_password"), ("short", "shorter than 32"), (FAKE_PW + "'", "not base64url")],
)
def test_migrate_refuses_a_missing_or_odd_password(stub_env, value, why):
    env, tmp = stub_env
    env = _Env({**env, "STUB_SECRET_B64": base64.b64encode(value.encode()).decode()})
    proc = _wrapper(env, "migrate")
    assert proc.returncode != 0
    assert why in proc.stderr
    assert not (tmp / "psql.stdin").exists(), "no SQL runs after a refusal"


@pytest.mark.parametrize("step", ["precheck", "verify", "rollback"])
def test_read_only_and_rollback_steps_never_read_the_password(stub_env, step):
    env, tmp = stub_env
    if step == "rollback":
        (tmp / "ini").write_text("[pgbouncer]\nauth_type = plain\n")
    proc = _wrapper(env, step)
    assert proc.returncode == 0, proc.stderr
    assert "get secret pgbouncer-auth -o jsonpath" not in _calls(tmp)


def test_read_only_steps_send_no_ddl(stub_env):
    env, tmp = stub_env
    for step in ("precheck", "verify"):
        assert _wrapper(env, step).returncode == 0
    sql = (tmp / "psql.stdin").read_text().upper()
    for verb in ("CREATE ", "ALTER ", "DROP ", "GRANT ", "REVOKE ", "INSERT ", "UPDATE ", "DELETE "):
        assert verb not in sql, f"{verb.strip()} in a read-only step"


def test_rollback_refuses_while_the_config_still_uses_auth_query(stub_env):
    env, tmp = stub_env
    proc = _wrapper(env, "rollback")
    assert proc.returncode != 0
    assert "still sets auth_query" in proc.stderr
    assert not (tmp / "psql.stdin").exists()


def test_rollback_runs_the_rollback_sql(stub_env):
    env, tmp = stub_env
    (tmp / "ini").write_text("[pgbouncer]\nauth_type = plain\n")
    assert _wrapper(env, "rollback").returncode == 0
    assert ROLLBACK_SQL.read_text() in (tmp / "psql.stdin").read_text()


def test_probe_drops_its_role_and_keeps_the_password_off_argv(stub_env):
    env, tmp = stub_env
    proc = _wrapper(env, "probe")
    assert proc.returncode == 0, proc.stderr
    stdin = (tmp / "psql.stdin").read_text()
    pw = re.search(r"^\\set pw (\S+)$", stdin, re.M).group(1)
    assert len(pw) == 40
    assert pw not in _calls(tmp) and pw not in proc.stdout + proc.stderr
    assert stdin.count("DROP ROLE IF EXISTS pgbouncer_auth_probe;") == 2, "before (leftover) and after"
    assert proc.stdout.count(": OK") == 2, "one line per pooler pod"


def test_verify_skips_the_admin_console_visibly_without_an_admin_line(stub_env):
    env, tmp = stub_env
    proc = _wrapper(env, "verify")
    assert proc.returncode == 0, proc.stderr
    assert proc.stdout.count("admin console: SKIPPED") == 2, "one line per pooler pod"
    assert "[WARN] admin console check skipped on 2 pooler pod(s)" in proc.stdout
    assert "enclii secrets intake submit data/pgbouncer-admin --generate pgbouncer_admin_password" in proc.stdout
    assert "SHOW USERS" not in _calls(tmp), "no login attempt with an empty password"


def test_verify_runs_show_users_when_the_admin_line_exists(stub_env):
    env, tmp = stub_env
    env = _Env({**env, "STUB_ADMIN_LINE": "present"})
    proc = _wrapper(env, "verify")
    assert proc.returncode == 0, proc.stderr
    assert "SKIPPED" not in proc.stdout and "[WARN]" not in proc.stdout
    assert _calls(tmp).count("SHOW USERS") == 2


def test_precheck_reports_the_admin_secret_by_name_only(stub_env):
    env, _ = stub_env
    proc = _wrapper(env, "precheck")
    assert proc.returncode == 0, proc.stderr
    assert "secret/pgbouncer-admin is missing: the admin console stays closed" in proc.stdout
    env = _Env({**env, "STUB_ADMIN_LINE": "present"})
    assert "secret/pgbouncer-admin exists" in _wrapper(env, "precheck").stdout


def test_verify_rejects_a_role_name_that_is_not_an_identifier(stub_env):
    env, _ = stub_env
    proc = _wrapper(env, "verify", "ceq; DROP ROLE x")
    assert proc.returncode != 0
    assert "plain lowercase identifier" in proc.stderr


# --------------------------------------------------------------------------
# 3. End to end with real PostgreSQL and pgbouncer
# --------------------------------------------------------------------------


def _find_pg_bin() -> Path | None:
    candidates = [os.environ.get("PG_BIN_DIR", "")]
    candidates += sorted((str(p) for p in Path("/usr/lib/postgresql").glob("*/bin")), reverse=True) if Path("/usr/lib/postgresql").exists() else []
    which = shutil.which("initdb")
    if which:
        candidates.append(str(Path(which).parent))
    for c in candidates:
        if c and (Path(c) / "initdb").exists() and (Path(c) / "pg_ctl").exists() and (Path(c) / "psql").exists():
            return Path(c)
    return None


def _free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


PG_BIN = _find_pg_bin()
PGBOUNCER = os.environ.get("PGBOUNCER_BIN") or shutil.which("pgbouncer") or (
    "/usr/sbin/pgbouncer" if Path("/usr/sbin/pgbouncer").exists() else None
)
_MISSING_BINARIES = PG_BIN is None or PGBOUNCER is None or (hasattr(os, "geteuid") and os.geteuid() == 0)
# CI sets PGBOUNCER_AUTH_QUERY_E2E_REQUIRED=1: there a missing binary is a
# failure, never a quiet skip.
if _MISSING_BINARIES and os.environ.get("PGBOUNCER_AUTH_QUERY_E2E_REQUIRED") == "1":
    raise RuntimeError(
        f"end-to-end binaries required but not found: PG_BIN={PG_BIN} PGBOUNCER={PGBOUNCER}"
    )
needs_binaries = pytest.mark.skipif(
    _MISSING_BINARIES,
    reason="needs PostgreSQL (initdb, pg_ctl, psql) and pgbouncer binaries and a non-root user; "
    "CI job 'pgbouncer auth_query' installs them (set PG_BIN_DIR / PGBOUNCER_BIN locally)",
)


@pytest.fixture()
def cluster(tmp_path_factory):
    # Short base path: the Unix socket path limit is about 100 bytes.
    base = Path(subprocess.check_output(["mktemp", "-d", "/tmp/pgbaq.XXXXXX"], text=True).strip())
    pg_port, pgb_port = _free_port(), _free_port()
    data = base / "data"
    subprocess.run(
        [PG_BIN / "initdb", "-D", data, "-U", "postgres", "--auth-local=trust",
         "--auth-host=scram-sha-256", "-E", "UTF8", "--locale=C"],
        check=True, capture_output=True,
    )
    (data / "pg_hba.conf").write_text("local all all trust\nhost all all 127.0.0.1/32 scram-sha-256\n")
    subprocess.run(
        [PG_BIN / "pg_ctl", "-D", data, "-l", base / "pg.log", "-w", "start",
         "-o", f"-p {pg_port} -c listen_addresses=127.0.0.1 -c unix_socket_directories={base}"],
        check=True, capture_output=True,
    )
    procs = {}

    def psql(sql: str, *, stdin: str | None = None, db: str = "postgres", check: bool = True):
        args = [PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-h", str(base), "-p", str(pg_port), "-U", "postgres", "-d", db]
        if sql:
            args += ["-c", sql]
        return subprocess.run(args, input=stdin, capture_output=True, text=True, check=check)

    def start_pgbouncer(userlist: str):
        if "pgb" in procs:
            procs["pgb"].terminate()
            procs["pgb"].wait(10)
        cm, _ = _pgbouncer()
        ini = cm["data"]["pgbouncer.ini"]
        ini = re.sub(r"(?m)^listen_addr = .*$", "listen_addr = 127.0.0.1", ini)
        ini = re.sub(r"(?m)^listen_port = .*$", f"listen_port = {pgb_port}\nunix_socket_dir =", ini)
        ini = ini.replace("auth_file = /etc/pgbouncer/userlist.txt", f"auth_file = {base / 'userlist.txt'}")
        ini = ini.replace("host=postgres.data.svc.cluster.local port=5432", f"host=127.0.0.1 port={pg_port}")
        (base / "pgbouncer.ini").write_text(ini)
        (base / "userlist.txt").write_text(userlist)
        procs["pgb"] = subprocess.Popen(
            [PGBOUNCER, str(base / "pgbouncer.ini")],
            stdout=open(base / "pgb.log", "a"), stderr=subprocess.STDOUT,
        )
        for _ in range(100):
            with socket.socket() as s:
                if s.connect_ex(("127.0.0.1", pgb_port)) == 0:
                    return
            time.sleep(0.1)
        raise AssertionError("pgbouncer did not start:\n" + (base / "pgb.log").read_text())

    def pooled(user: str, password: str, db: str = "postgres", sql: str = "select current_user"):
        env = _Env({**os.environ, "PGPASSWORD": password, "PGCONNECT_TIMEOUT": "5"})
        return subprocess.run(
            [PG_BIN / "psql", "-X", "-At", "-h", "127.0.0.1", "-p", str(pgb_port), "-U", user, "-d", db, "-c", sql],
            env=env, capture_output=True, text=True,
        )

    try:
        yield base, psql, start_pgbouncer, pooled
    finally:
        if "pgb" in procs:
            procs["pgb"].terminate()
            procs["pgb"].wait(10)
        subprocess.run([PG_BIN / "pg_ctl", "-D", data, "-m", "fast", "stop"], capture_output=True)
        shutil.rmtree(base, ignore_errors=True)


@needs_binaries
def test_end_to_end_pooled_logins(cluster, tmp_path):
    base, psql, start_pgbouncer, pooled = cluster
    for stmt in (
        "CREATE ROLE listed LOGIN PASSWORD 'listedpw'",
        "CREATE ROLE ceq LOGIN PASSWORD 'ceqpw'",
        "CREATE ROLE ops_super LOGIN SUPERUSER PASSWORD 'superpw'",
        "CREATE ROLE pgbouncer_admin LOGIN PASSWORD 'adminpw'",
        "CREATE ROLE expired_svc LOGIN PASSWORD 'exppw' VALID UNTIL '2000-01-01'",
        "CREATE DATABASE ceq OWNER ceq",
    ):
        psql(stmt)
    assert psql("SELECT left(rolpassword, 14) FROM pg_authid WHERE rolname = 'ceq'", check=True).returncode == 0

    # Before the migration: the manifest's ini on a pod that has no auth line
    # (a pod of the previous ReplicaSet restarting mid-rollout). Listed roles work.
    legacy = '"listed" "listedpw"\n"pgbouncer_admin" "adminpw"\n'
    start_pgbouncer(legacy)
    assert pooled("listed", "listedpw").returncode == 0
    assert pooled("ceq", "ceqpw", "ceq").returncode != 0

    # The real migration, as the wrapper feeds it.
    proc = psql("", stdin=f"\\set auth_password {FAKE_PW}\n" + MIGRATE_SQL.read_text())
    assert proc.returncode == 0, proc.stderr
    assert FAKE_PW not in proc.stdout + proc.stderr
    # Idempotent.
    assert psql("", stdin=f"\\set auth_password {FAKE_PW}\n" + MIGRATE_SQL.read_text()).returncode == 0

    # The userlist exactly as the init container builds it.
    proc, built, _ = _run_init(tmp_path, FAKE_PW, legacy)
    assert proc.returncode == 0, proc.stderr
    start_pgbouncer(built)
    assert pooled("listed", "listedpw").returncode == 0, "a userlist line still wins"
    ok = pooled("ceq", "ceqpw", "ceq")
    assert ok.returncode == 0 and ok.stdout.strip() == "ceq", ok.stderr
    assert pooled("ceq", "wrong", "ceq").returncode != 0
    assert pooled("ops_super", "superpw").returncode != 0, "get_auth never returns a superuser"
    assert pooled("expired_svc", "exppw").returncode != 0
    assert pooled("pgbouncer_auth", FAKE_PW).returncode == 0, "the auth line itself"

    # The admin console: closed without the optional admin Secret (the
    # hand-kept list has no admin line), open with it (2026-10-22).
    proc, no_admin, _ = _run_init(tmp_path / "no-admin", FAKE_PW, '"listed" "listedpw"\n')
    assert proc.returncode == 0, proc.stderr
    start_pgbouncer(no_admin)
    assert pooled("pgbouncer_admin", FAKE_ADMIN_PW, "pgbouncer", "SHOW USERS").returncode != 0
    proc, with_admin, _ = _run_init(tmp_path / "admin", FAKE_PW, '"listed" "listedpw"\n', admin=FAKE_ADMIN_PW)
    assert proc.returncode == 0, proc.stderr
    start_pgbouncer(with_admin)
    console = pooled("pgbouncer_admin", FAKE_ADMIN_PW, "pgbouncer", "SHOW USERS")
    assert console.returncode == 0, console.stderr
    assert "pgbouncer_admin" in console.stdout
    assert pooled("pgbouncer_admin", "wrong", "pgbouncer", "SHOW USERS").returncode != 0
    assert pooled("listed", "listedpw").returncode == 0
    assert pooled("ceq", "ceqpw", "ceq").returncode == 0, "auth_query unaffected by the admin line"
    start_pgbouncer(built)

    # Nobody but pgbouncer_auth can call the lookup.
    denied = psql("SET ROLE listed; SELECT * FROM pgbouncer.get_auth('ceq')", check=False)
    assert denied.returncode != 0 and "permission denied" in denied.stderr

    # Rollback with the config still on: unlisted roles fail again, listed ones do not.
    assert psql("", stdin=ROLLBACK_SQL.read_text()).returncode == 0
    assert psql("", stdin=ROLLBACK_SQL.read_text()).returncode == 0, "idempotent"
    start_pgbouncer(built)
    assert pooled("listed", "listedpw").returncode == 0
    assert pooled("ceq", "ceqpw", "ceq").returncode != 0
