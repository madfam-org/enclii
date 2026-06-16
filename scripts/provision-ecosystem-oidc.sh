#!/usr/bin/env bash
# Auto-provision MADFAM ecosystem OIDC credentials (Janua → Vault).
#
# Requires: enclii login as admin@madfam.io, ENCLII_API_ENDPOINT=https://api.enclii.dev
#
# Usage:
#   ./scripts/provision-ecosystem-oidc.sh dhanam
#   ./scripts/provision-ecosystem-oidc.sh --all
#
# Last Updated: 2026-06-16
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CLI="${ENCLII_BIN:-enclii}"
export ENCLII_API_ENDPOINT="${ENCLII_API_ENDPOINT:-https://api.enclii.dev}"

if [[ "${1:-}" == "--all" ]]; then
  exec "$CLI" secrets provision oidc --all --reason "${REASON:-ecosystem oidc auto-provision}" "${@:2}"
fi

PLATFORM="${1:-dhanam}"
shift || true
exec "$CLI" secrets provision oidc --platform "$PLATFORM" --reason "${REASON:-ecosystem oidc auto-provision}" "$@"
