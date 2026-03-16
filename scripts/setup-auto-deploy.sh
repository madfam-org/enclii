#!/bin/bash
# Setup Auto-Deploy for MADFAM Ecosystem
# This script configures auto-deploy for all Janua, Enclii, and Solarpunk Foundry services
#
# Prerequisites:
# - ENCLII_API_TOKEN environment variable set
# - GITHUB_TOKEN environment variable set (for webhook configuration)
#
# Usage:
#   export ENCLII_API_TOKEN="your-token"
#   export GITHUB_TOKEN="your-github-pat"
#   ./scripts/setup-auto-deploy.sh

set -euo pipefail

# Configuration
API_ENDPOINT="${ENCLII_API_ENDPOINT:-https://api.enclii.dev}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check required environment variables
check_env() {
    if [[ -z "${ENCLII_API_TOKEN:-}" ]]; then
        log_error "ENCLII_API_TOKEN environment variable is not set"
        echo "Please set it with: export ENCLII_API_TOKEN=\"your-token\""
        exit 1
    fi

    if [[ -z "${GITHUB_TOKEN:-}" ]]; then
        log_warning "GITHUB_TOKEN environment variable is not set"
        log_warning "GitHub webhook configuration will be skipped"
    fi
}

# Check API health
check_api_health() {
    log_info "Checking API health at ${API_ENDPOINT}..."

    local health_response
    health_response=$(curl -s -w "\n%{http_code}" "${API_ENDPOINT}/health" 2>/dev/null || echo -e "\n000")
    local http_code=$(echo "$health_response" | tail -n1)

    if [[ "$http_code" != "200" ]]; then
        log_error "API health check failed (HTTP $http_code)"
        exit 1
    fi

    log_success "API is healthy"
}

# Create or get project
ensure_project() {
    local project_name="$1"
    local project_slug="$2"
    local git_repo="$3"

    log_info "Ensuring project '$project_slug' exists..."

    # Check if project exists
    local check_response
    check_response=$(curl -s -w "\n%{http_code}" \
        -H "Authorization: Bearer ${ENCLII_API_TOKEN}" \
        "${API_ENDPOINT}/v1/projects/${project_slug}" 2>/dev/null || echo -e "\n000")
    local http_code=$(echo "$check_response" | tail -n1)

    if [[ "$http_code" == "200" ]]; then
        log_success "Project '$project_slug' already exists"
        return 0
    fi

    # Create project
    log_info "Creating project '$project_slug'..."
    local create_response
    create_response=$(curl -s -w "\n%{http_code}" \
        -X POST \
        -H "Authorization: Bearer ${ENCLII_API_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"${project_name}\",
            \"slug\": \"${project_slug}\",
            \"git_repo\": \"${git_repo}\",
            \"default_branch\": \"main\"
        }" \
        "${API_ENDPOINT}/v1/projects" 2>/dev/null || echo -e "\n000")
    http_code=$(echo "$create_response" | tail -n1)

    if [[ "$http_code" == "201" || "$http_code" == "200" ]]; then
        log_success "Project '$project_slug' created"
        return 0
    else
        log_error "Failed to create project '$project_slug' (HTTP $http_code)"
        echo "$create_response" | head -n -1
        return 1
    fi
}

# Create production environment for a project
ensure_production_environment() {
    local project_slug="$1"

    log_info "Ensuring 'production' environment exists for '$project_slug'..."

    # Check if environment exists
    local check_response
    check_response=$(curl -s -w "\n%{http_code}" \
        -H "Authorization: Bearer ${ENCLII_API_TOKEN}" \
        "${API_ENDPOINT}/v1/projects/${project_slug}/environments/production" 2>/dev/null || echo -e "\n000")
    local http_code=$(echo "$check_response" | tail -n1)

    if [[ "$http_code" == "200" ]]; then
        log_success "Production environment already exists"
        return 0
    fi

    # Create environment
    log_info "Creating 'production' environment..."
    local create_response
    create_response=$(curl -s -w "\n%{http_code}" \
        -X POST \
        -H "Authorization: Bearer ${ENCLII_API_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "production",
            "slug": "production",
            "auto_deploy": true
        }' \
        "${API_ENDPOINT}/v1/projects/${project_slug}/environments" 2>/dev/null || echo -e "\n000")
    http_code=$(echo "$create_response" | tail -n1)

    if [[ "$http_code" == "201" || "$http_code" == "200" ]]; then
        log_success "Production environment created"
        return 0
    else
        log_warning "Failed to create production environment (HTTP $http_code) - may already exist"
        return 0
    fi
}

# Run services-sync for a project
sync_services() {
    local project_slug="$1"

    log_info "Syncing services for project '$project_slug'..."

    # Check if enclii CLI exists
    local cli_path="${ROOT_DIR}/bin/enclii"
    if [[ ! -x "$cli_path" ]]; then
        log_warning "Enclii CLI not found at $cli_path"
        log_warning "Please run 'make build-cli' first, or sync services manually"
        return 1
    fi

    # Run services-sync
    ENCLII_API_TOKEN="${ENCLII_API_TOKEN}" \
    "$cli_path" services-sync \
        --api-endpoint "${API_ENDPOINT}" \
        --api-token "${ENCLII_API_TOKEN}" \
        --dir "${ROOT_DIR}" \
        --project "$project_slug"

    log_success "Services synced for '$project_slug'"
}

# Configure GitHub webhook for a repository
configure_github_webhook() {
    local repo="$1"  # e.g., "madfam-org/janua"
    local webhook_url="${API_ENDPOINT}/v1/webhooks/github"

    if [[ -z "${GITHUB_TOKEN:-}" ]]; then
        log_warning "Skipping GitHub webhook configuration (GITHUB_TOKEN not set)"
        return 0
    fi

    log_info "Configuring GitHub webhook for $repo..."

    # Check if webhook already exists
    local existing_hooks
    existing_hooks=$(curl -s \
        -H "Authorization: token ${GITHUB_TOKEN}" \
        "https://api.github.com/repos/${repo}/hooks" 2>/dev/null || echo "[]")

    if echo "$existing_hooks" | grep -q "$webhook_url"; then
        log_success "Webhook already configured for $repo"
        return 0
    fi

    # Generate webhook secret (or retrieve from Enclii)
    local webhook_secret
    webhook_secret=$(openssl rand -hex 32)

    # Create webhook
    local create_response
    create_response=$(curl -s -w "\n%{http_code}" \
        -X POST \
        -H "Authorization: token ${GITHUB_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"web\",
            \"active\": true,
            \"events\": [\"push\", \"pull_request\"],
            \"config\": {
                \"url\": \"${webhook_url}\",
                \"content_type\": \"json\",
                \"secret\": \"${webhook_secret}\",
                \"insecure_ssl\": \"0\"
            }
        }" \
        "https://api.github.com/repos/${repo}/hooks" 2>/dev/null || echo -e "\n000")
    local http_code=$(echo "$create_response" | tail -n1)

    if [[ "$http_code" == "201" ]]; then
        log_success "Webhook configured for $repo"
        log_info "Webhook secret: $webhook_secret"
        log_warning "Please update the webhook secret in Enclii project settings!"
        return 0
    else
        log_error "Failed to configure webhook for $repo (HTTP $http_code)"
        echo "$create_response" | head -n -1
        return 1
    fi
}

# Main setup function
main() {
    echo ""
    echo "==========================================="
    echo "  MADFAM Auto-Deploy Setup"
    echo "==========================================="
    echo ""

    check_env
    check_api_health

    echo ""
    echo "--- Phase 1: Creating Projects ---"
    echo ""

    # Create Janua project
    ensure_project "Janua SSO" "janua" "https://github.com/madfam-org/janua"
    ensure_production_environment "janua"

    # Create Dhanam project (Wave 13: added for webhook automation)
    ensure_project "Dhanam Finance" "dhanam" "https://github.com/madfam-org/dhanam"
    ensure_production_environment "dhanam"

    # Create Solarpunk Foundry project
    ensure_project "Solarpunk Foundry" "solarpunk-foundry" "https://github.com/madfam-org/solarpunk-foundry"
    ensure_production_environment "solarpunk-foundry"

    # Ensure Enclii project exists
    ensure_project "Enclii Platform" "enclii" "https://github.com/madfam-org/enclii"
    ensure_production_environment "enclii"

    echo ""
    echo "--- Phase 2: Syncing Services ---"
    echo ""

    # Note: services-sync currently syncs all YAML files to a single project
    # For per-project sync, we'd need to filter the YAML files
    sync_services "enclii" || true

    echo ""
    echo "--- Phase 3: Configuring GitHub Webhooks ---"
    echo ""

    if [[ -n "${GITHUB_TOKEN:-}" ]]; then
        configure_github_webhook "madfam-org/janua" || true
        configure_github_webhook "madfam-org/dhanam" || true
        configure_github_webhook "madfam-org/solarpunk-foundry" || true
        configure_github_webhook "madfam-org/enclii" || true
    else
        log_warning "Skipping webhook configuration - GITHUB_TOKEN not set"
        echo ""
        echo "To configure webhooks later, run:"
        echo "  export GITHUB_TOKEN=\"your-github-pat\""
        echo "  ./scripts/setup-auto-deploy.sh"
    fi

    echo ""
    echo "==========================================="
    echo "  Setup Complete!"
    echo "==========================================="
    echo ""
    echo "Next steps:"
    echo "1. Verify services are registered: enclii services list"
    echo "2. Test auto-deploy by pushing a change to main branch"
    echo "3. Monitor deployments: enclii deployments list --watch"
    echo ""
}

# Run main function
main "$@"
