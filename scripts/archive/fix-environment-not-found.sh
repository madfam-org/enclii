#!/bin/bash
# fix-environment-not-found.sh
# Fixes the "environment not found" error for auto-deploy
#
# Problem: Auto-deploy fails because no "production" environment exists in DB
# Solution: Create production environments for existing projects
#
# Usage:
#   ./scripts/fix-environment-not-found.sh check   # Show current state
#   ./scripts/fix-environment-not-found.sh fix     # Create missing environments
#   ./scripts/fix-environment-not-found.sh api     # Fix via API (requires TOKEN env)

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Configuration
API_URL="${API_URL:-https://api.enclii.dev}"
NAMESPACE="${NAMESPACE:-enclii}"
KUBE_CONTEXT="${KUBE_CONTEXT:-enclii}"

# Helper function to run SQL via kubectl
run_sql() {
    local sql="$1"
    kubectl exec -n "$NAMESPACE" deploy/postgres -- psql -U postgres -d enclii -t -c "$sql" 2>/dev/null || \
    kubectl exec -n "$NAMESPACE" deploy/switchyard-api -- psql -t -c "$sql" 2>/dev/null
}

# Check current state
check_state() {
    echo -e "${YELLOW}=== Checking Current State ===${NC}"

    echo -e "\n${GREEN}Projects:${NC}"
    run_sql "SELECT id, name, slug FROM projects;"

    echo -e "\n${GREEN}Environments:${NC}"
    run_sql "SELECT e.id, p.slug as project, e.name, e.kube_namespace FROM environments e JOIN projects p ON e.project_id = p.id;"

    echo -e "\n${GREEN}Services with auto_deploy:${NC}"
    run_sql "SELECT s.id, s.name, p.slug as project, s.auto_deploy FROM services s JOIN projects p ON s.project_id = p.id WHERE s.auto_deploy = true;"

    echo -e "\n${GREEN}Pending Releases:${NC}"
    run_sql "SELECT r.id, s.name as service, r.version, r.status FROM releases r JOIN services s ON r.service_id = s.id WHERE r.status IN ('pending', 'ready') ORDER BY r.created_at DESC LIMIT 10;"
}

# Fix via SQL (direct DB)
fix_via_sql() {
    echo -e "${YELLOW}=== Creating Production Environments ===${NC}"

    # Get list of projects without production environment
    local projects_without_env=$(run_sql "
        SELECT p.id, p.slug
        FROM projects p
        WHERE NOT EXISTS (
            SELECT 1 FROM environments e
            WHERE e.project_id = p.id AND e.name = 'production'
        );
    ")

    if [ -z "$projects_without_env" ]; then
        echo -e "${GREEN}All projects already have production environments!${NC}"
        return 0
    fi

    echo -e "Projects missing production environment:"
    echo "$projects_without_env"

    # Create production environments for all projects that don't have one
    echo -e "\n${YELLOW}Creating production environments...${NC}"
    run_sql "
        INSERT INTO environments (id, project_id, name, kube_namespace, created_at, updated_at)
        SELECT
            gen_random_uuid(),
            p.id,
            'production',
            'enclii',
            NOW(),
            NOW()
        FROM projects p
        WHERE NOT EXISTS (
            SELECT 1 FROM environments e
            WHERE e.project_id = p.id AND e.name = 'production'
        );
    "

    echo -e "${GREEN}Done! Verifying...${NC}"

    # Verify
    run_sql "SELECT e.id, p.slug as project, e.name, e.kube_namespace FROM environments e JOIN projects p ON e.project_id = p.id WHERE e.name = 'production';"

    echo -e "\n${GREEN}Production environments created successfully!${NC}"
}

# Fix via API
fix_via_api() {
    if [ -z "$TOKEN" ]; then
        echo -e "${RED}ERROR: TOKEN environment variable required${NC}"
        echo "Usage: TOKEN=\$JWT_TOKEN ./scripts/fix-environment-not-found.sh api"
        exit 1
    fi

    echo -e "${YELLOW}=== Creating Environments via API ===${NC}"

    # Get project slugs
    local projects=$(curl -s -H "Authorization: Bearer $TOKEN" "$API_URL/v1/projects" | jq -r '.projects[].slug')

    for slug in $projects; do
        echo -e "\n${YELLOW}Creating production environment for project: $slug${NC}"

        response=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/v1/projects/$slug/environments" \
            -H "Authorization: Bearer $TOKEN" \
            -H "Content-Type: application/json" \
            -d '{"name": "production", "kube_namespace": "enclii"}')

        http_code=$(echo "$response" | tail -n1)
        body=$(echo "$response" | head -n-1)

        case $http_code in
            201)
                echo -e "${GREEN}Created: $body${NC}"
                ;;
            409)
                echo -e "${YELLOW}Already exists (skipping)${NC}"
                ;;
            *)
                echo -e "${RED}Failed (HTTP $http_code): $body${NC}"
                ;;
        esac
    done

    echo -e "\n${GREEN}Done!${NC}"
}

# Verify reconciler is working
verify_reconciler() {
    echo -e "${YELLOW}=== Verifying Reconciler ===${NC}"

    echo -e "\n${GREEN}Checking reconciler pod:${NC}"
    kubectl get pods -n "$NAMESPACE" -l app=switchyard-api

    echo -e "\n${GREEN}Recent reconciler logs:${NC}"
    kubectl logs -n "$NAMESPACE" deploy/switchyard-api --tail=50 2>/dev/null | grep -i "reconcil\|deploy\|environment" || echo "(no matching logs)"

    echo -e "\n${GREEN}Checking pending releases:${NC}"
    run_sql "SELECT r.id, s.name, r.version, r.status, r.created_at FROM releases r JOIN services s ON r.service_id = s.id WHERE r.status = 'ready' ORDER BY r.created_at DESC LIMIT 5;"
}

# Main
case "${1:-check}" in
    check)
        check_state
        ;;
    fix)
        fix_via_sql
        ;;
    api)
        fix_via_api
        ;;
    verify)
        verify_reconciler
        ;;
    *)
        echo "Usage: $0 {check|fix|api|verify}"
        echo ""
        echo "Commands:"
        echo "  check   - Show current state (projects, environments, services)"
        echo "  fix     - Create missing production environments via SQL"
        echo "  api     - Create environments via API (requires TOKEN env)"
        echo "  verify  - Check reconciler status and pending releases"
        exit 1
        ;;
esac
