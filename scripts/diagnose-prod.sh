#!/bin/bash
# ==============================================================================
# diagnose-prod.sh - Production Infrastructure Diagnostic Script
# ==============================================================================
# Usage: ./diagnose-prod.sh [--full] [--quick]
#
# This script performs a comprehensive audit of the production infrastructure.
# Run via SSH or directly on the production host.
#
# Generated: 2026-01-17
# ==============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Thresholds
DISK_WARN_THRESHOLD=75
DISK_CRIT_THRESHOLD=85
INODE_WARN_THRESHOLD=80
INODE_CRIT_THRESHOLD=90

# ==============================================================================
# Helper Functions
# ==============================================================================

print_header() {
    echo ""
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
}

print_section() {
    echo ""
    echo -e "${YELLOW}▶ $1${NC}"
    echo "─────────────────────────────────────────────────────────────"
}

check_pass() {
    echo -e "${GREEN}✓${NC} $1"
}

check_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

check_fail() {
    echo -e "${RED}✗${NC} $1"
}

check_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

# ==============================================================================
# Phase 0: Location Check
# ==============================================================================

phase0_location_check() {
    print_header "Phase 0: Location Check"

    local hostname=$(hostname)
    local kernel=$(uname -r)

    echo "Hostname: $hostname"
    echo "Kernel: $kernel"

    if [[ "$hostname" == *"MacBook"* ]] || [[ "$hostname" == "localhost" ]]; then
        check_fail "ABORT: Running on local machine, not production!"
        exit 1
    fi

    check_pass "Running on production host: $hostname"
}

# ==============================================================================
# Phase 1: Ghost Hunter Scan
# ==============================================================================

phase1_ghost_hunter() {
    print_header "Phase 1: Ghost Hunter Scan"

    # 1.1 Zombie containers
    print_section "Zombie Containers"
    local zombie_count=$(sudo crictl ps -a 2>/dev/null | grep -v Running | grep -v CONTAINER | wc -l)
    if [[ $zombie_count -gt 0 ]]; then
        check_warn "Found $zombie_count non-running containers"
        sudo crictl ps -a | grep -v Running | head -20
    else
        check_pass "No zombie containers found"
    fi

    # 1.2 Problematic pods
    print_section "Problematic Pods"
    local problem_pods=$(sudo kubectl get pods --all-namespaces 2>/dev/null | grep -E "Error|Evicted|CrashLoopBackOff|ImagePullBackOff" | wc -l)
    if [[ $problem_pods -gt 0 ]]; then
        check_warn "Found $problem_pods problematic pods"
        sudo kubectl get pods --all-namespaces | grep -E "Error|Evicted|CrashLoopBackOff|ImagePullBackOff"
    else
        check_pass "No problematic pods found"
    fi

    # 1.3 Rogue cloudflared processes
    print_section "Cloudflare Tunnel Status"

    # Check systemd services
    local cf_systemd=$(sudo systemctl list-units --type=service 2>/dev/null | grep -i cloudflare | wc -l)
    if [[ $cf_systemd -gt 0 ]]; then
        check_fail "Found $cf_systemd rogue systemd cloudflared services!"
        sudo systemctl list-units --type=service | grep -i cloudflare
    else
        check_pass "No rogue systemd cloudflared services"
    fi

    # Check cloudflared processes
    local cf_procs=$(ps aux | grep cloudflared | grep -v grep | wc -l)
    check_info "Total cloudflared processes: $cf_procs"

    # Check K8s pods
    local cf_pods=$(sudo kubectl get pods -n cloudflare-tunnel 2>/dev/null | grep -c Running || echo 0)
    check_info "K8s cloudflared pods: $cf_pods"

    # 1.4 Port bindings
    print_section "Port 80/443 Bindings"
    sudo ss -tlnp | grep -E ':80|:443' || check_pass "No services on 80/443"
}

# ==============================================================================
# Phase 2: Storage Audit
# ==============================================================================

phase2_storage_audit() {
    print_header "Phase 2: Storage Audit"

    # 2.1 Disk usage
    print_section "Disk Usage"
    local disk_usage=$(df -h / | awk 'NR==2 {print $5}' | tr -d '%')
    local disk_avail=$(df -h / | awk 'NR==2 {print $4}')

    if [[ $disk_usage -ge $DISK_CRIT_THRESHOLD ]]; then
        check_fail "Root disk at ${disk_usage}% (CRITICAL) - Available: $disk_avail"
    elif [[ $disk_usage -ge $DISK_WARN_THRESHOLD ]]; then
        check_warn "Root disk at ${disk_usage}% (WARNING) - Available: $disk_avail"
    else
        check_pass "Root disk at ${disk_usage}% - Available: $disk_avail"
    fi

    df -h / /var/lib/containerd /var/lib/kubelet /var/log 2>/dev/null | head -10

    # 2.2 Inode usage
    print_section "Inode Usage"
    local inode_usage=$(df -i / | awk 'NR==2 {print $5}' | tr -d '%')

    if [[ $inode_usage -ge $INODE_CRIT_THRESHOLD ]]; then
        check_fail "Root inodes at ${inode_usage}% (CRITICAL)"
    elif [[ $inode_usage -ge $INODE_WARN_THRESHOLD ]]; then
        check_warn "Root inodes at ${inode_usage}% (WARNING)"
    else
        check_pass "Root inodes at ${inode_usage}%"
    fi

    # 2.3 PVC status
    print_section "PVC Status"
    local pending_pvcs=$(sudo kubectl get pvc --all-namespaces 2>/dev/null | grep -v Bound | grep -v STATUS | wc -l)
    if [[ $pending_pvcs -gt 0 ]]; then
        check_warn "Found $pending_pvcs pending PVCs"
        sudo kubectl get pvc --all-namespaces | grep -v Bound
    else
        check_pass "All PVCs bound"
    fi
}

# ==============================================================================
# Phase 3: Security Check
# ==============================================================================

phase3_security_check() {
    print_header "Phase 3: Security Check"

    # 3.1 Database exposure
    print_section "Database Port Exposure"

    if sudo ss -tlnp | grep -q "0.0.0.0:5432"; then
        check_fail "PostgreSQL exposed on 0.0.0.0:5432!"
    elif sudo ss -tlnp | grep -q ":5432"; then
        check_warn "PostgreSQL binding detected"
        sudo ss -tlnp | grep ":5432"
    else
        check_pass "PostgreSQL not exposed on host"
    fi

    if sudo ss -tlnp | grep -q "0.0.0.0:6379"; then
        check_fail "Redis exposed on 0.0.0.0:6379!"
    elif sudo ss -tlnp | grep -q ":6379"; then
        check_warn "Redis binding detected"
        sudo ss -tlnp | grep ":6379"
    else
        check_pass "Redis not exposed on host"
    fi

    # 3.2 K8s service types
    print_section "Service Type Audit"
    local nodeport_svcs=$(sudo kubectl get svc --all-namespaces 2>/dev/null | grep NodePort | wc -l)
    local loadbalancer_svcs=$(sudo kubectl get svc --all-namespaces 2>/dev/null | grep LoadBalancer | wc -l)

    if [[ $nodeport_svcs -gt 0 ]]; then
        check_warn "Found $nodeport_svcs NodePort services"
    fi
    if [[ $loadbalancer_svcs -gt 0 ]]; then
        check_warn "Found $loadbalancer_svcs LoadBalancer services"
    fi
    check_info "Use ClusterIP for internal services"
}

# ==============================================================================
# Phase 4: Config Integrity
# ==============================================================================

phase4_config_integrity() {
    print_header "Phase 4: Config Integrity"

    # 4.1 OIDC endpoints
    print_section "OIDC Endpoint Health"

    local oidc_response=$(curl -s -o /dev/null -w "%{http_code}" https://auth.madfam.io/.well-known/openid-configuration 2>/dev/null || echo "000")
    if [[ "$oidc_response" == "200" ]]; then
        check_pass "auth.madfam.io OIDC endpoint: $oidc_response"
    else
        check_fail "auth.madfam.io OIDC endpoint: $oidc_response"
    fi

    local api_oidc=$(curl -s -o /dev/null -w "%{http_code}" https://api.janua.dev/.well-known/openid-configuration 2>/dev/null || echo "000")
    if [[ "$api_oidc" == "200" ]]; then
        check_pass "api.janua.dev OIDC endpoint: $api_oidc"
    else
        check_fail "api.janua.dev OIDC endpoint: $api_oidc"
    fi

    # 4.1b OIDC Client Credentials Validation
    print_section "OIDC Client Credentials"

    local client_id=$(sudo kubectl -n enclii get secret enclii-oidc-credentials -o jsonpath='{.data.client-id}' 2>/dev/null | base64 -d || echo "")
    local client_secret=$(sudo kubectl -n enclii get secret enclii-oidc-credentials -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d || echo "")
    local placeholder_value="REPLACE_WITH_""ACTUAL_SECRET"

    if [[ -z "$client_id" ]] || [[ -z "$client_secret" ]]; then
        check_fail "OIDC credentials not found in K8s secret"
    elif [[ "$client_secret" == "$placeholder_value" || "$client_secret" == "example-client-secret" ]]; then
        check_fail "OIDC client secret is placeholder value!"
    else
        check_info "Client ID: ${client_id:0:15}..."
        # Test credentials with client_credentials grant
        local token_response=$(curl -s -o /dev/null -w "%{http_code}" \
            -X POST "https://auth.madfam.io/oauth/token" \
            -H "Content-Type: application/x-www-form-urlencoded" \
            -d "grant_type=client_credentials" \
            -d "client_id=$client_id" \
            -d "client_secret=$client_secret" \
            -d "scope=openid" 2>/dev/null || echo "000")

        if [[ "$token_response" == "200" ]]; then
            check_pass "OIDC client credentials VALID"
        elif [[ "$token_response" == "401" ]]; then
            check_fail "OIDC client credentials INVALID (secret mismatch)"
            check_info "Fix: Run ./scripts/rotate-oidc-secret.sh"
        else
            check_warn "OIDC credential check returned HTTP $token_response"
        fi
    fi

    # 4.2 Image pull policies
    print_section "Image Pull Policies"

    local ifnotpresent_count=$(sudo kubectl get deploy -A -o json 2>/dev/null | grep -c '"imagePullPolicy": "IfNotPresent"' || echo 0)
    local always_count=$(sudo kubectl get deploy -A -o json 2>/dev/null | grep -c '"imagePullPolicy": "Always"' || echo 0)

    check_info "Deployments with IfNotPresent: $ifnotpresent_count"
    check_info "Deployments with Always: $always_count"

    if [[ $ifnotpresent_count -gt 0 ]]; then
        check_warn "Consider using 'Always' for production deployments"
    fi

    # 4.3 External URL check
    print_section "External URL Configuration"

    local redis_external=$(sudo kubectl exec -n enclii deploy/switchyard-api -- printenv ENCLII_REDIS_URL 2>/dev/null | grep -cE '^redis://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' || echo 0)
    if [[ $redis_external -gt 0 ]]; then
        check_fail "switchyard-api using external Redis IP!"
    else
        check_pass "No external IPs in Redis config"
    fi
}

# ==============================================================================
# Phase 5: Summary Report
# ==============================================================================

phase5_summary() {
    print_header "Phase 5: Summary Report"

    echo ""
    echo "Namespaces:"
    sudo kubectl get ns --no-headers 2>/dev/null | awk '{print "  " $1 " (" $2 ")"}'

    echo ""
    echo "Node Status:"
    sudo kubectl get nodes 2>/dev/null

    echo ""
    echo "Docker Containers:"
    docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null | head -10

    echo ""
    echo "Recent Events (warnings/errors):"
    sudo kubectl get events --all-namespaces --sort-by='.lastTimestamp' 2>/dev/null | grep -E "Warning|Error" | tail -10
}

# ==============================================================================
# Cleanup helpers
# ==============================================================================

cleanup_zombies() {
    print_header "Cleanup: Zombie Containers"

    echo "Removing stopped containers..."
    sudo crictl rmp -a 2>/dev/null || true

    echo "Removing failed pods..."
    sudo kubectl delete pods --field-selector=status.phase=Failed -A 2>/dev/null || true

    echo "Removing evicted pods..."
    sudo kubectl delete pods --field-selector=status.phase=Evicted -A 2>/dev/null || true

    check_pass "Cleanup complete"
}

# ==============================================================================
# Main
# ==============================================================================

main() {
    local mode="${1:-full}"

    case "$mode" in
        --quick)
            phase0_location_check
            phase2_storage_audit
            phase3_security_check
            ;;
        --cleanup)
            phase0_location_check
            cleanup_zombies
            ;;
        --full|*)
            phase0_location_check
            phase1_ghost_hunter
            phase2_storage_audit
            phase3_security_check
            phase4_config_integrity
            phase5_summary
            ;;
    esac

    echo ""
    print_header "Diagnostic Complete"
    echo "Run with --cleanup to remove zombies and evicted pods"
}

main "$@"
