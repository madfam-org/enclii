#!/usr/bin/env bash
# Bootstrap GitHub environment commercial-ga-staging for lifecycle proofs (O-13–O-15).
#
# Creates the environment if missing and reports which E2E secrets still need values.
# Secret values must come from throwaway Enclii projects — never commit them.
#
# Usage:
#   ./scripts/setup-commercial-ga-staging-env.sh
#   ./scripts/setup-commercial-ga-staging-env.sh --check-only

set -euo pipefail

REPO="${GITHUB_REPOSITORY:-madfam-org/enclii}"
CHECK_ONLY=false

while [ $# -gt 0 ]; do
  case "$1" in
    --check-only)
      CHECK_ONLY=true
      shift
      ;;
    -h|--help)
      sed -n '2,12p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 2
fi

ENV_NAME="commercial-ga-staging"
REQUIRED_SECRETS=(
  PREVIEW_E2E_TOKEN
  PREVIEW_E2E_SERVICE_ID
  DOMAIN_E2E_TOKEN
  DOMAIN_E2E_SERVICE_ID
  DOMAIN_E2E_ENVIRONMENT_ID
  STORAGE_E2E_TOKEN
  STORAGE_E2E_SERVICE_ID
  STORAGE_E2E_RELEASE_ID
)
OPTIONAL_SECRETS=(
  DOMAIN_E2E_DOMAIN
  STORAGE_E2E_ENVIRONMENT_NAME
)

echo "=== Commercial GA staging environment ==="
echo "repo=$REPO env=$ENV_NAME"

if ! gh api "repos/${REPO}/environments/${ENV_NAME}" >/dev/null 2>&1; then
  if [ "$CHECK_ONLY" = true ]; then
    echo "environment ${ENV_NAME} does not exist" >&2
    exit 1
  fi
  echo "creating environment ${ENV_NAME}..."
  gh api --method PUT "repos/${REPO}/environments/${ENV_NAME}" --input - <<'EOF'
{}
EOF
else
  echo "environment ${ENV_NAME} exists"
fi

present="$(gh secret list --env "$ENV_NAME" -R "$REPO" --json name -q '.[].name' 2>/dev/null | sort || true)"
missing_required=()
missing_optional=()

for secret in "${REQUIRED_SECRETS[@]}"; do
  if ! printf '%s\n' "$present" | grep -qx "$secret"; then
    missing_required+=("$secret")
  fi
done
for secret in "${OPTIONAL_SECRETS[@]}"; do
  if ! printf '%s\n' "$present" | grep -qx "$secret"; then
    missing_optional+=("$secret")
  fi
done

echo
echo "Required secrets present: $((${#REQUIRED_SECRETS[@]} - ${#missing_required[@]}))/${#REQUIRED_SECRETS[@]}"
if [ ${#missing_required[@]} -gt 0 ]; then
  echo "Missing required:"
  for s in "${missing_required[@]}"; do
    echo "  - $s"
  done
  echo
  if gh secret list -R "$REPO" --json name -q '.[].name' 2>/dev/null | grep -qx ENCLII_SYNTHETICS_BEARER_TOKEN; then
    echo "Hint: repo secret ENCLII_SYNTHETICS_BEARER_TOKEN exists — may map to PREVIEW_E2E_TOKEN / DOMAIN_E2E_TOKEN / STORAGE_E2E_TOKEN if scoped to throwaway project."
  fi
  echo "Set with: gh secret set NAME --env ${ENV_NAME} -R ${REPO}"
  echo "See docs/production/STAGING_SECRETS_SETUP.md"
fi

if [ ${#missing_optional[@]} -gt 0 ]; then
  echo
  echo "Optional secrets not set:"
  for s in "${missing_optional[@]}"; do
    echo "  - $s"
  done
fi

if [ ${#missing_required[@]} -eq 0 ]; then
  echo
  echo "Ready to run: gh workflow run commercial-ga-staging-proof -f bets=all"
  exit 0
fi

exit 1
