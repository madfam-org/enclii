#!/usr/bin/env bash
# Wave 1 Commercial GA ops — Stability GA orchestration (O-8 through O-11).
#
# Dry-run by default. Pass --apply to execute mutating ops (requires admin token + --reason).
#
# Usage:
#   export ENCLII_API_TOKEN=...   # admin token
#   ./scripts/wave1-ga-ops.sh
#   ./scripts/wave1-ga-ops.sh --apply --reason "Commercial GA Wave 1 stability"
#   ./scripts/wave1-ga-ops.sh --apply --backup-drill --reason "Commercial GA O-9 restore drill"

set -euo pipefail

APPLY=false
REASON=""
BACKUP_DRILL=false

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
    --backup-drill)
      BACKUP_DRILL=true
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

echo "=== Wave 1 GA ops (apply=$APPLY) ==="

echo
echo "--- Stability verify (read-only) ---"
enclii admin ga-verify --stability

echo
echo "--- ArgoCD sync sweep (O-8) ---"
if [ "$APPLY" = true ]; then
  enclii ops apps sync-sweep -n argocd --apply --reason "$REASON"
else
  enclii ops apps sync-sweep -n argocd
  enclii ops apps diff -n argocd
fi

echo
echo "--- Backup / restore drill (O-9) ---"
enclii ops jobs list -n data
if [ "$BACKUP_DRILL" = true ] || [ "$APPLY" = true ]; then
  if [ "$APPLY" = true ]; then
    for job in github-repos-backup cloudflare-config-backup postgres-restore-drill; do
      if enclii ops jobs list "$job" -n data --json 2>/dev/null | grep -q '"name"'; then
        enclii ops jobs trigger "$job" -n data --apply --reason "$REASON"
      else
        echo "skip $job (CronJob not found in data namespace)"
      fi
    done
  else
    echo "Pass --backup-drill with --apply to trigger backup/restore drill CronJobs (O-9)."
  fi
else
  echo "Skipped backup drill triggers. Use --backup-drill --apply --reason when secrets are provisioned."
fi

echo
echo "--- Vault / ESO readiness (O-10) ---"
enclii ops secrets vault
enclii ops secrets external -n enclii
if [ "$APPLY" = true ]; then
  enclii ops secrets sync-sweep --apply --reason "$REASON"
else
  enclii ops secrets sync-sweep
fi

echo
echo "--- Cosign / policy surface (O-11) ---"
enclii ops policy violations
if [ "$APPLY" = true ]; then
  enclii ops policy cosign-enable --apply --reason "$REASON"
else
  enclii ops policy cosign-enable
fi

echo
echo "--- Longhorn StorageClass reconcile ---"
if [ "$APPLY" = true ]; then
  enclii ops storage storageclass-apply --apply --reason "$REASON"
else
  enclii ops storage storageclass-apply
fi

echo
echo "--- SLO clock (O-12) ---"
echo "After O-8–O-11 pass, record SLO start date in docs/production/GA_READINESS_SCORECARD.md Gate 4."

if [ "$APPLY" = true ]; then
  echo
  echo "Wave 1 mutating ops submitted. Re-run: enclii admin ga-verify --stability"
fi
