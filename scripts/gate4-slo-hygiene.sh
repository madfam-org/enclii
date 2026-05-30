#!/usr/bin/env bash
# Gate 4 — weekly SLO window hygiene (Enclii-first).
#
# Run during the 30-day Stability GA clock. Does not replace Prometheus SLO
# dashboards; captures public health + ops smoke evidence for the checkpoint log.
#
# Usage:
#   ./scripts/gate4-slo-hygiene.sh
#   ./scripts/gate4-slo-hygiene.sh --append-log   # append row to docs/production/GATE4_SLO_WINDOW_LOG.md

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APPEND_LOG=false
if [ "${1:-}" = "--append-log" ]; then
  APPEND_LOG=true
fi

API_URL="${API_URL:-https://api.enclii.dev}"
APP_URL="${APP_URL:-https://app.enclii.dev}"
STATUS_URL="${STATUS_URL:-https://status.enclii.dev}"
KUBE_CONTEXT="${KUBE_CONTEXT:-foundry}"

pass=0
fail=0
notes=()

record() {
  local ok="$1"
  local name="$2"
  local detail="${3:-}"
  if [ "$ok" = "1" ]; then
    pass=$((pass + 1))
    printf "PASS %s" "$name"
  else
    fail=$((fail + 1))
    printf "FAIL %s" "$name"
    notes+=("$name: $detail")
  fi
  if [ -n "$detail" ]; then
    printf " - %s" "$detail"
  fi
  printf "\n"
}

http_code() {
  local url="$1"
  shift || true
  curl -sS -L --max-time 15 -o /dev/null -w "%{http_code}" "$@" "$url" || echo "000"
}

checked_at="$(date -u +%Y-%m-%dT%H:%MZ)"
printf "Gate 4 SLO hygiene @ %s\n\n" "$checked_at"

code="$(http_code "${API_URL%/}/health/public")"
record "$([ "$code" = "200" ] && echo 1 || echo 0)" "public api health" "HTTP $code"

code="$(http_code "$APP_URL")"
record "$([ "$code" = "200" ] && echo 1 || echo 0)" "app ui" "HTTP $code"

code="$(http_code "$STATUS_URL")"
record "$([ "$code" = "200" ] && echo 1 || echo 0)" "status page" "HTTP $code"

# Signup surface (lightweight — avoids rate limits from full wizard smoke)
code="$(http_code "${API_URL%/}/v1/signup" -X POST -H 'Content-Type: application/json' -d '{"email":"bad"}')"
record "$([ "$code" = "400" ] || [ "$code" = "429" ] && echo 1 || echo 0)" "signup api enabled" "POST /v1/signup HTTP $code"

code="$(http_code "${APP_URL%/}/signup")"
record "$([ "$code" = "200" ] && echo 1 || echo 0)" "signup ui" "HTTP $code"

if kubectl --context "$KUBE_CONTEXT" get application core-services -n argocd \
  -o jsonpath='{.status.sync.status}{" "}{.status.health.status}' 2>/dev/null | grep -q 'Synced Healthy'; then
  record 1 "argocd core-services" "Synced/Healthy"
else
  sync_health="$(kubectl --context "$KUBE_CONTEXT" get application core-services -n argocd \
    -o jsonpath='sync={.status.sync.status} health={.status.health.status}' 2>/dev/null || echo unknown)"
  record 0 "argocd core-services" "$sync_health"
fi

eso_lines="$(kubectl --context "$KUBE_CONTEXT" get externalsecret -n enclii \
  -o jsonpath='{range .items[*]}{.status.conditions[0].reason}{"\n"}{end}' 2>/dev/null || true)"
eso_bad=0
if [ -n "$eso_lines" ]; then
  eso_bad="$(printf '%s\n' "$eso_lines" | awk '$1!="SecretSynced"{c++} END{print c+0}')"
fi
record "$([ "$eso_bad" = "0" ] && echo 1 || echo 0)" "enclii ESO sync" "${eso_bad} not SecretSynced"

printf "\npassed=%s failed=%s\n" "$pass" "$fail"

if [ "$APPEND_LOG" = true ]; then
  log_file="$ROOT/docs/production/GATE4_SLO_WINDOW_LOG.md"
  note_str="all green"
  if [ "${#notes[@]}" -gt 0 ]; then
    note_str="$(printf '%s; ' "${notes[@]}")"
    note_str="${note_str%; }"
  fi
  python3 - "$log_file" "$checked_at" "$pass" "$fail" "$note_str" <<'PY'
import sys
from pathlib import Path

path, checked_at, passed, failed, notes = sys.argv[1:]
row = f"| {checked_at} | {passed} | {failed} | {notes} | Ops |"
text = Path(path).read_text(encoding="utf-8")
anchor = "|------------|---------------|---------------|-------|-------|"
if anchor not in text:
    raise SystemExit(f"checkpoint table anchor missing in {path}")
text = text.replace(anchor, anchor + "\n" + row, 1)
Path(path).write_text(text, encoding="utf-8")
print(f"Appended checkpoint to {path}")
PY
fi

if [ "$fail" -gt 0 ]; then
  exit 1
fi
