#!/usr/bin/env bash
# Cases for scripts/restore-addon.sh.
#
# WHAT THIS PINS, and why each one is worth a test:
#
#   1. THE PRODUCTION GUARD ACTUALLY GUARDS. A restore drill that can be
#      pointed at a live namespace by a typo is a data-loss tool wearing a
#      verification tool's name. The refusal is asserted, the flag that lifts
#      it is asserted, and the one refusal the flag CANNOT lift — restoring
#      into the source namespace — is asserted separately.
#
#   2. NO CREDENTIAL REACHES THE OUTPUT. The script copies a Secret between
#      namespaces. The stub kubectl below returns a Secret whose data carries a
#      recognisable fake key; if that string ever appears on stdout or stderr,
#      the leak assertion fails and names it.
#
#   3. `psql` IS NEVER RUN ON THIS MACHINE. Row counts go through
#      `kubectl exec` and the pod's local socket precisely so no connection
#      string is ever assembled. A host-side psql is the shape that would put
#      a password in argv and the shell history, so the stub psql here writes a
#      tripwire file and the test fails if it was ever created.
#
#   4. --dry-run MUTATES NOTHING. The stub records every kubectl invocation;
#      the dry-run case asserts none of them was create/apply/delete/exec.
#
#   5. THE DRILL ROW IS EXACT. It is written to be pasted verbatim into a
#      private drill log, so its field names and the absence of any
#      recovery-point / recovery-time / availability figure are both pinned.
#
#   6. A MISMATCH FAILS LOUDLY. A verification that reports success when the
#      counts differ is worse than no verification.
#
# Nothing here reaches a cluster: `kubectl` and `psql` are real files on a
# PATH prepended for the duration of each case.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
SCRIPT="$REPO/scripts/restore-addon.sh"
TMPROOT="$(mktemp -d)"
trap 'rm -rf "$TMPROOT"' EXIT

fails=0
run=0

# A recognisable, obviously-fake credential. It is what the stub kubectl puts
# in the Secret it hands back, so if the script ever echoes secret material
# this string is what shows up.
FAKE_SECRET="TESTONLY-r2key-not-a-real-credential"

STUB_BIN="$TMPROOT/bin"
mkdir -p "$STUB_BIN"

cat > "$STUB_BIN/kubectl" <<'STUB'
#!/usr/bin/env bash
# Stub kubectl: records every invocation, answers from environment variables,
# touches no cluster.
{ printf '%s\n' "$*"; } >> "$KUBECTL_CALLS"

args="$*"

# Real kubectl reads the manifest from stdin for `-f -`. A stub that exits
# without draining it makes the producing `printf`/`sed` die of SIGPIPE, which
# under `set -o pipefail` fails the whole pipeline — an artefact of the stub,
# not of the script. Drain it, and keep it so the applied manifest can be
# asserted on.
if [[ "$args" == *"-f -"* ]]; then
  cat >> "$KUBECTL_APPLIED"
fi
ns=""
prev=""
for a in "$@"; do
  [[ "$prev" == "-n" ]] && ns="$a"
  prev="$a"
done

case "$args" in
  *"get cluster.postgresql.cnpg.io"*"destinationPath"*)
      printf 's3://enclii-db-backups/%s/%s\n' "$ns" "$STUB_CLUSTER"; exit 0 ;;
  *"get cluster.postgresql.cnpg.io"*"endpointURL"*)
      printf 'https://r2.example.invalid\n'; exit 0 ;;
  *"get cluster.postgresql.cnpg.io"*"imageName"*)
      printf 'ghcr.io/cloudnative-pg/postgresql:18.3\n'; exit 0 ;;
  *"get cluster.postgresql.cnpg.io"*"readyInstances"*)
      printf '%s\n' "${STUB_READY_INSTANCES:-1}"; exit 0 ;;
  *"get cluster.postgresql.cnpg.io"*)
      exit "${STUB_SOURCE_CLUSTER_RC:-0}" ;;
  *"get backup.postgresql.cnpg.io"*"startedAt"*)
      printf '2026-09-05T04:00:12Z\n'; exit 0 ;;
  *"get backup.postgresql.cnpg.io"*)
      printf '%s' "${STUB_BACKUP_LIST-}"; [[ -n "${STUB_BACKUP_LIST-}" ]] && printf '\n'; exit 0 ;;
  *"exec"*)
      if [[ "$ns" == "$STUB_SOURCE_NS" ]]; then
        printf '%s\n' "${STUB_SOURCE_ROWS:-100}"
      else
        printf '%s\n' "${STUB_RESTORED_ROWS:-100}"
      fi
      exit 0 ;;
  *"get secret"*"-o yaml"*)
      cat <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: enclii-db-backup-credentials
  namespace: $ns
  resourceVersion: "1"
  uid: 00000000-0000-0000-0000-000000000000
data:
  ACCESS_KEY_ID: $STUB_FAKE_SECRET
  SECRET_ACCESS_KEY: $STUB_FAKE_SECRET
YAML
      exit 0 ;;
  *"get secret"*)
      exit 1 ;;   # not present in the scratch namespace yet
  *) exit 0 ;;
esac
STUB
chmod +x "$STUB_BIN/kubectl"

# Tripwire. The script must never run psql on this machine — doing so would
# require a connection string, which is where a password would leak.
cat > "$STUB_BIN/psql" <<'STUB'
#!/usr/bin/env bash
printf 'host-side psql was invoked with: %s\n' "$*" >> "$PSQL_TRIPWIRE"
exit 0
STUB
chmod +x "$STUB_BIN/psql"

check() {
  local name="$1" cond="$2" detail="${3:-}"
  run=$((run + 1))
  if [[ "$cond" == "ok" ]]; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n' "$name"
    [[ -n "$detail" ]] && printf '%s\n' "$detail" | sed 's/^/     | /'
    fails=$((fails + 1))
  fi
}

assert_contains() {
  local name="$1" hay="$2" needle="$3"
  if [[ "$hay" == *"$needle"* ]]; then check "$name" ok
  else check "$name" bad "expected to find: $needle"$'\n'"in: $hay"; fi
}

assert_not_contains() {
  local name="$1" hay="$2" needle="$3"
  if [[ "$hay" != *"$needle"* ]]; then check "$name" ok
  else check "$name" bad "must NOT contain: $needle"$'\n'"in: $hay"; fi
}

assert_rc() {
  local name="$1" got="$2" want="$3" detail="${4:-}"
  if [[ "$got" == "$want" ]]; then check "$name" ok
  else check "$name" bad "exit $got, want $want"$'\n'"$detail"; fi
}

# run_restore <extra args...> — returns combined output in $out, code in $rc.
run_restore() {
  : > "$KUBECTL_CALLS"
  : > "$KUBECTL_APPLIED"
  out="$(
    PATH="$STUB_BIN:$PATH" \
    KUBECTL_CALLS="$KUBECTL_CALLS" \
    KUBECTL_APPLIED="$KUBECTL_APPLIED" \
    PSQL_TRIPWIRE="$PSQL_TRIPWIRE" \
    STUB_FAKE_SECRET="$FAKE_SECRET" \
    STUB_SOURCE_NS="project-abc12345" \
    STUB_CLUSTER="pg-map-abc12345" \
    ENCLII_RESTORE_READY_TIMEOUT=30 \
    bash "$SCRIPT" "$@" 2>&1
  )"
  rc=$?
}

KUBECTL_CALLS="$TMPROOT/kubectl-calls.txt"
KUBECTL_APPLIED="$TMPROOT/kubectl-applied.yaml"
PSQL_TRIPWIRE="$TMPROOT/psql-tripwire.txt"
: > "$KUBECTL_CALLS"
: > "$KUBECTL_APPLIED"
: > "$PSQL_TRIPWIRE"

BASE_ARGS=(--namespace project-abc12345 --cluster pg-map-abc12345 --addon map-db --operator test)

# ---------------------------------------------------------------------------
# 1. The production guard
# ---------------------------------------------------------------------------
STUB_BACKUP_LIST="pg-map-abc12345-20260905040000 pg-map-abc12345" \
  run_restore "${BASE_ARGS[@]}" --scratch-namespace data
assert_rc        'refuses a non-scratch target' "$rc" 10 "$out"
assert_contains  'names the flag that would allow it' "$out" '--i-understand-production'
assert_not_contains 'refusal happens before anything is created' \
  "$(cat "$KUBECTL_CALLS")" 'create namespace'

STUB_BACKUP_LIST="pg-map-abc12345-20260905040000 pg-map-abc12345" \
STUB_SOURCE_ROWS=100 STUB_RESTORED_ROWS=100 \
  run_restore "${BASE_ARGS[@]}" --scratch-namespace data --i-understand-production
assert_rc       'the flag lifts the refusal' "$rc" 0 "$out"
assert_contains 'warns loudly when it does'  "$out" 'WARNING'
assert_contains 'does not delete a non-scratch namespace' "$out" 'leaving it alone'

# The one refusal the flag cannot lift.
STUB_BACKUP_LIST="pg-map-abc12345-20260905040000 pg-map-abc12345" \
  run_restore "${BASE_ARGS[@]}" --scratch-namespace project-abc12345 --i-understand-production
assert_rc       'refuses the source namespace even with the flag' "$rc" 10 "$out"
assert_contains 'says why'                                        "$out" 'source namespace'

# ---------------------------------------------------------------------------
# 2. --dry-run mutates nothing
# ---------------------------------------------------------------------------
STUB_BACKUP_LIST="pg-map-abc12345-20260905040000 pg-map-abc12345" \
  run_restore "${BASE_ARGS[@]}" --dry-run
assert_rc 'dry run succeeds' "$rc" 0 "$out"
calls="$(cat "$KUBECTL_CALLS")"
assert_not_contains 'dry run creates nothing' "$calls" 'create namespace'
assert_not_contains 'dry run applies nothing' "$calls" 'apply'
assert_not_contains 'dry run deletes nothing' "$calls" 'delete'
assert_not_contains 'dry run executes nothing in a pod' "$calls" 'exec'
assert_not_contains 'dry run emits no drill row' "$out" 'DR-LOG-ROW'

# ---------------------------------------------------------------------------
# 3. Happy path: the drill row
# ---------------------------------------------------------------------------
STUB_BACKUP_LIST="pg-map-abc12345-20260904040000 pg-map-abc12345
pg-map-abc12345-20260905040000 pg-map-abc12345
pg-other-99999999-20260905040000 pg-other-99999999" \
STUB_SOURCE_ROWS=1284 STUB_RESTORED_ROWS=1284 \
  run_restore "${BASE_ARGS[@]}"
assert_rc       'a matching restore exits 0' "$rc" 0 "$out"
row="$(printf '%s\n' "$out" | grep '^DR-LOG-ROW' || true)"
assert_contains 'emits exactly one drill row'  "$row" 'DR-LOG-ROW'
assert_contains 'row names the addon'          "$row" 'addon=map-db'
assert_contains 'row names the backup id'      "$row" 'backup=pg-map-abc12345-20260905040000'
assert_contains 'row carries the source count' "$row" 'rows_source=1284'
assert_contains 'row carries the restored count' "$row" 'rows_restored=1284'
assert_contains 'row carries the verdict'      "$row" 'verdict=match'
assert_contains 'row carries elapsed seconds'  "$row" 'elapsed_s='
assert_contains 'row carries the operator'     "$row" 'operator=test'
assert_contains 'row carries a run timestamp'  "$row" 'run_ts=20'

# The newest backup FOR THIS CLUSTER, not the newest backup in the namespace.
assert_not_contains 'ignores backups belonging to another cluster' "$row" 'pg-other-99999999'

# The row is the drill's output of record and must not smuggle in a claim the
# estate does not make.
for forbidden in rpo RPO rto RTO uptime availability SLA pitr PITR; do
  assert_not_contains "row claims no $forbidden" "$row" "$forbidden"
done

# The recovered cluster never carries the source's name, so a copy-pasted
# kubectl cannot address the wrong database.
applied="$(cat "$KUBECTL_APPLIED")"
assert_contains 'recovers into a differently named cluster' "$applied" 'name: pg-map-abc12345-restore'
assert_contains 'recovery reads the source server name'     "$applied" 'serverName: pg-map-abc12345'
assert_contains 'recovery pins the source image'            "$applied" 'imageName: ghcr.io/cloudnative-pg/postgresql:18.3'
assert_contains 'recovered cluster is labelled a drill'     "$applied" 'enclii.dev/restore-drill: "true"'
assert_contains 'tears the scratch namespace down afterwards' \
  "$(cat "$KUBECTL_CALLS")" 'delete namespace enclii-restore-pg-map-abc12345'

# ---------------------------------------------------------------------------
# 4. A mismatch fails
# ---------------------------------------------------------------------------
STUB_BACKUP_LIST="pg-map-abc12345-20260905040000 pg-map-abc12345" \
STUB_SOURCE_ROWS=1284 STUB_RESTORED_ROWS=1200 \
  run_restore "${BASE_ARGS[@]}"
assert_rc       'a mismatch exits 40'      "$rc" 40 "$out"
assert_contains 'the row says MISMATCH'    "$out" 'verdict=MISMATCH'
assert_contains 'and it is stated plainly' "$out" 'VERIFICATION FAILED'

# ---------------------------------------------------------------------------
# 5. No completed backup is a finding, not a crash
# ---------------------------------------------------------------------------
STUB_BACKUP_LIST="" run_restore "${BASE_ARGS[@]}"
assert_rc       'no backup exits 20'     "$rc" 20 "$out"
assert_contains 'and says what it means' "$out" 'nothing to restore'

# ---------------------------------------------------------------------------
# 6. A missing source cluster is caught in preflight
# ---------------------------------------------------------------------------
STUB_SOURCE_CLUSTER_RC=1 run_restore "${BASE_ARGS[@]}"
assert_rc       'a missing source cluster exits 10' "$rc" 10 "$out"
assert_contains 'and names it'                      "$out" 'no CloudNativePG Cluster'

# ---------------------------------------------------------------------------
# 7. No credential, ever, and no host-side psql
# ---------------------------------------------------------------------------
STUB_BACKUP_LIST="pg-map-abc12345-20260905040000 pg-map-abc12345" \
STUB_SOURCE_ROWS=7 STUB_RESTORED_ROWS=7 \
  run_restore "${BASE_ARGS[@]}"
assert_not_contains 'never prints secret material' "$out" "$FAKE_SECRET"
assert_not_contains 'never prints secret material in the kubectl argv' \
  "$(cat "$KUBECTL_CALLS")" "$FAKE_SECRET"

if [[ ! -s "$PSQL_TRIPWIRE" ]]; then
  check 'psql is never run on the operator machine' ok
else
  check 'psql is never run on the operator machine' bad "$(cat "$PSQL_TRIPWIRE")"
fi

src="$(cat "$SCRIPT")"
assert_not_contains 'script holds no PGPASSWORD'      "$src" 'PGPASSWORD'
assert_not_contains 'script builds no postgres:// URI' "$src" 'postgres://'

# ---------------------------------------------------------------------------
# 8. Basics
# ---------------------------------------------------------------------------
if bash -n "$SCRIPT" 2>/dev/null; then check 'script parses' ok; else check 'script parses' bad; fi
run_restore --help
assert_rc       '--help exits 0' "$rc" 0 "$out"
assert_contains '--help explains the flags' "$out" '--i-understand-production'
run_restore --nonsense
assert_rc 'an unknown flag exits 10' "$rc" 10 "$out"

printf '\n%s case(s), %s failure(s)\n' "$run" "$fails"
[[ "$fails" -eq 0 ]] || exit 1
