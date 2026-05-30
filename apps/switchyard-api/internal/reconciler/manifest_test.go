package reconciler

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mustParseQuantity
// ---------------------------------------------------------------------------

func TestMustParseQuantity_ValidInputs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  resource.Quantity
	}{
		{"cpu millicores", "100m", resource.MustParse("100m")},
		{"memory mebibytes", "128Mi", resource.MustParse("128Mi")},
		{"memory gibibytes", "1Gi", resource.MustParse("1Gi")},
		{"cpu whole cores", "2", resource.MustParse("2")},
		{"cpu fractional", "0.5", resource.MustParse("0.5")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustParseQuantity(tt.input)
			assert.True(t, tt.want.Equal(got),
				"mustParseQuantity(%q) = %v, want %v", tt.input, got.String(), tt.want.String())
		})
	}
}

func TestMustParseQuantity_PanicsOnInvalid(t *testing.T) {
	assert.Panics(t, func() {
		mustParseQuantity("not-a-quantity")
	}, "mustParseQuantity should panic on invalid input")
}

// ---------------------------------------------------------------------------
// buildResourceRequirements
// ---------------------------------------------------------------------------

func TestBuildResourceRequirements_NilConfig(t *testing.T) {
	reqs := buildResourceRequirements(nil)

	// Verify defaults
	assert.True(t, resource.MustParse("100m").Equal(reqs.Requests[corev1.ResourceCPU]),
		"default CPU request should be 100m")
	assert.True(t, resource.MustParse("128Mi").Equal(reqs.Requests[corev1.ResourceMemory]),
		"default memory request should be 128Mi")
	assert.True(t, resource.MustParse("500m").Equal(reqs.Limits[corev1.ResourceCPU]),
		"default CPU limit should be 500m")
	assert.True(t, resource.MustParse("512Mi").Equal(reqs.Limits[corev1.ResourceMemory]),
		"default memory limit should be 512Mi")
}

func TestBuildResourceRequirements_EmptyConfig(t *testing.T) {
	cfg := &types.ResourceConfig{}
	reqs := buildResourceRequirements(cfg)

	// All fields empty should fall back to defaults
	assert.True(t, resource.MustParse("100m").Equal(reqs.Requests[corev1.ResourceCPU]))
	assert.True(t, resource.MustParse("128Mi").Equal(reqs.Requests[corev1.ResourceMemory]))
	assert.True(t, resource.MustParse("500m").Equal(reqs.Limits[corev1.ResourceCPU]))
	assert.True(t, resource.MustParse("512Mi").Equal(reqs.Limits[corev1.ResourceMemory]))
}

func TestBuildResourceRequirements_FullCustomConfig(t *testing.T) {
	cfg := &types.ResourceConfig{
		CPURequest:    "250m",
		CPULimit:      "1",
		MemoryRequest: "256Mi",
		MemoryLimit:   "1Gi",
	}
	reqs := buildResourceRequirements(cfg)

	assert.True(t, resource.MustParse("250m").Equal(reqs.Requests[corev1.ResourceCPU]))
	assert.True(t, resource.MustParse("256Mi").Equal(reqs.Requests[corev1.ResourceMemory]))
	assert.True(t, resource.MustParse("1").Equal(reqs.Limits[corev1.ResourceCPU]))
	assert.True(t, resource.MustParse("1Gi").Equal(reqs.Limits[corev1.ResourceMemory]))
}

func TestBuildResourceRequirements_PartialConfig(t *testing.T) {
	cfg := &types.ResourceConfig{
		CPURequest:  "200m",
		MemoryLimit: "1Gi",
		// CPULimit and MemoryRequest use defaults
	}
	reqs := buildResourceRequirements(cfg)

	assert.True(t, resource.MustParse("200m").Equal(reqs.Requests[corev1.ResourceCPU]),
		"custom CPU request should be used")
	assert.True(t, resource.MustParse("128Mi").Equal(reqs.Requests[corev1.ResourceMemory]),
		"memory request should default to 128Mi")
	assert.True(t, resource.MustParse("500m").Equal(reqs.Limits[corev1.ResourceCPU]),
		"CPU limit should default to 500m")
	assert.True(t, resource.MustParse("1Gi").Equal(reqs.Limits[corev1.ResourceMemory]),
		"custom memory limit should be used")
}

func TestBuildResourceRequirements_HasAllFourFields(t *testing.T) {
	reqs := buildResourceRequirements(nil)

	_, hasCPUReq := reqs.Requests[corev1.ResourceCPU]
	_, hasMemReq := reqs.Requests[corev1.ResourceMemory]
	_, hasCPULim := reqs.Limits[corev1.ResourceCPU]
	_, hasMemLim := reqs.Limits[corev1.ResourceMemory]

	assert.True(t, hasCPUReq, "Requests must include CPU")
	assert.True(t, hasMemReq, "Requests must include Memory")
	assert.True(t, hasCPULim, "Limits must include CPU")
	assert.True(t, hasMemLim, "Limits must include Memory")
}

// ---------------------------------------------------------------------------
// buildLivenessProbe
// ---------------------------------------------------------------------------

func TestBuildLivenessProbe_NilConfig(t *testing.T) {
	probe := buildLivenessProbe(nil, 4200)

	require.NotNil(t, probe)
	require.NotNil(t, probe.ProbeHandler.HTTPGet)
	assert.Equal(t, "/health", probe.HTTPGet.Path)
	assert.Equal(t, int32(4200), probe.HTTPGet.Port.IntVal)
	assert.Equal(t, int32(30), probe.InitialDelaySeconds)
	assert.Equal(t, int32(5), probe.TimeoutSeconds)
	assert.Equal(t, int32(10), probe.PeriodSeconds)
	assert.Equal(t, int32(3), probe.FailureThreshold)
}

func TestBuildLivenessProbe_Disabled(t *testing.T) {
	cfg := &types.HealthCheckConfig{Disabled: true}
	probe := buildLivenessProbe(cfg, 4200)

	assert.Nil(t, probe, "disabled probe should return nil")
}

func TestBuildLivenessProbe_CustomLivenessPath(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Path:         "/healthz",
		LivenessPath: "/livez",
	}
	probe := buildLivenessProbe(cfg, 8080)

	require.NotNil(t, probe)
	assert.Equal(t, "/livez", probe.HTTPGet.Path,
		"LivenessPath should take precedence over Path")
}

func TestBuildLivenessProbe_FallbackToPath(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Path: "/healthz",
	}
	probe := buildLivenessProbe(cfg, 8080)

	require.NotNil(t, probe)
	assert.Equal(t, "/healthz", probe.HTTPGet.Path,
		"should fall back to Path when LivenessPath is empty")
}

func TestBuildLivenessProbe_HTTPHeaders(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Path: "/api/v1/health/",
		Port: 8000,
		HTTPHeaders: map[string]string{
			"Host":              "tulana-api.madfam.io",
			"X-Forwarded-Proto": "https",
		},
	}
	probe := buildLivenessProbe(cfg, 8000)

	require.NotNil(t, probe)
	require.Len(t, probe.HTTPGet.HTTPHeaders, 2)
	headerMap := map[string]string{}
	for _, h := range probe.HTTPGet.HTTPHeaders {
		headerMap[h.Name] = h.Value
	}
	assert.Equal(t, "tulana-api.madfam.io", headerMap["Host"])
	assert.Equal(t, "https", headerMap["X-Forwarded-Proto"])
}

func TestBuildReadinessProbe_HTTPHeaders(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Path:        "/api/v1/health/",
		HTTPHeaders: map[string]string{"Host": "tulana-api.madfam.io"},
	}
	probe := buildReadinessProbe(cfg, 8000)

	require.NotNil(t, probe)
	require.Len(t, probe.HTTPGet.HTTPHeaders, 1)
	assert.Equal(t, "Host", probe.HTTPGet.HTTPHeaders[0].Name)
	assert.Equal(t, "tulana-api.madfam.io", probe.HTTPGet.HTTPHeaders[0].Value)
}

func TestBuildLivenessProbe_CustomPort(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Port: 9090,
	}
	probe := buildLivenessProbe(cfg, 4200)

	require.NotNil(t, probe)
	assert.Equal(t, int32(9090), probe.HTTPGet.Port.IntVal,
		"should use configured port instead of container port")
}

func TestBuildLivenessProbe_AllCustomValues(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		LivenessPath:        "/live",
		Port:                9090,
		InitialDelaySeconds: 60,
		TimeoutSeconds:      10,
		PeriodSeconds:       30,
		FailureThreshold:    5,
	}
	probe := buildLivenessProbe(cfg, 4200)

	require.NotNil(t, probe)
	assert.Equal(t, "/live", probe.HTTPGet.Path)
	assert.Equal(t, int32(9090), probe.HTTPGet.Port.IntVal)
	assert.Equal(t, int32(60), probe.InitialDelaySeconds)
	assert.Equal(t, int32(10), probe.TimeoutSeconds)
	assert.Equal(t, int32(30), probe.PeriodSeconds)
	assert.Equal(t, int32(5), probe.FailureThreshold)
}

func TestBuildLivenessProbe_EmptyConfig(t *testing.T) {
	cfg := &types.HealthCheckConfig{}
	probe := buildLivenessProbe(cfg, 4200)

	require.NotNil(t, probe, "empty (non-disabled) config should still produce a probe")
	assert.Equal(t, "/health", probe.HTTPGet.Path, "should use default path")
	assert.Equal(t, int32(4200), probe.HTTPGet.Port.IntVal, "should use container port")
}

func TestBuildLivenessProbe_ZeroValuesIgnored(t *testing.T) {
	// Zero values in int fields should be treated as "not set" and use defaults
	cfg := &types.HealthCheckConfig{
		Port:                0,
		InitialDelaySeconds: 0,
		TimeoutSeconds:      0,
		PeriodSeconds:       0,
		FailureThreshold:    0,
	}
	probe := buildLivenessProbe(cfg, 4200)

	require.NotNil(t, probe)
	assert.Equal(t, int32(4200), probe.HTTPGet.Port.IntVal, "zero port should keep container port")
	assert.Equal(t, int32(30), probe.InitialDelaySeconds, "zero should use liveness default 30")
	assert.Equal(t, int32(5), probe.TimeoutSeconds, "zero should use default 5")
	assert.Equal(t, int32(10), probe.PeriodSeconds, "zero should use default 10")
	assert.Equal(t, int32(3), probe.FailureThreshold, "zero should use default 3")
}

// ---------------------------------------------------------------------------
// buildReadinessProbe
// ---------------------------------------------------------------------------

func TestBuildReadinessProbe_NilConfig(t *testing.T) {
	probe := buildReadinessProbe(nil, 4200)

	require.NotNil(t, probe)
	require.NotNil(t, probe.ProbeHandler.HTTPGet)
	assert.Equal(t, "/health", probe.HTTPGet.Path)
	assert.Equal(t, int32(4200), probe.HTTPGet.Port.IntVal)
	assert.Equal(t, int32(5), probe.InitialDelaySeconds, "readiness default initial delay is 5")
	assert.Equal(t, int32(3), probe.TimeoutSeconds, "readiness default timeout is 3")
	assert.Equal(t, int32(5), probe.PeriodSeconds, "readiness default period is 5")
	assert.Equal(t, int32(2), probe.FailureThreshold, "readiness default failure threshold is 2")
}

func TestBuildReadinessProbe_Disabled(t *testing.T) {
	cfg := &types.HealthCheckConfig{Disabled: true}
	probe := buildReadinessProbe(cfg, 4200)

	assert.Nil(t, probe, "disabled probe should return nil")
}

func TestBuildReadinessProbe_CustomReadinessPath(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Path:          "/healthz",
		ReadinessPath: "/readyz",
	}
	probe := buildReadinessProbe(cfg, 8080)

	require.NotNil(t, probe)
	assert.Equal(t, "/readyz", probe.HTTPGet.Path,
		"ReadinessPath should take precedence over Path")
}

func TestBuildReadinessProbe_FallbackToPath(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		Path: "/healthz",
	}
	probe := buildReadinessProbe(cfg, 8080)

	require.NotNil(t, probe)
	assert.Equal(t, "/healthz", probe.HTTPGet.Path,
		"should fall back to Path when ReadinessPath is empty")
}

func TestBuildReadinessProbe_AllCustomValues(t *testing.T) {
	cfg := &types.HealthCheckConfig{
		ReadinessPath:       "/ready",
		Port:                9090,
		InitialDelaySeconds: 15,
		TimeoutSeconds:      8,
		PeriodSeconds:       20,
		FailureThreshold:    4,
	}
	probe := buildReadinessProbe(cfg, 4200)

	require.NotNil(t, probe)
	assert.Equal(t, "/ready", probe.HTTPGet.Path)
	assert.Equal(t, int32(9090), probe.HTTPGet.Port.IntVal)
	assert.Equal(t, int32(15), probe.InitialDelaySeconds)
	assert.Equal(t, int32(8), probe.TimeoutSeconds)
	assert.Equal(t, int32(20), probe.PeriodSeconds)
	assert.Equal(t, int32(4), probe.FailureThreshold)
}

// ---------------------------------------------------------------------------
// Liveness vs Readiness default differentiation
// ---------------------------------------------------------------------------

func TestLivenessAndReadinessHaveDifferentDefaults(t *testing.T) {
	liveness := buildLivenessProbe(nil, 4200)
	readiness := buildReadinessProbe(nil, 4200)

	require.NotNil(t, liveness)
	require.NotNil(t, readiness)

	// Liveness should have longer initial delay and higher failure threshold
	assert.Greater(t, liveness.InitialDelaySeconds, readiness.InitialDelaySeconds,
		"liveness initial delay (30) should be greater than readiness (5)")
	assert.Greater(t, liveness.FailureThreshold, readiness.FailureThreshold,
		"liveness failure threshold (3) should be greater than readiness (2)")

	// Both use /health by default
	assert.Equal(t, liveness.HTTPGet.Path, readiness.HTTPGet.Path,
		"both probes should default to the same health path")
}

// ---------------------------------------------------------------------------
// parseContainerPort (additional edge cases beyond service_test.go)
// ---------------------------------------------------------------------------

func TestParseContainerPort_NilMap(t *testing.T) {
	// Verify nil map does not panic and returns the default
	port, err := parseContainerPort(nil)
	assert.NoError(t, err)
	assert.Equal(t, int32(4200), port)
}

func TestParseContainerPortWithSource_NilMap(t *testing.T) {
	port, source, err := parseContainerPortWithSource(nil)
	assert.NoError(t, err)
	assert.Equal(t, int32(4200), port)
	assert.Equal(t, PortSourceDefault, source)
}

func TestParseContainerPort_WhitespaceOnlyPort(t *testing.T) {
	// A port value of spaces should be treated as "not set" since the map key
	// exists but the value is whitespace. The function checks for empty string
	// but not whitespace, so this tests actual behavior.
	envVars := map[string]string{"ENCLII_PORT": "   "}
	port, err := parseContainerPort(envVars)

	// The current implementation will try to parse "   " as an int and fail
	assert.Error(t, err, "whitespace-only port value should produce an error")
	assert.Equal(t, int32(4200), port, "should fall back to default on parse failure")
}

// ---------------------------------------------------------------------------
// extractVersionFromImage (controller_sync.go)
// ---------------------------------------------------------------------------

func TestExtractVersionFromImage(t *testing.T) {
	tests := []struct {
		name     string
		imageURI string
		want     string
	}{
		{
			name:     "standard GHCR image with short SHA tag",
			imageURI: "ghcr.io/madfam-org/enclii/waybill:1ead1b30fdb4",
			want:     "1ead1b30fdb4",
		},
		{
			name:     "truncates tags longer than 12 chars",
			imageURI: "ghcr.io/org/repo:abcdef1234567890",
			want:     "abcdef123456",
		},
		{
			name:     "semantic version tag preserved",
			imageURI: "docker.io/library/nginx:1.25.3",
			want:     "1.25.3",
		},
		{
			name:     "latest tag",
			imageURI: "nginx:latest",
			want:     "latest",
		},
		{
			name:     "no tag returns unknown",
			imageURI: "ghcr.io/madfam-org/enclii/api",
			want:     "unknown",
		},
		{
			name:     "empty string returns unknown",
			imageURI: "",
			want:     "unknown",
		},
		{
			name:     "tag with exactly 12 chars is not truncated",
			imageURI: "repo:123456789012",
			want:     "123456789012",
		},
		{
			name:     "trailing colon with no tag returns unknown",
			imageURI: "repo:",
			want:     "unknown",
		},
		{
			name:     "digest-style image (sha256 prefix)",
			imageURI: "ghcr.io/org/repo:sha256-abc123def456789",
			want:     "sha256-abc12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractVersionFromImage(tt.imageURI)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// extractGitSHAFromImage (controller_sync.go)
// ---------------------------------------------------------------------------

func TestExtractGitSHAFromImage(t *testing.T) {
	tests := []struct {
		name     string
		imageURI string
		want     string
	}{
		{
			name:     "standard GHCR image with SHA tag",
			imageURI: "ghcr.io/madfam-org/enclii/waybill:1ead1b30fdb4",
			want:     "1ead1b30fdb4",
		},
		{
			name:     "full SHA tag is preserved",
			imageURI: "ghcr.io/org/repo:abcdef1234567890abcdef1234567890abcdef12",
			want:     "abcdef1234567890abcdef1234567890abcdef12",
		},
		{
			name:     "semver tag preserved as-is",
			imageURI: "nginx:1.25.3",
			want:     "1.25.3",
		},
		{
			name:     "no tag returns empty",
			imageURI: "ghcr.io/madfam-org/enclii/api",
			want:     "",
		},
		{
			name:     "empty string returns empty",
			imageURI: "",
			want:     "",
		},
		{
			name:     "trailing colon with no tag returns empty",
			imageURI: "repo:",
			want:     "",
		},
		{
			name:     "latest tag",
			imageURI: "nginx:latest",
			want:     "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGitSHAFromImage(tt.imageURI)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// nodeNames (admin_reconciler.go)
// ---------------------------------------------------------------------------

func TestNodeNames(t *testing.T) {
	tests := []struct {
		name    string
		nodes   map[string]corev1.Node
		wantLen int
	}{
		{
			name:    "nil map returns empty slice",
			nodes:   nil,
			wantLen: 0,
		},
		{
			name:    "empty map returns empty slice",
			nodes:   map[string]corev1.Node{},
			wantLen: 0,
		},
		{
			name: "single node",
			nodes: map[string]corev1.Node{
				"foundry-core": {},
			},
			wantLen: 1,
		},
		{
			name: "multiple nodes",
			nodes: map[string]corev1.Node{
				"foundry-core":       {},
				"foundry-builder-01": {},
				"foundry-gpu-01":     {},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeNames(tt.nodes)
			assert.Len(t, got, tt.wantLen)

			// Verify every returned name exists as a key in the input map
			for _, name := range got {
				_, exists := tt.nodes[name]
				assert.True(t, exists, "returned name %q should exist in input map", name)
			}
		})
	}
}
