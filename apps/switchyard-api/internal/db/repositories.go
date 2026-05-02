package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Repositories provides access to all repository types
type Repositories struct {
	db                   *sql.DB // Keep reference for transaction support
	Projects             *ProjectRepository
	Environments         *EnvironmentRepository
	Services             *ServiceRepository
	Releases             *ReleaseRepository
	Deployments          *DeploymentRepository
	CanaryRollouts       *CanaryRolloutRepository
	Users                *UserRepository
	ProjectAccess        *ProjectAccessRepository
	AuditLogs            *AuditLogRepository
	ApprovalRecords      *ApprovalRecordRepository
	RotationAuditLogs    *RotationAuditLogRepository
	CustomDomains        *CustomDomainRepository
	Routes               *RouteRepository
	DeploymentGroups     *DeploymentGroupRepository
	ServiceDependencies  *ServiceDependencyRepository
	EnvVars              *EnvVarRepository
	PreviewEnvironments  *PreviewEnvironmentRepository
	PreviewComments      *PreviewCommentRepository
	PreviewAccessLogs    *PreviewAccessLogRepository
	Teams                *TeamRepository
	TeamMembers          *TeamMemberRepository
	TeamInvitations      *TeamInvitationRepository
	APITokens            *APITokenRepository
	DatabaseAddons       *DatabaseAddonRepository
	ManagedDBPlans       *ManagedDBPlanRepository
	ManagedDBAddonEvents *ManagedDBAddonEventRepository
	Templates            *TemplateRepository
	Webhooks             *WebhookRepository
	OutboundWebhooks     *OutboundWebhookRepository
	CIRuns               *CIRunRepository
	Functions            *FunctionRepository

	// Deployment Lifecycle & Onboarding
	LifecycleEvents *LifecycleEventRepository
	Onboardings     *OnboardingRepository

	// Timetable (Cron Jobs & One-Off Jobs)
	CronJobs    *CronJobRepository
	CronJobRuns *CronJobRunRepository
	OneOffJobs  *OneOffJobRepository

	// Junctions (Routing & Ingress)
	Junctions *JunctionRepository

	// Admin Control Plane repositories
	Clusters            *ClusterRepository
	BareMetalHosts      *BareMetalHostRepository
	ManagedResources    *ManagedResourceRepository
	VirtualClusters     *VirtualClusterRepository
	PropagationPolicies *PropagationPolicyRepository
	DriftEvents         *DriftEventRepository
	CostAllocations     *CostAllocationRepository

	// Tenant data export (P3.6)
	TenantExports *TenantExportRepository

	// Self-serve signup (P3.2 Sprint 1)
	Signups *SignupRepository

	// Namespace Discoverer (parity audit gap #2): tracks workloads
	// observed in cluster with no matching service row.
	DiscoveredOrphans *DiscoveredOrphanRepository

	// XC-2 master-admin acting sessions
	AdminActingSessions *AdminActingSessionRepository
}

// Ping checks database connectivity for health probes
func (r *Repositories) Ping(ctx context.Context) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("database connection not initialized")
	}
	return r.db.PingContext(ctx)
}

// DB returns the underlying *sql.DB for handlers that need to issue ad-hoc
// queries which don't fit the existing repository scan paths. Use sparingly —
// prefer adding a method to the appropriate repository when the query is
// reusable. The Sentry observability handler uses this for the
// (services.name, services.sentry_project_slug) lookup since the override
// column is read on a single, narrow path and adding it to ServiceRepository
// would inflate every existing scanner.
func (r *Repositories) DB() *sql.DB {
	if r == nil {
		return nil
	}
	return r.db
}

// WithTransaction executes the given function within a database transaction.
// If the function returns an error, the transaction is rolled back.
// If the function succeeds, the transaction is committed.
func (r *Repositories) WithTransaction(ctx context.Context, fn func(txRepos *Repositories) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Create transaction-scoped repositories
	txRepos := &Repositories{
		db:                   r.db, // Keep original db for nested transaction prevention
		Projects:             &ProjectRepository{db: tx},
		Environments:         &EnvironmentRepository{db: tx},
		Services:             &ServiceRepository{db: tx},
		Releases:             &ReleaseRepository{db: tx},
		Deployments:          &DeploymentRepository{db: tx},
		CanaryRollouts:       NewCanaryRolloutRepositoryWithTx(tx),
		Users:                &UserRepository{db: tx},
		ProjectAccess:        &ProjectAccessRepository{db: tx},
		AuditLogs:            &AuditLogRepository{db: tx},
		ApprovalRecords:      &ApprovalRecordRepository{db: tx},
		RotationAuditLogs:    &RotationAuditLogRepository{db: tx},
		CustomDomains:        NewCustomDomainRepositoryWithTx(tx),
		Routes:               NewRouteRepositoryWithTx(tx),
		DeploymentGroups:     NewDeploymentGroupRepositoryWithTx(tx),
		ServiceDependencies:  NewServiceDependencyRepositoryWithTx(tx),
		EnvVars:              NewEnvVarRepositoryWithTx(tx),
		PreviewEnvironments:  NewPreviewEnvironmentRepositoryWithTx(tx),
		PreviewComments:      NewPreviewCommentRepositoryWithTx(tx),
		PreviewAccessLogs:    NewPreviewAccessLogRepositoryWithTx(tx),
		Teams:                NewTeamRepositoryWithTx(tx),
		TeamMembers:          NewTeamMemberRepositoryWithTx(tx),
		TeamInvitations:      NewTeamInvitationRepositoryWithTx(tx),
		APITokens:            NewAPITokenRepositoryWithTx(tx),
		DatabaseAddons:       NewDatabaseAddonRepositoryWithTx(tx),
		ManagedDBPlans:       NewManagedDBPlanRepositoryWithTx(tx),
		ManagedDBAddonEvents: NewManagedDBAddonEventRepositoryWithTx(tx),
		Templates:            NewTemplateRepositoryWithTx(tx),
		Webhooks:             NewWebhookRepositoryWithTx(tx),
		OutboundWebhooks:     NewOutboundWebhookRepositoryWithTx(tx),
		CIRuns:               NewCIRunRepositoryWithTx(tx),
		Functions:            NewFunctionRepositoryWithTx(tx),

		// Timetable
		CronJobs:    NewCronJobRepositoryWithTx(tx),
		CronJobRuns: NewCronJobRunRepositoryWithTx(tx),
		OneOffJobs:  NewOneOffJobRepositoryWithTx(tx),

		// Junctions
		Junctions: NewJunctionRepositoryWithTx(tx),

		// Deployment Lifecycle & Onboarding
		LifecycleEvents: NewLifecycleEventRepositoryWithTx(tx),
		Onboardings:     NewOnboardingRepositoryWithTx(tx),

		// Admin Control Plane
		Clusters:            NewClusterRepositoryWithTx(tx),
		BareMetalHosts:      NewBareMetalHostRepositoryWithTx(tx),
		ManagedResources:    NewManagedResourceRepositoryWithTx(tx),
		VirtualClusters:     NewVirtualClusterRepositoryWithTx(tx),
		PropagationPolicies: NewPropagationPolicyRepositoryWithTx(tx),
		DriftEvents:         NewDriftEventRepositoryWithTx(tx),
		CostAllocations:     NewCostAllocationRepositoryWithTx(tx),

		// P3.2 Self-serve signup
		Signups: NewSignupRepositoryWithTx(tx),

		// Parity audit gap #2: namespace discoverer
		DiscoveredOrphans: NewDiscoveredOrphanRepository(tx),

		// XC-2 master-admin acting sessions
		AdminActingSessions: NewAdminActingSessionRepository(tx),
	}

	// Execute the function with transaction repositories
	if err := fn(txRepos); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx failed: %v, rollback failed: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// NewRepositories creates a new Repositories instance with all repositories initialized
func NewRepositories(db *sql.DB) *Repositories {
	return &Repositories{
		db:                   db,
		Projects:             NewProjectRepository(db),
		Environments:         NewEnvironmentRepository(db),
		Services:             NewServiceRepository(db),
		Releases:             NewReleaseRepository(db),
		Deployments:          NewDeploymentRepository(db),
		CanaryRollouts:       NewCanaryRolloutRepository(db),
		Users:                NewUserRepository(db),
		ProjectAccess:        NewProjectAccessRepository(db),
		AuditLogs:            NewAuditLogRepository(db),
		ApprovalRecords:      NewApprovalRecordRepository(db),
		RotationAuditLogs:    NewRotationAuditLogRepository(db),
		CustomDomains:        NewCustomDomainRepository(db),
		Routes:               NewRouteRepository(db),
		DeploymentGroups:     NewDeploymentGroupRepository(db),
		ServiceDependencies:  NewServiceDependencyRepository(db),
		EnvVars:              NewEnvVarRepository(db),
		PreviewEnvironments:  NewPreviewEnvironmentRepository(db),
		PreviewComments:      NewPreviewCommentRepository(db),
		PreviewAccessLogs:    NewPreviewAccessLogRepository(db),
		Teams:                NewTeamRepository(db),
		TeamMembers:          NewTeamMemberRepository(db),
		TeamInvitations:      NewTeamInvitationRepository(db),
		APITokens:            NewAPITokenRepository(db),
		DatabaseAddons:       NewDatabaseAddonRepository(db),
		ManagedDBPlans:       NewManagedDBPlanRepository(db),
		ManagedDBAddonEvents: NewManagedDBAddonEventRepository(db),
		Templates:            NewTemplateRepository(db),
		Webhooks:             NewWebhookRepository(db),
		OutboundWebhooks:     NewOutboundWebhookRepository(db),
		CIRuns:               NewCIRunRepository(db),
		Functions:            NewFunctionRepository(db),

		// Timetable
		CronJobs:    NewCronJobRepository(db),
		CronJobRuns: NewCronJobRunRepository(db),
		OneOffJobs:  NewOneOffJobRepository(db),

		// Junctions
		Junctions: NewJunctionRepository(db),

		// Deployment Lifecycle & Onboarding
		LifecycleEvents: NewLifecycleEventRepository(db),
		Onboardings:     NewOnboardingRepository(db),

		// Admin Control Plane
		Clusters:            NewClusterRepository(db),
		BareMetalHosts:      NewBareMetalHostRepository(db),
		ManagedResources:    NewManagedResourceRepository(db),
		VirtualClusters:     NewVirtualClusterRepository(db),
		PropagationPolicies: NewPropagationPolicyRepository(db),
		DriftEvents:         NewDriftEventRepository(db),
		CostAllocations:     NewCostAllocationRepository(db),

		// P3.6 Tenant export
		TenantExports: NewTenantExportRepository(db),

		// P3.2 Self-serve signup
		Signups: NewSignupRepository(db),

		// Parity audit gap #2: namespace discoverer
		DiscoveredOrphans: NewDiscoveredOrphanRepository(db),

		// XC-2 master-admin acting sessions
		AdminActingSessions: NewAdminActingSessionRepository(db),
	}
}
