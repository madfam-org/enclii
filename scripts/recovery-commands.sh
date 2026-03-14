#!/bin/bash
# ==============================================================================
# PRODUCTION RECOVERY COMMANDS
# ==============================================================================
# Generated: 2026-01-17
# Purpose: Manual execution commands for production recovery
#
# IMPORTANT: SSH requires Cloudflare Access browser authentication:
#   cloudflared access ssh --hostname ssh.madfam.io
#
# After authentication, run these commands on the production host.
# ==============================================================================

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo_step() { echo -e "${BLUE}==>${NC} $1"; }
echo_pass() { echo -e "${GREEN}✓${NC} $1"; }
echo_fail() { echo -e "${RED}✗${NC} $1"; }
echo_warn() { echo -e "${YELLOW}⚠${NC} $1"; }

# ==============================================================================
# STEP 1: VERIFY PRODUCTION HOST
# ==============================================================================

verify_host() {
    echo_step "Verifying production host..."

    local hostname=$(hostname)
    if [[ "$hostname" == *"MacBook"* ]] || [[ "$hostname" == "localhost" ]]; then
        echo_fail "ABORT: Running on local machine, not production!"
        exit 1
    fi

    echo_pass "Confirmed on production host: $hostname"
}

# ==============================================================================
# STEP 2: CHECK CURRENT REDIS URL (DIAGNOSIS)
# ==============================================================================

check_redis_url() {
    echo_step "Checking current Redis URL in switchyard-api..."

    local redis_url=$(sudo kubectl exec -n enclii deploy/switchyard-api -- printenv ENCLII_REDIS_URL 2>/dev/null || echo "NOT_FOUND")

    echo "Current ENCLII_REDIS_URL: $redis_url"

    if echo "$redis_url" | grep -qE '^redis://[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+'; then
        echo_fail "EXTERNAL IP DETECTED! Redis URL uses public IP instead of internal K8s DNS"
        echo "Expected: redis://redis:6379 or redis://redis.data.svc.cluster.local:6379"
        return 1
    elif [[ "$redis_url" == "redis://redis:6379"* ]] || [[ "$redis_url" == *"redis.data.svc"* ]]; then
        echo_pass "Redis URL is correctly using internal K8s DNS"
        return 0
    else
        echo_warn "Unknown Redis URL format: $redis_url"
        return 2
    fi
}

# ==============================================================================
# STEP 3: FIX REDIS URL (ArgoCD Sync or Kubectl Patch)
# ==============================================================================

fix_redis_url() {
    echo_step "Fixing Redis URL via ArgoCD sync..."

    # Option A: Force ArgoCD sync (preferred - uses GitOps manifests)
    echo "Attempting ArgoCD sync for enclii namespace..."
    sudo kubectl -n argocd patch application core-services --type merge -p '{"operation":{"sync":{"force":true}}}'

    # Wait for sync
    echo "Waiting for ArgoCD sync to complete..."
    sleep 30

    # Check if fixed
    if check_redis_url; then
        echo_pass "Redis URL fixed via ArgoCD sync"
        return 0
    fi

    # Option B: Direct kubectl patch (if ArgoCD sync doesn't fix it)
    echo_warn "ArgoCD sync didn't fix it. Applying direct patch..."

    sudo kubectl set env deployment/switchyard-api -n enclii \
        ENCLII_REDIS_URL="redis://redis.data.svc.cluster.local:6379"

    echo_pass "Direct patch applied. Deployment will restart automatically."
}

# ==============================================================================
# STEP 4: RESTART SWITCHYARD-API
# ==============================================================================

restart_switchyard() {
    echo_step "Restarting switchyard-api deployment..."

    sudo kubectl rollout restart deployment/switchyard-api -n enclii

    echo "Waiting for rollout to complete..."
    sudo kubectl rollout status deployment/switchyard-api -n enclii --timeout=180s

    # Verify health
    echo "Checking switchyard-api health..."
    local health=$(sudo kubectl exec -n enclii deploy/switchyard-api -- wget -qO- http://localhost:4200/health 2>/dev/null || echo "FAILED")

    if [[ "$health" == *"ok"* ]] || [[ "$health" == *"healthy"* ]]; then
        echo_pass "switchyard-api is healthy"
    else
        echo_warn "Health check returned: $health"
    fi
}

# ==============================================================================
# STEP 5: LOCK DATABASE PORTS (Security Fix)
# ==============================================================================

lock_database_ports() {
    echo_step "Checking database port exposure..."

    # Check PostgreSQL
    local pg_exposed=$(sudo ss -tlnp | grep -c "0.0.0.0:5432" || echo 0)
    if [[ $pg_exposed -gt 0 ]]; then
        echo_fail "PostgreSQL is exposed on 0.0.0.0:5432!"
        echo "  Fix: Edit Docker compose to bind 127.0.0.1:5432:5432 instead of 0.0.0.0:5432:5432"
        echo "  Or migrate to K8s-only database access"
    else
        echo_pass "PostgreSQL not exposed on public interface"
    fi

    # Check Redis
    local redis_exposed=$(sudo ss -tlnp | grep -c "0.0.0.0:6379" || echo 0)
    if [[ $redis_exposed -gt 0 ]]; then
        echo_fail "Redis is exposed on 0.0.0.0:6379!"
        echo "  Fix: Edit Docker compose to bind 127.0.0.1:6379:6379 instead of 0.0.0.0:6379:6379"
    else
        echo_pass "Redis not exposed on public interface"
    fi

    echo ""
    echo "To fix exposed ports, locate the Docker Compose file and change:"
    echo "  ports:"
    echo "    - \"0.0.0.0:5432:5432\"  # INSECURE"
    echo "  To:"
    echo "  ports:"
    echo "    - \"127.0.0.1:5432:5432\"  # SECURE"
}

# ==============================================================================
# STEP 6: CLEANUP ZOMBIE CONTAINERS AND EVICTED PODS
# ==============================================================================

cleanup_zombies() {
    echo_step "Cleaning up zombie containers and evicted pods..."

    # Remove stopped containers
    echo "Removing stopped containers..."
    sudo crictl rmp -a 2>/dev/null || true

    # Remove failed pods
    echo "Removing failed pods..."
    sudo kubectl delete pods --field-selector=status.phase=Failed -A 2>/dev/null || true

    # Remove evicted pods
    echo "Removing evicted pods..."
    for ns in $(sudo kubectl get ns -o jsonpath='{.items[*].metadata.name}'); do
        sudo kubectl delete pods -n "$ns" --field-selector=status.phase=Evicted 2>/dev/null || true
    done

    echo_pass "Cleanup complete"
}

# ==============================================================================
# STEP 7: STOP ROGUE SYSTEMD CLOUDFLARED SERVICES
# ==============================================================================

stop_rogue_tunnels() {
    echo_step "Checking for rogue systemd cloudflared services..."

    local cf_services=$(sudo systemctl list-units --type=service 2>/dev/null | grep -i cloudflare | wc -l)

    if [[ $cf_services -gt 0 ]]; then
        echo_fail "Found $cf_services rogue systemd cloudflared services"
        sudo systemctl list-units --type=service | grep -i cloudflare

        echo ""
        echo "To stop rogue tunnels, run:"
        echo "  sudo systemctl stop cloudflared.service cloudflared-janua.service"
        echo "  sudo systemctl disable cloudflared.service cloudflared-janua.service"
    else
        echo_pass "No rogue systemd cloudflared services found"
    fi

    # Check K8s cloudflared pods (these are expected)
    local cf_pods=$(sudo kubectl get pods -n cloudflare-tunnel 2>/dev/null | grep -c Running || echo 0)
    echo "K8s cloudflared pods running: $cf_pods (expected: 2-4)"
}

# ==============================================================================
# STEP 8: VERIFY OIDC ENDPOINTS
# ==============================================================================

verify_oidc() {
    echo_step "Verifying OIDC endpoints..."

    # auth.madfam.io
    local auth_status=$(curl -s -o /dev/null -w "%{http_code}" https://auth.madfam.io/.well-known/openid-configuration 2>/dev/null || echo "000")
    if [[ "$auth_status" == "200" ]]; then
        echo_pass "auth.madfam.io OIDC endpoint: $auth_status"
    else
        echo_fail "auth.madfam.io OIDC endpoint: $auth_status"
    fi

    # api.janua.dev
    local api_status=$(curl -s -o /dev/null -w "%{http_code}" https://api.janua.dev/.well-known/openid-configuration 2>/dev/null || echo "000")
    if [[ "$api_status" == "200" ]]; then
        echo_pass "api.janua.dev OIDC endpoint: $api_status"
    else
        echo_fail "api.janua.dev OIDC endpoint: $api_status"
    fi
}

# ==============================================================================
# STEP 9: DISK PRESSURE CHECK
# ==============================================================================

check_disk_pressure() {
    echo_step "Checking disk pressure..."

    local disk_usage=$(df -h / | awk 'NR==2 {print $5}' | tr -d '%')
    local disk_avail=$(df -h / | awk 'NR==2 {print $4}')

    if [[ $disk_usage -ge 85 ]]; then
        echo_fail "Root disk at ${disk_usage}% (CRITICAL) - Available: $disk_avail"
        echo "  Run cleanup_zombies to free space"
        echo "  Also consider: sudo crictl rmi --prune"
    elif [[ $disk_usage -ge 75 ]]; then
        echo_warn "Root disk at ${disk_usage}% (WARNING) - Available: $disk_avail"
    else
        echo_pass "Root disk at ${disk_usage}% - Available: $disk_avail"
    fi
}

# ==============================================================================
# MAIN
# ==============================================================================

main() {
    local cmd="${1:-all}"

    echo ""
    echo "═══════════════════════════════════════════════════════════════"
    echo "  ENCLII PRODUCTION RECOVERY COMMANDS"
    echo "═══════════════════════════════════════════════════════════════"
    echo ""

    case "$cmd" in
        verify)
            verify_host
            ;;
        check-redis)
            verify_host
            check_redis_url
            ;;
        fix-redis)
            verify_host
            fix_redis_url
            ;;
        restart)
            verify_host
            restart_switchyard
            ;;
        lock-ports)
            verify_host
            lock_database_ports
            ;;
        cleanup)
            verify_host
            cleanup_zombies
            ;;
        stop-tunnels)
            verify_host
            stop_rogue_tunnels
            ;;
        verify-oidc)
            verify_host
            verify_oidc
            ;;
        disk)
            verify_host
            check_disk_pressure
            ;;
        all)
            verify_host
            echo ""
            check_disk_pressure
            echo ""
            check_redis_url || true
            echo ""
            verify_oidc
            echo ""
            stop_rogue_tunnels
            echo ""
            lock_database_ports
            echo ""
            echo "═══════════════════════════════════════════════════════════════"
            echo "  RECOVERY COMPLETE - Review issues above"
            echo "═══════════════════════════════════════════════════════════════"
            echo ""
            echo "Next steps if issues found:"
            echo "  ./recovery-commands.sh fix-redis      # Fix Redis URL"
            echo "  ./recovery-commands.sh restart        # Restart switchyard-api"
            echo "  ./recovery-commands.sh cleanup        # Remove zombie pods"
            echo "  ./recovery-commands.sh stop-tunnels   # Stop rogue cloudflared"
            ;;
        *)
            echo "Usage: $0 {verify|check-redis|fix-redis|restart|lock-ports|cleanup|stop-tunnels|verify-oidc|disk|all}"
            exit 1
            ;;
    esac
}

main "$@"
