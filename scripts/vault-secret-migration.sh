#!/usr/bin/env bash
# =============================================================================
# Vault Secret Migration: K8s Opaque Secrets → HashiCorp Vault KV v2
# =============================================================================
# Migrates all Kubernetes Opaque secrets to Vault KV v2 engine (secret/<ns>/<name>).
# Designed for the Enclii platform's ESO-based secret management architecture.
#
# Usage:
#   ./scripts/vault-secret-migration.sh [--dry-run] [--namespace <ns>] [--all]
#
# Options:
#   --dry-run           Print what would be written without writing to Vault
#   --namespace <ns>    Migrate secrets for a single namespace
#   --all               Migrate secrets for all platform namespaces
#   --verbose           Show decoded secret keys (values remain hidden)
#   --help              Show this help message
#
# Prerequisites:
#   - kubectl configured with cluster access
#   - Vault pod running and unsealed in the 'vault' namespace
#   - VAULT_TOKEN env var set, or root token accessible in vault-0 pod
#   - KV v2 secrets engine enabled at path 'secret/'
#
# Vault Path Convention:
#   secret/<namespace>/<secret-name>   (multiple secrets per namespace)
#   secret/<namespace>                 (single consolidated secret)
#
# See also:
#   - infra/k8s/base/external-secrets/vault-secrets/ (ESO ExternalSecret CRDs)
#   - infra/helm/vault/values.yaml (Vault Helm values)
#   - docs/infrastructure/EXTERNAL_SECRETS.md
# =============================================================================

set -euo pipefail

# --- Script directory ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# --- Source shared logging if available, otherwise define inline ---
if [[ -f "$SCRIPT_DIR/lib/logging.sh" ]]; then
    source "$SCRIPT_DIR/lib/logging.sh"
else
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    DIM='\033[2m'
    NC='\033[0m'

    log_info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
    log_success() { echo -e "${GREEN}[OK]${NC} $*"; }
    log_warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
    log_error()   { echo -e "${RED}[ERROR]${NC} $*" >&2; }
    die()         { log_error "$@"; exit 1; }
    section()     { echo ""; echo -e "${BOLD}${CYAN}═══ $* ═══${NC}"; echo ""; }
fi

# --- All platform namespaces ---
ALL_NAMESPACES=(
    enclii
    janua
    data
    cloudflare-tunnel
    dhanam
    selva
    tezca
    yantra4d
    karafiel
    forgesight
    pravara-mes
    monitoring
    arc-runners
    enclii-builds
    npm-registry
    madfam-site
)

# --- Secret types to skip (not Opaque / not user-managed) ---
SKIP_TYPES=(
    "kubernetes.io/service-account-token"
    "kubernetes.io/dockerconfigjson"
    "kubernetes.io/dockercfg"
    "kubernetes.io/tls"
    "helm.sh/release.v1"
    "bootstrap.kubernetes.io/token"
)

# --- Defaults ---
DRY_RUN=false
VERBOSE=false
TARGET_NAMESPACE=""
MIGRATE_ALL=false
VAULT_NS="vault"
VAULT_POD="vault-0"
VAULT_KV_PATH="secret"

# --- Counters ---
TOTAL_SECRETS=0
TOTAL_MIGRATED=0
TOTAL_SKIPPED=0
TOTAL_FAILED=0
FAILED_SECRETS=()

# =============================================================================
# Functions
# =============================================================================

usage() {
    cat <<'EOF'
Usage: ./scripts/vault-secret-migration.sh [OPTIONS]

Migrate Kubernetes Opaque secrets to HashiCorp Vault KV v2.

Options:
  --dry-run           Print what would be written without writing to Vault
  --namespace <ns>    Migrate secrets for a single namespace
  --all               Migrate secrets for all platform namespaces
  --verbose           Show secret key names during migration
  --help              Show this help message

Environment:
  VAULT_TOKEN         Vault authentication token (optional if using vault-0 pod token)

Examples:
  # Dry run across all namespaces
  ./scripts/vault-secret-migration.sh --all --dry-run

  # Migrate a single namespace
  ./scripts/vault-secret-migration.sh --namespace enclii

  # Migrate everything with verbose output
  VAULT_TOKEN=hvs.xxx ./scripts/vault-secret-migration.sh --all --verbose
EOF
    exit 0
}

# Run a vault command inside the vault-0 pod
vault_exec() {
    local token_env=""
    if [[ -n "${VAULT_TOKEN:-}" ]]; then
        token_env="VAULT_TOKEN=${VAULT_TOKEN}"
    fi
    kubectl exec -n "$VAULT_NS" "$VAULT_POD" -- \
        env ${token_env:+"$token_env"} \
        vault "$@" 2>&1
}

# Check if a secret type should be skipped
should_skip_type() {
    local secret_type="$1"
    for skip in "${SKIP_TYPES[@]}"; do
        if [[ "$secret_type" == "$skip" ]]; then
            return 0
        fi
    done
    return 1
}

# Convert K8s secret key to Vault property name (lowercase snake_case)
normalize_key() {
    local key="$1"
    # Convert to lowercase and replace hyphens/dots with underscores
    echo "$key" | tr '[:upper:]' '[:lower:]' | tr '-.' '__'
}

# Preflight: verify kubectl context
check_kubectl() {
    log_info "Checking kubectl context..."
    local context
    context=$(kubectl config current-context 2>/dev/null) || die "kubectl not configured or no context set"
    log_success "kubectl context: ${BOLD}${context}${NC}"

    # Verify cluster is reachable
    kubectl cluster-info &>/dev/null || die "Cannot reach Kubernetes cluster"
    log_success "Cluster is reachable"
}

# Preflight: verify Vault is running and unsealed
check_vault() {
    log_info "Checking Vault status..."

    # Verify vault pod exists
    local pod_status
    pod_status=$(kubectl get pod -n "$VAULT_NS" "$VAULT_POD" -o jsonpath='{.status.phase}' 2>/dev/null) \
        || die "Vault pod '$VAULT_POD' not found in namespace '$VAULT_NS'"

    if [[ "$pod_status" != "Running" ]]; then
        die "Vault pod is not Running (status: $pod_status)"
    fi
    log_success "Vault pod is Running"

    # Check Vault seal status
    local vault_status
    vault_status=$(kubectl exec -n "$VAULT_NS" "$VAULT_POD" -- vault status -format=json 2>/dev/null) \
        || die "Failed to get Vault status (pod may not be ready)"

    local sealed
    sealed=$(echo "$vault_status" | python3 -c "import sys,json; print(json.load(sys.stdin).get('sealed', True))" 2>/dev/null)

    if [[ "$sealed" == "True" ]]; then
        die "Vault is sealed. Unseal it first: kubectl exec -n $VAULT_NS $VAULT_POD -- vault operator unseal <key>"
    fi
    log_success "Vault is unsealed"

    # Check Vault token
    if [[ -z "${VAULT_TOKEN:-}" ]]; then
        log_warn "VAULT_TOKEN not set. Attempting to use vault-0 pod's token..."
        # Try to verify auth by listing KV mounts
        if ! vault_exec secrets list -format=json &>/dev/null; then
            die "No valid Vault token. Set VAULT_TOKEN env var or ensure vault-0 has a valid root token."
        fi
        log_success "Vault pod has valid authentication"
    else
        # Verify the provided token works
        if ! vault_exec token lookup -format=json &>/dev/null; then
            die "VAULT_TOKEN is invalid or expired"
        fi
        log_success "VAULT_TOKEN is valid"
    fi

    # Verify KV v2 engine is enabled at expected path
    local mounts
    mounts=$(vault_exec secrets list -format=json 2>/dev/null) || true
    if echo "$mounts" | python3 -c "import sys,json; d=json.load(sys.stdin); assert '${VAULT_KV_PATH}/' in d" 2>/dev/null; then
        log_success "KV v2 engine enabled at '${VAULT_KV_PATH}/'"
    else
        log_warn "KV v2 engine at '${VAULT_KV_PATH}/' not detected. Will attempt to enable it."
        if [[ "$DRY_RUN" == false ]]; then
            vault_exec secrets enable -path="$VAULT_KV_PATH" kv-v2 2>/dev/null \
                || log_warn "Engine may already exist or enable failed — continuing"
        fi
    fi
}

# Migrate secrets for a single namespace
migrate_namespace() {
    local ns="$1"
    local ns_secrets=0
    local ns_migrated=0
    local ns_skipped=0
    local ns_failed=0

    section "Namespace: $ns"

    # Check if namespace exists
    if ! kubectl get namespace "$ns" &>/dev/null; then
        log_warn "Namespace '$ns' does not exist — skipping"
        return
    fi

    # Get all secrets in namespace as JSON
    local secrets_json
    secrets_json=$(kubectl get secrets -n "$ns" -o json 2>/dev/null) \
        || { log_warn "Failed to list secrets in '$ns' — skipping"; return; }

    # Count total secrets
    local secret_count
    secret_count=$(echo "$secrets_json" | python3 -c "
import sys, json
data = json.load(sys.stdin)
print(len(data.get('items', [])))
" 2>/dev/null)

    log_info "Found $secret_count secret(s) in namespace '$ns'"

    if [[ "$secret_count" -eq 0 ]]; then
        return
    fi

    # Process each secret
    local processing_script
    processing_script=$(cat <<'PYEOF'
import sys, json, base64

data = json.load(sys.stdin)
skip_types = sys.argv[1].split(",") if sys.argv[1] else []

for item in data.get("items", []):
    secret_type = item.get("type", "Opaque")
    name = item["metadata"]["name"]

    if secret_type in skip_types:
        print(f"SKIP|{name}|{secret_type}")
        continue

    # Only migrate Opaque secrets
    if secret_type != "Opaque":
        print(f"SKIP|{name}|{secret_type}")
        continue

    secret_data = item.get("data", {})
    if not secret_data:
        print(f"EMPTY|{name}|no-data")
        continue

    # Decode base64 values
    kv_pairs = []
    for key, b64val in sorted(secret_data.items()):
        try:
            decoded = base64.b64decode(b64val).decode("utf-8", errors="replace")
            # Escape single quotes and newlines for shell safety
            decoded = decoded.replace("'", "'\\''")
            decoded = decoded.replace("\n", "\\n")
            kv_pairs.append(f"{key}={decoded}")
        except Exception as e:
            kv_pairs.append(f"{key}=DECODE_ERROR:{e}")

    keys_str = "|".join(kv_pairs)
    print(f"MIGRATE|{name}|{len(kv_pairs)}|{keys_str}")
PYEOF
    )

    local skip_types_csv
    skip_types_csv=$(IFS=,; echo "${SKIP_TYPES[*]}")

    # Process secrets through Python for reliable JSON/base64 handling
    while IFS= read -r line; do
        local action secret_name

        action=$(echo "$line" | cut -d'|' -f1)
        secret_name=$(echo "$line" | cut -d'|' -f2)

        case "$action" in
            SKIP)
                local skip_reason
                skip_reason=$(echo "$line" | cut -d'|' -f3)
                if [[ "$VERBOSE" == true ]]; then
                    log_info "  Skipping ${DIM}${secret_name}${NC} (type: $skip_reason)"
                fi
                ((ns_skipped++))
                ((TOTAL_SKIPPED++))
                ;;

            EMPTY)
                log_warn "  Skipping ${secret_name} — no data keys"
                ((ns_skipped++))
                ((TOTAL_SKIPPED++))
                ;;

            MIGRATE)
                local key_count kv_data
                key_count=$(echo "$line" | cut -d'|' -f3)
                # Everything after the third pipe is kv_pairs joined by |
                kv_data=$(echo "$line" | cut -d'|' -f4-)

                local vault_path="${VAULT_KV_PATH}/${ns}/${secret_name}"
                ((ns_secrets++))
                ((TOTAL_SECRETS++))

                if [[ "$VERBOSE" == true ]]; then
                    local key_names
                    key_names=$(echo "$kv_data" | tr '|' '\n' | cut -d'=' -f1 | tr '\n' ', ' | sed 's/,$//')
                    log_info "  Secret ${BOLD}${secret_name}${NC} → vault:${vault_path} (${key_count} keys: ${key_names})"
                else
                    log_info "  Secret ${BOLD}${secret_name}${NC} → vault:${vault_path} (${key_count} keys)"
                fi

                if [[ "$DRY_RUN" == true ]]; then
                    echo -e "    ${YELLOW}[DRY-RUN]${NC} Would write ${key_count} key(s) to ${vault_path}"
                    ((ns_migrated++))
                    ((TOTAL_MIGRATED++))
                    continue
                fi

                # Build vault kv put command arguments
                # Use a temp file to avoid shell escaping issues with complex values
                local tmpfile
                tmpfile=$(mktemp)

                # Write kv pairs as JSON for vault kv put -
                echo "$secrets_json" | python3 -c "
import sys, json, base64

data = json.load(sys.stdin)
secret_name = '$secret_name'

for item in data.get('items', []):
    if item['metadata']['name'] == secret_name:
        secret_data = item.get('data', {})
        decoded = {}
        for key, b64val in secret_data.items():
            try:
                decoded[key] = base64.b64decode(b64val).decode('utf-8', errors='replace')
            except Exception:
                decoded[key] = ''
        json.dump(decoded, sys.stdout)
        break
" > "$tmpfile" 2>/dev/null

                # Write to Vault using JSON input (safest for complex values)
                local write_result
                if write_result=$(kubectl exec -i -n "$VAULT_NS" "$VAULT_POD" -- \
                    env ${VAULT_TOKEN:+"VAULT_TOKEN=${VAULT_TOKEN}"} \
                    vault kv put -format=json "${vault_path}" - < "$tmpfile" 2>&1); then
                    log_success "  Wrote ${key_count} key(s) to ${vault_path}"
                    ((ns_migrated++))
                    ((TOTAL_MIGRATED++))
                else
                    log_error "  Failed to write ${vault_path}: ${write_result}"
                    ((ns_failed++))
                    ((TOTAL_FAILED++))
                    FAILED_SECRETS+=("${ns}/${secret_name}")
                fi

                rm -f "$tmpfile"
                ;;
        esac
    done < <(echo "$secrets_json" | python3 -c "$processing_script" "$skip_types_csv" 2>/dev/null)

    # --- Verification (skip in dry-run) ---
    if [[ "$DRY_RUN" == false && "$ns_migrated" -gt 0 ]]; then
        echo ""
        log_info "Verifying namespace '$ns'..."

        # List all secrets written under this namespace path
        local vault_list
        vault_list=$(vault_exec kv list -format=json "${VAULT_KV_PATH}/${ns}" 2>/dev/null) || true

        if [[ -n "$vault_list" ]]; then
            local vault_count
            vault_count=$(echo "$vault_list" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "0")

            if [[ "$vault_count" -ge "$ns_migrated" ]]; then
                log_success "Verification passed: ${vault_count} secret(s) in Vault path ${VAULT_KV_PATH}/${ns}/ (expected >= ${ns_migrated})"
            else
                log_warn "Verification mismatch: ${vault_count} in Vault vs ${ns_migrated} migrated (pre-existing secrets may differ)"
            fi
        else
            # If only one secret was migrated, it might be at the path directly
            if [[ "$ns_migrated" -eq 1 ]]; then
                log_info "Single secret migrated — verifying directly..."
                # Verification of individual secret handled during write
            else
                log_warn "Could not list Vault path ${VAULT_KV_PATH}/${ns}/"
            fi
        fi
    fi

    # Namespace summary
    echo ""
    log_info "Namespace '${ns}' summary: ${GREEN}${ns_migrated} migrated${NC}, ${YELLOW}${ns_skipped} skipped${NC}, ${RED}${ns_failed} failed${NC}"
}

# =============================================================================
# Argument Parsing
# =============================================================================

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --namespace)
            TARGET_NAMESPACE="${2:-}"
            if [[ -z "$TARGET_NAMESPACE" ]]; then
                die "--namespace requires a value"
            fi
            shift 2
            ;;
        --all)
            MIGRATE_ALL=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --help|-h)
            usage
            ;;
        *)
            die "Unknown option: $1 (use --help for usage)"
            ;;
    esac
done

# Validate: must specify --namespace or --all
if [[ -z "$TARGET_NAMESPACE" && "$MIGRATE_ALL" == false ]]; then
    log_error "Must specify --namespace <ns> or --all"
    echo ""
    usage
fi

# =============================================================================
# Main
# =============================================================================

section "Vault Secret Migration"

if [[ "$DRY_RUN" == true ]]; then
    echo -e "${YELLOW}${BOLD}  DRY-RUN MODE — no changes will be written to Vault${NC}"
    echo ""
fi

# Determine namespaces to process
NAMESPACES=()
if [[ -n "$TARGET_NAMESPACE" ]]; then
    NAMESPACES=("$TARGET_NAMESPACE")
elif [[ "$MIGRATE_ALL" == true ]]; then
    NAMESPACES=("${ALL_NAMESPACES[@]}")
fi

log_info "Target namespaces: ${NAMESPACES[*]}"
echo ""

# --- Preflight Checks ---
section "Preflight Checks"
check_kubectl
check_vault
log_success "All preflight checks passed"

# --- Confirm before proceeding (non-dry-run) ---
if [[ "$DRY_RUN" == false ]]; then
    echo ""
    echo -e "${YELLOW}${BOLD}WARNING: This will write Kubernetes secrets into Vault KV v2.${NC}"
    echo -e "${YELLOW}Existing Vault secrets at the same paths will be overwritten.${NC}"
    echo ""
    echo -en "${YELLOW}Proceed with migration? [y/N]:${NC} "
    read -r confirm_answer
    if [[ ! "$confirm_answer" =~ ^[Yy]$ ]]; then
        log_warn "Aborted by user."
        exit 1
    fi
fi

# --- Migration ---
section "Migrating Secrets"

for ns in "${NAMESPACES[@]}"; do
    migrate_namespace "$ns"
done

# =============================================================================
# Summary
# =============================================================================

section "Migration Summary"

echo -e "  Namespaces processed:  ${BOLD}${#NAMESPACES[@]}${NC}"
echo -e "  Total Opaque secrets:  ${BOLD}${TOTAL_SECRETS}${NC}"
echo -e "  Successfully migrated: ${GREEN}${BOLD}${TOTAL_MIGRATED}${NC}"
echo -e "  Skipped (non-Opaque):  ${YELLOW}${TOTAL_SKIPPED}${NC}"
echo -e "  Failed:                ${RED}${BOLD}${TOTAL_FAILED}${NC}"

if [[ "$DRY_RUN" == true ]]; then
    echo ""
    echo -e "  ${YELLOW}${BOLD}DRY-RUN: No changes were made. Re-run without --dry-run to migrate.${NC}"
fi

if [[ ${#FAILED_SECRETS[@]} -gt 0 ]]; then
    echo ""
    log_error "Failed secrets:"
    for fs in "${FAILED_SECRETS[@]}"; do
        echo -e "    ${RED}- ${fs}${NC}"
    done
    echo ""
    log_info "Retry failed secrets individually:"
    echo "  ./scripts/vault-secret-migration.sh --namespace <ns> --verbose"
    exit 1
fi

if [[ "$TOTAL_MIGRATED" -gt 0 && "$DRY_RUN" == false ]]; then
    echo ""
    log_success "Migration complete. Next steps:"
    echo "  1. Verify in Vault UI:  kubectl port-forward -n vault svc/vault 8200:8200"
    echo "  2. Deploy ExternalSecret CRDs:  kubectl apply -f infra/k8s/base/external-secrets/vault-secrets/"
    echo "  3. Verify ESO sync:  kubectl get externalsecrets -A"
    echo "  4. After ESO is syncing, remove original K8s Opaque secrets"
fi

echo ""
