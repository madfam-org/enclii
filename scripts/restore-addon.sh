#!/usr/bin/env bash
#
# restore-addon.sh — restore ONE managed-database addon from its own backup
# into a scratch cluster, and prove the restore by counting rows.
#
# WHY THIS EXISTS
# ===============
# The estate could restore the SHARED platform Postgres (scripts/dr-drill.sh,
# scripts/backup-restore-drill.sh) and could restore nothing per addon. Every
# addon has had a barman ScheduledBackup since the 2026-08-17 audit, and not
# one of those backups had ever been restored, so "we back up every client
# database" was a statement about a CronJob, not about recoverable data. A
# backup nobody has restored is a hypothesis.
#
# WHAT IT PROVES, AND WHAT IT DOES NOT
# ====================================
# It proves ONE thing, precisely: the named backup of the named addon can be
# recovered into a fresh CloudNativePG cluster, and the restored database
# contains the same number of rows as the source did at the moment the drill
# started.
#
# It does NOT prove, claim, or print a recovery-point or recovery-time figure,
# an availability number, or that point-in-time recovery works. Those are
# separate claims requiring separate evidence, and this repository does not
# make them. `elapsed_s` below is the wall clock of THIS drill on THIS cluster
# under THIS load — it is a drill duration, not a commitment, and the emitted
# row says so by not naming it anything else.
#
# A row-count match is also not a content match: a restore can preserve counts
# and lose column values. It is the strongest cheap assertion available and it
# is what the drill row records, no more.
#
# SAFETY
# ======
#   * The restore target must be a scratch namespace (default prefix
#     `enclii-restore-`). Any other target is refused unless
#     --i-understand-production is passed, deliberately, by a human.
#   * Restoring ONTO the source cluster is refused unconditionally — the flag
#     does not unlock it. There is no legitimate use of this script that
#     overwrites the database it is verifying.
#   * The source is only ever READ: one row-count query per table, no writes,
#     no schema access beyond count(*).
#   * No credential is ever handled by this script. Row counts run INSIDE the
#     database pods over the local socket via `kubectl exec`, so no password,
#     DSN or connection URI is written to argv, an environment variable, the
#     shell history, or this script's output. `psql` is never invoked on the
#     operator's machine; if it were, the connection string would be the leak.
#
# USAGE
#   scripts/restore-addon.sh --namespace project-abc12345 --cluster pg-map-abc12345 \
#       [--addon map-db] [--backup <name>] [--scratch-namespace enclii-restore-map] \
#       [--table public.orders] [--operator name] [--keep] [--dry-run] \
#       [--i-understand-production]
#
# EXIT CODES
#    0  restore verified, counts match
#   10  preflight failed (missing tool, missing source, refused target)
#   20  no usable backup found
#   30  restore failed or never became ready
#   40  verification failed (counts differ, or a count could not be taken)
#
# The single line beginning `DR-LOG-ROW` on stdout is the drill's output of
# record. It is written to be cited verbatim in the private drill log; nothing
# else on stdout is contractual.

set -euo pipefail

SCRIPT_NAME="$(basename "${BASH_SOURCE[0]}")"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
NAMESPACE=""
CLUSTER=""
ADDON=""
BACKUP=""
SCRATCH_NS=""
TABLE=""
OPERATOR="${USER:-unknown}"
KEEP=0
DRY_RUN=0
ALLOW_PRODUCTION=0

# A target namespace is scratch when it starts with this. Everything else is
# treated as production until a human says otherwise.
SCRATCH_PREFIX="${ENCLII_RESTORE_SCRATCH_PREFIX:-enclii-restore-}"

# How long to wait for the recovered cluster to report healthy.
READY_TIMEOUT="${ENCLII_RESTORE_READY_TIMEOUT:-900}"

# The Secret CNPG reads barman credentials from, replicated into each addon
# namespace at provision time (addons/types.go BackupCredentialsSecretName).
BACKUP_CREDENTIALS_SECRET="${ENCLII_ADDON_BACKUP_CREDENTIALS_SECRET:-enclii-db-backup-credentials}"

KUBECTL="${KUBECTL:-kubectl}"

# ---------------------------------------------------------------------------
# Output helpers. Everything diagnostic goes to stderr so stdout carries the
# drill row and nothing else a log could mistake for it.
# ---------------------------------------------------------------------------
log()  { printf '%s\n' "$*" >&2; }
step() { printf '\n== %s\n' "$*" >&2; }
die()  { local code="$1"; shift; printf 'error: %s\n' "$*" >&2; exit "$code"; }

usage() {
  sed -n '2,60p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)              NAMESPACE="${2:?--namespace needs a value}"; shift 2 ;;
    --cluster)                CLUSTER="${2:?--cluster needs a value}"; shift 2 ;;
    --addon)                  ADDON="${2:?--addon needs a value}"; shift 2 ;;
    --backup)                 BACKUP="${2:?--backup needs a value}"; shift 2 ;;
    --scratch-namespace)      SCRATCH_NS="${2:?--scratch-namespace needs a value}"; shift 2 ;;
    --table)                  TABLE="${2:?--table needs a value}"; shift 2 ;;
    --operator)               OPERATOR="${2:?--operator needs a value}"; shift 2 ;;
    --keep)                   KEEP=1; shift ;;
    --dry-run)                DRY_RUN=1; shift ;;
    --i-understand-production) ALLOW_PRODUCTION=1; shift ;;
    -h|--help)                usage; exit 0 ;;
    *) die 10 "unknown argument: $1 (try --help)" ;;
  esac
done

[[ -n "$NAMESPACE" ]] || die 10 "--namespace is required (the addon's project namespace)"
[[ -n "$CLUSTER"   ]] || die 10 "--cluster is required (the CloudNativePG Cluster name)"
[[ -n "$ADDON"     ]] || ADDON="$CLUSTER"
[[ -n "$SCRATCH_NS" ]] || SCRATCH_NS="${SCRATCH_PREFIX}${CLUSTER}"

# The recovered cluster never carries the source's name, so a copy-pasted
# kubectl against the wrong context cannot address the wrong database.
RESTORED_CLUSTER="${CLUSTER}-restore"

# ---------------------------------------------------------------------------
# Target guard. This runs before anything is read, created or connected to.
# ---------------------------------------------------------------------------
step "Target guard"

if [[ "$SCRATCH_NS" == "$NAMESPACE" ]]; then
  die 10 "refusing to restore into the source namespace ($NAMESPACE). A restore drill that writes into the namespace it is verifying is not a drill."
fi

if [[ "$SCRATCH_NS" == "$NAMESPACE" && "$RESTORED_CLUSTER" == "$CLUSTER" ]]; then
  die 10 "refusing to restore onto the source cluster"
fi

if [[ "$SCRATCH_NS" != "$SCRATCH_PREFIX"* ]]; then
  if [[ "$ALLOW_PRODUCTION" -ne 1 ]]; then
    die 10 "target namespace '$SCRATCH_NS' is not a scratch namespace (expected prefix '$SCRATCH_PREFIX').
Restoring into a namespace that holds real workloads can overwrite live data.
If that is genuinely what you intend, re-run with --i-understand-production."
  fi
  log "WARNING: --i-understand-production was given; restoring into NON-SCRATCH namespace '$SCRATCH_NS'."
  log "WARNING: any existing cluster named '$RESTORED_CLUSTER' in that namespace will be replaced."
fi

log "source      namespace=$NAMESPACE cluster=$CLUSTER addon=$ADDON"
log "restore into namespace=$SCRATCH_NS cluster=$RESTORED_CLUSTER"

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
step "Preflight"

command -v "$KUBECTL" >/dev/null 2>&1 || die 10 "$KUBECTL not found on PATH"

if ! "$KUBECTL" get cluster.postgresql.cnpg.io "$CLUSTER" -n "$NAMESPACE" >/dev/null 2>&1; then
  die 10 "no CloudNativePG Cluster '$CLUSTER' in namespace '$NAMESPACE'"
fi
log "source cluster found"

# ---------------------------------------------------------------------------
# Pick the backup
# ---------------------------------------------------------------------------
step "Selecting backup"

if [[ -z "$BACKUP" ]]; then
  # Newest completed Backup for this cluster. `--sort-by` on the creation
  # timestamp and take the last line: a Backup object that is still running
  # has no restorable artefact behind it, so phase is filtered first.
  BACKUP="$(
    "$KUBECTL" get backup.postgresql.cnpg.io -n "$NAMESPACE" \
      --sort-by=.metadata.creationTimestamp \
      -o jsonpath='{range .items[?(@.status.phase=="completed")]}{.metadata.name}{" "}{.spec.cluster.name}{"\n"}{end}' 2>/dev/null \
      | awk -v c="$CLUSTER" '$2 == c { last = $1 } END { if (last != "") print last }'
  )" || true
fi

[[ -n "$BACKUP" ]] || die 20 "no completed Backup found for cluster '$CLUSTER' in '$NAMESPACE'.
A cluster with no completed backup has nothing to restore — that is what the
AddonBackupNeverCompleted alert reports, and it is a real finding, not a
script failure."

log "backup: $BACKUP"

BACKUP_STARTED="$("$KUBECTL" get backup.postgresql.cnpg.io "$BACKUP" -n "$NAMESPACE" \
  -o jsonpath='{.status.startedAt}' 2>/dev/null || true)"

# ---------------------------------------------------------------------------
# Row count helper.
#
# Runs INSIDE the database pod over the local socket. No password, DSN or URI
# is ever assembled here, so there is nothing for argv, the environment or the
# shell history to leak. `psql` on the operator's machine would need a
# connection string; that is precisely why it is not used.
# ---------------------------------------------------------------------------
count_rows() {
  local ns="$1" cluster="$2" query="$3"
  "$KUBECTL" exec -n "$ns" "${cluster}-1" -c postgres -- \
    psql -U postgres -d app -tAc "$query" 2>/dev/null | tr -d '[:space:]'
}

# The count query. With no --table, count every row in every ordinary table of
# the public schema — a whole-database figure that does not depend on the
# operator knowing the tenant's schema. With --table, count that table only.
if [[ -n "$TABLE" ]]; then
  COUNT_QUERY="SELECT count(*) FROM ${TABLE};"
  COUNT_SCOPE="$TABLE"
else
  COUNT_QUERY="SELECT coalesce(sum(c.reltuples_exact),0)::bigint FROM (SELECT (xpath('/row/c/text()', query_to_xml(format('SELECT count(*) AS c FROM %I.%I', n.nspname, t.relname), false, true, '')))[1]::text::bigint AS reltuples_exact FROM pg_class t JOIN pg_namespace n ON n.oid = t.relnamespace WHERE t.relkind = 'r' AND n.nspname = 'public') c;"
  COUNT_SCOPE="public.*"
fi

# ---------------------------------------------------------------------------
# Dry run stops here, having done every check that does not mutate anything.
# ---------------------------------------------------------------------------
if [[ "$DRY_RUN" -eq 1 ]]; then
  step "Dry run"
  log "would count rows in $NAMESPACE/$CLUSTER over $COUNT_SCOPE"
  log "would create namespace $SCRATCH_NS and Cluster $RESTORED_CLUSTER recovering from backup $BACKUP"
  log "would count rows in the restored cluster and compare"
  [[ "$KEEP" -eq 1 ]] && log "would keep the scratch namespace" || log "would delete namespace $SCRATCH_NS"
  log "dry run complete; nothing was created, deleted or written"
  exit 0
fi

DRILL_START_EPOCH="$(date -u +%s)"
RUN_TS="$(date -u +%Y-%m-%dT%H:%MZ)"

# ---------------------------------------------------------------------------
# Count the source FIRST. Taken before the restore so the number the restore
# is judged against is the state the backup should represent, not a moving
# target that drifted while the restore ran.
# ---------------------------------------------------------------------------
step "Counting source rows"
SOURCE_ROWS="$(count_rows "$NAMESPACE" "$CLUSTER" "$COUNT_QUERY" || true)"
[[ "$SOURCE_ROWS" =~ ^[0-9]+$ ]] || die 40 "could not count rows in the source cluster (got: '${SOURCE_ROWS}')"
log "source rows ($COUNT_SCOPE): $SOURCE_ROWS"

# ---------------------------------------------------------------------------
# Scratch namespace + credentials
# ---------------------------------------------------------------------------
step "Preparing scratch namespace"

cleanup() {
  local rc=$?
  if [[ "$KEEP" -eq 1 ]]; then
    log "--keep set; leaving namespace $SCRATCH_NS in place. Delete it with: $KUBECTL delete namespace $SCRATCH_NS"
  elif [[ "$SCRATCH_NS" == "$SCRATCH_PREFIX"* ]]; then
    log "tearing down $SCRATCH_NS"
    "$KUBECTL" delete namespace "$SCRATCH_NS" --wait=false >/dev/null 2>&1 || true
  else
    # A non-scratch namespace was targeted deliberately; deleting it here
    # would destroy whatever else lives in it.
    log "target namespace $SCRATCH_NS is not scratch; leaving it alone. Remove Cluster $RESTORED_CLUSTER by hand when finished."
  fi
  exit "$rc"
}
trap cleanup EXIT

"$KUBECTL" create namespace "$SCRATCH_NS" >/dev/null 2>&1 || true

# CNPG resolves barman credentials in the cluster's OWN namespace, so the
# credential Secret has to exist beside the restored cluster. Copied, never
# printed: the value passes from kubectl to kubectl and is not expanded into a
# shell variable.
if ! "$KUBECTL" get secret "$BACKUP_CREDENTIALS_SECRET" -n "$SCRATCH_NS" >/dev/null 2>&1; then
  "$KUBECTL" get secret "$BACKUP_CREDENTIALS_SECRET" -n "$NAMESPACE" -o yaml \
    | sed -e "s/^  namespace: .*/  namespace: ${SCRATCH_NS}/" \
          -e '/^  resourceVersion:/d' -e '/^  uid:/d' -e '/^  creationTimestamp:/d' \
          -e '/^  selfLink:/d' \
    | "$KUBECTL" apply -n "$SCRATCH_NS" -f - >/dev/null \
    || die 30 "could not replicate the backup credentials Secret into $SCRATCH_NS"
fi

# ---------------------------------------------------------------------------
# The recovery Cluster
# ---------------------------------------------------------------------------
step "Creating recovery cluster"

SOURCE_DEST_PATH="$("$KUBECTL" get cluster.postgresql.cnpg.io "$CLUSTER" -n "$NAMESPACE" \
  -o jsonpath='{.spec.backup.barmanObjectStore.destinationPath}' 2>/dev/null || true)"
SOURCE_ENDPOINT="$("$KUBECTL" get cluster.postgresql.cnpg.io "$CLUSTER" -n "$NAMESPACE" \
  -o jsonpath='{.spec.backup.barmanObjectStore.endpointURL}' 2>/dev/null || true)"
SOURCE_IMAGE="$("$KUBECTL" get cluster.postgresql.cnpg.io "$CLUSTER" -n "$NAMESPACE" \
  -o jsonpath='{.spec.imageName}' 2>/dev/null || true)"

[[ -n "$SOURCE_DEST_PATH" ]] || die 20 "source cluster has no barmanObjectStore.destinationPath — it was provisioned without backups, so there is nothing to restore"

# `externalClusters` + `bootstrap.recovery` is the cross-namespace shape: a
# `recovery.backup` reference only resolves to a Backup object in the SAME
# namespace, and the whole point here is to land somewhere else. The image is
# copied from the source because recovering onto a different major version
# does not work and failing at `kubectl apply` beats failing halfway through a
# restore.
RECOVERY_MANIFEST="$(cat <<YAML
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: ${RESTORED_CLUSTER}
  namespace: ${SCRATCH_NS}
  labels:
    app.kubernetes.io/part-of: enclii
    enclii.dev/restore-drill: "true"
    enclii.dev/source-cluster: ${CLUSTER}
spec:
  instances: 1
$( [[ -n "$SOURCE_IMAGE" ]] && printf '  imageName: %s\n' "$SOURCE_IMAGE" )
  storage:
    size: 10Gi
  bootstrap:
    recovery:
      source: ${CLUSTER}
  externalClusters:
    - name: ${CLUSTER}
      barmanObjectStore:
        destinationPath: ${SOURCE_DEST_PATH}
$( [[ -n "$SOURCE_ENDPOINT" ]] && printf '        endpointURL: %s\n' "$SOURCE_ENDPOINT" )
        serverName: ${CLUSTER}
        s3Credentials:
          accessKeyId:
            name: ${BACKUP_CREDENTIALS_SECRET}
            key: ACCESS_KEY_ID
          secretAccessKey:
            name: ${BACKUP_CREDENTIALS_SECRET}
            key: SECRET_ACCESS_KEY
YAML
)"

printf '%s\n' "$RECOVERY_MANIFEST" | "$KUBECTL" apply -f - >/dev/null \
  || die 30 "could not create the recovery cluster"

step "Waiting for the recovery to finish (timeout ${READY_TIMEOUT}s)"
deadline=$(( $(date -u +%s) + READY_TIMEOUT ))
ready=0
while [[ "$(date -u +%s)" -lt "$deadline" ]]; do
  ready_instances="$("$KUBECTL" get cluster.postgresql.cnpg.io "$RESTORED_CLUSTER" -n "$SCRATCH_NS" \
    -o jsonpath='{.status.readyInstances}' 2>/dev/null || true)"
  if [[ "$ready_instances" =~ ^[0-9]+$ ]] && [[ "$ready_instances" -ge 1 ]]; then
    ready=1
    break
  fi
  sleep 10
done
[[ "$ready" -eq 1 ]] || die 30 "recovery cluster never reported a ready instance within ${READY_TIMEOUT}s"
log "recovery cluster ready"

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
step "Counting restored rows"
RESTORED_ROWS="$(count_rows "$SCRATCH_NS" "$RESTORED_CLUSTER" "$COUNT_QUERY" || true)"
[[ "$RESTORED_ROWS" =~ ^[0-9]+$ ]] || die 40 "could not count rows in the restored cluster (got: '${RESTORED_ROWS}')"
log "restored rows ($COUNT_SCOPE): $RESTORED_ROWS"

ELAPSED=$(( $(date -u +%s) - DRILL_START_EPOCH ))

if [[ "$SOURCE_ROWS" == "$RESTORED_ROWS" ]]; then
  VERDICT="match"
  EXIT_CODE=0
else
  VERDICT="MISMATCH"
  EXIT_CODE=40
fi

# ---------------------------------------------------------------------------
# The row of record.
#
# One line, stable field order, `key=value` throughout so it can be pasted into
# the private drill log or parsed without this script being consulted. It
# carries no recovery-point figure, no recovery-time figure and no availability
# number: `elapsed_s` is how long THIS drill took, and is labelled as nothing
# else.
# ---------------------------------------------------------------------------
printf 'DR-LOG-ROW | run_ts=%s | addon=%s | namespace=%s | cluster=%s | backup=%s | backup_started=%s | scope=%s | rows_source=%s | rows_restored=%s | verdict=%s | elapsed_s=%s | operator=%s\n' \
  "$RUN_TS" "$ADDON" "$NAMESPACE" "$CLUSTER" "$BACKUP" "${BACKUP_STARTED:-unknown}" \
  "$COUNT_SCOPE" "$SOURCE_ROWS" "$RESTORED_ROWS" "$VERDICT" "$ELAPSED" "$OPERATOR"

if [[ "$EXIT_CODE" -ne 0 ]]; then
  log "VERIFICATION FAILED: the restored database does not hold the same number of rows as the source."
  log "Note that a row-count match would not have proven content equality either; a mismatch proves inequality."
fi

exit "$EXIT_CODE"
