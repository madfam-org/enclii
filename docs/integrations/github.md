---
title: GitHub Integration
description: Connect GitHub repositories for automatic builds, deployments, and preview environments
sidebar_position: 1
tags: [integrations, github, webhooks, ci-cd, preview-environments]
---

# GitHub Integration

Connect your GitHub repositories to Enclii for automatic builds, deployments, and preview environments.

## Overview

The GitHub integration enables:
- **Automatic Builds**: Build on every push to configured branches
- **Preview Environments**: Automatic deployments for pull requests
- **Status Checks**: Build/deploy status on commits and PRs
- **Auto-Deploy**: Deploy to staging/production on merge

---

## Setup

### 1. Install GitHub App

1. Go to the [Enclii Dashboard](https://app.enclii.dev)
2. Navigate to **Settings → Integrations → GitHub**
3. Click **Install GitHub App**
4. Select the repositories to grant access

### 2. Link Repository to Service

```bash
# Via CLI
enclii services link --repo https://github.com/org/repo

# Or update enclii.yaml
```

In your `enclii.yaml`:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
  project: my-project
spec:
  # ... other config
  github:
    repo: org/repo
    branch: main
```

---

## Webhook Configuration

If using self-hosted GitHub Enterprise or need manual webhook setup:

### 1. Create Webhook

1. Go to your repository **Settings → Webhooks**
2. Click **Add webhook**
3. Configure:
   - **Payload URL**: `https://api.enclii.dev/webhooks/github`
   - **Content type**: `application/json`
   - **Secret**: Generate via `enclii webhooks create`
   - **Events**: Select `Push`, `Pull request`, `Check run`

### 2. Verify Webhook

```bash
enclii webhooks verify --repo org/repo
```

---

## Automatic Builds

Every push triggers a build based on your configuration.

### Build Configuration

```yaml
spec:
  build:
    type: auto          # auto-detect build method
    # Or specify:
    # type: dockerfile
    # dockerfile: ./Dockerfile

  github:
    repo: org/repo
    branch: main        # Build on pushes to main
    buildOnPush: true   # Enable automatic builds
```

### Build Status

Enclii posts build status as GitHub commit status checks:

- **pending**: Build queued or in progress
- **success**: Build completed successfully
- **failure**: Build failed
- **error**: Infrastructure error

View build logs:
```bash
enclii builds logs --latest
```

---

## Preview Environments

Automatically deploy pull requests to isolated environments.

### Enable Preview Environments

```yaml
spec:
  github:
    repo: org/repo
    previewEnvironments:
      enabled: true
      pattern: "pr-{number}"    # URL pattern
      autoSleep: 30             # Sleep after 30min of inactivity
      baseDomain: preview.enclii.app
```

### Preview URL

When you open a PR, Enclii:
1. Builds your branch
2. Deploys to `pr-{number}.preview.enclii.app`
3. Comments on the PR with the preview URL
4. Updates status on new commits

**Example PR Comment:**
```
🚀 Preview Environment Ready!

URL: https://pr-123.preview.enclii.app
Status: healthy
Deployed: a1b2c3d (feat: add user endpoint)

[View Logs](https://app.enclii.dev/...) | [Dashboard](https://app.enclii.dev/...)
```

### Preview Environment Lifecycle

| PR Event | Enclii Action |
|----------|---------------|
| Opened | Create environment, build, deploy |
| Commit pushed | Rebuild and redeploy |
| Closed (not merged) | Delete environment |
| Merged | Delete environment (deploy to target via auto-deploy) |
| Reopened | Recreate environment |

### Auto-Sleep

Preview environments automatically sleep after inactivity to save resources:

```yaml
spec:
  github:
    previewEnvironments:
      autoSleep: 30  # Minutes, 0 = never sleep
```

When a sleeping preview receives traffic:
1. First request may take 10-30 seconds (cold start)
2. Environment stays awake for configured duration
3. Returns to sleep after inactivity

---

## Auto-Deploy

Automatically deploy when code is merged.

### Configuration

```yaml
spec:
  autoDeploy:
    enabled: true
    branch: main
    environment: staging

  # Optional: production requires manual approval
  approvals:
    production:
      required: true
      approvers:
        - team/devops
```

### Deployment Flow

1. **PR Merged to `main`**
2. **Build Triggered** → Creates new release
3. **Deploy to Staging** → Automatic (if configured)
4. **Deploy to Production** → Manual approval (if required)

### Branch Strategies

**Trunk-Based Development:**
```yaml
spec:
  autoDeploy:
    enabled: true
    branch: main
    environment: staging
```

**GitFlow:**
```yaml
spec:
  autoDeploy:
    enabled: true
    branches:
      - branch: develop
        environment: development
      - branch: release/*
        environment: staging
      - branch: main
        environment: production
```

---

## GitHub Actions Integration

Use Enclii in your GitHub Actions workflows.

### Setup

1. Create an API token:
   ```bash
   enclii tokens create --name "github-actions" --scopes "deploy"
   ```

2. Add to repository secrets:
   - Go to **Settings → Secrets → Actions**
   - Add `ENCLII_TOKEN` with your token

### Workflow Example

```yaml
# .github/workflows/deploy.yml
name: Deploy to Enclii

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install Enclii CLI
        run: |
          git clone --depth 1 https://github.com/madfam-org/enclii.git /tmp/enclii
          make -C /tmp/enclii install-cli CLI_INSTALL_DIR=/usr/local/bin

      - name: Deploy to Staging
        env:
          ENCLII_TOKEN: ${{ secrets.ENCLII_TOKEN }}
        run: |
          enclii deploy --env staging --wait

      - name: Run E2E Tests
        run: npm run test:e2e

      - name: Deploy to Production
        if: success()
        env:
          ENCLII_TOKEN: ${{ secrets.ENCLII_TOKEN }}
        run: |
          enclii deploy --env production --strategy canary --canary-percent 10
```

### Deployment Status Action

```yaml
      - name: Comment Deployment URL
        uses: madfam-org/enclii-action@v1
        with:
          token: ${{ secrets.ENCLII_TOKEN }}
          service: api
          environment: staging
          comment-on-pr: true
```

---

## Monorepo Support

Deploy multiple services from a monorepo.

### Directory Structure

```
my-monorepo/
├── apps/
│   ├── api/
│   │   └── enclii.yaml
│   └── web/
│       └── enclii.yaml
└── packages/
    └── shared/
```

### Service Configuration

```yaml
# apps/api/enclii.yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: api
  project: my-project
spec:
  github:
    repo: org/my-monorepo
    appPath: apps/api        # Path to this service
    watchPaths:              # Trigger build on changes to:
      - apps/api/**
      - packages/shared/**
```

### Selective Builds

Enclii only builds services whose `watchPaths` match changed files:

```
# Commit changes apps/api/src/index.js
→ Only 'api' service builds

# Commit changes packages/shared/utils.js
→ Both 'api' and 'web' build (if both watch packages/shared)
```

---

## Security

### Repository Access

The Enclii GitHub App requests minimal permissions:
- **Read**: Repository contents, metadata
- **Write**: Commit statuses, pull request comments, deployments

### Webhook Verification

All webhooks are verified using HMAC-SHA256:

```go
// Verify webhook signature
signature := r.Header.Get("X-Hub-Signature-256")
if !webhook.Verify(payload, signature, secret) {
    return errors.New("invalid signature")
}
```

### Secret Scanning

Enclii automatically prevents committing secrets in `enclii.yaml`:
- API keys detected → Warning in build logs
- Secrets should use `enclii secrets set` instead

---

## Troubleshooting

### Webhook Not Triggering

1. Check webhook delivery status in GitHub:
   - Go to **Settings → Webhooks → Recent Deliveries**

2. Verify webhook secret:
   ```bash
   enclii webhooks verify --repo org/repo
   ```

3. Check Enclii service status:
   - [status.enclii.dev](https://status.enclii.dev)

### Build Not Starting

1. Verify repository is linked:
   ```bash
   enclii services show api
   ```

2. Check branch configuration:
   ```bash
   enclii services config api
   ```

3. View webhook logs:
   ```bash
   enclii webhooks logs --repo org/repo --since 1h
   ```

### Preview Environment Not Created

1. Verify preview environments are enabled
2. Check if PR author has access to the project
3. View error in PR comment or build logs

---

## Related Documentation

- **Getting Started**: [Quick Start Guide](/getting-started/QUICKSTART)
- **CLI**: [CLI Reference](/cli/) | [Deploy Command](/cli/commands/deploy)
- **SDK**: [TypeScript SDK - Deployments](/sdk/typescript/deployments)
- **Troubleshooting**: [Build Failures](/troubleshooting/build-failures) | [Deployment Issues](/troubleshooting/deployment-issues)
- **FAQ**: [General FAQ](/faq/general)
- **Other Integrations**: [SSO Integration](/integrations/sso)
