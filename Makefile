.PHONY: bootstrap install-hooks build-all build-api build-cli build-ui build-roundhouse install-cli
.PHONY: test test-integration test-coverage test-benchmark test-all check-drift lint
.PHONY: run-switchyard run-ui run-roundhouse-worker run-all
.PHONY: kind-up kind-down infra-dev dns-dev deploy-staging deploy-prod health-check commercial-ga-proof wave0-ga-ops clean
.PHONY: precommit e2e

# Variables
REGISTRY ?= ghcr.io/madfam
VERSION ?= $(shell git describe --always --dirty)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
KIND_CLUSTER_NAME ?= enclii

# CLI build flags: strip symbols (-s -w) and inject version metadata.
CLI_LDFLAGS = -s -w \
	-X github.com/madfam-org/enclii/packages/cli/internal/cmd.Version=$(VERSION) \
	-X github.com/madfam-org/enclii/packages/cli/internal/cmd.Commit=$(COMMIT) \
	-X github.com/madfam-org/enclii/packages/cli/internal/cmd.BuildDate=$(BUILD_DATE)
CLI_INSTALL_DIR ?= /usr/local/bin

# Bootstrap development environment
bootstrap:
	@echo "🚂 Bootstrapping Enclii development environment..."
	go mod download
	cd apps/switchyard-ui && pnpm install
	@echo "🔐 Installing git hooks..."
	@cp scripts/hooks/pre-commit .git/hooks/pre-commit 2>/dev/null || true
	@chmod +x .git/hooks/pre-commit 2>/dev/null || true
	@cp scripts/hooks/pre-push .git/hooks/pre-push 2>/dev/null || true
	@chmod +x .git/hooks/pre-push 2>/dev/null || true
	@echo "✅ Bootstrap complete"

# Install git hooks (pre-commit and pre-push)
install-hooks:
	@echo "🔐 Installing git hooks..."
	@if command -v pre-commit &> /dev/null; then \
		pre-commit install; \
	else \
		echo "⚠️  pre-commit not found, using built-in git hook"; \
		cp scripts/hooks/pre-commit .git/hooks/pre-commit; \
		chmod +x .git/hooks/pre-commit; \
	fi
	@echo "🔐 Installing pre-push hook (production health gate)..."
	@cp scripts/hooks/pre-push .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "✅ All hooks installed"

# Build all components
build-all: build-api build-cli build-ui build-roundhouse

build-api:
	@echo "🏗️ Building Switchyard API..."
	cd apps/switchyard-api && go build -o ../../bin/switchyard-api ./cmd/api

build-cli:
	@echo "🏗️ Building CLI ($(VERSION) / $(COMMIT))..."
	cd packages/cli && CGO_ENABLED=0 go build -trimpath -ldflags "$(CLI_LDFLAGS)" -o ../../bin/enclii ./cmd/enclii
	@echo "✅ Built bin/enclii ($$(du -h bin/enclii | cut -f1))"

# Install the CLI to CLI_INSTALL_DIR (default /usr/local/bin) so agents and
# shells in this workspace can invoke `enclii` directly. Override the
# destination with `make install-cli CLI_INSTALL_DIR=$$HOME/.local/bin`.
install-cli: build-cli
	@echo "📦 Installing enclii to $(CLI_INSTALL_DIR)/enclii..."
	@install -m 0755 bin/enclii $(CLI_INSTALL_DIR)/enclii
	@echo "✅ enclii $$($(CLI_INSTALL_DIR)/enclii version --json 2>/dev/null || echo installed)"

build-ui:
	@echo "🏗️ Building UI..."
	cd apps/switchyard-ui && pnpm run build

build-roundhouse:
	@echo "🏗️ Building Roundhouse..."
	cd apps/roundhouse && go build -o ../../bin/roundhouse-api ./cmd/api
	cd apps/roundhouse && go build -o ../../bin/roundhouse-worker ./cmd/worker

# Testing
test:
	@echo "🧪 Running unit tests..."
	cd apps/switchyard-api && go test -v -race -cover ./...
	cd packages/cli && go test -v -race -cover ./...
	cd apps/switchyard-ui && pnpm test

test-integration:
	@echo "🧪 Running integration tests..."
	cd apps/switchyard-api && go test -v -tags=integration ./...

test-coverage:
	@echo "📊 Generating test coverage report..."
	cd apps/switchyard-api && go test -coverprofile=coverage.out ./...
	cd apps/switchyard-api && go tool cover -html=coverage.out -o coverage.html
	cd packages/cli && go test -coverprofile=cli-coverage.out ./...
	cd packages/cli && go tool cover -html=cli-coverage.out -o cli-coverage.html
	@echo "Coverage reports generated"

test-benchmark:
	@echo "⚡ Running benchmark tests..."
	cd apps/switchyard-api && go test -bench=. -benchmem ./...
	cd packages/cli && go test -bench=. -benchmem ./...

test-all: test test-integration test-coverage
	@echo "✅ All tests completed successfully"

# Tunnel-route drift check (JSON ↔ Terraform)
# Compares expected-tunnel-config.json against cloudflare.tf.
# NOTE: This only catches drift at the Terraform layer. The 31 ecosystem
# routes added at runtime by switchyard-api's domain provisioner are
# intentionally NOT in Terraform — see docs/infrastructure/INFRA_ANATOMY.md.
# To compare against live Cloudflare state, use:
#   cloudflared tunnel route ingress list enclii-production
check-drift:
	@./tests/golden/infra/tunnel-routes-test.sh

# Linting
lint:
	@echo "🔍 Linting code..."
	golangci-lint run ./...
	cd apps/switchyard-ui && pnpm run lint

# Run services locally
run-switchyard: build-api
	@echo "🚂 Starting Switchyard API on :8080..."
	./bin/switchyard-api

run-ui: build-ui
	@echo "🌐 Starting UI on :3000..."
	cd apps/switchyard-ui && pnpm run dev

run-roundhouse-worker: build-roundhouse
	@echo "🔄 Starting Roundhouse worker..."
	./bin/roundhouse-worker

# Kind cluster management
kind-up:
	@echo "🏗️ Creating kind cluster $(KIND_CLUSTER_NAME)..."
	kind create cluster --name $(KIND_CLUSTER_NAME) --config infra/dev/kind-config.yaml
	kubectl config use-context kind-$(KIND_CLUSTER_NAME)

kind-down:
	@echo "🗑️ Deleting kind cluster $(KIND_CLUSTER_NAME)..."
	kind delete cluster --name $(KIND_CLUSTER_NAME)

# Install development infrastructure
infra-dev:
	@echo "🏗️ Installing development infrastructure..."
	kubectl apply -f infra/dev/namespace.yaml
	kubectl apply -k infra/k8s/base
	@echo "⏳ Waiting for services to be ready..."
	kubectl wait --for=condition=ready pod -l app=postgres --timeout=300s
	kubectl wait --for=condition=ready pod -l app=redis --timeout=300s
	kubectl wait --for=condition=ready pod -l app=switchyard-api --timeout=300s

# Deploy to staging
deploy-staging:
	@echo "🚀 Deploying to staging environment..."
	kubectl create namespace enclii-staging --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -k infra/k8s/staging
	kubectl rollout status deployment/switchyard-api -n enclii-staging --timeout=300s

# Deploy to production  
deploy-prod:
	@echo "🚀 Deploying to production environment..."
	@echo "⚠️  Production deployment requires manual confirmation"
	@read -p "Deploy to production? (yes/no): " confirm && [ "$$confirm" = "yes" ]
	kubectl create namespace enclii --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -k infra/k8s/production
	kubectl rollout status deployment/switchyard-api -n enclii --timeout=600s

# Health check all environments
health-check:
	@echo "🏥 Checking health of all environments..."
	@echo "Development:"
	kubectl get pods -l app=switchyard-api || true
	@echo "Staging:"  
	kubectl get pods -l app=switchyard-api -n enclii-staging || true
	@echo "Production:"
	kubectl get pods -l app=switchyard-api -n enclii || true

commercial-ga-proof:
	@bash scripts/commercial-ga-proof.sh

wave0-ga-ops:
	@bash scripts/wave0-ga-ops.sh

# Run all services locally (API + UI)
run-all: build-api build-ui
	@echo "🚂 Starting all services..."
	@echo "Starting Switchyard API on :8080..."
	@./bin/switchyard-api &
	@echo "Starting UI on :3000..."
	@cd apps/switchyard-ui && pnpm run dev

# Configure development DNS entries (requires /etc/hosts or local DNS)
dns-dev:
	@echo "🌐 Configuring development DNS..."
	@echo "Add the following to /etc/hosts (or use dnsmasq):"
	@echo "127.0.0.1 api.enclii.local"
	@echo "127.0.0.1 app.enclii.local"
	@echo ""
	@echo "Or use nip.io for automatic wildcard DNS:"
	@echo "  API: http://api.127.0.0.1.nip.io:8080"
	@echo "  UI:  http://app.127.0.0.1.nip.io:3000"

# Pre-commit checks (lint + test + build)
precommit: lint test build-all
	@echo "✅ Pre-commit checks passed"

# End-to-end tests
e2e:
	@echo "🧪 Running E2E tests..."
	cd apps/switchyard-ui && pnpm run test:e2e

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -rf bin/
	rm -rf apps/switchyard-ui/dist
	rm -rf apps/switchyard-ui/.next
