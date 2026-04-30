#!/bin/bash
# ==============================================================================
# smart-build.sh - Conservation Law Implementation (ADR-001 Law 6)
# ==============================================================================
# Detects changes in specific service directories to enable differential builds.
# Only rebuilds services that have actually changed.
#
# Usage:
#   ./scripts/smart-build.sh <service-path> [base-ref]
#
# Examples:
#   ./scripts/smart-build.sh apps/switchyard-api
#   ./scripts/smart-build.sh apps/admin-console HEAD~5
#   ./scripts/smart-build.sh packages/ui origin/main
#
# GitHub Actions Integration:
#   - name: Check for changes
#     id: changes
#     run: ./scripts/smart-build.sh apps/switchyard-api
#   - name: Build if changed
#     if: steps.changes.outputs.build == 'true'
#     run: make build-api
#
# Exit Codes:
#   0 - Success (build output set)
#   1 - Missing arguments
#   2 - Invalid service path
# ==============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Arguments
SERVICE_PATH="${1:-}"
BASE_REF="${2:-HEAD~1}"

# Validate arguments
if [[ -z "$SERVICE_PATH" ]]; then
    echo -e "${RED}Error: Service path required${NC}"
    echo "Usage: $0 <service-path> [base-ref]"
    echo "Example: $0 apps/switchyard-api"
    exit 1
fi

# Normalize path (remove trailing slash)
SERVICE_PATH="${SERVICE_PATH%/}"

# Check if path exists (allow for new services)
if [[ ! -d "$SERVICE_PATH" ]] && [[ ! -f "$SERVICE_PATH" ]]; then
    echo -e "${YELLOW}Warning: Path '$SERVICE_PATH' does not exist locally${NC}"
    echo -e "${YELLOW}Checking git history for changes...${NC}"
fi

# Detect changes
echo -e "${BLUE}=== Conservation Law Check ===${NC}"
echo -e "Service Path: ${GREEN}$SERVICE_PATH${NC}"
echo -e "Base Reference: ${GREEN}$BASE_REF${NC}"
echo ""

# Get changed files in the service path
CHANGED_FILES=$(git diff --name-only "$BASE_REF" -- "$SERVICE_PATH" 2>/dev/null || true)

# Also check for shared dependencies that should trigger rebuilds
SHARED_DEPS=""
case "$SERVICE_PATH" in
    apps/switchyard-ui|apps/admin-console)
        # Frontend apps depend on packages/ui
        SHARED_DEPS=$(git diff --name-only "$BASE_REF" -- "packages/ui" 2>/dev/null || true)
        ;;
    apps/*)
        # All apps might depend on shared packages
        SHARED_DEPS=$(git diff --name-only "$BASE_REF" -- "packages/core" 2>/dev/null || true)
        ;;
esac

# Combine changes
ALL_CHANGES="$CHANGED_FILES"
if [[ -n "$SHARED_DEPS" ]]; then
    ALL_CHANGES="$CHANGED_FILES
$SHARED_DEPS"
fi

# Filter empty lines
ALL_CHANGES=$(echo "$ALL_CHANGES" | grep -v '^$' || true)

if [[ -n "$ALL_CHANGES" ]]; then
    echo -e "${GREEN}Changes detected in $SERVICE_PATH:${NC}"
    echo "$ALL_CHANGES" | head -20

    CHANGE_COUNT=$(echo "$ALL_CHANGES" | wc -l | tr -d ' ')
    if [[ "$CHANGE_COUNT" -gt 20 ]]; then
        echo -e "${YELLOW}... and $((CHANGE_COUNT - 20)) more files${NC}"
    fi

    echo ""
    echo -e "${GREEN}Proceeding with build.${NC}"

    # GitHub Actions output
    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
        echo "build=true" >> "$GITHUB_OUTPUT"
        echo "changes=$CHANGE_COUNT" >> "$GITHUB_OUTPUT"
    else
        # Legacy output format for older runners
        echo "::set-output name=build::true"
        echo "::set-output name=changes::$CHANGE_COUNT"
    fi

    exit 0
else
    echo -e "${YELLOW}No changes detected in $SERVICE_PATH.${NC}"
    echo -e "${YELLOW}Skipping build (Conservation Law).${NC}"

    # GitHub Actions output
    if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
        echo "build=false" >> "$GITHUB_OUTPUT"
        echo "changes=0" >> "$GITHUB_OUTPUT"
    else
        # Legacy output format for older runners
        echo "::set-output name=build::false"
        echo "::set-output name=changes::0"
    fi

    exit 0
fi
