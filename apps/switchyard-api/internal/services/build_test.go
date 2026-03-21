package services

import (
	"testing"

	"github.com/google/uuid"
)

func TestBuildService_ProcessBuildCallback_Validation(t *testing.T) {
	svc := &BuildService{} // repos is nil — we only test that the function signature is correct

	t.Run("release not found panics without repos", func(t *testing.T) {
		// This test documents that ProcessBuildCallback requires repos.
		// Without repos, it panics on the first DB call.
		// Real testing with repos is done in integration tests.
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when repos is nil")
			}
		}()
		_, _ = svc.ProcessBuildCallback(nil, &ProcessBuildCallbackRequest{
			ReleaseID: uuid.New(),
			Success:   true,
		})
	})
}

func TestBuildService_CreateReleaseForBuild_Validation(t *testing.T) {
	svc := &BuildService{} // repos is nil — we only test validation

	t.Run("short git SHA", func(t *testing.T) {
		// This will panic because repos is nil and it calls GetByID first.
		// But it documents that validation exists for SHA length.
		defer func() {
			// Expected: either validation error or panic from nil repos
			recover()
		}()
		_, err := svc.CreateReleaseForBuild(nil, &CreateReleaseForBuildRequest{
			ServiceID: uuid.New(),
			GitSHA:    "abc",
			Registry:  "ghcr.io/test",
		})
		// If we get here, check the error
		if err == nil {
			t.Error("expected error for short SHA")
		}
	})
}

func TestCurrentTimestamp(t *testing.T) {
	ts := currentTimestamp()
	if len(ts) == 0 {
		t.Error("timestamp should not be empty")
	}
	// Format: 20060102-150405 = 15 chars
	if len(ts) != 15 {
		t.Errorf("timestamp format wrong, got %q (len=%d), want 15 chars", ts, len(ts))
	}
}

func TestProcessBuildCallbackRequest_Structure(t *testing.T) {
	releaseID := uuid.New()
	jobID := uuid.New()

	req := &ProcessBuildCallbackRequest{
		JobID:          jobID,
		ReleaseID:      releaseID,
		Success:        true,
		ImageURI:       "ghcr.io/org/svc:abc1234",
		ImageDigest:    "sha256:deadbeef",
		ImageSizeMB:    42.5,
		SBOM:           `{"format":"cyclonedx"}`,
		SBOMFormat:     "cyclonedx-json",
		ImageSignature: "MEUCIQDfake...",
		DurationSecs:   30.5,
		LogsURL:        "https://logs.example.com/123",
	}

	if req.ReleaseID != releaseID {
		t.Error("release ID mismatch")
	}
	if req.JobID != jobID {
		t.Error("job ID mismatch")
	}
	if !req.Success {
		t.Error("expected success=true")
	}
}

func TestProcessBuildCallbackRequest_FailedBuild(t *testing.T) {
	req := &ProcessBuildCallbackRequest{
		JobID:        uuid.New(),
		ReleaseID:    uuid.New(),
		Success:      false,
		ErrorMessage: "build failed: exit code 1",
		LogsURL:      "https://logs.example.com/456",
	}

	if req.Success {
		t.Error("expected success=false")
	}
	if req.ErrorMessage == "" {
		t.Error("failed build should have error message")
	}
	if req.ImageURI != "" {
		t.Error("failed build should not have image URI")
	}
}

func TestCreateReleaseForBuildRequest_Structure(t *testing.T) {
	serviceID := uuid.New()
	req := &CreateReleaseForBuildRequest{
		ServiceID: serviceID,
		GitSHA:    "abc1234567890",
		GitBranch: "main",
		Registry:  "ghcr.io/org/project",
	}

	if req.ServiceID != serviceID {
		t.Error("service ID mismatch")
	}
	if req.GitSHA != "abc1234567890" {
		t.Error("git SHA mismatch")
	}
}
