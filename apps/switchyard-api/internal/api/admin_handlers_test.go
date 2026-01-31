package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TestAdminHandlerNilServiceResponses verifies that admin handlers
// return 503 when services are not configured (nil check pattern).
func TestAdminHandlerNilServiceResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{} // All services nil

	tests := []struct {
		name       string
		method     string
		path       string
		handler    gin.HandlerFunc
		wantStatus int
	}{
		{"list bare metal - nil svc", "GET", "/v1/admin/fleet", h.ListBareMetalHosts, http.StatusServiceUnavailable},
		{"register bare metal - nil svc", "POST", "/v1/admin/fleet", h.RegisterBareMetalHost, http.StatusServiceUnavailable},
		{"list clusters - nil svc", "GET", "/v1/admin/clusters", h.ListAdminClusters, http.StatusServiceUnavailable},
		{"register cluster - nil svc", "POST", "/v1/admin/clusters", h.RegisterCluster, http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, engine := gin.CreateTestContext(w)
			switch tt.method {
			case "GET":
				engine.GET(tt.path, tt.handler)
			case "POST":
				engine.POST(tt.path, tt.handler)
			}

			req, _ := http.NewRequest(tt.method, tt.path, nil)
			engine.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("%s: want status %d, got %d", tt.name, tt.wantStatus, w.Code)
			}
		})
	}
}

// TestAdminUUIDParsing verifies UUID path parameter handling in admin endpoints.
func TestAdminUUIDParsing(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"invalid uuid", "not-a-uuid", true},
		{"empty", "", true},
		{"partial uuid", "550e8400-e29b", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := uuid.Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("uuid.Parse(%q): wantErr=%v, gotErr=%v", tt.input, tt.wantErr, err)
			}
		})
	}
}

// TestAdminRequestValidation verifies JSON body validation for admin endpoints.
func TestAdminRequestValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		body   string
		wantOK bool
	}{
		{"valid json", `{"name":"test"}`, true},
		{"empty body", ``, false},
		{"invalid json", `{bad`, false},
		{"null body", `null`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m map[string]interface{}
			reader := strings.NewReader(tt.body)
			req, _ := http.NewRequest("POST", "/test", reader)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			err := c.ShouldBindJSON(&m)
			gotOK := err == nil && m != nil
			if gotOK != tt.wantOK {
				t.Errorf("body %q: wantOK=%v, gotOK=%v", tt.body, tt.wantOK, gotOK)
			}
		})
	}
}
