#!/usr/bin/env bash
# Provision Vault policy + enclii/vault-credentials for Switchyard secret intake.
#
# Requires a Vault admin token (root or policy writer). Token is read from
# VAULT_TOKEN or VAULT_TOKEN_FILE and is never printed.
#
# Usage:
#   VAULT_TOKEN_FILE=~/.config/madfam/vault-admin.token \
#     ./scripts/provision-switchyard-vault-writer.sh
#
# Policy-only (add paths without rotating writer token):
#   POLICY_ONLY=1 VAULT_TOKEN_FILE=... ./scripts/provision-switchyard-vault-writer.sh
#
# Last Updated: 2026-10-08 (selva: intake target selva/yantra4d-client for the selva-yantra4d machine edge)
set -euo pipefail

VAULT_NS="${VAULT_NS:-vault}"
VAULT_POD="${VAULT_POD:-vault-0}"
ENCLII_NS="${ENCLII_NS:-enclii}"
SECRET_NAME="${SECRET_NAME:-vault-credentials}"
VAULT_ADDR="${VAULT_ADDR:-http://vault.vault.svc.cluster.local:8200}"
POLICY_NAME="${POLICY_NAME:-switchyard-secret-writer}"
TOKEN_DISPLAY_NAME="${TOKEN_DISPLAY_NAME:-switchyard-api-intake}"
TTL="${TTL:-8760h}"

log() { printf '[INFO] %s\n' "$*"; }
die() { printf '[FAIL] %s\n' "$*" >&2; exit 1; }

command -v kubectl >/dev/null || die "kubectl required"

if [[ -z "${VAULT_TOKEN:-}" ]]; then
  if [[ -n "${VAULT_TOKEN_FILE:-}" ]]; then
    [[ -r "$VAULT_TOKEN_FILE" ]] || die "VAULT_TOKEN_FILE not readable"
    VAULT_TOKEN="$(tr -d '\r\n' < "$VAULT_TOKEN_FILE")"
    export VAULT_TOKEN
  else
    die "Set VAULT_TOKEN or VAULT_TOKEN_FILE"
  fi
fi

vault_exec() {
  kubectl exec -n "$VAULT_NS" "$VAULT_POD" -- \
    env "VAULT_TOKEN=${VAULT_TOKEN}" vault "$@"
}

POLICY_HCL="$(mktemp)"
trap 'rm -f "$POLICY_HCL"' EXIT
cat >"$POLICY_HCL" <<'EOF'
# Switchyard secret intake + vault-backfill writer (P0)
# Merge into platform secret paths; list/read metadata for merge semantics.
path "secret/data/ceq" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/ceq/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/dhanam" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/dhanam/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/enclii" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/enclii/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/comms" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/comms/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/pgbackrest-r2" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/pgbackrest-r2/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/karafiel" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/karafiel/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/lexidrop" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/lexidrop/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/phynd-crm" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/phynd-crm/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/coupler" {
  capabilities = ["create", "update", "patch", "read"]
}
# nauta — added 2026-08-11. The intake registry gained nauta/oidc-janua in
# enclii#379 and this policy did not follow, so the FIRST live use of
# `enclii secrets provision oidc --platform nauta` failed: Janua reconciled the
# client, then the Vault merge got 403 permission denied, surfaced to the CLI
# as an opaque "API error 500: failed to write to Vault". The registry and this
# policy are two copies of one truth; scripts/check-intake-policy-parity.sh now
# fails CI when they drift, so the next platform added to the registry cannot
# ship without its policy path.
path "secret/data/nauta" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/nauta/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# telesia (2026-09-12, enclii#546): the intake registry writes secret/telesia
# (targets telesia/oidc-janua and telesia/runtime); without this path the
# vault-backfill dry-run reads ready_to_apply and the apply returns 403.
path "secret/data/telesia" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/telesia/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/coupler/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/janua" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/janua/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/madfam-site" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/madfam-site/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# kalya — added 2026-09-04. Unlike crea-map/symbiosis-hcm, kalya is not an
# intake target: nothing writes secret/kalya through `secrets intake submit`.
# It is here because `enclii secrets provision kalya-feed` READS
# secret/kalya:internal_api_key (kalya_feed_provisioner.go) to authorize minting
# the feed token before writing the rendered URLs to secret/crea-map and the
# token map to secret/nauta. A read capability the policy does not grant fails
# the same opaque way a write does, so the whole three-path set ships together.
path "secret/data/kalya" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/kalya/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# crea-map + symbiosis-hcm — added 2026-09-03 alongside the intake targets for
# the MAP smoke gate, the kalya feeds and the HCM absence feed. Both apps are
# deployed through Enclii rather than this repo's infra/ tree, but the writer
# policy is what Switchyard's own token carries, so their paths belong here or
# the very first intake 403s exactly the way nauta did in enclii#379.
path "secret/data/crea-map" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/crea-map/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/symbiosis-hcm" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/symbiosis-hcm/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# crea (the TENANT, distinct from the crea-map app above) — holds Crea Tu
# Mundo's own Porkbun registrar API credentials, intake target
# crea/porkbun-registrar. READ matters as much as write here: unlike every
# other path in this policy, Switchyard reads these back on every registrar
# operation to build a per-tenant Porkbun client (the global ENCLII_PORKBUN_*
# key belongs to MADFAM's account and cannot see a client-owned domain at all).
# Without this block the first `providers porkbun --tenant crea` call 403s and
# surfaces as "credentials missing", indistinguishable from never having run
# the intake.
path "secret/data/crea" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/crea/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/phynd-crm-staging" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/phynd-crm-staging/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# angelia — added 2026-09-05 with the three Courier intake targets
# (angelia/courier-producer-keys, angelia/courier-channel-tokens,
# angelia/courier-alertmanager). Angelia VERIFIES every Courier credential, so
# secret/angelia is their one writable home and every consumer — enclii's
# Alertmanager, tulana, the ops scripts — reads that single copy cross-path
# through ESO instead of holding its own. Without this block the very first
# `enclii secrets intake angelia/courier-alertmanager` 403s the way nauta did
# in enclii#379, days after a green merge.
path "secret/data/angelia" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/angelia/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# creator-census — added 2026-09-24 with the intake targets
# creator-census/web-oidc (written by `enclii secrets provision oidc --platform
# creator-census-web`) and creator-census/web-session (`--generate
# session_secret`). Merged in git is not applied in Vault: until an operator
# re-applies this policy (ASSERT_PATH=creator-census
# scripts/apply-switchyard-vault-policy-remote.sh), the first provision run
# reconciles the Janua client and then 403s on the Vault merge.
path "secret/data/creator-census" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/creator-census/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# family-history — added 2026-10-01 with the intake targets
# family-history/web-oidc (written by `enclii secrets provision oidc --platform
# family-history-web`), family-history/web-session (`--generate
# fh_session_secret`) and family-history/api-access (the early-access
# allowlist, typed at the masked prompt). Merged in git is not applied in
# Vault: until an operator re-applies this policy (ASSERT_PATH=family-history
# scripts/apply-switchyard-vault-policy-remote.sh), the first provision run
# reconciles the Janua client and then 403s on the Vault merge.
path "secret/data/family-history" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/family-history/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# Digital-twins machine edges — added 2026-10-03 with the nine
# `<app>/...` client_credentials intake targets, each written only by
# `enclii secrets provision oidc --platform <id>`. One path per consumer:
# pravara-mes (three targets), yantra4d, fashion-cabinet, forj,
# digifab-quoting (Cotiza), zavlo and routecraft. Merged in git is not applied
# in Vault: re-apply this policy (ASSERT_PATH=pravara-mes
# scripts/apply-switchyard-vault-policy-remote.sh) before the first run, or
# the run rotates the Janua secret and then 403s on the Vault merge.
path "secret/data/pravara-mes" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/pravara-mes/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/yantra4d" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/yantra4d/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/fashion-cabinet" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/fashion-cabinet/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/forj" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/forj/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/digifab-quoting" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/digifab-quoting/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/zavlo" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/zavlo/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/routecraft" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/routecraft/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# voxa — added 2026-10-04 with the voxa/selva-client intake target (Voxa's
# API → Selva inference edge), written only by
# `enclii secrets provision oidc --platform voxa-selva`. Merged in git is not
# applied in Vault: re-apply this policy (ASSERT_PATH=voxa
# scripts/apply-switchyard-vault-policy-remote.sh) before the first run, or
# the run rotates the Janua secret and then 403s on the Vault merge.
path "secret/data/voxa" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/voxa/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# voxa-staging — added 2026-10-04 with voxa-staging/web-session and
# voxa-staging/api-runtime (staging's AUTH_SECRET and REDIS_URL). The
# production voxa targets voxa/web-session and voxa/api-runtime write the
# secret/voxa paths above. Re-apply with ASSERT_PATH=voxa-staging.
path "secret/data/voxa-staging" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/voxa-staging/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# tulana — added 2026-10-05 with the four tulana/* intake targets, each a key
# tulana generates for itself (`--generate <key>`) at secret/tulana. Merged in
# git is not applied in Vault: re-apply this policy (ASSERT_PATH=tulana
# scripts/apply-switchyard-vault-policy-remote.sh) before the first intake, or
# the intake 403s on the Vault merge.
path "secret/data/tulana" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/tulana/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# converge-dash — added 2026-10-05 with converge-dash/session and
# converge-dash/internal-read (`--generate dash_session_secret`,
# `--generate dash_internal_read_token`). Re-apply with
# ASSERT_PATH=converge-dash before the first intake.
path "secret/data/converge-dash" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/converge-dash/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# monitoring — added 2026-10-05 with the monitoring/alertmanager-smtp intake
# target (Alertmanager's SMTP password, typed by the owner at the masked
# prompt). secret/monitoring also holds the Grafana properties that
# monitoring-secrets reads; the intake write is a read-merge-write, so READ is
# required here too, and a denied read fails the submit rather than replacing
# the path. Merged in git is not applied in Vault: re-apply this policy
# (ASSERT_PATH=monitoring scripts/apply-switchyard-vault-policy-remote.sh)
# before the first submit, or it 403s.
path "secret/data/monitoring" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/monitoring/*" {
  capabilities = ["create", "update", "patch", "read"]
}
# selva — added 2026-10-08 with the selva/yantra4d-client intake target
# (Selva's phygital tools → Yantra4D edge), written only by
# `enclii secrets provision oidc --platform selva-yantra4d`. The write is a
# read-merge-write, so READ is required too. Merged in git is not applied in
# Vault: re-apply this policy (ASSERT_PATH=selva
# scripts/apply-switchyard-vault-policy-remote.sh) before the first run, or
# the run rotates the Janua secret and then 403s on the Vault merge.
path "secret/data/selva" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/data/selva/*" {
  capabilities = ["create", "update", "patch", "read"]
}
path "secret/metadata/ceq" {
  capabilities = ["read", "list"]
}
path "secret/metadata/ceq/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/dhanam" {
  capabilities = ["read", "list"]
}
path "secret/metadata/dhanam/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/enclii" {
  capabilities = ["read", "list"]
}
path "secret/metadata/enclii/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/comms" {
  capabilities = ["read", "list"]
}
path "secret/metadata/comms/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/pgbackrest-r2" {
  capabilities = ["read", "list"]
}
path "secret/metadata/pgbackrest-r2/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/karafiel" {
  capabilities = ["read", "list"]
}
path "secret/metadata/karafiel/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/phynd-crm" {
  capabilities = ["read", "list"]
}
path "secret/metadata/phynd-crm/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/coupler" {
  capabilities = ["read", "list"]
}
path "secret/metadata/coupler/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/janua" {
  capabilities = ["read", "list"]
}
path "secret/metadata/janua/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/madfam-site" {
  capabilities = ["read", "list"]
}
path "secret/metadata/madfam-site/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/kalya" {
  capabilities = ["read", "list"]
}
path "secret/metadata/kalya/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/crea-map" {
  capabilities = ["read", "list"]
}
path "secret/metadata/crea-map/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/symbiosis-hcm" {
  capabilities = ["read", "list"]
}
path "secret/metadata/symbiosis-hcm/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/phynd-crm-staging" {
  capabilities = ["read", "list"]
}
path "secret/metadata/phynd-crm-staging/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/nauta" {
  capabilities = ["read", "list"]
}
path "secret/metadata/nauta/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/lexidrop" {
  capabilities = ["read", "list"]
}
path "secret/metadata/lexidrop/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/angelia" {
  capabilities = ["read", "list"]
}
path "secret/metadata/angelia/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/creator-census" {
  capabilities = ["read", "list"]
}
path "secret/metadata/creator-census/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/family-history" {
  capabilities = ["read", "list"]
}
path "secret/metadata/family-history/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/pravara-mes" {
  capabilities = ["read", "list"]
}
path "secret/metadata/pravara-mes/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/yantra4d" {
  capabilities = ["read", "list"]
}
path "secret/metadata/yantra4d/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/fashion-cabinet" {
  capabilities = ["read", "list"]
}
path "secret/metadata/fashion-cabinet/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/forj" {
  capabilities = ["read", "list"]
}
path "secret/metadata/forj/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/digifab-quoting" {
  capabilities = ["read", "list"]
}
path "secret/metadata/digifab-quoting/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/zavlo" {
  capabilities = ["read", "list"]
}
path "secret/metadata/zavlo/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/routecraft" {
  capabilities = ["read", "list"]
}
path "secret/metadata/routecraft/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/voxa" {
  capabilities = ["read", "list"]
}
path "secret/metadata/voxa/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/voxa-staging" {
  capabilities = ["read", "list"]
}
path "secret/metadata/voxa-staging/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/tulana" {
  capabilities = ["read", "list"]
}
path "secret/metadata/tulana/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/converge-dash" {
  capabilities = ["read", "list"]
}
path "secret/metadata/converge-dash/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/monitoring" {
  capabilities = ["read", "list"]
}
path "secret/metadata/monitoring/*" {
  capabilities = ["read", "list"]
}
path "secret/metadata/selva" {
  capabilities = ["read", "list"]
}
path "secret/metadata/selva/*" {
  capabilities = ["read", "list"]
}
EOF

log "Writing Vault policy ${POLICY_NAME}..."
kubectl cp "$POLICY_HCL" "${VAULT_NS}/${VAULT_POD}:/tmp/${POLICY_NAME}.hcl"
vault_exec policy write "$POLICY_NAME" "/tmp/${POLICY_NAME}.hcl"

if [[ "${POLICY_ONLY:-}" == "1" ]]; then
  log "POLICY_ONLY=1 — skipping token rotation and switchyard-api restart"
  log "Done. Existing switchyard-secret-writer tokens inherit updated paths."
  exit 0
fi

log "Creating scoped token (TTL=${TTL})..."
token_json="$(vault_exec token create \
  -policy="$POLICY_NAME" \
  -display-name="$TOKEN_DISPLAY_NAME" \
  -ttl="$TTL" \
  -renewable=true \
  -format=json)"

writer_token="$(printf '%s' "$token_json" | python3 -c "import json,sys; print(json.load(sys.stdin)['auth']['client_token'])")"
[[ -n "$writer_token" ]] || die "failed to parse writer token from vault response"

log "Upserting Kubernetes secret ${ENCLII_NS}/${SECRET_NAME}..."
kubectl -n "$ENCLII_NS" create secret generic "$SECRET_NAME" \
  --from-literal=address="$VAULT_ADDR" \
  --from-literal=token="$writer_token" \
  --dry-run=client -o yaml | kubectl apply -f -

log "Rolling switchyard-api to pick up vault-credentials..."
kubectl -n "$ENCLII_NS" rollout restart deploy/switchyard-api
kubectl -n "$ENCLII_NS" rollout status deploy/switchyard-api --timeout=180s

log "Done. Verify: enclii secrets intake targets (requires admin ENCLII_TOKEN)"
