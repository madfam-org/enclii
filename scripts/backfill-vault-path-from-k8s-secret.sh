#!/usr/bin/env bash
set -euo pipefail

VAULT_NAMESPACE="${VAULT_NAMESPACE:-vault}"
VAULT_POD="${VAULT_POD:-vault-0}"
DRY_RUN=false
NAMESPACE=""
SECRET=""
VAULT_PATH=""

usage() {
  cat <<'EOF'
Usage: scripts/backfill-vault-path-from-k8s-secret.sh --namespace <ns> --secret <name> --vault-path <path> [--dry-run]

Backfill a flat Vault KV v2 path from an existing Kubernetes Secret without
printing secret values. Kubernetes Secret keys are normalized to lower snake
case before writing to Vault:

  API_KEY_SALT       -> api_key_salt
  JANUA_JWT_SECRET   -> janua_jwt_secret

Required for writes:
  VAULT_TOKEN       Vault token with read/write access to the target path, or
  VAULT_TOKEN_FILE  Local file containing that token.

Examples:
  scripts/backfill-vault-path-from-k8s-secret.sh \
    --namespace forgesight \
    --secret forgesight-secrets \
    --vault-path secret/forgesight \
    --dry-run

  VAULT_TOKEN_FILE=/secure/vault-token \
    scripts/backfill-vault-path-from-k8s-secret.sh \
      --namespace forgesight \
      --secret forgesight-secrets \
      --vault-path secret/forgesight
EOF
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace)
      NAMESPACE="${2:-}"
      shift 2
      ;;
    --secret)
      SECRET="${2:-}"
      shift 2
      ;;
    --vault-path)
      VAULT_PATH="${2:-}"
      shift 2
      ;;
    --vault-namespace)
      VAULT_NAMESPACE="${2:-}"
      shift 2
      ;;
    --vault-pod)
      VAULT_POD="${2:-}"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ -n "$NAMESPACE" ]] || die "--namespace is required"
[[ -n "$SECRET" ]] || die "--secret is required"
[[ -n "$VAULT_PATH" ]] || die "--vault-path is required"

command -v kubectl >/dev/null || die "kubectl is required"
command -v jq >/dev/null || die "jq is required"

tmpdir="$(mktemp -d)"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT

k8s_secret_json="$tmpdir/k8s-secret.json"
updates_json="$tmpdir/updates.json"
existing_json="$tmpdir/existing.json"
merged_json="$tmpdir/merged.json"
vault_raw_json="$tmpdir/vault-raw.json"
vault_err="$tmpdir/vault.err"

kubectl -n "$NAMESPACE" get secret "$SECRET" -o json > "$k8s_secret_json"

jq '
  (.data // {})
  | with_entries(
      .key |= (
        ascii_downcase
        | gsub("[^a-z0-9]+"; "_")
        | gsub("^_+|_+$"; "")
      )
      | .value |= @base64d
    )
' "$k8s_secret_json" > "$updates_json"

key_count="$(jq 'length' "$updates_json")"
[[ "$key_count" -gt 0 ]] || die "$NAMESPACE/$SECRET has no data keys"

key_list="$(jq -r 'keys | join(", ")' "$updates_json")"

if [[ "$DRY_RUN" == true ]]; then
  printf 'Would merge %s key(s) from %s/%s into Vault %s: %s\n' \
    "$key_count" "$NAMESPACE" "$SECRET" "$VAULT_PATH" "$key_list"
  exit 0
fi

if [[ -z "${VAULT_TOKEN:-}" ]]; then
  if [[ -n "${VAULT_TOKEN_FILE:-}" ]]; then
    [[ -r "$VAULT_TOKEN_FILE" ]] || die "VAULT_TOKEN_FILE is not readable: $VAULT_TOKEN_FILE"
    VAULT_TOKEN="$(tr -d '\r\n' < "$VAULT_TOKEN_FILE")"
    export VAULT_TOKEN
  else
    die "VAULT_TOKEN or VAULT_TOKEN_FILE is required for writes"
  fi
fi

if kubectl exec -n "$VAULT_NAMESPACE" "$VAULT_POD" -- \
  env "VAULT_TOKEN=$VAULT_TOKEN" \
  vault kv get -format=json "$VAULT_PATH" > "$vault_raw_json" 2> "$vault_err"; then
  jq '.data.data // {}' "$vault_raw_json" > "$existing_json"
else
  if rg -q "No value found|not found|does not exist" "$vault_err"; then
    printf '{}\n' > "$existing_json"
  else
    sed -n '1,3p' "$vault_err" >&2
    die "failed to read existing Vault path $VAULT_PATH"
  fi
fi

jq -s '.[0] * .[1]' "$existing_json" "$updates_json" > "$merged_json"

kubectl exec -i -n "$VAULT_NAMESPACE" "$VAULT_POD" -- \
  env "VAULT_TOKEN=$VAULT_TOKEN" \
  vault kv put -format=json "$VAULT_PATH" - < "$merged_json" >/dev/null

printf 'Patched Vault %s with %s key(s): %s\n' "$VAULT_PATH" "$key_count" "$key_list"
