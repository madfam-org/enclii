package db

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DBTX is an interface that both *sql.DB and *sql.Tx satisfy.
// This allows repositories to work with either a direct database connection
// or within a transaction context.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	// Note: Exec, Query, QueryRow without context are also available on both
	// but we prefer context-aware versions for production use
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// Ensure *sql.DB and *sql.Tx implement DBTX at compile time
var (
	_ DBTX = (*sql.DB)(nil)
	_ DBTX = (*sql.Tx)(nil)
)

// Repository interfaces define standard CRUD operations for each entity

// ProjectRepositoryInterface defines operations for projects
type ProjectRepositoryInterface interface {
	Create(project *types.Project) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.Project, error)
	GetBySlug(slug string) (*types.Project, error)
	List() ([]*types.Project, error)
}

// EnvironmentRepositoryInterface defines operations for environments
type EnvironmentRepositoryInterface interface {
	Create(env *types.Environment) error
	UpdateKubeNamespace(ctx context.Context, id uuid.UUID, namespace string) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.Environment, error)
	GetByProjectAndName(projectID uuid.UUID, name string) (*types.Environment, error)
	ListByProject(projectID uuid.UUID) ([]*types.Environment, error)
}

// ServiceRepositoryInterface defines operations for services
type ServiceRepositoryInterface interface {
	Create(service *types.Service) error
	GetByID(id uuid.UUID) (*types.Service, error)
	ListAll(ctx context.Context) ([]*types.Service, error)
	ListByProject(projectID uuid.UUID) ([]*types.Service, error)
	Update(ctx context.Context, service *types.Service) error
	Delete(ctx context.Context, id uuid.UUID) error
	UpdateHealthStatus(ctx context.Context, id uuid.UUID, health types.HealthStatus, status string, desiredReplicas, readyReplicas int32, rolloutBlockedReason string) error
}

// ReleaseRepositoryInterface defines operations for releases
type ReleaseRepositoryInterface interface {
	Create(release *types.Release) error
	GetByID(id uuid.UUID) (*types.Release, error)
	UpdateStatus(id uuid.UUID, status types.ReleaseStatus) error
	UpdateStatusWithError(id uuid.UUID, status types.ReleaseStatus, errorMsg *string) error
	UpdateImageURI(id uuid.UUID, imageURI string) error
	UpdateSBOM(ctx context.Context, id uuid.UUID, sbom, sbomFormat string) error
	UpdateSignature(ctx context.Context, id uuid.UUID, signature string) error
	CleanupAllStaleBuilding(ctx context.Context, maxAge time.Duration) (int64, error)
	ListByService(serviceID uuid.UUID) ([]*types.Release, error)
}

// DeploymentRepositoryInterface defines operations for deployments
type DeploymentRepositoryInterface interface {
	Create(deployment *types.Deployment) error
	GetByID(ctx context.Context, id string) (*types.Deployment, error)
	GetByServiceAndVersion(ctx context.Context, serviceID uuid.UUID, version int) (*types.Deployment, error)
	UpdateStatus(id uuid.UUID, status types.DeploymentStatus, health types.HealthStatus) error
	ListByRelease(ctx context.Context, releaseID string) ([]*types.Deployment, error)
	GetLatestByService(ctx context.Context, serviceID string) (*types.Deployment, error)
	GetByStatus(ctx context.Context, status types.DeploymentStatus) ([]*types.Deployment, error)
	GetByServiceSince(ctx context.Context, serviceID string, since time.Time) ([]*types.Deployment, error)
}

// UserRepositoryInterface defines operations for users
type UserRepositoryInterface interface {
	Create(user *types.User) error
	GetByID(id uuid.UUID) (*types.User, error)
	GetByEmail(email string) (*types.User, error)
	Update(user *types.User) error
}

// ProjectAccessRepositoryInterface defines operations for project access control
type ProjectAccessRepositoryInterface interface {
	Grant(access *types.ProjectAccess) error
	Revoke(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID) error
	GetByUserAndProject(ctx context.Context, userID, projectID uuid.UUID) ([]*types.ProjectAccess, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*types.ProjectAccess, error)
	HasAccess(ctx context.Context, userID, projectID uuid.UUID, environmentID *uuid.UUID, requiredRole types.Role) (bool, error)
}

// AuditLogRepositoryInterface defines operations for audit logs
type AuditLogRepositoryInterface interface {
	Create(ctx context.Context, log *types.AuditLog) error
	ListByActor(ctx context.Context, actorID uuid.UUID, limit int) ([]*types.AuditLog, error)
	ListByResource(ctx context.Context, resourceType, resourceID string, limit int) ([]*types.AuditLog, error)
	ListRecent(ctx context.Context, limit int) ([]*types.AuditLog, error)
}

// ApprovalRecordRepositoryInterface defines operations for approval records
type ApprovalRecordRepositoryInterface interface {
	Create(ctx context.Context, record *types.ApprovalRecord) error
	GetByDeployment(ctx context.Context, deploymentID uuid.UUID) (*types.ApprovalRecord, error)
	ListByService(ctx context.Context, serviceID uuid.UUID, limit int) ([]*types.ApprovalRecord, error)
}

// RotationAuditLogRepositoryInterface defines operations for rotation audit logs
type RotationAuditLogRepositoryInterface interface {
	Create(ctx context.Context, log interface{}) error
	GetByServiceID(ctx context.Context, serviceID uuid.UUID, limit int) ([]interface{}, error)
	GetByEventID(ctx context.Context, eventID uuid.UUID) (interface{}, error)
}

// RepositoryProvider provides access to all repositories
type RepositoryProvider interface {
	Projects() ProjectRepositoryInterface
	Environments() EnvironmentRepositoryInterface
	Services() ServiceRepositoryInterface
	Releases() ReleaseRepositoryInterface
	Deployments() DeploymentRepositoryInterface
	Users() UserRepositoryInterface
	ProjectAccess() ProjectAccessRepositoryInterface
	AuditLogs() AuditLogRepositoryInterface
	ApprovalRecords() ApprovalRecordRepositoryInterface
	RotationAuditLogs() RotationAuditLogRepositoryInterface
}

// ClusterRepositoryInterface defines operations for clusters
type ClusterRepositoryInterface interface {
	Create(ctx context.Context, cluster *types.Cluster) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.Cluster, error)
	List(ctx context.Context) ([]*types.Cluster, error)
	Update(ctx context.Context, cluster *types.Cluster) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// BareMetalHostRepositoryInterface defines operations for bare metal hosts
type BareMetalHostRepositoryInterface interface {
	Create(ctx context.Context, host *types.BareMetalHost) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.BareMetalHost, error)
	List(ctx context.Context) ([]*types.BareMetalHost, error)
	UpdateState(ctx context.Context, id uuid.UUID, state types.BMHState, powerState types.BMHPowerState) error
}

// ManagedResourceRepositoryInterface defines operations for managed resources
type ManagedResourceRepositoryInterface interface {
	Create(ctx context.Context, resource *types.ManagedResource) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.ManagedResource, error)
	List(ctx context.Context, provider, kind string, status types.SyncStatus) ([]*types.ManagedResource, error)
	UpdateSyncStatus(ctx context.Context, id uuid.UUID, status types.SyncStatus, conditions []byte) error
	UpdatePolicy(ctx context.Context, id uuid.UUID, policy types.ManagementPolicy) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// VirtualClusterRepositoryInterface defines operations for virtual clusters
type VirtualClusterRepositoryInterface interface {
	Create(ctx context.Context, vc *types.VirtualCluster) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.VirtualCluster, error)
	List(ctx context.Context) ([]*types.VirtualCluster, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status types.VClusterStatus) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// PropagationPolicyRepositoryInterface defines operations for propagation policies
type PropagationPolicyRepositoryInterface interface {
	Create(ctx context.Context, policy *types.PropagationPolicy) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.PropagationPolicy, error)
	List(ctx context.Context) ([]*types.PropagationPolicy, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// DriftEventRepositoryInterface defines operations for drift events
type DriftEventRepositoryInterface interface {
	Create(ctx context.Context, event *types.DriftEvent) error
	GetByID(ctx context.Context, id uuid.UUID) (*types.DriftEvent, error)
	List(ctx context.Context, resolved *bool) ([]*types.DriftEvent, error)
	Resolve(ctx context.Context, id uuid.UUID) error
}

// CostAllocationRepositoryInterface defines operations for cost allocations
type CostAllocationRepositoryInterface interface {
	Create(ctx context.Context, allocation *types.CostAllocation) error
	ListByTenant(ctx context.Context, tenantID string, start, end time.Time) ([]*types.CostAllocation, error)
	ListByHost(ctx context.Context, hostID uuid.UUID, start, end time.Time) ([]*types.CostAllocation, error)
	GetSummary(ctx context.Context, start, end time.Time) ([]*types.CostAllocation, error)
}

// Note: RepositoryProvider interface exists for documentation and potential future use,
// but Repositories struct already exposes all repositories as public fields, so no
// accessor methods are needed (they would conflict with the field names anyway).
