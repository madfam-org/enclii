#!/usr/bin/env bash
#
# join-worker-node.sh - Join a new worker node to the k3s cluster
#
# Idempotent script that configures a fresh Hetzner server as a k3s agent
# and joins it to the existing foundry-cp control plane.
#
# Prerequisites:
#   - Fresh Ubuntu 24.04 LTS server
#   - SSH access as root
#   - K3S_TOKEN set (from foundry-cp: sudo cat /var/lib/rancher/k3s/server/node-token)
#
# Usage:
#   # From your workstation:
#   ssh root@<new-server-ip>
#   curl -sfL https://raw.githubusercontent.com/madfam-org/enclii/main/scripts/join-worker-node.sh | \
#     K3S_TOKEN=<token> bash
#
#   # Or with explicit variables:
#   K3S_TOKEN=<token> K3S_URL=https://37.27.235.104:6443 HOSTNAME=foundry-node-02 bash join-worker-node.sh
#

set -euo pipefail

# ═══════════════════════════════════════════════════
# Configuration
# ═══════════════════════════════════════════════════

K3S_VERSION="${K3S_VERSION:-v1.33.7+k3s3}"
K3S_URL="${K3S_URL:-https://37.27.235.104:6443}"
HOSTNAME="${HOSTNAME:-foundry-node-02}"
NODE_LABEL="${NODE_LABEL:-node-role=worker}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log()  { echo -e "${GREEN}[JOIN]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()  { echo -e "${RED}[ERROR]${NC} $*" >&2; }

# ═══════════════════════════════════════════════════
# Validation
# ═══════════════════════════════════════════════════

if [[ -z "${K3S_TOKEN:-}" ]]; then
    err "K3S_TOKEN is required."
    echo ""
    echo "Retrieve it from the control plane:"
    echo "  ssh solarpunk@37.27.235.104 'sudo cat /var/lib/rancher/k3s/server/node-token'"
    echo ""
    echo "Then run:"
    echo "  K3S_TOKEN=<token> bash $(basename "$0")"
    exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
    err "This script must be run as root."
    exit 1
fi

# Idempotency: skip if k3s agent is already running and joined
if systemctl is-active --quiet k3s-agent 2>/dev/null; then
    warn "k3s-agent is already running on this node."
    echo ""
    kubectl get node "$(hostname)" 2>/dev/null && {
        log "Node is already joined to the cluster. Nothing to do."
        exit 0
    }
fi

# ═══════════════════════════════════════════════════
# Step 1: Set hostname
# ═══════════════════════════════════════════════════

log "Step 1/5: Setting hostname to ${HOSTNAME}"

current_hostname="$(hostname)"
if [[ "${current_hostname}" != "${HOSTNAME}" ]]; then
    hostnamectl set-hostname "${HOSTNAME}"
    # Update /etc/hosts so the hostname resolves
    if ! grep -q "${HOSTNAME}" /etc/hosts; then
        sed -i "s/127.0.1.1.*/127.0.1.1\t${HOSTNAME}/" /etc/hosts 2>/dev/null || \
            echo "127.0.1.1	${HOSTNAME}" >> /etc/hosts
    fi
    log "Hostname set to ${HOSTNAME}"
else
    log "Hostname already set to ${HOSTNAME}"
fi

# ═══════════════════════════════════════════════════
# Step 2: System update
# ═══════════════════════════════════════════════════

log "Step 2/5: Running system update"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get upgrade -y -qq
apt-get install -y -qq curl open-iscsi nfs-common

# open-iscsi and nfs-common required for Longhorn CSI
systemctl enable --now iscsid

log "System updated"

# ═══════════════════════════════════════════════════
# Step 3: Install k3s agent
# ═══════════════════════════════════════════════════

log "Step 3/5: Installing k3s agent ${K3S_VERSION}"

curl -sfL https://get.k3s.io | \
    INSTALL_K3S_VERSION="${K3S_VERSION}" \
    K3S_URL="${K3S_URL}" \
    K3S_TOKEN="${K3S_TOKEN}" \
    INSTALL_K3S_EXEC="agent" \
    sh -

log "k3s agent installed"

# ═══════════════════════════════════════════════════
# Step 4: Wait for node to be Ready
# ═══════════════════════════════════════════════════

log "Step 4/5: Waiting for node to become Ready (up to 120s)"

READY=false
for i in $(seq 1 24); do
    # The agent doesn't have kubectl — check the service is running
    if systemctl is-active --quiet k3s-agent; then
        READY=true
        break
    fi
    sleep 5
done

if [[ "${READY}" != "true" ]]; then
    err "k3s-agent failed to start within 120 seconds."
    echo "Check logs: journalctl -u k3s-agent -n 50"
    exit 1
fi

log "k3s-agent is running"

# ═══════════════════════════════════════════════════
# Step 5: Label node (run from control plane)
# ═══════════════════════════════════════════════════

log "Step 5/5: Node labeling"

echo ""
echo -e "${BLUE}════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}Node ${HOSTNAME} joined the cluster successfully!${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════${NC}"
echo ""
echo "Next steps (run from your workstation or foundry-cp):"
echo ""
echo "  # Verify node is Ready"
echo "  kubectl get nodes -o wide"
echo ""
echo "  # Label as worker (no builder taint — general workloads)"
echo "  kubectl label node ${HOSTNAME} ${NODE_LABEL}"
echo ""
echo "  # Longhorn auto-deploys via DaemonSet — watch volume replication"
echo "  kubectl get volumes.longhorn.io -n longhorn-system -w"
echo ""
echo "  # Restart large deployments to spread across nodes"
echo "  for ns in enclii janua tezca dhanam; do"
echo "    kubectl rollout restart deploy -n \$ns"
echo "  done"
echo ""
