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

# Node identity (hostnames, IPs, hardware SKUs) is private and must not live in
# this public repo — see docs/PUBLIC_REPO_BOUNDARY.md. The names below are the
# real cluster/host records this script seeds, so they are REQUIRED inputs:
# resolve them from internal-devops/infrastructure/nodes.md and export them
# before running. `:?` makes the script fail loudly rather than seed a wrong
# entity under a placeholder name.
#
#   CONTROL_PLANE_NODE=... BUILDER_NODE=... ./scripts/seed-production-data.sh
CONTROL_PLANE_NODE="${CONTROL_PLANE_NODE:?set to the control-plane node name (see internal-devops/infrastructure/nodes.md)}"
BUILDER_NODE="${BUILDER_NODE:?set to the builder node name (see internal-devops/infrastructure/nodes.md)}"
BUILDER_ENDPOINT="${BUILDER_ENDPOINT:-https://${BUILDER_NODE}:6443}"

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

# Cluster: control-plane node
if echo "$existing_clusters" | grep -qx "$CONTROL_PLANE_NODE"; then
  echo "  $CONTROL_PLANE_NODE: already exists"
  CORE_CLUSTER_ID=$(echo "$body" | jq -r --arg n "$CONTROL_PLANE_NODE" '.clusters[] | select(.name==$n) | .id')
else
  echo "  Creating $CONTROL_PLANE_NODE..."
  resp=$(api POST /clusters -d "{
    \"name\": \"${CONTROL_PLANE_NODE}\",
    \"slug\": \"${CONTROL_PLANE_NODE}\",
    \"type\": \"k3s\",
    \"endpoint\": \"${CONTROL_PLANE_ENDPOINT:-https://<CONTROL_PLANE_IP>:6443}\",
    \"region\": \"eu-central\",
    \"status\": \"ready\",
    \"metadata\": {\"k3s_version\":\"v1.33.7+k3s3\",\"role\":\"control-plane\",\"node_count\":3}
  }")
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_c=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    CORE_CLUSTER_ID=$(echo "$body_c" | jq -r '.id')
    echo "  $CONTROL_PLANE_NODE: created ($CORE_CLUSTER_ID)"
  else
    echo "  ERROR creating $CONTROL_PLANE_NODE (HTTP $code): $body_c"
  fi
fi

# Cluster: builder node
if echo "$existing_clusters" | grep -qx "$BUILDER_NODE"; then
  echo "  $BUILDER_NODE: already exists"
  BUILDER_CLUSTER_ID=$(echo "$body" | jq -r --arg n "$BUILDER_NODE" '.clusters[] | select(.name==$n) | .id')
else
  echo "  Creating $BUILDER_NODE..."
  resp=$(api POST /clusters -d "{
    \"name\": \"${BUILDER_NODE}\",
    \"slug\": \"${BUILDER_NODE}\",
    \"type\": \"k3s\",
    \"endpoint\": \"${BUILDER_ENDPOINT}\",
    \"region\": \"eu-central\",
    \"status\": \"ready\",
    \"metadata\": {\"k3s_version\":\"v1.33.7+k3s3\",\"role\":\"worker\",\"taints\":[\"builder=true:NoSchedule\"],\"purpose\":\"CI builds + ARC runners\"}
  }")
  parsed=$(parse_response "$resp")
  code=${parsed%%|*}
  body_c=${parsed#*|}
  if [[ "$code" == "201" || "$code" == "200" ]]; then
    BUILDER_CLUSTER_ID=$(echo "$body_c" | jq -r '.id')
    echo "  $BUILDER_NODE: created ($BUILDER_CLUSTER_ID)"
  else
    echo "  ERROR creating $BUILDER_NODE (HTTP $code): $body_c"
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

# Host: control-plane node
if echo "$existing_hosts" | grep -qx "$CONTROL_PLANE_NODE"; then
  echo "  $CONTROL_PLANE_NODE: already exists"
  CORE_BMH_ID=$(echo "$body" | jq -r --arg n "$CONTROL_PLANE_NODE" '.hosts[] | select(.name==$n) | .id')
else
  echo "  Registering $CONTROL_PLANE_NODE..."
  resp=$(api POST /fleet -d "{
    \"name\": \"${CONTROL_PLANE_NODE}\",
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
    echo "  $CONTROL_PLANE_NODE: registered ($CORE_BMH_ID)"
  else
    echo "  ERROR registering $CONTROL_PLANE_NODE (HTTP $code): $body_h"
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
    \"hardware_profile\": {\"cpu\":\"Shared vCPU\",\"cores\":2,\"threads\":2,\"ram_gb\":4,\"storage\":[{\"type\":\"SSD\",\"size_gb\":40}],\"network\":\"20TB traffic\",\"type\":\"cloud compute instance\"},
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
echo "  $CONTROL_PLANE_NODE cost: HTTP $code"

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
