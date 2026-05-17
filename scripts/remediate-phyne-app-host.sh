#!/usr/bin/env bash
# Restore app.phyne.app through Enclii-managed DNS paths.
#
# Dry-run by default. Use --apply only after the domain is registered/restored
# and the required provider credentials are present through approved secret
# management. Secret values are never printed by this script.

set -euo pipefail

TARGET="${TARGET:-app.phyne.app}"
DOMAIN="${DOMAIN:-phyne.app}"
RECORD_TYPE="${RECORD_TYPE:-CNAME}"
RECORD_CONTENT="${RECORD_CONTENT:-c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com}"
REASON="${REASON:-restore PhyneCRM app host through Enclii}"
ENCLII_BIN="${ENCLII_BIN:-enclii}"
APPLY=false
REPAIR_VAULT=true

log() { printf '[INFO] %s\n' "$*"; }
ok() { printf '[ OK ] %s\n' "$*"; }
warn() { printf '[WARN] %s\n' "$*" >&2; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage: scripts/remediate-phyne-app-host.sh [--apply] [--skip-vault-repair]

Environment:
  TARGET          Host to restore. Default: app.phyne.app
  DOMAIN          Apex domain. Default: phyne.app
  RECORD_CONTENT  Tunnel CNAME target. Default: Enclii prod tunnel CNAME
  REASON          Audit reason used for Enclii apply operations
  ENCLII_BIN      CLI binary to use. Default: enclii
  VAULT_TOKEN     Optional. If present, repairs vault-store before provider checks.

The script exits before DNS mutations if the apex domain is not registered.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --apply)
      APPLY=true
      shift
      ;;
    --skip-vault-repair)
      REPAIR_VAULT=false
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

json_field() {
  jq -r "$1 // empty"
}

domain_registered() {
  local tmp status
  tmp="$(mktemp)"
  status="$(curl -sS -o "$tmp" -w '%{http_code}' -L "https://rdap.org/domain/${DOMAIN}" || true)"
  if [[ "$status" == "200" ]]; then
    rm -f "$tmp"
    return 0
  fi

  warn "RDAP returned HTTP ${status} for ${DOMAIN}"
  if [[ "$status" == "000" ]]; then
    rm -f "$tmp"
    return 2
  fi

  if grep -q "not found" "$tmp"; then
    warn "Registry reports ${DOMAIN} not found"
  fi
  rm -f "$tmp"
  return 1
}

assert_domain_ready() {
  log "Checking registration and delegation for ${DOMAIN}..."
  set +e
  domain_registered
  local registered_rc=$?
  set -e
  if [[ "$registered_rc" -eq 2 ]]; then
    die "could not verify ${DOMAIN} through RDAP; retry when external DNS/network is available"
  fi
  if [[ "$registered_rc" -ne 0 ]]; then
    dig "@ns-tld1.charlestonroadregistry.com" "$DOMAIN" NS || true
    die "${DOMAIN} is not registered/restored; register or restore it before DNS apply"
  fi

  dig "$DOMAIN" NS || true
  ok "${DOMAIN} exists at RDAP"
}

vault_store_ready() {
  local ready
  ready="$(kubectl get clustersecretstore vault-store -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  [[ "$ready" == "True" ]]
}

porkbun_secret_ready() {
  local ready
  ready="$(kubectl -n enclii get externalsecret enclii-porkbun-credentials -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  [[ "$ready" == "True" ]]
}

repair_vault_if_possible() {
  if vault_store_ready; then
    ok "vault-store is Ready"
    return 0
  fi

  warn "vault-store is not Ready"
  kubectl get clustersecretstore vault-store || true

  if [[ "$REPAIR_VAULT" != "true" ]]; then
    return 1
  fi

  if [[ -z "${VAULT_TOKEN:-}" ]]; then
    warn "VAULT_TOKEN is not set; skipping Vault ESO auth repair"
    return 1
  fi

  log "Repairing Vault ESO auth..."
  scripts/repair-vault-eso-auth.sh
  vault_store_ready
}

ensure_provider_prereqs() {
  repair_vault_if_possible || true

  if porkbun_secret_ready; then
    ok "enclii-porkbun-credentials is Ready"
  else
    warn "enclii-porkbun-credentials is not Ready"
    kubectl -n enclii get externalsecret enclii-porkbun-credentials || true
  fi

  if kubectl -n enclii get deployment switchyard-api -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}{"\n"}{end}' \
    | grep -qx 'ENCLII_PORKBUN_API_KEY'; then
    ok "switchyard-api has Porkbun env wiring"
  else
    warn "switchyard-api does not yet have Porkbun env wiring; roll the production manifest after secret sync"
  fi
}

run_cloudflare_plan() {
  "$ENCLII_BIN" providers cloudflare dns-apply "$TARGET" --json
}

run_porkbun_plan() {
  "$ENCLII_BIN" providers porkbun dns-apply "$TARGET" \
    --domain "$DOMAIN" \
    --type "$RECORD_TYPE" \
    --content "$RECORD_CONTENT" \
    --json
}

apply_cloudflare() {
  "$ENCLII_BIN" providers cloudflare dns-apply "$TARGET" \
    --apply \
    --reason "$REASON" \
    --json
}

apply_porkbun() {
  "$ENCLII_BIN" providers porkbun dns-apply "$TARGET" \
    --domain "$DOMAIN" \
    --type "$RECORD_TYPE" \
    --content "$RECORD_CONTENT" \
    --apply \
    --reason "$REASON" \
    --json
}

restore_dns() {
  local cf_plan cf_status pb_plan pb_status

  log "Checking Cloudflare authority path..."
  cf_plan="$(run_cloudflare_plan)"
  printf '%s\n' "$cf_plan" | jq '{operation,status,summary,data,next}'
  cf_status="$(printf '%s\n' "$cf_plan" | json_field '.status')"

  if [[ "$cf_status" != "blocked_by_dns_authority" ]]; then
    if [[ "$APPLY" == "true" ]]; then
      log "Applying Cloudflare DNS change through Enclii..."
      apply_cloudflare | jq '{operation,status,summary,data,next}'
    else
      ok "Cloudflare path is not blocked. Re-run with --apply to mutate DNS."
    fi
    return 0
  fi

  log "Cloudflare lacks authority; checking Porkbun fallback..."
  pb_plan="$(run_porkbun_plan)"
  printf '%s\n' "$pb_plan" | jq '{operation,status,summary,data,next,warnings}'
  pb_status="$(printf '%s\n' "$pb_plan" | json_field '.status')"

  if [[ "$pb_status" == "adapter_unconfigured" ]]; then
    die "Porkbun adapter is not configured; repair vault-store and sync enclii-porkbun-credentials"
  fi

  if [[ "$APPLY" == "true" ]]; then
    log "Applying Porkbun DNS fallback through Enclii..."
    apply_porkbun | jq '{operation,status,summary,data,next,warnings}'
  else
    ok "Porkbun path is available. Re-run with --apply to mutate DNS."
  fi
}

verify_final_state() {
  log "Checking public DNS for ${TARGET}..."
  dig "$TARGET" CNAME || true

  log "Checking live status summary..."
  curl -fsS https://status.madfam.io/api/status \
    | jq '{overall, total: (.services|length), affected: [.services[] | select(.status != "operational") | {service,url,status,error}]}'

  log "Checking active incidents..."
  curl -fsS 'https://status.madfam.io/api/incidents?status=investigating' \
    | jq '{total, incidents: [.incidents[] | {id,title,status,affectedServices}]}'
}

main() {
  require_command curl
  require_command dig
  require_command jq
  require_command kubectl
  require_command "$ENCLII_BIN"

  assert_domain_ready
  ensure_provider_prereqs
  restore_dns

  if [[ "$APPLY" == "true" ]]; then
    verify_final_state
  fi
}

main "$@"
