package services

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNewClusterAdminService(t *testing.T) {
	logger := logrus.New()
	svc := NewClusterAdminService(nil, logger)
	if svc == nil {
		t.Fatal("expected non-nil ClusterAdminService")
	}
	if svc.logger != logger {
		t.Error("expected logger to be set")
	}
}

func TestClusterSlugValidation(t *testing.T) {
	// Slug validation tests - these test the expected format for cluster slugs
	tests := []struct {
		name  string
		slug  string
		valid bool
	}{
		{"simple slug", "foundry-core", true},
		{"with numbers", "cluster-01", true},
		{"single word", "production", true},
		{"empty slug", "", false},
		{"with spaces", "my cluster", false},
		{"with uppercase", "MyCluster", false},
		{"with underscore", "my_cluster", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := isValidSlug(tt.slug)
			if valid != tt.valid {
				t.Errorf("slug %q: want valid=%v, got %v", tt.slug, tt.valid, valid)
			}
		})
	}
}

// isValidSlug is defined in projects.go - reused here for cluster slug validation
