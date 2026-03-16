# Monorepo Project Deployment Model

> **Status**: Design Complete
> **Author**: Claude Code
> **Date**: 2025-12-11

## Overview

This document defines the architecture for deploying multi-service monorepo projects through Enclii. The design enables users to import a single repository containing multiple deployable services (like Janua with `apps/api`, `apps/dashboard`, `apps/docs`) and manage them as a cohesive project with coordinated deployments.

## Problem Statement

### Current State
- Each `Service` has its own `git_repo` and `app_path`
- Users must create services individually when importing a monorepo
- No coordination between services during deployment
- Webhook handling treats each service independently
- No shared build context or caching

### Desired State
- Import a repository once → auto-discover services
- Project-level repository binding with service inheritance
- Coordinated multi-service deployments with dependency ordering
- Smart webhook handling that detects affected services
- Shared build artifacts and caching

## Architecture

### Data Model

```
┌─────────────────────────────────────────────────────────────────┐
│                           PROJECT                                │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ id: uuid                                                  │  │
│  │ name: "Janua"                                             │  │
│  │ slug: "janua"                                             │  │
│  │ git_repo: "https://github.com/madfam-org/janua"           │  │
│  │ git_branch: "main"                                        │  │
│  │ monorepo_mode: true                                       │  │
│  └──────────────────────────────────────────────────────────┘  │
│                              │                                   │
│                              ▼                                   │
│  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐    │
│  │    SERVICE     │  │    SERVICE     │  │    SERVICE     │    │
│  │  janua-api     │  │ janua-dashboard│  │  janua-docs    │    │
│  │ path: apps/api │  │ path: apps/    │  │ path: apps/    │    │
│  │ port: 4100     │  │      dashboard │  │      docs      │    │
│  └───────┬────────┘  │ port: 4101     │  │ port: 4103     │    │
│          │           └───────┬────────┘  └────────────────┘    │
│          │                   │                                   │
│          │    depends_on     │                                   │
│          └───────────────────┘                                   │
└─────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      DEPLOYMENT GROUP                            │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ id: uuid                                                  │  │
│  │ project_id: <project>                                     │  │
│  │ environment_id: <prod>                                    │  │
│  │ strategy: "dependency-ordered"                            │  │
│  │ status: "in_progress"                                     │  │
│  │ git_sha: "abc123"                                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│          │                   │                   │               │
│          ▼                   ▼                   ▼               │
│   ┌────────────┐      ┌────────────┐      ┌────────────┐       │
│   │ DEPLOYMENT │      │ DEPLOYMENT │      │ DEPLOYMENT │       │
│   │ api@v1.2.3 │      │dashboard@  │      │ docs@v1.2.3│       │
│   │ status: ✓  │      │  v1.2.3    │      │ status: ⏳  │       │
│   └────────────┘      │ status: 🔄 │      └────────────┘       │
│                       └────────────┘                             │
└─────────────────────────────────────────────────────────────────┘
```

### Database Schema Changes

```sql
-- Extend projects table for monorepo support
ALTER TABLE projects ADD COLUMN git_repo TEXT;
ALTER TABLE projects ADD COLUMN git_branch TEXT DEFAULT 'main';
ALTER TABLE projects ADD COLUMN monorepo_mode BOOLEAN DEFAULT false;
ALTER TABLE projects ADD COLUMN monorepo_config JSONB DEFAULT '{}';

-- Service dependencies for deployment ordering
CREATE TABLE service_dependencies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    depends_on_service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    dependency_type TEXT NOT NULL DEFAULT 'runtime',
    -- Types: 'runtime' (must be running), 'build' (must build first), 'deploy' (deploy order only)
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(service_id, depends_on_service_id),
    CHECK (service_id != depends_on_service_id)
);

-- Deployment groups for atomic multi-service deployments
CREATE TABLE deployment_groups (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    environment_id UUID NOT NULL REFERENCES environments(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    -- Status: pending, building, deploying, completed, failed, rolled_back
    strategy TEXT NOT NULL DEFAULT 'parallel',
    -- Strategy: parallel, sequential, dependency-ordered
    trigger_type TEXT NOT NULL DEFAULT 'manual',
    -- Trigger: manual, webhook, scheduled, cli
    git_sha TEXT,
    created_by UUID REFERENCES users(id),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    rollback_group_id UUID REFERENCES deployment_groups(id),
    error_message TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Link deployments to groups
ALTER TABLE deployments ADD COLUMN deployment_group_id UUID REFERENCES deployment_groups(id);
ALTER TABLE deployments ADD COLUMN deploy_order INT DEFAULT 0;

-- Index for efficient queries
CREATE INDEX idx_deployment_groups_project ON deployment_groups(project_id, environment_id);
CREATE INDEX idx_deployments_group ON deployments(deployment_group_id);
CREATE INDEX idx_service_dependencies_service ON service_dependencies(service_id);
```

### Go Types

```go
// Extended Project type
type Project struct {
    ID            uuid.UUID       `json:"id" db:"id"`
    Name          string          `json:"name" db:"name"`
    Slug          string          `json:"slug" db:"slug"`
    GitRepo       *string         `json:"git_repo,omitempty" db:"git_repo"`
    GitBranch     string          `json:"git_branch" db:"git_branch"`
    MonorepoMode  bool            `json:"monorepo_mode" db:"monorepo_mode"`
    MonorepoConfig *MonorepoConfig `json:"monorepo_config,omitempty" db:"monorepo_config"`
    CreatedAt     time.Time       `json:"created_at" db:"created_at"`
    UpdatedAt     time.Time       `json:"updated_at" db:"updated_at"`
}

type MonorepoConfig struct {
    Tool             string   `json:"tool,omitempty"`             // turborepo, nx, lerna, pnpm
    PackageManager   string   `json:"package_manager,omitempty"`  // npm, yarn, pnpm
    SharedPaths      []string `json:"shared_paths,omitempty"`     // paths that affect all services
    DetectionEnabled bool     `json:"detection_enabled"`          // auto-detect affected services
}

type ServiceDependency struct {
    ID                  uuid.UUID `json:"id" db:"id"`
    ServiceID           uuid.UUID `json:"service_id" db:"service_id"`
    DependsOnServiceID  uuid.UUID `json:"depends_on_service_id" db:"depends_on_service_id"`
    DependencyType      string    `json:"dependency_type" db:"dependency_type"`
    CreatedAt           time.Time `json:"created_at" db:"created_at"`
}

type DeploymentGroup struct {
    ID              uuid.UUID   `json:"id" db:"id"`
    ProjectID       uuid.UUID   `json:"project_id" db:"project_id"`
    EnvironmentID   uuid.UUID   `json:"environment_id" db:"environment_id"`
    Name            string      `json:"name" db:"name"`
    Status          string      `json:"status" db:"status"`
    Strategy        string      `json:"strategy" db:"strategy"`
    TriggerType     string      `json:"trigger_type" db:"trigger_type"`
    GitSHA          *string     `json:"git_sha,omitempty" db:"git_sha"`
    CreatedBy       *uuid.UUID  `json:"created_by,omitempty" db:"created_by"`
    StartedAt       *time.Time  `json:"started_at,omitempty" db:"started_at"`
    CompletedAt     *time.Time  `json:"completed_at,omitempty" db:"completed_at"`
    RollbackGroupID *uuid.UUID  `json:"rollback_group_id,omitempty" db:"rollback_group_id"`
    ErrorMessage    *string     `json:"error_message,omitempty" db:"error_message"`
    CreatedAt       time.Time   `json:"created_at" db:"created_at"`
}

type DeploymentGroupStatus string

const (
    DeploymentGroupPending    DeploymentGroupStatus = "pending"
    DeploymentGroupBuilding   DeploymentGroupStatus = "building"
    DeploymentGroupDeploying  DeploymentGroupStatus = "deploying"
    DeploymentGroupCompleted  DeploymentGroupStatus = "completed"
    DeploymentGroupFailed     DeploymentGroupStatus = "failed"
    DeploymentGroupRolledBack DeploymentGroupStatus = "rolled_back"
)

type DeploymentStrategy string

const (
    StrategyParallel          DeploymentStrategy = "parallel"
    StrategySequential        DeploymentStrategy = "sequential"
    StrategyDependencyOrdered DeploymentStrategy = "dependency-ordered"
)
```

## API Design

### Repository Analysis

```
POST /v1/integrations/github/repos/:owner/:repo/analyze
Authorization: Bearer <token>

Request:
{
    "branch": "main"
}

Response:
{
    "repository": {
        "full_name": "madfam-org/janua",
        "default_branch": "main",
        "is_monorepo": true
    },
    "monorepo_config": {
        "tool": "pnpm",
        "package_manager": "pnpm",
        "workspace_file": "pnpm-workspace.yaml"
    },
    "detected_services": [
        {
            "name": "api",
            "path": "apps/api",
            "type": "python-fastapi",
            "dockerfile": "apps/api/Dockerfile",
            "port": 4100,
            "confidence": 0.95
        },
        {
            "name": "dashboard",
            "path": "apps/dashboard",
            "type": "nextjs",
            "dockerfile": "apps/dashboard/Dockerfile",
            "port": 3000,
            "confidence": 0.90
        },
        {
            "name": "docs",
            "path": "apps/docs",
            "type": "docusaurus",
            "dockerfile": null,
            "buildpack": "node",
            "port": 3000,
            "confidence": 0.85
        }
    ],
    "suggested_dependencies": [
        {
            "from": "dashboard",
            "to": "api",
            "type": "runtime",
            "reason": "Detected API_URL environment variable"
        }
    ],
    "shared_packages": [
        "packages/ui",
        "packages/core",
        "packages/config"
    ]
}
```

### Create Monorepo Project

```
POST /v1/projects
Authorization: Bearer <token>

Request:
{
    "name": "Janua",
    "slug": "janua",
    "description": "Self-hosted OAuth/OIDC provider",
    "source": {
        "git": {
            "repository": "https://github.com/madfam-org/janua",
            "branch": "main"
        }
    },
    "monorepo_config": {
        "tool": "pnpm",
        "package_manager": "pnpm",
        "detection_enabled": true,
        "shared_paths": ["packages/", "libs/"]
    },
    "services": [
        {
            "name": "api",
            "path": "apps/api",
            "build": {
                "type": "dockerfile",
                "dockerfile": "apps/api/Dockerfile"
            },
            "runtime": {
                "port": 4100,
                "replicas": 3
            },
            "auto_deploy": {
                "enabled": true,
                "branch": "main",
                "environment": "production"
            }
        },
        {
            "name": "dashboard",
            "path": "apps/dashboard",
            "build": {
                "type": "dockerfile",
                "dockerfile": "apps/dashboard/Dockerfile"
            },
            "runtime": {
                "port": 4101,
                "replicas": 2
            }
        }
    ],
    "dependencies": [
        {
            "from": "dashboard",
            "to": "api",
            "type": "runtime"
        }
    ],
    "environments": [
        {"name": "development", "auto_create": true},
        {"name": "staging", "auto_create": true},
        {"name": "production", "auto_create": true}
    ]
}

Response:
{
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "Janua",
    "slug": "janua",
    "git_repo": "https://github.com/madfam-org/janua",
    "monorepo_mode": true,
    "services": [
        {"id": "...", "name": "api", "path": "apps/api"},
        {"id": "...", "name": "dashboard", "path": "apps/dashboard"}
    ],
    "environments": [
        {"id": "...", "name": "development"},
        {"id": "...", "name": "staging"},
        {"id": "...", "name": "production"}
    ],
    "created_at": "2025-12-11T00:00:00Z"
}
```

### Group Deployment

```
POST /v1/projects/:slug/deploy
Authorization: Bearer <token>

Request:
{
    "environment": "production",
    "services": ["api", "dashboard"],  // or "all" for all services
    "strategy": "dependency-ordered",
    "git_ref": "main",  // branch, tag, or SHA
    "wait": false       // return immediately or wait for completion
}

Response:
{
    "deployment_group": {
        "id": "...",
        "name": "deploy-2025-12-11-abc123",
        "status": "building",
        "strategy": "dependency-ordered",
        "git_sha": "abc123def456...",
        "deployments": [
            {
                "service": "api",
                "deployment_id": "...",
                "release_id": null,
                "status": "building",
                "deploy_order": 1
            },
            {
                "service": "dashboard",
                "deployment_id": "...",
                "release_id": null,
                "status": "pending",
                "deploy_order": 2
            }
        ]
    }
}
```

### Get Deployment Group Status

```
GET /v1/projects/:slug/deployments/:group_id
Authorization: Bearer <token>

Response:
{
    "id": "...",
    "name": "deploy-2025-12-11-abc123",
    "status": "deploying",
    "strategy": "dependency-ordered",
    "git_sha": "abc123def456...",
    "started_at": "2025-12-11T10:00:00Z",
    "deployments": [
        {
            "service": "api",
            "status": "running",
            "health": "healthy",
            "release": {
                "version": "v1.2.3",
                "image_uri": "...",
                "git_sha": "abc123"
            }
        },
        {
            "service": "dashboard",
            "status": "deploying",
            "health": "unknown",
            "release": {
                "version": "v1.2.3",
                "image_uri": "...",
                "git_sha": "abc123"
            }
        }
    ],
    "progress": {
        "total": 2,
        "completed": 1,
        "in_progress": 1,
        "failed": 0
    }
}
```

## UI Flow

### Import Wizard

```
┌─────────────────────────────────────────────────────────────────┐
│  Step 1: Select Repository                                      │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  🔍 Search repositories...                                       │
│                                                                  │
│  📦 madfam-org/janua              ⭐ Monorepo Detected            │
│     Self-hosted OAuth/OIDC       5 services found                │
│     Updated 2 hours ago          [ Select → ]                    │
│                                                                  │
│  📦 madfam-org/enclii             ⭐ Monorepo Detected            │
│     Open source DevOps platform  8 services found                │
│     Updated 1 day ago            [ Select → ]                    │
│                                                                  │
│  📦 madfam-org/simple-app                                         │
│     Simple Node.js app           Single service                  │
│     Updated 3 days ago           [ Select → ]                    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  Step 2: Configure Services                                      │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  📦 janua - 5 services detected                                  │
│                                                                  │
│  ☑ apps/api                                                      │
│    │ Name: janua-api                                             │
│    │ Type: Python FastAPI    Port: 4100                          │
│    │ Dockerfile: apps/api/Dockerfile                             │
│    └ [ Configure ▼ ]                                             │
│                                                                  │
│  ☑ apps/dashboard                                                │
│    │ Name: janua-dashboard                                       │
│    │ Type: Next.js           Port: 4101                          │
│    │ Depends on: janua-api                                       │
│    └ [ Configure ▼ ]                                             │
│                                                                  │
│  ☐ apps/admin                                                    │
│    │ Name: janua-admin                                           │
│    │ Type: Next.js           Port: 4102                          │
│    └ [ Configure ▼ ]                                             │
│                                                                  │
│  ☑ apps/docs                                                     │
│    │ Name: janua-docs                                            │
│    │ Type: Docusaurus        Port: 4103                          │
│    │ Build: Nixpacks (no Dockerfile)                             │
│    └ [ Configure ▼ ]                                             │
│                                                                  │
│  ☐ apps/landing                                                  │
│    │ Name: janua-landing                                         │
│    │ Type: Next.js           Port: 4104                          │
│    └ [ Configure ▼ ]                                             │
│                                                                  │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ Dependencies                                               │  │
│  │ janua-dashboard ───depends on───→ janua-api              │  │
│  │ [ + Add Dependency ]                                       │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  [ ← Back ]                              [ Next: Environments → ]│
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  Step 3: Environment Setup                                       │
│  ─────────────────────────────────────────────────────────────  │
│                                                                  │
│  Environments                                                    │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ ☑ development   Auto-deploy: main → preview               │  │
│  │ ☑ staging       Auto-deploy: main → staging               │  │
│  │ ☑ production    Manual deploy only                        │  │
│  │ [ + Add Environment ]                                      │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Domain Configuration                                            │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ janua-api                                                  │  │
│  │   production: auth.madfam.io                               │  │
│  │   staging:    api-staging.janua.dev                       │  │
│  │                                                            │  │
│  │ janua-dashboard                                            │  │
│  │   production: app.janua.dev                               │  │
│  │   staging:    app-staging.janua.dev                       │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  [ ← Back ]                                  [ Create Project → ]│
└─────────────────────────────────────────────────────────────────┘
```

### Project Dashboard

```
┌─────────────────────────────────────────────────────────────────┐
│  Janua                                              ⚙️ Settings  │
│  github.com/madfam-org/janua                                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Services                                    [ Deploy All ▼ ]    │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  Service          Production      Staging      Development│  │
│  ├───────────────────────────────────────────────────────────┤  │
│  │  🟢 janua-api     v1.2.3 ✓       v1.2.4 ✓    v1.3.0-dev   │  │
│  │     auth.madfam.io                                          │  │
│  │                                                            │  │
│  │  🟢 janua-dash    v1.2.3 ✓       v1.2.4 ✓    v1.3.0-dev   │  │
│  │     app.janua.dev                                          │  │
│  │                                                            │  │
│  │  🟢 janua-docs    v1.2.3 ✓       v1.2.3 ✓    v1.2.3       │  │
│  │     docs.janua.dev                                         │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Recent Deployments                                              │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │  🟢 deploy-2025-12-11-abc123    production    2 services  │  │
│  │     Completed 10 minutes ago                               │  │
│  │                                                            │  │
│  │  🟢 deploy-2025-12-10-def456    staging       3 services  │  │
│  │     Completed 1 day ago                                    │  │
│  │                                                            │  │
│  │  🔴 deploy-2025-12-09-ghi789    production    2 services  │  │
│  │     Failed - rolled back                                   │  │
│  └───────────────────────────────────────────────────────────┘  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Webhook Handling

### Change Detection Algorithm

```go
func (h *WebhookHandler) HandlePush(push GitPushEvent) error {
    // Find project by repository
    project, err := h.db.GetProjectByGitRepo(push.Repository.CloneURL)
    if err != nil {
        return err
    }

    if !project.MonorepoMode {
        // Legacy single-service handling
        return h.handleSingleServicePush(push, project)
    }

    // Get all changed files from commits
    changedFiles := collectChangedFiles(push.Commits)

    // Determine affected services
    affected := h.getAffectedServices(project, changedFiles)
    if len(affected) == 0 {
        log.Info("No services affected by push", "project", project.Slug)
        return nil
    }

    // Create deployment group
    group := &DeploymentGroup{
        ProjectID:     project.ID,
        EnvironmentID: getAutoDeployEnv(project, push.Ref),
        Strategy:      StrategyDependencyOrdered,
        TriggerType:   "webhook",
        GitSHA:        push.After,
    }

    // Build and deploy affected services
    return h.deployGroup(group, affected)
}

func (h *WebhookHandler) getAffectedServices(project Project, changed []string) []Service {
    services := h.db.GetServicesByProject(project.ID)
    config := project.MonorepoConfig
    affected := make(map[uuid.UUID]Service)

    for _, file := range changed {
        // Check each service's path
        for _, svc := range services {
            if strings.HasPrefix(file, svc.AppPath+"/") {
                affected[svc.ID] = svc
            }
        }

        // Check shared paths (packages/, libs/)
        for _, sharedPath := range config.SharedPaths {
            if strings.HasPrefix(file, sharedPath) {
                // Get services that depend on this shared package
                dependents := h.getServicesDependingOnPath(services, sharedPath, file)
                for _, svc := range dependents {
                    affected[svc.ID] = svc
                }
            }
        }

        // Root config changes affect all services
        if isRootConfig(file, config.Tool) {
            for _, svc := range services {
                affected[svc.ID] = svc
            }
            break
        }
    }

    // Expand to include dependents
    return h.expandDependents(affected, project.ID)
}

func isRootConfig(file string, tool string) bool {
    rootConfigs := []string{
        "turbo.json", "nx.json", "lerna.json",
        "pnpm-workspace.yaml", "package.json",
        ".enclii/project.yaml",
    }
    for _, cfg := range rootConfigs {
        if file == cfg {
            return true
        }
    }
    return false
}
```

## Deployment Orchestration

### Dependency-Ordered Strategy

```go
func (o *Orchestrator) DeployGroup(group *DeploymentGroup) error {
    services := o.db.GetServicesByGroup(group.ID)
    deps := o.db.GetDependencies(group.ProjectID)

    // Build dependency graph
    graph := buildDependencyGraph(services, deps)

    // Topological sort for deploy order
    order, err := graph.TopologicalSort()
    if err != nil {
        return fmt.Errorf("circular dependency detected: %w", err)
    }

    // Deploy in order
    for i, serviceID := range order {
        deployment := o.db.GetDeployment(group.ID, serviceID)
        deployment.DeployOrder = i + 1

        // Wait for dependencies to be healthy
        if err := o.waitForDependencies(deployment, deps); err != nil {
            return o.rollbackGroup(group, err)
        }

        // Deploy this service
        if err := o.deployService(deployment); err != nil {
            return o.rollbackGroup(group, err)
        }

        // Wait for healthy
        if err := o.waitForHealthy(deployment); err != nil {
            return o.rollbackGroup(group, err)
        }
    }

    group.Status = DeploymentGroupCompleted
    group.CompletedAt = time.Now()
    return o.db.UpdateDeploymentGroup(group)
}

func (o *Orchestrator) rollbackGroup(group *DeploymentGroup, cause error) error {
    log.Error("Deployment group failed, initiating rollback",
        "group", group.ID, "error", cause)

    group.Status = DeploymentGroupFailed
    group.ErrorMessage = cause.Error()

    // Get previous successful deployment for each service
    for _, deployment := range o.db.GetDeploymentsByGroup(group.ID) {
        if deployment.Status == DeploymentStatusRunning {
            prev := o.db.GetPreviousDeployment(deployment)
            if prev != nil {
                o.rollbackDeployment(deployment, prev)
            }
        }
    }

    group.Status = DeploymentGroupRolledBack
    return o.db.UpdateDeploymentGroup(group)
}
```

## Implementation Phases

### Phase 1: Database & Core Types
- Add schema migrations
- Update Go types
- Backward compatibility for existing projects

### Phase 2: Repository Analysis API
- Implement `/v1/integrations/github/repos/:owner/:repo/analyze`
- Service detection heuristics
- Dependency suggestion logic

### Phase 3: Project Creation API
- Enhanced `POST /v1/projects` with services array
- Service dependency creation
- Environment auto-creation

### Phase 4: Group Deployment
- Deployment group creation and tracking
- Dependency-ordered deployment
- Rollback orchestration

### Phase 5: UI Implementation
- Import wizard with service detection
- Project dashboard with multi-service view
- Deployment group status tracking

### Phase 6: Smart Webhooks
- Change detection algorithm
- Affected service calculation
- Auto-deploy for monorepos

### Phase 7: Build Optimization
- Shared build cache
- Turborepo/Nx integration
- Parallel builds for independent services

## Backward Compatibility

- Existing projects without `git_repo` continue to work unchanged
- Existing services with their own `git_repo` are not affected
- `monorepo_mode: false` (default) preserves current behavior
- Single-service imports create legacy-style projects

## Security Considerations

- Repository access verified through GitHub OAuth token
- Service detection only reads repository structure (no code execution)
- Deployment permissions apply per-environment as before
- Audit logs track group deployments with all included services

## Metrics & Observability

- `enclii_deployment_group_duration_seconds` - Group deployment duration
- `enclii_deployment_group_status` - Status per group (gauge)
- `enclii_affected_services_total` - Services affected by webhook (counter)
- `enclii_rollback_total` - Group rollbacks (counter)

## Related Documents

- [Onboarding Guide](../guides/ONBOARDING_GUIDE.md)
- [Production Deployment Roadmap](../production/PRODUCTION_DEPLOYMENT_ROADMAP.md)
- [Service Spec Format](../reference/service-spec.md)
