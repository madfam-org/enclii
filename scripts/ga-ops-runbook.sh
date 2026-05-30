#!/usr/bin/env bash
# Commercial GA ops orchestrator — ROI-ordered dry-run or apply pass.
#
# Runs public proof, post-deploy adapter smoke (when token set), then Wave 0/1
# Enclii-first ops scripts. Dry-run by default.
#
# Usage:
#   export ENCLII_API_TOKEN=...   # admin token for mutating ops
#   ./scripts/ga-ops-runbook.sh
#   ./scripts/ga-ops-runbook.sh --apply --disk-prune --backup-drill --reason "Commercial GA close"

set -euo pipefail

APPLY=false
DISK_PRUNE=false
BACKUP_DRILL=false
REASON=""
SKIP_PUBLIC=false

while [ $# -gt 0 ]; do
  case "$1" in
    --apply)
      APPLY=true
      shift
      ;;
    --reason)
      REASON="${2:-}"
      shift 2
      ;;
    --disk-prune)
      DISK_PRUNE=true
      shift
      ;;
    --backup-drill)
      BACKUP_DRILL=true
      shift
      ;;
    --skip-public)
      SKIP_PUBLIC=true
      shift
      ;;
    -h|--help)
      sed -n '2,14p' "$0" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [ "$APPLY" = true ] && [ -z "$REASON" ]; then
  echo "--reason is required with --apply" >&2
  exit 2
fi

WAVE0_ARGS=()
WAVE1_ARGS=()
if [ "$APPLY" = true ]; then
  WAVE0_ARGS+=(--apply --reason "$REASON")
  WAVE1_ARGS+=(--apply --reason "$REASON")
fi
if [ "$DISK_PRUNE" = true ]; then
  WAVE0_ARGS+=(--disk-prune)
fi
if [ "$BACKUP_DRILL" = true ]; then
  WAVE1_ARGS+=(--backup-drill)
fi

echo "=== Commercial GA ops runbook (apply=$APPLY) ==="

if [ "$SKIP_PUBLIC" = false ]; then
  echo
  echo "--- Public commercial proof ---"
  bash scripts/commercial-ga-proof.sh
fi

echo
echo "--- Staging environment readiness ---"
bash scripts/setup-commercial-ga-staging-env.sh --check-only || true

  echo
  echo "--- Post-deploy adapter smoke ---"
  if [ -n "${ENCLII_API_TOKEN:-${ENCLII_SYNTHETICS_TOKEN:-}}" ]; then
    bash scripts/post-deploy-ga-adapters.sh
  else
    bash scripts/post-deploy-ga-adapters.sh --public-only
  fi

echo
echo "--- Security release smoke (O-3 automatable) ---"
bash scripts/security-release-smoke.sh || true

echo
echo "--- Wave 0 (O-2–O-6) ---"
bash scripts/wave0-ga-ops.sh "${WAVE0_ARGS[@]}"

echo
echo "--- Wave 1 (O-8–O-11) ---"
bash scripts/wave1-ga-ops.sh "${WAVE1_ARGS[@]}"

echo
echo "Manual blockers remain: O-3 SECURITY_RELEASE_PR steps 2–3, O-9/O-10 secrets in Vault."
echo "Record SLO start (O-12) in docs/production/GA_READINESS_SCORECARD.md when Wave 1 passes."
