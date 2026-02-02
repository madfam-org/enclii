# Change Management Policy

Formalizes the GitOps-based change management workflow for the Enclii platform.

**Last reviewed:** 2026-02-01
**Owner:** Platform Engineering
**Review cadence:** Quarterly

---

## Principles

1. **Git is the source of truth.** All changes to infrastructure and application code flow through version-controlled repositories.
2. **No manual changes in production.** ArgoCD reconciles desired state from Git; manual kubectl edits trigger drift alerts.
3. **Every change is reviewed.** Pull requests with CI gates are required for all changes to protected branches.
4. **Changes are reversible.** Canary deployments and automated rollback ensure rapid recovery.

---

## Branching Strategy

Enclii uses **trunk-based development** on `main`.

| Branch Type | Naming | Lifetime | Merge Target |
|------------|--------|----------|-------------|
| Main | `main` | Permanent | -- |
| Feature | `feature/<description>` | Days | `main` |
| Fix | `fix/<description>` | Hours to days | `main` |
| Release | Tag-based (`v1.2.3`) | Permanent tag | -- |

Rules:
- Direct commits to `main` are blocked.
- Feature branches must be short-lived (target: less than 3 days).
- Rebase or squash merge to maintain linear history.

---

## Commit Standards

All commits follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

| Type | Usage |
|------|-------|
| `feat` | New feature or capability |
| `fix` | Bug fix |
| `build` | Build system or dependency changes |
| `ci` | CI/CD pipeline changes |
| `docs` | Documentation only |
| `refactor` | Code change that neither fixes a bug nor adds a feature |
| `test` | Adding or updating tests |
| `chore` | Maintenance tasks |

Commit messages are validated by a pre-commit hook installed via `make bootstrap`.

---

## Pull Request Requirements

### Authoring

- Title follows conventional commit format.
- Description includes: what changed, why, and how to verify.
- Related issues are linked.
- Self-review completed before requesting review.

### Review Gates

| Gate | Requirement | Enforced By |
|------|-------------|-------------|
| Peer review | At least 1 approving review | GitHub branch protection |
| CI -- Lint | `golangci-lint run` (Go), `pnpm lint` (TS) | GitHub Actions |
| CI -- Test | `make test` passes | GitHub Actions |
| CI -- Security | gosec, govulncheck pass with no Critical/High | GitHub Actions |
| CI -- Manifest audit | `kubectl apply --dry-run=client` for K8s changes | GitHub Actions |
| CI -- Build | Container image builds successfully | GitHub Actions |
| Merge conflict | Branch is up to date with `main` | GitHub branch protection |

### Approval Matrix

| Change Scope | Required Approvers |
|-------------|-------------------|
| Application code | 1 engineer |
| Infrastructure (`infra/`) | 1 engineer with infra access |
| Security-sensitive (auth, secrets, policies) | 2 engineers |
| Production configuration | Platform lead |

---

## CI/CD Pipeline

### Build Pipeline

```
Push to feature branch
  |
  v
GitHub Actions trigger
  |
  +-- Lint (golangci-lint, eslint, prettier)
  +-- Unit tests (go test, pnpm test)
  +-- Security scan (gosec, govulncheck, Trivy)
  +-- K8s manifest validation (dry-run)
  +-- Container image build
  +-- Image signing (cosign)
  +-- SBOM generation (Syft)
  |
  v
All gates pass --> PR mergeable
```

### Deployment Pipeline (ArgoCD)

```
Merge to main
  |
  v
ArgoCD detects change (pull-based, 3-minute poll)
  |
  v
Kyverno admission check
  |  - Image signed?
  |  - SBOM present?
  |  - Resource quotas respected?
  |
  v
RollingUpdate deployment
  |
  v
Health check verification (liveness + readiness probes)
  |
  v
Deployment complete
```

ArgoCD configuration: `infra/argocd/root-application.yaml`
App-of-Apps pattern: `infra/argocd/apps/`

### Drift Detection

ArgoCD self-heal is enabled. When detected:

1. ArgoCD reverts the drift automatically.
2. An alert fires to the platform team.
3. The incident is logged for audit.

---

## Deployment Strategies

| Strategy | When Used | Configuration |
|----------|-----------|---------------|
| RollingUpdate | Default for all services | Zero-downtime, managed by ArgoCD |
| Canary | Production releases of critical services | `--strategy canary --canary-percent 10` |
| Blue-Green | Database migrations or breaking changes | Manual cutover after validation |

### Canary Deployment Process

1. Deploy new version to 10% of traffic.
2. Monitor error rate and latency for 5 minutes.
3. If error rate exceeds 2% for 2 minutes, automatic rollback triggers.
4. If healthy, progressively increase to 50%, then 100%.

---

## Rollback Procedures

### Automated Rollback

Triggers automatically when:
- Error rate exceeds 2% for 2 consecutive minutes during canary.
- Liveness probe fails 3 consecutive times.
- ArgoCD sync fails (reverts to last known good state).

### Manual Rollback

```bash
# Via CLI
enclii rollback <service> --env production

# Via kubectl
kubectl rollout undo deployment/<service> -n <namespace>

# Via ArgoCD (revert Git commit)
git revert <commit-sha> && git push origin main
```

### Rollback Verification

After any rollback:
1. Confirm service health via `/health` endpoint.
2. Verify error rates return to baseline in Grafana.
3. File an incident report documenting the cause and resolution.

---

## Emergency Changes

For incidents requiring immediate production changes:

1. **Declare emergency** -- Notify the platform team.
2. **Expedited PR** -- Create PR with `emergency:` prefix. Requires 1 approver (can be post-merge for Critical).
3. **Deploy** -- Merge triggers standard ArgoCD sync.
4. **Post-incident** -- Within 48 hours: file incident report, complete any skipped review, update runbooks.

Emergency changes that bypass review must be retroactively reviewed within 24 hours.

---

## Audit Trail

All changes produce the following audit evidence:

| Artifact | Location | Retention |
|----------|----------|-----------|
| Git commits | GitHub repository | Indefinite |
| PR reviews and approvals | GitHub PR history | Indefinite |
| CI/CD pipeline logs | GitHub Actions | 90 days |
| ArgoCD sync events | ArgoCD audit log | 90 days |
| Deployment events | `apps/switchyard-api/internal/audit/` | 12 months |
| Image signatures | Container registry (ghcr.io) | Image lifecycle |
| SBOMs | Cloudflare R2 | Image lifecycle |

---

## Change Categories and Lead Times

| Category | Examples | Typical Lead Time |
|----------|---------|-------------------|
| Standard | Feature PRs, dependency updates | 1--3 days |
| Infrastructure | K8s manifest changes, Terraform | 1--5 days |
| Security patch | CVE remediation | Per vulnerability SLA |
| Emergency | Production incident fix | Hours |
