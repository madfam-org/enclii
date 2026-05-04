package topology

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// GraphBuilder constructs topology graphs from service data
type GraphBuilder struct {
	repos     *db.Repositories
	k8sClient *k8s.Client
	logger    *logrus.Logger
}

// NewGraphBuilder creates a new topology graph builder
func NewGraphBuilder(repos *db.Repositories, k8sClient *k8s.Client, logger *logrus.Logger) *GraphBuilder {
	return &GraphBuilder{
		repos:     repos,
		k8sClient: k8sClient,
		logger:    logger,
	}
}

// BuildTopology constructs the complete topology graph for an environment
func (b *GraphBuilder) BuildTopology(ctx context.Context, environment string) (*TopologyGraph, error) {
	b.logger.Infof("Building topology graph for environment: %s", environment)

	// Fetch all services (we'll filter by environment later)
	services, err := b.repos.Services.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch services: %w", err)
	}

	// Build service nodes
	nodes := make([]*ServiceNode, 0)
	nodeMap := make(map[string]*ServiceNode)

	for _, service := range services {
		// Get latest deployment for health status
		deployment, err := b.repos.Deployments.GetLatestByService(ctx, service.ID.String())
		var healthStatus HealthStatus
		var replicas, availableReplicas int
		var version, imageURI string
		var updatedAt time.Time

		if err == nil && deployment != nil {
			// Get the environment to determine the correct namespace
			env, envErr := b.repos.Environments.GetByID(ctx, deployment.EnvironmentID)
			namespace := ""
			if envErr == nil && env.KubeNamespace != "" {
				namespace = env.KubeNamespace
			}

			// Get Kubernetes status if we have a valid namespace
			var k8sStatus *k8s.DeploymentStatusInfo
			if namespace != "" {
				k8sStatus, err = b.k8sClient.GetDeploymentStatusInfo(ctx, namespace, service.Name)
			}

			if err == nil && k8sStatus != nil {
				replicas = int(k8sStatus.Replicas)
				availableReplicas = int(k8sStatus.AvailableReplicas)
				healthStatus = deriveTopologyHealth(ctx, b.k8sClient, namespace, service.Name, replicas, availableReplicas, b.logger)
			} else {
				healthStatus = HealthStatusUnknown
			}

			// Get release info
			release, err := b.repos.Releases.GetByID(deployment.ReleaseID)
			if err == nil {
				version = release.Version
				imageURI = release.ImageURI
			}
			updatedAt = deployment.UpdatedAt
		} else {
			healthStatus = HealthStatusUnknown
			updatedAt = service.UpdatedAt
		}

		// Get project name
		project, err := b.repos.Projects.GetByID(ctx, service.ProjectID)
		projectName := service.ProjectID.String() // Fallback to ID
		if err == nil && project != nil {
			projectName = project.Name
		}

		node := &ServiceNode{
			ID:                service.ID.String(),
			Name:              service.Name,
			ProjectID:         service.ProjectID.String(),
			ProjectName:       projectName,
			Environment:       environment,
			Type:              detectServiceType(service),
			Status:            healthStatus,
			Metadata:          make(map[string]string),
			Replicas:          replicas,
			AvailableReplicas: availableReplicas,
			Version:           version,
			ImageURI:          imageURI,
			UpdatedAt:         updatedAt,
		}

		nodes = append(nodes, node)
		nodeMap[service.ID.String()] = node
	}

	// Build dependency edges
	edges := make([]*DependencyEdge, 0)

	// Detect dependencies from service configuration
	for _, service := range services {
		deps := b.detectDependencies(ctx, service, services)
		edges = append(edges, deps...)
	}

	// Calculate stats
	stats := b.calculateStats(nodes, edges)

	graph := &TopologyGraph{
		Nodes:       nodes,
		Edges:       edges,
		Environment: environment,
		GeneratedAt: time.Now().UTC(),
		Stats:       stats,
	}

	b.logger.Infof("Topology graph built: %d services, %d dependencies",
		len(nodes), len(edges))

	return graph, nil
}

// detectServiceType attempts to determine the service type from configuration
func detectServiceType(service *types.Service) ServiceType {
	// In production, analyze service config, ports, environment variables
	// Check for database-related patterns
	gitRepo := strings.ToLower(service.GitRepo)
	name := strings.ToLower(service.Name)

	if strings.Contains(name, "postgres") || strings.Contains(name, "mysql") || strings.Contains(name, "database") {
		return ServiceTypeDatabase
	}
	if strings.Contains(name, "redis") || strings.Contains(name, "cache") {
		return ServiceTypeCache
	}
	if strings.Contains(name, "queue") || strings.Contains(name, "kafka") || strings.Contains(name, "rabbitmq") {
		return ServiceTypeQueue
	}
	if strings.Contains(name, "grpc") || strings.Contains(gitRepo, "grpc") {
		return ServiceTypeGRPC
	}

	// Default to HTTP
	return ServiceTypeHTTP
}

// detectDependencies analyzes a service to find its dependencies
func (b *GraphBuilder) detectDependencies(ctx context.Context, service *types.Service, allServices []*types.Service) []*DependencyEdge {
	edges := make([]*DependencyEdge, 0)

	// In production, analyze:
	// 1. Environment variables (DATABASE_URL, REDIS_URL, etc.)
	// 2. Service discovery configuration
	// 3. Explicitly declared dependencies in service config
	// 4. Network traffic analysis (if available)

	// For MVP, detect common dependency patterns
	// Example: if service name contains "api" and another service contains "database"
	// we might infer a dependency

	serviceName := strings.ToLower(service.Name)

	for _, target := range allServices {
		if target.ID == service.ID {
			continue // Skip self
		}

		targetName := strings.ToLower(target.Name)

		// Simple heuristic: API services depend on database services
		if (strings.Contains(serviceName, "api") || strings.Contains(serviceName, "backend")) &&
			(strings.Contains(targetName, "database") || strings.Contains(targetName, "postgres")) {

			edge := &DependencyEdge{
				ID:        fmt.Sprintf("%s-%s", service.ID.String(), target.ID.String()),
				SourceID:  service.ID.String(),
				TargetID:  target.ID.String(),
				Type:      DependencyTypeStorage,
				Protocol:  "postgres",
				Required:  true,
				Metadata:  make(map[string]string),
				CreatedAt: time.Now().UTC(),
			}
			edges = append(edges, edge)
		}

		// API services often depend on cache
		if strings.Contains(serviceName, "api") && strings.Contains(targetName, "cache") {
			edge := &DependencyEdge{
				ID:        fmt.Sprintf("%s-%s", service.ID.String(), target.ID.String()),
				SourceID:  service.ID.String(),
				TargetID:  target.ID.String(),
				Type:      DependencyTypeStorage,
				Protocol:  "redis",
				Required:  false,
				Metadata:  make(map[string]string),
				CreatedAt: time.Now().UTC(),
			}
			edges = append(edges, edge)
		}
	}

	return edges
}

// calculateStats computes topology statistics
func (b *GraphBuilder) calculateStats(nodes []*ServiceNode, edges []*DependencyEdge) *TopologyStats {
	stats := &TopologyStats{
		TotalServices:     len(nodes),
		TotalDependencies: len(edges),
		ServicesByType:    make(map[ServiceType]int),
		HealthByProject:   make(map[string]*ProjectHealth),
	}

	// Count by type and health
	for _, node := range nodes {
		stats.ServicesByType[node.Type]++

		switch node.Status {
		case HealthStatusHealthy:
			stats.HealthyServices++
		case HealthStatusDegraded:
			stats.DegradedServices++
		case HealthStatusUnhealthy:
			stats.UnhealthyServices++
		}

		// Aggregate by project
		if _, exists := stats.HealthByProject[node.ProjectID]; !exists {
			stats.HealthByProject[node.ProjectID] = &ProjectHealth{
				ProjectID:   node.ProjectID,
				ProjectName: node.ProjectName,
			}
		}

		projectHealth := stats.HealthByProject[node.ProjectID]
		projectHealth.TotalServices++

		switch node.Status {
		case HealthStatusHealthy:
			projectHealth.HealthyServices++
		case HealthStatusDegraded:
			projectHealth.DegradedServices++
		case HealthStatusUnhealthy:
			projectHealth.UnhealthyServices++
		}

		// Calculate health score (0.0 to 1.0)
		if projectHealth.TotalServices > 0 {
			projectHealth.HealthScore = float64(projectHealth.HealthyServices) / float64(projectHealth.TotalServices)
		}
	}

	return stats
}

// GetServiceDependencies returns all dependencies for a specific service
func (b *GraphBuilder) GetServiceDependencies(ctx context.Context, serviceID string) (*ServiceDependencies, error) {
	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		return nil, fmt.Errorf("invalid service ID: %w", err)
	}

	// Get service
	service, err := b.repos.Services.GetByID(serviceUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	// Build topology to get all edges
	topology, err := b.BuildTopology(ctx, "all")
	if err != nil {
		return nil, fmt.Errorf("failed to build topology: %w", err)
	}

	upstream := make([]*DependencyEdge, 0)
	downstream := make([]*DependencyEdge, 0)

	// Find all edges involving this service
	for _, edge := range topology.Edges {
		if edge.SourceID == serviceID {
			upstream = append(upstream, edge)
		}
		if edge.TargetID == serviceID {
			downstream = append(downstream, edge)
		}
	}

	deps := &ServiceDependencies{
		ServiceID:       serviceID,
		ServiceName:     service.Name,
		Upstream:        upstream,
		Downstream:      downstream,
		UpstreamCount:   len(upstream),
		DownstreamCount: len(downstream),
	}

	return deps, nil
}

// AnalyzeImpact calculates the impact of a service failure
func (b *GraphBuilder) AnalyzeImpact(ctx context.Context, serviceID string) (*ImpactAnalysis, error) {
	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		return nil, fmt.Errorf("invalid service ID: %w", err)
	}

	// Get service
	service, err := b.repos.Services.GetByID(serviceUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	// Build topology
	topology, err := b.BuildTopology(ctx, "all")
	if err != nil {
		return nil, fmt.Errorf("failed to build topology: %w", err)
	}

	// Build adjacency list for graph traversal
	adjacency := make(map[string][]string)
	for _, edge := range topology.Edges {
		adjacency[edge.TargetID] = append(adjacency[edge.TargetID], edge.SourceID)
	}

	// Find direct dependents
	directDependents := adjacency[serviceID]

	// Find indirect dependents via BFS
	visited := make(map[string]bool)
	queue := make([]string, len(directDependents))
	copy(queue, directDependents)

	indirectDependents := make([]string, 0)

	for _, dep := range directDependents {
		visited[dep] = true
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Get dependents of current
		for _, dependent := range adjacency[current] {
			if !visited[dependent] {
				visited[dependent] = true
				indirectDependents = append(indirectDependents, dependent)
				queue = append(queue, dependent)
			}
		}
	}

	totalImpact := len(directDependents) + len(indirectDependents)

	// Determine severity
	var severity ImpactSeverity
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

	analysis := &ImpactAnalysis{
		ServiceID:          serviceID,
		ServiceName:        service.Name,
		DirectDependents:   directDependents,
		IndirectDependents: indirectDependents,
		TotalImpact:        totalImpact,
		CriticalPath:       len(directDependents) > 0, // Simple heuristic
		Severity:           severity,
	}

	return analysis, nil
}

// FindPath finds a dependency path between two services
func (b *GraphBuilder) FindPath(ctx context.Context, sourceID, targetID string) (*DependencyPath, error) {
	// Build topology
	topology, err := b.BuildTopology(ctx, "all")
	if err != nil {
		return nil, fmt.Errorf("failed to build topology: %w", err)
	}

	// Build adjacency list
	adjacency := make(map[string][]string)
	for _, edge := range topology.Edges {
		adjacency[edge.SourceID] = append(adjacency[edge.SourceID], edge.TargetID)
	}

	// BFS to find shortest path
	queue := [][]string{{sourceID}}
	visited := make(map[string]bool)
	visited[sourceID] = true

	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]

		current := path[len(path)-1]

		if current == targetID {
			// Found path
			return &DependencyPath{
				Source:   sourceID,
				Target:   targetID,
				Path:     path,
				Distance: len(path) - 1,
				Type:     "downstream",
			}, nil
		}

		// Explore neighbors
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

// deriveTopologyHealth maps Deployment replica counts + EvaluateRolloutState
// onto the topology graph's HealthStatus enum.
//
// Truth table:
//   - rollout blocked  → HealthStatusUnhealthy   (newest RS dead, dashboard had been hiding this)
//   - all replicas Ready → HealthStatusHealthy
//   - some replicas Ready → HealthStatusDegraded
//   - zero Ready         → HealthStatusUnhealthy
//
// On EvaluateRolloutState error or missing client we silently fall back to the
// replica-count baseline — same conservative behavior as the reconciler path.
func deriveTopologyHealth(
	ctx context.Context,
	k8sClient *k8s.Client,
	namespace, deploymentName string,
	replicas, availableReplicas int,
	logger *logrus.Logger,
) HealthStatus {
	// Rollout-state overlay. If unavailable, keep baseline.
	if k8sClient == nil || !k8sClient.IsValid() || namespace == "" || deploymentName == "" {
		return applyTopologyRolloutState(replicas, availableReplicas, nil, namespace, deploymentName, logger)
	}

	eval, err := k8s.EvaluateRolloutState(
		ctx,
		k8sClient.Clientset,
		namespace,
		deploymentName,
		time.Now(),
		k8s.DefaultRolloutGrace,
	)
	if err != nil {
		if logger != nil {
			logger.WithError(err).WithFields(logrus.Fields{
				"deployment": deploymentName,
				"namespace":  namespace,
			}).Debug("topology: EvaluateRolloutState failed; keeping baseline")
		}
		return applyTopologyRolloutState(replicas, availableReplicas, nil, namespace, deploymentName, logger)
	}

	return applyTopologyRolloutState(replicas, availableReplicas, &eval, namespace, deploymentName, logger)
}

// applyTopologyRolloutState is the pure decision function for topology health.
// Pass eval=nil to take the baseline-only path. Extracted so unit tests can
// exercise the truth table without standing up a K8s client.
func applyTopologyRolloutState(
	replicas, availableReplicas int,
	eval *k8s.RolloutEvaluation,
	namespace, deploymentName string,
	logger *logrus.Logger,
) HealthStatus {
	// Replica-count baseline (preserved for backward compatibility / fallback).
	var baseline HealthStatus
	switch {
	case availableReplicas == replicas && replicas > 0:
		baseline = HealthStatusHealthy
	case availableReplicas > 0:
		baseline = HealthStatusDegraded
	default:
		baseline = HealthStatusUnhealthy
	}

	if eval == nil {
		return baseline
	}

	if eval.State == k8s.RolloutStateBlocked {
		if logger != nil {
			logger.WithFields(logrus.Fields{
				"deployment":             deploymentName,
				"namespace":              namespace,
				"rollout_state":          string(eval.State),
				"rollout_blocked_reason": string(eval.BlockedReason),
			}).Warn("topology: rollout blocked; downgrading service health to unhealthy")
		}
		return HealthStatusUnhealthy
	}

	return baseline
}
