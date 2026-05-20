#!/bin/bash
# ARC Runner Verification Script
# Usage: ./verify-runners.sh [--wait] [--timeout SECONDS]
#
# Verifies that ARC runners are healthy and registered with GitHub.
# Returns exit code 0 if healthy, 1 if unhealthy.

set -euo pipefail

# Configuration
NAMESPACE="arc-runners"
CONTROLLER_NAMESPACE="arc-system"
SCALE_SET_PREFIX="${SCALE_SET_PREFIX:-madfam-runners}"  # Override via env: SCALE_SET_PREFIX=custom-runners
DEFAULT_TIMEOUT=120

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

# Check if ARC controller is healthy
check_controller() {
    log_info "Checking ARC Controller..."

    local ready
    ready=$(kubectl get deployment arc-controller-gha-rs-controller -n "${CONTROLLER_NAMESPACE}" \
        -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")

    local desired
    desired=$(kubectl get deployment arc-controller-gha-rs-controller -n "${CONTROLLER_NAMESPACE}" \
        -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "2")

    if [[ "${ready}" -ge 1 ]] && [[ "${ready}" -eq "${desired}" ]]; then
        log_success "Controller: ${ready}/${desired} replicas ready"
        return 0
    else
        log_error "Controller: ${ready}/${desired} replicas ready"
        return 1
    fi
}

# Check if runner scale sets exist
check_scale_sets() {
    log_info "Checking Runner Scale Sets..."

    local scale_sets
    scale_sets=$(kubectl get autoscalingrunnerset -n "${NAMESPACE}" --no-headers 2>/dev/null | wc -l | tr -d ' ')

    if [[ ${scale_sets} -ge 1 ]]; then
        log_success "Scale sets found: ${scale_sets}"

        # List each scale set
        kubectl get autoscalingrunnerset -n "${NAMESPACE}" -o wide 2>/dev/null || true
        echo ""
        return 0
    else
        log_error "No scale sets found"
        return 1
    fi
}

check_image_pull_secrets() {
    log_info "Checking Runner Image Pull Secrets..."

    local failed=0
    local scale_sets
    scale_sets=$(kubectl get autoscalingrunnerset -n "${NAMESPACE}" -o json 2>/dev/null | jq -r '.items[].metadata.name' || true)

    if [[ -z "${scale_sets}" ]]; then
        log_error "No scale sets found for image pull secret validation"
        return 1
    fi

    while IFS= read -r scale_set; do
        [[ -z "${scale_set}" ]] && continue

        local runner_image pull_secret
        runner_image=$(kubectl get autoscalingrunnerset "${scale_set}" -n "${NAMESPACE}" \
            -o jsonpath='{.spec.template.spec.containers[?(@.name=="runner")].image}' 2>/dev/null || true)
        pull_secret=$(kubectl get autoscalingrunnerset "${scale_set}" -n "${NAMESPACE}" \
            -o jsonpath='{.spec.template.spec.imagePullSecrets[?(@.name=="ghcr-credentials")].name}' 2>/dev/null || true)

        if [[ "${runner_image}" == ghcr.io/madfam-org/* ]]; then
            if [[ "${pull_secret}" == "ghcr-credentials" ]]; then
                log_success "${scale_set}: ghcr-credentials configured"
            else
                log_error "${scale_set}: missing ghcr-credentials for ${runner_image}"
                failed=$((failed + 1))
            fi
        else
            log_info "${scale_set}: runner image is not a private MADFAM GHCR image"
        fi
    done <<< "${scale_sets}"

    if [[ ${failed} -eq 0 ]]; then
        return 0
    fi

    return 1
}

# Check if runners are registered (pods running)
check_runner_pods() {
    log_info "Checking Runner Pods..."

    local total_runners=0
    local healthy_runners=0

    for color in blue green; do
        local scale_set="${SCALE_SET_PREFIX}-${color}"
        local pods
        pods=$(kubectl get pods -n "${NAMESPACE}" \
            -l "actions.github.com/scale-set-name=${scale_set}" \
            --no-headers 2>/dev/null | wc -l | tr -d ' ')

        local running
        running=$(kubectl get pods -n "${NAMESPACE}" \
            -l "actions.github.com/scale-set-name=${scale_set}" \
            --field-selector=status.phase=Running \
            --no-headers 2>/dev/null | wc -l | tr -d ' ')

        total_runners=$((total_runners + pods))
        healthy_runners=$((healthy_runners + running))

        if [[ ${pods} -gt 0 ]]; then
            if [[ ${running} -eq ${pods} ]]; then
                log_success "${scale_set}: ${running}/${pods} pods running"
            else
                log_warn "${scale_set}: ${running}/${pods} pods running"
            fi
        else
            log_info "${scale_set}: 0 pods (standby)"
        fi
    done

    if [[ ${healthy_runners} -ge 1 ]]; then
        return 0
    else
        log_error "No healthy runners found"
        return 1
    fi
}

# Check PVC status
check_pvcs() {
    log_info "Checking PVCs..."

    local pvcs
    pvcs=$(kubectl get pvc -n "${NAMESPACE}" --no-headers 2>/dev/null | wc -l | tr -d ' ')

    if [[ ${pvcs} -ge 1 ]]; then
        local bound
        bound=$(kubectl get pvc -n "${NAMESPACE}" --no-headers 2>/dev/null | awk '$2 == "Bound" { count++ } END { print count + 0 }')

        if [[ ${bound} -eq ${pvcs} ]]; then
            log_success "PVCs: ${bound}/${pvcs} bound"
        else
            log_warn "PVCs: ${bound}/${pvcs} bound"
        fi

        kubectl get pvc -n "${NAMESPACE}" 2>/dev/null || true
        echo ""
        return 0
    else
        log_warn "No PVCs found (caching disabled)"
        return 0
    fi
}

# Check controller metrics endpoint
check_metrics() {
    log_info "Checking Metrics Endpoint..."

    # Port-forward in background
    kubectl port-forward -n "${CONTROLLER_NAMESPACE}" svc/arc-gha-rs-controller-metrics 18080:8080 &>/dev/null &
    local pf_pid=$!
    sleep 2

    if curl -s "http://localhost:18080/metrics" | grep -q "arc_"; then
        log_success "Metrics endpoint responding"
        kill ${pf_pid} 2>/dev/null || true
        return 0
    else
        log_warn "Metrics endpoint not responding (may be ok)"
        kill ${pf_pid} 2>/dev/null || true
        return 0
    fi
}

# Wait for runners with timeout
wait_for_runners() {
    local timeout=$1
    local elapsed=0
    local interval=10

    log_info "Waiting for runners to become ready (timeout: ${timeout}s)..."

    while [[ ${elapsed} -lt ${timeout} ]]; do
        if check_runner_pods &>/dev/null; then
            log_success "Runners are ready!"
            return 0
        fi

        sleep ${interval}
        elapsed=$((elapsed + interval))
        log_info "  Waiting... (${elapsed}s/${timeout}s)"
    done

    log_error "Timeout waiting for runners"
    return 1
}

# Run all checks
run_checks() {
    local failed=0

    echo ""
    echo "============================================"
    echo "  ARC Runner Verification"
    echo "============================================"
    echo ""

    check_controller || failed=$((failed + 1))
    echo ""

    check_scale_sets || failed=$((failed + 1))
    echo ""

    check_image_pull_secrets || failed=$((failed + 1))
    echo ""

    check_runner_pods || failed=$((failed + 1))
    echo ""

    check_pvcs
    echo ""

    # check_metrics  # Uncomment if you want metrics check
    # echo ""

    echo "============================================"
    if [[ ${failed} -eq 0 ]]; then
        log_success "All checks passed"
        return 0
    else
        log_error "${failed} check(s) failed"
        return 1
    fi
}

print_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --wait              Wait for runners to become ready"
    echo "  --timeout SECONDS   Timeout for --wait (default: ${DEFAULT_TIMEOUT})"
    echo "  --quiet             Only output on failure"
    echo "  -h, --help          Show this help message"
    echo ""
    echo "Exit Codes:"
    echo "  0   All checks passed"
    echo "  1   One or more checks failed"
}

# Main execution
main() {
    local wait_mode=false
    local timeout=${DEFAULT_TIMEOUT}
    local quiet=false

    while [[ $# -gt 0 ]]; do
        case $1 in
            --wait)
                wait_mode=true
                shift
                ;;
            --timeout)
                timeout=$2
                shift 2
                ;;
            --quiet)
                quiet=true
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

    if [[ "${wait_mode}" == "true" ]]; then
        if [[ "${quiet}" == "true" ]]; then
            wait_for_runners "${timeout}" &>/dev/null
        else
            wait_for_runners "${timeout}"
        fi
    else
        if [[ "${quiet}" == "true" ]]; then
            run_checks &>/dev/null
        else
            run_checks
        fi
    fi
}

main "$@"
