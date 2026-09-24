#!/bin/bash
# backup-database.sh - Manage PostgreSQL backups to Cloudflare R2
#
# Usage:
#   ./scripts/backup-database.sh [command]
#
# Commands:
#   setup     Create R2 bucket and configure backup credentials
#   backup    Trigger immediate backup
#   list      List available backups
#   restore   Refuses: points at the PITR runbook and the restore drills
#   status    Show backup CronJob status
#   test      Test backup configuration without uploading

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BACKUP_DIR="${PROJECT_ROOT}/infra/k8s/production/backup"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

check_prerequisites() {
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi

    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
}

setup_backups() {
    log_info "Setting up PostgreSQL backups to R2..."

    # Check if secret exists
    if ! kubectl get secret r2-backup-credentials -n enclii &> /dev/null; then
        log_warn "R2 backup credentials not found"
        echo ""
        echo "Please create the R2 credentials secret:"
        echo "  1. Create R2 bucket 'enclii-backups' in Cloudflare dashboard"
        echo "  2. Create R2 API token with Object Read & Write permissions"
        echo "  3. Copy and fill in the template:"
        echo ""
        echo "     cp ${BACKUP_DIR}/backup-secrets.yaml.template ${BACKUP_DIR}/backup-secrets.yaml"
        echo "     # Edit backup-secrets.yaml with your credentials"
        echo "     kubectl apply -f ${BACKUP_DIR}/backup-secrets.yaml"
        echo ""
        exit 1
    fi

    log_success "R2 credentials found"

    # Apply backup manifests
    log_info "Applying backup CronJob..."
    kubectl apply -f "${BACKUP_DIR}/postgres-backup.yaml"

    log_success "Backup setup complete!"
    echo ""
    show_status
}

trigger_backup() {
    log_info "Triggering immediate backup..."

    # Create job from cronjob
    JOB_NAME="postgres-backup-manual-$(date +%Y%m%d%H%M%S)"

    kubectl create job "${JOB_NAME}" \
        --from=cronjob/postgres-backup \
        -n enclii

    log_info "Backup job created: ${JOB_NAME}"
    log_info "Watching job progress..."

    # Wait for job with timeout
    kubectl wait --for=condition=complete --timeout=600s "job/${JOB_NAME}" -n enclii || {
        log_warn "Job did not complete in time. Check logs:"
        echo "  kubectl logs -n enclii job/${JOB_NAME}"
        exit 1
    }

    log_success "Backup completed successfully!"

    # Show job logs
    kubectl logs -n enclii "job/${JOB_NAME}"
}

list_backups() {
    log_info "Listing available backups..."

    # Get R2 credentials from secret
    R2_ACCOUNT_ID=$(kubectl get secret r2-backup-credentials -n enclii -o jsonpath='{.data.account-id}' | base64 -d)
    AWS_ACCESS_KEY_ID=$(kubectl get secret r2-backup-credentials -n enclii -o jsonpath='{.data.access-key-id}' | base64 -d)
    AWS_SECRET_ACCESS_KEY=$(kubectl get secret r2-backup-credentials -n enclii -o jsonpath='{.data.secret-access-key}' | base64 -d)

    if ! command -v aws &> /dev/null; then
        log_warn "AWS CLI not installed locally. Listing via kubectl..."

        # Create a pod to list backups
        kubectl run list-backups --rm -i --restart=Never \
            --image=amazon/aws-cli:latest \
            --env="AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID}" \
            --env="AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY}" \
            -n enclii \
            -- s3 ls s3://enclii-backups/postgres/ \
               --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
    else
        # Use local AWS CLI
        AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID}" \
        AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY}" \
        aws s3 ls s3://enclii-backups/postgres/ \
            --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
    fi
}

restore_backup() {
    # This used to `kubectl create job --from=cronjob/postgres-backup -n enclii
    # -- /bin/bash /scripts/restore.sh`. No `restore.sh` exists in the
    # postgres-backup-script ConfigMap (it holds dump.sh and upload.sh only),
    # the CronJob lives in `data`, not `enclii`, and the command would have
    # overwritten the shared production instance. It could never have worked,
    # so it now refuses loudly instead of pretending.
    log_error "In-place restore is not automated. Production restores are a"
    log_error "break-glass operation: follow docs/runbooks/POSTGRES_WAL_ARCHIVING.md"
    log_error "(pgBackRest PITR into a side-channel instance first; never in place)."
    log_error "To PROVE a backup restores, run a drill instead:"
    log_error "  ./scripts/backup-restore-drill.sh     (logical pg_dumpall drill)"
    log_error "  kubectl -n data create job --from=cronjob/pgbackrest-restore-drill pgbackrest-restore-drill-manual-\$(date -u +%m%d%H%M)"
    exit 1
}

show_status() {
    log_info "Backup Status:"
    echo ""

    echo "=== CronJob ==="
    kubectl get cronjob postgres-backup -n enclii 2>/dev/null || echo "CronJob not found"
    echo ""

    echo "=== Recent Jobs ==="
    kubectl get jobs -n enclii -l app=postgres-backup --sort-by=.metadata.creationTimestamp 2>/dev/null | tail -5 || echo "No backup jobs found"
    echo ""

    echo "=== Last Backup Logs ==="
    LAST_JOB=$(kubectl get jobs -n enclii -l app=postgres-backup -o jsonpath='{.items[-1].metadata.name}' 2>/dev/null || echo "")
    if [[ -n "${LAST_JOB}" ]]; then
        kubectl logs -n enclii "job/${LAST_JOB}" --tail=10 2>/dev/null || echo "No logs available"
    else
        echo "No backup jobs found"
    fi
}

test_backup() {
    log_info "Testing backup configuration..."

    # Check secrets
    log_info "Checking R2 credentials..."
    if kubectl get secret r2-backup-credentials -n enclii &> /dev/null; then
        log_success "R2 credentials found"
    else
        log_error "R2 credentials not found"
        exit 1
    fi

    # Check postgres credentials
    log_info "Checking PostgreSQL credentials..."
    if kubectl get secret postgres-credentials -n enclii &> /dev/null; then
        log_success "PostgreSQL credentials found"
    else
        log_error "PostgreSQL credentials not found"
        exit 1
    fi

    # Test database connection
    log_info "Testing database connection..."
    kubectl exec -n enclii deploy/postgres -- pg_isready -U postgres && log_success "Database is ready" || log_error "Database not ready"

    # Test R2 connectivity
    log_info "Testing R2 connectivity..."
    R2_ACCOUNT_ID=$(kubectl get secret r2-backup-credentials -n enclii -o jsonpath='{.data.account-id}' | base64 -d)

    kubectl run test-r2 --rm -i --restart=Never \
        --image=amazon/aws-cli:latest \
        --env="AWS_ACCESS_KEY_ID=$(kubectl get secret r2-backup-credentials -n enclii -o jsonpath='{.data.access-key-id}' | base64 -d)" \
        --env="AWS_SECRET_ACCESS_KEY=$(kubectl get secret r2-backup-credentials -n enclii -o jsonpath='{.data.secret-access-key}' | base64 -d)" \
        -n enclii \
        -- s3 ls s3://enclii-backups/ \
           --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com" 2>/dev/null && \
        log_success "R2 connectivity OK" || log_warn "R2 connectivity test failed (bucket may not exist yet)"

    log_success "Configuration test complete"
}

# Main
case "${1:-status}" in
    setup)
        check_prerequisites
        setup_backups
        ;;
    backup)
        check_prerequisites
        trigger_backup
        ;;
    list)
        check_prerequisites
        list_backups
        ;;
    restore)
        check_prerequisites
        restore_backup "${2:-postgres/latest.sql.gz}"
        ;;
    status)
        check_prerequisites
        show_status
        ;;
    test)
        check_prerequisites
        test_backup
        ;;
    *)
        echo "Usage: $0 {setup|backup|list|restore|status|test}"
        echo ""
        echo "Commands:"
        echo "  setup           Configure backup CronJob and credentials"
        echo "  backup          Trigger immediate backup"
        echo "  list            List available backups in R2"
        echo "  restore         Refuses; see the PITR runbook and restore drills"
        echo "  status          Show backup job status"
        echo "  test            Test backup configuration"
        exit 1
        ;;
esac
