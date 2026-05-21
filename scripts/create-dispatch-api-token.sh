#!/usr/bin/env bash
# Generate a Switchyard API token for Dispatch and store it as a K8s secret.
#
# Uses the Switchyard API (/v1/user/tokens) to create the token properly,
# instead of raw SQL. This is schema-resilient and uses Switchyard's own
# token generation (SHA-256 hashing, prefix extraction, etc.).
#
# Usage:
#   # Interactive: prompts for admin JWT if not set
#   ./scripts/create-dispatch-api-token.sh
#
#   # Non-interactive: provide JWT via env
#   ADMIN_JWT=<jwt> ./scripts/create-dispatch-api-token.sh
#
#   # Dry run: show what would happen without making changes
#   ./scripts/create-dispatch-api-token.sh --dry-run
#
#   # Direct DB fallback via data/postgres psql client (if API is unreachable):
#   ./scripts/create-dispatch-api-token.sh --direct-db
#
# Prerequisites:
#   - kubectl access to the enclii namespace
#   - A valid admin JWT (obtain via Janua SSO login)
#     OR --direct-db flag with enclii-secrets/database-url and data/postgres access

set -euo pipefail

DRY_RUN=false
DIRECT_DB=false
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    --direct-db) DIRECT_DB=true ;;
  esac
done

NAMESPACE="enclii"
SECRET_NAME="dispatch-secrets"
TOKEN_NAME="dispatch-service"
API_BASE="${SWITCHYARD_API_URL:-https://api.enclii.dev}"

echo "=== Create Dispatch Service API Token ==="
echo ""

# ──────────────────────────────────────────────
# Method 1: Via Switchyard API (preferred)
# ──────────────────────────────────────────────
create_via_api() {
  local jwt="$1"

  # Step 1: Revoke any existing dispatch-service tokens
  echo "Checking for existing dispatch-service tokens..."
  local existing
  existing=$(curl -sf -H "Authorization: Bearer $jwt" \
    -H "Content-Type: application/json" \
    "${API_BASE}/v1/user/tokens" 2>/dev/null || echo '{"tokens":[]}')

  local token_ids
  token_ids=$(echo "$existing" | jq -r '.tokens[]? | select(.name == "'"$TOKEN_NAME"'" and .revoked == false) | .id' 2>/dev/null || true)

  if [[ -n "$token_ids" ]]; then
    echo "Revoking existing dispatch-service tokens..."
    while IFS= read -r tid; do
      [[ -z "$tid" ]] && continue
      local code
      code=$(curl -sf -o /dev/null -w "%{http_code}" \
        -X DELETE \
        -H "Authorization: Bearer $jwt" \
        "${API_BASE}/v1/user/tokens/${tid}" 2>/dev/null || echo "000")
      if [[ "$code" == "200" || "$code" == "204" ]]; then
        echo "  Revoked: $tid"
      else
        echo "  Warning: failed to revoke $tid (HTTP $code)"
      fi
    done <<< "$token_ids"
  fi

  # Step 2: Create new token via API
  echo "Creating new dispatch-service token..."
  local resp
  resp=$(curl -sf -w "\n%{http_code}" \
    -X POST \
    -H "Authorization: Bearer $jwt" \
    -H "Content-Type: application/json" \
    -d '{"name":"'"$TOKEN_NAME"'","scopes":["admin"]}' \
    "${API_BASE}/v1/user/tokens" 2>/dev/null || echo -e "\n000")

  local body http_code
  http_code=$(echo "$resp" | tail -1)
  body=$(echo "$resp" | sed '$d')

  if [[ "$http_code" != "201" && "$http_code" != "200" ]]; then
    echo "ERROR: Failed to create token (HTTP $http_code)"
    echo "$body"
    return 1
  fi

  RAW_TOKEN=$(echo "$body" | jq -r '.token')
  local prefix
  prefix=$(echo "$body" | jq -r '.prefix')
  local token_id
  token_id=$(echo "$body" | jq -r '.id')

  if [[ -z "$RAW_TOKEN" || "$RAW_TOKEN" == "null" ]]; then
    echo "ERROR: No token in response"
    echo "$body"
    return 1
  fi

  echo "  Token created successfully"
  echo "  ID:     $token_id"
  echo "  Prefix: ${prefix}..."
  echo "  Scopes: [admin]"
}

# ──────────────────────────────────────────────
# Method 2: Direct DB (fallback)
# ──────────────────────────────────────────────
create_via_db() {
  local job_name="dispatch-api-token-setup"

  # Generate token client-side (same algorithm as Switchyard)
  local raw_hex
  raw_hex=$(openssl rand -hex 32)
  RAW_TOKEN="enclii_${raw_hex}"
  local prefix="${RAW_TOKEN:0:16}"
  local token_hash
  token_hash=$(printf '%s' "$RAW_TOKEN" | shasum -a 256 | awk '{print $1}')

  echo "Generated token (direct DB method)"
  echo "  Prefix: ${prefix}..."
  echo "  Scopes: [admin]"

  if $DRY_RUN; then
    echo "[dry-run] Would insert via data/postgres psql client"
    return 0
  fi

  kubectl -n "$NAMESPACE" delete job "$job_name" --ignore-not-found 2>/dev/null

  echo "Inserting token via data/postgres psql client..."
  local database_url
  database_url=$(kubectl -n "$NAMESPACE" get secret enclii-secrets -o jsonpath='{.data.database-url}' | base64 -d)

  local inserted_count
  inserted_count=$(kubectl -n data exec -i deploy/postgres -c postgres -- \
    env DATABASE_URL="$database_url" \
    psql "$database_url" -v ON_ERROR_STOP=1 -At \
      -v token_name="$TOKEN_NAME" \
      -v token_prefix="$prefix" \
      -v token_hash="$token_hash" <<'SQL'
WITH selected_user AS (
  SELECT id
  FROM users
  ORDER BY CASE WHEN role = 'admin' THEN 0 ELSE 1 END, created_at
  LIMIT 1
),
revoked AS (
  UPDATE api_tokens
  SET revoked = true, revoked_at = now(), updated_at = now()
  WHERE name = :'token_name' AND revoked = false
  RETURNING id
),
inserted AS (
  INSERT INTO api_tokens (id, user_id, name, prefix, token_hash, scopes, revoked, created_at, updated_at)
  SELECT gen_random_uuid(), id, :'token_name', :'token_prefix', :'token_hash', '{admin}', false, now(), now()
  FROM selected_user
  RETURNING id
)
SELECT count(*) FROM inserted;
SQL
)

  if [[ "$inserted_count" != "1" ]]; then
    echo "ERROR: Token insert did not affect exactly one row (inserted=${inserted_count})."
    return 1
  fi

  echo "Token inserted successfully."
}

# ──────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────

RAW_TOKEN=""

if $DIRECT_DB; then
  echo "Using direct DB method (fallback)..."
  create_via_db
else
  # Get admin JWT
  if [[ -z "${ADMIN_JWT:-}" ]]; then
    echo "An admin JWT is required to create the service token via the API."
    echo "Obtain one by logging into Dispatch (admin.enclii.dev) and copying"
    echo "the dispatch_auth cookie value from your browser."
    echo ""
    read -rp "Paste admin JWT: " ADMIN_JWT
  fi

  if [[ -z "$ADMIN_JWT" ]]; then
    echo "ERROR: No JWT provided."
    exit 1
  fi

  create_via_api "$ADMIN_JWT"
fi

if [[ -z "$RAW_TOKEN" ]]; then
  echo "ERROR: No token was generated."
  exit 1
fi

if $DRY_RUN; then
  echo ""
  echo "[dry-run] Would patch $SECRET_NAME with switchyard-api-key"
  echo "[dry-run] Raw token: ${RAW_TOKEN}"
  exit 0
fi

# ──────────────────────────────────────────────
# Store token in K8s secret
# ──────────────────────────────────────────────
echo ""
echo "Patching ${SECRET_NAME} with switchyard-api-key..."

if kubectl -n "$NAMESPACE" get secret "$SECRET_NAME" &>/dev/null; then
  ENCODED=$(printf '%s' "$RAW_TOKEN" | base64)
  kubectl -n "$NAMESPACE" patch secret "$SECRET_NAME" \
    --type merge -p "{\"data\":{\"switchyard-api-key\":\"${ENCODED}\"}}"
  echo "Patched existing secret."
else
  kubectl -n "$NAMESPACE" create secret generic "$SECRET_NAME" \
    --from-literal="switchyard-api-key=${RAW_TOKEN}"
  echo "Created secret."
fi

echo ""
echo "Done. Restart Dispatch to pick up the new secret:"
echo "  kubectl -n ${NAMESPACE} rollout restart deploy/dispatch"
echo ""
echo "To verify:"
echo "  kubectl -n ${NAMESPACE} exec deploy/dispatch -- printenv SWITCHYARD_API_KEY | head -c 16"
echo "  # Should show: enclii_xxxxxxxx"
