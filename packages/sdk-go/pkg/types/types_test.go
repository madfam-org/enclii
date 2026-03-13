package types

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestBuildTypeConstants(t *testing.T) {
	tests := []struct {
		bt   BuildType
		want string
	}{
		{BuildTypeAuto, "auto"},
		{BuildTypeDockerfile, "dockerfile"},
		{BuildTypeBuildpack, "buildpack"},
	}
	for _, tt := range tests {
		if string(tt.bt) != tt.want {
			t.Errorf("BuildType = %q, want %q", tt.bt, tt.want)
		}
	}
}

func TestDeploymentStatusConstants(t *testing.T) {
	statuses := []DeploymentStatus{
		DeploymentStatusPending,
		DeploymentStatusDeploying,
		DeploymentStatusRunning,
		DeploymentStatusFailed,
		DeploymentStatusCancelled,
	}
	expected := []string{"pending", "deploying", "running", "failed", "cancelled"}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("DeploymentStatus[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestReleaseStatusConstants(t *testing.T) {
	statuses := []ReleaseStatus{
		ReleaseStatusBuilding,
		ReleaseStatusReady,
		ReleaseStatusFailed,
	}
	expected := []string{"building", "ready", "failed"}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("ReleaseStatus[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestHealthStatusConstants(t *testing.T) {
	statuses := []HealthStatus{
		HealthStatusUnknown,
		HealthStatusHealthy,
		HealthStatusUnhealthy,
	}
	expected := []string{"unknown", "healthy", "unhealthy"}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("HealthStatus[%d] = %q, want %q", i, s, expected[i])
		}
	}
}

func TestRoleConstants(t *testing.T) {
	roles := []Role{RoleAdmin, RoleDeveloper, RoleViewer, RoleSystem}
	expected := []string{"admin", "developer", "viewer", "system"}

	for i, r := range roles {
		if string(r) != expected[i] {
			t.Errorf("Role[%d] = %q, want %q", i, r, expected[i])
		}
	}
}

func TestProjectJSONRoundTrip(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	p := &Project{
		ID:   id,
		Name: "Test Project",
		Slug: "test-project",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Project
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != id {
		t.Errorf("ID = %v, want %v", decoded.ID, id)
	}
	if decoded.Name != "Test Project" {
		t.Errorf("Name = %q, want %q", decoded.Name, "Test Project")
	}
	if decoded.Slug != "test-project" {
		t.Errorf("Slug = %q, want %q", decoded.Slug, "test-project")
	}
}

func TestServiceJSONMarshal(t *testing.T) {
	svc := &Service{
		Name:       "api",
		GitRepo:    "https://github.com/org/repo",
		AutoDeploy: true,
		Health:     HealthStatusHealthy,
		Status:     "running",
	}

	data, err := json.Marshal(svc)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if m["name"] != "api" {
		t.Errorf("name = %v, want %q", m["name"], "api")
	}
	if m["auto_deploy"] != true {
		t.Errorf("auto_deploy = %v, want true", m["auto_deploy"])
	}
	if m["health"] != "healthy" {
		t.Errorf("health = %v, want %q", m["health"], "healthy")
	}
}

func TestUserPasswordHashHidden(t *testing.T) {
	u := &User{
		Email:        "test@example.com",
		PasswordHash: "secret-hash",
		Name:         "Test User",
		Role:         "admin",
	}

	data, err := json.Marshal(u)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	if _, ok := m["password_hash"]; ok {
		t.Error("password_hash should not be in JSON output")
	}
	if m["email"] != "test@example.com" {
		t.Errorf("email = %v, want %q", m["email"], "test@example.com")
	}
}

func TestPreviewEnvironmentStatusConstants(t *testing.T) {
	statuses := []PreviewEnvironmentStatus{
		PreviewStatusPending,
		PreviewStatusBuilding,
		PreviewStatusDeploying,
		PreviewStatusActive,
		PreviewStatusSleeping,
		PreviewStatusFailed,
		PreviewStatusClosed,
	}
	expected := []string{"pending", "building", "deploying", "active", "sleeping", "failed", "closed"}

	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("PreviewStatus[%d] = %q, want %q", i, s, expected[i])
		}
	}
}
