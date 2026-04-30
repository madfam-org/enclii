#!/bin/bash
# ops/cartographer.sh - Enclii Service Discovery & Adoption Tool
# Usage: ./ops/cartographer.sh [--dry-run]
#
# Discovers K8s services across all namespaces and populates the Enclii
# services table with appropriate project mappings and health data.

set -eo pipefail

# Configuration
DRY_RUN="${1:-}"
SSH_HOST="solarpunk@ssh.madfam.io"

# Helper function for namespace to project mapping
get_ns_project() {
  local ns="$1"
  case "$ns" in
    janua) echo "janua" ;;
    enclii) echo "enclii" ;;
    dhanam) echo "dhanam" ;;
    arc-runners) echo "anvil" ;;
    *) echo "solarpunk-foundry" ;;
  esac
}

# Helper function for service git repo lookup
get_service_repo() {
  local name="$1"
  local ns="$2"
  case "$name" in
    janua-api|janua-dashboard|janua-admin|janua-website|janua-docs)
      echo "https://github.com/madfam-org/janua" ;;
    switchyard-api|switchyard-ui|dispatch)
      echo "https://github.com/madfam-org/enclii" ;;
    dhanam-api|dhanam-web)
      echo "https://github.com/madfam-org/dhanam" ;;
    postgres|postgres-headless)
      echo "helm://bitnami/postgresql" ;;
    redis|redis-master|redis-headless)
      echo "helm://bitnami/redis" ;;
    prometheus|prometheus-server)
      echo "helm://prometheus-community/prometheus" ;;
    grafana)
      echo "helm://grafana/grafana" ;;
    cloudflared)
      echo "external://cloudflare/cloudflared" ;;
    runner-monitor)
      echo "https://github.com/actions/actions-runner-controller" ;;
    cert-manager|cert-manager-webhook)
      echo "helm://jetstack/cert-manager" ;;
    argocd-server|argocd-repo-server|argocd-redis|argocd-applicationset-controller|argocd-dex-server|argocd-notifications-controller)
      echo "helm://argo/argocd" ;;
    longhorn-backend|longhorn-frontend|longhorn-admission-webhook|longhorn-conversion-webhook|longhorn-recovery-backend)
      echo "helm://longhorn/longhorn" ;;
    *)
      echo "k8s://$ns/$name" ;;
  esac
}

# Helper function for app path lookup
get_app_path() {
  local name="$1"
  case "$name" in
    janua-api) echo "apps/api" ;;
    janua-dashboard) echo "apps/dashboard" ;;
    janua-admin) echo "apps/admin" ;;
    janua-website) echo "apps/landing" ;;
    janua-docs) echo "apps/docs" ;;
    switchyard-api) echo "apps/switchyard-api" ;;
    switchyard-ui) echo "apps/switchyard-ui" ;;
    dispatch) echo "apps/admin-console" ;;
    dhanam-api) echo "apps/api" ;;
    dhanam-web) echo "apps/web" ;;
    *) echo "" ;;
  esac
}

echo "=== Cartographer: Enclii Service Discovery ==="
echo "Mode: ${DRY_RUN:-LIVE}"
echo ""

# Get all project IDs upfront
echo "Fetching project IDs..."
PROJECT_DATA=$(ssh "$SSH_HOST" "sudo kubectl exec -n data postgres-0 -- psql -U enclii -d enclii -t -c 'SELECT slug, id FROM projects;'")

JANUA_ID=$(echo "$PROJECT_DATA" | grep "janua" | head -1 | sed 's/.*| *//' | xargs)
ENCLII_ID=$(echo "$PROJECT_DATA" | grep " enclii" | head -1 | sed 's/.*| *//' | xargs)
DHANAM_ID=$(echo "$PROJECT_DATA" | grep "dhanam" | head -1 | sed 's/.*| *//' | xargs)
ANVIL_ID=$(echo "$PROJECT_DATA" | grep "anvil" | head -1 | sed 's/.*| *//' | xargs)
FOUNDRY_ID=$(echo "$PROJECT_DATA" | grep "solarpunk-foundry" | head -1 | sed 's/.*| *//' | xargs)

echo "  janua -> $JANUA_ID"
echo "  enclii -> $ENCLII_ID"
echo "  dhanam -> $DHANAM_ID"
echo "  anvil -> $ANVIL_ID"
echo "  solarpunk-foundry -> $FOUNDRY_ID"

get_project_id() {
  local slug="$1"
  case "$slug" in
    janua) echo "$JANUA_ID" ;;
    enclii) echo "$ENCLII_ID" ;;
    dhanam) echo "$DHANAM_ID" ;;
    anvil) echo "$ANVIL_ID" ;;
    solarpunk-foundry) echo "$FOUNDRY_ID" ;;
  esac
}

echo ""
echo "Fetching K8s services..."
SERVICES_JSON=$(ssh "$SSH_HOST" "sudo kubectl get services -A -o json")

echo "Fetching K8s deployments and statefulsets..."
# Query BOTH - databases (Postgres, Redis) are StatefulSets, not Deployments
WORKLOADS_JSON=$(ssh "$SSH_HOST" "sudo kubectl get deployments,statefulsets -A -o json")

echo "Building deployment lookup file..."

# Build deployment lookup file: namespace/name|desired|ready
DEPLOYMENT_FILE="/tmp/cartographer_deployments_$$.txt"
echo "$WORKLOADS_JSON" | jq -r '.items[] | "\(.metadata.namespace)/\(.metadata.name)|\(.spec.replicas // 1)|\(.status.readyReplicas // 0)"' > "$DEPLOYMENT_FILE"

WORKLOAD_COUNT=$(wc -l < "$DEPLOYMENT_FILE" | xargs)
echo "  Found $WORKLOAD_COUNT workloads"

# Helper to look up deployment data
get_deployment_data() {
  local key="$1"
  grep "^${key}|" "$DEPLOYMENT_FILE" 2>/dev/null | head -1 || echo ""
}

echo ""
echo "Generating SQL..."

# Generate SQL file
SQL_FILE="/tmp/cartographer_$$.sql"
> "$SQL_FILE"

# Process services using process substitution to avoid subshell issues
while IFS='|' read -r namespace name; do
  [[ -z "$namespace" || -z "$name" ]] && continue

  case "$namespace" in
    kube-system|kube-public|kube-node-lease|default) continue ;;
  esac

  [[ "$name" == "kubernetes" ]] && continue

  project_slug=$(get_ns_project "$namespace")

  # Name-based override: janua-* services always go to janua project
  # This handles janua apps deployed in non-janua namespaces (e.g., enclii)
  if [[ "$name" == janua-* ]]; then
    project_slug="janua"
  fi

  project_id=$(get_project_id "$project_slug")
  [[ -z "$project_id" ]] && continue

  git_repo=$(get_service_repo "$name" "$namespace")
  app_path=$(get_app_path "$name")

  # Look up deployment health data
  deployment_key="${namespace}/${name}"
  deployment_data=$(get_deployment_data "$deployment_key")

  if [[ -n "$deployment_data" ]]; then
    IFS='|' read -r _ desired ready <<< "$deployment_data"

    # Calculate health
    if [[ "$desired" -gt 0 && "$desired" -eq "$ready" ]]; then
      health="healthy"
    elif [[ "$ready" -gt 0 ]]; then
      health="unhealthy"
    else
      health="unknown"
    fi

    # Calculate status
    if [[ "$ready" -gt 0 ]]; then
      status="running"
    elif [[ "$desired" -gt 0 ]]; then
      status="pending"
    else
      status="unknown"
    fi
  else
    desired=0
    ready=0
    health="unknown"
    status="unknown"
  fi

  # Special handling for runner-monitor: count active runner pods instead of deployment replicas
  # Runner pods are ephemeral - active means builds in progress, none means idle (normal)
  if [[ "$name" == "runner-monitor" ]]; then
    ACTIVE_RUNNERS=$(ssh "$SSH_HOST" "sudo kubectl get pods -n arc-runners -l app.kubernetes.io/component=runner --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l" | xargs)
    if [[ "$ACTIVE_RUNNERS" -gt 0 ]]; then
      health="healthy"
      status="running"
      desired=$ACTIVE_RUNNERS
      ready=$ACTIVE_RUNNERS
    else
      # No active runners = idle state (normal for ephemeral runners)
      health="unknown"
      status="pending"
      desired=0
      ready=0
    fi
  fi

  # Check if service exists and if health/status changed - create deployment record
  PREV_STATE=$(ssh "$SSH_HOST" "sudo kubectl exec -n data postgres-0 -- psql -U enclii -d enclii -t -c \"SELECT health, status, desired_replicas, ready_replicas FROM services WHERE project_id = '$project_id' AND name = '$name'\"" 2>/dev/null | xargs)

  if [[ -n "$PREV_STATE" ]]; then
    prev_health=$(echo "$PREV_STATE" | awk '{print $1}')
    prev_status=$(echo "$PREV_STATE" | awk '{print $2}')
    prev_desired=$(echo "$PREV_STATE" | awk '{print $3}')
    prev_ready=$(echo "$PREV_STATE" | awk '{print $4}')

    # Detect meaningful change
    if [[ "$health" != "$prev_health" ]] || [[ "$status" != "$prev_status" ]] || \
       [[ "$desired" != "$prev_desired" ]] || [[ "$ready" != "$prev_ready" ]]; then

      # Get service_id and default environment_id
      SERVICE_ID=$(ssh "$SSH_HOST" "sudo kubectl exec -n data postgres-0 -- psql -U enclii -d enclii -t -c \"SELECT id FROM services WHERE project_id = '$project_id' AND name = '$name'\"" 2>/dev/null | xargs)
      ENV_ID=$(ssh "$SSH_HOST" "sudo kubectl exec -n data postgres-0 -- psql -U enclii -d enclii -t -c \"SELECT id FROM environments WHERE project_id = '$project_id' ORDER BY created_at LIMIT 1\"" 2>/dev/null | xargs)

      if [[ -n "$SERVICE_ID" ]] && [[ -n "$ENV_ID" ]]; then
        # Create ghost deployment record for tracking
        cat >> "$SQL_FILE" << EOF
-- Ghost deployment record: $name ($prev_health -> $health)
INSERT INTO deployments (id, release_id, environment_id, replicas, status, health, created_at, updated_at)
SELECT gen_random_uuid(),
       (SELECT id FROM releases WHERE service_id = '$SERVICE_ID' ORDER BY created_at DESC LIMIT 1),
       '$ENV_ID',
       $desired,
       '$status',
       '$health',
       NOW(),
       NOW()
WHERE EXISTS (SELECT 1 FROM releases WHERE service_id = '$SERVICE_ID');
EOF
        echo "  [DEPLOYMENT] Recorded change: $name ($prev_health -> $health)"
      fi
    fi
  fi

  echo "[$namespace] $name -> $project_slug (health: $health, replicas: $ready/$desired)"

  # Build SQL with all health fields
  if [[ -n "$app_path" ]]; then
    cat >> "$SQL_FILE" << EOF
INSERT INTO services (id, project_id, name, git_repo, app_path, k8s_namespace, health, status, desired_replicas, ready_replicas, last_health_check, created_at, updated_at)
VALUES (gen_random_uuid(), '$project_id', '$name', '$git_repo', '$app_path', '$namespace', '$health', '$status', $desired, $ready, NOW(), NOW(), NOW())
ON CONFLICT (project_id, name)
DO UPDATE SET
    git_repo = EXCLUDED.git_repo,
    app_path = EXCLUDED.app_path,
    k8s_namespace = EXCLUDED.k8s_namespace,
    health = EXCLUDED.health,
    status = EXCLUDED.status,
    desired_replicas = EXCLUDED.desired_replicas,
    ready_replicas = EXCLUDED.ready_replicas,
    last_health_check = NOW(),
    updated_at = NOW();
EOF
  else
    cat >> "$SQL_FILE" << EOF
INSERT INTO services (id, project_id, name, git_repo, k8s_namespace, health, status, desired_replicas, ready_replicas, last_health_check, created_at, updated_at)
VALUES (gen_random_uuid(), '$project_id', '$name', '$git_repo', '$namespace', '$health', '$status', $desired, $ready, NOW(), NOW(), NOW())
ON CONFLICT (project_id, name)
DO UPDATE SET
    git_repo = EXCLUDED.git_repo,
    k8s_namespace = EXCLUDED.k8s_namespace,
    health = EXCLUDED.health,
    status = EXCLUDED.status,
    desired_replicas = EXCLUDED.desired_replicas,
    ready_replicas = EXCLUDED.ready_replicas,
    last_health_check = NOW(),
    updated_at = NOW();
EOF
  fi
done < <(echo "$SERVICES_JSON" | jq -r '.items[] | "\(.metadata.namespace)|\(.metadata.name)"')

# Cleanup deployment file
rm -f "$DEPLOYMENT_FILE"

# Count statements
STATEMENT_COUNT=$(grep -c "^INSERT INTO services" "$SQL_FILE" 2>/dev/null || echo "0")

echo ""
echo "Generated $STATEMENT_COUNT SQL statements"

if [[ "$DRY_RUN" == "--dry-run" ]]; then
  echo ""
  echo "=== DRY-RUN Complete ==="
  echo "Would insert/update $STATEMENT_COUNT services"
  echo ""
  echo "Sample SQL:"
  head -20 "$SQL_FILE"
  rm -f "$SQL_FILE"
  exit 0
fi

echo ""
echo "Executing batch insert via stdin..."

# Execute SQL by piping through SSH to kubectl exec
if cat "$SQL_FILE" | ssh "$SSH_HOST" "sudo kubectl exec -i -n data postgres-0 -- psql -U enclii -d enclii"; then
  echo ""
  echo "=== Discovery Complete ==="
  echo "Successfully upserted $STATEMENT_COUNT services with health data"
else
  echo ""
  echo "ERROR: Batch insert failed"
  exit 1
fi

rm -f "$SQL_FILE"
