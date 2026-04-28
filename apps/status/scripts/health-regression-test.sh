#!/usr/bin/env bash
# health-regression-test.sh — Pillar 5: external regression test for the status page itself.
#
# Architectural context: see claudedocs/ecosystem-deploy-+-status-architecture-2026-04-28.md.
#
# This is the "watcher of the watchers" — a daily, external, read-only sanity check
# that the status page itself has not silently regressed. It runs three independent
# checks and emits findings to stdout. The caller (GitHub Actions workflow) is
# responsible for taking the findings array and converting them into a single
# GH issue (or appending to an existing one) — this script only diagnoses.
#
# Output format:
#   - On success (all checks pass): exits 0, prints summary to stdout.
#   - On any failure: exits 1, writes one finding per failure to $FINDINGS_FILE,
#     and prints a human-readable summary to stdout.
#
# Findings format (one JSON object per line in $FINDINGS_FILE):
#   {"check":"image-freshness","severity":"alert","title":"...","detail":"..."}
#   {"check":"configmap-freshness","severity":"warn","title":"...","detail":"..."}
#   {"check":"assertion-effectiveness","severity":"alert","title":"...","detail":"..."}
#
# Severities:
#   - "alert" : caller should file/append GH issue
#   - "warn"  : caller should log to job summary only (data source unavailable, etc.)
#   - "info"  : check ran successfully, no action needed
#
# Required environment:
#   GIT_REPO_ROOT      : absolute path to enclii repo checkout (default: $PWD)
#   FINDINGS_FILE      : where to write JSON-line findings (default: ./findings.jsonl)
#   STATUS_PAGE_URL    : public URL of status page (default: https://status.madfam.io)
#   STATUS_API_URL     : status JSON endpoint (default: $STATUS_PAGE_URL/api/status)
#   IMAGE_STALENESS_DAYS : alert threshold for pod image age (default: 7)
#   CONFIGMAP_STALENESS_HOURS : alert threshold for configmap regenerate (default: 24)
#
# Optional environment (enables Check #1 pod inspection):
#   SSH_TUNNEL_HOST    : SSH host via cloudflared (default: ssh.madfam.io)
#   SSH_TUNNEL_USER    : SSH user (e.g. "operator")
#   SSH_PRIVATE_KEY_FILE : path to SSH key for tunnel access
#   CF_ACCESS_CLIENT_ID, CF_ACCESS_CLIENT_SECRET : optional Cloudflare service-token
#                        headers (only needed for non-interactive tunnel access)
#
# Without the SSH-cloudflared secrets, Check #1 emits a "warn" finding (data source
# unavailable) — it does NOT alert. This lets the workflow ship and incrementally
# unlock checks as secrets are provisioned.
#
# Exit codes:
#   0 : all checks passed (or only warns; no alerts)
#   1 : at least one alert finding written
#   2 : script-internal error (bad inputs, missing tools)
set -euo pipefail

GIT_REPO_ROOT="${GIT_REPO_ROOT:-$PWD}"
FINDINGS_FILE="${FINDINGS_FILE:-${GIT_REPO_ROOT}/findings.jsonl}"
STATUS_PAGE_URL="${STATUS_PAGE_URL:-https://status.madfam.io}"
STATUS_API_URL="${STATUS_API_URL:-${STATUS_PAGE_URL}/api/status}"
IMAGE_STALENESS_DAYS="${IMAGE_STALENESS_DAYS:-7}"
CONFIGMAP_STALENESS_HOURS="${CONFIGMAP_STALENESS_HOURS:-24}"
SSH_TUNNEL_HOST="${SSH_TUNNEL_HOST:-ssh.madfam.io}"

# Reset findings file so reruns are idempotent.
: > "$FINDINGS_FILE"

ALERT_COUNT=0
WARN_COUNT=0
INFO_COUNT=0

# emit_finding <check> <severity> <title> <detail>
emit_finding() {
    local check="$1"
    local severity="$2"
    local title="$3"
    local detail="$4"
    # JSON-encode using jq to escape control chars / quotes safely.
    jq -nc \
        --arg check "$check" \
        --arg severity "$severity" \
        --arg title "$title" \
        --arg detail "$detail" \
        '{check:$check, severity:$severity, title:$title, detail:$detail}' \
        >> "$FINDINGS_FILE"
    case "$severity" in
        alert) ALERT_COUNT=$((ALERT_COUNT + 1)); echo "ALERT  [$check] $title" >&2 ;;
        warn)  WARN_COUNT=$((WARN_COUNT + 1));   echo "WARN   [$check] $title" >&2 ;;
        info)  INFO_COUNT=$((INFO_COUNT + 1));   echo "OK     [$check] $title" >&2 ;;
    esac
}

# ssh_tunnel_available: returns 0 if SSH-via-cloudflared is configured, 1 otherwise.
ssh_tunnel_available() {
    [[ -n "${SSH_PRIVATE_KEY_FILE:-}" && -f "${SSH_PRIVATE_KEY_FILE:-/nonexistent}" ]] && \
    [[ -n "${SSH_TUNNEL_USER:-}" ]] && \
    command -v cloudflared >/dev/null 2>&1
}

# ssh_via_tunnel <remote-command>: runs the command on the cluster control plane node
# via the cloudflared SSH tunnel. STDOUT of the remote command is forwarded.
ssh_via_tunnel() {
    local cmd="$1"
    local proxy="cloudflared access ssh --hostname ${SSH_TUNNEL_HOST}"
    ssh -o StrictHostKeyChecking=no \
        -o UserKnownHostsFile=/dev/null \
        -o BatchMode=yes \
        -o ConnectTimeout=20 \
        -o "ProxyCommand=${proxy}" \
        -i "${SSH_PRIVATE_KEY_FILE}" \
        "${SSH_TUNNEL_USER}@${SSH_TUNNEL_HOST}" \
        "$cmd"
}

# ─────────────────────────────────────────────────────────────────────────────
# Check #1 — Pod image freshness
#
# The status page pod's running image SHA should be no more than
# $IMAGE_STALENESS_DAYS behind the most recent commit on main that touched
# apps/status/ (the "deployable commit" for the status app).
#
# Strategy:
#   1. Find the latest commit on main touching apps/status/ via `git log`.
#   2. Inspect the running pod's image via SSH-cloudflared + kubectl.
#   3. Extract the image tag's git SHA prefix (we tag images with the commit SHA).
#   4. Compute the time delta between (1) and the commit (3) refers to.
#
# Falls back to a "warn" finding (no alert) when SSH-cloudflared is unavailable.
# ─────────────────────────────────────────────────────────────────────────────
check_image_freshness() {
    cd "$GIT_REPO_ROOT"

    local latest_status_sha latest_status_date latest_status_iso
    latest_status_sha=$(git log -1 --format=%H -- apps/status/ 2>/dev/null || true)
    if [[ -z "$latest_status_sha" ]]; then
        emit_finding "image-freshness" "warn" \
            "Could not determine latest commit touching apps/status/" \
            "git log returned empty for apps/status/. Repo may not be checked out at the expected ref."
        return
    fi
    latest_status_iso=$(git show -s --format=%cI "$latest_status_sha")
    latest_status_date=$(git show -s --format=%ct "$latest_status_sha")

    if ! ssh_tunnel_available; then
        emit_finding "image-freshness" "warn" \
            "Pod-image freshness check skipped: SSH-cloudflared tunnel not configured" \
            "This check requires SSH_PRIVATE_KEY_FILE, SSH_TUNNEL_USER, and the cloudflared CLI to query kubectl. Without it, we cannot read the running pod's image SHA. The latest deployable commit on apps/status/ is ${latest_status_sha:0:12} (${latest_status_iso}). Provision SSH secrets to enable this check; see workflow file for setup."
        return
    fi

    # Query the running pod's image via kubectl.
    local pod_image
    pod_image=$(ssh_via_tunnel "kubectl get deployment status-madfam -n enclii -o jsonpath='{.spec.template.spec.containers[0].image}'" 2>/dev/null || true)
    if [[ -z "$pod_image" ]]; then
        emit_finding "image-freshness" "warn" \
            "Could not read status-madfam deployment image from cluster" \
            "ssh+kubectl returned empty. Cluster may be unreachable, deployment may have been renamed, or RBAC may have changed. Latest commit on apps/status/: ${latest_status_sha:0:12}"
        return
    fi

    # Image format is typically: ghcr.io/madfam-org/enclii-status@sha256:abc... OR
    # ghcr.io/madfam-org/enclii-status:<git-sha-12>. We extract the tag/digest
    # and try to map it back to a commit. The current build pipeline tags by
    # commit SHA, so if it's `:<sha>`, we have a direct lookup.
    local pod_image_tag
    pod_image_tag=$(echo "$pod_image" | sed -E 's|^[^:@]+(:|@)||; s|^sha256:||')

    # Try to resolve the tag as a git commit. Short SHAs from image tags are
    # typically 7-12 chars; full digest SHAs (64 hex chars) won't resolve.
    local pod_commit_sha pod_commit_date age_days
    if [[ "$pod_image_tag" =~ ^[a-f0-9]{7,40}$ ]]; then
        pod_commit_sha=$(git rev-parse --verify "$pod_image_tag^{commit}" 2>/dev/null || true)
    fi

    if [[ -z "${pod_commit_sha:-}" ]]; then
        # Could not resolve — image is likely tagged by digest, not commit.
        # In this case, we can still compare against image creation time on the
        # remote pod via `kubectl get pod ... -o jsonpath='{.status.startTime}'`.
        local pod_start_time
        pod_start_time=$(ssh_via_tunnel "kubectl get pod -n enclii -l app=status-madfam -o jsonpath='{.items[0].status.startTime}'" 2>/dev/null || true)
        if [[ -z "$pod_start_time" ]]; then
            emit_finding "image-freshness" "warn" \
                "Could not resolve pod image to a commit and could not read pod start time" \
                "Pod image: ${pod_image}. Tag '${pod_image_tag}' did not resolve as a git SHA. This is fine if images are digest-tagged, but in that case we need the pod start time as a proxy — and that read also failed."
            return
        fi
        # Compute age from pod start time (approximation — under-counts if pod restarted recently).
        local pod_start_epoch now_epoch
        pod_start_epoch=$(date -d "$pod_start_time" +%s 2>/dev/null || date -j -f "%Y-%m-%dT%H:%M:%SZ" "${pod_start_time%.*}Z" +%s 2>/dev/null || echo 0)
        now_epoch=$(date +%s)
        if [[ "$pod_start_epoch" -eq 0 ]]; then
            emit_finding "image-freshness" "warn" \
                "Could not parse pod start time" \
                "Raw value: ${pod_start_time}. Pod image: ${pod_image}."
            return
        fi
        age_days=$(( (now_epoch - pod_start_epoch) / 86400 ))
        if [[ $age_days -gt $IMAGE_STALENESS_DAYS ]]; then
            emit_finding "image-freshness" "alert" \
                "[Status-page health regression] Pod start time stale by ${age_days} days" \
                "Status page pod started at ${pod_start_time} (${age_days} days ago), exceeding the ${IMAGE_STALENESS_DAYS}-day threshold. Latest deployable commit on apps/status/: ${latest_status_sha:0:12} at ${latest_status_iso}. Pod image: ${pod_image}. Investigate why ArgoCD has not rolled the new image; check kustomization.yaml digest pin and ArgoCD sync status."
            return
        fi
        emit_finding "image-freshness" "info" \
            "Pod start time within threshold (${age_days}d <= ${IMAGE_STALENESS_DAYS}d)" \
            "Pod started ${pod_start_time}. Latest commit on apps/status/: ${latest_status_sha:0:12}."
        return
    fi

    pod_commit_date=$(git show -s --format=%ct "$pod_commit_sha" 2>/dev/null || echo 0)
    if [[ "$pod_commit_date" -eq 0 ]]; then
        emit_finding "image-freshness" "warn" \
            "Resolved pod commit but could not read its timestamp" \
            "Pod commit: ${pod_commit_sha:0:12}, image: ${pod_image}"
        return
    fi
    age_days=$(( (latest_status_date - pod_commit_date) / 86400 ))
    if [[ $age_days -gt $IMAGE_STALENESS_DAYS ]]; then
        emit_finding "image-freshness" "alert" \
            "[Status-page health regression] Pod image stale by ${age_days} days" \
            "Pod is running commit ${pod_commit_sha:0:12} ($(git show -s --format=%cI "$pod_commit_sha")). Latest deployable commit on apps/status/ is ${latest_status_sha:0:12} (${latest_status_iso}). The ${age_days}-day gap exceeds the ${IMAGE_STALENESS_DAYS}-day threshold. Investigate ArgoCD sync, build pipeline, and digest-pin updates."
        return
    fi
    emit_finding "image-freshness" "info" \
        "Pod image within freshness threshold (${age_days}d behind, threshold ${IMAGE_STALENESS_DAYS}d)" \
        "Pod commit: ${pod_commit_sha:0:12}, latest apps/status/ commit: ${latest_status_sha:0:12}"
}

# ─────────────────────────────────────────────────────────────────────────────
# Check #2 — Configmap regenerate freshness
#
# The platform configmaps (status-config-madfam, status-config-enclii) should
# have been regenerated within the last $CONFIGMAP_STALENESS_HOURS. If they
# haven't, two scenarios:
#   (a) Legitimate: no enclii.yaml status entries changed across the ecosystem
#       in the last 24h — nothing to regenerate.
#   (b) Bug: POST /v1/admin/status/regenerate is broken, OR the regenerate
#       cron isn't running, OR the configmap reload isn't propagating.
#
# We can't easily distinguish (a) vs (b) from outside, so this check fires a
# WARN-level finding (not alert) when the configmap is stale, with a hint to
# check whether any enclii.yaml status block was touched recently. The operator
# triages from the GH issue.
#
# The annotation we read is the kustomization-applied
# `enclii.dev/configmap-version` on the deployment template — that's the value
# that gets bumped (or should be bumped) every time the source configmap.yaml
# is regenerated. If it's older than the threshold, we surface it.
# ─────────────────────────────────────────────────────────────────────────────
check_configmap_freshness() {
    cd "$GIT_REPO_ROOT"

    if ! ssh_tunnel_available; then
        emit_finding "configmap-freshness" "warn" \
            "Configmap freshness check skipped: SSH-cloudflared tunnel not configured" \
            "Without ssh+kubectl, we cannot read the live configmap-version annotation. Provision SSH secrets to enable this check."
        return
    fi

    local annotation
    annotation=$(ssh_via_tunnel "kubectl get deployment status-madfam -n enclii -o jsonpath='{.spec.template.metadata.annotations.enclii\\.dev/configmap-version}'" 2>/dev/null || true)
    if [[ -z "$annotation" ]]; then
        emit_finding "configmap-freshness" "warn" \
            "Could not read configmap-version annotation from status-madfam deployment" \
            "ssh+kubectl returned empty. Deployment may have been renamed or annotation may have been removed."
        return
    fi

    # Annotation format is YYYY-MM-DD-vN. Extract the date portion.
    local annotation_date
    annotation_date=$(echo "$annotation" | grep -oE '^[0-9]{4}-[0-9]{2}-[0-9]{2}' || true)
    if [[ -z "$annotation_date" ]]; then
        emit_finding "configmap-freshness" "warn" \
            "Configmap-version annotation has unexpected format: ${annotation}" \
            "Expected YYYY-MM-DD-vN. Skipping freshness comparison."
        return
    fi

    local annotation_epoch now_epoch age_hours
    annotation_epoch=$(date -d "$annotation_date" +%s 2>/dev/null || date -j -f "%Y-%m-%d" "$annotation_date" +%s 2>/dev/null || echo 0)
    now_epoch=$(date +%s)
    age_hours=$(( (now_epoch - annotation_epoch) / 3600 ))

    if [[ $age_hours -gt $CONFIGMAP_STALENESS_HOURS ]]; then
        local age_days=$((age_hours / 24))
        emit_finding "configmap-freshness" "alert" \
            "[Status-page health regression] Configmap-version annotation stale by ${age_days} days" \
            "Annotation enclii.dev/configmap-version=${annotation} (${age_days} days old) on status-madfam deployment exceeds ${CONFIGMAP_STALENESS_HOURS}-hour threshold. Possible causes: (1) POST /v1/admin/status/regenerate is broken, (2) the kustomization annotation isn't being bumped on regeneration, (3) Stakater Reloader isn't restarting the pod on configmap change. Check Switchyard API logs for /v1/admin/status/regenerate calls; verify the configmap.yaml in apps/status/k8s/madfam/ has been updated recently. Runbook: docs/runbooks/STATUS_REGENERATE.md."
        return
    fi
    emit_finding "configmap-freshness" "info" \
        "Configmap-version annotation within threshold (${age_hours}h <= ${CONFIGMAP_STALENESS_HOURS}h)" \
        "Annotation: ${annotation}"
}

# ─────────────────────────────────────────────────────────────────────────────
# Check #3 — Assertion-effectiveness
#
# For every service in the deployed status page configmap that carries
# assertContains/assertNotContains, externally probe its probeUrl and re-evaluate
# the assertion. If the assertion FAILS externally but the status page reports
# the service as Operational, the assertion isn't being applied → file alert.
#
# Gracefully no-ops when:
#   - no service has assertions configured (Pillar 3 not landed yet)
#   - the configmap can't be read
#   - the external probe fails for unrelated reasons (network, etc.)
# ─────────────────────────────────────────────────────────────────────────────
check_assertion_effectiveness() {
    cd "$GIT_REPO_ROOT"

    local configmap_path="apps/status/k8s/madfam/configmap.yaml"
    if [[ ! -f "$configmap_path" ]]; then
        emit_finding "assertion-effectiveness" "warn" \
            "Configmap source file not found: ${configmap_path}" \
            "Cannot enumerate services with assertions."
        return
    fi

    # Extract services-config JSON from the configmap. The block is YAML literal
    # (`|`) and contains a JSON array under the `services-config` key.
    local services_json
    services_json=$(awk '
        /^  services-config: \|/ { capturing=1; next }
        capturing && /^  [a-zA-Z]/ && !/^    / { capturing=0 }
        capturing { sub(/^    /, ""); print }
    ' "$configmap_path") || true

    if [[ -z "$services_json" ]]; then
        emit_finding "assertion-effectiveness" "warn" \
            "Could not extract services-config block from configmap" \
            "File: ${configmap_path}. Schema may have changed."
        return
    fi

    # Filter to services that carry assertions.
    local services_with_assertions
    services_with_assertions=$(echo "$services_json" | jq -c '
        [ .[] | select(has("assertContains") or has("assertNotContains")) ]
    ' 2>/dev/null || echo "[]")

    local assertion_count
    assertion_count=$(echo "$services_with_assertions" | jq 'length' 2>/dev/null || echo 0)

    if [[ "$assertion_count" -eq 0 ]]; then
        emit_finding "assertion-effectiveness" "info" \
            "No services with content-match assertions configured (Pillar 3 not yet landed)" \
            "Check is a no-op until at least one service in apps/status/k8s/madfam/configmap.yaml has assertContains/assertNotContains. This is expected during the rollout window of the architectural fix."
        return
    fi

    # Fetch live status page report for cross-reference.
    local live_status_json
    live_status_json=$(curl -fsSL --max-time 30 "$STATUS_API_URL" 2>/dev/null || true)
    if [[ -z "$live_status_json" ]]; then
        emit_finding "assertion-effectiveness" "warn" \
            "Could not fetch live status page JSON from ${STATUS_API_URL}" \
            "Without the live report, we can't cross-reference external probe results against what the status page is publishing. Skipping ${assertion_count} assertion check(s)."
        return
    fi

    # For each service with an assertion, probe externally and compare.
    local failed=0
    local fail_details=""
    while IFS= read -r svc_obj; do
        [[ -z "$svc_obj" ]] && continue
        local name probe_url assert_contains assert_not_contains
        name=$(echo "$svc_obj" | jq -r '.name')
        probe_url=$(echo "$svc_obj" | jq -r '.probeUrl // .url')
        assert_contains=$(echo "$svc_obj" | jq -r '.assertContains // empty')
        assert_not_contains=$(echo "$svc_obj" | jq -r '.assertNotContains // empty')

        local body http_code
        body=$(curl -fsSL --max-time 30 "$probe_url" 2>/dev/null || true)
        http_code=$(curl -fsS --max-time 30 -o /dev/null -w '%{http_code}' "$probe_url" 2>/dev/null || echo "000")
        if [[ -z "$body" ]]; then
            # Probe itself failed — don't alert. The probe-failure path is
            # already covered by the regular status-page health check.
            continue
        fi

        local should_be_degraded=0
        local reason=""
        if [[ -n "$assert_contains" ]] && [[ "$body" != *"$assert_contains"* ]]; then
            should_be_degraded=1
            reason="assertContains '${assert_contains}' missing from body"
        fi
        if [[ -n "$assert_not_contains" ]] && [[ "$body" == *"$assert_not_contains"* ]]; then
            should_be_degraded=1
            reason="${reason}${reason:+; }assertNotContains '${assert_not_contains}' present in body"
        fi

        if [[ $should_be_degraded -eq 1 ]]; then
            # Look up what the status page is reporting for this service.
            local reported_status
            reported_status=$(echo "$live_status_json" | jq -r --arg name "$name" \
                '.services[]? | select(.service == $name) | .status' 2>/dev/null || echo "unknown")
            if [[ "$reported_status" == "operational" ]]; then
                failed=$((failed + 1))
                fail_details="${fail_details}\n- ${name}: ${reason}. Status page reports 'operational' — assertion not being applied. Probe URL: ${probe_url} (HTTP ${http_code})."
            fi
        fi
    done < <(echo "$services_with_assertions" | jq -c '.[]')

    if [[ $failed -gt 0 ]]; then
        emit_finding "assertion-effectiveness" "alert" \
            "[Status-page health regression] ${failed} service(s) failing externally but reported Operational" \
            "Assertions were configured for these services but the status page is not surfacing them as Degraded. This typically indicates the probe code is not reading the assertContains/assertNotContains fields, or the configmap was regenerated without the new schema. Failing services:${fail_details}"
        return
    fi
    emit_finding "assertion-effectiveness" "info" \
        "All ${assertion_count} assertion(s) effective" \
        "External re-evaluation matches status page report."
}

# ─────────────────────────────────────────────────────────────────────────────
# Driver
# ─────────────────────────────────────────────────────────────────────────────

# Ensure required tools are present.
for tool in jq curl git; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "ERROR: required tool '$tool' not found in PATH" >&2
        exit 2
    fi
done

echo "=== Status Page Health Regression Test ==="
echo "Repo:                ${GIT_REPO_ROOT}"
echo "Status page:         ${STATUS_PAGE_URL}"
echo "Image stale (days):  ${IMAGE_STALENESS_DAYS}"
echo "Configmap stale (h): ${CONFIGMAP_STALENESS_HOURS}"
echo "Findings file:       ${FINDINGS_FILE}"
echo "SSH tunnel:          $(ssh_tunnel_available && echo 'available' || echo 'NOT configured (Check #1, #2 will warn-only)')"
echo

check_image_freshness
check_configmap_freshness
check_assertion_effectiveness

echo
echo "=== Summary ==="
echo "Alerts: ${ALERT_COUNT}"
echo "Warns:  ${WARN_COUNT}"
echo "Info:   ${INFO_COUNT}"

if [[ $ALERT_COUNT -gt 0 ]]; then
    exit 1
fi
exit 0
