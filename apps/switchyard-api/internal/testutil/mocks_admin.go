package testutil

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ============================================================================
// ADMIN CONTROL PLANE MOCKS
// ============================================================================

// MockClusterRepository is a mock implementation of ClusterRepositoryInterface
type MockClusterRepository struct {
	mu       sync.RWMutex
	clusters map[uuid.UUID]*types.Cluster
	CreateFn func(context.Context, *types.Cluster) error
	UpdateFn func(context.Context, *types.Cluster) error
	DeleteFn func(context.Context, uuid.UUID) error
}

func NewMockClusterRepository() *MockClusterRepository {
	return &MockClusterRepository{
		clusters: make(map[uuid.UUID]*types.Cluster),
	}
}

func (m *MockClusterRepository) Create(ctx context.Context, cluster *types.Cluster) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, cluster)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters[cluster.ID] = cluster
	return nil
}

func (m *MockClusterRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.Cluster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if c, ok := m.clusters[id]; ok {
		return c, nil
	}
	return nil, errors.ErrNotFound
}

func (m *MockClusterRepository) List(ctx context.Context) ([]*types.Cluster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		result = append(result, c)
	}
	return result, nil
}

func (m *MockClusterRepository) Update(ctx context.Context, cluster *types.Cluster) error {
	if m.UpdateFn != nil {
		return m.UpdateFn(ctx, cluster)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clusters[cluster.ID]; !ok {
		return errors.ErrNotFound
	}
	m.clusters[cluster.ID] = cluster
	return nil
}

func (m *MockClusterRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.clusters[id]; !ok {
		return errors.ErrNotFound
	}
	delete(m.clusters, id)
	return nil
}

// MockBareMetalHostRepository is a mock implementation of BareMetalHostRepositoryInterface
type MockBareMetalHostRepository struct {
	mu            sync.RWMutex
	hosts         map[uuid.UUID]*types.BareMetalHost
	CreateFn      func(context.Context, *types.BareMetalHost) error
	UpdateStateFn func(context.Context, uuid.UUID, types.BMHState, types.BMHPowerState) error
}

func NewMockBareMetalHostRepository() *MockBareMetalHostRepository {
	return &MockBareMetalHostRepository{
		hosts: make(map[uuid.UUID]*types.BareMetalHost),
	}
}

func (m *MockBareMetalHostRepository) Create(ctx context.Context, host *types.BareMetalHost) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, host)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hosts[host.ID] = host
	return nil
}

func (m *MockBareMetalHostRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.BareMetalHost, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if h, ok := m.hosts[id]; ok {
		return h, nil
	}
	return nil, errors.ErrNotFound
}

func (m *MockBareMetalHostRepository) List(ctx context.Context) ([]*types.BareMetalHost, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.BareMetalHost, 0, len(m.hosts))
	for _, h := range m.hosts {
		result = append(result, h)
	}
	return result, nil
}

func (m *MockBareMetalHostRepository) UpdateState(ctx context.Context, id uuid.UUID, state types.BMHState, powerState types.BMHPowerState) error {
	if m.UpdateStateFn != nil {
		return m.UpdateStateFn(ctx, id, state, powerState)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hosts[id]
	if !ok {
		return errors.ErrNotFound
	}
	h.State = state
	h.PowerState = powerState
	return nil
}

// MockManagedResourceRepository is a mock implementation of ManagedResourceRepositoryInterface
type MockManagedResourceRepository struct {
	mu                 sync.RWMutex
	resources          map[uuid.UUID]*types.ManagedResource
	CreateFn           func(context.Context, *types.ManagedResource) error
	UpdateSyncStatusFn func(context.Context, uuid.UUID, types.SyncStatus, []byte) error
	UpdatePolicyFn     func(context.Context, uuid.UUID, types.ManagementPolicy) error
	DeleteFn           func(context.Context, uuid.UUID) error
}

func NewMockManagedResourceRepository() *MockManagedResourceRepository {
	return &MockManagedResourceRepository{
		resources: make(map[uuid.UUID]*types.ManagedResource),
	}
}

func (m *MockManagedResourceRepository) Create(ctx context.Context, resource *types.ManagedResource) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, resource)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resources[resource.ID] = resource
	return nil
}

func (m *MockManagedResourceRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.ManagedResource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.resources[id]; ok {
		return r, nil
	}
	return nil, errors.ErrNotFound
}

func (m *MockManagedResourceRepository) List(ctx context.Context, provider, kind string, status types.SyncStatus) ([]*types.ManagedResource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.ManagedResource, 0)
	for _, r := range m.resources {
		if provider != "" && r.Provider != provider {
			continue
		}
		if kind != "" && r.Kind != kind {
			continue
		}
		if status != "" && r.SyncStatus != status {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

func (m *MockManagedResourceRepository) UpdateSyncStatus(ctx context.Context, id uuid.UUID, status types.SyncStatus, conditions []byte) error {
	if m.UpdateSyncStatusFn != nil {
		return m.UpdateSyncStatusFn(ctx, id, status, conditions)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.resources[id]
	if !ok {
		return errors.ErrNotFound
	}
	r.SyncStatus = status
	r.Conditions = conditions
	return nil
}

func (m *MockManagedResourceRepository) UpdatePolicy(ctx context.Context, id uuid.UUID, policy types.ManagementPolicy) error {
	if m.UpdatePolicyFn != nil {
		return m.UpdatePolicyFn(ctx, id, policy)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.resources[id]
	if !ok {
		return errors.ErrNotFound
	}
	r.ManagementPolicy = policy
	return nil
}

func (m *MockManagedResourceRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.resources[id]; !ok {
		return errors.ErrNotFound
	}
	delete(m.resources, id)
	return nil
}

// MockVirtualClusterRepository is a mock implementation of VirtualClusterRepositoryInterface
type MockVirtualClusterRepository struct {
	mu             sync.RWMutex
	vclusters      map[uuid.UUID]*types.VirtualCluster
	CreateFn       func(context.Context, *types.VirtualCluster) error
	UpdateStatusFn func(context.Context, uuid.UUID, types.VClusterStatus) error
	DeleteFn       func(context.Context, uuid.UUID) error
}

func NewMockVirtualClusterRepository() *MockVirtualClusterRepository {
	return &MockVirtualClusterRepository{
		vclusters: make(map[uuid.UUID]*types.VirtualCluster),
	}
}

func (m *MockVirtualClusterRepository) Create(ctx context.Context, vc *types.VirtualCluster) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, vc)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vclusters[vc.ID] = vc
	return nil
}

func (m *MockVirtualClusterRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.VirtualCluster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if vc, ok := m.vclusters[id]; ok {
		return vc, nil
	}
	return nil, errors.ErrNotFound
}

func (m *MockVirtualClusterRepository) List(ctx context.Context) ([]*types.VirtualCluster, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.VirtualCluster, 0, len(m.vclusters))
	for _, vc := range m.vclusters {
		result = append(result, vc)
	}
	return result, nil
}

func (m *MockVirtualClusterRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status types.VClusterStatus) error {
	if m.UpdateStatusFn != nil {
		return m.UpdateStatusFn(ctx, id, status)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	vc, ok := m.vclusters[id]
	if !ok {
		return errors.ErrNotFound
	}
	vc.Status = status
	return nil
}

func (m *MockVirtualClusterRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.vclusters[id]; !ok {
		return errors.ErrNotFound
	}
	delete(m.vclusters, id)
	return nil
}

// MockPropagationPolicyRepository is a mock implementation of PropagationPolicyRepositoryInterface
type MockPropagationPolicyRepository struct {
	mu       sync.RWMutex
	policies map[uuid.UUID]*types.PropagationPolicy
	CreateFn func(context.Context, *types.PropagationPolicy) error
	DeleteFn func(context.Context, uuid.UUID) error
}

func NewMockPropagationPolicyRepository() *MockPropagationPolicyRepository {
	return &MockPropagationPolicyRepository{
		policies: make(map[uuid.UUID]*types.PropagationPolicy),
	}
}

func (m *MockPropagationPolicyRepository) Create(ctx context.Context, policy *types.PropagationPolicy) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, policy)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies[policy.ID] = policy
	return nil
}

func (m *MockPropagationPolicyRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.PropagationPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.policies[id]; ok {
		return p, nil
	}
	return nil, errors.ErrNotFound
}

func (m *MockPropagationPolicyRepository) List(ctx context.Context) ([]*types.PropagationPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.PropagationPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		result = append(result, p)
	}
	return result, nil
}

func (m *MockPropagationPolicyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if m.DeleteFn != nil {
		return m.DeleteFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[id]; !ok {
		return errors.ErrNotFound
	}
	delete(m.policies, id)
	return nil
}

// MockDriftEventRepository is a mock implementation of DriftEventRepositoryInterface
type MockDriftEventRepository struct {
	mu        sync.RWMutex
	events    map[uuid.UUID]*types.DriftEvent
	CreateFn  func(context.Context, *types.DriftEvent) error
	ResolveFn func(context.Context, uuid.UUID) error
}

func NewMockDriftEventRepository() *MockDriftEventRepository {
	return &MockDriftEventRepository{
		events: make(map[uuid.UUID]*types.DriftEvent),
	}
}

func (m *MockDriftEventRepository) Create(ctx context.Context, event *types.DriftEvent) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, event)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[event.ID] = event
	return nil
}

func (m *MockDriftEventRepository) GetByID(ctx context.Context, id uuid.UUID) (*types.DriftEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.events[id]; ok {
		return e, nil
	}
	return nil, errors.ErrNotFound
}

func (m *MockDriftEventRepository) List(ctx context.Context, resolved *bool) ([]*types.DriftEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.DriftEvent, 0)
	for _, e := range m.events {
		if resolved != nil && e.Resolved != *resolved {
			continue
		}
		result = append(result, e)
	}
	return result, nil
}

func (m *MockDriftEventRepository) Resolve(ctx context.Context, id uuid.UUID) error {
	if m.ResolveFn != nil {
		return m.ResolveFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.events[id]
	if !ok {
		return errors.ErrNotFound
	}
	e.Resolved = true
	now := time.Now()
	e.ResolvedAt = &now
	return nil
}

// MockCostAllocationRepository is a mock implementation of CostAllocationRepositoryInterface
type MockCostAllocationRepository struct {
	mu          sync.RWMutex
	allocations map[uuid.UUID]*types.CostAllocation
	CreateFn    func(context.Context, *types.CostAllocation) error
}

func NewMockCostAllocationRepository() *MockCostAllocationRepository {
	return &MockCostAllocationRepository{
		allocations: make(map[uuid.UUID]*types.CostAllocation),
	}
}

func (m *MockCostAllocationRepository) Create(ctx context.Context, allocation *types.CostAllocation) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, allocation)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allocations[allocation.ID] = allocation
	return nil
}

func (m *MockCostAllocationRepository) ListByTenant(ctx context.Context, tenantID string, start, end time.Time) ([]*types.CostAllocation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.CostAllocation, 0)
	for _, a := range m.allocations {
		if a.TenantID == tenantID && !a.PeriodStart.Before(start) && !a.PeriodEnd.After(end) {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *MockCostAllocationRepository) ListByHost(ctx context.Context, hostID uuid.UUID, start, end time.Time) ([]*types.CostAllocation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.CostAllocation, 0)
	for _, a := range m.allocations {
		if a.BareMetalHostID == hostID && !a.PeriodStart.Before(start) && !a.PeriodEnd.After(end) {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *MockCostAllocationRepository) GetSummary(ctx context.Context, start, end time.Time) ([]*types.CostAllocation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.CostAllocation, 0)
	for _, a := range m.allocations {
		if !a.PeriodStart.Before(start) && !a.PeriodEnd.After(end) {
			result = append(result, a)
		}
	}
	return result, nil
}

// MockAuditLogRepository is a mock implementation of AuditLogRepositoryInterface
type MockAuditLogRepository struct {
	mu       sync.RWMutex
	logs     map[uuid.UUID]*types.AuditLog
	ordered  []*types.AuditLog
	CreateFn func(context.Context, *types.AuditLog) error
}

func NewMockAuditLogRepository() *MockAuditLogRepository {
	return &MockAuditLogRepository{
		logs:    make(map[uuid.UUID]*types.AuditLog),
		ordered: make([]*types.AuditLog, 0),
	}
}

func (m *MockAuditLogRepository) Create(ctx context.Context, log *types.AuditLog) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, log)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs[log.ID] = log
	m.ordered = append(m.ordered, log)
	return nil
}

func (m *MockAuditLogRepository) ListByActor(ctx context.Context, actorID uuid.UUID, limit int) ([]*types.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.AuditLog, 0)
	for _, l := range m.ordered {
		if l.ActorID != nil && *l.ActorID == actorID {
			result = append(result, l)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *MockAuditLogRepository) ListByResource(ctx context.Context, resourceType, resourceID string, limit int) ([]*types.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.AuditLog, 0)
	for _, l := range m.ordered {
		if l.ResourceType == resourceType && l.ResourceID == resourceID {
			result = append(result, l)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *MockAuditLogRepository) ListRecent(ctx context.Context, limit int) ([]*types.AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := len(m.ordered)
	if limit > 0 && limit < count {
		count = limit
	}
	result := make([]*types.AuditLog, 0, count)
	for i := len(m.ordered) - 1; i >= 0 && len(result) < count; i-- {
		result = append(result, m.ordered[i])
	}
	return result, nil
}

// MockApprovalRecordRepository is a mock implementation of ApprovalRecordRepositoryInterface
type MockApprovalRecordRepository struct {
	mu       sync.RWMutex
	records  map[uuid.UUID]*types.ApprovalRecord
	CreateFn func(context.Context, *types.ApprovalRecord) error
}

func NewMockApprovalRecordRepository() *MockApprovalRecordRepository {
	return &MockApprovalRecordRepository{
		records: make(map[uuid.UUID]*types.ApprovalRecord),
	}
}

func (m *MockApprovalRecordRepository) Create(ctx context.Context, record *types.ApprovalRecord) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, record)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[record.ID] = record
	return nil
}

func (m *MockApprovalRecordRepository) GetByDeployment(ctx context.Context, deploymentID uuid.UUID) (*types.ApprovalRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.records {
		if r.DeploymentID == deploymentID {
			return r, nil
		}
	}
	return nil, errors.ErrNotFound
}

func (m *MockApprovalRecordRepository) ListByService(ctx context.Context, serviceID uuid.UUID, limit int) ([]*types.ApprovalRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*types.ApprovalRecord, 0)
	for _, r := range m.records {
		result = append(result, r)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// MockRotationAuditLogRepository is a mock implementation of RotationAuditLogRepositoryInterface
type MockRotationAuditLogRepository struct {
	mu       sync.RWMutex
	logs     map[uuid.UUID]interface{}
	ordered  []interface{}
	CreateFn func(context.Context, interface{}) error
}

func NewMockRotationAuditLogRepository() *MockRotationAuditLogRepository {
	return &MockRotationAuditLogRepository{
		logs:    make(map[uuid.UUID]interface{}),
		ordered: make([]interface{}, 0),
	}
}

func (m *MockRotationAuditLogRepository) Create(ctx context.Context, log interface{}) error {
	if m.CreateFn != nil {
		return m.CreateFn(ctx, log)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	m.logs[id] = log
	m.ordered = append(m.ordered, log)
	return nil
}

func (m *MockRotationAuditLogRepository) GetByServiceID(ctx context.Context, serviceID uuid.UUID, limit int) ([]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := len(m.ordered)
	if limit > 0 && limit < count {
		count = limit
	}
	result := make([]interface{}, count)
	copy(result, m.ordered[:count])
	return result, nil
}

func (m *MockRotationAuditLogRepository) GetByEventID(ctx context.Context, eventID uuid.UUID) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if l, ok := m.logs[eventID]; ok {
		return l, nil
	}
	return nil, errors.ErrNotFound
}

// ============================================================================
// AGGREGATE MOCK STRUCT
// ============================================================================

// MockRepositories holds all mock repository implementations for testing
type MockRepositories struct {
	Projects            *MockProjectRepository
	Services            *MockServiceRepository
	Users               *MockUserRepository
	Releases            *MockReleaseRepository
	Deployments         *MockDeploymentRepository
	Environments        *MockEnvironmentRepository
	ProjectAccess       *MockProjectAccessRepository
	Clusters            *MockClusterRepository
	BareMetalHosts      *MockBareMetalHostRepository
	ManagedResources    *MockManagedResourceRepository
	VirtualClusters     *MockVirtualClusterRepository
	PropagationPolicies *MockPropagationPolicyRepository
	DriftEvents         *MockDriftEventRepository
	CostAllocations     *MockCostAllocationRepository
	AuditLogs           *MockAuditLogRepository
	ApprovalRecords     *MockApprovalRecordRepository
	RotationAuditLogs   *MockRotationAuditLogRepository
}

// NewMockRepositories creates a new set of mock repositories for testing
func NewMockRepositories() *MockRepositories {
	return &MockRepositories{
		Projects:            NewMockProjectRepository(),
		Services:            NewMockServiceRepository(),
		Users:               NewMockUserRepository(),
		Releases:            NewMockReleaseRepository(),
		Deployments:         NewMockDeploymentRepository(),
		Environments:        NewMockEnvironmentRepository(),
		ProjectAccess:       NewMockProjectAccessRepository(),
		Clusters:            NewMockClusterRepository(),
		BareMetalHosts:      NewMockBareMetalHostRepository(),
		ManagedResources:    NewMockManagedResourceRepository(),
		VirtualClusters:     NewMockVirtualClusterRepository(),
		PropagationPolicies: NewMockPropagationPolicyRepository(),
		DriftEvents:         NewMockDriftEventRepository(),
		CostAllocations:     NewMockCostAllocationRepository(),
		AuditLogs:           NewMockAuditLogRepository(),
		ApprovalRecords:     NewMockApprovalRecordRepository(),
		RotationAuditLogs:   NewMockRotationAuditLogRepository(),
	}
}

// ErrNotFound is a sentinel error for not found
var ErrNotFound = sql.ErrNoRows
