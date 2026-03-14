#!/bin/bash
# Janua Health Check and Auto-Healing Script
# Monitors Janua services and attempts automatic recovery on failures
# Usage: ./janua-healthcheck.sh [--heal] [--notify] [--verbose]
#
# Run via cron for continuous monitoring:
#   */5 * * * * /path/to/janua-healthcheck.sh --heal --notify >> /var/log/janua-health.log 2>&1

set -euo pipefail

# Configuration
SSH_HOST="${JANUA_SSH_HOST:?Set JANUA_SSH_HOST (e.g. user@host)}"
NAMESPACE="janua"
SLACK_WEBHOOK="${JANUA_SLACK_WEBHOOK:-}"
MAX_RESTART_ATTEMPTS=3
RESTART_COOLDOWN=300  # seconds between restart attempts

# Service definitions: name -> port
declare -A SERVICES=(
  ["janua-api"]=4100
  ["janua-dashboard"]=4101
  ["janua-admin"]=4102
  ["janua-docs"]=4103
  ["janua-website"]=4104
)

# Public URL definitions
declare -A PUBLIC_URLS=(
  ["janua-api"]="https://api.janua.dev/health"
  ["janua-dashboard"]="https://app.janua.dev/"
  ["janua-admin"]="https://admin.janua.dev/"
  ["janua-docs"]="https://docs.janua.dev/"
  ["janua-website"]="https://janua.dev/"
)

# Parse arguments
HEAL=false
NOTIFY=false
VERBOSE=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --heal) HEAL=true; shift ;;
    --notify) NOTIFY=true; shift ;;
    --verbose) VERBOSE=true; shift ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

verbose() {
  if $VERBOSE; then
    log "[VERBOSE] $*"
  fi
}

send_alert() {
  local message="$1"
  local severity="${2:-warning}"

  log "[ALERT:$severity] $message"

  if $NOTIFY && [[ -n "$SLACK_WEBHOOK" ]]; then
    local color="warning"
    [[ "$severity" == "critical" ]] && color="danger"
    [[ "$severity" == "ok" ]] && color="good"

    curl -s -X POST "$SLACK_WEBHOOK" \
      -H 'Content-Type: application/json' \
      -d "{\"attachments\":[{\"color\":\"$color\",\"title\":\"Janua Health Alert\",\"text\":\"$message\",\"footer\":\"janua-healthcheck\",\"ts\":$(date +%s)}]}" \
      >/dev/null 2>&1 || true
  fi
}

# Check pod health via kubectl
check_pod_health() {
  local service="$1"
  local result

  result=$(ssh -o ConnectTimeout=10 "$SSH_HOST" \
    "sudo kubectl get pods -n $NAMESPACE -l app=$service -o jsonpath='{.items[*].status.phase}'" 2>/dev/null) || {
    verbose "Failed to connect to cluster for $service"
    return 1
  }

  if [[ -z "$result" ]]; then
    verbose "No pods found for $service"
    return 1
  fi

  if [[ "$result" == *"Running"* ]]; then
    verbose "$service pod is Running"
    return 0
  else
    verbose "$service pod status: $result"
    return 1
  fi
}

# Check public URL health
check_url_health() {
  local url="$1"
  local http_code

  http_code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "$url" 2>/dev/null) || {
    verbose "Failed to reach $url"
    return 1
  }

  if [[ "$http_code" =~ ^(200|301|302|307|308)$ ]]; then
    verbose "$url returned HTTP $http_code"
    return 0
  else
    verbose "$url returned HTTP $http_code (unhealthy)"
    return 1
  fi
}

# Attempt to restart a service
restart_service() {
  local service="$1"

  log "Attempting to restart $service..."

  if ssh -o ConnectTimeout=10 "$SSH_HOST" \
    "sudo kubectl rollout restart deploy/$service -n $NAMESPACE" 2>/dev/null; then
    log "Restart initiated for $service"

    # Wait for rollout to complete (max 2 minutes)
    if ssh -o ConnectTimeout=10 "$SSH_HOST" \
      "sudo kubectl rollout status deploy/$service -n $NAMESPACE --timeout=120s" 2>/dev/null; then
      log "Restart completed for $service"
      return 0
    else
      log "Restart timed out for $service"
      return 1
    fi
  else
    log "Failed to initiate restart for $service"
    return 1
  fi
}

# Get restart count for cooldown tracking
get_restart_count() {
  local service="$1"
  local count_file="/tmp/janua-restart-$service"

  if [[ -f "$count_file" ]]; then
    local last_time count
    read -r last_time count < "$count_file"
    local now=$(date +%s)

    if (( now - last_time > RESTART_COOLDOWN )); then
      echo "0"
    else
      echo "$count"
    fi
  else
    echo "0"
  fi
}

# Increment restart count
increment_restart_count() {
  local service="$1"
  local count_file="/tmp/janua-restart-$service"
  local count=$(get_restart_count "$service")

  echo "$(date +%s) $((count + 1))" > "$count_file"
}

# Main health check loop
main() {
  log "Starting Janua health check..."

  local overall_healthy=true
  local unhealthy_services=()
  local healed_services=()

  for service in "${!SERVICES[@]}"; do
    local port="${SERVICES[$service]}"
    local url="${PUBLIC_URLS[$service]:-}"
    local pod_healthy=true
    local url_healthy=true

    verbose "Checking $service (port $port)..."

    # Check pod health
    if ! check_pod_health "$service"; then
      pod_healthy=false
    fi

    # Check public URL if defined
    if [[ -n "$url" ]]; then
      if ! check_url_health "$url"; then
        url_healthy=false
      fi
    fi

    # Determine overall service health
    if ! $pod_healthy || ! $url_healthy; then
      overall_healthy=false
      unhealthy_services+=("$service")

      log "[UNHEALTHY] $service - pod:$pod_healthy url:$url_healthy"

      # Attempt auto-healing if enabled
      if $HEAL; then
        local restart_count=$(get_restart_count "$service")

        if (( restart_count < MAX_RESTART_ATTEMPTS )); then
          if restart_service "$service"; then
            increment_restart_count "$service"
            healed_services+=("$service")
            send_alert "$service was unhealthy and has been restarted" "warning"
          else
            send_alert "$service restart failed after attempt $((restart_count + 1))" "critical"
          fi
        else
          send_alert "$service has been restarted $restart_count times in the last $RESTART_COOLDOWN seconds - manual intervention required" "critical"
        fi
      fi
    else
      verbose "[HEALTHY] $service"
    fi
  done

  # Summary
  if $overall_healthy; then
    log "All Janua services are healthy"
  else
    log "Unhealthy services: ${unhealthy_services[*]}"
    if [[ ${#healed_services[@]} -gt 0 ]]; then
      log "Healed services: ${healed_services[*]}"
    fi
  fi

  # Return appropriate exit code
  if $overall_healthy || [[ ${#healed_services[@]} -eq ${#unhealthy_services[@]} ]]; then
    exit 0
  else
    exit 1
  fi
}

main "$@"
