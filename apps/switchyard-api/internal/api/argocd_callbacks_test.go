package api

import (
	"testing"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func TestExtractServiceCandidates(t *testing.T) {
	tests := []struct {
		name     string
		imageURI string
		want     []string
	}{
		{
			name:     "enclii image returns prefixed first then simple",
			imageURI: "ghcr.io/madfam-org/enclii/switchyard-api:latest",
			want:     []string{"enclii-switchyard-api", "switchyard-api"},
		},
		{
			name:     "nested tezca image with digest",
			imageURI: "ghcr.io/madfam-org/tezca/api@sha256:abc123",
			want:     []string{"tezca-api", "api"},
		},
		{
			name:     "nested dhanam image with tag",
			imageURI: "ghcr.io/madfam-org/dhanam/admin:main",
			want:     []string{"dhanam-admin", "admin"},
		},
		{
			name:     "nested dhanam web image",
			imageURI: "ghcr.io/madfam-org/dhanam/web:main",
			want:     []string{"dhanam-web", "web"},
		},
		{
			name:     "nested tezca web image no tag",
			imageURI: "ghcr.io/madfam-org/tezca/web",
			want:     []string{"tezca-web", "web"},
		},
		{
			name:     "docker.io image",
			imageURI: "docker.io/library/nginx:latest",
			want:     []string{"nginx"},
		},
		{
			name:     "simple image no registry prefix",
			imageURI: "myapp:v1.0",
			want:     []string{"myapp"},
		},
		{
			name:     "enclii switchyard-ui with digest returns both candidates",
			imageURI: "ghcr.io/madfam-org/enclii/switchyard-ui@sha256:deadbeef",
			want:     []string{"enclii-switchyard-ui", "switchyard-ui"},
		},
		{
			name:     "janua api image",
			imageURI: "ghcr.io/madfam-org/janua/api:main",
			want:     []string{"janua-api", "api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractServiceCandidates(tt.imageURI)
			if len(got) != len(tt.want) {
				t.Errorf("extractServiceCandidates(%q) returned %d candidates %v, want %d candidates %v",
					tt.imageURI, len(got), got, len(tt.want), tt.want)
				return
			}
			for i, g := range got {
				if g != tt.want[i] {
					t.Errorf("extractServiceCandidates(%q)[%d] = %q, want %q",
						tt.imageURI, i, g, tt.want[i])
				}
			}
		})
	}
}

func TestExtractServiceName(t *testing.T) {
	tests := []struct {
		name     string
		imageURI string
		want     string
	}{
		{
			name:     "returns first candidate for nested path",
			imageURI: "ghcr.io/madfam-org/tezca/api@sha256:abc",
			want:     "tezca-api",
		},
		{
			name:     "returns prefixed name for 3-segment path",
			imageURI: "ghcr.io/madfam-org/enclii/switchyard-api:latest",
			want:     "enclii-switchyard-api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractServiceName(tt.imageURI)
			if got != tt.want {
				t.Errorf("extractServiceName(%q) = %q, want %q", tt.imageURI, got, tt.want)
			}
		})
	}
}

func TestShortSHA(t *testing.T) {
	tests := []struct {
		sha  string
		want string
	}{
		{"abc1234567890", "abc1234"},
		{"abc", "abc"},
		{"1234567", "1234567"},
		{"", ""},
	}

	for _, tt := range tests {
		got := shortSHA(tt.sha)
		if got != tt.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tt.sha, got, tt.want)
		}
	}
}

func TestArgocdEventType(t *testing.T) {
	tests := []struct {
		name         string
		syncStatus   string
		healthStatus string
		want         string
	}{
		{
			name:         "synced+healthy → deploy_healthy",
			syncStatus:   "Synced",
			healthStatus: "Healthy",
			want:         types.LifecycleDeployHealthy,
		},
		{
			name:         "synced+empty health → deploy_healthy (primary signal is SyncStatus)",
			syncStatus:   "Synced",
			healthStatus: "",
			want:         types.LifecycleDeployHealthy,
		},
		{
			name:         "synced+progressing → deploy_healthy (not syncing)",
			syncStatus:   "Synced",
			healthStatus: "Progressing",
			want:         types.LifecycleDeployHealthy,
		},
		{
			name:         "synced+degraded → deploy_degraded",
			syncStatus:   "Synced",
			healthStatus: "Degraded",
			want:         types.LifecycleDeployDegraded,
		},
		{
			name:         "synced+unknown → deploy_healthy",
			syncStatus:   "Synced",
			healthStatus: "Unknown",
			want:         types.LifecycleDeployHealthy,
		},
		{
			name:         "outofsync → deploy_synced (in-progress)",
			syncStatus:   "OutOfSync",
			healthStatus: "Healthy",
			want:         types.LifecycleDeploySynced,
		},
		{
			name:         "empty sync status → deploy_synced",
			syncStatus:   "",
			healthStatus: "Healthy",
			want:         types.LifecycleDeploySynced,
		},
		{
			name:         "unknown sync status → deploy_synced",
			syncStatus:   "Unknown",
			healthStatus: "",
			want:         types.LifecycleDeploySynced,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argocdEventType(tt.syncStatus, tt.healthStatus)
			if got != tt.want {
				t.Errorf("argocdEventType(%q, %q) = %q, want %q",
					tt.syncStatus, tt.healthStatus, got, tt.want)
			}
		})
	}
}

func TestDeploymentStatusCancelledConstant(t *testing.T) {
	// Verify the cancelled status constant exists and has the expected value.
	// This guards against accidental removal or renaming of the constant
	// used by CleanupStaleDeploying.
	if types.DeploymentStatusCancelled != "cancelled" {
		t.Errorf("DeploymentStatusCancelled = %q, want %q",
			types.DeploymentStatusCancelled, "cancelled")
	}
}

func TestRepoFullNameFromImage(t *testing.T) {
	tests := []struct {
		name     string
		imageURI string
		want     string
	}{
		{
			name:     "enclii image",
			imageURI: "ghcr.io/madfam-org/enclii/switchyard-api:latest",
			want:     "madfam-org/enclii",
		},
		{
			name:     "dhanam image with digest",
			imageURI: "ghcr.io/madfam-org/dhanam/api@sha256:abc123",
			want:     "madfam-org/dhanam",
		},
		{
			name:     "tezca image",
			imageURI: "ghcr.io/madfam-org/tezca/web:main",
			want:     "madfam-org/tezca",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repoFullNameFromImage(tt.imageURI)
			if got != tt.want {
				t.Errorf("repoFullNameFromImage(%q) = %q, want %q", tt.imageURI, got, tt.want)
			}
		})
	}
}
