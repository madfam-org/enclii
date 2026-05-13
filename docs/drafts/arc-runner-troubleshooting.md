---
title: ARC Runner Troubleshooting
description: Troubleshooting guide for GitHub Actions Runner Controller (ARC) issues
sidebar_label: ARC Runners (Draft)
draft: true
tags: [draft, troubleshooting, arc, github-actions, ci-cd]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# CI/CD Pipeline Troubleshooting - FULLY RESOLVED

:::note Draft Document
This document is a draft and may be moved to the main troubleshooting section after review.
:::

**Date**: 2026-01-14
**Status**: ✅ RESOLVED - ARC runners and webhooks working

## Executive Summary

CI/CD pipeline is now **fully operational**. Three issues were resolved:

1. **Runner crashes** - Fixed by simplifying Helm values (no custom containers)
2. **Jobs not assigned** - Fixed by enabling "Allow public repositories" in Default runner group
3. **Webhook signature verification failing** - Fixed by recreating GitHub webhook with correct secret

**Current State:**
- ARC runners active and processing GitHub Actions jobs
- Auto-scaling working (1 → 6 based on demand)
- GitHub webhook verified (200 status)
- Builds being triggered and enqueued to Roundhouse

---

## Issue History

### Issue 1: Runner Pods Crashing (RESOLVED)

**Problem**: Runners crashed immediately (~1-2 seconds) after starting.

**Root Cause**: When defining custom `template.spec.containers`, ARC's default injection is overridden. The runner image has NO entrypoint (`Entrypoint: [] | Cmd: [/bin/bash]`), so without explicit command, it exits immediately.

**Solution**: Remove all custom container definitions from values files. Let ARC handle container injection.

### Issue 2: Jobs Not Being Assigned (RESOLVED)

**Problem**: Runner is stable, registered (runnerId: 44), and "Listening for Jobs", but GitHub jobs remain "queued" forever.

**Root Cause**: The runner is registered at org level in the "Default" runner group, but this group doesn't have access to the `enclii` repository.

**Evidence**:
```bash
# Repo-level runners show 0:
$ gh api repos/madfam-org/enclii/actions/runners
{"total_count":0,"runners":[]}

# EphemeralRunner shows:
# actions.github.com/runner-group-name: Default

# Listener shows repeated:
# "assigned job": 0, "totalAvailableJobs": 0
```

### Issue 3: Webhook Signature Verification Failing (RESOLVED)

**Problem**: After pushing commits to `main`, GitHub webhooks were rejected with 401 (signature verification failed). Builds weren't being triggered.

**Root Cause**: The GitHub webhook was configured with a different secret than what switchyard-api was using. The GitHub API PATCH endpoint doesn't reliably update webhook secrets.

**Evidence**:
```bash
# Webhook delivery showing 401
gh api repos/madfam-org/enclii/hooks/585841923/deliveries --jq '.[0]'
# {"status":"Invalid HTTP Response: 401","status_code":401}

# API logs
kubectl logs -n enclii deploy/switchyard-api | grep webhook
# "message":"GitHub webhook signature verification failed"
```

**Solution**: Delete and recreate the webhook with the correct secret:
```bash
# Delete old webhook
gh api repos/madfam-org/enclii/hooks/585841923 -X DELETE

# Create new webhook with correct secret
gh api repos/madfam-org/enclii/hooks -X POST \
  -f 'name=web' \
  -f 'config[url]=https://api.enclii.dev/v1/webhooks/github' \
  -f 'config[content_type]=json' \
  -f 'config[secret]=<WEBHOOK_SECRET_FROM_SWITCHYARD_ENV>' \
  -f 'events[]=push' \
  -F 'active=true'
```

**Verification**:
```bash
# Check switchyard-api secret
kubectl exec -n enclii deploy/switchyard-api -- env | grep GITHUB_WEBHOOK_SECRET

# After push, check for 200 response
kubectl logs -n enclii deploy/switchyard-api | grep "webhooks/github"
# Should show: 200 | POST "/v1/webhooks/github"
```

---

## Solution: Configure Runner Group Access

### What Was Done (2026-01-14)

1. Went to: `https://github.com/organizations/madfam-org/settings/actions/runner-groups/1`
2. The "Default" runner group already had "All repositories" access
3. **Key fix**: Checked "Allow public repositories" checkbox
   - The `enclii` repo is PUBLIC
   - By default, self-hosted runners don't run on public repos (security)
   - Enabling this allows runners to pick up jobs from public repos

### Alternative: Use GitHub UI

1. **Log into GitHub** as an organization admin
2. Go to: `https://github.com/organizations/madfam-org/settings/actions/runner-groups`
3. Click on the "Default" runner group
4. Ensure "All repositories" is selected
5. **Check "Allow public repositories"** if your repos are public
6. Save changes

### Option B: Use GitHub API (Requires admin:org scope)

```bash
# Get runner group ID
gh api orgs/madfam-org/actions/runner-groups | jq '.runner_groups[] | {id, name}'

# Set repository access (assuming group ID is 1)
gh api -X PUT orgs/madfam-org/actions/runner-groups/1/repositories/REPO_ID
```

### Option C: Create New Runner Group with All Access

```bash
# Create a new runner group with "All repositories" access
gh api -X POST orgs/madfam-org/actions/runner-groups \
  -f name="madfam-runners" \
  -f visibility="all"
```

Then update `values-runner-set.yaml`:
```yaml
runnerGroup: "madfam-runners"
```

---

## Current Configuration

### values-runner-set.yaml
```yaml
# Org-level registration - GitHub App has org permissions
githubConfigUrl: "https://github.com/madfam-org"
githubConfigSecret: github-app-secret

# NOTE: runnerGroup removed - using Default group
# runnerGroup needs org-level configuration for repo access

template:
  spec:
    serviceAccountName: arc-runner
    terminationGracePeriodSeconds: 300
    # Do NOT define containers - let ARC inject them
```

### Working Runner State
- **Runner Pod**: Running (1/1) and stable
- **Runner ID**: 44
- **Status**: Connected to GitHub, Listening for Jobs
- **Runner Group**: Default
- **Label**: `madfam-runners-blue`

### Queued Jobs
```
10+ jobs queued with label "madfam-runners-blue"
All showing "Waiting for a runner to pick up this job"
```

---

## Verification After Fix

After configuring runner group access:

```bash
# 1. Check listener logs - should show jobs being assigned
KUBECONFIG=~/.kube/config-hetzner kubectl logs -n arc-system \
  -l actions.github.com/scale-set-name=madfam-runners-blue --tail=20

# Look for: "assigned job": N (where N > 0)

# 2. Check GitHub - queued jobs should start running
gh run list --repo madfam-org/enclii --status in_progress

# 3. Verify runner appears at repo level
gh api repos/madfam-org/enclii/actions/runners
# Should show: {"total_count":1,"runners":[...]}
```

---

## What We Tried (and failed)

1. **Repo-level URL** (`https://github.com/madfam-org/enclii`)
   - Failed: GitHub App has org permissions, not repo-level runner registration permissions
   - Error: `Resource not accessible by integration`

2. **Removed runnerGroup setting**
   - No change: Still registers to Default group
   - Group permissions are set at org level, not in Helm values

3. **Multiple redeployments**
   - Runner registers successfully each time
   - Issue is GitHub-side runner group configuration

---

## Architecture Notes

### How ARC Job Assignment Works

```
1. GitHub Actions workflow triggered
2. GitHub checks runs-on label (madfam-runners-blue)
3. GitHub looks for matching runner in runner groups with repo access
4. If found, GitHub sends job to listener via long-poll
5. Listener creates EphemeralRunner pod for job
```

The break is at step 3: GitHub has the runner, but the Default runner group doesn't allow the `enclii` repo to use it.

### GitHub App Permissions

The GitHub App (`madfam-org-arc-app`) has:
- Organization-level runner management
- Does NOT have repository-level runner registration
- This is correct - the issue is runner group configuration, not App permissions

---

## Files Modified

- `infra/helm/arc/values-runner-set.yaml` - Simplified, no custom containers
- `infra/helm/arc/values-runner-set-blue.yaml` - Removed PVC volumes
- `infra/helm/arc/values-runner-set-green.yaml` - Removed PVC volumes

---

## Useful Commands

```bash
# Set kubeconfig
export KUBECONFIG=~/.kube/config-hetzner

# Check runner status
kubectl get ephemeralrunner,pods -n arc-runners

# Check listener logs for job assignment
kubectl logs -n arc-system -l actions.github.com/scale-set-name=madfam-runners-blue --tail=50

# Check runner pod logs
kubectl logs -n arc-runners -l actions.github.com/scale-set-name=madfam-runners-blue --tail=50

# Redeploy runners (if needed)
helm upgrade --install madfam-runners-blue \
  oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
  --namespace arc-runners \
  --version 0.10.1 \
  --values infra/helm/arc/values-runner-set.yaml \
  --values infra/helm/arc/values-runner-set-blue.yaml

# Check GitHub queued jobs
gh run list --repo madfam-org/enclii --status queued

# Verify org runner groups (requires admin:org scope)
gh api orgs/madfam-org/actions/runner-groups
```

---

## Future Improvements (After Fix)

### Docker Support Options
1. **Use `setup-docker` GitHub Action** - Installs Docker at job runtime
2. **Use Kaniko for builds** - Rootless container builds (already planned)
3. **Re-enable DinD** - Once basic runners are verified working with jobs

### Caching
1. **Use GitHub Actions cache** - Built-in caching for dependencies
2. **Future: PVC-based caching** - When we add custom volumes back
