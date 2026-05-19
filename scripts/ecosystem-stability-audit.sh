#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENCLII_BIN="${ENCLII_BIN:-$ROOT/bin/enclii}"
WINDOW_HOURS="${WINDOW_HOURS:-24}"
TMP_DIR="$(mktemp -d)"
ISSUES=0

trap 'rm -rf "$TMP_DIR"' EXIT

section() {
  printf '\n== %s ==\n' "$1"
}

warn() {
  printf 'WARN: %s\n' "$1" >&2
}

issue() {
  ISSUES=$((ISSUES + 1))
  printf 'ISSUE: %s\n' "$1"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    warn "missing required command: $1"
    exit 127
  fi
}

epoch_hours_ago() {
  if date -u -v-"$1"H +%s >/dev/null 2>&1; then
    date -u -v-"$1"H +%s
  else
    date -u -d "$1 hours ago" +%s
  fi
}

normalize_app_slug() {
  local value="$1"
  value="${value%-services}"
  case "$value" in
    core|dispatch)
      printf 'enclii\n'
      ;;
    *)
      printf '%s\n' "$value"
      ;;
  esac
}

slug_is_covered() {
  local slug="$1"
  local project
  if grep -qx "$slug" "$TMP_DIR/enclii-projects.txt"; then
    return 0
  fi
  while IFS= read -r project; do
    if [[ "$slug" == "$project"-* ]]; then
      return 0
    fi
  done < "$TMP_DIR/enclii-projects.txt"
  return 1
}

require_cmd jq
require_cmd kubectl
require_cmd curl

section "Enclii Project Inventory"
if "$ENCLII_BIN" projects list --json > "$TMP_DIR/enclii-projects.json"; then
  jq -r '
    if type == "array" then .[]
    elif has("projects") then .projects[]
    else empty end
    | .slug // .Slug // empty
  ' "$TMP_DIR/enclii-projects.json" | sort -u > "$TMP_DIR/enclii-projects.txt"
  printf 'Enclii projects: %s\n' "$(wc -l < "$TMP_DIR/enclii-projects.txt" | tr -d ' ')"
else
  issue "unable to read Enclii project inventory"
  : > "$TMP_DIR/enclii-projects.txt"
fi

section "Argo Coverage"
if kubectl -n argocd get applications.argoproj.io -o json > "$TMP_DIR/argo-apps.json"; then
  jq -r '.items[].metadata.name' "$TMP_DIR/argo-apps.json" | sort -u > "$TMP_DIR/argo-app-names.txt"
  : > "$TMP_DIR/argo-normalized.txt"
  while IFS= read -r app; do
    normalize_app_slug "$app" >> "$TMP_DIR/argo-normalized.txt"
  done < "$TMP_DIR/argo-app-names.txt"
  sort -u "$TMP_DIR/argo-normalized.txt" -o "$TMP_DIR/argo-normalized.txt"
  printf 'Argo applications: %s\n' "$(wc -l < "$TMP_DIR/argo-app-names.txt" | tr -d ' ')"

  missing=0
  while IFS= read -r app; do
    slug="$(normalize_app_slug "$app")"
    if ! slug_is_covered "$slug"; then
      printf 'Argo app without Enclii project coverage: %s -> %s\n' "$app" "$slug"
      missing=$((missing + 1))
    fi
  done < "$TMP_DIR/argo-app-names.txt"
  if [[ "$missing" -gt 0 ]]; then
    issue "$missing Argo application(s) lack Enclii project coverage"
  fi

  orphan_projects=0
  while IFS= read -r project; do
    if ! grep -qx "$project" "$TMP_DIR/argo-normalized.txt"; then
      printf 'Enclii project without direct Argo app: %s\n' "$project"
      orphan_projects=$((orphan_projects + 1))
    fi
  done < "$TMP_DIR/enclii-projects.txt"
  printf 'Enclii projects without direct Argo app: %s\n' "$orphan_projects"
else
  issue "unable to read Argo Applications"
fi

section "ExternalSecrets"
if kubectl get externalsecrets.external-secrets.io -A -o json > "$TMP_DIR/externalsecrets.json"; then
  not_ready="$(
    jq -r '
      .items[]
      | ([.status.conditions[]? | select(.type == "Ready")][0]) as $ready
      | select(($ready.status // "False") != "True")
      | "\(.metadata.namespace)/\(.metadata.name)\tReady=\($ready.status // "False")\t\($ready.reason // "unknown")"
    ' "$TMP_DIR/externalsecrets.json"
  )"
  if [[ -n "$not_ready" ]]; then
    printf '%s\n' "$not_ready"
    issue "ExternalSecret sync failures detected"
  else
    printf 'All ExternalSecrets report Ready=True\n'
  fi
else
  issue "unable to read ExternalSecrets"
fi

section "Unavailable Deployments"
if kubectl get deployments.apps -A -o json > "$TMP_DIR/deployments.json"; then
  unavailable="$(
    jq -r '
      .items[]
      | (.spec.replicas // 1) as $desired
      | (.status.availableReplicas // 0) as $available
      | select($desired > 0 and $available < $desired)
      | "\(.metadata.namespace)/\(.metadata.name)\tavailable=\($available)/\($desired)"
    ' "$TMP_DIR/deployments.json"
  )"
  if [[ -n "$unavailable" ]]; then
    printf '%s\n' "$unavailable"
    issue "unavailable Deployments detected"
  else
    printf 'All non-zero Deployment replica targets are available\n'
  fi
else
  issue "unable to read Deployments"
fi

section "Recent Failed Jobs"
cutoff_epoch="$(epoch_hours_ago "$WINDOW_HOURS")"
if kubectl get jobs.batch -A -o json > "$TMP_DIR/jobs.json"; then
  failed_jobs="$(
    jq -r --argjson cutoff "$cutoff_epoch" '
      def cron_name:
        ([.metadata.ownerReferences[]? | select(.kind == "CronJob") | .name][0]
          // (.metadata.name | sub("-[0-9]+$"; "")));
      [
        .items[]
        | ([.metadata.ownerReferences[]? | select(.kind == "CronJob")] | length) as $cron_owner_count
        | (.metadata.annotations["batch.kubernetes.io/cronjob-scheduled-timestamp"] // "") as $scheduled_annotation
        | select($cron_owner_count > 0 or $scheduled_annotation != "")
        | (.metadata.annotations["batch.kubernetes.io/cronjob-scheduled-timestamp"] // .status.completionTime // .metadata.creationTimestamp) as $ts
        | ($ts | fromdateiso8601? // 0) as $epoch
        | select($epoch >= $cutoff)
        | . + {"_cron_name": cron_name, "_scheduled": $ts, "_epoch": $epoch}
      ]
      | sort_by(.metadata.namespace, ._cron_name, ._epoch)
      | group_by([.metadata.namespace, ._cron_name])[]
      | last
      | ([.status.conditions[]? | select(.type == "Failed" and .status == "True")] | length) as $failed_condition
      | (.status.succeeded // 0) as $succeeded
      | (.status.active // 0) as $active
      | (.status.failed // 0) as $failed
      | select($failed_condition > 0 or ($succeeded == 0 and $active == 0 and $failed > 0))
      | "\(.metadata.namespace)/\(.metadata.name)\tcronjob=\(._cron_name)\tfailed=\($failed)\tscheduled=\(._scheduled)"
    ' "$TMP_DIR/jobs.json"
  )"
  if [[ -n "$failed_jobs" ]]; then
    printf '%s\n' "$failed_jobs"
    issue "recent failed Jobs detected within ${WINDOW_HOURS}h"
  else
    printf 'No failed Jobs within %sh\n' "$WINDOW_HOURS"
  fi
else
  issue "unable to read Jobs"
fi

section "Active Jobs Near Deadline"
now_epoch="$(date -u +%s)"
if [[ -s "$TMP_DIR/jobs.json" ]]; then
  active_jobs="$(
    jq -r --argjson now "$now_epoch" '
      .items[]
      | select((.status.active // 0) > 0)
      | (.status.startTime // .metadata.creationTimestamp) as $start
      | ($start | fromdateiso8601? // 0) as $start_epoch
      | (.spec.activeDeadlineSeconds // 0) as $deadline
      | ($now - $start_epoch) as $age
      | select(($deadline > 0 and $age > ($deadline * 0.8)) or ($deadline == 0 and $age > 1800))
      | "\(.metadata.namespace)/\(.metadata.name)\tactive=\(.status.active // 0)\tage_seconds=\($age)\tdeadline_seconds=\($deadline)"
    ' "$TMP_DIR/jobs.json"
  )"
  if [[ -n "$active_jobs" ]]; then
    printf '%s\n' "$active_jobs"
    issue "active Jobs are near or past their deadline"
  else
    printf 'No active Jobs are near their activeDeadlineSeconds threshold\n'
  fi
else
  issue "unable to evaluate active Jobs"
fi

section "Public Status APIs"
for url in "https://status.enclii.dev/api/status" "https://status.madfam.io/api/status"; do
  if curl -fsS "$url" > "$TMP_DIR/status.json"; then
    printf '%s\t%s\n' "$url" "$(jq -r '.overallStatus // .overall // .status // "unknown"' "$TMP_DIR/status.json")"
  else
    issue "status API unreachable: $url"
  fi
done

section "Result"
if [[ "$ISSUES" -gt 0 ]]; then
  printf 'Ecosystem stability audit found %s issue group(s).\n' "$ISSUES"
  exit 2
fi

printf 'Ecosystem stability audit found no issue groups.\n'
