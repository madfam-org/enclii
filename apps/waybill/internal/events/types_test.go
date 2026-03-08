package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateGBEquivalent_MemoryDominant(t *testing.T) {
	tests := []struct {
		name     string
		metrics  DeploymentMetrics
		expected float64
	}{
		{
			name: "1GB_memory_exceeds_half_cpu",
			metrics: DeploymentMetrics{
				Replicas:      1,
				CPUMillicores: 500,
				MemoryMB:      1024,
			},
			expected: 1.0, // 1024/1024 = 1.0 GB > 500/1000 = 0.5
		},
		{
			name: "8GB_memory_exceeds_250m_cpu",
			metrics: DeploymentMetrics{
				Replicas:      1,
				CPUMillicores: 250,
				MemoryMB:      8192,
			},
			expected: 8.0, // 8192/1024 = 8.0 GB > 250/1000 = 0.25
		},
		{
			name: "2GB_memory_with_3_replicas",
			metrics: DeploymentMetrics{
				Replicas:      3,
				CPUMillicores: 500,
				MemoryMB:      2048,
			},
			expected: 6.0, // 2.0 GB * 3 replicas
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.metrics.CalculateGBEquivalent()
			assert.InDelta(t, tt.expected, got, 1e-9, "GB equivalent mismatch")
		})
	}
}

func TestCalculateGBEquivalent_CPUDominant(t *testing.T) {
	tests := []struct {
		name     string
		metrics  DeploymentMetrics
		expected float64
	}{
		{
			name: "4_cpu_exceeds_512MB_memory",
			metrics: DeploymentMetrics{
				Replicas:      1,
				CPUMillicores: 4000,
				MemoryMB:      512,
			},
			expected: 4.0, // 4000/1000 = 4.0 > 512/1024 = 0.5
		},
		{
			name: "2_cpu_exceeds_1GB_memory_with_2_replicas",
			metrics: DeploymentMetrics{
				Replicas:      2,
				CPUMillicores: 2000,
				MemoryMB:      1024,
			},
			// CPU = 2.0, Memory = 1.0, equal -> cpuGB is NOT > memoryGB, so memory wins
			// Actually: memoryGB (1.0) > cpuGB (2.0) is false, so cpuGB path: 2.0 * 2 = 4.0
			expected: 4.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.metrics.CalculateGBEquivalent()
			assert.InDelta(t, tt.expected, got, 1e-9, "GB equivalent mismatch")
		})
	}
}

func TestCalculateGBEquivalent_EqualCPUAndMemory(t *testing.T) {
	// When CPU and memory are equal, CPU path is taken (memoryGB > cpuGB is false)
	m := DeploymentMetrics{
		Replicas:      1,
		CPUMillicores: 1000,
		MemoryMB:      1024,
	}

	got := m.CalculateGBEquivalent()
	// cpuGB = 1.0, memoryGB = 1.0 -> memoryGB > cpuGB is false -> returns cpuGB * replicas
	assert.InDelta(t, 1.0, got, 1e-9, "equal CPU/memory should return CPU path value")
}

func TestCalculateGBEquivalent_ZeroResources(t *testing.T) {
	m := DeploymentMetrics{
		Replicas:      0,
		CPUMillicores: 0,
		MemoryMB:      0,
	}

	got := m.CalculateGBEquivalent()
	assert.Equal(t, 0.0, got, "zero resources should yield 0 GB equivalent")
}

func TestCalculateGBEquivalent_ZeroReplicas(t *testing.T) {
	// Even with CPU/memory set, zero replicas means zero GB-equivalent
	m := DeploymentMetrics{
		Replicas:      0,
		CPUMillicores: 4000,
		MemoryMB:      8192,
	}

	got := m.CalculateGBEquivalent()
	assert.Equal(t, 0.0, got, "zero replicas should yield 0 GB equivalent regardless of CPU/memory")
}

func TestCalculateGBEquivalent_LargeScale(t *testing.T) {
	m := DeploymentMetrics{
		Replicas:      100,
		CPUMillicores: 8000,
		MemoryMB:      16384, // 16 GB
	}

	got := m.CalculateGBEquivalent()
	// Memory: 16384/1024 = 16.0 GB, CPU: 8000/1000 = 8.0 GB
	// Memory dominates: 16.0 * 100 = 1600.0
	assert.InDelta(t, 1600.0, got, 1e-9, "large-scale GB equivalent mismatch")
}

func TestMetricTypeConstants(t *testing.T) {
	// Verify metric type string values match expected database/API values
	tests := []struct {
		name     string
		metric   MetricType
		expected string
	}{
		{"compute_gb_hours", MetricComputeGBHours, "compute_gb_hours"},
		{"build_minutes", MetricBuildMinutes, "build_minutes"},
		{"storage_gb_hours", MetricStorageGBHours, "storage_gb_hours"},
		{"bandwidth_gb", MetricBandwidthGB, "bandwidth_gb"},
		{"custom_domains", MetricCustomDomains, "custom_domains"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, MetricType(tt.expected), tt.metric,
				"metric type constant value mismatch")
		})
	}
}

func TestEventTypeConstants(t *testing.T) {
	// Verify event type string values match expected database/API values
	tests := []struct {
		name      string
		eventType EventType
		expected  string
	}{
		{"deployment_started", EventDeploymentStarted, "deployment.started"},
		{"deployment_stopped", EventDeploymentStopped, "deployment.stopped"},
		{"deployment_scaled", EventDeploymentScaled, "deployment.scaled"},
		{"build_started", EventBuildStarted, "build.started"},
		{"build_completed", EventBuildCompleted, "build.completed"},
		{"build_failed", EventBuildFailed, "build.failed"},
		{"volume_created", EventVolumeCreated, "volume.created"},
		{"volume_deleted", EventVolumeDeleted, "volume.deleted"},
		{"volume_resized", EventVolumeResized, "volume.resized"},
		{"bandwidth_usage", EventBandwidthUsage, "bandwidth.usage"},
		{"domain_added", EventDomainAdded, "domain.added"},
		{"domain_removed", EventDomainRemoved, "domain.removed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, EventType(tt.expected), tt.eventType,
				"event type constant value mismatch")
		})
	}
}
