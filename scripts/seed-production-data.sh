#!/usr/bin/env bash
set -euo pipefail

# Seed production infrastructure data into Switchyard admin API.
# Idempotent: checks for existing entities before creating.
#
# Usage:
#   # Via dispatch proxy (requires valid dispatch_auth cookie):
#   API_BASE=https://admin.enclii.dev/api/admin ./scripts/seed-production-data.sh
#
#   # Direct to Switchyard with JWT:
#   API_BASE=https://api.enclii.dev/v1/admin AUTH_TOKEN=<jwt> ./scripts/seed-production-data.sh

API_BASE="${API_BASE:-https://api.enclii.dev/v1/admin}"
BEARER_TOKEN="${AUTH_TOKEN:-}"

auth_header=""
if [[ -n "$BEARER_TOKEN" ]]; then
  auth_header="Authorization: Bearer $BEARER_TOKEN"
fi

api() {
  local method="$1" path="$2"
  shift 2
  local args=(-s -w "\n%{http_code}" -H "Content-Type: application/json")
  [[ -n "$auth_header" ]] && args+=(-H "$auth_header")
  args+=(-X "$method" "${API_BASE}${path}" "$@")
  curl "${args[@]}"
}

parse_response() {
  local resp="$1"
  local body http_code
  http_code=$(echo "$resp" | tail -1)
  body=$(echo "$resp" | sed '$d')
  echo "$http_code|$body"
}

echo "=== Enclii Production Data Seed ==="
echo "API: $API_BASE"
echo ""

# --- 1. Clusters ---
echo "--- Clusters ---"

resp=$(api GET /clusters)
parsed=$(parse_response "$resp")
code=${parsed%%|*}
body=${parsed#*|}

if [[ "$code" != "200" ]]; then
  echo "ERROR: Cannot reach clusters endpoint (HTTP $code). Check auth."
  echo "$body"
  exit 1
fi

existing_clusters=$(echo "$body" | jq -r '.clusters[]?.name // empty' 2>/dev/null || true)

# Cluster: foundry-cp
if echo "$existing_clusters" | grep -qx "foundry-cp"; then
  echo "  foundry-cp: already exists"
  CORE_CLUSTER_ID=$(echo "$body" | jq -r '.clusters[] | select(.name=="foundry-cp") | .id')
else
  echo "  Creating foundry-cp..."
  resp=$(api POST /clusters -d '{
    "name": "foundry-cp",
    "slug": "foundry-cp",
    "type": "k3s",
    "endpoint": "https://37.27.235.104:6443",
    "region": "eu-central",
    "status": "ready",
    "metadata": {"k3s_version":"v1.33.7+k3s3","role":"control-plane","node_count":3}
  }')
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_c=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    CORE_CLUSTER_ID=$(echo "$body_c" | jq -r '.id')
    echo "  foundry-cp: created ($CORE_CLUSTER_ID)"
  else
    echo "  ERROR creating foundry-cp (HTTP $code): $body_c"
  fi
fi

# Cluster: foundry-builder-01
if echo "$existing_clusters" | grep -qx "foundry-builder-01"; then
  echo "  foundry-builder-01: already exists"
  BUILDER_CLUSTER_ID=$(echo "$body" | jq -r '.clusters[] | select(.name=="foundry-builder-01") | .id')
else
  echo "  Creating foundry-builder-01..."
  resp=$(api POST /clusters -d '{
    "name": "foundry-builder-01",
    "slug": "foundry-builder-01",
    "type": "k3s",
    "endpoint": "https://foundry-builder-01:6443",
    "region": "eu-central",
    "status": "ready",
    "metadata": {"k3s_version":"v1.33.7+k3s3","role":"worker","taints":["builder=true:NoSchedule"],"purpose":"CI builds + ARC runners"}
  }')
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_c=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    BUILDER_CLUSTER_ID=$(echo "$body_c" | jq -r '.id')
    echo "  foundry-builder-01: created ($BUILDER_CLUSTER_ID)"
  else
    echo "  ERROR creating foundry-builder-01 (HTTP $code): $body_c"
  fi
fi

echo ""

# --- 2. Bare Metal Hosts ---
echo "--- Bare Metal Hosts ---"

resp=$(api GET /fleet)
parsed=$(parse_response "$resp")
code=${parsed%%|*}
body=${parsed#*|}

existing_hosts=$(echo "$body" | jq -r '.hosts[]?.name // empty' 2>/dev/null || true)

# Host: foundry-cp
if echo "$existing_hosts" | grep -qx "foundry-cp"; then
  echo "  foundry-cp: already exists"
  CORE_BMH_ID=$(echo "$body" | jq -r '.hosts[] | select(.name=="foundry-cp") | .id')
else
  echo "  Registering foundry-cp..."
  resp=$(api POST /fleet -d "{
    \"name\": \"foundry-cp\",
    \"cluster_id\": \"${CORE_CLUSTER_ID:-}\",
    \"bmc_address\": \"https://robot.hetzner.com\",
    \"boot_mode\": \"UEFI\",
    \"state\": \"provisioned\",
    \"power_state\": \"on\",
    \"hardware_profile\": {\"cpu\":\"server-cpu\",\"cores\":0,\"threads\":0,\"ram_gb\":0,\"storage\":[],\"network\":\"1Gbit\"},
    \"cost_per_hour_cents\": 0
  }")
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_h=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    CORE_BMH_ID=$(echo "$body_h" | jq -r '.id')
    echo "  foundry-cp: registered ($CORE_BMH_ID)"
  else
    echo "  ERROR registering foundry-cp (HTTP $code): $body_h"
  fi
fi

# Host: the-forge
if echo "$existing_hosts" | grep -qx "the-forge"; then
  echo "  the-forge: already exists"
  FORGE_BMH_ID=$(echo "$body" | jq -r '.hosts[] | select(.name=="the-forge") | .id')
else
  echo "  Registering the-forge..."
  resp=$(api POST /fleet -d "{
    \"name\": \"the-forge\",
    \"cluster_id\": \"${BUILDER_CLUSTER_ID:-}\",
    \"bmc_address\": \"https://console.hetzner.cloud\",
    \"boot_mode\": \"UEFI\",
    \"state\": \"provisioned\",
    \"power_state\": \"on\",
    \"hardware_profile\": {\"cpu\":\"Shared vCPU\",\"cores\":2,\"threads\":2,\"ram_gb\":4,\"storage\":[{\"type\":\"SSD\",\"size_gb\":40}],\"network\":\"20TB traffic\",\"type\":\"VPS (CX22)\"},
    \"cost_per_hour_cents\": 1
  }")
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_h=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    FORGE_BMH_ID=$(echo "$body_h" | jq -r '.id')
    echo "  the-forge: registered ($FORGE_BMH_ID)"
  else
    echo "  ERROR registering the-forge (HTTP $code): $body_h"
  fi
fi

echo ""

# --- 3. Propagation Policy ---
echo "--- Propagation Policy ---"

resp=$(api GET /propagation)
parsed=$(parse_response "$resp")
code=${parsed%%|*}
body=${parsed#*|}

existing_policies=$(echo "$body" | jq -r '.policies[]?.name // empty' 2>/dev/null || true)

if echo "$existing_policies" | grep -qx "production-default"; then
  echo "  production-default: already exists"
else
  echo "  Creating production-default..."
  resp=$(api POST /propagation -d "{
    \"name\": \"production-default\",
    \"cluster_ids\": [\"${CORE_CLUSTER_ID:-}\"],
    \"resource_selectors\": [{\"matchLabels\":{\"env\":\"production\"}}],
    \"placement_strategy\": \"Binpack\",
    \"gpu_required\": false,
    \"priority\": 10
  }")
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_p=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    echo "  production-default: created"
  else
    echo "  ERROR creating policy (HTTP $code): $body_p"
  fi
fi

echo ""

# --- 4. Cost Allocations (January 2026) ---
echo "--- Cost Allocations ---"

resp=$(api POST /costs/allocate -d "{
  \"bare_metal_host_id\": \"${CORE_BMH_ID:-}\",
  \"tenant_id\": \"madfam\",
  \"period_start\": \"2026-01-01T00:00:00Z\",
  \"period_end\": \"2026-02-01T00:00:00Z\",
  \"cost_cents\": 5000,
  \"allocation_percent\": 100
}")
parsed=$(parse_response "$resp")
code=${parsed%%|*}
echo "  foundry-cp cost: HTTP $code"

resp=$(api POST /costs/allocate -d "{
  \"bare_metal_host_id\": \"${FORGE_BMH_ID:-}\",
  \"tenant_id\": \"madfam\",
  \"period_start\": \"2026-01-01T00:00:00Z\",
  \"period_end\": \"2026-02-01T00:00:00Z\",
  \"cost_cents\": 500,
  \"allocation_percent\": 100
}")
parsed=$(parse_response "$resp")
code=${parsed%%|*}
echo "  the-forge cost: HTTP $code"

echo ""
echo "=== Seed complete ==="
