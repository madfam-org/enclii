#!/usr/bin/env bash
#
# check-status-configmaps.sh
#
# Audit ST-3 enforcement: prevent silent drift between the two status-page
# configmaps that back status.enclii.dev and status.madfam.io.
#
# Why this script exists
# ----------------------
# `apps/status/k8s/enclii/configmap.yaml` (12 services) and
# `apps/status/k8s/madfam/configmap.yaml` (60+ services) are deployed
# independently. The smaller "enclii" file is a per-product status page,
# the larger "madfam" file is the platform-wide page. The expected
# invariant is that **every service shown on the per-product page must
# also appear on the platform-wide page** (matched by `url`, since the
# `name` legitimately differs — e.g. "Switchyard API" vs "Enclii API").
#
# Before this check, the YAML could drift silently — and on 2026-05-02
# the audit found exactly one drifted entry ("Analytics Proxy" was on
# enclii but missing from madfam). The check below catches that class
# of bug deterministically.
#
# What it verifies
# ----------------
#   1. Both configmaps' `data.services-config` is well-formed JSON.
#   2. Every (name, url) pair within each file is unique (no in-file dupes).
#   3. Every `url` in the enclii configmap is also present in the madfam
#      configmap (subset rule). The reverse is NOT enforced — madfam
#      is a strict superset by design.
#
# It deliberately does NOT try to re-derive the YAML from the Go source
# (`apps/switchyard-api/internal/api/status_handlers.go`). The Go
# regenerator merges in onboarded-project entries from the runtime
# database, so deterministic in-CI regeneration would require DB
# fixtures. See the audit doc for the design rationale.
#
# Performance: <1s on a clean checkout. Zero new dependencies (jq is
# already used elsewhere in CI).
#
# Exit codes:
#   0  all checks pass
#   1  drift / malformed JSON / duplicate detected

set -euo pipefail

# Resolve repo root regardless of where the script is invoked from.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENCLII_CM="$ROOT/apps/status/k8s/enclii/configmap.yaml"
MADFAM_CM="$ROOT/apps/status/k8s/madfam/configmap.yaml"

fail() { echo "❌ $*" >&2; exit 1; }
pass() { echo "✅ $*"; }

for f in "$ENCLII_CM" "$MADFAM_CM"; do
  [ -f "$f" ] || fail "missing configmap: $f"
done

# Extract the `services-config` block scalar from a v1 ConfigMap and emit
# the inner JSON to stdout. Uses awk rather than yq to keep the
# dependency footprint to (bash + jq + awk) — all of which are already
# present on every GitHub runner and self-hosted ARC runner.
extract_services_json() {
  local file="$1"
  awk '
    BEGIN { in_block = 0; indent = 0 }
    /^  services-config:[[:space:]]*\|/ { in_block = 1; next }
    in_block {
      # First content line establishes the block-scalar indent level.
      if (indent == 0) {
        match($0, /^[[:space:]]*/)
        indent = RLENGTH
        if (indent == 0) { in_block = 0; next }
      }
      # Block ends when we hit a line less-indented than the block content.
      if ($0 !~ /^[[:space:]]*$/ && match($0, /^[[:space:]]*/) && RLENGTH < indent) {
        in_block = 0; next
      }
      # Strip the block-scalar indent prefix.
      print substr($0, indent + 1)
    }
  ' "$file"
}

# Pull JSON out of both files.
ENCLII_JSON="$(extract_services_json "$ENCLII_CM")"
MADFAM_JSON="$(extract_services_json "$MADFAM_CM")"

[ -n "$ENCLII_JSON" ] || fail "could not extract services-config from $ENCLII_CM"
[ -n "$MADFAM_JSON" ] || fail "could not extract services-config from $MADFAM_CM"

# 1. JSON validity ------------------------------------------------------
echo "$ENCLII_JSON" | jq -e 'type == "array"' >/dev/null \
  || fail "enclii configmap services-config is not a JSON array"
echo "$MADFAM_JSON" | jq -e 'type == "array"' >/dev/null \
  || fail "madfam configmap services-config is not a JSON array"

ENCLII_COUNT=$(echo "$ENCLII_JSON" | jq 'length')
MADFAM_COUNT=$(echo "$MADFAM_JSON" | jq 'length')
pass "enclii configmap parses as JSON ($ENCLII_COUNT services)"
pass "madfam configmap parses as JSON ($MADFAM_COUNT services)"

# 2. In-file uniqueness -------------------------------------------------
DUP_NAMES_E=$(echo "$ENCLII_JSON" | jq -r '[.[] | .name] | (length - (unique | length))')
DUP_URLS_E=$(echo "$ENCLII_JSON" | jq -r '[.[] | .url]  | (length - (unique | length))')
DUP_NAMES_M=$(echo "$MADFAM_JSON" | jq -r '[.[] | .name] | (length - (unique | length))')
DUP_URLS_M=$(echo "$MADFAM_JSON" | jq -r '[.[] | .url]  | (length - (unique | length))')

[ "$DUP_NAMES_E" = "0" ] || fail "duplicate service names in enclii configmap ($DUP_NAMES_E dupes)"
[ "$DUP_URLS_E"  = "0" ] || fail "duplicate service urls in enclii configmap ($DUP_URLS_E dupes)"
[ "$DUP_NAMES_M" = "0" ] || fail "duplicate service names in madfam configmap ($DUP_NAMES_M dupes)"
[ "$DUP_URLS_M"  = "0" ] || fail "duplicate service urls in madfam configmap ($DUP_URLS_M dupes)"
pass "no in-file duplicates (name or url)"

# 3. enclii ⊆ madfam (by url) ------------------------------------------
# Compute the set difference: urls in enclii but not in madfam.
DRIFT=$(jq -n \
  --argjson e "$ENCLII_JSON" \
  --argjson m "$MADFAM_JSON" \
  '($e | map(.url)) - ($m | map(.url))')
DRIFT_COUNT=$(echo "$DRIFT" | jq 'length')

if [ "$DRIFT_COUNT" -ne 0 ]; then
  echo "❌ Drift detected: $DRIFT_COUNT enclii service(s) missing from madfam:" >&2
  echo "$DRIFT" | jq -r '.[]' | sed 's/^/   - /' >&2
  echo "" >&2
  echo "   Fix: add the missing entries to apps/status/k8s/madfam/configmap.yaml" >&2
  echo "   (the per-product page must be a subset of the platform-wide page)." >&2
  exit 1
fi
pass "enclii configmap is a strict subset of madfam configmap (by url)"

echo ""
pass "all status configmap drift checks passed"
