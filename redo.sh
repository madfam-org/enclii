#!/bin/bash
set -e

# Phase 1
git rm -rf apps/switchyard-api/migrations/ 2>/dev/null || true
git rm -rf apps/switchyard-ui/components/log-viewer/ 2>/dev/null || true
git mv claudedocs docs/archive/ai-sessions-2026 2>/dev/null || true
git mv ops scripts/ops 2>/dev/null || true
git mv tools .serena 2>/dev/null || true
git mv docs/api/openapi.yaml docs/api-reference/openapi.yaml 2>/dev/null || true
git mv docs/audits/* docs/archive/audits/ 2>/dev/null || true
git mv docs/cross-repo/* docs/archive/cross-repo/ 2>/dev/null || true
git mv docs/quickstart/deploy-first-app.md docs/getting-started/deploy-first-app.md 2>/dev/null || true
git mv docs/reusable-workflows.md docs/guides/reusable-workflows.md 2>/dev/null || true
mkdir -p docs/operations
git mv docs/runbooks/* docs/operations/ 2>/dev/null || true
git rm -r docs/runbooks docs/quickstart.md docs/quickstart 2>/dev/null || true
git mv docs/templates.md docs/templates/templates.md 2>/dev/null || true

# Phase 3
python3 -c '
import os
try:
    with open("AI_CONTEXT.md", "r") as f:
        ai_ctx = f.read()
    with open("CLAUDE.md", "r") as f:
        claude_md = f.read()
    start_idx = ai_ctx.find("## Agent Directives")
    end_idx = ai_ctx.find("## Testing Commands", start_idx)
    directives = ai_ctx[start_idx:end_idx] if start_idx != -1 else ""
    start_idx2 = ai_ctx.find("## Secret Management Protocols")
    if start_idx2 != -1:
        end_idx2 = ai_ctx.find("##", start_idx2 + 2)
        if end_idx2 == -1: end_idx2 = len(ai_ctx)
        directives += "\n" + ai_ctx[start_idx2:end_idx2]
    unified = "# Enclii AI Context & Guidelines\n\nThis file provides authoritative guidance to all AI agents and LLMs working in this repository.\n\n" + directives + "\n" + claude_md.replace("# CLAUDE.md\n\nThis file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.\n\n", "")
    with open("AI_CONTEXT.md", "w") as f: f.write(unified)
    with open(".cursorrules", "w") as f:
        f.write("# Please refer to AI_CONTEXT.md for all project guidelines and agent directives.\n# The complete ruleset has been consolidated there to optimize context windows.\n")
    os.remove("CLAUDE.md")
except Exception as e:
    print("Phase 3 err:", e)
'
git rm CLAUDE.md 2>/dev/null || true
git add AI_CONTEXT.md .cursorrules

# Phase 2 - Rename
git mv apps/admin-console apps/admin-console
find . -type f -not -path "*/\.git/*" -not -path "*/node_modules/*" -not -path "*/\.next/*" -exec sed -i '' 's|apps/admin-console|apps/admin-console|g' {} +
find . -type f -not -path "*/\.git/*" -not -path "*/node_modules/*" -not -path "*/\.next/*" -exec sed -i '' 's|enclii/admin-console|enclii/admin-console|g' {} +
sed -i '' 's|name: dispatch|name: admin-console|g' infra/argocd/apps/admin-console.yaml 2>/dev/null || true
git mv infra/argocd/apps/admin-console.yaml infra/argocd/apps/admin-console.yaml 2>/dev/null || true
sed -i '' 's/"name": "dispatch"/"name": "admin-console"/g' apps/admin-console/package.json 2>/dev/null || true

# Phase 2 - Micro-packages
git mv apps/switchyard-api/internal/analytics/posthog.go apps/switchyard-api/internal/middleware/analytics_posthog.go
git mv apps/switchyard-api/internal/analytics/posthog_test.go apps/switchyard-api/internal/middleware/analytics_posthog_test.go
git mv apps/switchyard-api/internal/github/client.go apps/switchyard-api/internal/services/github_client.go
git mv apps/switchyard-api/internal/sbom/syft.go apps/switchyard-api/internal/provenance/sbom_syft.go
git mv apps/switchyard-api/internal/telemetry/telemetry.go apps/switchyard-api/internal/logging/telemetry.go

sed -i '' 's/package analytics/package middleware/g' apps/switchyard-api/internal/middleware/analytics_posthog.go apps/switchyard-api/internal/middleware/analytics_posthog_test.go
sed -i '' 's/package github/package services/g' apps/switchyard-api/internal/services/github_client.go
sed -i '' 's/package sbom/package provenance/g' apps/switchyard-api/internal/provenance/sbom_syft.go
sed -i '' 's/package telemetry/package logging/g' apps/switchyard-api/internal/logging/telemetry.go

find apps/switchyard-api -type f -name "*.go" -exec sed -i '' 's|"github.com/madfam-org/enclii/apps/switchyard-api/internal/telemetry"|"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"|g' {} +
find apps/switchyard-api -type f -name "*.go" -exec sed -i '' 's|"github.com/madfam-org/enclii/apps/switchyard-api/internal/sbom"|"github.com/madfam-org/enclii/apps/switchyard-api/internal/provenance"|g' {} +
find apps/switchyard-api -type f -name "*.go" -exec sed -i '' 's|"github.com/madfam-org/enclii/apps/switchyard-api/internal/github"|"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"|g' {} +
find apps/switchyard-api -type f -name "*.go" -exec sed -i '' 's|"github.com/madfam-org/enclii/apps/switchyard-api/internal/analytics"|"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"|g' {} +

# Fix specific imports
sed -i '' 's/sbom\./provenance\./g' apps/switchyard-api/internal/builder/service.go
sed -i '' 's/analytics\./middleware\./g' apps/switchyard-api/cmd/api/main.go 2>/dev/null || true
sed -i '' 's/telemetry\./logging\./g' apps/switchyard-api/internal/logging/telemetry.go 2>/dev/null || true
