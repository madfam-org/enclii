package topology

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newService creates a minimal *types.Service with the given name and git repo.
func newService(name, gitRepo string) *types.Service {
	return &types.Service{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      name,
		GitRepo:   gitRepo,
		UpdatedAt: time.Now(),
	}
}

// newServiceWithProject creates a *types.Service tied to a specific project ID.
func newServiceWithProject(name string, projectID uuid.UUID) *types.Service {
	return &types.Service{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      name,
		UpdatedAt: time.Now(),
	}
}

// newNode creates a ServiceNode with the minimum fields needed for stats tests.
func newNode(id, projectID, projectName string, svcType ServiceType, status HealthStatus) *ServiceNode {
	return &ServiceNode{
		ID:          id,
		ProjectID:   projectID,
		ProjectName: projectName,
		Type:        svcType,
		Status:      status,
	}
}

// newEdge creates a DependencyEdge.
func newEdge(sourceID, targetID string, depType DependencyType, protocol string, required bool) *DependencyEdge {
	return &DependencyEdge{
		ID:        fmt.Sprintf("%s-%s", sourceID, targetID),
		SourceID:  sourceID,
		TargetID:  targetID,
		Type:      depType,
		Protocol:  protocol,
		Required:  required,
		Metadata:  make(map[string]string),
		CreatedAt: time.Now().UTC(),
	}
}

// zeroBuilder returns a GraphBuilder with nil dependencies, usable for methods
// that do not access repos or k8sClient (calculateStats, detectDependencies).
func zeroBuilder() *GraphBuilder {
	return &GraphBuilder{}
}

// ---------------------------------------------------------------------------
// Test: Type constants
// ---------------------------------------------------------------------------

func TestServiceTypeConstants(t *testing.T) {
	tests := []struct {
		constant ServiceType
		expected string
	}{
		{ServiceTypeHTTP, "http"},
		{ServiceTypeGRPC, "grpc"},
		{ServiceTypeDatabase, "database"},
		{ServiceTypeQueue, "queue"},
		{ServiceTypeCache, "cache"},
		{ServiceTypeStorage, "storage"},
		{ServiceTypeUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, ServiceType(tt.expected), tt.constant)
		})
	}
}

func TestHealthStatusConstants(t *testing.T) {
	tests := []struct {
		constant HealthStatus
		expected string
	}{
		{HealthStatusHealthy, "healthy"},
		{HealthStatusDegraded, "degraded"},
		{HealthStatusUnhealthy, "unhealthy"},
		{HealthStatusUnknown, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, HealthStatus(tt.expected), tt.constant)
		})
	}
}

func TestDependencyTypeConstants(t *testing.T) {
	tests := []struct {
		constant DependencyType
		expected string
	}{
		{DependencyTypeSync, "sync"},
		{DependencyTypeAsync, "async"},
		{DependencyTypeStorage, "storage"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, DependencyType(tt.expected), tt.constant)
		})
	}
}

func TestImpactSeverityConstants(t *testing.T) {
	tests := []struct {
		constant ImpactSeverity
		expected string
	}{
		{ImpactSeverityLow, "low"},
		{ImpactSeverityModerate, "moderate"},
		{ImpactSeverityHigh, "high"},
		{ImpactSeverityCritical, "critical"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, ImpactSeverity(tt.expected), tt.constant)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: detectServiceType (pure function, 13 cases)
// ---------------------------------------------------------------------------

func TestDetectServiceType(t *testing.T) {
	tests := []struct {
		name     string
		service  *types.Service
		expected ServiceType
	}{
		{
			name:     "name contains postgres",
			service:  newService("my-postgres", ""),
			expected: ServiceTypeDatabase,
		},
		{
			name:     "name contains mysql",
			service:  newService("mysql-primary", ""),
			expected: ServiceTypeDatabase,
		},
		{
			name:     "name contains database",
			service:  newService("user-database", ""),
			expected: ServiceTypeDatabase,
		},
		{
			name:     "name contains redis",
			service:  newService("redis-cluster", ""),
			expected: ServiceTypeCache,
		},
		{
			name:     "name contains cache",
			service:  newService("session-cache", ""),
			expected: ServiceTypeCache,
		},
		{
			name:     "name contains queue",
			service:  newService("job-queue", ""),
			expected: ServiceTypeQueue,
		},
		{
			name:     "name contains kafka",
			service:  newService("events-kafka", ""),
			expected: ServiceTypeQueue,
		},
		{
			name:     "name contains rabbitmq",
			service:  newService("rabbitmq-broker", ""),
			expected: ServiceTypeQueue,
		},
		{
			name:     "git repo contains grpc",
			service:  newService("payment-service", "github.com/org/grpc-gateway"),
			expected: ServiceTypeGRPC,
		},
		{
			name:     "name contains grpc",
			service:  newService("user-grpc", ""),
			expected: ServiceTypeGRPC,
		},
		{
			name:     "default to HTTP for generic name",
			service:  newService("my-api", "github.com/org/my-api"),
			expected: ServiceTypeHTTP,
		},
		{
			name:     "case insensitivity for uppercase PostgreSQL",
			service:  newService("PostgreSQL-Main", ""),
			expected: ServiceTypeDatabase,
		},
		{
			name:     "empty name defaults to HTTP",
			service:  newService("", ""),
			expected: ServiceTypeHTTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectServiceType(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: calculateStats (10 cases)
// ---------------------------------------------------------------------------

func TestCalculateStats_EmptyInputs(t *testing.T) {
	stats := zeroBuilder().calculateStats(nil, nil)

	require.NotNil(t, stats)
	assert.Equal(t, 0, stats.TotalServices)
	assert.Equal(t, 0, stats.TotalDependencies)
	assert.Equal(t, 0, stats.HealthyServices)
	assert.Equal(t, 0, stats.DegradedServices)
	assert.Equal(t, 0, stats.UnhealthyServices)
	assert.Empty(t, stats.ServicesByType)
	assert.Empty(t, stats.HealthByProject)
}

func TestCalculateStats_AllHealthy(t *testing.T) {
	projID := "proj-1"
	nodes := []*ServiceNode{
		newNode("svc-1", projID, "Project Alpha", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-2", projID, "Project Alpha", ServiceTypeHTTP, HealthStatusHealthy),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	assert.Equal(t, 2, stats.TotalServices)
	assert.Equal(t, 2, stats.HealthyServices)
	assert.Equal(t, 0, stats.DegradedServices)
	assert.Equal(t, 0, stats.UnhealthyServices)
	assert.Equal(t, 0, stats.TotalDependencies)
}

func TestCalculateStats_MixedHealth(t *testing.T) {
	projID := "proj-1"
	nodes := []*ServiceNode{
		newNode("svc-1", projID, "Mixed Project", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-2", projID, "Mixed Project", ServiceTypeDatabase, HealthStatusDegraded),
		newNode("svc-3", projID, "Mixed Project", ServiceTypeCache, HealthStatusUnhealthy),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	assert.Equal(t, 3, stats.TotalServices)
	assert.Equal(t, 1, stats.HealthyServices)
	assert.Equal(t, 1, stats.DegradedServices)
	assert.Equal(t, 1, stats.UnhealthyServices)
}

func TestCalculateStats_UnknownHealthNotCounted(t *testing.T) {
	// HealthStatusUnknown does not increment healthy/degraded/unhealthy counters.
	nodes := []*ServiceNode{
		newNode("svc-1", "proj-1", "P", ServiceTypeHTTP, HealthStatusUnknown),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	assert.Equal(t, 1, stats.TotalServices)
	assert.Equal(t, 0, stats.HealthyServices)
	assert.Equal(t, 0, stats.DegradedServices)
	assert.Equal(t, 0, stats.UnhealthyServices)
}

func TestCalculateStats_ServicesByType(t *testing.T) {
	nodes := []*ServiceNode{
		newNode("svc-1", "p1", "P", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-2", "p1", "P", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-3", "p1", "P", ServiceTypeDatabase, HealthStatusHealthy),
		newNode("svc-4", "p1", "P", ServiceTypeGRPC, HealthStatusHealthy),
		newNode("svc-5", "p1", "P", ServiceTypeCache, HealthStatusHealthy),
		newNode("svc-6", "p1", "P", ServiceTypeQueue, HealthStatusHealthy),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	assert.Equal(t, 2, stats.ServicesByType[ServiceTypeHTTP])
	assert.Equal(t, 1, stats.ServicesByType[ServiceTypeDatabase])
	assert.Equal(t, 1, stats.ServicesByType[ServiceTypeGRPC])
	assert.Equal(t, 1, stats.ServicesByType[ServiceTypeCache])
	assert.Equal(t, 1, stats.ServicesByType[ServiceTypeQueue])
}

func TestCalculateStats_DependencyCount(t *testing.T) {
	nodes := []*ServiceNode{
		newNode("svc-1", "p1", "P", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-2", "p1", "P", ServiceTypeDatabase, HealthStatusHealthy),
	}
	edges := []*DependencyEdge{
		newEdge("svc-1", "svc-2", DependencyTypeStorage, "postgres", true),
	}

	stats := zeroBuilder().calculateStats(nodes, edges)

	assert.Equal(t, 1, stats.TotalDependencies)
}

func TestCalculateStats_ProjectHealthAggregation(t *testing.T) {
	projA := "proj-a"
	projB := "proj-b"
	nodes := []*ServiceNode{
		newNode("svc-1", projA, "Alpha", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-2", projA, "Alpha", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-3", projB, "Beta", ServiceTypeHTTP, HealthStatusUnhealthy),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	require.Contains(t, stats.HealthByProject, projA)
	require.Contains(t, stats.HealthByProject, projB)

	alphaHealth := stats.HealthByProject[projA]
	assert.Equal(t, 2, alphaHealth.TotalServices)
	assert.Equal(t, 2, alphaHealth.HealthyServices)
	assert.Equal(t, 0, alphaHealth.UnhealthyServices)
	assert.InDelta(t, 1.0, alphaHealth.HealthScore, 0.001)

	betaHealth := stats.HealthByProject[projB]
	assert.Equal(t, 1, betaHealth.TotalServices)
	assert.Equal(t, 0, betaHealth.HealthyServices)
	assert.Equal(t, 1, betaHealth.UnhealthyServices)
	assert.InDelta(t, 0.0, betaHealth.HealthScore, 0.001)
}

func TestCalculateStats_HealthScorePartialHealthy(t *testing.T) {
	projID := "proj-1"
	nodes := []*ServiceNode{
		newNode("svc-1", projID, "P", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-2", projID, "P", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("svc-3", projID, "P", ServiceTypeHTTP, HealthStatusDegraded),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	ph := stats.HealthByProject[projID]
	// 2 healthy out of 3 total = 0.666...
	assert.InDelta(t, 2.0/3.0, ph.HealthScore, 0.001)
}

func TestCalculateStats_ProjectHealthName(t *testing.T) {
	projID := "proj-xyz"
	nodes := []*ServiceNode{
		newNode("svc-1", projID, "My Custom Project", ServiceTypeHTTP, HealthStatusHealthy),
	}

	stats := zeroBuilder().calculateStats(nodes, nil)

	ph := stats.HealthByProject[projID]
	assert.Equal(t, projID, ph.ProjectID)
	assert.Equal(t, "My Custom Project", ph.ProjectName)
}

func TestCalculateStats_MultipleEdgesMultipleNodes(t *testing.T) {
	nodes := []*ServiceNode{
		newNode("api", "p1", "P", ServiceTypeHTTP, HealthStatusHealthy),
		newNode("db", "p1", "P", ServiceTypeDatabase, HealthStatusDegraded),
		newNode("cache", "p1", "P", ServiceTypeCache, HealthStatusHealthy),
	}
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
		newEdge("api", "cache", DependencyTypeStorage, "redis", false),
	}

	stats := zeroBuilder().calculateStats(nodes, edges)

	assert.Equal(t, 3, stats.TotalServices)
	assert.Equal(t, 2, stats.TotalDependencies)
	assert.Equal(t, 2, stats.HealthyServices)
	assert.Equal(t, 1, stats.DegradedServices)
}

// ---------------------------------------------------------------------------
// Test: detectDependencies (method, but does not access repos/k8s)
// ---------------------------------------------------------------------------

func TestDetectDependencies_APIDependsOnDatabase(t *testing.T) {
	apiSvc := newService("switchyard-api", "")
	dbSvc := newService("user-database", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{apiSvc, dbSvc})

	require.Len(t, edges, 1)
	assert.Equal(t, apiSvc.ID.String(), edges[0].SourceID)
	assert.Equal(t, dbSvc.ID.String(), edges[0].TargetID)
	assert.Equal(t, DependencyTypeStorage, edges[0].Type)
	assert.Equal(t, "postgres", edges[0].Protocol)
	assert.True(t, edges[0].Required)
}

func TestDetectDependencies_APIDependsOnPostgres(t *testing.T) {
	apiSvc := newService("my-api", "")
	pgSvc := newService("my-postgres", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{apiSvc, pgSvc})

	require.Len(t, edges, 1)
	assert.Equal(t, "postgres", edges[0].Protocol)
	assert.True(t, edges[0].Required)
}

func TestDetectDependencies_APIDependsOnCache(t *testing.T) {
	apiSvc := newService("my-api", "")
	cacheSvc := newService("session-cache", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{apiSvc, cacheSvc})

	require.Len(t, edges, 1)
	assert.Equal(t, "redis", edges[0].Protocol)
	assert.False(t, edges[0].Required)
}

func TestDetectDependencies_BackendDependsOnDatabase(t *testing.T) {
	backendSvc := newService("my-backend", "")
	dbSvc := newService("main-database", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), backendSvc, []*types.Service{backendSvc, dbSvc})

	require.Len(t, edges, 1)
	assert.Equal(t, DependencyTypeStorage, edges[0].Type)
}

func TestDetectDependencies_SkipsSelfDependency(t *testing.T) {
	apiSvc := newService("api-database", "") // contains both "api" and "database"

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{apiSvc})

	assert.Empty(t, edges, "service should not depend on itself")
}

func TestDetectDependencies_NoDependencyForUnrelatedServices(t *testing.T) {
	uiSvc := newService("web-ui", "")
	workerSvc := newService("background-worker", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), uiSvc, []*types.Service{uiSvc, workerSvc})

	assert.Empty(t, edges, "unrelated services should produce no edges")
}

func TestDetectDependencies_APIDependsOnBothDatabaseAndCache(t *testing.T) {
	apiSvc := newService("main-api", "")
	dbSvc := newService("core-database", "")
	cacheSvc := newService("api-cache", "")

	b := zeroBuilder()
	all := []*types.Service{apiSvc, dbSvc, cacheSvc}
	edges := b.detectDependencies(context.Background(), apiSvc, all)

	require.Len(t, edges, 2, "API should depend on both database and cache")

	protocols := map[string]bool{}
	for _, e := range edges {
		protocols[e.Protocol] = true
	}
	assert.True(t, protocols["postgres"])
	assert.True(t, protocols["redis"])
}

func TestDetectDependencies_EdgeIDFormat(t *testing.T) {
	apiSvc := newService("my-api", "")
	dbSvc := newService("my-database", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{apiSvc, dbSvc})

	require.Len(t, edges, 1)
	expectedID := fmt.Sprintf("%s-%s", apiSvc.ID.String(), dbSvc.ID.String())
	assert.Equal(t, expectedID, edges[0].ID)
}

func TestDetectDependencies_EdgeMetadataInitialized(t *testing.T) {
	apiSvc := newService("my-api", "")
	dbSvc := newService("app-database", "")

	b := zeroBuilder()
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{apiSvc, dbSvc})

	require.Len(t, edges, 1)
	assert.NotNil(t, edges[0].Metadata, "metadata map should be initialized, not nil")
}

func TestDetectDependencies_EmptyServiceList(t *testing.T) {
	apiSvc := newService("my-api", "")

	b := zeroBuilder()
	// Pass an empty slice as allServices -- the service itself is the only candidate
	// and it should be skipped (self-reference).
	edges := b.detectDependencies(context.Background(), apiSvc, []*types.Service{})

	assert.Empty(t, edges)
}

// ---------------------------------------------------------------------------
// Test: Graph algorithm helpers (BFS impact, BFS path)
//
// Since AnalyzeImpact and FindPath both call BuildTopology (which needs repos),
// we test the core BFS algorithms by constructing TopologyGraph data directly
// and running the algorithm logic extracted into helper functions.
// ---------------------------------------------------------------------------

// impactBFS replicates the BFS traversal from AnalyzeImpact on a pre-built graph.
func impactBFS(edges []*DependencyEdge, serviceID string) (directDependents, indirectDependents []string, severity ImpactSeverity) {
	// Build reverse adjacency list (target -> sources that depend on it)
	adjacency := make(map[string][]string)
	for _, edge := range edges {
		adjacency[edge.TargetID] = append(adjacency[edge.TargetID], edge.SourceID)
	}

	directDependents = adjacency[serviceID]

	visited := make(map[string]bool)
	queue := make([]string, len(directDependents))
	copy(queue, directDependents)

	for _, dep := range directDependents {
		visited[dep] = true
	}

	indirectDependents = make([]string, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, dependent := range adjacency[current] {
			if !visited[dependent] {
				visited[dependent] = true
				indirectDependents = append(indirectDependents, dependent)
				queue = append(queue, dependent)
			}
		}
	}

	totalImpact := len(directDependents) + len(indirectDependents)
	switch {
	case totalImpact <= 2:
		severity = ImpactSeverityLow
	case totalImpact <= 5:
		severity = ImpactSeverityModerate
	case totalImpact <= 10:
		severity = ImpactSeverityHigh
	default:
		severity = ImpactSeverityCritical
	}

	return directDependents, indirectDependents, severity
}

// findPathBFS replicates the BFS path-finding from FindPath on a pre-built graph.
func findPathBFS(edges []*DependencyEdge, sourceID, targetID string) (*DependencyPath, error) {
	adjacency := make(map[string][]string)
	for _, edge := range edges {
		adjacency[edge.SourceID] = append(adjacency[edge.SourceID], edge.TargetID)
	}

	queue := [][]string{{sourceID}}
	visited := make(map[string]bool)
	visited[sourceID] = true

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]

		current := path[len(path)-1]

		if current == targetID {
			return &DependencyPath{
				Source:   sourceID,
				Target:   targetID,
				Path:     path,
				Distance: len(path) - 1,
				Type:     "downstream",
			}, nil
		}

		for _, neighbor := range adjacency[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				newPath := make([]string, len(path)+1)
				copy(newPath, path)
				newPath[len(path)] = neighbor
				queue = append(queue, newPath)
			}
		}
	}

	return nil, fmt.Errorf("no path found between %s and %s", sourceID, targetID)
}

// -- BFS Impact tests --

func TestImpactBFS_NoEdges(t *testing.T) {
	direct, indirect, severity := impactBFS(nil, "svc-1")

	assert.Empty(t, direct)
	assert.Empty(t, indirect)
	assert.Equal(t, ImpactSeverityLow, severity)
}

func TestImpactBFS_SingleDirectDependent(t *testing.T) {
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	direct, indirect, severity := impactBFS(edges, "db")

	assert.Equal(t, []string{"api"}, direct)
	assert.Empty(t, indirect)
	assert.Equal(t, ImpactSeverityLow, severity)
}

func TestImpactBFS_ChainedDependencies(t *testing.T) {
	// frontend -> api -> db
	edges := []*DependencyEdge{
		newEdge("frontend", "api", DependencyTypeSync, "http", true),
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	direct, indirect, severity := impactBFS(edges, "db")

	assert.Equal(t, []string{"api"}, direct)
	assert.Equal(t, []string{"frontend"}, indirect)
	assert.Equal(t, ImpactSeverityLow, severity) // total=2
}

func TestImpactBFS_SeverityModerate(t *testing.T) {
	// 3 services depend on db directly
	edges := []*DependencyEdge{
		newEdge("api-1", "db", DependencyTypeStorage, "postgres", true),
		newEdge("api-2", "db", DependencyTypeStorage, "postgres", true),
		newEdge("api-3", "db", DependencyTypeStorage, "postgres", true),
	}

	_, _, severity := impactBFS(edges, "db")
	assert.Equal(t, ImpactSeverityModerate, severity) // total=3
}

func TestImpactBFS_SeverityHigh(t *testing.T) {
	// Build a graph where db failure affects 6 services
	edges := []*DependencyEdge{
		newEdge("svc-1", "db", DependencyTypeStorage, "postgres", true),
		newEdge("svc-2", "db", DependencyTypeStorage, "postgres", true),
		newEdge("svc-3", "db", DependencyTypeStorage, "postgres", true),
		newEdge("svc-4", "svc-1", DependencyTypeSync, "http", true),
		newEdge("svc-5", "svc-2", DependencyTypeSync, "http", true),
		newEdge("svc-6", "svc-3", DependencyTypeSync, "http", true),
	}

	direct, indirect, severity := impactBFS(edges, "db")

	assert.Len(t, direct, 3)
	assert.Len(t, indirect, 3)
	assert.Equal(t, ImpactSeverityHigh, severity) // total=6
}

func TestImpactBFS_SeverityCritical(t *testing.T) {
	// 11 services depend directly
	edges := make([]*DependencyEdge, 11)
	for i := 0; i < 11; i++ {
		edges[i] = newEdge(fmt.Sprintf("svc-%d", i), "core-db", DependencyTypeStorage, "postgres", true)
	}

	_, _, severity := impactBFS(edges, "core-db")
	assert.Equal(t, ImpactSeverityCritical, severity) // total=11
}

func TestImpactBFS_IsolatedService(t *testing.T) {
	// Service exists but has no dependents
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	direct, indirect, _ := impactBFS(edges, "api")

	assert.Empty(t, direct)
	assert.Empty(t, indirect)
}

// -- BFS Path-finding tests --

func TestFindPathBFS_DirectPath(t *testing.T) {
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	path, err := findPathBFS(edges, "api", "db")

	require.NoError(t, err)
	assert.Equal(t, "api", path.Source)
	assert.Equal(t, "db", path.Target)
	assert.Equal(t, []string{"api", "db"}, path.Path)
	assert.Equal(t, 1, path.Distance)
	assert.Equal(t, "downstream", path.Type)
}

func TestFindPathBFS_TwoHopPath(t *testing.T) {
	edges := []*DependencyEdge{
		newEdge("frontend", "api", DependencyTypeSync, "http", true),
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	path, err := findPathBFS(edges, "frontend", "db")

	require.NoError(t, err)
	assert.Equal(t, []string{"frontend", "api", "db"}, path.Path)
	assert.Equal(t, 2, path.Distance)
}

func TestFindPathBFS_NoPathExists(t *testing.T) {
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	_, err := findPathBFS(edges, "db", "api") // reverse direction, no edge

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no path found")
}

func TestFindPathBFS_SourceEqualsTarget(t *testing.T) {
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
	}

	path, err := findPathBFS(edges, "api", "api")

	require.NoError(t, err)
	assert.Equal(t, []string{"api"}, path.Path)
	assert.Equal(t, 0, path.Distance)
}

func TestFindPathBFS_ShortestPathPreferred(t *testing.T) {
	// Two paths: api -> db (1 hop) and api -> cache -> db (2 hops)
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
		newEdge("api", "cache", DependencyTypeStorage, "redis", false),
		newEdge("cache", "db", DependencyTypeStorage, "postgres", true),
	}

	path, err := findPathBFS(edges, "api", "db")

	require.NoError(t, err)
	assert.Equal(t, 1, path.Distance, "BFS should find the shortest path")
	assert.Equal(t, []string{"api", "db"}, path.Path)
}

func TestFindPathBFS_DisconnectedGraph(t *testing.T) {
	edges := []*DependencyEdge{
		newEdge("api", "db", DependencyTypeStorage, "postgres", true),
		newEdge("worker", "queue", DependencyTypeAsync, "amqp", true),
	}

	_, err := findPathBFS(edges, "api", "queue")
	require.Error(t, err, "no path should exist between disconnected components")
}

func TestFindPathBFS_EmptyGraph(t *testing.T) {
	_, err := findPathBFS(nil, "api", "db")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: NewGraphBuilder
// ---------------------------------------------------------------------------

func TestNewGraphBuilder(t *testing.T) {
	// Verify constructor initializes fields correctly.
	// We pass nil for repos/k8sClient since we only check assignment.
	b := NewGraphBuilder(nil, nil, nil)

	require.NotNil(t, b)
	assert.Nil(t, b.repos)
	assert.Nil(t, b.k8sClient)
	assert.Nil(t, b.logger)
}

// ---------------------------------------------------------------------------
// Test: TopologyGraph struct construction
// ---------------------------------------------------------------------------

func TestTopologyGraph_StructConstruction(t *testing.T) {
	now := time.Now().UTC()
	graph := &TopologyGraph{
		Nodes: []*ServiceNode{
			{
				ID:   "svc-1",
				Name: "test-service",
				Type: ServiceTypeHTTP,
			},
		},
		Edges:       []*DependencyEdge{},
		Environment: "production",
		GeneratedAt: now,
		Stats: &TopologyStats{
			TotalServices:     1,
			TotalDependencies: 0,
			ServicesByType:    map[ServiceType]int{ServiceTypeHTTP: 1},
			HealthByProject:   map[string]*ProjectHealth{},
		},
	}

	assert.Equal(t, "production", graph.Environment)
	assert.Len(t, graph.Nodes, 1)
	assert.Empty(t, graph.Edges)
	assert.Equal(t, 1, graph.Stats.TotalServices)
}

func TestServiceDependencies_StructConstruction(t *testing.T) {
	deps := &ServiceDependencies{
		ServiceID:   "svc-1",
		ServiceName: "my-api",
		Upstream: []*DependencyEdge{
			newEdge("svc-1", "svc-2", DependencyTypeStorage, "postgres", true),
		},
		Downstream: []*DependencyEdge{
			newEdge("svc-3", "svc-1", DependencyTypeSync, "http", true),
		},
		UpstreamCount:   1,
		DownstreamCount: 1,
	}

	assert.Equal(t, "svc-1", deps.ServiceID)
	assert.Equal(t, "my-api", deps.ServiceName)
	assert.Len(t, deps.Upstream, 1)
	assert.Len(t, deps.Downstream, 1)
}

func TestImpactAnalysis_StructConstruction(t *testing.T) {
	analysis := &ImpactAnalysis{
		ServiceID:          "svc-db",
		ServiceName:        "core-database",
		DirectDependents:   []string{"api-1", "api-2"},
		IndirectDependents: []string{"frontend"},
		TotalImpact:        3,
		CriticalPath:       true,
		Severity:           ImpactSeverityModerate,
	}

	assert.Equal(t, "core-database", analysis.ServiceName)
	assert.Len(t, analysis.DirectDependents, 2)
	assert.Len(t, analysis.IndirectDependents, 1)
	assert.Equal(t, 3, analysis.TotalImpact)
	assert.True(t, analysis.CriticalPath)
	assert.Equal(t, ImpactSeverityModerate, analysis.Severity)
}
