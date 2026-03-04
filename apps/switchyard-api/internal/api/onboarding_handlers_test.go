package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
)

func TestComputeOnboardStatus(t *testing.T) {
	tests := []struct {
		name   string
		steps  []stepResult
		expect string
	}{
		{
			"all ok",
			[]stepResult{
				{Name: "namespace", Critical: true, Err: nil, Status: "ok"},
				{Name: "argocd_config", Critical: true, Err: nil, Status: "ok"},
				{Name: "postgres", Critical: false, Err: nil, Status: "ok"},
			},
			"completed",
		},
		{
			"critical failure",
			[]stepResult{
				{Name: "namespace", Critical: true, Err: fmt.Errorf("k8s unavailable"), Status: "failed"},
				{Name: "argocd_config", Critical: true, Err: nil, Status: "ok"},
			},
			"failed",
		},
		{
			"non-critical failure",
			[]stepResult{
				{Name: "namespace", Critical: true, Err: nil, Status: "ok"},
				{Name: "argocd_config", Critical: true, Err: nil, Status: "ok"},
				{Name: "postgres", Critical: false, Err: fmt.Errorf("db timeout"), Status: "failed"},
			},
			"partial",
		},
		{
			"critical overrides non-critical",
			[]stepResult{
				{Name: "namespace", Critical: true, Err: fmt.Errorf("fail"), Status: "failed"},
				{Name: "postgres", Critical: false, Err: fmt.Errorf("also fail"), Status: "failed"},
			},
			"failed",
		},
		{
			"empty steps",
			[]stepResult{},
			"completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeOnboardStatus(tt.steps)
			if got != tt.expect {
				t.Errorf("computeOnboardStatus() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestOnboardRepoRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{} // All deps nil — tests JSON binding validation only

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			"empty body",
			``,
			http.StatusBadRequest,
		},
		{
			"missing required fields",
			`{}`,
			http.StatusBadRequest,
		},
		// Note: "invalid repo format" requires h.repos to be non-nil (GetByRepo check comes first).
		// That test requires a full integration setup.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.POST("/v1/admin/onboard", h.OnboardRepo)

			req, _ := http.NewRequest("POST", "/v1/admin/onboard", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("%s: want status %d, got %d (body: %s)", tt.name, tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestPreflightOnboardRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{config: &config.Config{}} // config non-nil but GitHubToken empty

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			"empty body",
			``,
			http.StatusBadRequest,
		},
		{
			"missing required fields",
			`{}`,
			http.StatusBadRequest,
		},
		{
			"no github token configured",
			`{"repo_full_name":"org/repo","project_name":"test"}`,
			http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			engine.POST("/v1/admin/onboard/preflight", h.PreflightOnboard)

			req, _ := http.NewRequest("POST", "/v1/admin/onboard/preflight", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			engine.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("%s: want status %d, got %d (body: %s)", tt.name, tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}
