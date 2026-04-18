#!/usr/bin/env bash
# dr-drill.sh - Disaster Recovery drill: end-to-end Postgres restore from R2 to ephemeral namespace.
#
# This is the P0.1 DR automation. It answers the single audit question: "can we
# actually restore production Postgres from R2 today, and how long does it take?"
#
# The script is SAFE by construction:
#   - Only operates in the `dr-test` namespace (never `enclii`, `data`, or any prod ns).
#   - Never writes to the production R2 bucket (only lists + downloads).
#   - Never touches production Postgres, PVCs, or services.
#   - Tears down `dr-test` at the end unless --keep is passed.
#
# Flow:
#   1. Preflight (kubectl, secrets, connectivity).
#   2. (Re)create `dr-test` namespace.
#   3. Copy R2 + Postgres credentials from `enclii`/`data` into `dr-test`.
#   4. Apply ephemeral Postgres 16 manifest, wait for readiness.
#   5. Identify latest backup object in R2 via a throwaway aws-cli pod.
#   6. Download backup into the Postgres pod's /tmp.
#   7. Run pg_restore / psql (dump format auto-detected).
#   8. Run sanity SELECTs against real tables, capturing counts + MAX(created_at).
#   9. Compute phase timings, emit structured JSON + human log.
#  10. Append row to internal-devops/runbooks/dr-log.md.
#  11. Delete dr-test namespace (unless --keep).
#
# Usage:
#   ./scripts/dr-drill.sh [--dry-run] [--keep] [--operator <name>] [--backup-key <key>]
#
# Exit codes:
#    0 = drill passed
#   10 = preflight failed
#   20 = R2 download failed
#   30 = restore failed
#   40 = sanity query failed
#   50 = teardown failed

set -euo pipefail

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
MANIFEST="${REPO_ROOT}/infra/k8s/dr-test/ephemeral-postgres.yaml"

DR_NAMESPACE="dr-test"
DR_POSTGRES_DEPLOY="dr-postgres"
DR_DB_NAME="enclii_dr"
DR_DB_USER="drpg"
# Ephemeral credential, regenerated each run, scoped only to the dr-test pod
# that tears down at the end of the script. Not a secret — that is the point.
DR_DB_PASSWORD="drpg-ephem-$(date +%s)-$$"

R2_BUCKET="${R2_BUCKET:-enclii-backups}"
R2_PREFIX="${R2_PREFIX:-postgres}"
SOURCE_R2_SECRET_NS="${SOURCE_R2_SECRET_NS:-enclii}"
SOURCE_R2_SECRET_NAME="${SOURCE_R2_SECRET_NAME:-r2-backup-credentials}"

INTERNAL_DEVOPS_ROOT="${INTERNAL_DEVOPS_ROOT:-/Users/aldoruizluna/labspace/internal-devops}"
DR_LOG="${INTERNAL_DEVOPS_ROOT}/runbooks/dr-log.md"

DRY_RUN=false
KEEP=false
OPERATOR="${USER:-unknown}"
BACKUP_KEY=""

# Colors (disabled if not a tty)
if [ -t 1 ]; then
    C_RED='\033[0;31m'
    C_GREEN='\033[0;32m'
    C_YELLOW='\033[1;33m'
    C_BLUE='\033[0;34m'
    C_RESET='\033[0m'
else
    C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_RESET=''
fi

log_info()  { echo -e "${C_BLUE}[INFO]${C_RESET}  $*"; }
log_ok()    { echo -e "${C_GREEN}[OK]${C_RESET}    $*"; }
log_warn()  { echo -e "${C_YELLOW}[WARN]${C_RESET}  $*" >&2; }
log_error() { echo -e "${C_RED}[ERROR]${C_RESET} $*" >&2; }
log_step()  { echo ""; echo -e "${C_BLUE}=== $* ===${C_RESET}"; }

usage() {
    cat <<'USAGE'
Usage: dr-drill.sh [OPTIONS]

Options:
  --dry-run            Print every action without executing. Safe on any cluster.
  --keep               Do not tear down the dr-test namespace at the end. Useful for post-mortem.
  --operator <name>    Operator identity for the dr-log row (defaults to $USER).
  --backup-key <key>   Use a specific backup key instead of auto-discovering the latest.
                       Example: --backup-key postgres/20260414_030000.sql.gz
  -h | --help          Show this help.

Environment overrides:
  R2_BUCKET                 Default: enclii-backups
  R2_PREFIX                 Default: postgres
  SOURCE_R2_SECRET_NS       Namespace to copy R2 creds from (default: enclii)
  SOURCE_R2_SECRET_NAME     R2 secret name (default: r2-backup-credentials)
  INTERNAL_DEVOPS_ROOT      Path to the internal-devops repo (default: /Users/aldoruizluna/labspace/internal-devops)

Exit codes: 0 success; 10 preflight; 20 download; 30 restore; 40 query; 50 teardown.
USAGE
}

# ---------------------------------------------------------------------------
# Arg parsing
# ---------------------------------------------------------------------------

while [ $# -gt 0 ]; do
    case "$1" in
        --dry-run)     DRY_RUN=true; shift ;;
        --keep)        KEEP=true; shift ;;
        --operator)    OPERATOR="$2"; shift 2 ;;
        --backup-key)  BACKUP_KEY="$2"; shift 2 ;;
        -h|--help)     usage; exit 0 ;;
        *)             log_error "Unknown argument: $1"; usage; exit 10 ;;
    esac
done

# ---------------------------------------------------------------------------
# Dry-run wrapper
# ---------------------------------------------------------------------------

# run_cmd: execute a command, or print it in --dry-run mode.
run_cmd() {
    if [ "$DRY_RUN" = true ]; then
        echo "  [dry-run] $*"
    else
        "$@"
    fi
}

# ---------------------------------------------------------------------------
# Timing helpers
# ---------------------------------------------------------------------------

# Seconds since epoch, integer only (portable across macOS + GNU).
now_s() { date +%s; }

elapsed() {
    local start="$1"
    local end
    end="$(now_s)"
    echo $(( end - start ))
}

# r2_secret_key: fetch a single key out of the copied R2 secret in DR_NAMESPACE.
# Centralized so we can quote cleanly and swap out the source if needed.
r2_secret_key() {
    local key="$1"
    kubectl get secret "${SOURCE_R2_SECRET_NAME}" -n "${DR_NAMESPACE}" \
        -o jsonpath="{.data.${key}}" | base64 -d
}

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------

preflight() {
    log_step "Phase 0: Preflight"

    if ! command -v kubectl >/dev/null 2>&1; then
        log_error "kubectl not found in PATH"; exit 10
    fi
    log_ok "kubectl: $(kubectl version --client --output=yaml 2>/dev/null | grep gitVersion | head -1 | awk '{print $2}')"

    if [ "$DRY_RUN" = false ]; then
        if ! kubectl cluster-info >/dev/null 2>&1; then
            log_error "Cannot reach Kubernetes cluster. Check KUBECONFIG."; exit 10
        fi
        log_ok "cluster reachable: $(kubectl config current-context)"

        if ! kubectl get secret "${SOURCE_R2_SECRET_NAME}" -n "${SOURCE_R2_SECRET_NS}" >/dev/null 2>&1; then
            log_error "R2 credentials secret ${SOURCE_R2_SECRET_NS}/${SOURCE_R2_SECRET_NAME} not found"
            exit 10
        fi
        log_ok "R2 secret found in ${SOURCE_R2_SECRET_NS}"
    else
        log_info "dry-run mode — skipping cluster + secret checks"
    fi

    if [ ! -f "${MANIFEST}" ]; then
        log_error "Missing manifest: ${MANIFEST}"; exit 10
    fi
    log_ok "manifest present: ${MANIFEST}"

    if [ ! -d "${INTERNAL_DEVOPS_ROOT}" ]; then
        log_warn "internal-devops not found at ${INTERNAL_DEVOPS_ROOT}; dr-log append will be skipped"
    fi
}

# ---------------------------------------------------------------------------
# Namespace + ephemeral Postgres
# ---------------------------------------------------------------------------

ensure_namespace() {
    log_step "Phase 1: (Re)create dr-test namespace"

    if [ "$DRY_RUN" = false ] && kubectl get namespace "${DR_NAMESPACE}" >/dev/null 2>&1; then
        log_warn "Namespace ${DR_NAMESPACE} exists — deleting for clean state"
        run_cmd kubectl delete namespace "${DR_NAMESPACE}" --wait=true --timeout=120s
    fi

    run_cmd kubectl create namespace "${DR_NAMESPACE}"

    # Replicate the R2 secret into dr-test so the downloader pod can auth.
    if [ "$DRY_RUN" = false ]; then
        kubectl get secret "${SOURCE_R2_SECRET_NAME}" -n "${SOURCE_R2_SECRET_NS}" -o yaml \
            | sed "s/namespace: ${SOURCE_R2_SECRET_NS}/namespace: ${DR_NAMESPACE}/" \
            | sed '/resourceVersion:/d' \
            | sed '/uid:/d' \
            | sed '/creationTimestamp:/d' \
            | kubectl apply -f -
    else
        echo "  [dry-run] kubectl get secret ${SOURCE_R2_SECRET_NAME} -n ${SOURCE_R2_SECRET_NS} -o yaml | sed ... | kubectl apply -f -"
    fi

    # Create a transient postgres-credentials secret scoped to this run.
    if [ "$DRY_RUN" = false ]; then
        kubectl create secret generic dr-postgres-credentials \
            --from-literal=username="${DR_DB_USER}" \
            --from-literal=password="${DR_DB_PASSWORD}" \
            --from-literal=database="${DR_DB_NAME}" \
            -n "${DR_NAMESPACE}"
    else
        echo "  [dry-run] kubectl create secret generic dr-postgres-credentials -n ${DR_NAMESPACE} (username/password/database)"
    fi

    log_ok "namespace ${DR_NAMESPACE} ready"
}

apply_manifest() {
    log_step "Phase 2: Apply ephemeral Postgres manifest"
    run_cmd kubectl apply -f "${MANIFEST}" -n "${DR_NAMESPACE}"

    if [ "$DRY_RUN" = false ]; then
        log_info "waiting for ${DR_POSTGRES_DEPLOY} to be Ready (timeout 180s)..."
        kubectl rollout status "deployment/${DR_POSTGRES_DEPLOY}" -n "${DR_NAMESPACE}" --timeout=180s
        kubectl wait --for=condition=Ready pod -l app="${DR_POSTGRES_DEPLOY}" -n "${DR_NAMESPACE}" --timeout=60s
    fi
    log_ok "ephemeral Postgres is Ready"
}

# ---------------------------------------------------------------------------
# Backup discovery + download
# ---------------------------------------------------------------------------

discover_and_download_backup() {
    log_step "Phase 3: Discover latest backup in R2"

    local start ts_ls
    start="$(now_s)"

    local ak sk acc
    if [ "$DRY_RUN" = false ]; then
        ak="$(r2_secret_key access-key-id)"
        sk="$(r2_secret_key secret-access-key)"
        acc="$(r2_secret_key account-id)"
    fi

    if [ -z "${BACKUP_KEY}" ]; then
        if [ "$DRY_RUN" = true ]; then
            BACKUP_KEY="${R2_PREFIX}/DRY_RUN_LATEST.sql.gz"
            BACKUP_TS="DRY_RUN_LATEST"
            echo "  [dry-run] would list s3://${R2_BUCKET}/${R2_PREFIX}/ and pick latest"
        else
            # shellcheck disable=SC2016
            local ls_cmd='aws s3 ls "s3://${R2_BUCKET}/${R2_PREFIX}/" --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com" | grep -v "latest.sql.gz" | sort | tail -1 | awk "{print \$4}"'
            local discovered
            discovered="$(kubectl run r2-ls --rm -i --quiet --restart=Never \
                --image=docker.io/amazon/aws-cli:2.33.16 \
                -n "${DR_NAMESPACE}" \
                --env="AWS_ACCESS_KEY_ID=${ak}" \
                --env="AWS_SECRET_ACCESS_KEY=${sk}" \
                --env="R2_ACCOUNT_ID=${acc}" \
                --env="R2_BUCKET=${R2_BUCKET}" \
                --env="R2_PREFIX=${R2_PREFIX}" \
                --env="AWS_DEFAULT_REGION=auto" \
                --command -- /bin/bash -c "${ls_cmd}" | tr -d '[:space:]')"
            if [ -z "${discovered}" ]; then
                log_error "R2 listing returned no backups in s3://${R2_BUCKET}/${R2_PREFIX}/"
                exit 20
            fi
            BACKUP_KEY="${R2_PREFIX}/${discovered}"
            BACKUP_TS="$(basename "${BACKUP_KEY}" .sql.gz)"
        fi
    else
        BACKUP_TS="$(basename "${BACKUP_KEY}" .sql.gz)"
    fi

    ts_ls="$(elapsed "$start")"
    log_ok "selected backup: ${BACKUP_KEY} (discovery took ${ts_ls}s)"

    log_step "Phase 4: Download backup into Postgres pod"
    local dl_start
    dl_start="$(now_s)"

    if [ "$DRY_RUN" = false ]; then
        local pod
        pod="$(kubectl get pod -n "${DR_NAMESPACE}" -l app="${DR_POSTGRES_DEPLOY}" -o jsonpath='{.items[0].metadata.name}')"

        # Pre-create the staging dir inside the Postgres pod.
        kubectl exec -n "${DR_NAMESPACE}" "${pod}" -- /bin/sh -c 'mkdir -p /tmp/dr && chmod 700 /tmp/dr'

        # postgres:16-alpine does not ship aws-cli. Spawn a short-lived aws-cli
        # pod, stream the object to stdout, capture locally, then kubectl cp
        # into the Postgres pod. This keeps aws creds out of the Postgres pod.
        kubectl run r2-dl --rm -i --quiet --restart=Never \
            --image=docker.io/amazon/aws-cli:2.33.16 \
            -n "${DR_NAMESPACE}" \
            --env="AWS_ACCESS_KEY_ID=${ak}" \
            --env="AWS_SECRET_ACCESS_KEY=${sk}" \
            --env="R2_ACCOUNT_ID=${acc}" \
            --env="AWS_DEFAULT_REGION=auto" \
            --command -- /bin/bash -c "aws s3 cp 's3://${R2_BUCKET}/${BACKUP_KEY}' - --endpoint-url \"https://\$R2_ACCOUNT_ID.r2.cloudflarestorage.com\"" \
            > /tmp/dr-drill-backup.sql.gz

        if [ ! -s /tmp/dr-drill-backup.sql.gz ]; then
            log_error "downloaded backup is empty"
            rm -f /tmp/dr-drill-backup.sql.gz
            exit 20
        fi

        kubectl cp /tmp/dr-drill-backup.sql.gz "${DR_NAMESPACE}/${pod}:/tmp/dr/backup.sql.gz"
        rm -f /tmp/dr-drill-backup.sql.gz
    else
        echo "  [dry-run] would spawn aws-cli pod, stream s3://${R2_BUCKET}/${BACKUP_KEY} to local, then kubectl cp into Postgres pod"
    fi

    PHASE_DOWNLOAD_S="$(elapsed "$dl_start")"
    log_ok "download completed in ${PHASE_DOWNLOAD_S}s"
}

# ---------------------------------------------------------------------------
# Restore
# ---------------------------------------------------------------------------

restore_backup() {
    log_step "Phase 5: Restore into ephemeral Postgres"
    local start pod
    start="$(now_s)"

    if [ "$DRY_RUN" = false ]; then
        pod="$(kubectl get pod -n "${DR_NAMESPACE}" -l app="${DR_POSTGRES_DEPLOY}" -o jsonpath='{.items[0].metadata.name}')"

        # Detect format by sniffing the first bytes after gunzip.
        # pg_dumpall output (the current backup shape) is plain SQL → use psql.
        # pg_dump -Fc output would start with the PGDMP magic → use pg_restore.
        local first_bytes
        first_bytes="$(kubectl exec -n "${DR_NAMESPACE}" "${pod}" -- \
            /bin/sh -c "gunzip -c /tmp/dr/backup.sql.gz | head -c 5 | od -An -c | tr -d ' '")"

        log_info "backup leading bytes: ${first_bytes}"

        # pg_dumpall output starts with "--" (SQL comment).
        # pg_dump -Fc output starts with "PGDMP".
        if echo "${first_bytes}" | grep -q "PGDMP"; then
            log_info "custom-format dump detected — using pg_restore"
            kubectl exec -n "${DR_NAMESPACE}" "${pod}" -- /bin/sh -c "
                set -e
                export PGPASSWORD='${DR_DB_PASSWORD}'
                gunzip -c /tmp/dr/backup.sql.gz > /tmp/dr/backup.dump
                pg_restore -U '${DR_DB_USER}' -d '${DR_DB_NAME}' --no-owner --no-privileges /tmp/dr/backup.dump 2>&1 | tail -50
                rm -f /tmp/dr/backup.dump
            "
        else
            log_info "plain SQL dump detected — using psql"
            # pg_dumpall creates its own roles / databases. Since we're running as a
            # single-user ephemeral instance, we pipe into the target DB and tolerate
            # role-creation noise.
            kubectl exec -n "${DR_NAMESPACE}" "${pod}" -- /bin/sh -c "
                set -e
                export PGPASSWORD='${DR_DB_PASSWORD}'
                gunzip -c /tmp/dr/backup.sql.gz | psql -U '${DR_DB_USER}' -d '${DR_DB_NAME}' -v ON_ERROR_STOP=0 -q 2>&1 | tail -30
            " || {
                log_error "restore encountered errors"
                exit 30
            }
        fi
    else
        echo "  [dry-run] would detect dump format and run psql/pg_restore inside ${DR_POSTGRES_DEPLOY} pod"
    fi

    PHASE_RESTORE_S="$(elapsed "$start")"
    log_ok "restore completed in ${PHASE_RESTORE_S}s"
}

# ---------------------------------------------------------------------------
# Sanity queries — uses real Enclii schema (all tables in public)
# ---------------------------------------------------------------------------

run_sanity_queries() {
    log_step "Phase 6: Sanity queries against restored data"
    local start pod
    start="$(now_s)"

    # 5 tables we want real counts + latest timestamps for. These come from
    # apps/switchyard-api/internal/db/migrations/001_genesis.up.sql. All tables
    # live in public, not switchyard.* or waybill.* (see task-brief note).
    #   1. projects        -- control-plane anchor
    #   2. services        -- per-project service registry
    #   3. deployments     -- deployment history
    #   4. releases        -- immutable release records
    #   5. daily_usage     -- Waybill cost-equivalent (genesis: public.daily_usage)
    local queries=(
        "SELECT 'projects' AS name, count(*) AS rows, coalesce(max(created_at)::text, 'NULL') AS latest FROM public.projects;"
        "SELECT 'services' AS name, count(*) AS rows, coalesce(max(created_at)::text, 'NULL') AS latest FROM public.services;"
        "SELECT 'deployments' AS name, count(*) AS rows, coalesce(max(created_at)::text, 'NULL') AS latest FROM public.deployments;"
        "SELECT 'releases' AS name, count(*) AS rows, coalesce(max(created_at)::text, 'NULL') AS latest FROM public.releases;"
        "SELECT 'daily_usage' AS name, count(*) AS rows, coalesce(max(usage_date)::text, 'NULL') AS latest FROM public.daily_usage;"
    )

    QUERY_OUTPUT=""
    if [ "$DRY_RUN" = false ]; then
        pod="$(kubectl get pod -n "${DR_NAMESPACE}" -l app="${DR_POSTGRES_DEPLOY}" -o jsonpath='{.items[0].metadata.name}')"
        for q in "${queries[@]}"; do
            local row
            if ! row="$(kubectl exec -n "${DR_NAMESPACE}" "${pod}" -- /bin/sh -c "
                export PGPASSWORD='${DR_DB_PASSWORD}'
                psql -U '${DR_DB_USER}' -d '${DR_DB_NAME}' -tAF'|' -c \"${q}\"
            " 2>&1)"; then
                log_error "query failed: ${q}"
                log_error "output: ${row}"
                exit 40
            fi
            echo "  ${row}"
            QUERY_OUTPUT="${QUERY_OUTPUT}${row}\n"
        done
    else
        for q in "${queries[@]}"; do
            echo "  [dry-run] psql -c \"${q}\""
        done
    fi

    PHASE_QUERY_S="$(elapsed "$start")"
    log_ok "sanity queries completed in ${PHASE_QUERY_S}s"
}

# ---------------------------------------------------------------------------
# Structured output + dr-log append
# ---------------------------------------------------------------------------

compute_metrics() {
    RUN_TS_UTC="$(date -u +%Y-%m-%dT%H:%MZ)"
    RUN_TS_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    # Extract backup timestamp from key like `postgres/20260414_030000.sql.gz`.
    # If the key is `postgres/latest.sql.gz` we leave backup_ts blank — the operator
    # should re-run with --backup-key <explicit> for accurate RPO tracking.
    if echo "${BACKUP_TS}" | grep -qE '^[0-9]{8}_[0-9]{6}$'; then
        # Format: YYYYMMDD_HHMMSS → ISO
        BACKUP_TS_ISO="${BACKUP_TS:0:4}-${BACKUP_TS:4:2}-${BACKUP_TS:6:2}T${BACKUP_TS:9:2}:${BACKUP_TS:11:2}Z"

        # RPO observed = time between backup capture and drill start.
        # Only compute on non-dry-run to avoid BACKUP_TS = DRY_RUN_LATEST weirdness.
        if [ "$DRY_RUN" = false ]; then
            local backup_epoch now_epoch
            backup_epoch="$(date -u -j -f "%Y%m%dT%H%M%SZ" "${BACKUP_TS:0:8}T${BACKUP_TS:9:6}Z" +%s 2>/dev/null || \
                            date -u -d "${BACKUP_TS:0:4}-${BACKUP_TS:4:2}-${BACKUP_TS:6:2} ${BACKUP_TS:9:2}:${BACKUP_TS:11:2}:${BACKUP_TS:13:2}" +%s 2>/dev/null || echo 0)"
            now_epoch="$(date -u +%s)"
            if [ "${backup_epoch}" -gt 0 ]; then
                local age_s=$(( now_epoch - backup_epoch ))
                RPO_OBSERVED_HR="$(( age_s / 3600 ))h$(( (age_s % 3600) / 60 ))m"
            else
                RPO_OBSERVED_HR="unknown"
            fi
        else
            RPO_OBSERVED_HR="dry-run"
        fi
    else
        BACKUP_TS_ISO="${BACKUP_TS}"
        RPO_OBSERVED_HR="unknown (non-timestamped key)"
    fi

    # RTO observed = total drill time (download + restore + query). This is a
    # useful proxy — a real DB failover would involve ops time we don't measure here.
    local total_s=$(( ${PHASE_DOWNLOAD_S:-0} + ${PHASE_RESTORE_S:-0} + ${PHASE_QUERY_S:-0} ))
    RTO_OBSERVED_S="${total_s}"

    echo ""
    echo "=== STRUCTURED OUTPUT ==="
    cat <<JSON
{
  "run_ts": "${RUN_TS_ISO}",
  "operator": "${OPERATOR}",
  "dry_run": ${DRY_RUN},
  "backup_key": "${BACKUP_KEY}",
  "backup_ts": "${BACKUP_TS_ISO}",
  "phase_download_s": ${PHASE_DOWNLOAD_S:-0},
  "phase_restore_s": ${PHASE_RESTORE_S:-0},
  "phase_query_s": ${PHASE_QUERY_S:-0},
  "rpo_observed": "${RPO_OBSERVED_HR}",
  "rto_observed_s": ${RTO_OBSERVED_S},
  "namespace": "${DR_NAMESPACE}",
  "kept": ${KEEP}
}
JSON
    echo ""
}

append_dr_log() {
    if [ ! -d "${INTERNAL_DEVOPS_ROOT}/runbooks" ]; then
        log_warn "skipping dr-log append: ${INTERNAL_DEVOPS_ROOT}/runbooks not found"
        return 0
    fi

    # Create file with header if missing.
    if [ ! -f "${DR_LOG}" ]; then
        log_info "creating ${DR_LOG}"
        cat > "${DR_LOG}" <<'HEADER'
# DR Drill Log

> **Last Updated:** auto-appended by `scripts/dr-drill.sh` in the `enclii` repo.
> **Purpose:** evidence trail for the RPO/RTO claims in `runbooks/disaster-recovery.md`.
> Each row is the output of one Postgres restore drill: download backup from R2, restore
> into ephemeral `dr-test` namespace, run 5 sanity queries, tear down.

## Schema

| Column | Meaning |
|---|---|
| `run_ts` | When the drill started (ISO-8601 UTC) |
| `operator` | Human who ran the drill (or `cron-*` for automated) |
| `backup_ts` | Timestamp embedded in the backup filename (ISO) |
| `download_s` | Seconds to fetch the backup from R2 |
| `restore_s` | Seconds to `pg_restore` / `psql` the dump into ephemeral Postgres |
| `query_s` | Seconds to run the 5 sanity SELECTs |
| `rpo_observed` | Gap between backup capture and drill start — validates the RPO claim |
| `rto_observed` | Total drill duration (download + restore + query) in seconds — proxy for partial RTO |
| `notes` | Anything operator wants to record (failures, anomalies, `--keep` reasons) |

## Drills

| run_ts | operator | backup_ts | download_s | restore_s | query_s | rpo_observed | rto_observed | notes |
|---|---|---|---|---|---|---|---|---|
| 2026-04-17T00:00Z | _placeholder_ | _YYYYMMDD_HHMMSS_ | _N_ | _N_ | _N_ | _Xh Ym_ | _Ns_ | _delete this row after first real drill_ |
HEADER
    fi

    # Append one-line row
    local note="ok"
    if [ "$DRY_RUN" = true ]; then note="dry-run (no real restore)"; fi
    if [ "$KEEP" = true ]; then note="${note}; --keep set, dr-test retained"; fi

    local row
    row="| ${RUN_TS_UTC} | ${OPERATOR} | ${BACKUP_TS_ISO} | ${PHASE_DOWNLOAD_S:-0} | ${PHASE_RESTORE_S:-0} | ${PHASE_QUERY_S:-0} | ${RPO_OBSERVED_HR} | ${RTO_OBSERVED_S}s | ${note} |"

    if [ "$DRY_RUN" = false ]; then
        printf '%s\n' "${row}" >> "${DR_LOG}"
        log_ok "appended row to ${DR_LOG}"
    else
        echo "  [dry-run] would append to ${DR_LOG}:"
        echo "    ${row}"
    fi
}

# ---------------------------------------------------------------------------
# Teardown
# ---------------------------------------------------------------------------

teardown() {
    log_step "Phase 7: Teardown"
    if [ "$KEEP" = true ]; then
        log_warn "--keep set — namespace ${DR_NAMESPACE} preserved for post-mortem."
        log_warn "clean up manually with: kubectl delete namespace ${DR_NAMESPACE}"
        return 0
    fi

    if ! run_cmd kubectl delete namespace "${DR_NAMESPACE}" --wait=false; then
        log_error "namespace delete failed — cluster may need manual cleanup"
        exit 50
    fi
    log_ok "namespace ${DR_NAMESPACE} deletion initiated"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    echo "=== Enclii DR Drill (P0.1) ==="
    echo "  mode:      $([ "$DRY_RUN" = true ] && echo dry-run || echo live)"
    echo "  operator:  ${OPERATOR}"
    echo "  namespace: ${DR_NAMESPACE} (ephemeral)"
    echo "  r2:        s3://${R2_BUCKET}/${R2_PREFIX}/"
    echo "  keep:      ${KEEP}"
    if [ -n "${BACKUP_KEY}" ]; then
        echo "  backup:    ${BACKUP_KEY} (explicit)"
    else
        echo "  backup:    <auto-discover latest>"
    fi

    preflight
    ensure_namespace
    apply_manifest
    discover_and_download_backup
    restore_backup
    run_sanity_queries
    compute_metrics
    append_dr_log
    teardown

    echo ""
    log_ok "=== DR DRILL PASSED ==="
    echo "  backup:     ${BACKUP_KEY}"
    echo "  download:   ${PHASE_DOWNLOAD_S:-0}s"
    echo "  restore:    ${PHASE_RESTORE_S:-0}s"
    echo "  query:      ${PHASE_QUERY_S:-0}s"
    echo "  rpo:        ${RPO_OBSERVED_HR}"
    echo "  rto(proxy): ${RTO_OBSERVED_S}s"
    echo ""
}

main "$@"
