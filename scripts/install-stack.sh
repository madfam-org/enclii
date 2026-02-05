#!/bin/bash
# =============================================================================
# install-stack.sh - Zero-Tinker Unified Installer
# =============================================================================
# Deploy Enclii (DevOps), Janua (Auth), or The Trinity (Both + Foundry)
# Target: Single Hetzner AX41-NVME node with k3s
#
# SECURITY NOTE: This script contains TEMPLATE PLACEHOLDERS for credentials
# (e.g., ${GITHUB_TOKEN}, $(POSTGRES_PASSWORD)). These are shell variable
# references, NOT actual secrets. Real values must be provided via environment
# variables at runtime.
#
# Usage:
#   ./install-stack.sh              # Interactive mode
#   ./install-stack.sh --enclii     # Deploy Enclii only
#   ./install-stack.sh --janua      # Deploy Janua only
#   ./install-stack.sh --trinity    # Deploy full stack (3-phase bootstrap)
#
# Environment Variables:
#   DOMAIN_BASE       - Base domain (default: example.com)
#   ADMIN_EMAIL       - Admin email for certs and bootstrap
#   CLOUDFLARE_TOKEN  - Cloudflare API token (optional, for DNS automation)
#   GITHUB_TOKEN      - GitHub token for pulling containers
# =============================================================================

set -euo pipefail

# =============================================================================
# Configuration
# =============================================================================
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_LOG="/var/log/install-stack.log"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Defaults
DOMAIN_BASE="${DOMAIN_BASE:-example.com}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@${DOMAIN_BASE}}"
K3S_VERSION="${K3S_VERSION:-v1.29.0+k3s1}"
DEPLOY_MODE=""

# =============================================================================
# Utility Functions
# =============================================================================
log() { echo -e "${GREEN}[$(date -u '+%H:%M:%S')]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
info() { echo -e "${CYAN}[INFO]${NC} $*"; }

check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "This script must be run as root (sudo ./install-stack.sh)"
        exit 1
    fi
}

check_requirements() {
    log "Checking system requirements..."

    local missing=()

    # Required commands
    for cmd in curl jq openssl; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done

    if [[ ${#missing[@]} -gt 0 ]]; then
        error "Missing required tools: ${missing[*]}"
        echo "Install with: apt-get install -y ${missing[*]}"
        exit 1
    fi

    # Check memory (minimum 16GB recommended)
    local mem_gb
    mem_gb=$(free -g | awk '/^Mem:/{print $2}')
    if [[ $mem_gb -lt 16 ]]; then
        warn "System has ${mem_gb}GB RAM. 16GB+ recommended for Trinity mode."
    fi

    # Check disk space (minimum 50GB)
    local disk_gb
    disk_gb=$(df -BG / | awk 'NR==2{print $4}' | tr -d 'G')
    if [[ $disk_gb -lt 50 ]]; then
        warn "Only ${disk_gb}GB disk space available. 50GB+ recommended."
    fi

    log "System requirements: ✓"
}

# =============================================================================
# Interactive Mode Selection
# =============================================================================
show_banner() {
    cat << 'EOF'

    ╔═══════════════════════════════════════════════════════════════════╗
    ║                                                                   ║
    ║     ███████╗███╗   ██╗ ██████╗██╗     ██╗██╗                      ║
    ║     ██╔════╝████╗  ██║██╔════╝██║     ██║██║                      ║
    ║     █████╗  ██╔██╗ ██║██║     ██║     ██║██║                      ║
    ║     ██╔══╝  ██║╚██╗██║██║     ██║     ██║██║                      ║
    ║     ███████╗██║ ╚████║╚██████╗███████╗██║██║                      ║
    ║     ╚══════╝╚═╝  ╚═══╝ ╚═════╝╚══════╝╚═╝╚═╝                      ║
    ║                                                                   ║
    ║          Zero-Tinker Unified Stack Installer v1.0                 ║
    ║                                                                   ║
    ╚═══════════════════════════════════════════════════════════════════╝

EOF
}

select_deployment_mode() {
    echo ""
    echo -e "${CYAN}What would you like to deploy?${NC}"
    echo ""
    echo "  1) Enclii Only      - Open Source DevOps Platform"
    echo "  2) Janua Only       - Auth Platform (Auth0 alternative)"
    echo "  3) The Trinity      - Both + Foundry (Complete self-hosted stack)"
    echo ""
    echo -e "${YELLOW}Note: Trinity mode uses 3-Phase Bootstrap to solve chicken-egg problem${NC}"
    echo ""

    while true; do
        read -rp "Enter choice [1-3]: " choice
        case $choice in
            1) DEPLOY_MODE="enclii"; break ;;
            2) DEPLOY_MODE="janua"; break ;;
            3) DEPLOY_MODE="trinity"; break ;;
            *) echo "Invalid choice. Please enter 1, 2, or 3." ;;
        esac
    done

    log "Selected deployment mode: ${DEPLOY_MODE}"
}

collect_configuration() {
    echo ""
    echo -e "${CYAN}Configuration${NC}"
    echo ""

    # Domain
    read -rp "Base domain [$DOMAIN_BASE]: " input_domain
    DOMAIN_BASE="${input_domain:-$DOMAIN_BASE}"

    # Admin email
    local default_email="admin@${DOMAIN_BASE}"
    read -rp "Admin email [$default_email]: " input_email
    ADMIN_EMAIL="${input_email:-$default_email}"

    # Cloudflare token (optional)
    if [[ -z "${CLOUDFLARE_TOKEN:-}" ]]; then
        read -rp "Cloudflare API Token (optional, press Enter to skip): " input_cf
        CLOUDFLARE_TOKEN="${input_cf:-}"
    fi

    # GitHub token for container registry
    if [[ -z "${GITHUB_TOKEN:-}" ]]; then
        read -rp "GitHub Token for container registry (required): " input_gh
        GITHUB_TOKEN="${input_gh:-}"
        if [[ -z "$GITHUB_TOKEN" ]]; then
            error "GitHub token is required to pull container images"
            exit 1
        fi
    fi

    echo ""
    log "Configuration collected"
}

# =============================================================================
# k3s Installation
# =============================================================================
install_k3s() {
    log "Installing k3s ${K3S_VERSION}..."

    if command -v k3s &>/dev/null; then
        info "k3s already installed, skipping..."
        return 0
    fi

    # Install k3s with disable traefik (we use Cloudflare Tunnel)
    curl -sfL https://get.k3s.io | INSTALL_K3S_VERSION="${K3S_VERSION}" sh -s - \
        --disable traefik \
        --disable servicelb \
        --write-kubeconfig-mode 644

    # Wait for k3s to be ready
    local retries=30
    while ! kubectl get nodes &>/dev/null && [[ $retries -gt 0 ]]; do
        sleep 2
        ((retries--))
    done

    if ! kubectl get nodes &>/dev/null; then
        error "k3s failed to start"
        exit 1
    fi

    # Setup kubeconfig for non-root users
    mkdir -p ~/.kube
    cp /etc/rancher/k3s/k3s.yaml ~/.kube/config
    chmod 600 ~/.kube/config

    log "k3s installed: ✓"
}

# =============================================================================
# Phase A: Local Auth Mode (No Janua Dependency)
# =============================================================================
phase_a_local_auth() {
    log "═══════════════════════════════════════════════════════════════"
    log "PHASE A: Local Auth Mode"
    log "Enclii starts with in-memory RSA keys, no Janua dependency"
    log "═══════════════════════════════════════════════════════════════"

    # Create namespace
    kubectl create namespace enclii --dry-run=client -o yaml | kubectl apply -f -

    # Create GitHub registry secret
    kubectl create secret docker-registry github-registry \
        --namespace enclii \
        --docker-server=ghcr.io \
        --docker-username=enclii \
        --docker-password="${GITHUB_TOKEN}" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Generate local RSA keys for Phase A
    log "Generating local RSA keys for Phase A auth..."
    openssl genrsa -out /tmp/enclii-local.key 2048
    openssl rsa -in /tmp/enclii-local.key -pubout -out /tmp/enclii-local.pub

    # Create secret with local keys
    kubectl create secret generic enclii-local-auth \
        --namespace enclii \
        --from-file=private.key=/tmp/enclii-local.key \
        --from-file=public.key=/tmp/enclii-local.pub \
        --dry-run=client -o yaml | kubectl apply -f -

    # Clean up temp keys
    rm -f /tmp/enclii-local.key /tmp/enclii-local.pub

    # Deploy Enclii with local auth mode
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: enclii-config
  namespace: enclii
data:
  ENCLII_AUTH_MODE: "local"
  ENCLII_DOMAIN: "api.${DOMAIN_BASE}"
  ENCLII_UI_DOMAIN: "app.${DOMAIN_BASE}"
  ENCLII_LOG_LEVEL: "info"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-api
  namespace: enclii
spec:
  replicas: 1
  selector:
    matchLabels:
      app: switchyard-api
  template:
    metadata:
      labels:
        app: switchyard-api
    spec:
      imagePullSecrets:
        - name: github-registry
      containers:
        - name: api
          image: ghcr.io/madfam-org/enclii-api:latest
          ports:
            - containerPort: 4200
          envFrom:
            - configMapRef:
                name: enclii-config
          env:
            - name: ENCLII_AUTH_MODE
              value: "local"
          volumeMounts:
            - name: local-auth
              mountPath: /etc/enclii/keys
              readOnly: true
      volumes:
        - name: local-auth
          secret:
            secretName: enclii-local-auth
---
apiVersion: v1
kind: Service
metadata:
  name: switchyard-api
  namespace: enclii
spec:
  selector:
    app: switchyard-api
  ports:
    - port: 80
      targetPort: 4200
EOF

    log "Phase A complete: Enclii running with local auth ✓"
    info "You can now register local users and deploy services"
}

# =============================================================================
# Phase B: External JWKS Mode (Deploy Janua via Enclii)
# =============================================================================
phase_b_jwks_validation() {
    log "═══════════════════════════════════════════════════════════════"
    log "PHASE B: External JWKS Validation Mode"
    log "CLI validates Janua tokens directly, Deploy Janua via Enclii"
    log "═══════════════════════════════════════════════════════════════"

    # Wait for Phase A to be ready
    kubectl wait --for=condition=available deployment/switchyard-api -n enclii --timeout=120s

    # Create Janua namespace
    kubectl create namespace janua --dry-run=client -o yaml | kubectl apply -f -

    # Create registry secret for Janua
    kubectl create secret docker-registry github-registry \
        --namespace janua \
        --docker-server=ghcr.io \
        --docker-username=enclii \
        --docker-password="${GITHUB_TOKEN}" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Generate Janua secrets
    log "Generating Janua secrets..."
    local jwt_secret
    local session_secret
    jwt_secret=$(openssl rand -base64 32)
    session_secret=$(openssl rand -base64 32)

    kubectl create secret generic janua-secrets \
        --namespace janua \
        --from-literal=JWT_SECRET="${jwt_secret}" \
        --from-literal=SESSION_SECRET="${session_secret}" \
        --from-literal=ADMIN_BOOTSTRAP_PASSWORD="$(openssl rand -base64 16)" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Deploy Janua
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: janua-config
  namespace: janua
data:
  BASE_URL: "https://auth.${DOMAIN_BASE}"
  ADMIN_BOOTSTRAP_EMAIL: "${ADMIN_EMAIL}"
  DEFAULT_ORG_SLUG: "${DOMAIN_BASE%%.*}"
  LOG_LEVEL: "info"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: janua-api
  namespace: janua
spec:
  replicas: 1
  selector:
    matchLabels:
      app: janua-api
  template:
    metadata:
      labels:
        app: janua-api
    spec:
      imagePullSecrets:
        - name: github-registry
      containers:
        - name: api
          image: ghcr.io/madfam-org/janua-api:latest
          ports:
            - containerPort: 8000
          envFrom:
            - configMapRef:
                name: janua-config
            - secretRef:
                name: janua-secrets
---
apiVersion: v1
kind: Service
metadata:
  name: janua-api
  namespace: janua
spec:
  selector:
    app: janua-api
  ports:
    - port: 80
      targetPort: 8000
EOF

    # Wait for Janua to be ready
    log "Waiting for Janua to start..."
    kubectl wait --for=condition=available deployment/janua-api -n janua --timeout=180s

    # Update Enclii to use external JWKS
    log "Configuring Enclii for external JWKS validation..."
    kubectl patch configmap enclii-config -n enclii --type merge -p '{
        "data": {
            "ENCLII_EXTERNAL_JWKS_URL": "http://janua-api.janua.svc.cluster.local/.well-known/jwks.json"
        }
    }'

    # Restart Enclii to pick up new config
    kubectl rollout restart deployment/switchyard-api -n enclii
    kubectl wait --for=condition=available deployment/switchyard-api -n enclii --timeout=120s

    log "Phase B complete: Janua deployed, Enclii validating JWKS ✓"
    info "CLI can now validate Janua tokens directly"
}

# =============================================================================
# Phase C: Full OIDC Mode (Complete Integration)
# =============================================================================
phase_c_full_oidc() {
    log "═══════════════════════════════════════════════════════════════"
    log "PHASE C: Full OIDC Mode"
    log "All users authenticate through Janua, complete integration"
    log "═══════════════════════════════════════════════════════════════"

    # Update Enclii to full OIDC mode
    log "Switching Enclii to full OIDC mode..."
    kubectl patch configmap enclii-config -n enclii --type merge -p "{
        \"data\": {
            \"ENCLII_AUTH_MODE\": \"oidc\",
            \"ENCLII_OIDC_ISSUER\": \"https://auth.${DOMAIN_BASE}\",
            \"ENCLII_OIDC_CLIENT_ID\": \"enclii-platform\"
        }
    }"

    # Register Enclii as OAuth client in Janua
    log "Registering Enclii as OAuth client in Janua..."
    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: janua-oauth-clients
  namespace: janua
data:
  enclii-platform.json: |
    {
      "client_id": "enclii-platform",
      "client_name": "Enclii Platform",
      "redirect_uris": [
        "https://app.${DOMAIN_BASE}/auth/callback",
        "http://127.0.0.1:8080/callback",
        "http://127.0.0.1:3000/callback"
      ],
      "grant_types": ["authorization_code", "refresh_token"],
      "response_types": ["code"],
      "token_endpoint_auth_method": "none",
      "pkce_required": true,
      "scopes": ["openid", "profile", "email", "offline_access"]
    }
EOF

    # Deploy Enclii UI
    log "Deploying Enclii UI..."
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-ui
  namespace: enclii
spec:
  replicas: 1
  selector:
    matchLabels:
      app: switchyard-ui
  template:
    metadata:
      labels:
        app: switchyard-ui
    spec:
      imagePullSecrets:
        - name: github-registry
      containers:
        - name: ui
          image: ghcr.io/madfam-org/enclii-ui:latest
          ports:
            - containerPort: 4201
          env:
            - name: NEXT_PUBLIC_API_URL
              value: "https://api.${DOMAIN_BASE}"
            - name: NEXT_PUBLIC_JANUA_URL
              value: "https://auth.${DOMAIN_BASE}"
---
apiVersion: v1
kind: Service
metadata:
  name: switchyard-ui
  namespace: enclii
spec:
  selector:
    app: switchyard-ui
  ports:
    - port: 80
      targetPort: 4201
EOF

    # Final restart to apply OIDC mode
    kubectl rollout restart deployment/switchyard-api -n enclii
    kubectl wait --for=condition=available deployment/switchyard-api -n enclii --timeout=120s
    kubectl wait --for=condition=available deployment/switchyard-ui -n enclii --timeout=120s

    log "Phase C complete: Full OIDC integration active ✓"
}

# =============================================================================
# Deploy Foundry Infrastructure
# =============================================================================
deploy_foundry() {
    log "═══════════════════════════════════════════════════════════════"
    log "Deploying Foundry Infrastructure"
    log "PostgreSQL, Redis, Prometheus, Longhorn, ArgoCD"
    log "═══════════════════════════════════════════════════════════════"

    # Create foundry namespace
    kubectl create namespace foundry --dry-run=client -o yaml | kubectl apply -f -

    # Deploy PostgreSQL
    log "Deploying PostgreSQL..."
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgresql
  namespace: foundry
spec:
  serviceName: postgresql
  replicas: 1
  selector:
    matchLabels:
      app: postgresql
  template:
    metadata:
      labels:
        app: postgresql
    spec:
      containers:
        - name: postgresql
          image: postgres:15-alpine
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_USER
              value: enclii
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: foundry-secrets
                  key: POSTGRES_PASSWORD
            - name: POSTGRES_DB
              value: enclii
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 20Gi
---
apiVersion: v1
kind: Service
metadata:
  name: postgresql
  namespace: foundry
spec:
  selector:
    app: postgresql
  ports:
    - port: 5432
EOF

    # Deploy Redis
    log "Deploying Redis..."
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: foundry
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
        - name: redis
          image: redis:7-alpine
          ports:
            - containerPort: 6379
          command: ["redis-server", "--appendonly", "yes"]
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: foundry
spec:
  selector:
    app: redis
  ports:
    - port: 6379
EOF

    # Generate foundry secrets
    log "Generating foundry secrets..."
    kubectl create secret generic foundry-secrets \
        --namespace foundry \
        --from-literal=POSTGRES_PASSWORD="$(openssl rand -base64 24)" \
        --dry-run=client -o yaml | kubectl apply -f -

    log "Foundry infrastructure deployed ✓"
}

# =============================================================================
# Setup Cloudflare Tunnel (Ingress)
# =============================================================================
setup_cloudflare_tunnel() {
    if [[ -z "${CLOUDFLARE_TOKEN:-}" ]]; then
        warn "Cloudflare token not provided. Skipping tunnel setup."
        info "You'll need to manually configure ingress for your domains."
        return 0
    fi

    log "Setting up Cloudflare Tunnel..."

    # Create cloudflare namespace
    kubectl create namespace cloudflare --dry-run=client -o yaml | kubectl apply -f -

    # Create tunnel credentials secret
    kubectl create secret generic cloudflare-credentials \
        --namespace cloudflare \
        --from-literal=TUNNEL_TOKEN="${CLOUDFLARE_TOKEN}" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Deploy cloudflared
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudflared
  namespace: cloudflare
spec:
  replicas: 2
  selector:
    matchLabels:
      app: cloudflared
  template:
    metadata:
      labels:
        app: cloudflared
    spec:
      containers:
        - name: cloudflared
          image: cloudflare/cloudflared:latest
          args:
            - tunnel
            - --no-autoupdate
            - run
          env:
            - name: TUNNEL_TOKEN
              valueFrom:
                secretKeyRef:
                  name: cloudflare-credentials
                  key: TUNNEL_TOKEN
EOF

    log "Cloudflare Tunnel deployed ✓"
}

# =============================================================================
# Individual Stack Deployments
# =============================================================================
deploy_enclii_only() {
    log "Deploying Enclii (DevOps Platform) standalone..."

    install_k3s
    deploy_foundry

    # Deploy Enclii without Janua integration
    kubectl create namespace enclii --dry-run=client -o yaml | kubectl apply -f -

    kubectl create secret docker-registry github-registry \
        --namespace enclii \
        --docker-server=ghcr.io \
        --docker-username=enclii \
        --docker-password="${GITHUB_TOKEN}" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Generate local auth keys
    openssl genrsa -out /tmp/enclii-local.key 2048
    openssl rsa -in /tmp/enclii-local.key -pubout -out /tmp/enclii-local.pub

    kubectl create secret generic enclii-local-auth \
        --namespace enclii \
        --from-file=private.key=/tmp/enclii-local.key \
        --from-file=public.key=/tmp/enclii-local.pub \
        --dry-run=client -o yaml | kubectl apply -f -

    rm -f /tmp/enclii-local.key /tmp/enclii-local.pub

    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: enclii-config
  namespace: enclii
data:
  ENCLII_AUTH_MODE: "local"
  ENCLII_DOMAIN: "api.${DOMAIN_BASE}"
  ENCLII_UI_DOMAIN: "app.${DOMAIN_BASE}"
  ENCLII_DB_URL: "postgres://enclii:\$(POSTGRES_PASSWORD)@postgresql.foundry.svc.cluster.local:5432/enclii?sslmode=disable"
  ENCLII_REDIS_URL: "redis://redis.foundry.svc.cluster.local:6379"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-api
  namespace: enclii
spec:
  replicas: 2
  selector:
    matchLabels:
      app: switchyard-api
  template:
    metadata:
      labels:
        app: switchyard-api
    spec:
      imagePullSecrets:
        - name: github-registry
      containers:
        - name: api
          image: ghcr.io/madfam-org/enclii-api:latest
          ports:
            - containerPort: 4200
          envFrom:
            - configMapRef:
                name: enclii-config
          volumeMounts:
            - name: local-auth
              mountPath: /etc/enclii/keys
              readOnly: true
      volumes:
        - name: local-auth
          secret:
            secretName: enclii-local-auth
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: switchyard-ui
  namespace: enclii
spec:
  replicas: 1
  selector:
    matchLabels:
      app: switchyard-ui
  template:
    metadata:
      labels:
        app: switchyard-ui
    spec:
      imagePullSecrets:
        - name: github-registry
      containers:
        - name: ui
          image: ghcr.io/madfam-org/enclii-ui:latest
          ports:
            - containerPort: 4201
          env:
            - name: NEXT_PUBLIC_API_URL
              value: "https://api.${DOMAIN_BASE}"
---
apiVersion: v1
kind: Service
metadata:
  name: switchyard-api
  namespace: enclii
spec:
  selector:
    app: switchyard-api
  ports:
    - port: 80
      targetPort: 4200
---
apiVersion: v1
kind: Service
metadata:
  name: switchyard-ui
  namespace: enclii
spec:
  selector:
    app: switchyard-ui
  ports:
    - port: 80
      targetPort: 4201
EOF

    setup_cloudflare_tunnel

    log "Enclii standalone deployment complete ✓"
}

deploy_janua_only() {
    log "Deploying Janua (Auth Platform) standalone..."

    install_k3s
    deploy_foundry

    kubectl create namespace janua --dry-run=client -o yaml | kubectl apply -f -

    kubectl create secret docker-registry github-registry \
        --namespace janua \
        --docker-server=ghcr.io \
        --docker-username=enclii \
        --docker-password="${GITHUB_TOKEN}" \
        --dry-run=client -o yaml | kubectl apply -f -

    # Generate secrets
    local jwt_secret session_secret admin_pass
    jwt_secret=$(openssl rand -base64 32)
    session_secret=$(openssl rand -base64 32)
    admin_pass=$(openssl rand -base64 16)

    kubectl create secret generic janua-secrets \
        --namespace janua \
        --from-literal=JWT_SECRET="${jwt_secret}" \
        --from-literal=SESSION_SECRET="${session_secret}" \
        --from-literal=ADMIN_BOOTSTRAP_PASSWORD="${admin_pass}" \
        --dry-run=client -o yaml | kubectl apply -f -

    cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: janua-config
  namespace: janua
data:
  BASE_URL: "https://auth.${DOMAIN_BASE}"
  DATABASE_URL: "postgres://enclii:\$(POSTGRES_PASSWORD)@postgresql.foundry.svc.cluster.local:5432/janua?sslmode=disable"
  REDIS_URL: "redis://redis.foundry.svc.cluster.local:6379"
  ADMIN_BOOTSTRAP_EMAIL: "${ADMIN_EMAIL}"
  DEFAULT_ORG_SLUG: "${DOMAIN_BASE%%.*}"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: janua-api
  namespace: janua
spec:
  replicas: 2
  selector:
    matchLabels:
      app: janua-api
  template:
    metadata:
      labels:
        app: janua-api
    spec:
      imagePullSecrets:
        - name: github-registry
      containers:
        - name: api
          image: ghcr.io/madfam-org/janua-api:latest
          ports:
            - containerPort: 8000
          envFrom:
            - configMapRef:
                name: janua-config
            - secretRef:
                name: janua-secrets
---
apiVersion: v1
kind: Service
metadata:
  name: janua-api
  namespace: janua
spec:
  selector:
    app: janua-api
  ports:
    - port: 80
      targetPort: 8000
EOF

    setup_cloudflare_tunnel

    log "Janua standalone deployment complete ✓"
    info "Admin credentials: ${ADMIN_EMAIL} / ${admin_pass}"
}

deploy_trinity() {
    log "Deploying The Trinity (Enclii + Janua + Foundry)..."
    log "Using 3-Phase Bootstrap Strategy"

    install_k3s
    deploy_foundry

    # Execute 3-phase bootstrap
    phase_a_local_auth
    sleep 10  # Allow Phase A to stabilize

    phase_b_jwks_validation
    sleep 10  # Allow Phase B to stabilize

    phase_c_full_oidc

    setup_cloudflare_tunnel

    log ""
    log "═══════════════════════════════════════════════════════════════"
    log "THE TRINITY DEPLOYMENT COMPLETE"
    log "═══════════════════════════════════════════════════════════════"
    log ""
    log "Services:"
    log "  • Enclii API:     https://api.${DOMAIN_BASE}"
    log "  • Enclii UI:      https://app.${DOMAIN_BASE}"
    log "  • Janua Auth:     https://auth.${DOMAIN_BASE}"
    log ""
    log "Bootstrap completed through all 3 phases:"
    log "  ✓ Phase A: Local Auth (in-memory RSA)"
    log "  ✓ Phase B: JWKS Validation (Janua deployed)"
    log "  ✓ Phase C: Full OIDC (complete integration)"
    log ""
}

# =============================================================================
# Post-Installation Summary
# =============================================================================
print_summary() {
    echo ""
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}                    INSTALLATION COMPLETE                       ${NC}"
    echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo -e "Deployment Mode: ${CYAN}${DEPLOY_MODE}${NC}"
    echo -e "Domain Base:     ${CYAN}${DOMAIN_BASE}${NC}"
    echo -e "Admin Email:     ${CYAN}${ADMIN_EMAIL}${NC}"
    echo ""

    case $DEPLOY_MODE in
        enclii)
            echo "Endpoints:"
            echo "  • API:      https://api.${DOMAIN_BASE}"
            echo "  • UI:       https://app.${DOMAIN_BASE}"
            ;;
        janua)
            echo "Endpoints:"
            echo "  • Auth:     https://auth.${DOMAIN_BASE}"
            echo "  • JWKS:     https://auth.${DOMAIN_BASE}/.well-known/jwks.json"
            ;;
        trinity)
            echo "Endpoints:"
            echo "  • Enclii API:   https://api.${DOMAIN_BASE}"
            echo "  • Enclii UI:    https://app.${DOMAIN_BASE}"
            echo "  • Janua Auth:   https://auth.${DOMAIN_BASE}"
            ;;
    esac

    echo ""
    echo "Useful Commands:"
    echo "  kubectl get pods -A           # View all pods"
    echo "  kubectl logs -n enclii -f     # View Enclii logs"
    echo "  kubectl logs -n janua -f      # View Janua logs"
    echo ""

    if [[ -z "${CLOUDFLARE_TOKEN:-}" ]]; then
        echo -e "${YELLOW}Note: Cloudflare tunnel not configured.${NC}"
        echo "Configure DNS manually or run with CLOUDFLARE_TOKEN set."
    fi

    echo ""
    echo -e "${GREEN}Installation log: ${INSTALL_LOG}${NC}"
    echo ""
}

# =============================================================================
# Main Entry Point
# =============================================================================
main() {
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --enclii)  DEPLOY_MODE="enclii"; shift ;;
            --janua)   DEPLOY_MODE="janua"; shift ;;
            --trinity) DEPLOY_MODE="trinity"; shift ;;
            --help|-h)
                echo "Usage: $0 [--enclii|--janua|--trinity]"
                echo "  --enclii   Deploy Enclii DevOps platform only"
                echo "  --janua    Deploy Janua Auth platform only"
                echo "  --trinity  Deploy both with 3-phase bootstrap"
                exit 0
                ;;
            *) error "Unknown option: $1"; exit 1 ;;
        esac
    done

    # Start logging
    exec > >(tee -a "$INSTALL_LOG") 2>&1

    show_banner
    check_root
    check_requirements

    # Interactive mode if no flag provided
    if [[ -z "$DEPLOY_MODE" ]]; then
        select_deployment_mode
    fi

    collect_configuration

    # Execute deployment
    case $DEPLOY_MODE in
        enclii)  deploy_enclii_only ;;
        janua)   deploy_janua_only ;;
        trinity) deploy_trinity ;;
    esac

    print_summary
}

# Run main
main "$@"
