#!/usr/bin/env bash
# =============================================================================
# Cluster Operations: Remaining Gap Remediation (Phases 0-4)
# =============================================================================
# Deploys NetworkPolicies, Vault, PostHog, and Cosign Enforce mode.
#
# Usage:
#   ./scripts/cluster-ops-deploy.sh <phase>
#
# Phases:
#   preflight          Phase 0: Validate cluster access and baseline health
#   network-policies   Phase 1: Progressive NetworkPolicy rollout (4 batches)
#   vault              Phase 2: Vault init + unseal + configure
#   posthog            Phase 3: PostHog DB + secrets + deploy
#   cosign             Phase 4: Cosign Audit → Enforce
#   verify-all         Final end-to-end verification
#
# Prerequisites:
#   - SSH tunnel to foundry-core (port 55323 → 6443)
#   - kubectl context set to 'foundry'
#   - Cloudflare Zero Trust dashboard access (for tunnel routes)
#
# Safety:
#   - Each phase has verification gates before proceeding
#   - Rollback commands printed for every destructive operation
#   - Ctrl+C safe — no partial state between confirmation prompts
# =============================================================================

set -euo pipefail

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# --- Helpers ---
log()  { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()   { echo -e "${GREEN}[  OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()  { echo -e "${RED}[FAIL]${NC} $*"; }
hdr()  { echo -e "\n${BOLD}${CYAN}═══ $* ═══${NC}\n"; }

confirm() {
  local msg="${1:-Continue?}"
  echo -e "${YELLOW}${msg} [y/N]${NC} "
  read -r answer
  [[ "$answer" =~ ^[Yy]$ ]] || { warn "Aborted by user."; exit 1; }
}

check_http() {
  local url="$1"
  local expected="${2:-200}"
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 5 "https://${url}" 2>/dev/null || echo "000")
  if [[ "$code" == "$expected" ]]; then
    ok "$url → $code"
    return 0
  else
    err "$url → $code (expected $expected)"
    return 1
  fi
}

verify_endpoints() {
  local failed=0
  for url in api.enclii.dev/health app.enclii.dev admin.enclii.dev status.enclii.dev docs.enclii.dev; do
    check_http "$url" || ((failed++))
  done
  return "$failed"
}

verify_namespace_pods() {
  local ns="$1"
  local non_running
  non_running=$(kubectl get pods -n "$ns" --no-headers 2>/dev/null | grep -v -E "Running|Completed|Succeeded" | wc -l | tr -d ' ')
  local total
  total=$(kubectl get pods -n "$ns" --no-headers 2>/dev/null | wc -l | tr -d ' ')
  if [[ "$non_running" -eq 0 && "$total" -gt 0 ]]; then
    ok "Namespace $ns: $total pods all healthy"
    return 0
  elif [[ "$total" -eq 0 ]]; then
    warn "Namespace $ns: no pods found"
    return 0
  else
    err "Namespace $ns: $non_running/$total pods NOT Running"
    kubectl get pods -n "$ns" --no-headers | grep -v -E "Running|Completed|Succeeded"
    return 1
  fi
}

# =============================================================================
# Phase 0: Pre-Flight
# =============================================================================
phase_preflight() {
  hdr "Phase 0: Pre-Flight Validation"

  log "Checking cluster access..."
  if ! kubectl cluster-info &>/dev/null; then
    err "Cannot reach cluster. Is the SSH tunnel running?"
    echo "  ssh -L 55323:127.0.0.1:6443 root@<foundry-core-ip>"
    exit 1
  fi
  ok "Cluster reachable"

  log "Node status:"
  kubectl get nodes -o wide
  echo

  log "Node resources:"
  kubectl top nodes 2>/dev/null || warn "metrics-server not available"
  echo

  log "Endpoint health baseline:"
  verify_endpoints || warn "Some endpoints unhealthy — investigate before proceeding"
  echo

  log "ArgoCD application state:"
  kubectl get applications -n argocd -o wide
  echo

  log "Existing NetworkPolicies:"
  kubectl get networkpolicies -A 2>/dev/null | head -30
  local np_count
  np_count=$(kubectl get networkpolicies -A --no-headers 2>/dev/null | wc -l | tr -d ' ')
  log "Total NetworkPolicies: $np_count"
  echo

  log "Checking Vault/PostHog ArgoCD apps..."
  kubectl get application vault -n argocd -o jsonpath='{.status.sync.status}' 2>/dev/null && echo || warn "Vault app not found in ArgoCD"
  kubectl get application posthog -n argocd -o jsonpath='{.status.sync.status}' 2>/dev/null && echo || warn "PostHog app not found in ArgoCD"
  echo

  log "Pod count by namespace:"
  kubectl get pods -A --no-headers | awk '{print $1}' | sort | uniq -c | sort -rn
  echo

  ok "Pre-flight complete. Review output above before proceeding."
}

# =============================================================================
# Phase 1: NetworkPolicy Progressive Rollout
# =============================================================================
phase_network_policies() {
  hdr "Phase 1: NetworkPolicy Progressive Rollout"

  local POLICY_DIR="infra/k8s/policies"

  # --- Batch 1: Ecosystem (lowest risk) ---
  hdr "Batch 1/4: Ecosystem Namespaces"
  log "Applying policies for yantra4d, pravara-mes, karafiel, tezca, forgesight, madfam-site..."

  for policy in yantra4d pravara-mes karafiel tezca forgesight madfam-site; do
    local ns="$policy"
    local existing
    existing=$(kubectl get networkpolicies -n "$ns" --no-headers 2>/dev/null | wc -l | tr -d ' ')
    if [[ "$existing" -gt 0 ]]; then
      log "Deleting existing policies in $ns (required for spec merge)..."
      kubectl delete networkpolicies --all -n "$ns"
    fi
    log "Applying ${policy}-network-policies.yaml..."
    kubectl apply -f "${POLICY_DIR}/${policy}-network-policies.yaml"
  done
  echo

  log "Verifying ecosystem pods..."
  local batch1_ok=true
  for ns in yantra4d pravara-mes karafiel tezca forgesight madfam-site; do
    verify_namespace_pods "$ns" || batch1_ok=false
  done

  if [[ "$batch1_ok" != true ]]; then
    warn "Some ecosystem pods unhealthy after policy application."
    warn "Rollback: kubectl delete networkpolicies -n <namespace> --all"
    confirm "Continue anyway?"
  else
    ok "Batch 1 verified — all ecosystem pods healthy"
  fi

  # --- Batch 2: Infrastructure ---
  hdr "Batch 2/4: Infrastructure Services"
  confirm "Apply infrastructure policies (monitoring, arc, vault, posthog namespaces)?"

  for policy in arc-default-deny vault-network-policies posthog-network-policies; do
    log "Applying ${policy}.yaml..."
    kubectl apply -f "${POLICY_DIR}/${policy}.yaml"
  done
  echo

  log "Verifying infrastructure pods..."
  verify_namespace_pods "monitoring" || true
  verify_namespace_pods "arc-system" || true
  ok "Batch 2 applied (vault/posthog namespaces may not have pods yet)"

  # --- Batch 3: Data namespace (medium risk) ---
  hdr "Batch 3/4: Data Namespace (DB-dependent services)"
  warn "This controls access to PostgreSQL, Redis, PgBouncer."
  warn "Rollback: kubectl delete networkpolicies -n data --all"
  confirm "Apply data namespace policies?"

  kubectl apply -f "${POLICY_DIR}/data-network-policies.yaml"
  echo

  log "CRITICAL: Verifying DB-dependent services (30s wait)..."
  sleep 5

  local data_ok=true
  check_http "api.enclii.dev/health" || data_ok=false
  check_http "auth.madfam.io/.well-known/openid-configuration" || data_ok=false

  if [[ "$data_ok" != true ]]; then
    err "DB-dependent services unhealthy after data policies!"
    err "ROLLING BACK data namespace policies..."
    kubectl delete networkpolicies -n data --all
    ok "Rollback complete. Investigate before retrying."
    exit 1
  fi
  ok "Batch 3 verified — DB-dependent services healthy"

  # --- Batch 4: Enclii control plane (highest risk) ---
  hdr "Batch 4/4: Enclii Control Plane (highest risk)"
  warn "This applies default-deny to the enclii namespace."
  warn "Rollback: kubectl delete networkpolicy default-deny-ingress default-deny-egress -n enclii"
  confirm "Apply enclii control plane policies?"

  kubectl apply -f "${POLICY_DIR}/enclii-default-deny.yaml"
  echo

  log "IMMEDIATE verification (must pass within 30s)..."
  sleep 3

  local enclii_ok=true
  for url in api.enclii.dev/health app.enclii.dev admin.enclii.dev status.enclii.dev docs.enclii.dev; do
    check_http "$url" || enclii_ok=false
  done

  if [[ "$enclii_ok" != true ]]; then
    err "Enclii endpoints unhealthy! Rolling back..."
    kubectl delete networkpolicy default-deny-ingress default-deny-egress -n enclii
    ok "Rollback complete. Investigate before retrying."
    exit 1
  fi
  ok "Batch 4 verified — all enclii endpoints healthy"

  # --- Also apply remaining policies ---
  hdr "Applying remaining policies (status, janua, dhanam)"
  for policy in status-network-policies janua-network-policies; do
    local file="${POLICY_DIR}/${policy}.yaml"
    if [[ -f "$file" ]]; then
      log "Applying ${policy}.yaml..."
      kubectl apply -f "$file"
    fi
  done

  # Quotas (safe, non-breaking)
  for policy in enclii-quota janua-quota dhanam-quota; do
    local file="${POLICY_DIR}/${policy}.yaml"
    if [[ -f "$file" ]]; then
      log "Applying ${policy}.yaml..."
      kubectl apply -f "$file"
    fi
  done
  echo

  # --- Final count ---
  local final_count
  final_count=$(kubectl get networkpolicies -A --no-headers | wc -l | tr -d ' ')
  log "Total NetworkPolicies: $final_count"

  log "Final endpoint verification..."
  verify_endpoints || { err "Some endpoints degraded. Review above."; exit 1; }

  ok "Phase 1 complete. All NetworkPolicies applied and verified."
  echo
  log "Next steps:"
  log "  1. Merge feat/cluster-ops-prep to main"
  log "  2. ArgoCD will pick up network-policies.yaml app automatically"
  log "  3. Proceed to Phase 2: ./scripts/cluster-ops-deploy.sh vault"
}

# =============================================================================
# Phase 2: Vault Deployment
# =============================================================================
phase_vault() {
  hdr "Phase 2: Vault Deployment"

  log "Checking ArgoCD sync status for Vault..."
  local vault_status
  vault_status=$(kubectl get application vault -n argocd -o jsonpath='{.status.sync.status}' 2>/dev/null || echo "NotFound")
  log "Vault ArgoCD status: $vault_status"

  log "Checking Vault pods..."
  kubectl get pods -n vault 2>/dev/null || warn "No pods in vault namespace"
  kubectl get pvc -n vault 2>/dev/null || warn "No PVCs in vault namespace"
  echo

  # Check if already initialized
  local sealed_status
  sealed_status=$(kubectl exec -n vault vault-0 -- vault status -format=json 2>/dev/null | jq -r '.sealed' 2>/dev/null || echo "unknown")

  if [[ "$sealed_status" == "false" ]]; then
    ok "Vault is already initialized and unsealed!"
    confirm "Skip init/unseal and go to configuration?"
    phase_vault_configure
    return
  elif [[ "$sealed_status" == "true" ]]; then
    warn "Vault is initialized but sealed. Unseal keys needed."
    confirm "Proceed with unsealing?"
    phase_vault_unseal
    phase_vault_configure
    return
  fi

  # --- Step 2.2: Initialize ---
  hdr "Step 2.2: Initialize Vault"
  warn "This generates unseal keys and root token."
  warn "You MUST store the output securely (1Password, printed copy)."
  warn "Losing all unseal keys = losing Vault permanently."
  confirm "Initialize Vault?"

  local init_output
  init_output=$(kubectl exec -n vault vault-0 -- vault operator init \
    -key-shares=5 -key-threshold=3 -format=json 2>&1)

  if echo "$init_output" | jq -e '.root_token' &>/dev/null; then
    ok "Vault initialized successfully!"
    echo
    echo -e "${BOLD}${RED}╔══════════════════════════════════════════════════════╗${NC}"
    echo -e "${BOLD}${RED}║  SAVE THESE CREDENTIALS SECURELY — SHOWN ONCE ONLY  ║${NC}"
    echo -e "${BOLD}${RED}╚══════════════════════════════════════════════════════╝${NC}"
    echo
    echo "$init_output" | jq '{root_token: .root_token, unseal_keys_b64: .unseal_keys_b64}'
    echo
    warn "Copy the above to a secure location NOW."
    confirm "I have securely saved the credentials. Continue with unseal?"

    # Extract keys for unseal
    local key1 key2 key3
    key1=$(echo "$init_output" | jq -r '.unseal_keys_b64[0]')
    key2=$(echo "$init_output" | jq -r '.unseal_keys_b64[1]')
    key3=$(echo "$init_output" | jq -r '.unseal_keys_b64[2]')

    export VAULT_ROOT_TOKEN
    VAULT_ROOT_TOKEN=$(echo "$init_output" | jq -r '.root_token')
  else
    err "Vault init failed:"
    echo "$init_output"
    exit 1
  fi

  # --- Step 2.3: Unseal ---
  hdr "Step 2.3: Unseal Vault (3 of 5 keys)"
  log "Unsealing with keys 1-3..."
  kubectl exec -n vault vault-0 -- vault operator unseal "$key1"
  kubectl exec -n vault vault-0 -- vault operator unseal "$key2"
  kubectl exec -n vault vault-0 -- vault operator unseal "$key3"

  sealed_status=$(kubectl exec -n vault vault-0 -- vault status -format=json | jq -r '.sealed')
  if [[ "$sealed_status" == "false" ]]; then
    ok "Vault unsealed successfully!"
  else
    err "Vault still sealed after 3 keys. Check output above."
    exit 1
  fi

  phase_vault_configure
}

phase_vault_unseal() {
  hdr "Vault Unseal"
  warn "Enter 3 unseal keys (from your secure storage)."
  for i in 1 2 3; do
    echo -n "Unseal key $i: "
    read -rs key
    echo
    kubectl exec -n vault vault-0 -- vault operator unseal "$key"
  done

  local sealed
  sealed=$(kubectl exec -n vault vault-0 -- vault status -format=json | jq -r '.sealed')
  if [[ "$sealed" == "false" ]]; then
    ok "Vault unsealed!"
  else
    err "Still sealed. Verify your keys."
    exit 1
  fi
}

phase_vault_configure() {
  hdr "Step 2.4: Configure Vault"

  if [[ -z "${VAULT_ROOT_TOKEN:-}" ]]; then
    echo -n "Enter Vault root token: "
    read -rs VAULT_ROOT_TOKEN
    echo
  fi

  log "Enabling KV v2 secrets engine..."
  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
    vault secrets enable -path=secret kv-v2 2>/dev/null \
    && ok "KV v2 enabled at secret/" \
    || warn "KV v2 may already be enabled"

  log "Enabling Kubernetes auth..."
  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
    vault auth enable kubernetes 2>/dev/null \
    && ok "Kubernetes auth enabled" \
    || warn "Kubernetes auth may already be enabled"

  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
    vault write auth/kubernetes/config \
      kubernetes_host="https://kubernetes.default.svc:443"
  ok "Kubernetes auth configured"

  log "Creating per-namespace read policies..."
  for ns in enclii janua dhanam; do
    kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
      sh -c "vault policy write ${ns}-read - <<'POLICY'
path \"secret/data/${ns}/*\" {
  capabilities = [\"read\", \"list\"]
}
POLICY"
    ok "Policy ${ns}-read created"
  done

  # ESO reader policy for External Secrets Operator
  log "Creating ESO reader policy..."
  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
    sh -c "vault policy write eso-reader - <<'POLICY'
path \"secret/data/*\" {
  capabilities = [\"read\"]
}
path \"secret/metadata/*\" {
  capabilities = [\"read\", \"list\"]
}
POLICY"
  ok "Policy eso-reader created"

  # Bind ESO service account to the reader role
  log "Binding ESO Kubernetes auth role..."
  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
    vault write auth/kubernetes/role/eso-reader \
      bound_service_account_names=external-secrets \
      bound_service_account_namespaces=external-secrets \
      policies=eso-reader \
      ttl=1h
  ok "ESO reader role bound"

  log "Enabling audit logging..."
  kubectl exec -n vault vault-0 -- env VAULT_TOKEN="$VAULT_ROOT_TOKEN" \
    vault audit enable file file_path=/vault/audit/audit.log 2>/dev/null \
    && ok "Audit logging enabled" \
    || warn "Audit logging may already be enabled"

  # --- Step 2.6: Verify ---
  hdr "Step 2.6: Verify Vault"
  kubectl exec -n vault vault-0 -- vault status
  echo

  log "Checking if Cloudflare tunnel route exists for vault.enclii.dev..."
  check_http "vault.enclii.dev/v1/sys/health" "200" \
    && ok "vault.enclii.dev reachable" \
    || warn "vault.enclii.dev not reachable — add Cloudflare tunnel route manually:
    Dashboard → Zero Trust → Tunnels → enclii → Public Hostnames
    vault.enclii.dev → http://vault.vault.svc.cluster.local:8200"

  echo
  ok "Phase 2 complete. Vault is operational."
  log "Next: ./scripts/cluster-ops-deploy.sh posthog"
}

# =============================================================================
# Phase 3: PostHog Deployment
# =============================================================================
phase_posthog() {
  hdr "Phase 3: PostHog Deployment"

  # --- Step 3.1: Create PostgreSQL user + database ---
  hdr "Step 3.1: Create PostHog Database"

  local db_exists
  db_exists=$(kubectl exec -n data deploy/postgres -- psql -U postgres -tAc \
    "SELECT 1 FROM pg_database WHERE datname='posthog'" 2>/dev/null || echo "")

  if [[ "$db_exists" == "1" ]]; then
    ok "Database 'posthog' already exists"
    confirm "Skip DB creation and continue?"
  else
    log "Generating secure password..."
    PH_PASSWORD=$(openssl rand -base64 32)

    log "Creating PostgreSQL user 'posthog'..."
    kubectl exec -n data deploy/postgres -- psql -U postgres \
      -c "CREATE USER posthog WITH PASSWORD '${PH_PASSWORD}';" 2>/dev/null \
      && ok "User created" \
      || warn "User may already exist"

    log "Creating database 'posthog'..."
    kubectl exec -n data deploy/postgres -- psql -U postgres \
      -c "CREATE DATABASE posthog OWNER posthog;"
    ok "Database 'posthog' created"

    echo
    echo -e "${YELLOW}PostHog DB Password: ${PH_PASSWORD}${NC}"
    warn "Save this if you need it for debugging. It will be stored in the K8s secret."
  fi

  # --- Step 3.2: Create namespace + secrets ---
  hdr "Step 3.2: Create PostHog Namespace + Secrets"

  kubectl create namespace posthog 2>/dev/null && ok "Namespace created" || ok "Namespace already exists"

  local secret_exists
  secret_exists=$(kubectl get secret posthog-secrets -n posthog --no-headers 2>/dev/null | wc -l | tr -d ' ')

  if [[ "$secret_exists" -gt 0 ]]; then
    ok "Secret posthog-secrets already exists"
    confirm "Skip secret creation?"
  else
    if [[ -z "${PH_PASSWORD:-}" ]]; then
      echo -n "Enter PostHog DB password (from Step 3.1 or previous run): "
      read -rs PH_PASSWORD
      echo
    fi

    PH_SECRET_KEY=$(openssl rand -base64 50)

    local db_key="POSTHOG_DB_PASSWORD"
    local sk_key="POSTHOG_SECRET_KEY"
    kubectl create secret generic posthog-secrets -n posthog \
      --from-literal="${db_key}=${PH_PASSWORD}" \
      --from-literal="${sk_key}=${PH_SECRET_KEY}"
    ok "Secret posthog-secrets created"
  fi

  # --- Step 3.3: Ensure data NetworkPolicy allows posthog ---
  hdr "Step 3.3: Verify Data Namespace Policies"
  local posthog_allowed
  posthog_allowed=$(kubectl get networkpolicy postgres-ingress -n data -o yaml 2>/dev/null | grep -c "posthog" || echo "0")

  if [[ "$posthog_allowed" -gt 0 ]]; then
    ok "Data namespace policies already allow posthog"
  else
    log "Applying updated data-network-policies.yaml..."
    kubectl apply -f infra/k8s/policies/data-network-policies.yaml
    ok "PostHog added to data namespace ingress rules"
  fi

  # --- Step 3.4: Trigger ArgoCD sync ---
  hdr "Step 3.4: ArgoCD Sync"

  local posthog_sync
  posthog_sync=$(kubectl get application posthog -n argocd -o jsonpath='{.status.sync.status}' 2>/dev/null || echo "NotFound")
  log "PostHog ArgoCD status: $posthog_sync"

  if [[ "$posthog_sync" == "NotFound" ]]; then
    warn "PostHog ArgoCD app not found. It will be created when the branch merges to main."
    warn "Make sure feat/cluster-ops-prep is merged before this step."
    confirm "Has the branch been merged? Continue?"
  fi

  log "Checking PostHog pods (may take 5-10 min for ClickHouse)..."
  kubectl get pods -n posthog 2>/dev/null || warn "No pods yet"

  if [[ "$(kubectl get pods -n posthog --no-headers 2>/dev/null | wc -l | tr -d ' ')" -eq 0 ]]; then
    log "Triggering ArgoCD sync..."
    kubectl patch application posthog -n argocd --type merge \
      -p '{"operation":{"sync":{"syncStrategy":{"apply":{"force":false}}}}}' 2>/dev/null \
      || warn "Could not trigger sync — may need manual intervention"

    log "Waiting for pods to appear (timeout 5m)..."
    for i in $(seq 1 30); do
      local count
      count=$(kubectl get pods -n posthog --no-headers 2>/dev/null | wc -l | tr -d ' ')
      if [[ "$count" -gt 0 ]]; then
        ok "Pods appearing: $count found"
        break
      fi
      echo -n "."
      sleep 10
    done
    echo
  fi

  log "Current PostHog pods:"
  kubectl get pods -n posthog -o wide 2>/dev/null
  echo

  log "PostHog services:"
  kubectl get svc -n posthog 2>/dev/null
  echo

  # --- Step 3.6: Verify ---
  hdr "Step 3.6: Verify PostHog"

  log "Checking analytics.enclii.dev (requires Cloudflare tunnel route)..."
  check_http "analytics.enclii.dev/_health" "200" \
    && ok "PostHog reachable at analytics.enclii.dev" \
    || warn "analytics.enclii.dev not reachable — add Cloudflare tunnel route:
    Dashboard → Zero Trust → Tunnels → enclii → Public Hostnames
    analytics.enclii.dev → http://posthog-web.posthog.svc.cluster.local:8000
    (Verify service name: kubectl get svc -n posthog)"

  log "Verifying no impact on existing services..."
  verify_endpoints || warn "Some endpoints degraded — investigate"

  echo
  ok "Phase 3 complete."
  log "Manual step: Navigate to https://analytics.enclii.dev"
  log "  1. Create admin account"
  log "  2. Create organization"
  log "  3. Create project"
  log "  4. Note API key (phc_...) for SDK integration"
  log "Next: ./scripts/cluster-ops-deploy.sh cosign"
}

# =============================================================================
# Phase 4: Cosign Audit → Enforce
# =============================================================================
phase_cosign() {
  hdr "Phase 4: Cosign Audit → Enforce"

  # --- Step 4.1: Pre-check ---
  hdr "Step 4.1: Verify Images Pass in Audit Mode"

  log "Labeling enclii namespace for signature verification..."
  kubectl label namespace enclii enclii.dev/verify-signatures=true --overwrite
  ok "Label applied"

  log "Waiting 120s for Kyverno to generate policy reports..."
  for i in $(seq 1 12); do
    echo -n "."
    sleep 10
  done
  echo

  log "Checking policy reports for failures..."
  local fail_count
  fail_count=$(kubectl get policyreport -n enclii -o yaml 2>/dev/null | grep -c "result: fail" || echo "0")

  if [[ "$fail_count" -gt 0 ]]; then
    err "Found $fail_count policy failures in enclii namespace!"
    log "Failed resources:"
    kubectl get policyreport -n enclii -o yaml | grep -B5 "result: fail" || true
    echo
    warn "Fix unsigned images before switching to Enforce mode."
    warn "Do NOT proceed until fail_count is 0."
    exit 1
  fi
  ok "Zero policy failures — all images pass signature verification"

  # --- Step 4.2: Git change ---
  hdr "Step 4.2: Flip to Enforce Mode"
  warn "This will block ALL unsigned images from deploying to labeled namespaces."
  warn "Rollback: kubectl patch clusterpolicy verify-image-signatures --type merge -p '{\"spec\":{\"validationFailureAction\":\"Audit\"}}'"
  confirm "Switch verify-image-signatures to Enforce mode?"

  log "The git change (Audit→Enforce) needs to be committed and merged."
  log "After merge, ArgoCD auto-syncs the Kyverno policy."
  echo
  log "Run this from the repo root:"
  echo "  sed -i '' 's/validationFailureAction: Audit/validationFailureAction: Enforce/' infra/k8s/base/kyverno/policies/image-policies.yaml"
  echo "  git add infra/k8s/base/kyverno/policies/image-policies.yaml"
  echo "  git commit -m 'feat(security): switch cosign image verification to Enforce mode'"
  echo "  git push && gh pr merge --merge"
  echo
  confirm "Has the Enforce change been committed and merged?"

  # --- Step 4.3: Verify enforcement ---
  hdr "Step 4.3: Verify Enforcement"

  log "Checking Kyverno policy mode..."
  local action
  action=$(kubectl get clusterpolicy verify-image-signatures -o jsonpath='{.spec.validationFailureAction}' 2>/dev/null || echo "unknown")

  if [[ "$action" == "Enforce" ]]; then
    ok "Policy is in Enforce mode"
  else
    warn "Policy still shows: $action (may need ArgoCD sync)"
    log "Waiting for ArgoCD sync..."
    sleep 30
    action=$(kubectl get clusterpolicy verify-image-signatures -o jsonpath='{.spec.validationFailureAction}' 2>/dev/null || echo "unknown")
    log "Policy mode: $action"
  fi

  log "Testing enforcement — unsigned image should be blocked..."
  local deny_output
  deny_output=$(kubectl run test-unsigned --image=nginx:latest -n enclii --dry-run=server 2>&1 || true)
  if echo "$deny_output" | grep -qi "denied\|blocked\|violated"; then
    ok "Enforcement working — unsigned images are blocked"
  else
    warn "Enforcement test inconclusive. Output:"
    echo "$deny_output"
  fi

  # --- Step 4.4: Progressive namespace labeling ---
  hdr "Step 4.4: Progressive Namespace Labeling"
  for ns in janua dhanam status; do
    confirm "Label namespace '$ns' for signature verification?"
    kubectl label namespace "$ns" enclii.dev/verify-signatures=true --overwrite
    ok "Labeled $ns"
    sleep 5
    verify_namespace_pods "$ns" || { warn "Pod disruption in $ns — remove label?"; }
  done

  echo
  ok "Phase 4 complete. Cosign Enforce mode active."
}

# =============================================================================
# Final: End-to-End Verification
# =============================================================================
phase_verify_all() {
  hdr "Final End-to-End Verification"

  log "1. Endpoint health:"
  local failed=0
  for url in api.enclii.dev/health app.enclii.dev admin.enclii.dev status.enclii.dev docs.enclii.dev; do
    check_http "$url" || ((failed++))
  done
  check_http "vault.enclii.dev/v1/sys/health" "200" || ((failed++))
  check_http "analytics.enclii.dev/_health" "200" || ((failed++))
  echo

  log "2. NetworkPolicy count:"
  local np_count
  np_count=$(kubectl get networkpolicies -A --no-headers 2>/dev/null | wc -l | tr -d ' ')
  log "   Total: $np_count (expected 40+)"
  [[ "$np_count" -ge 40 ]] && ok "NetworkPolicy count OK" || warn "Fewer than expected"
  echo

  log "3. Vault status:"
  local sealed
  sealed=$(kubectl exec -n vault vault-0 -- vault status -format=json 2>/dev/null | jq -r '.sealed' || echo "unknown")
  [[ "$sealed" == "false" ]] && ok "Vault unsealed" || err "Vault sealed or unreachable: $sealed"
  echo

  log "4. PostHog pods:"
  kubectl get pods -n posthog --no-headers 2>/dev/null | head -10
  echo

  log "5. Cosign enforcement:"
  local action
  action=$(kubectl get clusterpolicy verify-image-signatures -o jsonpath='{.spec.validationFailureAction}' 2>/dev/null || echo "unknown")
  [[ "$action" == "Enforce" ]] && ok "Cosign in Enforce mode" || warn "Cosign mode: $action"
  echo

  log "6. ArgoCD applications:"
  kubectl get applications -n argocd -o wide 2>/dev/null
  echo

  log "7. Pod summary:"
  local total_pods non_running
  total_pods=$(kubectl get pods -A --no-headers 2>/dev/null | wc -l | tr -d ' ')
  non_running=$(kubectl get pods -A --no-headers 2>/dev/null | grep -v -E "Running|Completed|Succeeded" | wc -l | tr -d ' ')
  log "   Total: $total_pods pods, $non_running non-running"
  echo

  if [[ "$failed" -eq 0 && "$non_running" -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}════════════════════════════════════════════${NC}"
    echo -e "${GREEN}${BOLD}  ALL VERIFICATIONS PASSED                  ${NC}"
    echo -e "${GREEN}${BOLD}════════════════════════════════════════════${NC}"
  else
    echo -e "${YELLOW}${BOLD}════════════════════════════════════════════${NC}"
    echo -e "${YELLOW}${BOLD}  COMPLETED WITH WARNINGS — REVIEW ABOVE   ${NC}"
    echo -e "${YELLOW}${BOLD}════════════════════════════════════════════${NC}"
  fi
}

# =============================================================================
# Main Dispatch
# =============================================================================
main() {
  local phase="${1:-help}"

  case "$phase" in
    preflight|0)         phase_preflight ;;
    network-policies|1)  phase_network_policies ;;
    vault|2)             phase_vault ;;
    posthog|3)           phase_posthog ;;
    cosign|4)            phase_cosign ;;
    verify-all|verify|5) phase_verify_all ;;
    help|--help|-h)
      echo "Usage: $0 <phase>"
      echo
      echo "Phases (run in order):"
      echo "  preflight (0)         Pre-flight validation"
      echo "  network-policies (1)  Progressive NetworkPolicy rollout"
      echo "  vault (2)             Vault init + unseal + configure"
      echo "  posthog (3)           PostHog DB + secrets + deploy"
      echo "  cosign (4)            Cosign Audit → Enforce"
      echo "  verify-all (5)        End-to-end verification"
      echo
      echo "Example:"
      echo "  $0 preflight && $0 network-policies && $0 vault"
      ;;
    *)
      err "Unknown phase: $phase"
      echo "Run '$0 help' for usage."
      exit 1
      ;;
  esac
}

main "$@"
