#!/usr/bin/env bash
# Generate a Switchyard API token for Dispatch and store it as a K8s secret.
#
# Uses a K8s Job with postgres:16-alpine to insert the token into the DB,
# since Switchyard pods don't have psql.
#
# Usage:
#   ./scripts/create-dispatch-api-token.sh [--dry-run]
#
# Prerequisites:
#   - kubectl access to the enclii namespace
#   - postgres-credentials secret with 'database-url' key

set -euo pipefail

DRY_RUN=false
if [[ "${1:-}" == "--dry-run" ]]; then
  DRY_RUN=true
fi

NAMESPACE="enclii"
SECRET_NAME="dispatch-secrets"
TOKEN_NAME="dispatch-service"
JOB_NAME="dispatch-api-token-setup"

# Generate token using the same algorithm as Switchyard:
# 32 random bytes → hex → prefix with "enclii_"
RAW_HEX=$(openssl rand -hex 32)
RAW_TOKEN="enclii_${RAW_HEX}"
PREFIX="${RAW_TOKEN:0:16}"
TOKEN_HASH=$(printf '%s' "$RAW_TOKEN" | shasum -a 256 | awk '{print $1}')

echo "Generated API token for Dispatch"
echo "  Prefix:  ${PREFIX}..."
echo "  Name:    ${TOKEN_NAME}"
echo "  Scopes:  [admin]"
echo ""

if $DRY_RUN; then
  echo "[dry-run] Would create K8s Job to insert token and patch dispatch-secrets."
  echo "[dry-run] Raw token (save this): ${RAW_TOKEN}"
  exit 0
fi

# --- Step 1: Clean up any previous job ---
kubectl -n "$NAMESPACE" delete job "$JOB_NAME" --ignore-not-found 2>/dev/null

# --- Step 2: Run a Job to insert the token into the database ---
echo "Creating K8s Job to insert token into database..."

kubectl apply -f - <<EOF
apiVersion: batch/v1
kind: Job
metadata:
  name: ${JOB_NAME}
  namespace: ${NAMESPACE}
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 120
  template:
    spec:
      restartPolicy: Never
      containers:
      - name: setup
        image: postgres:16-alpine
        command:
        - sh
        - -c
        - |
          set -e
          echo "Looking up admin user..."
          ADMIN_ID=\$(psql "\$DATABASE_URL" -tAc "SELECT id FROM users WHERE role = 'admin' LIMIT 1")
          if [ -z "\$ADMIN_ID" ]; then
            ADMIN_ID=\$(psql "\$DATABASE_URL" -tAc "SELECT id FROM users LIMIT 1")
          fi
          if [ -z "\$ADMIN_ID" ]; then
            echo "ERROR: No users found in database"
            exit 1
          fi
          echo "Using user_id: \$ADMIN_ID"

          # Revoke any existing dispatch-service token
          psql "\$DATABASE_URL" -c "
            UPDATE api_tokens SET revoked = true, revoked_at = now(), updated_at = now()
            WHERE name = '${TOKEN_NAME}' AND revoked = false;
          "

          psql "\$DATABASE_URL" -c "
            INSERT INTO api_tokens (id, user_id, name, prefix, token_hash, scopes, revoked, created_at, updated_at)
            VALUES (gen_random_uuid(), '\$ADMIN_ID', '${TOKEN_NAME}', '${PREFIX}', '${TOKEN_HASH}', '{admin}', false, now(), now());
          "
          echo "Token inserted successfully."
        env:
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: postgres-credentials
              key: database-url
EOF

echo "Waiting for Job to complete..."
kubectl -n "$NAMESPACE" wait --for=condition=complete "job/${JOB_NAME}" --timeout=60s

echo "Job completed. Checking logs:"
kubectl -n "$NAMESPACE" logs "job/${JOB_NAME}"

# --- Step 3: Patch K8s secret with the raw token ---
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

# --- Step 4: Cleanup ---
kubectl -n "$NAMESPACE" delete job "$JOB_NAME" --ignore-not-found 2>/dev/null

echo ""
echo "Done. Restart Dispatch to pick up the new secret:"
echo "  kubectl -n ${NAMESPACE} rollout restart deploy/dispatch"
