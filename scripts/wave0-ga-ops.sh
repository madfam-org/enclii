#!/usr/bin/env bash
# Wave 0 Commercial GA ops — Enclii-first orchestration (O-3 through O-6).
#
# Dry-run by default. Pass --apply to execute mutating ops (requires admin token + --reason).
#
# Usage:
#   export ENCLII_API_TOKEN=...   # admin token
#   ./scripts/wave0-ga-ops.sh
#   ./scripts/wave0-ga-ops.sh --apply --reason "Commercial GA Wave 0 close"

set -euo pipefail

APPLY=false
REASON=""
DISK_PRUNE=false

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

if ! command -v enclii >/dev/null 2>&1; then
  echo "enclii CLI not found in PATH; build with: cd packages/cli && go build -o enclii ./cmd/enclii" >&2
  exit 1
fi

if [ "$APPLY" = true ] && [ -z "$REASON" ]; then
  echo "--reason is required with --apply" >&2
  exit 2
fi

echo "=== Wave 0 GA ops (apply=$APPLY) ==="

echo
echo "--- Gate 1 verify (read-only) ---"
enclii admin ga-verify

echo
echo "--- DB schema (O-2) ---"
enclii db schema

echo
echo "--- Longhorn CPU settings (O-5) ---"
if [ "$APPLY" = true ]; then
  enclii ops storage settings-apply --apply --reason "$REASON"
else
  enclii ops storage settings-apply
fi

echo
echo "--- Longhorn detached orphan prune (O-4) ---"
if [ "$APPLY" = true ]; then
  enclii ops storage prune-detached --apply --reason "$REASON"
else
  enclii ops storage prune-detached
fi

echo
echo "--- Disk prune via node-maintenance CronJob (O-6) ---"
if [ "$DISK_PRUNE" = true ] || [ "$APPLY" = true ]; then
  if [ "$APPLY" = true ]; then
    enclii ops jobs trigger node-maintenance -n enclii --apply --reason "$REASON"
  else
    enclii ops jobs list node-maintenance -n enclii
    echo "Pass --disk-prune with --apply to trigger node-maintenance once (O-6)."
  fi
else
  enclii ops jobs list node-maintenance -n enclii
  echo "Skipped trigger (daily schedule 02:30 UTC). Use --disk-prune --apply --reason to run now."
fi

echo
echo "--- Manual SECURITY_RELEASE_PR items (O-3) ---"
echo "Complete manual steps 2–3 in docs/production/SECURITY_RELEASE_PR.md, then sign Gate 1."

if [ "$APPLY" = true ]; then
  echo
  echo "Wave 0 mutating ops submitted. Re-run: enclii admin ga-verify"
fi
