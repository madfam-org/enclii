#!/usr/bin/env bash
# pgbouncer-auth-query.sh — owner-run steps for the shared pgbouncer's
# auth_query (infra/k8s/platform-infra/pgbouncer.yaml).
#
# Usage:
#   scripts/pgbouncer-auth-query.sh precheck          # read-only
#   scripts/pgbouncer-auth-query.sh migrate           # superuser DDL
#   scripts/pgbouncer-auth-query.sh verify [ROLE]     # read-only; ROLE defaults to ceq
#   scripts/pgbouncer-auth-query.sh probe             # creates and drops a throwaway role
#   scripts/pgbouncer-auth-query.sh rollback          # superuser DDL
#
# Every step runs on the cluster host through `ssh ssh.madfam.io` and
# `sudo -n kubectl`. SQL goes to psql inside the postgres pod over the pod's
# local socket, so no connection string is assembled anywhere.
#
#   precheck  Postgres version, password_encryption, how login roles store
#             their passwords (SCRAM, md5, none), pg_hba methods, and whether
#             the auth_query objects and the pgbouncer-auth Secret exist.
#   migrate   Reads pgbouncer_auth's password from the pgbouncer-auth Secret
#             ON THE HOST and runs scripts/sql/pgbouncer-auth-query.sql with it
#             as a psql variable on stdin. The value is never printed, never an
#             argument, never on this machine.
#   verify    Role and function state, what get_auth returns for ROLE (the
#             verifier type only), each pooler pod's userlist head (user name
#             only), "no such user" log lines in 10 minutes, and SHOW USERS on
#             the admin console. The console check needs a pgbouncer_admin
#             line in the pod's userlist (intake target data/pgbouncer-admin,
#             then a pgbouncer restart); without one it prints SKIPPED with
#             that owner step instead of failing, and verify still exits 0.
#   probe     End-to-end proof of the auth_query path: creates the login role
#             pgbouncer_auth_probe with a random password made on the host,
#             logs in through each pooler pod on 127.0.0.1:6432 with
#             require_auth=scram-sha-256, and drops the role (also on failure).
#   rollback  Runs scripts/sql/pgbouncer-auth-query-rollback.sql. Refuses while
#             the pgbouncer-config ConfigMap still sets auth_query: revert the
#             config first.
#
# Environment: SSH_HOST (default ssh.madfam.io). PGB_AUTH_SSH replaces the ssh
# command (tests use it); it is called as `$PGB_AUTH_SSH "$SSH_HOST" bash -s`.
set -euo pipefail

SSH_HOST="${SSH_HOST:-ssh.madfam.io}"
PGB_AUTH_SSH="${PGB_AUTH_SSH:-ssh}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATE_SQL="$HERE/sql/pgbouncer-auth-query.sql"
ROLLBACK_SQL="$HERE/sql/pgbouncer-auth-query-rollback.sql"
SQL_EOF="__PGBOUNCER_AUTH_QUERY_SQL__"

die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 2
}

# Remote prelude shared by every step: strict mode, the kubectl wrapper and
# the psql call into the postgres pod.
read -r -d '' PRELUDE <<'REMOTE' || true
set -euo pipefail
set +x
K() { sudo -n kubectl "$@"; }
pg() { K -n data exec -i deploy/postgres -c postgres -- psql -X -q -v ON_ERROR_STOP=1 -U postgres -d postgres "$@"; }
pooler_pods() { K -n data get pods -l app=pgbouncer --field-selector=status.phase=Running -o 'jsonpath={range .items[*]}{.metadata.name}{"\n"}{end}'; }
REMOTE

# embed_sql FILE prints FILE as the body of a quoted heredoc for the remote
# program, refusing a file that contains the terminator line.
embed_sql() {
  local file="$1"
  [ -r "$file" ] || die "cannot read $file"
  if grep -qx "$SQL_EOF" "$file"; then
    die "$file contains the heredoc terminator $SQL_EOF"
  fi
  printf "cat <<'%s'\n" "$SQL_EOF"
  cat "$file"
  printf '%s\n' "$SQL_EOF"
}

run_remote() {
  # The program arrives on stdin, so nothing in it is an ssh argument.
  "$PGB_AUTH_SSH" "$SSH_HOST" bash -s
}

step_precheck() {
  {
    printf '%s\n' "$PRELUDE"
    cat <<'REMOTE'
echo "== Postgres"
pg -P footer=off <<'SQL'
SELECT current_setting('server_version') AS server_version,
       current_setting('password_encryption') AS password_encryption;
SELECT CASE WHEN rolpassword LIKE 'SCRAM-SHA-256$%' THEN 'scram-sha-256'
            WHEN rolpassword LIKE 'md5%' THEN 'md5'
            WHEN rolpassword IS NULL THEN 'none'
            ELSE 'other' END AS stored_password,
       count(*) AS login_roles
  FROM pg_authid WHERE rolcanlogin GROUP BY 1 ORDER BY 1;
SELECT rolname AS md5_or_other_login_role
  FROM pg_authid
 WHERE rolcanlogin AND rolpassword IS NOT NULL AND rolpassword NOT LIKE 'SCRAM-SHA-256$%'
 ORDER BY 1;
SELECT type, auth_method, count(*) AS rules FROM pg_hba_file_rules GROUP BY 1, 2 ORDER BY 1, 2;
SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pgbouncer_auth') AS auth_role_exists,
       to_regprocedure('pgbouncer.get_auth(text)') IS NOT NULL AS auth_function_exists;
SQL
echo "== Cluster objects (names only)"
K -n data get externalsecret pgbouncer-auth -o 'custom-columns=EXTERNALSECRET:.metadata.name,READY:.status.conditions[0].status,REASON:.status.conditions[0].reason' 2>&1 || true
if K -n data get secret pgbouncer-auth -o name >/dev/null 2>&1; then
  echo "secret/pgbouncer-auth exists"
else
  echo "secret/pgbouncer-auth is missing (run: enclii secrets intake submit data/pgbouncer-auth --generate pgbouncer_auth_password --reason ...)"
fi
if K -n data get secret pgbouncer-admin -o name >/dev/null 2>&1; then
  echo "secret/pgbouncer-admin exists"
else
  echo "secret/pgbouncer-admin is missing: the admin console stays closed (optional; run: enclii secrets intake submit data/pgbouncer-admin --generate pgbouncer_admin_password --reason ..., then restart pgbouncer)"
fi
if K -n data get configmap pgbouncer-config -o 'jsonpath={.data.pgbouncer\.ini}' | grep -Eq '^[[:space:]]*auth_query[[:space:]]*='; then
  echo "pgbouncer-config sets auth_query"
else
  echo "pgbouncer-config does not set auth_query"
fi
REMOTE
  } | run_remote
}

step_migrate() {
  {
    printf '%s\n' "$PRELUDE"
    cat <<'REMOTE'
pw="$(K -n data get secret pgbouncer-auth -o 'jsonpath={.data.pgbouncer_auth_password}' | base64 -d)"
if [ -z "$pw" ]; then
  echo "[FAIL] Secret data/pgbouncer-auth has no pgbouncer_auth_password. Run the intake first." >&2
  exit 1
fi
case "$pw" in
  *[!A-Za-z0-9_-]*) echo "[FAIL] the password is not base64url (intake --generate mints base64url); refusing" >&2; exit 1 ;;
esac
if [ "${#pw}" -lt 32 ]; then
  echo "[FAIL] the password is shorter than 32 characters; refusing" >&2
  exit 1
fi
{
  printf '\\set auth_password %s\n' "$pw"
REMOTE
    embed_sql "$MIGRATE_SQL"
    cat <<'REMOTE'
} | pg
unset pw
echo "migrate: done. Next: scripts/pgbouncer-auth-query.sh verify, then probe."
REMOTE
  } | run_remote
}

step_verify() {
  local role="${1:-ceq}"
  [[ "$role" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] || die "role name must be a plain lowercase identifier: $role"
  {
    printf '%s\n' "$PRELUDE"
    printf 'probe_role=%s\n' "$role"
    cat <<'REMOTE'
echo "== Postgres: auth role and lookup function"
{
  printf '\\set probe_role %s\n' "$probe_role"
  cat <<'SQL'
\pset footer off
SELECT r.rolname, r.rolcanlogin AS can_login, r.rolsuper AS superuser, r.rolconnlimit AS conn_limit,
       p.prosecdef AS security_definer, pg_get_userbyid(p.proowner) AS function_owner,
       has_function_privilege('pgbouncer_auth', p.oid, 'EXECUTE') AS auth_can_execute,
       has_function_privilege('public', p.oid, 'EXECUTE') AS public_can_execute
  FROM pg_roles r CROSS JOIN pg_proc p
 WHERE r.rolname = 'pgbouncer_auth' AND p.oid = 'pgbouncer.get_auth(text)'::regprocedure;
SET ROLE pgbouncer_auth;
SELECT :'probe_role' AS role,
       count(*) AS rows_returned,
       max(CASE WHEN passwd LIKE 'SCRAM-SHA-256$%' THEN 'scram-sha-256'
                WHEN passwd LIKE 'md5%' THEN 'md5' ELSE 'other' END) AS verifier_type
  FROM pgbouncer.get_auth(:'probe_role');
RESET ROLE;
SQL
} | pg
echo "== Pooler pods"
console_skipped=0
for pod in $(pooler_pods); do
  head_user="$(K -n data exec "$pod" -c pgbouncer -- sh -c 'head -n 1 /etc/pgbouncer/userlist.txt | cut -d " " -f 1')"
  no_such_user="$(K -n data logs "$pod" -c pgbouncer --since=10m | grep -c 'no such user' || true)"
  echo "$pod: first userlist entry ${head_user}; 'no such user' lines in 10m: ${no_such_user}"
  # The admin console only answers pgbouncer_admin through a userlist line
  # (auth_query has no backend for it). Ask whether the line exists; the
  # answer is the word present or absent, never the line.
  admin_line="$(K -n data exec "$pod" -c pgbouncer -- sh -c 'if grep -q "^\"pgbouncer_admin\" " /etc/pgbouncer/userlist.txt; then echo present; else echo absent; fi')"
  if [ "$admin_line" != present ]; then
    echo "  admin console: SKIPPED, no pgbouncer_admin line in this pod's userlist"
    console_skipped=$((console_skipped + 1))
    continue
  fi
  # SHOW USERS on the admin console. The admin password is read inside the
  # pod from its own userlist and handed to psql in the environment.
  K -n data exec "$pod" -c pgbouncer -- sh -c '
    PGPASSWORD="$(grep "^\"pgbouncer_admin\"" /etc/pgbouncer/userlist.txt | cut -d "\"" -f 4)"
    export PGPASSWORD
    psql -X -h 127.0.0.1 -p 6432 -U pgbouncer_admin -d pgbouncer -P footer=off -c "SHOW USERS"' | sed 's/^/  /'
done
if [ "$console_skipped" -gt 0 ]; then
  echo "[WARN] admin console check skipped on ${console_skipped} pooler pod(s). Owner step to open it:"
  echo "  enclii secrets intake submit data/pgbouncer-admin --generate pgbouncer_admin_password --reason \"pgbouncer admin console\""
  echo "  ssh ssh.madfam.io 'sudo -n kubectl -n data rollout restart deploy/pgbouncer && sudo -n kubectl -n data rollout status deploy/pgbouncer'"
fi
REMOTE
  } | run_remote
}

step_probe() {
  {
    printf '%s\n' "$PRELUDE"
    cat <<'REMOTE'
probe=pgbouncer_auth_probe
pw="$(head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 40)"
cleanup() { printf 'DROP ROLE IF EXISTS %s;\n' "$probe" | pg || echo "[WARN] could not drop $probe; drop it by hand" >&2; }
trap cleanup EXIT
cleanup   # a probe role left by an interrupted run
{ printf '\\set pw %s\n' "$pw"; printf 'CREATE ROLE %s LOGIN NOINHERIT CONNECTION LIMIT 4 PASSWORD :%s;\n' "$probe" "'pw'"; } | pg
fails=0
for pod in $(pooler_pods); do
  if printf '%s\n' "$pw" | K -n data exec -i "$pod" -c pgbouncer -- sh -c '
      read -r PGPASSWORD; export PGPASSWORD
      psql -X -At "host=127.0.0.1 port=6432 dbname=postgres user=pgbouncer_auth_probe require_auth=scram-sha-256 connect_timeout=10" \
        -c "SELECT '\''pooled SCRAM login ok as '\'' || current_user"'; then
    echo "$pod: OK"
  else
    echo "$pod: FAILED" >&2
    fails=$((fails + 1))
  fi
done
unset pw
[ "$fails" -eq 0 ] || { echo "[FAIL] $fails pooler pod(s) could not authenticate the probe role through auth_query" >&2; exit 1; }
echo "probe: every pooler pod authenticated a role that has no userlist line"
REMOTE
  } | run_remote
}

step_rollback() {
  {
    printf '%s\n' "$PRELUDE"
    cat <<'REMOTE'
if K -n data get configmap pgbouncer-config -o 'jsonpath={.data.pgbouncer\.ini}' | grep -Eq '^[[:space:]]*auth_query[[:space:]]*='; then
  echo "[FAIL] pgbouncer-config still sets auth_query. Revert the config PR and let the rollout finish first; dropping the role now breaks every role without a userlist line." >&2
  exit 1
fi
{
REMOTE
    embed_sql "$ROLLBACK_SQL"
    cat <<'REMOTE'
} | pg
echo "rollback: done."
REMOTE
  } | run_remote
}

case "${1:-}" in
  precheck) step_precheck ;;
  migrate) step_migrate ;;
  verify) shift; step_verify "${1:-ceq}" ;;
  probe) step_probe ;;
  rollback) step_rollback ;;
  *) usage ;;
esac
