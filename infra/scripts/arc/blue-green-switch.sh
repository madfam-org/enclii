#!/bin/bash
# Blue-Green Deployment Orchestration for ARC Runners
# Usage: ./blue-green-switch.sh [blue|green|auto]
#
# This script performs atomic blue-green switches for self-hosted runners:
# 1. Activates the target color (scales up from 0 to minRunners)
# 2. Waits for new runners to register with GitHub
# 3. Drains the current color (waits for running jobs to complete)
# 4. Sets the old color to standby (scales down to 0)
#
# The switch is safe because:
# - New runners must be healthy before old ones are drained
# - Running jobs are never interrupted
# - Rollback is instant (just switch back to the other color)

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
NAMESPACE="arc-runners"
CHART_VERSION="0.14.0"
SCALE_SET_PREFIX="${SCALE_SET_PREFIX:-madfam-runners}"  # Override via env: SCALE_SET_PREFIX=custom-runners
DRAIN_TIMEOUT=300        # 5 minutes max wait for jobs to complete
REGISTRATION_WAIT=120    # Seconds to wait for runners to register
MIN_RUNNERS=1            # Minimum runners for active scale set
SKIP_OLD_DEACTIVATION="${ARC_SKIP_OLD_DEACTIVATION:-false}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $(date '+%H:%M:%S') $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $(date '+%H:%M:%S') $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $(date '+%H:%M:%S') $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') $1"
}

max_runners_for_color() {
    local color=$1
    if [[ "${color}" == "blue" ]]; then
        echo "12"
    else
        echo "2"
    fi
}

# Get the currently active color from scale set labels
get_active_color() {
    local scale_sets_json
    if ! scale_sets_json=$(kubectl get autoscalingrunnerset -n "${NAMESPACE}" -o json 2>/dev/null); then
        log_error "Unable to read ARC scale sets from ${NAMESPACE}" >&2
        return 1
    fi

    local active
    active=$(jq -r '.items[] | select(((.metadata.labels["arc.enclii.dev/active"] // .spec.template.metadata.labels["arc.enclii.dev/active"]) == "true")) | .metadata.name' <<< "${scale_sets_json}" | \
        grep -oE '(blue|green)' | head -1) || true

    if [[ -z "${active}" ]]; then
        # If no active label, check which one has runners
        local blue_runners green_runners
        if ! blue_runners=$(kubectl get pods -n "${NAMESPACE}" -l "actions.github.com/scale-set-name=${SCALE_SET_PREFIX}-blue" --no-headers 2>/dev/null | wc -l | tr -d ' '); then
            log_error "Unable to read blue runner pods from ${NAMESPACE}" >&2
            return 1
        fi
        if ! green_runners=$(kubectl get pods -n "${NAMESPACE}" -l "actions.github.com/scale-set-name=${SCALE_SET_PREFIX}-green" --no-headers 2>/dev/null | wc -l | tr -d ' '); then
            log_error "Unable to read green runner pods from ${NAMESPACE}" >&2
            return 1
        fi

        if [[ ${blue_runners} -gt 0 ]]; then
            echo "blue"
        elif [[ ${green_runners} -gt 0 ]]; then
            echo "green"
        else
            # Default to blue if nothing is running
            echo "blue"
        fi
    else
        echo "${active}"
    fi
}

# Get count of running runner pods for a scale set
get_running_pods() {
    local scale_set=$1
    kubectl get pods -n "${NAMESPACE}" \
        -l "actions.github.com/scale-set-name=${scale_set}" \
        --field-selector=status.phase=Running \
        --no-headers 2>/dev/null | wc -l | tr -d ' '
}

# Get count of pods with active jobs (busy runners)
get_busy_runners() {
    local scale_set=$1
    # Treat a missing runner-state annotation as busy. That is conservative
    # during drains and still returns 0 for absent standby scale sets.
    kubectl get pods -n "${NAMESPACE}" \
        -l "actions.github.com/scale-set-name=${scale_set}" \
        -o json 2>/dev/null | \
        jq '[.items[] | select((.metadata.annotations["actions.github.com/runner-state"] // "busy") != "idle")] | length' || echo "0"
}

# Drain a scale set by waiting for all jobs to complete
drain_scale_set() {
    local scale_set=$1
    local elapsed=0

    log_info "Draining ${scale_set}..."

    # Set minRunners to 0 to prevent new jobs from being scheduled
    log_info "Setting minRunners=0 for ${scale_set}..."
    helm upgrade "${scale_set}" \
        oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
        --namespace "${NAMESPACE}" \
        --reuse-values \
        --set minRunners=0 \
        --wait \
        --timeout 2m

    # Wait for running jobs to complete
    local busy_runners
    busy_runners=$(get_busy_runners "${scale_set}")

    while [[ ${busy_runners} -gt 0 ]] && [[ ${elapsed} -lt ${DRAIN_TIMEOUT} ]]; do
        log_info "  Waiting for ${busy_runners} job(s) to complete... (${elapsed}s/${DRAIN_TIMEOUT}s)"
        sleep 10
        elapsed=$((elapsed + 10))
        busy_runners=$(get_busy_runners "${scale_set}")
    done

    if [[ ${busy_runners} -gt 0 ]]; then
        log_warn "Timeout reached, ${busy_runners} job(s) still running"
        log_warn "Jobs will complete on existing pods, new jobs go to new scale set"
        return 1
    fi

    log_success "${scale_set} drained successfully"
    return 0
}

# Activate a scale set (scale up and set active label)
activate_scale_set() {
    local color=$1
    local scale_set="${SCALE_SET_PREFIX}-${color}"
    local values_file="${REPO_ROOT}/infra/helm/arc/values-runner-set-${color}.yaml"
    local max_runners
    max_runners=$(max_runners_for_color "${color}")

    log_info "Activating ${scale_set}..."

    helm upgrade --install "${scale_set}" \
        oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
        --namespace "${NAMESPACE}" \
        --version "${CHART_VERSION}" \
        --values "${REPO_ROOT}/infra/helm/arc/values-runner-set.yaml" \
        --values "${values_file}" \
        --set minRunners="${MIN_RUNNERS}" \
        --set maxRunners="${max_runners}" \
        --set-string 'template.metadata.labels.arc\.enclii\.dev/active=true' \
        --set "template.metadata.annotations.arc\\.enclii\\.dev/deployed-at=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --wait \
        --timeout 5m

    kubectl label autoscalingrunnerset "${scale_set}" \
        -n "${NAMESPACE}" \
        --overwrite \
        arc.enclii.dev/color="${color}" \
        arc.enclii.dev/active="true"

    log_success "${scale_set} activated"
}

# Deactivate a scale set (scale down and set inactive label)
deactivate_scale_set() {
    local color=$1
    local scale_set="${SCALE_SET_PREFIX}-${color}"
    local max_runners
    max_runners=$(max_runners_for_color "${color}")

    log_info "Deactivating ${scale_set}..."

    helm upgrade "${scale_set}" \
        oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
        --namespace "${NAMESPACE}" \
        --version "${CHART_VERSION}" \
        --reuse-values \
        --set minRunners=0 \
        --set maxRunners="${max_runners}" \
        --set-string 'template.metadata.labels.arc\.enclii\.dev/active=false' \
        --wait \
        --timeout 2m

    kubectl label autoscalingrunnerset "${scale_set}" \
        -n "${NAMESPACE}" \
        --overwrite \
        arc.enclii.dev/active="false"

    log_success "${scale_set} set to standby"
}

# Wait for runners to register with GitHub
wait_for_registration() {
    local scale_set=$1
    local timeout=$2
    local elapsed=0

    log_info "Waiting for runners to register with GitHub..."

    while [[ ${elapsed} -lt ${timeout} ]]; do
        local running_pods
        running_pods=$(get_running_pods "${scale_set}")

        if [[ ${running_pods} -ge 1 ]]; then
            log_success "Runner(s) registered: ${running_pods} pod(s) running"
            return 0
        fi

        sleep 5
        elapsed=$((elapsed + 5))
        log_info "  Waiting... (${elapsed}s/${timeout}s)"
    done

    log_error "Timeout waiting for runners to register"
    return 1
}

# Print status of both scale sets
print_status() {
    echo ""
    echo "=== Current Status ==="
    echo ""

    for color in blue green; do
        local scale_set="${SCALE_SET_PREFIX}-${color}"
        local running busy
        running=$(get_running_pods "${scale_set}")
        busy=$(get_busy_runners "${scale_set}")

        # Get active label
        local active_label
        active_label=$(kubectl get autoscalingrunnerset "${scale_set}" -n "${NAMESPACE}" \
            -o jsonpath='{.metadata.labels.arc\.enclii\.dev/active}' 2>/dev/null || echo "unknown")

        printf "%-25s runners=%s busy=%s active=%s\n" "${scale_set}:" "${running}" "${busy}" "${active_label}"
    done

    echo ""
}

print_usage() {
    echo "Usage: $0 [TARGET_COLOR]"
    echo ""
    echo "Arguments:"
    echo "  TARGET_COLOR    'blue', 'green', or 'auto' (default: auto)"
    echo "                  auto: switches to the opposite of current active color"
    echo ""
    echo "Options:"
    echo "  --status        Print current status and exit"
    echo "  --force         Skip confirmation prompts"
    echo "  -h, --help      Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0              # Auto-switch to inactive color"
    echo "  $0 blue         # Switch to blue"
    echo "  $0 green        # Switch to green"
    echo "  $0 --status     # Show current status"
}

# Main execution
main() {
    local target_color=""
    local force=false
    local status_only=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            blue|green|auto)
                target_color=$1
                shift
                ;;
            --status)
                status_only=true
                shift
                ;;
            --force)
                force=true
                shift
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                print_usage
                exit 1
                ;;
        esac
    done

    # Print status and exit if requested
    if [[ "${status_only}" == "true" ]]; then
        print_status
        exit 0
    fi

    # Determine current and target colors
    local current_active
    current_active=$(get_active_color)

    if [[ -z "${target_color}" ]] || [[ "${target_color}" == "auto" ]]; then
        if [[ "${current_active}" == "blue" ]]; then
            target_color="green"
        else
            target_color="blue"
        fi
    fi

    echo ""
    echo "============================================"
    echo "  Blue-Green Switch"
    echo "============================================"
    echo ""
    echo "  Current active: ${current_active}"
    echo "  Target active:  ${target_color}"
    echo ""

    # Confirm unless --force
    if [[ "${force}" != "true" ]]; then
        read -p "Proceed with switch? [y/N] " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            log_info "Aborted"
            exit 0
        fi
    fi

    # Perform the switch
    if [[ "${target_color}" == "${current_active}" ]]; then
        log_info "Target is already active. Performing in-place update..."
        activate_scale_set "${target_color}"
    else
        log_info "Performing blue-green switch..."

        # 1. Activate new color
        activate_scale_set "${target_color}"

        # 2. Wait for new runners to register
        wait_for_registration "${SCALE_SET_PREFIX}-${target_color}" "${REGISTRATION_WAIT}" || {
            log_error "New runners failed to register. Aborting switch."
            log_info "Rolling back..."
            deactivate_scale_set "${target_color}"
            exit 1
        }

        if [[ "${SKIP_OLD_DEACTIVATION}" == "true" ]]; then
            log_warn "Skipping ${SCALE_SET_PREFIX}-${current_active} scale-down in this job"
            log_warn "The old scale set remains available until an out-of-band cleanup runs"
            kubectl label autoscalingrunnerset "${SCALE_SET_PREFIX}-${current_active}" \
                -n "${NAMESPACE}" \
                --overwrite \
                arc.enclii.dev/active="false"
            print_status
            log_success "Active scale set: ${SCALE_SET_PREFIX}-${target_color}"
            return 0
        fi

        # 3. Drain old color
        drain_scale_set "${SCALE_SET_PREFIX}-${current_active}" || {
            log_warn "Drain incomplete, but new runners are ready"
        }

        # 4. Deactivate old color
        deactivate_scale_set "${current_active}"
    fi

    echo ""
    echo "============================================"
    echo "  Switch Complete"
    echo "============================================"
    print_status

    log_success "Active scale set: ${SCALE_SET_PREFIX}-${target_color}"
}

main "$@"
